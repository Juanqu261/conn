// Package kirointegration manages Conn's optional, inspectable Kiro guidance
// bundle. It never edits MCP settings, agents, skills, or authoritative state.
package kirointegration

import (
	"bytes"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type Scope string

const (
	ScopeWorkspace Scope = "workspace"
	ScopeUser      Scope = "user"
)

type Mode string

const (
	ModeInstall Mode = "install"
	ModeRemove  Mode = "remove"
)

type Action string

const (
	ActionCreate    Action = "create"
	ActionUnchanged Action = "unchanged"
	ActionRemove    Action = "remove"
	ActionAbsent    Action = "absent"
	ActionConflict  Action = "conflict"
)

var (
	ErrConflict = errors.New("the Conn Kiro integration conflicts with existing files")
	ErrUnsafe   = errors.New("the Conn Kiro integration target is unsafe")
)

//go:embed templates/steering/conn-work-loop.md templates/hooks/conn-lifecycle.json
var templateFiles embed.FS

type Artifact struct {
	RelativePath string
	Content      []byte
}

type Change struct {
	RelativePath string
	Action       Action
}

type Plan struct {
	Scope     Scope
	Root      string
	Mode      Mode
	Changes   []Change
	Preserved []string
}

func (p Plan) HasConflicts() bool {
	for _, change := range p.Changes {
		if change.Action == ActionConflict {
			return true
		}
	}
	return false
}

// ResolveRoot resolves the configuration root without creating it. Workspace
// defaults to the current directory; user defaults to the operating-system
// home directory. An override is useful for explicit alternate workspaces and
// deterministic tests.
func ResolveRoot(scope Scope, override string) (string, error) {
	if scope != ScopeWorkspace && scope != ScopeUser {
		return "", fmt.Errorf("scope must be workspace or user")
	}
	root := override
	var err error
	if root == "" {
		if scope == ScopeUser {
			root, err = os.UserHomeDir()
		} else {
			root, err = os.Getwd()
		}
		if err != nil {
			return "", err
		}
	}
	root, err = filepath.Abs(filepath.Clean(root))
	if err != nil {
		return "", err
	}
	resolved, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", fmt.Errorf("resolve integration root: %w", err)
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return "", fmt.Errorf("inspect integration root: %w", err)
	}
	if !info.IsDir() {
		return "", errors.New("integration root is not a directory")
	}
	return resolved, nil
}

// Artifacts returns copies of the exact versioned files in the optional
// bundle. Callers cannot alter the embedded source through the returned bytes.
func Artifacts() ([]Artifact, error) {
	definitions := []struct {
		template string
		target   string
	}{
		{"templates/steering/conn-work-loop.md", filepath.Join(".kiro", "steering", "conn-work-loop.md")},
		{"templates/hooks/conn-lifecycle.json", filepath.Join(".kiro", "hooks", "conn-lifecycle.json")},
	}
	result := make([]Artifact, 0, len(definitions))
	for _, definition := range definitions {
		content, err := fs.ReadFile(templateFiles, definition.template)
		if err != nil {
			return nil, err
		}
		result = append(result, Artifact{RelativePath: definition.target, Content: append([]byte(nil), content...)})
	}
	return result, nil
}

// Preview reads the exact target files and inventories unrelated steering and
// hook entries. It never creates a directory or writes a file.
func Preview(scope Scope, root string, mode Mode) (Plan, error) {
	if scope != ScopeWorkspace && scope != ScopeUser {
		return Plan{}, errors.New("scope must be workspace or user")
	}
	if mode != ModeInstall && mode != ModeRemove {
		return Plan{}, errors.New("mode must be install or remove")
	}
	resolvedRoot, err := ResolveRoot(scope, root)
	if err != nil {
		return Plan{}, err
	}
	artifacts, err := Artifacts()
	if err != nil {
		return Plan{}, err
	}
	plan := Plan{Scope: scope, Root: resolvedRoot, Mode: mode, Changes: make([]Change, 0, len(artifacts))}
	targets := make(map[string]bool, len(artifacts))
	for _, artifact := range artifacts {
		targets[filepath.Clean(artifact.RelativePath)] = true
		target, err := safeTarget(resolvedRoot, artifact.RelativePath)
		if err != nil {
			return Plan{}, err
		}
		content, err := readExistingTarget(target)
		switch {
		case errors.Is(err, fs.ErrNotExist):
			action := ActionCreate
			if mode == ModeRemove {
				action = ActionAbsent
			}
			plan.Changes = append(plan.Changes, Change{RelativePath: artifact.RelativePath, Action: action})
		case err != nil:
			return Plan{}, err
		case bytes.Equal(content, artifact.Content):
			action := ActionUnchanged
			if mode == ModeRemove {
				action = ActionRemove
			}
			plan.Changes = append(plan.Changes, Change{RelativePath: artifact.RelativePath, Action: action})
		default:
			plan.Changes = append(plan.Changes, Change{RelativePath: artifact.RelativePath, Action: ActionConflict})
		}
	}
	plan.Preserved, err = inventoryUnrelated(resolvedRoot, targets)
	if err != nil {
		return Plan{}, err
	}
	return plan, nil
}

