// Package pairverify tests the two branches of one collision together on the
// developer's machine. It merges them in temporary detached worktrees,
// compares TypeScript diagnostics of the merged tree with each branch alone,
// runs the default branch's pair checks through the shared check runner, and
// records an advisory combination check. It runs code from both branches only
// after explicit confirmation, reads its commands only from the repository
// default branch, and passes no Conn credential to any command. MCP tools and
// hooks never use it.
package pairverify

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"uuid"

	"github.com/Juanqu261/conn/internal/analyzer"
	"github.com/Juanqu261/conn/internal/checkrun"
	"github.com/Juanqu261/conn/internal/httpclient"
	"github.com/Juanqu261/conn/internal/protocol"
	"github.com/Juanqu261/conn/internal/repositorygit"
)

// Bounds and timeouts.
const (
	gitTimeout         = 2 * time.Minute
	fetchTimeout       = 5 * time.Minute
	diagnosticsTimeout = 5 * time.Minute
	cleanupTimeout     = time.Minute
	maxConfigBytes     = 256 << 10
	mergeName          = "Conn verify-pair"
	mergeEmail         = "verify-pair@conn.invalid"
	// Warning is the statement shown before anything runs.
	Warning = "This runs code from both branches on this machine."
)

// ErrNotConfirmed means the human declined, or no confirmation was possible.
var ErrNotConfirmed = errors.New("pair verification was not confirmed; nothing ran")

// Request selects one collision and how to verify it.
type Request struct {
	// Worktree is the resolved top level of the developer's worktree.
	Worktree    string
	ProjectID   uuid.UUID
	CollisionID uuid.UUID
	// Fetch runs git fetch origin <left> <right> before resolving branches.
	Fetch bool
	// KeepWorktree leaves the temporary worktrees for inspection.
	KeepWorktree bool
	// Parent is the directory for the temporary worktrees; empty uses the
	// system temporary directory.
	Parent string
	// Confirm is asked before anything runs. Nil, or false, stops.
	Confirm func(Plan) (bool, error)
	// Output receives command output and progress.
	Output io.Writer
	// Environ is the caller's environment; CONN_ variables are removed.
	Environ           []string
	Analyzer          analyzer.Config
	AnalyzerAvailable bool
}

// Branch is one resolved side of the collision.
type Branch struct {
	IntentID      uuid.UUID
	Revision      int
	Name          string
	Commit        string
	HeadMatches   bool
	FromRemote    bool
	IntentSummary string
}

// Plan is what will run, shown before confirmation.
type Plan struct {
	Collision     protocol.CollisionAssessment
	Left, Right   Branch
	DefaultBranch string
	ConfigSource  *protocol.CombinationConfigSource
	Setup         *protocol.CheckDefinition
	Checks        []PlannedCheck
	ConfigNote    string
}

// PlannedCheck is one pair check from the default-branch configuration.
type PlannedCheck struct {
	Key   string
	Check protocol.CheckDefinition
}

// Result is a finished verification.
type Result struct {
	Plan     Plan
	Input    protocol.CombinationCheckInput
	Recorded *protocol.CombinationCheck
	// AsOfEvent is the cursor after recording.
	AsOfEvent int64
	// RecordErr is set when the check ran but Conn recorded nothing.
	RecordErr error
	// Kept lists worktrees that remain, because of --keep-worktree or a
	// failed cleanup, with the command that removes each.
	Kept    []string
	LogDir  string
	Details []string
}

