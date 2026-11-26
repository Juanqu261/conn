package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"uuid"

	"github.com/spf13/cobra"

	localanalyzer "github.com/Juanqu261/conn/internal/analyzer"
	"github.com/Juanqu261/conn/internal/checkrun"
	"github.com/Juanqu261/conn/internal/githook"
	"github.com/Juanqu261/conn/internal/hookstate"
	"github.com/Juanqu261/conn/internal/httpclient"
	"github.com/Juanqu261/conn/internal/preedit"
	"github.com/Juanqu261/conn/internal/protocol"
	"github.com/Juanqu261/conn/internal/repositorygit"
	"github.com/Juanqu261/conn/internal/review"
)

// trailerCommand reports whether args invoke `conn trailer`, which never
// fails a commit.
func trailerCommand(args []string) bool {
	return len(args) > 0 && args[0] == "trailer"
}

// focusTarget resolves the worktree, its local focus record, and the Project
// and Intent a command addresses. Flags win over the focus record.
type focusTarget struct {
	worktree  string
	focus     hookstate.Focus
	projectID uuid.UUID
	intentID  uuid.UUID
}

func resolveTarget(ctx context.Context, worktree string, projectID, intentID uuid.UUID) (focusTarget, error) {
	absolute, err := filepath.Abs(worktree)
	if err != nil {
		return focusTarget{}, usageError{err}
	}
	if absolute, err = filepath.EvalSymlinks(absolute); err != nil {
		return focusTarget{}, usageError{fmt.Errorf("resolve --worktree: %w", err)}
	}
	top, err := repositorygit.TopLevel(ctx, absolute)
	if err != nil {
		return focusTarget{}, usageError{err}
	}
	target := focusTarget{worktree: filepath.Clean(filepath.FromSlash(top)), projectID: projectID, intentID: intentID}
	if store, err := hookstate.Open(ctx, target.worktree); err == nil {
		target.focus = store.Focus()
	}
	if target.intentID == (uuid.UUID{}) && target.focus.IntentID != nil {
		target.intentID = *target.focus.IntentID
	}
	if target.projectID == (uuid.UUID{}) && target.focus.ProjectID != nil {
		target.projectID = *target.focus.ProjectID
	}
	if target.intentID == (uuid.UUID{}) || target.projectID == (uuid.UUID{}) {
		return focusTarget{}, usageError{errors.New("no Intent is focused in this worktree; pass --project and --intent")}
	}
	return target, nil
}

// useFocusServer makes the focus record's server the default after an
// explicit --server and CONN_SERVER_URL, as the hooks do.
func useFocusServer(cmd *cobra.Command, deps Dependencies, r *remote, focus hookstate.Focus) {
	original := r.server
	r.server = func() string {
		if cmd.Flags().Changed("server") {
			return original()
		}
		if value, ok := deps.LookupEnv(ServerURLEnv); ok && value != "" {
			return value
		}
		if focus.ServerURL != "" {
			return focus.ServerURL
		}
		return original()
	}
}

