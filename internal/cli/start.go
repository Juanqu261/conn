package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"uuid"

	"github.com/spf13/cobra"

	"github.com/Juanqu261/conn/internal/analyzer"
	"github.com/Juanqu261/conn/internal/githubissue"
	"github.com/Juanqu261/conn/internal/hookstate"
	"github.com/Juanqu261/conn/internal/httpclient"
	"github.com/Juanqu261/conn/internal/preedit"
	"github.com/Juanqu261/conn/internal/protocol"
	"github.com/Juanqu261/conn/internal/speckit"
	"github.com/Juanqu261/conn/internal/workstart"
)

// githubIssueSource fetches and prepares one issue import. The token from
// GH_TOKEN or GITHUB_TOKEN stays in this process.
func githubIssueSource(ctx context.Context, deps Dependencies, reference githubissue.Reference, criteria bool) (githubissue.Import, error) {
	client := githubissue.Client{BaseURL: deps.GitHubAPIURL, Token: githubissue.TokenFromEnv(deps.LookupEnv)}
	issue, err := client.Fetch(ctx, reference)
	if err != nil {
		return githubissue.Import{}, err
	}
	return githubissue.Prepare(reference, issue, criteria)
}

// warnUnconnected warns when the issue repository is not connected.
func warnUnconnected(ctx context.Context, client *httpclient.Client, projectID uuid.UUID, reference githubissue.Reference, out io.Writer) {
	repositories, err := client.RepositoryConnections(ctx, projectID)
	if err != nil {
		return
	}
	for _, repository := range repositories.Data.Repositories {
		if strings.EqualFold(repository.CanonicalRemote, reference.Repository()) {
			return
		}
	}
	fmt.Fprintf(out, "Warning: %s is not a RepositoryConnection of this Project. The Objective will be imported, but Intents for this work need a connected repository.\n", reference.Repository())
}

func renderImport(out io.Writer, resolution workstart.Resolution) {
	input := resolution.Input
	switch resolution.Decision {
	case workstart.DecisionReuse:
		fmt.Fprintf(out, "%s was imported before as Objective %s revision %d with the same content; it is reused.\n", resolution.SourceLabel, resolution.Objective.ObjectiveID, resolution.Objective.Revision)
		return
	case workstart.DecisionRevise:
		fmt.Fprintf(out, "%s changed since Objective %s revision %d was imported; Conn proposes revision %d.\n", resolution.SourceLabel, resolution.Objective.ObjectiveID, resolution.Objective.Revision, resolution.ExpectedRevision+1)
	default:
		fmt.Fprintf(out, "Conn proposes importing %s as a new Objective.\n", resolution.SourceLabel)
	}
	fmt.Fprintf(out, "Title:  %s\nSource: %s %s", clean(input.Title), input.SourceKind, input.SourceRef)
	if input.SourceRevisionRef != "" {
		fmt.Fprintf(out, " (updated %s)", input.SourceRevisionRef)
	}
	fmt.Fprintf(out, "\nSHA-256 %s\nOverview:\n", input.ContentHash)
	lines := strings.Split(strings.TrimSpace(clean(input.OverviewMarkdown)), "\n")
	for i, line := range lines {
		if i == 20 {
			fmt.Fprintf(out, "  … %d more line(s)\n", len(lines)-20)
			break
		}
		fmt.Fprintf(out, "  %s\n", line)
	}
	if len(input.KeyPoints) == 0 {
		fmt.Fprintln(out, "Suggested acceptance criteria: none")
	} else {
		fmt.Fprintln(out, "Suggested acceptance criteria:")
	}
	for _, point := range input.KeyPoints {
		fmt.Fprintf(out, "  %s: %s\n", point.Key, clean(point.Statement))
	}
	for _, note := range resolution.Notes {
		fmt.Fprintln(out, "Note: "+note)
	}
}

// importResolution performs an import or revision and returns the stored
// Objective. Reuse returns the existing revision.
func importResolution(ctx context.Context, client *httpclient.Client, projectID uuid.UUID, resolution workstart.Resolution) (protocol.Objective, error) {
	if resolution.Decision == workstart.DecisionReuse || resolution.Decision == workstart.DecisionExisting {
		return *resolution.Objective, nil
	}
	var result httpclient.Result[protocol.Objective]
	var err error
	if resolution.Input.SourceKind == githubissue.SourceKind {
		result, err = client.ImportGitHubIssueObjective(ctx, projectID, resolution.ExpectedRevision, resolution.Input)
	} else {
		result, err = client.ImportKiroSpecObjective(ctx, projectID, resolution.ExpectedRevision, resolution.Input)
	}
	if httpclient.IsCode(err, "forbidden") {
		return protocol.Objective{}, hint(err, "only a Project owner may import an Objective; ask an owner to run this command or pass an existing Objective ID")
	}
	return result.Data, err
}

