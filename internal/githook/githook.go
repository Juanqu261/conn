// Package githook manages Conn's optional commit-msg hook and adds the
// Conn-Intent commit trailer. Conn writes to the repository hooks directory
// only through explicit installation, which targets the effective hooks
// directory including core.hooksPath, never overwrites a different hook, and
// removes only byte-identical Conn content.
//
// The hook runs after the commit message is edited, and a message that is
// empty apart from comments is left untouched, so Git still aborts it.
// Version 1 used prepare-commit-msg, which runs before the editor; its
// trailer made an abandoned message non-empty and Git committed it.
// Installation and removal delete an unchanged version 1 hook.
package githook

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/Juanqu261/conn/internal/repositorygit"
)

// HookName is the Git hook Conn installs.
const HookName = "commit-msg"

// marker identifies Conn's hook content and its version.
const marker = "# conn commit-msg v2"

// LegacyHookName is the hook Conn installed in version 1.
const LegacyHookName = "prepare-commit-msg"

const (
	legacyMarker = "# conn prepare-commit-msg v1"
	legacySuffix = ` trailer "$1" "$2" || true`
	hookSuffix   = ` trailer "$1" || true`
)

// PathCommand runs conn from PATH.
const PathCommand = "conn"

// Mode is a planned operation.
type Mode string

const (
	ModeInstall Mode = "install"
	ModeRemove  Mode = "remove"
)

// Action is what applying a plan does to the hook file.
type Action string

const (
	ActionCreate    Action = "create"
	ActionUnchanged Action = "unchanged"
	ActionRemove    Action = "remove"
	ActionAbsent    Action = "absent"
	ActionConflict  Action = "conflict"
)

// Plan is an inspected, not yet applied, hook change.
type Plan struct {
	Mode     Mode
	HooksDir string
	Path     string
	Command  string
	Action   Action
	Content  []byte
	existing []byte
	// LegacyPath is the version 1 prepare-commit-msg hook. LegacyAction is
	// ActionRemove for unchanged version 1 content, ActionConflict for a
	// modified version 1 hook left for manual review, and ActionAbsent
	// otherwise, including for any hook that is not Conn's.
	LegacyPath     string
	LegacyAction   Action
	legacyExisting []byte
}

// HasConflict reports whether a different hook occupies the target.
func (p Plan) HasConflict() bool { return p.Action == ActionConflict }

// Snippet is the line to add to an existing hook to chain Conn manually.
func (p Plan) Snippet() string { return HookLine(p.Command) }

// RenderCommand quotes an executable path for a POSIX shell, which runs Git
// hooks on every platform, including Git for Windows.
func RenderCommand(executable string) string {
	if executable == "" {
		return PathCommand
	}
	path := filepath.ToSlash(executable)
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`, "$", `\$`, "`", "\\`").Replace(path) + `"`
}

// HookLine is the hook command: it never fails the commit.
func HookLine(command string) string {
	return command + hookSuffix
}

// Content renders the complete hook file for command.
func Content(command string) []byte {
	return []byte("#!/bin/sh\n" + marker + "\n" + HookLine(command) + "\n")
}

// legacyContent renders the version 1 prepare-commit-msg hook.
func legacyContent(command string) []byte {
	return []byte("#!/bin/sh\n" + legacyMarker + "\n" + command + legacySuffix + "\n")
}

// HooksDir returns the effective hooks directory of the worktree, honoring
// core.hooksPath and linked worktrees.
func HooksDir(ctx context.Context, worktree string) (string, error) {
	output, err := repositorygit.Output(ctx, worktree, 4096, "rev-parse", "--git-path", "hooks")
	if err != nil {
		return "", errors.New("the directory is not inside a Git worktree")
	}
	dir := filepath.FromSlash(strings.TrimSpace(output))
	if !filepath.IsAbs(dir) {
		dir = filepath.Join(worktree, dir)
	}
	return filepath.Clean(dir), nil
}

// Preview inspects the hook target and plans mode without writing.
func Preview(ctx context.Context, worktree string, mode Mode, command string) (Plan, error) {
	if mode != ModeInstall && mode != ModeRemove {
		return Plan{}, fmt.Errorf("unknown mode %q", mode)
	}
	dir, err := HooksDir(ctx, worktree)
	if err != nil {
		return Plan{}, err
	}
	plan := Plan{Mode: mode, HooksDir: dir, Path: filepath.Join(dir, HookName), Command: command}
	existing, err := os.ReadFile(plan.Path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		existing = nil
	case err != nil:
		return Plan{}, fmt.Errorf("read %s: %w", plan.Path, err)
	}
	plan.existing = existing
	if err := plan.inspectLegacy(); err != nil {
		return Plan{}, err
	}
	switch mode {
	case ModeInstall:
		plan.Content = Content(command)
		switch {
		case existing == nil:
			plan.Action = ActionCreate
		case bytes.Equal(existing, plan.Content):
			plan.Action = ActionUnchanged
		default:
			plan.Action = ActionConflict
		}
	case ModeRemove:
		switch installed, ok := installedCommand(existing, marker, hookSuffix); {
		case existing == nil:
			plan.Action = ActionAbsent
		case ok && bytes.Equal(existing, Content(installed)):
			plan.Command, plan.Action = installed, ActionRemove
		default:
			plan.Action = ActionConflict
		}
	}
	return plan, nil
}