func newReviewCommand(deps Dependencies, streams Streams) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "review",
		Short: "Generate the review packet for an Intent",
		Long: "Generate a bounded review packet: what the Intent set out to do, its acceptance evidence\n" +
			"with provenance and staleness, scope drift between declared and observed claims, collisions\n" +
			"and decisions, and integrity notes. It is built from Conn records and local Git facts and is\n" +
			"never stored. The Markdown form stays under 60,000 characters for a pull-request description.\n\n" +
			"The Intent defaults to the worktree's focused Intent. --analyze first runs observed analysis\n" +
			"of the current Intent revision, which attaches claims and is a write.",
		Args: cobra.NoArgs,
	}
	r := newRemote(cmd, deps, streams)
	var projectID, intentID uuid.UUID
	var base, format, output, worktree string
	var analyze bool
	cmd.Flags().Var(uuidValue{&projectID}, "project", "project ID (default: the focused Project)")
	cmd.Flags().Var(uuidValue{&intentID}, "intent", "intent ID (default: the focused Intent)")
	cmd.Flags().StringVar(&base, "base", "", "base commit or branch (default: the Intent's base_revision_ref)")
	cmd.Flags().StringVar(&format, "format", "markdown", "packet format: markdown or json")
	cmd.Flags().StringVar(&output, "output", "", "write the packet to this file instead of standard output")
	cmd.Flags().StringVar(&worktree, "worktree", ".", "local repository worktree")
	cmd.Flags().BoolVar(&analyze, "analyze", false, "run observed analysis of the current Intent revision first (a write)")
	cmd.RunE = func(cmd *cobra.Command, _ []string) error {
		if format != "markdown" && format != "json" {
			return usageError{errors.New("--format must be markdown or json")}
		}
		if r.json {
			format = "json"
		}
		target, err := resolveTarget(cmd.Context(), worktree, projectID, intentID)
		if err != nil {
			return err
		}
		useFocusServer(cmd, deps, r, target.focus)
		client, err := r.client()
		if err != nil {
			return err
		}
		if analyze {
			if err := analyzeForReview(cmd.Context(), deps, client, target, streams.Err); err != nil {
				return err
			}
		}
		packet, err := review.Generate(cmd.Context(), client, review.Request{ProjectID: target.projectID, IntentID: target.intentID, Worktree: target.worktree, Base: base})
		if err != nil {
			return err
		}
		text := review.RenderMarkdown(packet)
		if format == "json" {
			if text, err = review.RenderJSON(packet); err != nil {
				return err
			}
		}
		if output == "" {
			_, err = io.WriteString(streams.Out, text)
			return err
		}
		if err := os.WriteFile(output, []byte(text), 0o644); err != nil {
			return usageError{err}
		}
		fmt.Fprintf(streams.Err, "Wrote the review packet for Intent %s revision %d at event %d to %s.\n", packet.Intent.ID, packet.Intent.Revision, packet.GeneratedAtEvent, output)
		return nil
	}
	return cmd
}

// analyzeForReview runs observed analysis of the current Intent revision and
// attaches it as the CLI participant.
func analyzeForReview(ctx context.Context, deps Dependencies, client *httpclient.Client, target focusTarget, diagnostics io.Writer) error {
	intent, err := client.Intent(ctx, target.projectID, target.intentID)
	if err != nil {
		return err
	}
	revision := intent.Data.IntentRevision
	if revision.RepositoryID == nil {
		return usageError{errors.New("the Intent names no repository, so it cannot be analyzed")}
	}
	repositories, err := client.RepositoryConnections(ctx, target.projectID)
	if err != nil {
		return err
	}
	var repository *protocol.RepositoryConnection
	for i := range repositories.Data.Repositories {
		if repositories.Data.Repositories[i].RepositoryID == *revision.RepositoryID {
			repository = &repositories.Data.Repositories[i]
		}
	}
	metadata, err := repositorygit.Discover(ctx, target.worktree)
	if repository == nil || err != nil || metadata.CanonicalRemote != repository.CanonicalRemote {
		return usageError{errors.New("this worktree's origin does not match the Intent's repository connection")}
	}
	base := revision.BaseRevisionRef
	if base == "" {
		if base, err = preedit.BaseRevision(ctx, target.worktree, repository.DefaultBranch); err != nil {
			return usageError{err}
		}
	}
	location := locateAnalyzer(deps)
	if !location.Available() {
		return fmt.Errorf("%w: run conn doctor", localanalyzer.ErrUnavailable)
	}
	report, err := localanalyzer.Run(ctx, location.Config, localanalyzer.Request{
		Mode: protocol.AnalyzerModeObserved, Worktree: target.worktree, CanonicalRemote: repository.CanonicalRemote, BaseRevisionRef: base,
	})
	if err != nil {
		return err
	}
	if localanalyzer.HasDiagnostic(report, localanalyzer.DiagnosticBaseUnavailable) {
		return usageError{errors.New("the base revision is unavailable in this worktree; nothing was attached")}
	}
	if _, err := client.AttachStaticAnalysisAtRevision(ctx, target.projectID, target.intentID, revision.Revision, report); err != nil {
		if httpclient.IsCode(err, "forbidden") {
			return hint(err, "analysis attaches only through the Participant that owns the Intent; run analyze_intent in that Kiro session, or omit --analyze")
		}
		return err
	}
	fmt.Fprintf(diagnostics, "Attached observed analysis of Intent %s revision %d (%d claims, %d diagnostics).\n", target.intentID, revision.Revision, len(report.Claims), len(report.Diagnostics))
	return nil
}

