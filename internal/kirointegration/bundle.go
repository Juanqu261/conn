// Package kirointegration manages Conn's optional, inspectable Kiro guidance
// bundle. It never edits MCP settings, agents, skills, or authoritative state.
//
// Bundle version 2 contains warn-only command hooks and steering. The hook
// file renders the conn command into each hook. Removal recognizes Conn
// content by re-rendering the template with the command found in the
// installed file. An unchanged version 1 agent-prompt hook file is migrated
// away; a modified one is preserved for manual review.
package kirointegration

import (
	"bytes"
	"embed"
	"encoding/json"
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

// CommandPlaceholder marks where the rendered conn command goes.
const CommandPlaceholder = "{{CONN_COMMAND}}"

// PathCommand runs conn from PATH.
const PathCommand = "conn"

const (
	steeringTemplate = "templates/steering/conn-work-loop.md"
	hooksTemplate    = "templates/hooks/conn-hooks.json"
	legacyTemplate   = "templates/legacy/conn-lifecycle.v1.json"
)

var (
	steeringTarget = filepath.Join(".kiro", "steering", "conn-work-loop.md")
	hooksTarget    = filepath.Join(".kiro", "hooks", "conn-hooks.json")
	// LegacyHooksTarget is the version 1 agent-prompt hook file.
	LegacyHooksTarget = filepath.Join(".kiro", "hooks", "conn-lifecycle.json")
)

//go:embed templates/steering/conn-work-loop.md templates/hooks/conn-hooks.json templates/legacy/conn-lifecycle.v1.json
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
	Command   string
	Artifacts []Artifact
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

// Content returns the rendered artifact content for a planned path.
func (p Plan) Content(relativePath string) []byte {
	for _, artifact := range p.Artifacts {
		if filepath.Clean(artifact.RelativePath) == filepath.Clean(relativePath) {
			return artifact.Content
		}
	}
	return nil
}

// RenderCommand returns the shell command that runs the executable. A path
// with whitespace or shell metacharacters is double-quoted.
func RenderCommand(executable string) string {
	if executable == "" {
		return PathCommand
	}
	if strings.ContainsAny(executable, " \t&|<>^\"'();%!$`*?[]{}#~=,") {
		return `"` + executable + `"`
	}
	return executable
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

// Artifacts returns the bundle rendered with command. Callers cannot alter
// the embedded source through the returned bytes.
func Artifacts(command string) ([]Artifact, error) {
	if command == "" || strings.ContainsAny(command, "\r\n\x00") {
		return nil, errors.New("the hook command must be one non-empty line")
	}
	steering, err := fs.ReadFile(templateFiles, steeringTemplate)
	if err != nil {
		return nil, err
	}
	hooks, err := renderHooks(command)
	if err != nil {
		return nil, err
	}
	return []Artifact{
		{RelativePath: steeringTarget, Content: append([]byte(nil), steering...)},
		{RelativePath: hooksTarget, Content: hooks},
	}, nil
}

// LegacyArtifact returns the exact version 1 hook file.
func LegacyArtifact() (Artifact, error) {
	content, err := fs.ReadFile(templateFiles, legacyTemplate)
	if err != nil {
		return Artifact{}, err
	}
	return Artifact{RelativePath: LegacyHooksTarget, Content: append([]byte(nil), content...)}, nil
}

func renderHooks(command string) ([]byte, error) {
	template, err := fs.ReadFile(templateFiles, hooksTemplate)
	if err != nil {
		return nil, err
	}
	// The placeholder sits inside a JSON string, so the command is escaped
	// as JSON string content.
	quoted, err := json.Marshal(command)
	if err != nil {
		return nil, err
	}
	escaped := string(quoted[1 : len(quoted)-1])
	return []byte(strings.ReplaceAll(string(template), CommandPlaceholder, escaped)), nil
}

// installedCommand reads the conn command from an installed version 2 hook
// file, or reports false when the file is not a recognizable Conn file.
func installedCommand(content []byte) (string, bool) {
	var document struct {
		Hooks []struct {
			Name   string `json:"name"`
			Action struct {
				Command string `json:"command"`
			} `json:"action"`
		} `json:"hooks"`
	}
	if json.Unmarshal(content, &document) != nil {
		return "", false
	}
	for _, hook := range document.Hooks {
		if hook.Name == "conn-session-start" {
			command, found := strings.CutSuffix(hook.Action.Command, " hook session-start")
			return command, found && command != ""
		}
	}
	return "", false
}

// Preview reads the exact target files and inventories unrelated steering and
// hook entries. It never creates a directory or writes a file. In remove
// mode the command is detected from the installed hook file.
func Preview(scope Scope, root string, mode Mode, command string) (Plan, error) {
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
	if mode == ModeRemove {
		command = PathCommand
		if target, err := safeTarget(resolvedRoot, hooksTarget); err == nil {
			if content, err := readExistingTarget(target); err == nil {
				if detected, ok := installedCommand(content); ok {
					command = detected
				}
			}
		}
	}
	artifacts, err := Artifacts(command)
	if err != nil {
		return Plan{}, err
	}
	legacy, err := LegacyArtifact()
	if err != nil {
		return Plan{}, err
	}
	plan := Plan{Scope: scope, Root: resolvedRoot, Mode: mode, Command: command, Artifacts: artifacts, Changes: make([]Change, 0, len(artifacts)+1)}
	targets := map[string]bool{filepath.Clean(legacy.RelativePath): true}
	for _, artifact := range artifacts {
		targets[filepath.Clean(artifact.RelativePath)] = true
		content, err := targetContent(resolvedRoot, artifact.RelativePath)
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
	// Both installation and removal retire an unchanged version 1 file. A
	// modified one is preserved and reported as a conflict for review.
	switch content, err := targetContent(resolvedRoot, legacy.RelativePath); {
	case errors.Is(err, fs.ErrNotExist):
	case err != nil:
		return Plan{}, err
	case bytes.Equal(content, legacy.Content):
		plan.Changes = append(plan.Changes, Change{RelativePath: legacy.RelativePath, Action: ActionRemove})
	default:
		plan.Changes = append(plan.Changes, Change{RelativePath: legacy.RelativePath, Action: ActionConflict})
	}
	plan.Preserved, err = inventoryUnrelated(resolvedRoot, targets)
	if err != nil {
		return Plan{}, err
	}
	return plan, nil
}

func targetContent(root, relative string) ([]byte, error) {
	target, err := safeTarget(root, relative)
	if err != nil {
		return nil, err
	}
	return readExistingTarget(target)
}

// Apply re-reads every target before changing anything. Modified target files
// cause the whole operation to stop. Installation uses exclusive creation and
// removal deletes only byte-for-byte Conn artifacts.
func Apply(plan Plan) error {
	current, err := Preview(plan.Scope, plan.Root, plan.Mode, plan.Command)
	if err != nil {
		return err
	}
	if current.HasConflicts() {
		return ErrConflict
	}
	legacy, err := LegacyArtifact()
	if err != nil {
		return err
	}
	byPath := make(map[string]Artifact, len(current.Artifacts)+1)
	for _, artifact := range current.Artifacts {
		byPath[filepath.Clean(artifact.RelativePath)] = artifact
	}
	byPath[filepath.Clean(legacy.RelativePath)] = legacy
	if plan.Mode == ModeInstall {
		created, err := install(current.Root, current.Changes, byPath)
		if err != nil {
			return err
		}
		if err := remove(current.Root, current.Changes, byPath, false); err != nil {
			for i := len(created) - 1; i >= 0; i-- {
				_ = os.Remove(created[i])
			}
			return err
		}
		return nil
	}
	return remove(current.Root, current.Changes, byPath, true)
}

func install(root string, changes []Change, artifacts map[string]Artifact) ([]string, error) {
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
			return nil, err
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			rollback()
			return nil, err
		}
		if _, err := safeTarget(root, artifact.RelativePath); err != nil {
			rollback()
			return nil, err
		}
		file, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
		if err != nil {
			rollback()
			if errors.Is(err, fs.ErrExist) {
				return nil, ErrConflict
			}
			return nil, err
		}
		_, writeErr := file.Write(artifact.Content)
		closeErr := file.Close()
		if writeErr != nil || closeErr != nil {
			_ = os.Remove(target)
			rollback()
			if writeErr != nil {
				return nil, writeErr
			}
			return nil, closeErr
		}
		created = append(created, target)
	}
	return created, nil
}

func remove(root string, changes []Change, artifacts map[string]Artifact, pruneDirectories bool) error {
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
	if !pruneDirectories {
		return nil
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
