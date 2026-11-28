package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"

	"uuid"

	"github.com/spf13/cobra"

	"github.com/Juanqu261/conn/internal/analyzer"
	"github.com/Juanqu261/conn/internal/hookstate"
	"github.com/Juanqu261/conn/internal/httpclient"
	"github.com/Juanqu261/conn/internal/preedit"
	"github.com/Juanqu261/conn/internal/protocol"
	"github.com/Juanqu261/conn/internal/repositorygit"
	"github.com/Juanqu261/conn/internal/speckit"
	"github.com/Juanqu261/conn/internal/speckitext"
	"github.com/Juanqu261/conn/internal/workstart"
)

const maxCheckErrors = 20

func newSpecKitCommand(deps Dependencies, streams Streams) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "speckit",
		Short: "Coordinate GitHub Spec Kit features with teammates' in-progress work",
		Long: "Read a local Spec Kit feature (specs/<NNN>-<name>/) to check its plan against active\n" +
			"Intents before any code exists, and export the Conn Spec Kit extension.\n\n" +
			"Conn reads spec.md, tasks.md, plan.md, and the constitution locally and sends Conn only\n" +
			"confirmed Objective content and claims derived from paths. It never installs or runs\n" +
			"Spec Kit, never executes a command named in a Spec Kit file, and never writes into\n" +
			".specify/. Import a feature as an Objective with `conn start <feature-dir>`.",
		Args: cobra.NoArgs,
	}
	cmd.AddCommand(newSpecKitCheckCommand(deps, streams), newSpecKitExtensionCommand(streams))
	return cmd
}

func newSpecKitCheckCommand(deps Dependencies, streams Streams) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "check [<feature-dir>]",
		Short: "Preview the collisions a feature's planned paths would open, publishing nothing",
		Long: "Extract the planned paths from the feature's tasks.md, or from the source-structure block\n" +
			"of plan.md, suggest claims for them, and preview the collisions those claims would open\n" +
			"against active Intents. Nothing is imported or published.\n\n" +
			"The feature is the argument, then SPECIFY_FEATURE_DIRECTORY, then feature_directory in\n" +
			".specify/feature.json, then specs/<current-branch>/. When you already have an active\n" +
			"Intent for the feature's Objective, it is excluded from the preview and the claims the\n" +
			"task list adds or drops are listed.\n\n" +
			"The command always exits 0, so a Spec Kit hook never stops: findings and Conn, network,\n" +
			"or configuration errors are printed, and --format json lists errors in the document.",
		Args: cobra.MaximumNArgs(1),
	}
	server := serverFlag(cmd, deps)
	var projectID uuid.UUID
	var paths []string
	var format, worktree string
	cmd.Flags().Var(uuidValue{&projectID}, "project", "project ID (default: focus, CONN_PROJECT, sole Project, or the matching Project)")
	cmd.Flags().StringSliceVar(&paths, "paths", nil, "planned repository-relative paths to use instead of extracting them")
	cmd.Flags().StringVar(&format, "format", "text", "output format: text or json")
	cmd.Flags().StringVar(&worktree, "worktree", ".", "local repository worktree")
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		argument := ""
		if len(args) == 1 {
			argument = args[0]
		}
		check := runSpecKitCheck(cmd.Context(), deps, server(), specKitCheckRequest{
			projectID: projectID, feature: argument, paths: paths, worktree: worktree,
		})
		if format == "json" {
			data, err := protocol.Encode(check)
			if err != nil {
				fmt.Fprintf(streams.Err, "conn speckit check: %v\n", err)
				return nil
			}
			fmt.Fprintf(streams.Out, "%s\n", data)
			return nil
		}
		if format != "text" {
			fmt.Fprintln(streams.Err, "conn speckit check: --format must be text or json; printing text")
		}
		renderSpecKitCheck(streams.Out, check)
		for _, message := range check.Errors {
			fmt.Fprintf(streams.Err, "conn speckit check: %s\n", message)
		}
		return nil
	}
	return cmd
}

type specKitCheckRequest struct {
	projectID uuid.UUID
	feature   string
	paths     []string
	worktree  string
}

