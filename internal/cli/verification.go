package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"uuid"

	"github.com/spf13/cobra"

	"github.com/Juanqu261/conn/internal/pairverify"
	"github.com/Juanqu261/conn/internal/protocol"
	"github.com/Juanqu261/conn/internal/repositorygit"
)

func newVerifyPairCommand(deps Dependencies, streams Streams) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "verify-pair <collision-id>",
		Short: "Test the two branches of a collision together on this machine",
		Long: "Merge the branches of both Intents of a collision in temporary detached worktrees, compare\n" +
			"TypeScript diagnostics of the merged tree with each branch alone, run the pair_verification\n" +
			"setup and checks from .conn/checks.json on the repository default branch, and record an\n" +
			"advisory combination check. It never reads commands from either feature branch, passes no\n" +
			"CONN_ variable to any command, and never changes the collision state.\n\n" +
			pairverify.Warning + " It asks for confirmation; pass --yes for non-interactive use.\n" +
			"Temporary worktrees are removed after success, failure, or cancellation unless --keep-worktree\n" +
			"is passed. Branches missing locally are fetched only with --fetch.",
		Args: cobra.ExactArgs(1),
	}
	r := newRemote(cmd, deps, streams)
	var projectID uuid.UUID
	var worktree string
	var fetch, yes, keep bool
	cmd.Flags().Var(uuidValue{&projectID}, "project", "project ID (default: the focused Project, CONN_PROJECT, or the matching Project)")
	cmd.Flags().StringVar(&worktree, "worktree", ".", "local repository worktree")
	cmd.Flags().BoolVar(&fetch, "fetch", false, "fetch both branches from origin first")
	cmd.Flags().BoolVar(&yes, "yes", false, "confirm running code from both branches without a prompt")
	cmd.Flags().BoolVar(&keep, "keep-worktree", false, "keep the temporary worktrees for inspection")
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		collisionID, err := uuid.Parse(args[0])
		if err != nil {
			return usageError{errors.New("the collision ID is not a UUID")}
		}
		root, err := resolveWorktree(cmd.Context(), worktree)
		if err != nil {
			return err
		}
		client, err := r.client()
		if err != nil {
			return err
		}
		if projectID, err = resolveCLIProject(cmd.Context(), deps, client, projectID, root); err != nil {
			return err
		}
		if !yes && !interactive(streams.In) {
			return usageError{errors.New("verify-pair runs code from both branches; confirm interactively or pass --yes")}
		}
		out := streams.Out
		if r.json {
			out = streams.Err
		}
		environ := os.Environ
		if deps.Environ != nil {
			environ = deps.Environ
		}
		location := locateAnalyzer(deps)
		ask := newPrompter(streams)
		result, err := pairverify.Run(cmd.Context(), client, pairverify.Request{
			Worktree: root, ProjectID: projectID, CollisionID: collisionID, Fetch: fetch, KeepWorktree: keep,
			Output: out, Environ: environ(), Analyzer: location.Config, AnalyzerAvailable: location.Available(),
			Confirm: func(plan pairverify.Plan) (bool, error) {
				renderPairPlan(out, plan)
				if yes {
					return true, nil
				}
				return ask.confirm("Run these on this machine?")
			},
		})
		for _, kept := range result.Kept {
			fmt.Fprintf(streams.Err, "Temporary worktree kept at %s. Remove it with: git worktree remove --force %q\n", kept, kept)
		}
		if err != nil {
			return err
		}
		renderPairResult(out, result)
		if result.RecordErr != nil {
			return hint(result.RecordErr, "the pair verification ran, but Conn recorded nothing: "+explain(result.RecordErr).Error())
		}
		if r.json {
			data, err := protocol.Encode(*result.Recorded)
			if err != nil {
				return err
			}
			writeEnvelope(streams.Out, data, result.AsOfEvent)
		}
		return nil
	}
	return cmd
}

