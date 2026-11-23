package cli

import (
	"fmt"
	"io"
	"strings"

	"uuid"

	"github.com/spf13/cobra"

	"github.com/Juanqu261/conn/internal/protocol"
)

func newRadarCommand(deps Dependencies, streams Streams) *cobra.Command {
	cmd := &cobra.Command{Use: "radar", Short: "Read the project coordination Radar", Args: cobra.NoArgs}
	r := newRemote(cmd, deps, streams)
	var projectID uuid.UUID
	requiredUUID(cmd, &projectID, "project", "project ID")
	cmd.RunE = func(cmd *cobra.Command, _ []string) error {
		client, err := r.client()
		if err != nil {
			return err
		}
		result, err := client.Radar(cmd.Context(), projectID)
		if err != nil {
			return err
		}
		return printResult(r, result, printRadar)
	}
	return cmd
}

func newConflictsCommand(deps Dependencies, streams Streams) *cobra.Command {
	cmd := &cobra.Command{Use: "conflicts", Short: "List open advisory collision assessments", Args: cobra.NoArgs}
	r := newRemote(cmd, deps, streams)
	var projectID uuid.UUID
	requiredUUID(cmd, &projectID, "project", "project ID")
	cmd.RunE = func(cmd *cobra.Command, _ []string) error {
		client, err := r.client()
		if err != nil {
			return err
		}
		result, err := client.Radar(cmd.Context(), projectID)
		if err != nil {
			return err
		}
		return printResult(r, result, printConflicts)
	}
	return cmd
}

func newExplainCommand(deps Dependencies, streams Streams) *cobra.Command {
	cmd := &cobra.Command{Use: "explain", Short: "Explain one open advisory collision", Args: cobra.NoArgs}
	r := newRemote(cmd, deps, streams)
	var projectID, collisionID uuid.UUID
	requiredUUID(cmd, &projectID, "project", "project ID")
	requiredUUID(cmd, &collisionID, "collision", "open collision ID")
	cmd.RunE = func(cmd *cobra.Command, _ []string) error {
		client, err := r.client()
		if err != nil {
			return err
		}
		result, err := client.Radar(cmd.Context(), projectID)
		if err != nil {
			return err
		}
		for _, collision := range result.Data.OpenCollisions {
			if collision.CollisionID == collisionID {
				return printResult(r, result, func(out io.Writer, _ protocol.RadarView) { printCollision(out, collision, result.Data) })
			}
		}
		return fmt.Errorf("open collision %s is not available at event %d", collisionID, result.AsOfEvent)
	}
	return cmd
}

func printRadar(out io.Writer, view protocol.RadarView) {
	fmt.Fprintf(out, "Radar for %s at event %d\n", clean(view.Project.Name), view.AsOfEvent)
	if len(view.Objectives) == 0 {
		fmt.Fprintln(out, "Objectives: absent")
	} else {
		fmt.Fprintln(out, "Objectives:")
		for _, objective := range view.Objectives {
			fmt.Fprintf(out, "  %s revision %d: %s\n", objective.ObjectiveID, objective.Revision, clean(objective.Title))
		}
	}
	if view.ActiveBlueprint == nil {
		fmt.Fprintln(out, "Active blueprint: absent")
	} else {
		fmt.Fprintf(out, "Active blueprint: %s revision %d\n", view.ActiveBlueprint.BlueprintID, view.ActiveBlueprint.Revision)
	}
	fmt.Fprintf(out, "Repositories: %d\nActive Intents: %d\nSemantic claims: %d across %d concepts\nOpen collisions: %d\nDependencies: %d\nDecisions: %d\n", len(view.Repositories), len(view.ActiveIntents), len(view.SemanticClaims), len(view.SemanticConcepts), len(view.OpenCollisions), len(view.Dependencies), len(view.DecisionRefs))
	if len(view.EvidenceCoverage) == 0 {
		fmt.Fprintln(out, "Evidence coverage: absent (no keyed acceptance criteria)")
	} else {
		fmt.Fprintln(out, "Evidence coverage:")
		for _, coverage := range view.EvidenceCoverage {
			fmt.Fprintf(out, "  %s: %s (%d evidence references)\n", clean(coverage.AcceptanceKey), coverage.State, len(coverage.EvidenceRefs))
		}
	}
	printStatements(out, "Open questions", view.OpenQuestions)
	printStatements(out, "Assumptions", view.Assumptions)
	printStatements(out, "Known unknowns", view.KnownUnknowns)
}

func printConflicts(out io.Writer, view protocol.RadarView) {
	if len(view.OpenCollisions) == 0 {
		fmt.Fprintln(out, "No open collision assessments. This does not prove that the project has no conflicts.")
	} else {
		for _, collision := range view.OpenCollisions {
			fmt.Fprintf(out, "%s  %s  %s  confidence %.2f\n", collision.CollisionID, collision.Severity, clean(collision.RuleID), collision.Confidence)
		}
	}
	printStatements(out, "Known unknowns", view.KnownUnknowns)
	fmt.Fprintf(out, "As of event %d.\n", view.AsOfEvent)
}

func printCollision(out io.Writer, collision protocol.CollisionAssessment, view protocol.RadarView) {
	fmt.Fprintf(out, "Collision %s\nRule: %s\nStatus: %s (advisory)\nSeverity: %s\nConfidence: %.2f\n", collision.CollisionID, clean(collision.RuleID), collision.State, collision.Severity, collision.Confidence)
	fmt.Fprintf(out, "Intents: %s revision %d and %s revision %d\nMatching claims: %s\n\n%s\n", collision.LeftIntentID, collision.LeftIntentRevision, collision.RightIntentID, collision.RightIntentRevision, joinUUIDs(collision.MatchingClaimRefs), indent(collision.ExplanationMarkdown, "  "))
	printStatements(out, "Known unknowns", view.KnownUnknowns)
	fmt.Fprintf(out, "As of event %d.\n", view.AsOfEvent)
}

func printStatements(out io.Writer, title string, values []string) {
	if len(values) == 0 {
		return
	}
	fmt.Fprintf(out, "%s:\n", title)
	for _, value := range values {
		fmt.Fprintf(out, "  - %s\n", clean(value))
	}
}

func joinUUIDs(values []uuid.UUID) string {
	items := make([]string, len(values))
	for i, value := range values {
		items[i] = value.String()
	}
	return strings.Join(items, ", ")
}
