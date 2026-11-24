package cli

import (
	"fmt"
	"io"
	"strings"
	"uuid"

	"github.com/spf13/cobra"

	"github.com/Juanqu261/conn/internal/protocol"
)

func newObjectiveCommand(deps Dependencies, streams Streams) *cobra.Command {
	objective := group("objective", "Create, revise, and read objectives")

	create := &cobra.Command{
		Use:   "create",
		Short: "Create an objective in a project you own",
		Long: "Create revision 1 of an objective from an objective-revision-input JSON document.\n" +
			"Repeating the same file returns the existing revision.",
		Args: cobra.NoArgs,
	}
	rc := newRemote(create, deps, streams)
	var createProject uuid.UUID
	var createFile string
	requiredUUID(create, &createProject, "project", "project ID")
	requiredString(create, &createFile, "file", `objective-revision-input JSON document, or "-" for standard input`)
	create.RunE = func(cmd *cobra.Command, _ []string) error {
		input, err := readInput[protocol.ObjectiveRevisionInput](streams.In, createFile)
		if err != nil {
			return err
		}
		client, err := rc.client()
		if err != nil {
			return err
		}
		result, err := client.CreateObjective(cmd.Context(), createProject, input)
		if err != nil {
			return err
		}
		return printResult(rc, result, objectiveSaved)
	}

	revise := &cobra.Command{
		Use:   "revise",
		Short: "Append the next revision of an objective",
		Long: "Append the next immutable revision of an objective. --expected-revision is the latest\n" +
			"revision you read; if someone saved a newer one, the command fails with exit code 5.",
		Args: cobra.NoArgs,
	}
	rr := newRemote(revise, deps, streams)
	var reviseProject, reviseObjective uuid.UUID
	var expected int
	var reviseFile string
	requiredUUID(revise, &reviseProject, "project", "project ID")
	requiredUUID(revise, &reviseObjective, "objective", "objective ID")
	requiredInt(revise, &expected, "expected-revision", "latest revision you read")
	requiredString(revise, &reviseFile, "file", `objective-revision-input JSON document, or "-" for standard input`)
	revise.RunE = func(cmd *cobra.Command, _ []string) error {
		if err := positive("expected-revision", expected); err != nil {
			return err
		}
		input, err := readInput[protocol.ObjectiveRevisionInput](streams.In, reviseFile)
		if err != nil {
			return err
		}
		if input.ObjectiveID != reviseObjective {
			return usageError{fmt.Errorf("%s names objective %s, not --objective %s", reviseFile, input.ObjectiveID, reviseObjective)}
		}
		client, err := rr.client()
		if err != nil {
			return err
		}
		result, err := client.ReviseObjective(cmd.Context(), reviseProject, expected, input)
		if err != nil {
			return err
		}
		return printResult(rr, result, objectiveSaved)
	}

	importSpec := &cobra.Command{
		Use:   "import-kiro-spec",
		Short: "Import a local Kiro specification as an immutable Objective revision",
		Long: "Hash a bounded local Kiro specification and record only its safe relative reference, digest, and your reviewed Objective summary. " +
			"Conn never uploads or synchronizes the source. Refresh requires this command with --expected-revision. GitHub issue import remains deferred until GitHub App repository authorization exists.",
		Args: cobra.NoArgs,
	}
	ri := newRemote(importSpec, deps, streams)
	var importProject, importObjective, importAccountable uuid.UUID
	var importKey, importSource, importSourceRef, importSourceRevision, importTitle, importOverview string
	var importExpected int
	requiredUUID(importSpec, &importProject, "project", "project ID")
	requiredUUID(importSpec, &importObjective, "objective", "objective ID to create or revise")
	requiredUUID(importSpec, &importAccountable, "accountable-human", "Conn human ID accountable for the Objective")
	requiredString(importSpec, &importSource, "source", "local Kiro specification file to hash; it is never uploaded")
	requiredString(importSpec, &importSourceRef, "source-ref", "safe project-relative source reference stored with the Objective")
	requiredString(importSpec, &importTitle, "title", "reviewed Objective title")
	requiredString(importSpec, &importOverview, "overview", "reviewed Objective summary; source content is never uploaded")
	importSpec.Flags().IntVar(&importExpected, "expected-revision", 0, "current Objective revision for an explicit refresh; 0 creates revision 1")
	importSpec.Flags().StringVar(&importKey, "key", "", "optional stable Objective key")
	importSpec.Flags().StringVar(&importSourceRevision, "source-revision-ref", "", "optional opaque source revision reference")
	importSpec.RunE = func(cmd *cobra.Command, _ []string) error {
		if importExpected < 0 {
			return usageError{fmt.Errorf("--expected-revision cannot be negative")}
		}
		sourceRef, err := safeSourceReference(importSourceRef)
		if err != nil {
			return err
		}
		hash, err := readKiroSpecHash(importSource)
		if err != nil {
			return err
		}
		input := importKiroSpecInput(importObjective, importAccountable, importKey, sourceRef, importSourceRevision, hash, importTitle, importOverview)
		client, err := ri.client()
		if err != nil {
			return err
		}
		result, err := client.ImportKiroSpecObjective(cmd.Context(), importProject, importExpected, input)
		if err != nil {
			return err
		}
		return printResult(ri, result, objectiveSaved)
	}

	show := &cobra.Command{Use: "show", Short: "Show one objective revision", Args: cobra.NoArgs}
	rs := newRemote(show, deps, streams)
	var showProject, showObjective uuid.UUID
	var showRevision int
	requiredUUID(show, &showProject, "project", "project ID")
	requiredUUID(show, &showObjective, "objective", "objective ID")
	requiredInt(show, &showRevision, "revision", "revision number")
	show.RunE = func(cmd *cobra.Command, _ []string) error {
		if err := positive("revision", showRevision); err != nil {
			return err
		}
		client, err := rs.client()
		if err != nil {
			return err
		}
		result, err := client.ObjectiveRevision(cmd.Context(), showProject, showObjective, showRevision)
		if err != nil {
			return err
		}
		return printResult(rs, result, renderObjective)
	}

	objective.AddCommand(create, revise, importSpec, show)
	return objective
}