// runSpecKitCheck never fails: every error is recorded in the result, and
// the local extraction is reported even when Conn is unreachable.
func runSpecKitCheck(ctx context.Context, deps Dependencies, serverURL string, request specKitCheckRequest) protocol.SpecKitCheck {
	check := protocol.SpecKitCheck{
		Feature:    protocol.SpecKitFeatureRef{Directory: ".", SpecRef: speckit.SpecFile, Title: "Spec Kit feature"},
		Extraction: emptyExtraction(),
		Candidates: []protocol.SemanticClaimCandidate{}, Collisions: []protocol.SpecKitCollision{}, Errors: []string{},
	}
	fail := func(prefix string, err error) protocol.SpecKitCheck {
		addCheckError(&check, prefix+explain(err).Error())
		return check
	}
	root, err := resolveWorktree(ctx, request.worktree)
	if err != nil {
		return fail("the worktree is not a Git repository: ", err)
	}
	name := request.feature
	if name == "" {
		branch, _ := repositorygit.CurrentBranch(ctx, root)
		if name, err = speckit.CurrentFeature(root, deps.LookupEnv, branch); err != nil {
			return fail("the current Spec Kit feature could not be read: ", err)
		}
		if name == "" {
			return fail("", errors.New("no Spec Kit feature was named or found; pass the feature directory, such as specs/001-feature-name"))
		}
	}
	if !filepath.IsAbs(name) && !speckit.IsFeaturePath(name) {
		// A relative feature directory names a path inside the worktree.
		name = filepath.Join(root, name)
	}
	feature, err := speckit.Resolve(root, name)
	if err != nil {
		return fail("", err)
	}
	spec, err := speckit.ReadFile(feature.SpecPath())
	if err != nil {
		return fail("read spec.md: ", err)
	}
	check.Feature = protocol.SpecKitFeatureRef{Directory: feature.DirRef, SpecRef: feature.SpecRef, Title: speckit.Title(spec, feature.Name())}
	extraction, err := specKitExtraction(feature, request.paths)
	check.Extraction = extraction
	if err != nil {
		addCheckError(&check, "the planned paths could not be extracted: "+err.Error())
	}

	service, err := deps.Account(serverURL)
	if err != nil {
		return fail("Conn is not configured: ", err)
	}
	client, err := service.Client()
	if err != nil {
		return fail("Conn is not available: ", err)
	}
	projectID, err := resolveCLIProject(ctx, deps, client, request.projectID, root)
	if err != nil {
		return fail("the Project could not be resolved: ", err)
	}
	check.ProjectID = &projectID
	objective, err := findImportedFeature(ctx, client, projectID, feature)
	if err != nil {
		return fail("the feature's Objective could not be read: ", err)
	}
	var objectiveID uuid.UUID
	if objective != nil {
		objectiveID = objective.ObjectiveID
		check.Objective = &protocol.SpecKitObjective{
			ObjectiveID: objective.ObjectiveID, Revision: objective.Revision, Title: objective.Title,
			ContentChanged: objective.ImportProvenance.ContentHash != speckit.Digest(spec),
		}
	}
	var own *protocol.IntentView
	if objective != nil {
		if own, err = ownFeatureIntent(ctx, client, projectID, objective.ObjectiveID, root); err != nil {
			return fail("your Intents could not be read: ", err)
		}
	}

	claims, analysis, err := specKitClaims(ctx, deps, client, projectID, objectiveID, root, extraction.Paths)
	check.Analysis = analysis
	if err != nil {
		return fail("claims could not be suggested: ", err)
	}
	check.Candidates, check.CandidateCount = claims, len(claims)
	input := protocol.CollisionPreviewInput{Claims: claims}
	if own != nil {
		intent := own.IntentRevision
		input.AsIntentID = &intent.IntentID
		check.OwnIntent = &protocol.SpecKitIntentRef{IntentID: intent.IntentID, Revision: intent.Revision, Summary: summaryTitle(intent.SummaryMarkdown)}
		added, dropped := claimDelta(intent.DeclaredClaims, claims)
		check.Delta = &protocol.SpecKitDelta{Added: added, Dropped: dropped}
	}
	preview, err := client.PreviewCollisions(ctx, projectID, input)
	if err != nil {
		return fail("collisions could not be previewed: ", err)
	}
	check.Collisions, check.CollisionsTruncated = nameCollisions(ctx, client, projectID, preview.Data.Collisions), preview.Data.Truncated
	check.PublishOffered = len(check.Collisions) == 0 && own == nil
	return check
}