func newEvidenceRunCommand(deps Dependencies, streams Streams) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "run <check-key>",
		Short: "Run a reviewed check from .conn/checks.json and attach observed evidence",
		Long: "Run one check defined in the repository's .conn/checks.json and attach one check_run\n" +
			"evidence reference per acceptance key: passing when the command exits 0 in time, otherwise\n" +
			"failing. The command runs as an argument array without a shell, in its configured directory,\n" +
			"with its timeout, and with your environment minus every CONN_ variable.\n\n" +
			"Output streams to the terminal. At most 10 MiB is kept in a local log under the worktree's\n" +
			"Git directory; only its SHA-256 hash is sent to Conn. The evidence is labelled as observed on\n" +
			"this machine, which is not independent CI proof.\n\n" +
			"The Intent defaults to the worktree's focused Intent and may be owned by any of your\n" +
			"Participants. The command exits with the check's exit code.",
		Args: cobra.ExactArgs(1),
	}
	r := newRemote(cmd, deps, streams)
	var projectID, intentID uuid.UUID
	var acceptance []string
	var worktree string
	cmd.Flags().Var(uuidValue{&projectID}, "project", "project ID (default: the focused Project)")
	cmd.Flags().Var(uuidValue{&intentID}, "intent", "intent ID (default: the focused Intent)")
	cmd.Flags().StringArrayVar(&acceptance, "acceptance", nil, "acceptance key to attach evidence for, instead of the configured keys (repeatable)")
	cmd.Flags().StringVar(&worktree, "worktree", ".", "local repository worktree")
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		target, err := resolveTarget(cmd.Context(), worktree, projectID, intentID)
		if err != nil {
			return err
		}
		useFocusServer(cmd, deps, r, target.focus)
		client, err := r.client()
		if err != nil {
			return err
		}
		terminal, report := streams.Out, streams.Out
		if r.json {
			terminal, report = streams.Err, streams.Err
		}
		environ := os.Environ
		if deps.Environ != nil {
			environ = deps.Environ
		}
		outcome, err := checkrun.RunAndAttach(cmd.Context(), client, checkrun.Request{
			Worktree: target.worktree, ProjectID: target.projectID, IntentID: target.intentID, CheckKey: args[0],
			AcceptanceKeys: acceptance, Output: terminal, Environ: environ(),
		})
		var configErr *checkrun.ConfigError
		if errors.As(err, &configErr) {
			return usageError{err}
		}
		if err != nil {
			return err
		}
		observation := outcome.Result.Observation
		status := "passed"
		switch {
		case observation.TimedOut:
			status = "timed out"
		case observation.ExitCode != 0:
			status = fmt.Sprintf("failed with exit code %d", observation.ExitCode)
		}
		dirty := ""
		if observation.WorktreeDirty {
			dirty = " with uncommitted changes"
		}
		fmt.Fprintf(report, "\nCheck %s %s in %d ms at commit %s%s on this machine.\n", args[0], status, observation.DurationMS, observation.Commit[:min(12, len(observation.Commit))], dirty)
		truncated := ""
		if outcome.Result.LogTruncated {
			truncated = " (first 10 MiB)"
		}
		fmt.Fprintf(report, "Local log%s: %s\n", truncated, outcome.Result.LogPath)
		if outcome.AttachErr != nil {
			if len(outcome.Evidence) == 0 {
				return hint(outcome.AttachErr, "the check ran, but Conn recorded nothing: "+explain(outcome.AttachErr).Error()+". Rerun the check when the server is reachable")
			}
			return hint(outcome.AttachErr, "the check ran, but only some evidence was recorded: "+explain(outcome.AttachErr).Error())
		}
		for _, evidence := range outcome.Evidence {
			key := "no acceptance key"
			if evidence.AcceptanceKey != "" {
				key = "acceptance key " + evidence.AcceptanceKey
			}
			fmt.Fprintf(report, "Attached %s check_run evidence %s (%s) to Intent %s.\n", evidence.State, evidence.EvidenceID, key, target.intentID)
		}
		if r.json {
			data, err := json.Marshal(map[string]any{"evidence": outcome.Evidence, "log_path": outcome.Result.LogPath, "log_truncated": outcome.Result.LogTruncated})
			if err != nil {
				return err
			}
			writeEnvelope(streams.Out, data, outcome.AsOfEvent)
		}
		if !checkrun.Passed(observation) {
			code := int(observation.ExitCode)
			if code < 1 || code > 255 {
				code = ExitFailure
			}
			return exitWith{code: code}
		}
		return nil
	}
	return cmd
}