// Prepare reads the collision, resolves both branches and the default-branch
// configuration, and writes nothing except an explicit --fetch.
func Prepare(ctx context.Context, client *httpclient.Client, request Request) (Plan, error) {
	pack, err := client.ConflictContext(ctx, request.ProjectID, request.CollisionID)
	if err != nil {
		return Plan{}, err
	}
	collision := pack.Data.Collision
	if collision.State == protocol.CollisionStale {
		return Plan{}, errors.New("the collision is stale: a newer Intent revision superseded it; verify the current collision instead")
	}
	intents := map[uuid.UUID]protocol.IntentRevision{}
	for _, intent := range pack.Data.Intents {
		intents[intent.IntentID] = intent
	}
	left, leftOK := intents[collision.LeftIntentID]
	right, rightOK := intents[collision.RightIntentID]
	if !leftOK || !rightOK {
		return Plan{}, errors.New("the collision context does not include both Intent revisions")
	}
	for _, intent := range []protocol.IntentRevision{left, right} {
		if intent.BranchRef == "" || !repositorygit.ValidBranchName(intent.BranchRef) {
			return Plan{}, fmt.Errorf("Intent %s revision %d names no usable branch_ref; pair verification needs both branches", intent.IntentID, intent.Revision)
		}
		if intent.RepositoryID == nil {
			return Plan{}, fmt.Errorf("Intent %s names no repository", intent.IntentID)
		}
	}
	if *left.RepositoryID != *right.RepositoryID {
		return Plan{}, errors.New("the two Intents name different repositories, so their branches cannot be merged")
	}
	repositories, err := client.RepositoryConnections(ctx, request.ProjectID)
	if err != nil {
		return Plan{}, err
	}
	var repository *protocol.RepositoryConnection
	for i := range repositories.Data.Repositories {
		if repositories.Data.Repositories[i].RepositoryID == *left.RepositoryID {
			repository = &repositories.Data.Repositories[i]
		}
	}
	metadata, err := repositorygit.Discover(ctx, request.Worktree)
	if repository == nil || err != nil || metadata.CanonicalRemote != repository.CanonicalRemote {
		return Plan{}, errors.New("this worktree's origin does not match the Intents' repository connection")
	}
	if request.Fetch {
		if _, err := runGit(ctx, request.Worktree, fetchTimeout, nil, "fetch", "--quiet", "origin", "--", left.BranchRef, right.BranchRef); err != nil {
			return Plan{}, fmt.Errorf("git fetch origin %s %s failed: %w", left.BranchRef, right.BranchRef, err)
		}
	}
	plan := Plan{Collision: collision}
	if plan.Left, err = resolveBranch(ctx, request.Worktree, left, request.Fetch); err != nil {
		return Plan{}, err
	}
	if plan.Right, err = resolveBranch(ctx, request.Worktree, right, request.Fetch); err != nil {
		return Plan{}, err
	}
	if plan.DefaultBranch, err = repositorygit.DefaultBranch(ctx, request.Worktree, repository.DefaultBranch); err != nil {
		return Plan{}, errors.New("the repository default branch is unknown; record it on the RepositoryConnection or run git remote set-head origin --auto")
	}
	if err := loadDefaultConfig(ctx, request.Worktree, &plan); err != nil {
		return Plan{}, err
	}
	return plan, nil
}

func resolveBranch(ctx context.Context, worktree string, intent protocol.IntentRevision, fetched bool) (Branch, error) {
	branch := Branch{IntentID: intent.IntentID, Revision: intent.Revision, Name: intent.BranchRef, IntentSummary: intent.SummaryMarkdown}
	for _, candidate := range []struct {
		ref    string
		remote bool
	}{{"refs/heads/" + intent.BranchRef, false}, {"refs/remotes/origin/" + intent.BranchRef, true}} {
		output, err := runGit(ctx, worktree, gitTimeout, nil, "rev-parse", "--verify", "--quiet", candidate.ref+"^{commit}")
		if err == nil {
			branch.Commit, branch.FromRemote = strings.TrimSpace(string(output)), candidate.remote
			branch.HeadMatches = intent.HeadRevisionRef != "" && protocol.SameCommit(intent.HeadRevisionRef, branch.Commit)
			return branch, nil
		}
	}
	hint := " Pass --fetch to fetch it from origin."
	if fetched {
		hint = " It is not on origin either."
	}
	return Branch{}, fmt.Errorf("branch %s of Intent %s is not available locally or as origin/%s.%s", intent.BranchRef, intent.IntentID, intent.BranchRef, hint)
}

// loadDefaultConfig reads .conn/checks.json from origin/<default branch>,
// never from either feature branch or the working tree.
func loadDefaultConfig(ctx context.Context, worktree string, plan *Plan) error {
	ref := "origin/" + plan.DefaultBranch
	if _, err := runGit(ctx, worktree, gitTimeout, nil, "rev-parse", "--verify", "--quiet", "refs/remotes/"+ref+"^{commit}"); err != nil {
		plan.ConfigNote = ref + " is not available locally, so no pair commands run; only the diagnostic comparison runs."
		return nil
	}
	data, err := runGit(ctx, worktree, gitTimeout, nil, "show", "refs/remotes/"+ref+":"+checkrun.ConfigPath)
	if err != nil {
		plan.ConfigNote = checkrun.ConfigPath + " does not exist on " + ref + ", so only the diagnostic comparison runs."
		return nil
	}
	if len(data) > maxConfigBytes {
		return fmt.Errorf("%s on %s is larger than 256 KiB", checkrun.ConfigPath, ref)
	}
	config, err := checkrun.ParseConfig(data)
	if err != nil {
		return fmt.Errorf("%s on %s: %w", checkrun.ConfigPath, ref, err)
	}
	plan.ConfigSource = &protocol.CombinationConfigSource{Ref: ref, SHA256: config.SHA256}
	pair := config.Document.PairVerification
	if pair == nil {
		plan.ConfigNote = checkrun.ConfigPath + " on " + ref + " has no pair_verification section, so only the diagnostic comparison runs."
		return nil
	}
	plan.Setup = pair.Setup
	for _, key := range pair.Checks {
		check, err := config.Check(key)
		if err != nil {
			return err
		}
		plan.Checks = append(plan.Checks, PlannedCheck{Key: key, Check: check})
	}
	return nil
}

