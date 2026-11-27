// Package agentguide is the single source of Conn's work-loop guidance for
// coding agents. It renders the optional Kiro steering file and the Conn
// section of a repository's AGENTS.md, and manages that section between fixed
// markers without changing content outside them.
package agentguide

import (
	"bytes"
	_ "embed"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

//go:embed templates/guide.md
var guide string

// Section markers. A different version uses a different begin marker, so
// removal stays exact.
const (
	BeginMarker = "<!-- conn:begin v1 -->"
	EndMarker   = "<!-- conn:end -->"
	// DefaultFile is the conventional repository instruction file.
	DefaultFile = "AGENTS.md"
)

const kiroFrontMatter = "---\n\n## inclusion: always\n\n"

var promptPattern = regexp.MustCompile(`\{\{prompt:([a-z-]+)\}\}`)

// render keeps lines for the audience and names prompts the way it sees them.
func render(kiro bool) string {
	var out strings.Builder
	for _, line := range strings.SplitAfter(guide, "\n") {
		// A Markdown formatter may indent a tagged line that follows a list.
		trimmed := strings.TrimLeft(line, " ")
		switch {
		case strings.HasPrefix(trimmed, "{{kiro}}"):
			if !kiro {
				continue
			}
			line = strings.TrimPrefix(trimmed, "{{kiro}}")
		case strings.HasPrefix(trimmed, "{{generic}}"):
			if kiro {
				continue
			}
			line = strings.TrimPrefix(trimmed, "{{generic}}")
		}
		out.WriteString(promptPattern.ReplaceAllStringFunc(line, func(match string) string {
			name := promptPattern.FindStringSubmatch(match)[1]
			if kiro {
				return "`/" + name + "`"
			}
			return "the `" + name + "` prompt"
		}))
	}
	return out.String()
}

// KiroSteering returns the Kiro steering file.
func KiroSteering() []byte { return []byte(kiroFrontMatter + render(true)) }

// Section returns the marker-delimited Conn section for AGENTS.md.
func Section() string {
	return BeginMarker + "\n" + strings.TrimRight(render(false), "\n") + "\n" + EndMarker + "\n"
}

// knownSections are every Conn section version Conn may replace or remove.
func knownSections() []string { return []string{Section()} }

// Mode selects installation or removal.
type Mode string

const (
	ModeInstall Mode = "install"
	ModeRemove  Mode = "remove"
)

// Action is what applying a plan does.
type Action string

const (
	ActionCreateFile Action = "create"
	ActionAppend     Action = "append"
	ActionReplace    Action = "replace"
	ActionUnchanged  Action = "unchanged"
	ActionRemove     Action = "remove"
	ActionDeleteFile Action = "delete"
	ActionAbsent     Action = "absent"
	ActionConflict   Action = "conflict"
)

// ErrConflict means the Conn section was modified or the markers are broken.
var ErrConflict = errors.New("the Conn section in the instruction file was modified")

// Plan is a previewed change to one instruction file.
type Plan struct {
	Path     string
	Mode     Mode
	Action   Action
	Existing []byte
	Result   []byte
	Reason   string
}

// Preview inspects the instruction file and computes the result without
// writing. file is relative to root and must stay inside it.
func Preview(root, file string, mode Mode) (Plan, error) {
	if mode != ModeInstall && mode != ModeRemove {
		return Plan{}, errors.New("mode must be install or remove")
	}
	if file == "" {
		file = DefaultFile
	}
	path, err := target(root, file)
	if err != nil {
		return Plan{}, err
	}
	plan := Plan{Path: path, Mode: mode}
	existing, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		if mode == ModeInstall {
			plan.Action, plan.Result = ActionCreateFile, []byte(Section())
		} else {
			plan.Action = ActionAbsent
		}
		return plan, nil
	}
	if err != nil {
		return Plan{}, err
	}
	plan.Existing = existing
	start, end, found, broken := locate(existing)
	switch {
	case broken:
		plan.Action, plan.Reason = ActionConflict, "the Conn markers are incomplete or repeated"
		return plan, nil
	case !found && mode == ModeRemove:
		plan.Action = ActionAbsent
		return plan, nil
	case !found:
		separator := ""
		if len(existing) > 0 && !bytes.HasSuffix(existing, []byte("\n")) {
			separator = "\n"
		}
		if len(existing) > 0 {
			separator += "\n"
		}
		plan.Action, plan.Result = ActionAppend, append(append(append([]byte{}, existing...), separator...), Section()...)
		return plan, nil
	}
	current := string(existing[start:end])
	known := false
	for _, section := range knownSections() {
		known = known || current == section
	}
	if !known {
		plan.Action, plan.Reason = ActionConflict, "the Conn section differs from every version Conn wrote"
		return plan, nil
	}
	if mode == ModeInstall {
		if current == Section() {
			plan.Action, plan.Result = ActionUnchanged, existing
			return plan, nil
		}
		plan.Action = ActionReplace
		plan.Result = append(append(append([]byte{}, existing[:start]...), Section()...), existing[end:]...)
		return plan, nil
	}
	before, after := existing[:start], existing[end:]
	// Undo the blank line that installation added before the section.
	if bytes.HasSuffix(before, []byte("\n\n")) {
		before = before[:len(before)-1]
	}
	if len(bytes.TrimSpace(before)) == 0 && len(bytes.TrimSpace(after)) == 0 {
		plan.Action = ActionDeleteFile
		return plan, nil
	}
	plan.Action, plan.Result = ActionRemove, append(append([]byte{}, before...), after...)
	return plan, nil
}