func newImportGitHubIssueCommand(deps Dependencies, streams Streams) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "import-github-issue <owner/name#number | https://github.com/owner/name/issues/number>",
		Short: "Import a GitHub issue as an Objective with provenance",
		Long: "Fetch one issue from the GitHub REST API and import it as an Objective with source kind\n" +
			"github_issue, the issue update time as source revision, and a content hash. GH_TOKEN or\n" +
			"GITHUB_TOKEN is used only when set, only for the GitHub request, and is never persisted or sent\n" +
			"to Conn. Pull requests are refused. A body over 32,768 characters is refused.\n\n" +
			"Suggested acceptance criteria come from list items under the first heading that contains\n" +
			"\"acceptance criteria\". Review them before confirming; --yes confirms without a prompt.",
		Args: cobra.ExactArgs(1),
	}
	r := newRemote(cmd, deps, streams)
	var projectID, accountable uuid.UUID
	var key, criteria string
	var yes bool
	cmd.Flags().Var(uuidValue{&projectID}, "project", "project ID (default: CONN_PROJECT or the matching Project)")
	cmd.Flags().Var(uuidValue{&accountable}, "accountable", "Conn human ID accountable for the Objective (default: you)")
	cmd.Flags().StringVar(&key, "key", "", "optional stable Objective key")
	cmd.Flags().StringVar(&criteria, "criteria", "suggested", "acceptance criteria: suggested or none")
	cmd.Flags().BoolVar(&yes, "yes", false, "import without a confirmation prompt")
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		if criteria != "suggested" && criteria != "none" {
			return usageError{errors.New("--criteria must be suggested or none")}
		}
		reference, err := githubissue.ParseReference(args[0])
		if err != nil {
			return usageError{err}
		}
		if !yes && !interactive(streams.In) {
			return usageError{errors.New("review the import interactively or pass --yes")}
		}
		prepared, err := githubIssueSource(cmd.Context(), deps, reference, criteria == "suggested")
		if err != nil {
			if errors.Is(err, githubissue.ErrPullRequest) || errors.Is(err, githubissue.ErrTooLong) {
				return usageError{err}
			}
			return err
		}
		client, err := r.client()
		if err != nil {
			return err
		}
		if projectID, err = resolveCLIProject(cmd.Context(), deps, client, projectID, "."); err != nil {
			return err
		}
		input := prepared.Input
		input.Key = key
		if input.AccountableHumanID = accountable; accountable == (uuid.UUID{}) {
			me, err := client.Me(cmd.Context())
			if err != nil {
				return err
			}
			input.AccountableHumanID = me.HumanID
		}
		resolution, err := workstart.Resolve(cmd.Context(), client, projectID, input, "GitHub issue "+reference.String())
		if err != nil {
			return err
		}
		resolution.Notes = prepared.Notes
		info := streams.Err
		warnUnconnected(cmd.Context(), client, projectID, reference, info)
		renderImport(info, resolution)
		if resolution.Decision != workstart.DecisionReuse && !yes {
			confirmed, err := newPrompter(streams).confirm("Import this Objective?")
			if err != nil {
				return err
			}
			if !confirmed {
				return errors.New("nothing was imported")
			}
		}
		objective, err := importResolution(cmd.Context(), client, projectID, resolution)
		if err != nil {
			return err
		}
		return printResult(r, httpclient.Result[protocol.Objective]{Data: objective}, objectiveSaved)
	}
	return cmd
}