// Run confirms the plan, verifies the merged tree, records the combination
// check, and removes the temporary worktrees on success, failure, and
// cancellation unless KeepWorktree is set.
func Run(ctx context.Context, client *httpclient.Client, request Request) (result Result, err error) {
	plan, err := Prepare(ctx, client, request)
	if err != nil {
		return Result{}, err
	}
	if request.Confirm == nil {
		return Result{Plan: plan}, ErrNotConfirmed
	}
	confirmed, err := request.Confirm(plan)
	if err != nil {
		return Result{Plan: plan}, err
	}
	if !confirmed {
		return Result{Plan: plan}, ErrNotConfirmed
	}
	output := request.Output
	if output == nil {
		output = io.Discard
	}
	gitDir, err := repositorygit.GitDir(ctx, request.Worktree)
	if err != nil {
		return Result{Plan: plan}, err
	}
	result = Result{Plan: plan, LogDir: filepath.Join(gitDir, "conn", checkrun.RunsDir)}

	parent, err := os.MkdirTemp(request.Parent, "conn-verify-pair-")
	if err != nil {
		return result, fmt.Errorf("create the temporary worktree directory: %w", err)
	}
	leftDir, rightDir := filepath.Join(parent, "left"), filepath.Join(parent, "right")
	var created []string
	defer func() {
		if request.KeepWorktree {
			result.Kept = append(result.Kept, created...)
			return
		}
		// Cleanup must survive the cancellation that may have ended the run.
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), cleanupTimeout)
		defer cancel()
		for _, dir := range created {
			if _, err := runGit(cleanup, request.Worktree, cleanupTimeout, nil, "worktree", "remove", "--force", dir); err != nil {
				result.Kept = append(result.Kept, dir)
			}
		}
		if len(result.Kept) == 0 {
			_ = os.RemoveAll(parent)
		}
	}()
	for _, side := range []struct {
		dir    string
		commit string
	}{{leftDir, plan.Left.Commit}, {rightDir, plan.Right.Commit}} {
		if _, err := runGit(ctx, request.Worktree, gitTimeout, nil, "worktree", "add", "--quiet", "--detach", side.dir, side.commit); err != nil {
			return result, fmt.Errorf("create a detached worktree at %s: %w", side.commit, err)
		}
		created = append(created, side.dir)
	}

	input := protocol.CombinationCheckInput{
		Left:            side(plan.Left),
		Right:           side(plan.Right),
		MergeResult:     protocol.MergeClean,
		ConflictedPaths: []string{},
		Diagnostics:     protocol.CombinationDiagnostics{New: []protocol.CombinationDiagnostic{}},
		Checks:          []protocol.CombinationCheckRun{},
		ConfigSource:    plan.ConfigSource,
		Runner:          protocol.CombinationRunnerLocal,
	}
	incomplete := false

	// Both branches are analyzed before the merge and before any dependency
	// setup, so all three runs compare the same conditions.
	var leftCounts, rightCounts *protocol.LocalAnalyzerReport
	if request.AnalyzerAvailable {
		fmt.Fprintln(output, "Counting TypeScript diagnostics on each branch alone...")
		leftCounts, err = diagnostics(ctx, request, leftDir, plan)
		if err == nil {
			rightCounts, err = diagnostics(ctx, request, rightDir, plan)
		}
		if ctx.Err() != nil {
			return result, ctx.Err()
		}
		if err != nil {
			result.Details = append(result.Details, "The analyzer did not complete on the branches: "+err.Error())
			leftCounts, rightCounts, incomplete = nil, nil, true
		}
	} else {
		result.Details = append(result.Details, "The local TypeScript analyzer or Node.js is unavailable, so diagnostics were not compared.")
		incomplete = true
	}

	fmt.Fprintf(output, "Merging %s into %s in a temporary worktree...\n", short(plan.Right.Commit), short(plan.Left.Commit))
	conflicts, err := merge(ctx, leftDir, plan.Right.Commit)
	if ctx.Err() != nil {
		return result, ctx.Err()
	}
	if err != nil {
		return result, err
	}
	if len(conflicts) > 0 {
		input.MergeResult, input.Outcome = protocol.CombinationMergeConflict, protocol.CombinationMergeConflict
		if len(conflicts) > protocol.MaxCombinationPaths {
			result.Details = append(result.Details, fmt.Sprintf("Only %d of %d conflicted paths are recorded.", protocol.MaxCombinationPaths, len(conflicts)))
			conflicts = conflicts[:protocol.MaxCombinationPaths]
		}
		input.ConflictedPaths = conflicts
		return record(ctx, client, request, result, input)
	}

	if leftCounts != nil && rightCounts != nil {
		fmt.Fprintln(output, "Counting TypeScript diagnostics on the merged tree...")
		merged, err := diagnostics(ctx, request, leftDir, plan)
		if ctx.Err() != nil {
			return result, ctx.Err()
		}
		if err != nil {
			result.Details = append(result.Details, "The analyzer did not complete on the merged tree: "+err.Error())
			incomplete = true
		} else {
			input.Diagnostics = newDiagnostics(*leftCounts, *rightCounts, *merged)
		}
	}

	setupFailed := false
	if plan.Setup != nil {
		fmt.Fprintf(output, "Running setup: %s\n", strings.Join(plan.Setup.Command, " "))
		run, err := execute(ctx, request, leftDir, gitDir, "setup", *plan.Setup, plan)
		if ctx.Err() != nil {
			return result, ctx.Err()
		}
		if err != nil {
			return result, err
		}
		input.Setup = &protocol.CombinationRun{ExitCode: run.ExitCode, TimedOut: run.TimedOut, DurationMS: run.DurationMS, OutputSHA256: run.OutputSHA256}
		setupFailed = run.ExitCode != 0 || run.TimedOut
		if setupFailed {
			incomplete = true
			result.Details = append(result.Details, "Setup did not succeed, so no pair check ran.")
		}
	}
	failing := input.Diagnostics.NewCount > 0
	if !setupFailed {
		for _, planned := range plan.Checks {
			fmt.Fprintf(output, "Running pair check %s: %s\n", planned.Key, strings.Join(planned.Check.Command, " "))
			run, err := execute(ctx, request, leftDir, gitDir, planned.Key, planned.Check, plan)
			if ctx.Err() != nil {
				return result, ctx.Err()
			}
			if err != nil {
				return result, err
			}
			state := protocol.EvidencePassing
			switch {
			case run.TimedOut:
				state, incomplete = protocol.CombinationCheckTimedOut, true
			case run.ExitCode != 0:
				state, failing = protocol.EvidenceFailing, true
			}
			input.Checks = append(input.Checks, protocol.CombinationCheckRun{
				Key: planned.Key, ExitCode: run.ExitCode, TimedOut: run.TimedOut, DurationMS: run.DurationMS,
				OutputSHA256: run.OutputSHA256, State: state,
			})
		}
	}
	switch {
	case failing:
		input.Outcome = protocol.CombinationFailing
	case incomplete:
		input.Outcome = protocol.CombinationIncomplete
	default:
		input.Outcome = protocol.CombinationPassing
	}
	return record(ctx, client, request, result, input)
}

