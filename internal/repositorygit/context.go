package repositorygit

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"time"
)

// gitTimeout bounds each read-only Git call.
const gitTimeout = 10 * time.Second

var (
	// ErrDetachedHead means HEAD names no branch.
	ErrDetachedHead = errors.New("HEAD is detached")
	// ErrNoDefaultBranch means the default branch is unknown or has no
	// remote-tracking ref.
	ErrNoDefaultBranch = errors.New("the repository default branch is unknown")
	branchPattern      = regexp.MustCompile(`^[A-Za-z0-9._/-]{1,200}$`)
	commitPattern      = regexp.MustCompile(`^[0-9a-f]{40}([0-9a-f]{24})?$`)
)

// ValidBranchName accepts branch names that cannot be read as Git options or
// revision ranges.
func ValidBranchName(name string) bool {
	return branchPattern.MatchString(name) && !strings.HasPrefix(name, "-") && !strings.Contains(name, "..") && !strings.HasSuffix(name, ".lock")
}

// CurrentBranch returns the short name of the checked-out branch, or
// ErrDetachedHead.
func CurrentBranch(ctx context.Context, worktree string) (string, error) {
	output, err := git(ctx, worktree, "symbolic-ref", "--quiet", "--short", "HEAD")
	if err != nil {
		return "", ErrDetachedHead
	}
	branch := strings.TrimSpace(output)
	if !ValidBranchName(branch) {
		return "", fmt.Errorf("the current branch name cannot be represented safely")
	}
	return branch, nil
}

// Head returns the full commit ID of HEAD.
func Head(ctx context.Context, worktree string) (string, error) {
	return commit(ctx, worktree, "rev-parse", "--verify", "--quiet", "HEAD^{commit}")
}

// DefaultBranch returns the configured default branch, or the branch that
// origin/HEAD names.
func DefaultBranch(ctx context.Context, worktree, configured string) (string, error) {
	if configured != "" {
		if !ValidBranchName(configured) {
			return "", ErrNoDefaultBranch
		}
		return configured, nil
	}
	output, err := git(ctx, worktree, "symbolic-ref", "--quiet", "--short", "refs/remotes/origin/HEAD")
	if err != nil {
		return "", ErrNoDefaultBranch
	}
	branch, found := strings.CutPrefix(strings.TrimSpace(output), "origin/")
	if !found || !ValidBranchName(branch) {
		return "", ErrNoDefaultBranch
	}
	return branch, nil
}

// BaseRevision returns the commit an Intent's change is based on: HEAD when
// the current branch is the default branch, otherwise the merge base of HEAD
// and origin/<default>.
func BaseRevision(ctx context.Context, worktree, defaultBranch, currentBranch string) (string, error) {
	if !ValidBranchName(defaultBranch) {
		return "", ErrNoDefaultBranch
	}
	if currentBranch == defaultBranch {
		return Head(ctx, worktree)
	}
	base, err := commit(ctx, worktree, "merge-base", "HEAD", "refs/remotes/origin/"+defaultBranch)
	if err != nil {
		return "", fmt.Errorf("%w: no merge base with origin/%s", ErrNoDefaultBranch, defaultBranch)
	}
	return base, nil
}

// GitDir returns the absolute Git directory of the worktree. Each linked
// worktree has its own directory.
func GitDir(ctx context.Context, worktree string) (string, error) {
	output, err := git(ctx, worktree, "rev-parse", "--absolute-git-dir")
	if err != nil {
		return "", errors.New("the directory is not inside a Git worktree")
	}
	return strings.TrimSpace(output), nil
}

// TopLevel returns the absolute root of the worktree.
func TopLevel(ctx context.Context, worktree string) (string, error) {
	output, err := git(ctx, worktree, "rev-parse", "--show-toplevel")
	if err != nil {
		return "", errors.New("the directory is not inside a Git worktree")
	}
	return strings.TrimSpace(output), nil
}

func commit(ctx context.Context, worktree string, args ...string) (string, error) {
	output, err := git(ctx, worktree, args...)
	if err != nil {
		return "", err
	}
	value := strings.TrimSpace(output)
	if !commitPattern.MatchString(value) {
		return "", errors.New("git returned an unexpected commit identifier")
	}
	return value, nil
}

// git runs one read-only Git command with fixed arguments, no shell, a
// timeout, no terminal prompt, and an environment without Conn or credential
// variables. Its standard error is discarded because it may echo paths.
func git(ctx context.Context, worktree string, args ...string) (string, error) {
	return Output(ctx, worktree, 1<<16, args...)
}

// Output runs one read-only Git command like git, with a caller-chosen bound
// on standard output. Callers pass fixed arguments only.
func Output(ctx context.Context, worktree string, maxBytes int, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, gitTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", worktree}, args...)...)
	cmd.Env = gitEnvironment()
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	if err := cmd.Run(); err != nil {
		return "", err
	}
	if stdout.Len() > maxBytes {
		return "", errors.New("git output exceeds its bound")
	}
	return stdout.String(), nil
}

// Environment returns the minimized environment Conn uses for Git
// subprocesses.
func Environment() []string { return gitEnvironment() }

func gitEnvironment() []string {
	allow := map[string]bool{"PATH": true, "SYSTEMROOT": true, "COMSPEC": true, "TEMP": true, "TMP": true, "HOME": true, "USERPROFILE": true}
	env := []string{"GIT_TERMINAL_PROMPT=0"}
	for _, item := range os.Environ() {
		key, _, _ := strings.Cut(item, "=")
		if allow[strings.ToUpper(key)] {
			env = append(env, item)
		}
	}
	return env
}
