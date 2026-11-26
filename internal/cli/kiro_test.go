package cli_test

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Juanqu261/conn/internal/cli"
)

func TestKiroIntegrationCommandsPreviewBeforeWriting(t *testing.T) {
	root := t.TempDir()
	run := func(args ...string) (int, string, string) {
		var stdout, stderr bytes.Buffer
		code := cli.Execute(context.Background(), args, cli.Streams{In: strings.NewReader(""), Out: &stdout, Err: &stderr}, cli.Dependencies{
			Version: "test", LookupEnv: func(string) (string, bool) { return "", false },
		})
		return code, stdout.String(), stderr.String()
	}

	code, stdout, stderr := run("kiro", "integration", "preview", "--root", root)
	if code != cli.ExitOK || stderr != "" || !strings.Contains(stdout, "create") || !strings.Contains(stdout, "--- proposed .kiro/steering/conn-work-loop.md ---") ||
		!strings.Contains(stdout, "SessionStart") || !strings.Contains(stdout, "No files changed") {
		t.Fatalf("preview = code %d\nstdout: %s\nstderr: %s", code, stdout, stderr)
	}
	if _, err := os.Stat(filepath.Join(root, ".kiro")); !os.IsNotExist(err) {
		t.Fatal("preview created Kiro configuration")
	}

	code, stdout, stderr = run("kiro", "integration", "install", "--root", root)
	if code != cli.ExitOK || stderr != "" || !strings.Contains(stdout, "create") || !strings.Contains(stdout, "Installed") {
		t.Fatalf("install = code %d\nstdout: %s\nstderr: %s", code, stdout, stderr)
	}
	for _, relative := range []string{filepath.Join(".kiro", "steering", "conn-work-loop.md"), filepath.Join(".kiro", "hooks", "conn-lifecycle.json")} {
		if _, err := os.Stat(filepath.Join(root, relative)); err != nil {
			t.Fatalf("installed file %s: %v", relative, err)
		}
	}

	code, stdout, stderr = run("kiro", "integration", "install", "--root", root)
	if code != cli.ExitOK || stderr != "" || !strings.Contains(stdout, "unchanged") {
		t.Fatalf("idempotent install = code %d\nstdout: %s\nstderr: %s", code, stdout, stderr)
	}

	target := filepath.Join(root, ".kiro", "hooks", "conn-lifecycle.json")
	if err := os.WriteFile(target, []byte("user content\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	code, stdout, stderr = run("kiro", "integration", "remove", "--root", root)
	if code != cli.ExitFailure || !strings.Contains(stdout, "conflict") || !strings.Contains(stderr, "preserved for manual review") {
		t.Fatalf("conflicting remove = code %d\nstdout: %s\nstderr: %s", code, stdout, stderr)
	}
	content, err := os.ReadFile(target)
	if err != nil || string(content) != "user content\n" {
		t.Fatalf("conflicting remove changed target: %q, %v", content, err)
	}
}