func newStartCommand(deps Dependencies, streams Streams) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "start <speckit-feature-dir | kiro-spec-path | owner/name#number | issue-url | objective-id>",
		Short: "Bring an Objective into Conn and preview what your planned change collides with",
		Long: "Resolve the Project, then resolve or import the Objective: an Objective ID is read directly; a\n" +
			"Spec Kit feature, Kiro spec, or GitHub issue imported before with the same content is reused, a\n" +
			"changed one gets a new revision after confirmation, and a new one is imported after\n" +
			"confirmation. Then suggest claims for the planned paths, preview their collisions without\n" +
			"publishing, and choose:\n\n" +
			"  P  publish a CLI Intent with the suggested claims and Git context, and record the local focus\n" +
			"  K  print the /conn-start invocation to continue in Kiro, publishing nothing\n" +
			"  Q  quit\n\n" +
			"A Spec Kit feature is a directory with spec.md, or that spec.md. Its FR, SC, clarification, and\n" +
			"constitution lines become key points, and its planned paths come from tasks.md or plan.md for\n" +
			"you to confirm. When you already have an active Intent for the feature, P revises it.\n\n" +
			"Non-interactive runs require --publish or --import-only.",
		Args: cobra.ExactArgs(1),
	}
	r := newRemote(cmd, deps, streams)
	var projectID, accountable uuid.UUID
	var paths []string
	var publish, importOnly bool
	var key, worktree string
	cmd.Flags().Var(uuidValue{&projectID}, "project", "project ID (default: focus, CONN_PROJECT, sole Project, or the matching Project)")
	cmd.Flags().Var(uuidValue{&accountable}, "accountable", "Conn human ID accountable for an imported Objective (default: you)")
	cmd.Flags().StringSliceVar(&paths, "paths", nil, "planned repository-relative paths (repeatable or comma-separated)")
	cmd.Flags().BoolVar(&publish, "publish", false, "publish without prompting")
	cmd.Flags().BoolVar(&importOnly, "import-only", false, "resolve or import the Objective and stop")
	cmd.Flags().StringVar(&key, "objective-key", "", "optional stable key for an imported Objective")
	cmd.Flags().StringVar(&worktree, "worktree", ".", "local repository worktree")
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		if publish && importOnly {
			return usageError{errors.New("pass only one of --publish and --import-only")}
		}
		prompts := interactive(streams.In)
		if !prompts && !publish && !importOnly {
			return usageError{errors.New("input is not interactive; pass --publish or --import-only")}
		}
		root, err := resolveWorktree(ctx, worktree)
		if err != nil {
			return err
		}
		client, err := r.client()
		if err != nil {
			return err
		}
		if projectID, err = resolveCLIProject(ctx, deps, client, projectID, root); err != nil {
			return err
		}
		info, ask := streams.Err, newPrompter(streams)
		resolution, reference, feature, err := startResolution(ctx, deps, client, projectID, root, args[0], key, accountable)
		if err != nil {
			return err
		}
		if resolution.Decision != workstart.DecisionExisting {
			if reference != nil {
				warnUnconnected(ctx, client, projectID, *reference, info)
			}
			renderImport(info, resolution)
			if resolution.Decision != workstart.DecisionReuse && prompts && !publish && !importOnly {
				confirmed, err := ask.confirm("Import this Objective?")
				if err != nil {
					return err
				}
				if !confirmed {
					return errors.New("nothing was imported")
				}
			}
		}
		objective, err := importResolution(ctx, client, projectID, resolution)
		if err != nil {
			return err
		}
		label := resolution.SourceLabel
		if label == "" {
			label = "Objective " + objective.ObjectiveID.String()
		}
		if importOnly {
			fmt.Fprintf(streams.Out, "Objective: %s (%s, revision %d)\n", clean(objective.Title), label, objective.Revision)
			printContinue(streams.Out, objective)
			return nil
		}
		var own *protocol.IntentView
		if feature != nil {
			if len(paths) == 0 {
				if paths, err = confirmFeaturePaths(*feature, info, ask, prompts && !publish); err != nil {
					return err
				}
			}
			if own, err = ownFeatureIntent(ctx, client, projectID, objective.ObjectiveID, root); err != nil {
				return err
			}
		}
		if len(paths) == 0 && prompts {
			answer, err := ask.ask("Planned repository-relative paths (comma-separated, empty to skip): ")
			if err != nil {
				return err
			}
			for _, path := range strings.Split(answer, ",") {
				if path = strings.TrimSpace(path); path != "" {
					paths = append(paths, path)
				}
			}
		}
		var asIntent *uuid.UUID
		if own != nil {
			asIntent = &own.IntentRevision.IntentID
		}
		claims, candidates, preview, err := startClaims(ctx, deps, client, projectID, objective, root, paths, asIntent, info)
		if err != nil {
			return err
		}
		renderStart(streams.Out, objective, label, candidates, preview)
		action := "Publish Intent from this terminal"
		if own != nil {
			renderOwnDelta(streams.Out, *own, claims)
			action = "Revise your Intent from this terminal"
		}
		choice := "p"
		if !publish {
			if choice, err = ask.ask("[P] " + action + "   [K] Continue in Kiro   [Q] Quit: "); err != nil {
				return err
			}
			choice = strings.ToLower(choice)
		}
		switch choice {
		case "p":
			if own != nil {
				return reviseStart(ctx, client, projectID, objective, root, *own, claims, streams.Out)
			}
			return publishStart(ctx, client, projectID, objective, root, claims, streams.Out)
		case "k":
			printContinue(streams.Out, objective)
			return nil
		default:
			fmt.Fprintln(streams.Out, "Nothing was published.")
			return nil
		}
	}
	return cmd
}