func objectiveSaved(out io.Writer, o protocol.Objective) {
	fmt.Fprintf(out, "Saved objective %s revision %d: %s\n", o.ObjectiveID, o.Revision, clean(o.Title))
}

func renderObjective(out io.Writer, o protocol.Objective) {
	fmt.Fprintf(out, "Objective %s revision %d\n", o.ObjectiveID, o.Revision)
	fmt.Fprintf(out, "Title:       %s\n", clean(o.Title))
	if o.Key != "" {
		fmt.Fprintf(out, "Key:         %s\n", o.Key)
	}
	fmt.Fprintf(out, "Accountable: %s\n", o.AccountableHumanID)
	fmt.Fprintf(out, "Created:     %s\n", o.CreatedAt)
	if len(o.KeyPoints) > 0 {
		fmt.Fprintln(out, "Key points:")
		for _, p := range o.KeyPoints {
			fmt.Fprintf(out, "  %s (%s): %s\n", p.Key, p.Kind, clean(p.Statement))
		}
	}
	if len(o.SourceRefs) > 0 {
		fmt.Fprintln(out, "Sources:")
		for _, s := range o.SourceRefs {
			fmt.Fprintf(out, "  %s %s\n", clean(s.URI), clean(s.Title))
		}
	}
	if p := o.ImportProvenance; p != nil {
		fmt.Fprintln(out, "Imported source:")
		fmt.Fprintf(out, "  %s %s\n", clean(p.SourceKind), clean(p.SourceRef))
		fmt.Fprintf(out, "  SHA-256 %s, imported %s\n", p.ContentHash, p.ImportedAt)
		if p.SourceRevisionRef != "" {
			fmt.Fprintf(out, "  Source revision: %s\n", clean(p.SourceRevisionRef))
		}
		fmt.Fprintln(out, "  External divergence: unknown. Conn does not read or synchronize the source automatically; source changes require an explicit refresh.")
	}
	fmt.Fprintf(out, "\n%s\n", indent(o.OverviewMarkdown, ""))
}