func emptyExtraction() protocol.SpecKitExtraction {
	return protocol.SpecKitExtraction{Source: protocol.SpecKitSourceNone, Paths: []string{}, NewPaths: []string{}, Unresolved: []string{}, TasksWithoutPath: []string{}}
}

// specKitExtraction extracts the planned paths, or uses explicit ones.
func specKitExtraction(feature speckit.Feature, explicit []string) (protocol.SpecKitExtraction, error) {
	if len(explicit) > 0 {
		extraction := emptyExtraction()
		extraction.Source = protocol.SpecKitSourcePathsFlag
		seen := map[string]bool{}
		for _, path := range explicit {
			path = filepath.ToSlash(strings.TrimSpace(path))
			if path == "" || seen[path] {
				continue
			}
			if len(extraction.Paths) == speckit.MaxPaths {
				extraction.Truncated = true
				break
			}
			seen[path] = true
			extraction.Paths = append(extraction.Paths, path)
		}
		return extraction, nil
	}
	found, err := speckit.PlannedPaths(feature)
	return protocol.SpecKitExtraction{
		Source: found.Source, Paths: found.Paths, NewPaths: found.NewPaths, Unresolved: found.Unresolved,
		TasksWithoutPath: found.TasksWithoutPath, Truncated: found.Truncated,
	}, err
}

// findImportedFeature returns the Objective imported from the feature's
// spec.md, without importing anything.
func findImportedFeature(ctx context.Context, client *httpclient.Client, projectID uuid.UUID, feature speckit.Feature) (*protocol.Objective, error) {
	return workstart.FindImported(ctx, client, projectID, speckit.SourceKind, feature.SpecRef)
}

// ownFeatureIntent returns the caller's active Intent for the Objective,
// preferring the Intent focused in this worktree, then the newest.
func ownFeatureIntent(ctx context.Context, client *httpclient.Client, projectID, objectiveID uuid.UUID, root string) (*protocol.IntentView, error) {
	me, err := client.Me(ctx)
	if err != nil {
		return nil, err
	}
	intents, err := client.Intents(ctx, projectID)
	if err != nil {
		return nil, err
	}
	var focused uuid.UUID
	if store, err := hookstate.Open(ctx, root); err == nil {
		if focus := store.Focus(); focus.IntentID != nil {
			focused = *focus.IntentID
		}
	}
	var found *protocol.IntentView
	for i := range intents.Data.Intents {
		view := &intents.Data.Intents[i]
		if view.Status != "active" || view.IntentRevision.ObjectiveID != objectiveID || view.OwnerHumanID == nil || *view.OwnerHumanID != me.HumanID {
			continue
		}
		if view.IntentRevision.IntentID == focused {
			return view, nil
		}
		if found == nil || view.IntentRevision.IntentID.String() > found.IntentRevision.IntentID.String() {
			found = view
		}
	}
	return found, nil
}

