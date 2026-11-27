package ciaddon

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestWorkflowCheckedOutWithCRLFIsConnContent reproduces a defect: Git for
// Windows checks a committed workflow out with CRLF line endings
// (core.autocrlf=true), and the installer then called its own workflow a
// different file, refused to reinstall it, and refused to remove it.
func TestWorkflowCheckedOutWithCRLFIsConnContent(t *testing.T) {
	root := t.TempDir()
	options := Options{ConnRef: testRef, Comment: true}
	content, err := Render(options)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, TargetPath)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(strings.ReplaceAll(string(content), "\n", "\r\n")), 0o644); err != nil {
		t.Fatal(err)
	}
	if plan, err := Preview(root, ModeInstall, options); err != nil || plan.Action != ActionUnchanged {
		t.Fatalf("install plan = %s, %v; want unchanged", plan.Action, err)
	}
	plan, err := Preview(root, ModeRemove, Options{})
	if err != nil || plan.Action != ActionRemove || plan.Options != options {
		t.Fatalf("remove plan = %s %+v, %v; want remove with the installed options", plan.Action, plan.Options, err)
	}
	if err := Apply(plan); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("removal kept the CRLF workflow")
	}
}