func newBlueprintCommand(deps Dependencies, streams Streams) *cobra.Command {
	blueprint := group("blueprint", "Propose, revise, and read blueprints")

	propose := &cobra.Command{
		Use:   "propose",
		Short: "Propose revision 1 of a blueprint in a project you own",
		Long: "Propose a blueprint from a blueprint-proposal JSON document. It stays proposed, and\n" +
			"unapproved, until every accountable human runs `conn charter accept`.",
		Args: cobra.NoArgs,
	}
	rp := newRemote(propose, deps, streams)
	var proposeProject uuid.UUID
	var proposeFile string
	requiredUUID(propose, &proposeProject, "project", "project ID")
	requiredString(propose, &proposeFile, "file", `blueprint-proposal JSON document, or "-" for standard input`)
	propose.RunE = func(cmd *cobra.Command, _ []string) error {
		input, err := readInput[protocol.BlueprintProposal](streams.In, proposeFile)
		if err != nil {
			return err
		}
		client, err := rp.client()
		if err != nil {
			return err
		}
		result, err := client.ProposeBlueprint(cmd.Context(), proposeProject, input)
		if err != nil {
			return err
		}
		return printResult(rp, result, blueprintSaved)
	}

	revise := &cobra.Command{
		Use:   "revise",
		Short: "Propose the next revision of a blueprint",
		Long: "Propose the next blueprint revision. The active revision stays in force until the new\n" +
			"one is accepted by every accountable human. --expected-revision is the latest revision\n" +
			"you read; if someone proposed a newer one, the command fails with exit code 5.",
		Args: cobra.NoArgs,
	}
	rr := newRemote(revise, deps, streams)
	var reviseProject, reviseBlueprint uuid.UUID
	var expected int
	var reviseFile string
	requiredUUID(revise, &reviseProject, "project", "project ID")
	requiredUUID(revise, &reviseBlueprint, "blueprint", "blueprint ID")
	requiredInt(revise, &expected, "expected-revision", "latest revision you read")
	requiredString(revise, &reviseFile, "file", `blueprint-proposal JSON document, or "-" for standard input`)
	revise.RunE = func(cmd *cobra.Command, _ []string) error {
		if err := positive("expected-revision", expected); err != nil {
			return err
		}
		input, err := readInput[protocol.BlueprintProposal](streams.In, reviseFile)
		if err != nil {
			return err
		}
		if input.BlueprintID != reviseBlueprint {
			return usageError{fmt.Errorf("%s names blueprint %s, not --blueprint %s", reviseFile, input.BlueprintID, reviseBlueprint)}
		}
		client, err := rr.client()
		if err != nil {
			return err
		}
		result, err := client.ReviseBlueprint(cmd.Context(), reviseProject, expected, input)
		if err != nil {
			return err
		}
		return printResult(rr, result, blueprintSaved)
	}

	show := &cobra.Command{Use: "show", Short: "Show one blueprint revision with every charter", Args: cobra.NoArgs}
	rs := newRemote(show, deps, streams)
	var showProject, showBlueprint uuid.UUID
	var showRevision int
	requiredUUID(show, &showProject, "project", "project ID")
	requiredUUID(show, &showBlueprint, "blueprint", "blueprint ID")
	requiredInt(show, &showRevision, "revision", "revision number")
	show.RunE = func(cmd *cobra.Command, _ []string) error {
		if err := positive("revision", showRevision); err != nil {
			return err
		}
		client, err := rs.client()
		if err != nil {
			return err
		}
		result, err := client.BlueprintRevision(cmd.Context(), showProject, showBlueprint, showRevision)
		if err != nil {
			return err
		}
		return printResult(rs, result, renderBlueprint)
	}

	blueprint.AddCommand(propose, revise, show)
	return blueprint
}

func blueprintSaved(out io.Writer, v protocol.BlueprintRevisionView) {
	b := v.BlueprintRevision
	fmt.Fprintf(out, "Blueprint %s revision %d is %s.\n", b.BlueprintID, b.Revision, approval(v.Status))
	if v.Status == protocol.BlueprintProposed {
		fmt.Fprintln(out, "Each accountable human must run `conn charter accept` for their own cell.")
	}
}