func side(branch Branch) protocol.CombinationSide {
	return protocol.CombinationSide{IntentID: branch.IntentID, IntentRevision: branch.Revision, BranchRef: branch.Name, Commit: branch.Commit, HeadMatchesIntent: branch.HeadMatches}
}

func record(ctx context.Context, client *httpclient.Client, request Request, result Result, input protocol.CombinationCheckInput) (Result, error) {
	result.Input = input
	recorded, err := client.RecordCombinationCheck(ctx, request.ProjectID, request.CollisionID, input)
	if err != nil {
		result.RecordErr = err
		return result, nil
	}
	result.Recorded, result.AsOfEvent = &recorded.Data, recorded.AsOfEvent
	return result, nil
}

func diagnostics(ctx context.Context, request Request, dir string, plan Plan) (*protocol.LocalAnalyzerReport, error) {
	config := request.Analyzer
	if config.Timeout < diagnosticsTimeout {
		config.Timeout = diagnosticsTimeout
	}
	metadata, err := repositorygit.Discover(ctx, request.Worktree)
	if err != nil {
		return nil, err
	}
	report, err := analyzer.Run(ctx, config, analyzer.Request{Mode: protocol.AnalyzerModeDiagnostics, Worktree: dir, CanonicalRemote: metadata.CanonicalRemote})
	if err != nil {
		return nil, err
	}
	return &report, nil
}