// specKitClaims suggests declared claims for the planned paths. Catalog
// matches are left out, as in conn start. Planned analysis accepts at most
// analyzer.MaxPlannedPaths paths; the rest are reported.
func specKitClaims(ctx context.Context, deps Dependencies, client *httpclient.Client, projectID, objectiveID uuid.UUID, root string, paths []string) ([]protocol.SemanticClaimCandidate, protocol.SpecKitAnalysis, error) {
	claims := []protocol.SemanticClaimCandidate{}
	if len(paths) == 0 {
		return claims, protocol.SpecKitAnalysis{Reason: "The feature names no planned paths, so no claims were suggested."}, nil
	}
	analyzed := paths
	if len(analyzed) > analyzer.MaxPlannedPaths {
		analyzed = analyzed[:analyzer.MaxPlannedPaths]
	}
	location := locateAnalyzer(deps)
	suggestions, err := preedit.SuggestClaims(ctx, preedit.SuggestDeps{Client: client, Analyzer: location.Config, AnalyzerAvailable: location.Available()},
		preedit.SuggestRequest{ProjectID: projectID, ObjectiveID: objectiveID, Worktree: root, Paths: analyzed})
	if err != nil {
		return claims, protocol.SpecKitAnalysis{}, err
	}
	analysis := protocol.SpecKitAnalysis{Available: suggestions.Analysis.Available, Reason: suggestions.Analysis.Reason}
	if len(paths) > len(analyzed) {
		analysis.Reason = strings.TrimSpace(analysis.Reason + fmt.Sprintf(" Planned analysis covers the first %d of %d paths; remove paths to analyze the rest.", len(analyzed), len(paths)))
	}
	for _, candidate := range suggestions.Candidates {
		if candidate.Source != preedit.SourceCatalog {
			claims = append(claims, candidate.Claim)
		}
	}
	return claims, analysis, nil
}

func claimKey(subject, relation string) string { return subject + "\x00" + relation }

// claimDelta lists the claims the task list adds to, and drops from, the
// Intent's current revision.
func claimDelta(current, planned []protocol.SemanticClaimCandidate) (added, dropped []protocol.SpecKitClaimRef) {
	added, dropped = []protocol.SpecKitClaimRef{}, []protocol.SpecKitClaimRef{}
	have, want := map[string]bool{}, map[string]bool{}
	for _, claim := range current {
		have[claimKey(claim.NormalizedSubject, claim.Relation)] = true
	}
	for _, claim := range planned {
		key := claimKey(claim.NormalizedSubject, claim.Relation)
		if !have[key] && !want[key] {
			added = append(added, protocol.SpecKitClaimRef{NormalizedSubject: claim.NormalizedSubject, Relation: claim.Relation})
		}
		want[key] = true
	}
	for _, claim := range current {
		key := claimKey(claim.NormalizedSubject, claim.Relation)
		if !want[key] {
			want[key] = true
			dropped = append(dropped, protocol.SpecKitClaimRef{NormalizedSubject: claim.NormalizedSubject, Relation: claim.Relation})
		}
	}
	return added, dropped
}

// nameCollisions names each counterpart by its summary and Objective title.
// A counterpart the caller cannot read keeps only its reference.
func nameCollisions(ctx context.Context, client *httpclient.Client, projectID uuid.UUID, previewed []protocol.PreviewedCollision) []protocol.SpecKitCollision {
	collisions := make([]protocol.SpecKitCollision, 0, len(previewed))
	intents := map[uuid.UUID]*protocol.IntentRevision{}
	objectives := map[uuid.UUID]string{}
	for _, collision := range previewed {
		counterpart := protocol.SpecKitCounterpartRef{IntentID: collision.Counterpart.IntentID, Revision: collision.Counterpart.Revision}
		intent, known := intents[collision.Counterpart.IntentID]
		if !known {
			if view, err := client.Intent(ctx, projectID, collision.Counterpart.IntentID); err == nil {
				intent = &view.Data.IntentRevision
			}
			intents[collision.Counterpart.IntentID] = intent
		}
		if intent != nil {
			counterpart.Summary = summaryTitle(intent.SummaryMarkdown)
			title, known := objectives[intent.ObjectiveID]
			if !known {
				if objective, err := client.ObjectiveRevision(ctx, projectID, intent.ObjectiveID, intent.ObjectiveRevision); err == nil {
					title = objective.Data.Title
				}
				objectives[intent.ObjectiveID] = title
			}
			counterpart.ObjectiveTitle = title
		}
		collisions = append(collisions, protocol.SpecKitCollision{
			RuleID: collision.RuleID, Severity: collision.Severity, State: collision.State,
			MatchedSubjects: collision.MatchedSubjects, Counterpart: counterpart, DecisionRef: collision.DecisionRef,
		})
	}
	return collisions
}