func newTrailerCommand(deps Dependencies, streams Streams) *cobra.Command {
	return &cobra.Command{
		Use:   "trailer <message-file> [<source>]",
		Short: "Add the Conn-Intent trailer for the focused Intent to a commit message",
		Long: "Add `Conn-Intent: <intent-id>@<revision>` for the worktree's focused Intent to a commit\n" +
			"message file, unless the message already has a Conn-Intent trailer. The prepare-commit-msg\n" +
			"hook installed by `conn git-hook install` runs this command. It reads only the local focus\n" +
			"record, makes no network call, and always exits 0 so the commit continues; problems are\n" +
			"printed as one warning.",
		Args: cobra.RangeArgs(1, 3),
		RunE: func(cmd *cobra.Command, args []string) error {
			directory, err := os.Getwd()
			if err != nil {
				return err
			}
			store, err := hookstate.Open(cmd.Context(), directory)
			if err != nil {
				return err
			}
			focus := store.Focus()
			if !focus.HasIntent() || focus.IntentRevision < 1 {
				return errors.New("no Conn Intent is focused in this worktree")
			}
			return githook.AppendTrailer(cmd.Context(), args[0], protocol.CommitTrailer(*focus.IntentID, focus.IntentRevision))
		},
	}
}

func newGitHookCommand(deps Dependencies, streams Streams) *cobra.Command {
	var worktree string
	var commandOnPath bool
	cmd := &cobra.Command{
		Use:   "git-hook",
		Short: "Manage the optional prepare-commit-msg hook that adds Conn-Intent trailers",
		Long: "Preview, install, or remove Conn's optional prepare-commit-msg hook in the effective hooks\n" +
			"directory, including core.hooksPath. The hook runs `conn trailer` and never blocks a commit.\n" +
			"Installation refuses to overwrite a different hook and prints the line to add to it instead;\n" +
			"removal deletes only byte-identical Conn content. Preview writes nothing.",
		Args: cobra.NoArgs,
	}
	cmd.PersistentFlags().StringVar(&worktree, "worktree", ".", "local repository worktree")
	plan := func(c *cobra.Command, mode githook.Mode) (githook.Plan, error) {
		command := githook.PathCommand
		if mode == githook.ModeInstall && !commandOnPath {
			executable, err := gitHookExecutable(deps, streams.Err)
			if err != nil {
				return githook.Plan{}, err
			}
			command = githook.RenderCommand(executable)
		}
		absolute, err := filepath.Abs(worktree)
		if err != nil {
			return githook.Plan{}, usageError{err}
		}
		return githook.Preview(c.Context(), absolute, mode, command)
	}
	render := func(out io.Writer, plan githook.Plan) {
		fmt.Fprintf(out, "Conn Git hook %s plan for %s:\n  %-9s %s\n", plan.Mode, plan.HooksDir, plan.Action, filepath.ToSlash(plan.Path))
		if plan.Mode == githook.ModeInstall && plan.Action == githook.ActionCreate {
			fmt.Fprintf(out, "--- proposed %s ---\n%s--- end proposed %s ---\n", githook.HookName, plan.Content, githook.HookName)
		}
		if plan.Mode == githook.ModeInstall && plan.HasConflict() {
			fmt.Fprintf(out, "A different %s hook exists. To chain Conn manually, add this line to it:\n  %s\n", githook.HookName, plan.Snippet())
		}
	}
	preview := &cobra.Command{
		Use:   "preview",
		Short: "Show the hook installation plan without writing",
		Args:  cobra.NoArgs,
		RunE: func(c *cobra.Command, _ []string) error {
			proposed, err := plan(c, githook.ModeInstall)
			if err != nil {
				return err
			}
			render(streams.Out, proposed)
			fmt.Fprintln(streams.Out, "No files changed.")
			return nil
		},
	}
	install := &cobra.Command{
		Use:   "install",
		Short: "Install the prepare-commit-msg hook after showing its plan",
		Args:  cobra.NoArgs,
		RunE: func(c *cobra.Command, _ []string) error {
			proposed, err := plan(c, githook.ModeInstall)
			if err != nil {
				return err
			}
			render(streams.Out, proposed)
			if proposed.HasConflict() {
				return errors.New("installation stopped: a different prepare-commit-msg hook exists and was preserved")
			}
			if err := githook.Apply(proposed); err != nil {
				return err
			}
			fmt.Fprintln(streams.Out, "Installed the Conn prepare-commit-msg hook. Remove it with `conn git-hook remove`.")
			return nil
		},
	}
	for _, command := range []*cobra.Command{preview, install} {
		command.Flags().BoolVar(&commandOnPath, "command-on-path", false, "run `conn` from PATH instead of this executable's absolute path")
	}
	remove := &cobra.Command{
		Use:   "remove",
		Short: "Remove the hook only when it is unchanged Conn content",
		Args:  cobra.NoArgs,
		RunE: func(c *cobra.Command, _ []string) error {
			proposed, err := plan(c, githook.ModeRemove)
			if err != nil {
				return err
			}
			render(streams.Out, proposed)
			if proposed.HasConflict() {
				return errors.New("removal stopped: the prepare-commit-msg hook is not unchanged Conn content and was preserved")
			}
			if err := githook.Apply(proposed); err != nil {
				return err
			}
			if proposed.Action == githook.ActionRemove {
				fmt.Fprintln(streams.Out, "Removed the Conn prepare-commit-msg hook.")
			}
			return nil
		},
	}
	cmd.AddCommand(preview, install, remove)
	return cmd
}

// gitHookExecutable resolves the running conn executable for the hook.
func gitHookExecutable(deps Dependencies, diagnostics io.Writer) (string, error) {
	resolve := deps.Executable
	if resolve == nil {
		resolve = os.Executable
	}
	executable, err := resolve()
	if err != nil || executable == "" {
		return "", errors.New("the conn executable path is unavailable; pass --command-on-path to run conn from PATH")
	}
	if resolved, err := filepath.EvalSymlinks(executable); err == nil {
		executable = resolved
	}
	if temp, err := filepath.EvalSymlinks(os.TempDir()); err == nil {
		if relative, err := filepath.Rel(temp, executable); err == nil && !strings.HasPrefix(relative, "..") {
			fmt.Fprintln(diagnostics, "Note: this conn executable is in a temporary directory, which go run deletes. Build conn first or pass --command-on-path.")
		}
	}
	return executable, nil
}