// startResolution resolves the source argument into an Objective proposal.
// The detection order is an Objective ID, a GitHub issue reference, a Spec
// Kit feature, then a Kiro spec. feature is set for a Spec Kit source.
func startResolution(ctx context.Context, deps Dependencies, client *httpclient.Client, projectID uuid.UUID, root, source, key string, accountable uuid.UUID) (resolution workstart.Resolution, reference *githubissue.Reference, feature *speckit.Feature, err error) {
	if id, err := uuid.Parse(source); err == nil {
		objective, err := workstart.ReadObjective(ctx, client, projectID, id)
		if err != nil {
			return workstart.Resolution{}, nil, nil, err
		}
		return workstart.Resolution{Decision: workstart.DecisionExisting, Objective: &objective}, nil, nil, nil
	}
	var input protocol.ObjectiveImportInput
	var label string
	var notes []string
	if parsed, err := githubissue.ParseReference(source); err == nil {
		prepared, err := githubIssueSource(ctx, deps, parsed, true)
		if err != nil {
			if errors.Is(err, githubissue.ErrPullRequest) || errors.Is(err, githubissue.ErrTooLong) {
				return workstart.Resolution{}, nil, nil, usageError{err}
			}
			return workstart.Resolution{}, nil, nil, err
		}
		input, label, notes, reference = prepared.Input, parsed.SourceRef(), prepared.Notes, &parsed
	} else if speckit.IsFeaturePath(source) {
		var resolved speckit.Feature
		if input, resolved, notes, err = workstart.SpecKitInput(root, source); err != nil {
			return workstart.Resolution{}, nil, nil, usageError{err}
		}
		label, feature = "Spec Kit feature "+resolved.DirRef, &resolved
	} else if _, statErr := os.Stat(source); statErr == nil {
		if input, err = workstart.KiroSpecInput(root, source); err != nil {
			return workstart.Resolution{}, nil, nil, usageError{err}
		}
		label = "Kiro spec " + input.SourceRef
	} else {
		return workstart.Resolution{}, nil, nil, usageError{fmt.Errorf("%q is not an Objective ID, a GitHub issue reference, a Spec Kit feature directory, or an existing Kiro spec path", source)}
	}
	input.Key = key
	input.AccountableHumanID = accountable
	if accountable == (uuid.UUID{}) {
		me, err := client.Me(ctx)
		if err != nil {
			return workstart.Resolution{}, nil, nil, err
		}
		input.AccountableHumanID = me.HumanID
	}
	resolution, err = workstart.Resolve(ctx, client, projectID, input, label)
	resolution.Notes = notes
	return resolution, reference, feature, err
}

// confirmFeaturePaths extracts a Spec Kit feature's planned paths, shows
// them, and lets the human remove some before claims are suggested.
func confirmFeaturePaths(feature speckit.Feature, info io.Writer, ask *prompter, prompts bool) ([]string, error) {
	extraction, err := specKitExtraction(feature, nil)
	if err != nil {
		return nil, usageError{fmt.Errorf("extract the planned paths: %w", err)}
	}
	renderExtraction(info, extraction)
	paths := extraction.Paths
	if prompts && len(paths) > 0 {
		answer, err := ask.ask("Paths to remove before claims are suggested (comma-separated, empty keeps all): ")
		if err != nil {
			return nil, err
		}
		remove := map[string]bool{}
		for _, path := range strings.Split(answer, ",") {
			if path = filepath.ToSlash(strings.TrimSpace(path)); path != "" {
				remove[path] = true
			}
		}
		kept := make([]string, 0, len(paths))
		for _, path := range paths {
			if !remove[path] {
				kept = append(kept, path)
			}
		}
		paths = kept
	}
	if len(paths) > analyzer.MaxPlannedPaths {
		fmt.Fprintf(info, "Claims are suggested for the first %d of %d planned paths; pass --paths to choose others.\n", analyzer.MaxPlannedPaths, len(paths))
		paths = paths[:analyzer.MaxPlannedPaths]
	}
	return paths, nil
}