// summaryTitle is the first non-empty line of an Intent summary, bounded to
// a title, without Markdown heading or emphasis markers.
func summaryTitle(summary string) string {
	for _, line := range strings.Split(summary, "\n") {
		line = strings.Join(strings.Fields(strings.Trim(strings.TrimSpace(line), "#*_ ")), " ")
		if line == "" {
			continue
		}
		if utf8.RuneCountInString(line) > 200 {
			line = string([]rune(line)[:199]) + "…"
		}
		return line
	}
	return "Untitled Intent"
}

func addCheckError(check *protocol.SpecKitCheck, message string) {
	message = strings.Join(strings.Fields(message), " ")
	if message == "" || len(check.Errors) == maxCheckErrors {
		return
	}
	if utf8.RuneCountInString(message) > 1000 {
		message = string([]rune(message)[:999]) + "…"
	}
	check.Errors = append(check.Errors, message)
}

func renderSpecKitCheck(out io.Writer, check protocol.SpecKitCheck) {
	fmt.Fprintf(out, "Spec Kit feature: %s (%s)\n", clean(check.Feature.Title), check.Feature.Directory)
	switch {
	case check.Objective != nil:
		fmt.Fprintf(out, "Objective: %s, revision %d\n", clean(check.Objective.Title), check.Objective.Revision)
		if check.Objective.ContentChanged {
			fmt.Fprintf(out, "  spec.md changed since this revision was imported; conn start %s offers a new revision.\n", check.Feature.Directory)
		}
	case check.ProjectID != nil:
		fmt.Fprintf(out, "Objective: not imported yet; conn start %s imports it after you confirm it.\n", check.Feature.Directory)
	}
	renderExtraction(out, check.Extraction)
	if check.ProjectID == nil {
		fmt.Fprintln(out, "Nothing was published.")
		return
	}
	fmt.Fprintf(out, "Suggested claims: %d\n", check.CandidateCount)
	width := 0
	for _, claim := range check.Candidates {
		width = max(width, len(claim.NormalizedSubject))
	}
	for _, claim := range check.Candidates {
		fmt.Fprintf(out, "  %-*s  %s\n", width, clean(claim.NormalizedSubject), claim.Relation)
	}
	if !check.Analysis.Available && check.Analysis.Reason != "" {
		fmt.Fprintf(out, "Planned analysis unavailable: %s\n", clean(check.Analysis.Reason))
	} else if check.Analysis.Reason != "" {
		fmt.Fprintln(out, clean(check.Analysis.Reason))
	}
	if own := check.OwnIntent; own != nil {
		fmt.Fprintf(out, "Your Intent %q, revision %d, is this feature's Intent; it is excluded from the preview.\n", clean(own.Summary), own.Revision)
		if delta := check.Delta; delta != nil {
			if len(delta.Added) == 0 && len(delta.Dropped) == 0 {
				fmt.Fprintln(out, "The task list matches its claims.")
			} else {
				fmt.Fprintf(out, "The task list adds %d claim(s) and drops %d relative to revision %d:\n", len(delta.Added), len(delta.Dropped), own.Revision)
				for _, claim := range delta.Added {
					fmt.Fprintf(out, "  + %s  %s\n", clean(claim.NormalizedSubject), claim.Relation)
				}
				for _, claim := range delta.Dropped {
					fmt.Fprintf(out, "  - %s  %s\n", clean(claim.NormalizedSubject), claim.Relation)
				}
			}
		}
	}
	renderNamedCollisions(out, check.Collisions, check.CollisionsTruncated)
	if check.PublishOffered && len(check.Errors) == 0 {
		fmt.Fprintf(out, "Publish this feature's Intent now so that later plans collide with it: conn start %s\n", check.Feature.Directory)
	}
	fmt.Fprintln(out, "Nothing was published.")
}