func renderBlueprint(out io.Writer, v protocol.BlueprintRevisionView) {
	b := v.BlueprintRevision
	fmt.Fprintf(out, "Blueprint %s revision %d: %s\n", b.BlueprintID, b.Revision, approval(v.Status))
	fmt.Fprintf(out, "Objective:   %s revision %d\n", b.ObjectiveID, b.ObjectiveRevision)
	fmt.Fprintf(out, "Proposed by: %s at %s\n", b.ProposedByHumanID, b.CreatedAt)
	for _, c := range b.CellCharters {
		fmt.Fprintf(out, "\nCell %s: %s\n", c.CellID, clean(c.Title))
		fmt.Fprintf(out, "  Accountable: %s\n", c.AccountableHumanID)
		if len(c.ObjectivePointRefs) > 0 {
			fmt.Fprintf(out, "  Objective points: %s\n", strings.Join(c.ObjectivePointRefs, ", "))
		}
		for _, p := range c.BoundaryPoints {
			fmt.Fprintf(out, "  %s %s: %s\n", p.Kind, p.Key, clean(p.Statement))
		}
		for _, d := range c.Dependencies {
			fmt.Fprintf(out, "  depends on %s: %s\n", d.CellID, clean(d.Summary))
		}
		fmt.Fprintf(out, "\n%s\n", indent(c.OverviewMarkdown, "  "))
	}
}

func newCharterCommand(deps Dependencies, streams Streams) *cobra.Command {
	charter := group("charter", "Accept or reject your own cell charter")
	charter.Long = "Accept or reject the charter you are accountable for in one blueprint revision.\n" +
		"Decisions are final for that revision; changes need a new blueprint revision."

	type target struct {
		project, blueprint, cell uuid.UUID
		revision                 int
	}
	targetFlags := func(cmd *cobra.Command, t *target) {
		requiredUUID(cmd, &t.project, "project", "project ID")
		requiredUUID(cmd, &t.blueprint, "blueprint", "blueprint ID")
		requiredInt(cmd, &t.revision, "revision", "blueprint revision number")
		requiredUUID(cmd, &t.cell, "cell", "your cell ID")
	}
	decided := func(out io.Writer, d protocol.CharterDecision) {
		verb := "Accepted"
		if d.Outcome == protocol.CharterReject {
			verb = "Rejected"
		}
		fmt.Fprintf(out, "%s your charter for cell %s in blueprint %s revision %d.\n", verb, d.CellID, d.BlueprintID, d.BlueprintRevision)
		fmt.Fprintln(out, "Run `conn status` to see whether the revision is active.")
	}

	accept := &cobra.Command{Use: "accept", Short: "Accept your charter", Args: cobra.NoArgs}
	ra := newRemote(accept, deps, streams)
	var acceptTarget target
	targetFlags(accept, &acceptTarget)
	accept.RunE = func(cmd *cobra.Command, _ []string) error {
		t := acceptTarget
		if err := positive("revision", t.revision); err != nil {
			return err
		}
		client, err := ra.client()
		if err != nil {
			return err
		}
		result, err := client.AcceptCharter(cmd.Context(), t.project, t.blueprint, t.revision, t.cell)
		if err != nil {
			return err
		}
		return printResult(ra, result, decided)
	}

	reject := &cobra.Command{Use: "reject", Short: "Reject your charter with a reason", Args: cobra.NoArgs}
	rj := newRemote(reject, deps, streams)
	var rejectTarget target
	var reason string
	targetFlags(reject, &rejectTarget)
	requiredString(reject, &reason, "reason", "why the charter does not work for you")
	reject.RunE = func(cmd *cobra.Command, _ []string) error {
		t := rejectTarget
		if err := positive("revision", t.revision); err != nil {
			return err
		}
		if strings.TrimSpace(reason) == "" {
			return usageError{fmt.Errorf("--reason cannot be blank")}
		}
		client, err := rj.client()
		if err != nil {
			return err
		}
		result, err := client.RejectCharter(cmd.Context(), t.project, t.blueprint, t.revision, t.cell, reason)
		if err != nil {
			return err
		}
		return printResult(rj, result, decided)
	}

	charter.AddCommand(accept, reject)
	return charter
}
