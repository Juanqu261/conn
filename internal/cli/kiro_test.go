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
	executable := filepath.Join(t.TempDir(), "bin", "conn.exe")
	run := func(args ...string) (int, string, string) {
		var stdout, stderr bytes.Buffer
		code := cli.Execute(context.Background(), args, cli.Streams{In: strings.NewReader(""), Out: &stdout, Err: &stderr}, cli.Dependencies{
			Version: "test", LookupEnv: func(string) (string, bool) { return "", false },
			Executable: func() (string, error) { return executable, nil },
		})
		return code, stdout.String(), stderr.String()
	}

	code, stdout, _ := run("kiro", "integration", "preview", "--root", root)
	if code != cli.ExitOK || !strings.Contains(stdout, "create") || !strings.Contains(stdout, "--- proposed .kiro/steering/conn-work-loop.md ---") ||
		!strings.Contains(stdout, "SessionStart") || !strings.Contains(stdout, "hook pre-write") || !strings.Contains(stdout, "No files changed") {
		t.Fatalf("preview = code %d\nstdout: %s", code, stdout)
	}
	if _, err := os.Stat(filepath.Join(root, ".kiro")); !os.IsNotExist(err) {
		t.Fatal("preview created Kiro configuration")
	}

	code, stdout, stderr := run("kiro", "integration", "install", "--root", root, "--command-on-path")
	if code != cli.ExitOK || !strings.Contains(stdout, "create") || !strings.Contains(stdout, "Installed") || !strings.Contains(stdout, "hook command: conn") {
		t.Fatalf("install = code %d\nstdout: %s\nstderr: %s", code, stdout, stderr)
	}
	hooksPath := filepath.Join(root, ".kiro", "hooks", "conn-hooks.json")
	for _, path := range []string{filepath.Join(root, ".kiro", "steering", "conn-work-loop.md"), hooksPath} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("installed file %s: %v", path, err)
		}
	}
	if content, _ := os.ReadFile(hooksPath); !strings.Contains(string(content), `"conn hook prompt-submit"`) {
		t.Fatalf("PATH-based hooks = %s", content)
	}

	code, stdout, stderr = run("kiro", "integration", "install", "--root", root, "--command-on-path")
	if code != cli.ExitOK || !strings.Contains(stdout, "unchanged") {
		t.Fatalf("idempotent install = code %d\nstdout: %s\nstderr: %s", code, stdout, stderr)
	}

	if err := os.WriteFile(hooksPath, []byte("user content\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	code, stdout, stderr = run("kiro", "integration", "remove", "--root", root)
	if code != cli.ExitFailure || !strings.Contains(stdout, "conflict") || !strings.Contains(stderr, "preserved for manual review") {
		t.Fatalf("conflicting remove = code %d\nstdout: %s\nstderr: %s", code, stdout, stderr)
	}
	content, err := os.ReadFile(hooksPath)
	if err != nil || string(content) != "user content\n" {
		t.Fatalf("conflicting remove changed target: %q, %v", content, err)
	}
}