// renderExtraction lists the planned paths, which are new files, the
// path-shaped tokens that did not resolve, and the tasks without a path.
func renderExtraction(out io.Writer, extraction protocol.SpecKitExtraction) {
	source := map[string]string{
		protocol.SpecKitSourceTasks: "tasks.md", protocol.SpecKitSourcePlan: "the plan.md source structure", protocol.SpecKitSourcePathsFlag: "--paths",
	}[extraction.Source]
	if source == "" {
		fmt.Fprintln(out, "Planned paths: none; the feature has no tasks.md and no plan.md source-structure block. Pass --paths to name them.")
	} else {
		fmt.Fprintf(out, "Planned paths from %s: %d (%d new)\n", source, len(extraction.Paths), len(extraction.NewPaths))
		newPaths := map[string]bool{}
		for _, path := range extraction.NewPaths {
			newPaths[path] = true
		}
		for _, path := range extraction.Paths {
			if newPaths[path] {
				fmt.Fprintf(out, "  %s  (new file)\n", clean(path))
			} else {
				fmt.Fprintf(out, "  %s\n", clean(path))
			}
		}
		if extraction.Truncated {
			fmt.Fprintf(out, "  … only the first %d paths are listed\n", speckit.MaxPaths)
		}
	}
	if len(extraction.Unresolved) > 0 {
		fmt.Fprintf(out, "Not a path here (the directory does not exist): %s\n", clean(strings.Join(extraction.Unresolved, ", ")))
	}
	if len(extraction.TasksWithoutPath) > 0 {
		fmt.Fprintf(out, "Tasks that name no path: %s\n", strings.Join(extraction.TasksWithoutPath, ", "))
	}
}

func renderNamedCollisions(out io.Writer, collisions []protocol.SpecKitCollision, truncated bool) {
	if len(collisions) == 0 {
		fmt.Fprintln(out, "Previewed collisions: none")
		return
	}
	severities := map[string]bool{}
	for _, collision := range collisions {
		severities[strings.ToUpper(collision.Severity)] = true
	}
	names := make([]string, 0, len(severities))
	for name := range severities {
		names = append(names, name)
	}
	sort.Strings(names)
	fmt.Fprintf(out, "Previewed collisions: %d (%s). They are advisory; nothing is blocked.\n", len(collisions), strings.Join(names, ", "))
	for _, collision := range collisions {
		who := "another Intent"
		if collision.Counterpart.Summary != "" {
			who = fmt.Sprintf("%q", clean(collision.Counterpart.Summary))
		}
		if collision.Counterpart.ObjectiveTitle != "" {
			who += " (Objective: " + clean(collision.Counterpart.ObjectiveTitle) + ")"
		}
		fmt.Fprintf(out, "  %-5s %s with %s on %s\n", strings.ToUpper(collision.Severity), strings.ReplaceAll(collision.RuleID, "_", " "), who, clean(strings.Join(collision.MatchedSubjects, ", ")))
		if collision.State != "open" {
			fmt.Fprintf(out, "        inherits a %s decision from the earlier collision\n", collision.State)
		}
	}
	if truncated {
		fmt.Fprintln(out, "  … more collisions exist than the preview lists")
	}
}

func newSpecKitExtensionCommand(streams Streams) *cobra.Command {
	extension := &cobra.Command{
		Use:   "extension",
		Short: "Render the Conn Spec Kit extension",
		Long: "Render the Conn extension for GitHub Spec Kit: extension.yml with the after_tasks,\n" +
			"before_implement, and after_implement hooks, and the speckit.conn.check, start, finish,\n" +
			"and review commands. Install it with Spec Kit's own command; Conn never edits\n" +
			".specify/extensions.yml or any other Spec Kit file.",
		Args: cobra.NoArgs,
	}
	export := &cobra.Command{
		Use:   "export <dir>",
		Short: "Write the extension package into a folder you name",
		Long: "Write extension.yml and commands/ into <dir>. A file that already holds different content\n" +
			"is a conflict and nothing is written; remove it, or choose another folder, to export\n" +
			"another version. Export never deletes a file.",
		Args: cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			changes, err := speckitext.Export(args[0])
			fmt.Fprintf(streams.Out, "Conn Spec Kit extension %s export to %s:\n", speckitext.Version, filepath.ToSlash(args[0]))
			for _, change := range changes {
				fmt.Fprintf(streams.Out, "  %-9s %s\n", change.Action, change.RelativePath)
			}
			if err != nil {
				return err
			}
			fmt.Fprintf(streams.Out, "Install it in your Spec Kit project with: specify extension add --dev %s\n", filepath.ToSlash(args[0]))
			return nil
		},
	}
	extension.AddCommand(export)
	return extension
}
