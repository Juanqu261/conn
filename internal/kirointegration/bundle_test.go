package kirointegration

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// The hook command contains a space and a backslash so rendering must quote
// it for the shell and escape it for JSON.
const testCommand = `"C:\Program Files\conn\conn.exe"`

func TestBundleTemplatesAreWarnOnlyCommandHooks(t *testing.T) {
	artifacts, err := Artifacts(testCommand)
	if err != nil {
		t.Fatal(err)
	}
	if len(artifacts) != 2 {
		t.Fatalf("artifacts = %d, want 2", len(artifacts))
	}
	contents := make(map[string]string, len(artifacts))
	for _, artifact := range artifacts {
		contents[filepath.ToSlash(artifact.RelativePath)] = string(artifact.Content)
	}
	steering := contents[".kiro/steering/conn-work-loop.md"]
	for _, phrase := range []string{"inclusion: always", "/conn-start", "list_concepts", "suggest_claims", "/conn-sync", "/conn-finish", "Do not start background polling", "only warn", "optional planning context", "not Charter acceptance"} {
		if !strings.Contains(steering, phrase) {
			t.Errorf("steering omitted %q", phrase)
		}
	}

	var document struct {
		Version string `json:"version"`
		Hooks   []struct {
			Name    string `json:"name"`
			Trigger string `json:"trigger"`
			Matcher string `json:"matcher"`
			Enabled bool   `json:"enabled"`
			Timeout int    `json:"timeout"`
			Action  struct {
				Type    string `json:"type"`
				Command string `json:"command"`
				Prompt  string `json:"prompt"`
			} `json:"action"`
		} `json:"hooks"`
	}
	hooks := contents[".kiro/hooks/conn-hooks.json"]
	if err := json.Unmarshal([]byte(hooks), &document); err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"SessionStart": "session-start", "UserPromptSubmit": "prompt-submit", "PreToolUse": "pre-write", "PostToolUse": "post-write"}
	if document.Version != "v1" || len(document.Hooks) != len(want) {
		t.Fatalf("hook document = %+v", document)
	}
	for _, hook := range document.Hooks {
		name, ok := want[hook.Trigger]
		if !ok || hook.Action.Type != "command" || hook.Action.Prompt != "" || !hook.Enabled || hook.Timeout <= 0 {
			t.Errorf("hook %s is not an enabled command hook: %+v", hook.Name, hook)
		}
		if hook.Action.Command != testCommand+" hook "+name {
			t.Errorf("hook %s command = %q", hook.Name, hook.Action.Command)
		}
		if (hook.Trigger == "PreToolUse" || hook.Trigger == "PostToolUse") != (hook.Matcher == "write") {
			t.Errorf("hook %s matcher = %q", hook.Name, hook.Matcher)
		}
		if strings.Contains(hook.Action.Command, "--warn-exit-code 2") {
			t.Errorf("hook %s could block a tool call", hook.Name)
		}
	}
	if strings.Contains(hooks, CommandPlaceholder) {
		t.Error("rendered hooks still contain the command placeholder")
	}
	for _, path := range []string{`C:\Users\dev\conn.exe`, `/opt/conn/bin/conn`} {
		if RenderCommand(path) != path {
			t.Errorf("RenderCommand(%q) quoted a path without spaces", path)
		}
	}
	if RenderCommand(`C:\Program Files\conn\conn.exe`) != testCommand {
		t.Error("RenderCommand did not quote a path with spaces")
	}
}

