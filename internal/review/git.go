package review

import (
	"context"
	"errors"
	"sort"
	"strings"

	"github.com/Juanqu261/conn/internal/repositorygit"
)

// ChecksConfigPath is the repository-relative check configuration file.
const ChecksConfigPath = ".conn/checks.json"

const (
	maxGitListing   = 8 << 20
	maxChangedFiles = 10_000
)

var errUnsafeRevision = errors.New("the revision cannot be passed to Git safely")

// GitFacts are the local, read-only Git facts a packet reports. Nothing in
// it is source content.
type GitFacts struct {
	Available bool
	Branch    string
	Base      string
	Head      string
	Dirty     bool
	// ChangedFiles are paths changed between Base and the working tree,
	// including untracked files.
	ChangedFiles []string
	Trailers     *TrailerLog
	Notes        []string
}

// ReadGit collects the branch, the resolved base, HEAD, the dirty state, the
// changed files, and the commit trailers of base..HEAD. base may be a commit
// ID or a safe branch name; an empty base leaves the changed files unknown.
// Failures become notes, never errors.
func ReadGit(ctx context.Context, worktree, base string) GitFacts {
	facts := GitFacts{}
	head, err := repositorygit.Head(ctx, worktree)
	if err != nil {
		facts.Notes = append(facts.Notes, "Local Git facts are unavailable: the worktree has no readable HEAD commit.")
		return facts
	}
	facts.Available, facts.Head = true, head
	if branch, err := repositorygit.CurrentBranch(ctx, worktree); err == nil {
		facts.Branch = branch
	}
	status, err := repositorygit.Output(ctx, worktree, maxGitListing, "status", "--porcelain", "--untracked-files=normal")
	switch {
	case err != nil:
		facts.Dirty = true
		facts.Notes = append(facts.Notes, "Git could not report the worktree status; it is treated as having uncommitted changes.")
	default:
		facts.Dirty = strings.TrimSpace(status) != ""
	}
	if base == "" {
		facts.Notes = append(facts.Notes, "The Intent records no base revision, so changed files and commit trailers are unknown.")
		return facts
	}
	if !safeRevision(base) || strings.HasPrefix(base, "-") {
		facts.Notes = append(facts.Notes, "The base revision cannot be passed to Git safely, so changed files are unknown.")
		return facts
	}
	resolved, err := repositorygit.Output(ctx, worktree, 1<<10, "rev-parse", "--verify", "--quiet", base+"^{commit}")
	if err != nil || !revisionRef.MatchString(strings.TrimSpace(resolved)) {
		facts.Notes = append(facts.Notes, "The base revision is not available in this worktree, so changed files and commit trailers are unknown.")
		return facts
	}
	facts.Base = strings.TrimSpace(resolved)
	changed, err := changedFiles(ctx, worktree, facts.Base)
	if err != nil {
		facts.Notes = append(facts.Notes, "Git could not list the changed files: "+err.Error()+".")
	} else {
		facts.ChangedFiles = changed
	}
	if trailers, err := ReadTrailers(ctx, worktree, facts.Base, "HEAD"); err == nil {
		facts.Trailers = &trailers
	} else {
		facts.Notes = append(facts.Notes, "Git could not read the commit trailers on the branch.")
	}
	return facts
}

func changedFiles(ctx context.Context, worktree, base string) ([]string, error) {
	tracked, err := repositorygit.Output(ctx, worktree, maxGitListing, "diff", "--name-only", "-z", "--no-renames", base, "--")
	if err != nil {
		return nil, err
	}
	untracked, err := repositorygit.Output(ctx, worktree, maxGitListing, "ls-files", "--others", "--exclude-standard", "-z")
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	var files []string
	for _, path := range append(strings.Split(tracked, "\x00"), strings.Split(untracked, "\x00")...) {
		path = strings.TrimSpace(path)
		if path == "" || seen[path] {
			continue
		}
		seen[path] = true
		files = append(files, path)
		if len(files) > maxChangedFiles {
			return nil, errors.New("more than 10,000 files changed")
		}
	}
	sort.Strings(files)
	return files, nil
}