// locate finds the Conn section, from the begin marker through the end
// marker line. broken reports a missing or repeated marker.
func locate(content []byte) (start, end int, found, broken bool) {
	begins := bytes.Count(content, []byte(BeginMarker))
	ends := bytes.Count(content, []byte(EndMarker))
	anyBegin := regexp.MustCompile(`<!-- conn:begin v[0-9]+ -->`).FindAllIndex(content, -1)
	if begins == 0 && ends == 0 && len(anyBegin) == 0 {
		return 0, 0, false, false
	}
	if begins != 1 || ends != 1 || len(anyBegin) != 1 {
		return 0, 0, false, true
	}
	start = bytes.Index(content, []byte(BeginMarker))
	endMarker := bytes.Index(content, []byte(EndMarker))
	if endMarker < start {
		return 0, 0, false, true
	}
	end = endMarker + len(EndMarker)
	if end < len(content) && content[end] == '\n' {
		end++
	}
	return start, end, true, false
}

// Apply re-reads the file and performs the plan. A file that changed since
// the preview stops it.
func Apply(plan Plan) error {
	current, err := Preview(filepath.Dir(plan.Path), filepath.Base(plan.Path), plan.Mode)
	if err != nil {
		return err
	}
	if current.Action == ActionConflict || current.Action != plan.Action || !bytes.Equal(current.Existing, plan.Existing) {
		return ErrConflict
	}
	switch current.Action {
	case ActionCreateFile:
		file, err := os.OpenFile(current.Path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
		if err != nil {
			if errors.Is(err, fs.ErrExist) {
				return ErrConflict
			}
			return err
		}
		_, writeErr := file.Write(current.Result)
		closeErr := file.Close()
		if writeErr != nil || closeErr != nil {
			_ = os.Remove(current.Path)
			return errors.Join(writeErr, closeErr)
		}
	case ActionAppend, ActionReplace, ActionRemove:
		info, err := os.Stat(current.Path)
		if err != nil {
			return err
		}
		temporary := current.Path + ".conn-tmp"
		if err := os.WriteFile(temporary, current.Result, info.Mode().Perm()); err != nil {
			return err
		}
		if err := os.Rename(temporary, current.Path); err != nil {
			_ = os.Remove(temporary)
			return err
		}
	case ActionDeleteFile:
		return os.Remove(current.Path)
	}
	return nil
}

// target resolves the instruction file inside root, refusing traversal,
// absolute paths, and symbolic links.
func target(root, file string) (string, error) {
	if root == "" {
		var err error
		if root, err = os.Getwd(); err != nil {
			return "", err
		}
	}
	resolved, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", fmt.Errorf("resolve the root: %w", err)
	}
	if filepath.IsAbs(file) || filepath.VolumeName(file) != "" || strings.Contains(filepath.ToSlash(file), "..") || filepath.Dir(filepath.Clean(file)) != "." {
		return "", errors.New("the instruction file must be a file name in the root directory, such as AGENTS.md")
	}
	if !strings.HasSuffix(strings.ToLower(file), ".md") {
		return "", errors.New("the instruction file must be a Markdown file")
	}
	path := filepath.Join(resolved, filepath.Clean(file))
	if info, err := os.Lstat(path); err == nil && !info.Mode().IsRegular() {
		return "", errors.New("the instruction file is not a regular file")
	}
	return path, nil
}
