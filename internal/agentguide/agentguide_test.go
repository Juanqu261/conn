package agentguide

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestAgentsSectionLifecycle protects requirement 9: one marker-delimited
// section is created, appended, kept, and removed without changing content
// outside the markers; a modified section is preserved as a conflict; and
// the section and the Kiro steering come from one source.
func TestAgentsSectionLifecycle(t *testing.T) {
	section, steering := Section(), string(KiroSteering())
	for _, phrase := range []string{"list_concepts", "suggest_claims", "conn evidence run", "conn verify-pair", "Never create CI credentials", "not independent CI proof"} {
		if !strings.Contains(section, phrase) || !strings.Contains(steering, phrase) {
			t.Errorf("guidance omits %q from one rendering", phrase)
		}
	}
	if !strings.Contains(steering, "`/conn-start`") || !strings.Contains(section, "the `conn-start` prompt") || strings.Contains(section, "hooks only warn") {
		t.Fatal("the renderings do not name prompts for their client")
	}

	root := t.TempDir()
	path := filepath.Join(root, DefaultFile)
	apply := func(mode Mode, want Action) Plan {
		t.Helper()
		plan, err := Preview(root, "", mode)
		if err != nil || plan.Action != want {
			t.Fatalf("%s plan = %+v, %v; want %s", mode, plan, err, want)
		}
		if err := Apply(plan); err != nil {
			t.Fatal(err)
		}
		return plan
	}
	apply(ModeInstall, ActionCreateFile)
	apply(ModeInstall, ActionUnchanged)
	apply(ModeRemove, ActionDeleteFile)
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("removing the only content kept the file")
	}

	team := "# Team instructions\n\nUse pnpm.\n"
	if err := os.WriteFile(path, []byte(team), 0o644); err != nil {
		t.Fatal(err)
	}
	apply(ModeInstall, ActionAppend)
	installed, _ := os.ReadFile(path)
	if !strings.HasPrefix(string(installed), team) || !strings.HasSuffix(string(installed), section) {
		t.Fatalf("appended file = %q", installed)
	}
	apply(ModeRemove, ActionRemove)
	if restored, _ := os.ReadFile(path); string(restored) != team {
		t.Fatalf("removal left %q, want the team content", restored)
	}

	apply(ModeInstall, ActionAppend)
	edited := strings.Replace(string(mustRead(t, path)), "Use it for work", "Use it always for work", 1)
	if err := os.WriteFile(path, []byte(edited), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, mode := range []Mode{ModeInstall, ModeRemove} {
		if plan, err := Preview(root, "", mode); err != nil || plan.Action != ActionConflict {
			t.Fatalf("%s over a modified section = %+v, %v", mode, plan, err)
		}
	}
	if string(mustRead(t, path)) != edited {
		t.Fatal("a modified section changed")
	}
	if _, err := Preview(root, "../AGENTS.md", ModeInstall); err == nil {
		t.Fatal("a file outside the root was accepted")
	}
}

func mustRead(t *testing.T, path string) []byte {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return content
}

// TestPowerAudiencesShareTheGuide protects kiro-power requirements 5 and 6:
// the power's skill bodies reuse the steering text, name prompts as Kiro
// slash commands, and leave no template tag except the version placeholder.
func TestPowerAudiencesShareTheGuide(t *testing.T) {
	kiro, _ := Render(AudienceKiro)
	if "---\ninclusion: always\n---\n\n"+kiro != string(KiroSteering()) {
		t.Fatal("the Kiro audience differs from the steering body")
	}
	setup, err := Render(AudiencePowerSetup)
	if err != nil {
		t.Fatal(err)
	}
	review, err := Render(AudiencePowerReview)
	if err != nil {
		t.Fatal(err)
	}
	_, parts := split(kiro)
	for name, want := range map[string][]string{
		setup:  {MinimumVersionPlaceholder, "conn kiro integration preview --scope workspace --root . --command-on-path", "conn git-hook preview", "session_status", "list_projects", "`/conn-start`", parts["Authority"]},
		review: {parts["Finishing"], "gh pr create --body-file", "labelled reported", "`/conn-review`", parts["Authority"]},
	} {
		for _, phrase := range want {
			if !strings.Contains(name, phrase) {
				t.Errorf("a power rendering omits %q", phrase)
			}
		}
		if leftover := strings.ReplaceAll(name, MinimumVersionPlaceholder, ""); strings.Contains(leftover, "{{") {
			t.Errorf("a power rendering keeps a template tag: %q", leftover)
		}
	}
	if _, err := Render("unknown"); err == nil {
		t.Fatal("an unknown audience rendered")
	}
}