// renderOwnDelta shows the claims a revision adds and drops before the
// human chooses to revise.
func renderOwnDelta(out io.Writer, own protocol.IntentView, claims []protocol.SemanticClaimCandidate) {
	intent := own.IntentRevision
	added, dropped := claimDelta(intent.DeclaredClaims, claims)
	fmt.Fprintf(out, "You already have the Intent %q, revision %d, for this feature. Revising it adds %d claim(s) and drops %d:\n", clean(summaryTitle(intent.SummaryMarkdown)), intent.Revision, len(added), len(dropped))
	for _, claim := range added {
		fmt.Fprintf(out, "  + %s  %s\n", clean(claim.NormalizedSubject), claim.Relation)
	}
	for _, claim := range dropped {
		fmt.Fprintf(out, "  - %s  %s\n", clean(claim.NormalizedSubject), claim.Relation)
	}
}

// startClaims suggests declared claims for the planned paths and previews
// their collisions. Catalog matches are shown but not declared.
func startClaims(ctx context.Context, deps Dependencies, client *httpclient.Client, projectID uuid.UUID, objective protocol.Objective, root string, paths []string, asIntent *uuid.UUID, info io.Writer) ([]protocol.SemanticClaimCandidate, []preedit.Candidate, *protocol.CollisionPreview, error) {
	claims := []protocol.SemanticClaimCandidate{}
	var candidates []preedit.Candidate
	if len(paths) > 0 {
		location := locateAnalyzer(deps)
		suggestions, err := preedit.SuggestClaims(ctx, preedit.SuggestDeps{Client: client, Analyzer: location.Config, AnalyzerAvailable: location.Available()},
			preedit.SuggestRequest{ProjectID: projectID, ObjectiveID: objective.ObjectiveID, Worktree: root, Paths: paths})
		if err != nil {
			return nil, nil, nil, usageError{err}
		}
		if !suggestions.Analysis.Available {
			fmt.Fprintf(info, "Planned analysis unavailable: %s\n", suggestions.Analysis.Reason)
		}
		for _, candidate := range suggestions.Candidates {
			if candidate.Source == preedit.SourceCatalog {
				continue
			}
			candidates = append(candidates, candidate)
			claims = append(claims, candidate.Claim)
		}
	}
	preview, err := client.PreviewCollisions(ctx, projectID, protocol.CollisionPreviewInput{Claims: claims, AsIntentID: asIntent})
	if err != nil {
		return nil, nil, nil, err
	}
	return claims, candidates, &preview.Data, nil
}

func renderStart(out io.Writer, objective protocol.Objective, label string, candidates []preedit.Candidate, preview *protocol.CollisionPreview) {
	fmt.Fprintf(out, "Objective: %s (%s, revision %d)\n", clean(objective.Title), label, objective.Revision)
	fmt.Fprintln(out, "Concepts:")
	if len(candidates) == 0 {
		fmt.Fprintln(out, "  none suggested; pass --paths to suggest claims for planned files")
	}
	width := 0
	for _, candidate := range candidates {
		width = max(width, len(candidate.Claim.NormalizedSubject))
	}
	for _, candidate := range candidates {
		fmt.Fprintf(out, "  %-*s   %-8s  (%s)\n", width, candidate.Claim.NormalizedSubject, candidate.Claim.Relation, candidate.Source)
	}
	if preview == nil || len(preview.Collisions) == 0 {
		fmt.Fprintln(out, "Previewed collisions: none")
	} else {
		severities := map[string]bool{}
		for _, collision := range preview.Collisions {
			severities[strings.ToUpper(collision.Severity)] = true
		}
		names := make([]string, 0, len(severities))
		for name := range severities {
			names = append(names, name)
		}
		sort.Strings(names)
		fmt.Fprintf(out, "Previewed collisions: %s\n", strings.Join(names, ", "))
		for _, collision := range preview.Collisions {
			fmt.Fprintf(out, "  %s with Intent %s@%d on %s\n", collision.RuleID, collision.Counterpart.IntentID.String()[:8], collision.Counterpart.Revision, strings.Join(collision.MatchedSubjects, ", "))
		}
	}
}

func printContinue(out io.Writer, objective protocol.Objective) {
	fmt.Fprintf(out, "Continue in Kiro with: /conn-start %s\n", objective.ObjectiveID)
	fmt.Fprintf(out, "In another MCP client, run the conn-start prompt with focus %s.\n", objective.ObjectiveID)
}