// Apply re-reads every target before changing anything. Modified target files
// cause the whole operation to stop. Installation uses exclusive creation and
// removal deletes only byte-for-byte Conn artifacts.
func Apply(plan Plan) error {
	current, err := Preview(plan.Scope, plan.Root, plan.Mode)
	if err != nil {
		return err
	}
	if current.HasConflicts() {
		return ErrConflict
	}
	artifacts, err := Artifacts()
	if err != nil {
		return err
	}
	byPath := make(map[string]Artifact, len(artifacts))
	for _, artifact := range artifacts {
		byPath[filepath.Clean(artifact.RelativePath)] = artifact
	}
	if plan.Mode == ModeInstall {
		return install(current.Root, current.Changes, byPath)
	}
	return remove(current.Root, current.Changes, byPath)
}

func install(root string, changes []Change, artifacts map[string]Artifact) error {
	created := make([]string, 0, len(changes))
	rollback := func() {
		for i := len(created) - 1; i >= 0; i-- {
			_ = os.Remove(created[i])
		}
	}
	for _, change := range changes {
		if change.Action != ActionCreate {
			continue
		}
		artifact := artifacts[filepath.Clean(change.RelativePath)]
		target, err := safeTarget(root, artifact.RelativePath)
		if err != nil {
			rollback()
			return err
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			rollback()
			return err
		}
		if _, err := safeTarget(root, artifact.RelativePath); err != nil {
			rollback()
			return err
		}
		file, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
		if err != nil {
			rollback()
			if errors.Is(err, fs.ErrExist) {
				return ErrConflict
			}
			return err
		}
		_, writeErr := file.Write(artifact.Content)
		closeErr := file.Close()
		if writeErr != nil || closeErr != nil {
			_ = os.Remove(target)
			rollback()
			if writeErr != nil {
				return writeErr
			}
			return closeErr
		}
		created = append(created, target)
	}
	return nil
}

func remove(root string, changes []Change, artifacts map[string]Artifact) error {
	removed := make([]Artifact, 0, len(changes))
	rollback := func() {
		for _, artifact := range removed {
			target, err := safeTarget(root, artifact.RelativePath)
			if err != nil {
				continue
			}
			_ = os.MkdirAll(filepath.Dir(target), 0o755)
			file, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
			if err != nil {
				continue
			}
			_, _ = file.Write(artifact.Content)
			_ = file.Close()
		}
	}
	for _, change := range changes {
		if change.Action != ActionRemove {
			continue
		}
		artifact := artifacts[filepath.Clean(change.RelativePath)]
		target, err := safeTarget(root, artifact.RelativePath)
		if err != nil {
			rollback()
			return err
		}
		content, err := readExistingTarget(target)
		if err != nil || !bytes.Equal(content, artifact.Content) {
			rollback()
			return ErrConflict
		}
		if err := os.Remove(target); err != nil {
			rollback()
			return err
		}
		removed = append(removed, artifact)
	}
	for _, directory := range []string{
		filepath.Join(root, ".kiro", "hooks"),
		filepath.Join(root, ".kiro", "steering"),
		filepath.Join(root, ".kiro"),
	} {
		_ = os.Remove(directory)
	}
	return nil
}

func readExistingTarget(path string) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("%w: target %s is not a regular file", ErrUnsafe, path)
	}
	return os.ReadFile(path)
}

func inventoryUnrelated(root string, targets map[string]bool) ([]string, error) {
	var preserved []string
	for _, relativeDir := range []string{filepath.Join(".kiro", "steering"), filepath.Join(".kiro", "hooks")} {
		directory, err := safeTarget(root, relativeDir)
		if err != nil {
			return nil, err
		}
		entries, err := os.ReadDir(directory)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, err
		}
		for _, entry := range entries {
			relative := filepath.Join(relativeDir, entry.Name())
			if !targets[filepath.Clean(relative)] {
				preserved = append(preserved, relative)
			}
		}
	}
	sort.Strings(preserved)
	return preserved, nil
}

func safeTarget(root, relative string) (string, error) {
	if relative == "" || filepath.IsAbs(relative) || filepath.VolumeName(relative) != "" {
		return "", ErrUnsafe
	}
	clean := filepath.Clean(relative)
	if clean == "." || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", ErrUnsafe
	}
	target := filepath.Join(root, clean)
	parent := target
	for {
		_, err := os.Lstat(parent)
		if err == nil {
			break
		}
		if !errors.Is(err, fs.ErrNotExist) {
			return "", err
		}
		next := filepath.Dir(parent)
		if next == parent {
			return "", ErrUnsafe
		}
		parent = next
	}
	resolvedParent, err := filepath.EvalSymlinks(parent)
	if err != nil {
		return "", err
	}
	if !withinRoot(root, resolvedParent) {
		return "", fmt.Errorf("%w: target parent escapes the selected root", ErrUnsafe)
	}
	if info, err := os.Lstat(target); err == nil && info.Mode()&os.ModeSymlink != 0 {
		return "", fmt.Errorf("%w: target is a symbolic link", ErrUnsafe)
	} else if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return "", err
	}
	return target, nil
}

func withinRoot(root, candidate string) bool {
	relative, err := filepath.Rel(root, candidate)
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) && !filepath.IsAbs(relative)
}
