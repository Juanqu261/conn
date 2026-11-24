package cli

import (
	"fmt"
	"io"

	"uuid"

	"github.com/spf13/cobra"

	"github.com/Juanqu261/conn/internal/application"
	"github.com/Juanqu261/conn/internal/protocol"
)

func newConflictCommand(deps Dependencies, streams Streams) *cobra.Command {
	conflict := group("conflict", "Read context for an advisory collision")
	contextCmd := &cobra.Command{Use: "context", Short: "Render a portable collision context pack", Args: cobra.NoArgs}
	r := newRemote(contextCmd, deps, streams)
	var projectID, collisionID uuid.UUID
	var format string
	requiredUUID(contextCmd, &projectID, "project", "project ID")
	requiredUUID(contextCmd, &collisionID, "collision", "collision ID")
	contextCmd.Flags().StringVar(&format, "format", "markdown", "context format: json or markdown")
	contextCmd.RunE = func(cmd *cobra.Command, _ []string) error {
		if format != "json" && format != "markdown" {
			return usageError{fmt.Errorf("--format must be json or markdown")}
		}
		client, err := r.client()
		if err != nil {
			return err
		}
		result, err := client.ConflictContext(cmd.Context(), projectID, collisionID)
		if err != nil {
			return err
		}
		if format == "json" {
			r.json = true
			return printResult(r, result, func(io.Writer, protocol.ConflictContextPack) {})
		}
		_, err = fmt.Fprint(streams.Out, application.RenderConflictContextMarkdown(result.Data))
		return err
	}
	conflict.AddCommand(contextCmd)
	return conflict
}

func newResolutionCommand(deps Dependencies, streams Streams) *cobra.Command {
	resolution := group("resolution", "Record human decisions for advisory collisions")
	decide := &cobra.Command{Use: "decide", Short: "Append an immutable human resolution decision from JSON", Args: cobra.NoArgs}
	r := newRemote(decide, deps, streams)
	var projectID, collisionID uuid.UUID
	var file string
	requiredUUID(decide, &projectID, "project", "project ID")
	requiredUUID(decide, &collisionID, "collision", "collision ID")
	requiredString(decide, &file, "file", "resolution-decision-input JSON document, or - for standard input")
	decide.RunE = func(cmd *cobra.Command, _ []string) error {
		input, err := readInput[protocol.ResolutionDecisionInput](streams.In, file)
		if err != nil {
			return err
		}
		client, err := r.client()
		if err != nil {
			return err
		}
		result, err := client.DecideResolution(cmd.Context(), projectID, collisionID, input)
		if err != nil {
			return err
		}
		return printResult(r, result, func(out io.Writer, decision protocol.ResolutionDecision) {
			fmt.Fprintf(out, "Recorded %s resolution decision %s for collision %s.\n", decisionKindLabel(decision), decision.DecisionID, decision.CollisionID)
		})
	}
	resolution.AddCommand(decide)
	return resolution
}

func decisionKindLabel(decision protocol.ResolutionDecision) string {
	if decision.Binding {
		return "binding"
	}
	return "non-binding"
}