func TestBundleInstallMigrationAndRemovalPreserveUnrelatedConfiguration(t *testing.T) {
	root := t.TempDir()
	write := func(relative, content string) string {
		t.Helper()
		path := filepath.Join(root, relative)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		return path
	}
	unrelatedSteering := write(".kiro/steering/team.md", "team steering\n")
	unrelatedHook := write(".kiro/hooks/team.json", `{"version":"v1","hooks":[]}`)
	legacy, err := LegacyArtifact()
	if err != nil {
		t.Fatal(err)
	}
	legacyPath := write(filepath.ToSlash(legacy.RelativePath), string(legacy.Content))

	// An unchanged version 1 file is listed for removal, and preview writes nothing.
	preview, err := Preview(ScopeWorkspace, root, ModeInstall, testCommand)
	if err != nil {
		t.Fatal(err)
	}
	actions := map[string]Action{}
	for _, change := range preview.Changes {
		actions[filepath.ToSlash(change.RelativePath)] = change.Action
	}
	if preview.HasConflicts() || actions[".kiro/steering/conn-work-loop.md"] != ActionCreate ||
		actions[".kiro/hooks/conn-hooks.json"] != ActionCreate || actions[".kiro/hooks/conn-lifecycle.json"] != ActionRemove {
		t.Fatalf("install preview = %+v", preview)
	}
	preserved := make([]string, len(preview.Preserved))
	for i, path := range preview.Preserved {
		preserved[i] = filepath.ToSlash(path)
	}
	if !slices.Equal(preserved, []string{".kiro/hooks/team.json", ".kiro/steering/team.md"}) {
		t.Fatalf("preserved files = %v", preserved)
	}
	if _, err := os.Stat(filepath.Join(root, ".kiro", "hooks", "conn-hooks.json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("preview changed the workspace")
	}
	if _, err := os.Stat(legacyPath); err != nil {
		t.Fatal("preview removed the version 1 file")
	}

	if err := Apply(preview); err != nil {
		t.Fatal(err)
	}
	for _, artifact := range preview.Artifacts {
		content, err := os.ReadFile(filepath.Join(root, artifact.RelativePath))
		if err != nil || string(content) != string(artifact.Content) {
			t.Fatalf("installed %s differs: %v", artifact.RelativePath, err)
		}
	}
	if _, err := os.Stat(legacyPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("installation kept the unchanged version 1 hook file")
	}
	if _, err := os.Stat(filepath.Join(root, ".kiro", "settings", "mcp.json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("installation created MCP configuration")
	}

	// A modified version 1 file is preserved and stops installation.
	modifiedLegacy := strings.Replace(string(legacy.Content), "Conn session-start reminder.", "Team reminder.", 1)
	write(filepath.ToSlash(legacy.RelativePath), modifiedLegacy)
	conflict, err := Preview(ScopeWorkspace, root, ModeInstall, testCommand)
	if err != nil || !conflict.HasConflicts() {
		t.Fatalf("modified version 1 preview = %+v, %v", conflict, err)
	}
	if err := Apply(conflict); !errors.Is(err, ErrConflict) {
		t.Fatalf("install over modified version 1 = %v, want conflict", err)
	}
	if content, _ := os.ReadFile(legacyPath); string(content) != modifiedLegacy {
		t.Fatal("a modified version 1 hook file was changed")
	}
	if err := os.Remove(legacyPath); err != nil {
		t.Fatal(err)
	}

	// Installing with a different command does not overwrite the hooks.
	if other, err := Preview(ScopeWorkspace, root, ModeInstall, PathCommand); err != nil || !other.HasConflicts() {
		t.Fatalf("install with another command = %+v, %v", other, err)
	}

	// Removal recognizes the installed command and removes only Conn files.
	removal, err := Preview(ScopeWorkspace, root, ModeRemove, "")
	if err != nil || removal.HasConflicts() || removal.Command != testCommand {
		t.Fatalf("removal preview = %+v, %v", removal, err)
	}
	if err := Apply(removal); err != nil {
		t.Fatal(err)
	}
	for _, artifact := range preview.Artifacts {
		if _, err := os.Stat(filepath.Join(root, artifact.RelativePath)); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("Conn artifact %s remains after removal", artifact.RelativePath)
		}
	}
	for _, path := range []string{unrelatedSteering, unrelatedHook} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("unrelated configuration %s was removed: %v", path, err)
		}
	}

	// A modified hook file is preserved on removal.
	if err := Apply(mustPreview(t, root, ModeInstall, testCommand)); err != nil {
		t.Fatal(err)
	}
	hooksPath := filepath.Join(root, ".kiro", "hooks", "conn-hooks.json")
	if err := os.WriteFile(hooksPath, []byte("user content\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := Apply(mustPreview(t, root, ModeRemove, "")); !errors.Is(err, ErrConflict) {
		t.Fatalf("removal of a modified hook file = %v, want conflict", err)
	}
	if content, _ := os.ReadFile(hooksPath); string(content) != "user content\n" {
		t.Fatal("removal changed a modified hook file")
	}
}

func mustPreview(t *testing.T, root string, mode Mode, command string) Plan {
	t.Helper()
	plan, err := Preview(ScopeWorkspace, root, mode, command)
	if err != nil {
		t.Fatal(err)
	}
	return plan
}