// publishStart publishes a CLI Intent with the claims and Git context and
// records the local focus for hooks and commit trailers.
func publishStart(ctx context.Context, client *httpclient.Client, projectID uuid.UUID, objective protocol.Objective, root string, claims []protocol.SemanticClaimCandidate, out io.Writer) error {
	summary := "Work on " + objective.Title
	if runes := []rune(summary); len(runes) > 4000 {
		summary = string(runes[:4000])
	}
	input := protocol.IntentInput{
		IntentID: uuid.NewV7(), ObjectiveID: objective.ObjectiveID, ObjectiveRevision: objective.Revision,
		SummaryMarkdown: summary, DeclaredClaims: claims,
	}
	git := preedit.FillGitContext(ctx, client, projectID, root, nil, &input)
	result, err := client.PublishIntent(ctx, projectID, input)
	if err != nil {
		return err
	}
	intent := result.Data.IntentRevision
	fmt.Fprintf(out, "Published Intent %s revision %d with %d declared claim(s) as your CLI participant.\n", intent.IntentID, intent.Revision, len(intent.DeclaredClaims))
	for _, field := range git.Filled {
		fmt.Fprintf(out, "Filled %s=%s from the worktree.\n", field.Field, field.Value)
	}
	for _, note := range git.Notes {
		fmt.Fprintln(out, note)
	}
	recordStartFocus(ctx, client, projectID, root, intent, result.AsOfEvent)
	fmt.Fprintf(out, "Commit with the trailer %q.\n", protocol.CommitTrailer(intent.IntentID, intent.Revision))
	return nil
}

// reviseStart appends a revision with the new claims to the caller's own
// Intent for the feature, keeping the rest of its content.
func reviseStart(ctx context.Context, client *httpclient.Client, projectID uuid.UUID, objective protocol.Objective, root string, own protocol.IntentView, claims []protocol.SemanticClaimCandidate, out io.Writer) error {
	current := own.IntentRevision
	input := protocol.IntentInput{
		IntentID: current.IntentID, ObjectiveID: objective.ObjectiveID, ObjectiveRevision: objective.Revision,
		BlueprintID: current.BlueprintID, BlueprintRevision: current.BlueprintRevision, CharterID: current.CharterID,
		RepositoryID: current.RepositoryID, BaseRevisionRef: current.BaseRevisionRef, HeadRevisionRef: current.HeadRevisionRef,
		BranchRef: current.BranchRef, SummaryMarkdown: current.SummaryMarkdown, Assumptions: current.Assumptions, DeclaredClaims: claims,
	}
	if input.RepositoryID == nil {
		preedit.FillGitContext(ctx, client, projectID, root, nil, &input)
	}
	result, err := client.ReviseIntent(ctx, projectID, current.IntentID, current.Revision, input)
	if httpclient.IsCode(err, "forbidden") {
		return hint(err, "this Intent belongs to another of your participants, such as your coding agent; revise it there with revise_intent")
	}
	if err != nil {
		return err
	}
	intent := result.Data.IntentRevision
	fmt.Fprintf(out, "Revised Intent %q to revision %d with %d declared claim(s) as your CLI participant.\n", clean(summaryTitle(intent.SummaryMarkdown)), intent.Revision, len(intent.DeclaredClaims))
	recordStartFocus(ctx, client, projectID, root, intent, result.AsOfEvent)
	fmt.Fprintf(out, "Commit with the trailer %q.\n", protocol.CommitTrailer(intent.IntentID, intent.Revision))
	return nil
}

// recordStartFocus records the local focus for hooks and commit trailers.
func recordStartFocus(ctx context.Context, client *httpclient.Client, projectID uuid.UUID, root string, intent protocol.IntentRevision, asOfEvent int64) {
	if intent.RepositoryID == nil {
		return
	}
	if store, err := hookstate.Open(ctx, root); err == nil {
		project, repository, intentID, participant := projectID, *intent.RepositoryID, intent.IntentID, intent.ParticipantID
		_ = store.UpdateFocus(func(focus *hookstate.Focus) {
			focus.ServerURL, focus.ProjectID, focus.RepositoryID = client.URL(), &project, &repository
			focus.IntentID, focus.IntentRevision, focus.ParticipantID = &intentID, intent.Revision, &participant
			focus.KnownCollisions, focus.KnownPairs, focus.LastEvent = map[string]string{}, map[string]string{}, asOfEvent
		})
	}
}