// newDiagnostics reports each (code, path) pair whose merged count exceeds
// the larger of its counts on either branch alone.
func newDiagnostics(left, right, merged protocol.LocalAnalyzerReport) protocol.CombinationDiagnostics {
	key := func(count protocol.AnalyzerDiagnosticCount) string { return count.Code + "\x00" + count.Path }
	baseline := map[string]int{}
	for _, report := range []protocol.LocalAnalyzerReport{left, right} {
		for _, count := range report.DiagnosticCounts {
			baseline[key(count)] = max(baseline[key(count)], count.Count)
		}
	}
	var found []protocol.CombinationDiagnostic
	for _, count := range merged.DiagnosticCounts {
		if count.Count > baseline[key(count)] {
			found = append(found, protocol.CombinationDiagnostic{Code: count.Code, Path: count.Path})
		}
	}
	sort.Slice(found, func(i, j int) bool {
		if found[i].Path != found[j].Path {
			return found[i].Path < found[j].Path
		}
		return found[i].Code < found[j].Code
	})
	truncated := truncatedCounts(left) || truncatedCounts(right) || truncatedCounts(merged)
	result := protocol.CombinationDiagnostics{Ran: true, NewCount: len(found), New: []protocol.CombinationDiagnostic{}, Truncated: truncated}
	if len(found) > protocol.MaxCombinationNew {
		found, result.Truncated = found[:protocol.MaxCombinationNew], true
	}
	result.New = append(result.New, found...)
	return result
}

func truncatedCounts(report protocol.LocalAnalyzerReport) bool {
	return report.DiagnosticCountsTruncated != nil && *report.DiagnosticCountsTruncated
}

// merge merges commit into the worktree without committing, with a fixed
// identity. It returns the conflicted paths, or none for a clean merge.
func merge(ctx context.Context, dir, commit string) ([]string, error) {
	_, mergeErr := runGit(ctx, dir, gitTimeout, nil, "-c", "user.name="+mergeName, "-c", "user.email="+mergeEmail,
		"-c", "commit.gpgsign=false", "merge", "--no-commit", "--no-ff", "--no-edit", "--quiet", commit)
	output, err := runGit(ctx, dir, gitTimeout, nil, "diff", "--name-only", "-z", "--diff-filter=U")
	if err != nil {
		return nil, fmt.Errorf("git could not list conflicted paths: %w", err)
	}
	var conflicts []string
	for _, path := range strings.Split(string(output), "\x00") {
		if path = strings.TrimSpace(path); path != "" {
			conflicts = append(conflicts, path)
		}
	}
	if mergeErr != nil && len(conflicts) == 0 {
		return nil, fmt.Errorf("git merge failed without conflicts: %w", mergeErr)
	}
	sort.Strings(conflicts)
	return conflicts, nil
}

func execute(ctx context.Context, request Request, dir, gitDir, key string, check protocol.CheckDefinition, plan Plan) (protocol.EvidenceObservation, error) {
	sha := ""
	if plan.ConfigSource != nil {
		sha = plan.ConfigSource.SHA256
	}
	result, err := checkrun.Execute(ctx, checkrun.Execution{
		Root: dir, GitDir: gitDir, Key: key, Check: check, ConfigSHA256: sha,
		Output: request.Output, Environ: request.Environ,
	})
	if err != nil {
		return protocol.EvidenceObservation{}, err
	}
	return result.Observation, nil
}

// runGit runs git with fixed arguments, no shell, a timeout, no terminal
// prompt, and the minimized Conn Git environment.
func runGit(ctx context.Context, dir string, timeout time.Duration, stdin io.Reader, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", dir}, args...)...)
	cmd.Env = repositorygit.Environment()
	cmd.Stdin = stdin
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		message := strings.TrimSpace(stderr.String())
		if len(message) > 300 {
			message = message[:300]
		}
		if message != "" {
			return nil, fmt.Errorf("%w: %s", err, message)
		}
		return nil, err
	}
	return stdout.Bytes(), nil
}

func short(commit string) string {
	if len(commit) > 12 {
		return commit[:12]
	}
	return commit
}
