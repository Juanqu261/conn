package kirointegration

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestBundleCheckedOutWithCRLFIsConnContent reproduces a defect: Git for
// Windows checks committed Kiro files out with CRLF line endings
// (core.autocrlf=true), and the installer then reported its own unchanged
// files as conflicts and refused to remove them.
func TestBundleCheckedOutWithCRLFIsConnContent(t *testing.T) {
	root := t.TempDir()
	install, err := Preview(ScopeWorkspace, root, ModeInstall, testCommand)
	if err != nil || install.HasConflicts() {
		t.Fatalf("install preview = %+v, %v", install, err)
	}
	if err := Apply(install); err != nil {
		t.Fatal(err)
	}
	for _, artifact := range install.Artifacts {
		path := filepath.Join(root, artifact.RelativePath)
		if err := os.WriteFile(path, []byte(strings.ReplaceAll(string(artifact.Content), "\n", "\r\n")), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	again, err := Preview(ScopeWorkspace, root, ModeInstall, testCommand)
	if err != nil {
		t.Fatal(err)
	}
	for _, change := range again.Changes {
		if change.Action != ActionUnchanged {
			t.Fatalf("reinstall over CRLF files plans %s for %s", change.Action, filepath.ToSlash(change.RelativePath))
		}
	}
	remove, err := Preview(ScopeWorkspace, root, ModeRemove, testCommand)
	if err != nil || remove.HasConflicts() {
		t.Fatalf("remove preview over CRLF files = %+v, %v", remove.Changes, err)
	}
	if err := Apply(remove); err != nil {
		t.Fatal(err)
	}
	for _, artifact := range install.Artifacts {
		if _, err := os.Stat(filepath.Join(root, artifact.RelativePath)); !os.IsNotExist(err) {
			t.Fatalf("removal kept the CRLF file %s", filepath.ToSlash(artifact.RelativePath))
		}
	}
}