func resolveWorktree(ctx context.Context, worktree string) (string, error) {
	absolute, err := filepath.Abs(worktree)
	if err != nil {
		return "", usageError{err}
	}
	if absolute, err = filepath.EvalSymlinks(absolute); err != nil {
		return "", usageError{fmt.Errorf("resolve the worktree: %w", err)}
	}
	top, err := repositorygit.TopLevel(ctx, absolute)
	if err != nil {
		return "", usageError{err}
	}
	return filepath.Clean(filepath.FromSlash(top)), nil
}

func renderPairPlan(out io.Writer, plan pairverify.Plan) {
	describe := func(label string, branch pairverify.Branch) {
		source := "local branch"
		if branch.FromRemote {
			source = "origin"
		}
		matches := "matches"
		if !branch.HeadMatches {
			matches = "does not match"
		}
		fmt.Fprintf(out, "  %s: Intent %s revision %d, %s %s at %s (%s the Intent's recorded head)\n", label, branch.IntentID, branch.Revision, source, branch.Name, short12(branch.Commit), matches)
	}
	fmt.Fprintf(out, "Pair verification for collision %s (%s, %s):\n", plan.Collision.CollisionID, plan.Collision.RuleID, plan.Collision.State)
	describe("left ", plan.Left)
	describe("right", plan.Right)
	if plan.ConfigSource != nil {
		fmt.Fprintf(out, "Commands come from %s on %s (sha256 %s):\n", ".conn/checks.json", plan.ConfigSource.Ref, plan.ConfigSource.SHA256[:12])
	}
	if plan.ConfigNote != "" {
		fmt.Fprintln(out, plan.ConfigNote)
	}
	if plan.Setup != nil {
		fmt.Fprintf(out, "  setup: %s (timeout %ds)\n", strings.Join(plan.Setup.Command, " "), plan.Setup.TimeoutSeconds)
	}
	for _, check := range plan.Checks {
		fmt.Fprintf(out, "  check %s: %s (timeout %ds)\n", check.Key, strings.Join(check.Check.Command, " "), check.Check.TimeoutSeconds)
	}
	fmt.Fprintln(out, pairverify.Warning)
}

func renderPairResult(out io.Writer, result pairverify.Result) {
	input := result.Input
	fmt.Fprintf(out, "\nCombination outcome: %s (run on this machine; not independent CI proof).\n", strings.ReplaceAll(input.Outcome, "_", " "))
	if input.MergeResult == protocol.CombinationMergeConflict {
		fmt.Fprintf(out, "The branches conflict in %d path(s):\n", len(input.ConflictedPaths))
		for _, path := range input.ConflictedPaths {
			fmt.Fprintf(out, "  %s\n", path)
		}
	}
	if input.Diagnostics.Ran {
		fmt.Fprintf(out, "New TypeScript diagnostics in the merged tree: %d\n", input.Diagnostics.NewCount)
		for _, diagnostic := range input.Diagnostics.New {
			fmt.Fprintf(out, "  %s %s\n", diagnostic.Code, diagnostic.Path)
		}
	}
	if input.Setup != nil {
		fmt.Fprintf(out, "Setup exited %d in %d ms.\n", input.Setup.ExitCode, input.Setup.DurationMS)
	}
	for _, check := range input.Checks {
		fmt.Fprintf(out, "Pair check %s: %s (exit %d, %d ms)\n", check.Key, check.State, check.ExitCode, check.DurationMS)
	}
	for _, detail := range result.Details {
		fmt.Fprintln(out, detail)
	}
	if len(input.Checks) > 0 || input.Setup != nil {
		fmt.Fprintf(out, "Local logs: %s\n", result.LogDir)
	}
	if result.Recorded != nil {
		fmt.Fprintf(out, "Recorded combination check %s for collision %s at event %d. The collision state is unchanged.\n", result.Recorded.CheckID, result.Recorded.CollisionID, result.AsOfEvent)
	}
}

func short12(commit string) string {
	if len(commit) > 12 {
		return commit[:12]
	}
	return commit
}
