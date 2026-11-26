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

func TestBundleTemplatesAreAdvisoryAndInspectable(t *testing.T) {
	artifacts, err := Artifacts()
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
	for _, phrase := range []string{"inclusion: always", "/conn-start", "/conn-sync", "/conn-finish", "Do not start background polling", "not Charter acceptance"} {
		if !strings.Contains(steering, phrase) {
			t.Errorf("steering omitted %q", phrase)
		}
	}

	var hookDocument struct {
		Version string `json:"version"`
		Hooks   []struct {
			Name    string `json:"name"`
			Trigger string `json:"trigger"`
			Enabled bool   `json:"enabled"`
			Action  struct {
				Type   string `json:"type"`
				Prompt string `json:"prompt"`
			} `json:"action"`
		} `json:"hooks"`
	}
	hooks := contents[".kiro/hooks/conn-lifecycle.json"]
	if err := json.Unmarshal([]byte(hooks), &hookDocument); err != nil {
		t.Fatal(err)
	}
	if hookDocument.Version != "v1" || len(hookDocument.Hooks) != 2 {
		t.Fatalf("hook document = %+v", hookDocument)
	}
	triggers := []string{hookDocument.Hooks[0].Trigger, hookDocument.Hooks[1].Trigger}
	slices.Sort(triggers)
	if !slices.Equal(triggers, []string{"SessionStart", "Stop"}) {
		t.Fatalf("hook triggers = %v", triggers)
	}
	for _, hook := range hookDocument.Hooks {
		if hook.Name == "" || !hook.Enabled || hook.Action.Type != "agent" || !strings.Contains(hook.Action.Prompt, "Do not") {
			t.Errorf("unsafe or incomplete hook = %+v", hook)
		}
		for _, forbidden := range []string{"employee score", "background timer", "type\": \"command"} {
			if strings.Contains(hook.Action.Prompt, forbidden) {
				t.Errorf("hook %s contains forbidden behavior %q", hook.Name, forbidden)
			}
		}
	}
	if !strings.Contains(hookDocument.Hooks[0].Action.Prompt, "session_status") || !strings.Contains(hookDocument.Hooks[0].Action.Prompt, "read_work_context") {
		t.Error("SessionStart hook omitted read-only Conn context checks")
	}
	if !strings.Contains(hookDocument.Hooks[1].Action.Prompt, "Offer /conn-finish") || !strings.Contains(hookDocument.Hooks[1].Action.Prompt, "do not invoke it automatically") {
		t.Error("Stop hook omitted explicit, non-automatic finish guidance")
	}
}

func TestBundleInstallAndRemovePreserveUnrelatedConfiguration(t *testing.T) {
	root := t.TempDir()
	unrelatedSteering := filepath.Join(root, ".kiro", "steering", "team.md")
	unrelatedHook := filepath.Join(root, ".kiro", "hooks", "team.json")
	if err := os.MkdirAll(filepath.Dir(unrelatedSteering), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(unrelatedHook), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(unrelatedSteering, []byte("team steering\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(unrelatedHook, []byte(`{"version":"v1","hooks":[]}`), 0o600); err != nil {
		t.Fatal(err)
	}

	preview, err := Preview(ScopeWorkspace, root, ModeInstall)
	if err != nil {
		t.Fatal(err)
	}
	if preview.HasConflicts() || len(preview.Changes) != 2 || preview.Changes[0].Action != ActionCreate || preview.Changes[1].Action != ActionCreate {
		t.Fatalf("install preview = %+v", preview)
	}
	preserved := make([]string, len(preview.Preserved))
	for i, path := range preview.Preserved {
		preserved[i] = filepath.ToSlash(path)
	}
	if !slices.Equal(preserved, []string{".kiro/hooks/team.json", ".kiro/steering/team.md"}) {
		t.Fatalf("preserved files = %v", preserved)
	}
	if _, err := os.Stat(filepath.Join(root, ".kiro", "steering", "conn-work-loop.md")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("preview changed the workspace")
	}
	if err := Apply(preview); err != nil {
		t.Fatal(err)
	}
	artifacts, err := Artifacts()
	if err != nil {
		t.Fatal(err)
	}
	for _, artifact := range artifacts {
		content, err := os.ReadFile(filepath.Join(root, artifact.RelativePath))
		if err != nil || string(content) != string(artifact.Content) {
			t.Fatalf("installed %s differs: %v", artifact.RelativePath, err)
		}
	}
	if _, err := os.Stat(filepath.Join(root, ".kiro", "settings", "mcp.json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("installation created MCP configuration")
	}

	steeringTarget := filepath.Join(root, ".kiro", "steering", "conn-work-loop.md")
	modified := []byte("user-owned replacement\n")
	if err := os.WriteFile(steeringTarget, modified, 0o600); err != nil {
		t.Fatal(err)
	}
	conflict, err := Preview(ScopeWorkspace, root, ModeInstall)
	if err != nil || !conflict.HasConflicts() {
		t.Fatalf("modified install preview = %+v, %v", conflict, err)
	}
	if err := Apply(conflict); !errors.Is(err, ErrConflict) {
		t.Fatalf("conflicting install = %v, want conflict", err)
	}
	if content, _ := os.ReadFile(steeringTarget); string(content) != string(modified) {
		t.Fatal("conflicting install overwrote the modified target")
	}
	removeConflict, err := Preview(ScopeWorkspace, root, ModeRemove)
	if err != nil || !removeConflict.HasConflicts() {
		t.Fatalf("modified removal preview = %+v, %v", removeConflict, err)
	}
	if err := Apply(removeConflict); !errors.Is(err, ErrConflict) {
		t.Fatalf("conflicting removal = %v, want conflict", err)
	}

	for _, artifact := range artifacts {
		if err := os.WriteFile(filepath.Join(root, artifact.RelativePath), artifact.Content, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	removal, err := Preview(ScopeWorkspace, root, ModeRemove)
	if err != nil || removal.HasConflicts() {
		t.Fatalf("removal preview = %+v, %v", removal, err)
	}
	if err := Apply(removal); err != nil {
		t.Fatal(err)
	}
	for _, artifact := range artifacts {
		if _, err := os.Stat(filepath.Join(root, artifact.RelativePath)); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("Conn artifact %s remains after removal", artifact.RelativePath)
		}
	}
	for _, path := range []string{unrelatedSteering, unrelatedHook} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("unrelated configuration %s was removed: %v", path, err)
		}
	}
}
