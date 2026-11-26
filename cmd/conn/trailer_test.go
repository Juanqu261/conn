package main

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"uuid"

	"github.com/Juanqu261/conn/internal/hookstate"
	"github.com/Juanqu261/conn/internal/protocol"
	"github.com/Juanqu261/conn/internal/review"
)

// TestCommitTrailerHook protects requirement 6: preview writes nothing;
// installation targets the effective hooks directory, including
// core.hooksPath, and refuses to overwrite a different hook; a commit with a
// focused Intent gets the trailer once; an existing Conn-Intent trailer is
// kept; a commit without a focus still succeeds with one warning; and the
// shared parser reads the latest trailer per Intent from the range.
func TestCommitTrailerHook(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is required")
	}
	ctx := context.Background()
	repo := t.TempDir()
	env := append(os.Environ(), runMainEnv+"=1", "GIT_AUTHOR_NAME=Conn Test", "GIT_AUTHOR_EMAIL=conn@example.invalid",
		"GIT_COMMITTER_NAME=Conn Test", "GIT_COMMITTER_EMAIL=conn@example.invalid")
	run := func(dir string, name string, args ...string) (int, string, string) {
		t.Helper()
		cmd := exec.Command(name, args...)
		cmd.Dir, cmd.Env = dir, env
		var stdout, stderr bytes.Buffer
		cmd.Stdout, cmd.Stderr = &stdout, &stderr
		err := cmd.Run()
		if exit, ok := err.(*exec.ExitError); ok {
			return exit.ExitCode(), stdout.String(), stderr.String()
		} else if err != nil {
			t.Fatal(err)
		}
		return 0, stdout.String(), stderr.String()
	}
	gitOK := func(args ...string) string {
		t.Helper()
		code, stdout, stderr := run(repo, "git", args...)
		if code != 0 {
			t.Fatalf("git %v exited %d: %s", args, code, stderr)
		}
		return strings.TrimSpace(stdout)
	}
	conn := func(args ...string) (int, string, string) {
		t.Helper()
		return run(repo, os.Args[0], args...)
	}
	gitOK("init", "--quiet", "-b", "main")
	gitOK("commit", "--quiet", "--allow-empty", "-m", "base")
	base := gitOK("rev-parse", "HEAD")
	hook := filepath.Join(repo, ".git", "hooks", "prepare-commit-msg")

	store, err := hookstate.Open(ctx, repo)
	if err != nil {
		t.Fatal(err)
	}
	projectID, intentID, other := uuid.NewV7(), uuid.NewV7(), uuid.NewV7()
	if err := store.UpdateFocus(func(focus *hookstate.Focus) {
		focus.ProjectID, focus.IntentID, focus.IntentRevision = &projectID, &intentID, 2
	}); err != nil {
		t.Fatal(err)
	}
	trailer := protocol.CommitTrailer(intentID, 2)

	if code, stdout, _ := conn("git-hook", "preview"); code != 0 || !strings.Contains(stdout, "No files changed.") {
		t.Fatalf("preview exited %d: %s", code, stdout)
	}
	if _, err := os.Stat(hook); !os.IsNotExist(err) {
		t.Fatal("preview wrote the hook")
	}
	if code, stdout, stderr := conn("git-hook", "install"); code != 0 {
		t.Fatalf("install exited %d: %s %s", code, stdout, stderr)
	}

	gitOK("commit", "--quiet", "--allow-empty", "-m", "first")
	if message := gitOK("log", "-1", "--format=%B"); strings.Count(message, "Conn-Intent:") != 1 || !strings.Contains(message, trailer) {
		t.Fatalf("first commit message = %q, want the trailer once", message)
	}
	gitOK("commit", "--quiet", "--allow-empty", "--amend", "--no-edit")
	if message := gitOK("log", "-1", "--format=%B"); strings.Count(message, "Conn-Intent:") != 1 {
		t.Fatalf("amended commit message = %q, want the trailer once", message)
	}
	existing := protocol.CommitTrailer(other, 1)
	gitOK("commit", "--quiet", "--allow-empty", "-m", "second", "-m", existing)
	if message := gitOK("log", "-1", "--format=%B"); strings.Contains(message, trailer) || !strings.Contains(message, existing) {
		t.Fatalf("second commit message = %q, want the existing trailer kept", message)
	}

	if err := store.ClearIntent(intentID); err != nil {
		t.Fatal(err)
	}
	code, _, stderr := run(repo, "git", "commit", "--allow-empty", "-m", "unfocused")
	if code != 0 || strings.Count(stderr, "conn: warning:") != 1 || strings.Contains(gitOK("log", "-1", "--format=%B"), "Conn-Intent:") {
		t.Fatalf("unfocused commit exited %d: %q", code, stderr)
	}

	trailers, err := review.ReadTrailers(ctx, repo, base, "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	if len(trailers.Commits) != 3 || trailers.ByIntent[intentID].LatestRevision != 2 || len(trailers.ByIntent[intentID].Commits) != 1 ||
		trailers.ByIntent[other].LatestRevision != 1 {
		t.Fatalf("trailers = %+v", trailers)
	}

	if code, _, stderr := conn("git-hook", "remove"); code != 0 {
		t.Fatalf("remove exited %d: %s", code, stderr)
	}
	if _, err := os.Stat(hook); !os.IsNotExist(err) {
		t.Fatal("remove left the unchanged Conn hook")
	}
	custom := []byte("#!/bin/sh\necho team hook\n")
	if err := os.WriteFile(hook, custom, 0o755); err != nil {
		t.Fatal(err)
	}
	code, stdout, _ := conn("git-hook", "install")
	if data, _ := os.ReadFile(hook); code == 0 || !bytes.Equal(data, custom) || !strings.Contains(stdout, `trailer "$1" "$2" || true`) {
		t.Fatalf("install over a team hook exited %d and left %q: %s", code, data, stdout)
	}
	if code, _, _ := conn("git-hook", "remove"); code == 0 {
		t.Fatal("remove deleted a team hook")
	}

	gitOK("config", "core.hooksPath", ".githooks")
	if code, stdout, stderr := conn("git-hook", "install"); code != 0 || !strings.Contains(filepath.ToSlash(stdout), ".githooks/prepare-commit-msg") {
		t.Fatalf("install with core.hooksPath exited %d: %s %s", code, stdout, stderr)
	}
	if _, err := os.Stat(filepath.Join(repo, ".githooks", "prepare-commit-msg")); err != nil {
		t.Fatalf("core.hooksPath hook: %v", err)
	}
}
