package agentguide

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestSectionCheckedOutWithCRLFIsConnContent reproduces a defect: Git for
// Windows checks AGENTS.md out with CRLF line endings (core.autocrlf=true),
// and the installer then reported its unchanged section as modified and
// refused to remove it. A CRLF file also keeps CRLF when Conn edits it, so
// no content outside the markers changes.
func TestSectionCheckedOutWithCRLFIsConnContent(t *testing.T) {
	crlf := func(text string) string { return strings.ReplaceAll(text, "\n", "\r\n") }
	root := t.TempDir()
	path := filepath.Join(root, DefaultFile)
	team := crlf("# Team instructions\n\nUse pnpm.\n")
	if err := os.WriteFile(path, []byte(team), 0o644); err != nil {
		t.Fatal(err)
	}
	plan, err := Preview(root, "", ModeInstall)
	if err != nil || plan.Action != ActionAppend {
		t.Fatalf("append plan = %s, %v", plan.Action, err)
	}
	if err := Apply(plan); err != nil {
		t.Fatal(err)
	}
	installed, _ := os.ReadFile(path)
	if string(installed) != team+"\r\n"+crlf(Section()) {
		t.Fatalf("a section appended to a CRLF file mixed line endings: %q", installed)
	}

	if plan, err := Preview(root, "", ModeInstall); err != nil || plan.Action != ActionUnchanged {
		t.Fatalf("install plan for the CRLF section = %s %s, %v; want unchanged", plan.Action, plan.Reason, err)
	}
	plan, err = Preview(root, "", ModeRemove)
	if err != nil || plan.Action != ActionRemove {
		t.Fatalf("remove plan for the CRLF section = %s %s, %v; want remove", plan.Action, plan.Reason, err)
	}
	if err := Apply(plan); err != nil {
		t.Fatal(err)
	}
	if restored, _ := os.ReadFile(path); string(restored) != team {
		t.Fatalf("removal left %q, want the team content byte for byte", restored)
	}
}