// inspectLegacy plans the removal of an unchanged version 1 hook.
func (p *Plan) inspectLegacy() error {
	p.LegacyPath, p.LegacyAction = filepath.Join(p.HooksDir, LegacyHookName), ActionAbsent
	existing, err := os.ReadFile(p.LegacyPath)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read %s: %w", p.LegacyPath, err)
	}
	p.legacyExisting = existing
	if installed, ok := installedCommand(existing, legacyMarker, legacySuffix); ok && bytes.Equal(existing, legacyContent(installed)) {
		p.LegacyAction = ActionRemove
	} else if bytes.Contains(existing, []byte(legacyMarker)) {
		p.LegacyAction = ActionConflict
	}
	return nil
}

// Apply performs a conflict-free plan, rechecking that the targets have not
// changed since they were previewed.
func Apply(plan Plan) error {
	if plan.HasConflict() {
		return errors.New("the hook target contains a different hook")
	}
	if err := unchanged(plan.Path, plan.existing); err != nil {
		return err
	}
	removeLegacy := plan.LegacyAction == ActionRemove
	if removeLegacy {
		if err := unchanged(plan.LegacyPath, plan.legacyExisting); err != nil {
			return err
		}
	}
	switch plan.Action {
	case ActionCreate:
		if err := os.MkdirAll(plan.HooksDir, 0o755); err != nil {
			return err
		}
		file, err := os.OpenFile(plan.Path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o755)
		if err != nil {
			return err
		}
		_, writeErr := file.Write(plan.Content)
		if err := errors.Join(writeErr, file.Close()); err != nil {
			return err
		}
	case ActionRemove:
		if err := os.Remove(plan.Path); err != nil {
			return err
		}
	}
	// The version 1 hook goes only after the current hook is in place, so a
	// failed installation never leaves the worktree without either.
	if removeLegacy {
		return os.Remove(plan.LegacyPath)
	}
	return nil
}

// unchanged reports an error when path no longer holds inspected.
func unchanged(path string, inspected []byte) error {
	current, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		current, err = nil, nil
	}
	if err != nil || !bytes.Equal(current, inspected) {
		return errors.New("the hook target changed after it was inspected; preview again")
	}
	return nil
}

func installedCommand(content []byte, wantMarker, suffix string) (string, bool) {
	lines := strings.Split(string(content), "\n")
	if len(lines) != 4 || lines[0] != "#!/bin/sh" || lines[1] != wantMarker || lines[3] != "" {
		return "", false
	}
	return strings.CutSuffix(lines[2], suffix)
}

// scissors is the line below which `git commit --verbose` shows the diff.
const scissors = "------------------------ >8 ------------------------"

// MessageEmpty reports whether a commit message file holds nothing but
// whitespace, comments, and the verbose diff below the scissors line: the
// message Git aborts as empty. `git stripspace` removes the comments so
// core.commentChar is honored.
func MessageEmpty(ctx context.Context, messageFile string) (bool, error) {
	data, err := os.ReadFile(messageFile)
	if err != nil {
		return false, err
	}
	lines := strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n")
	for i, line := range lines {
		if trimmed := strings.TrimSpace(line); strings.HasSuffix(trimmed, scissors) && len(trimmed) <= len(scissors)+3 {
			lines = lines[:i]
			break
		}
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", "stripspace", "--strip-comments")
	cmd.Env, cmd.Stdin = repositorygit.Environment(), strings.NewReader(strings.Join(lines, "\n"))
	output, err := cmd.Output()
	if err != nil {
		return false, fmt.Errorf("git stripspace failed: %w", err)
	}
	return strings.TrimSpace(string(output)) == "", nil
}

// AppendTrailer adds trailer to the commit message file unless the message
// already carries a trailer with the same key. It uses
// git interpret-trailers with fixed arguments and no shell.
func AppendTrailer(ctx context.Context, messageFile, trailer string) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", "interpret-trailers", "--in-place", "--where", "end", "--if-exists", "doNothing", "--trailer", trailer, "--", messageFile)
	cmd.Env = repositorygit.Environment()
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git interpret-trailers failed: %s", strings.TrimSpace(string(bounded(output))))
	}
	return nil
}

func bounded(output []byte) []byte {
	if len(output) > 200 {
		return output[:200]
	}
	return output
}
