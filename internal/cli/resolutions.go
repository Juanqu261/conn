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
	propose := &cobra.Command{Use: "propose", Short: "Append a non-binding resolution proposal from JSON as your CLI participant", Args: cobra.NoArgs}
	rp := newRemote(propose, deps, streams)
	var proposeProject, proposeCollision uuid.UUID
	var proposeFile string
	requiredUUID(propose, &proposeProject, "project", "project ID")
	requiredUUID(propose, &proposeCollision, "collision", "collision ID")
	requiredString(propose, &proposeFile, "file", "resolution-proposal-input JSON document, or - for standard input")
	propose.RunE = func(cmd *cobra.Command, _ []string) error {
		input, err := readInput[protocol.ResolutionProposalInput](streams.In, proposeFile)
		if err != nil {
			return err
		}
		client, err := rp.client()
		if err != nil {
			return err
		}
		result, err := client.ProposeResolution(cmd.Context(), proposeProject, proposeCollision, input)
		if err != nil {
			return err
		}
		return printResult(rp, result, func(out io.Writer, proposal protocol.ResolutionProposal) {
			fmt.Fprintf(out, "Recorded non-binding %s proposal %s for collision %s. Only a human decision can adopt it.\n", proposal.Kind, proposal.ProposalID, proposal.CollisionID)
		})
	}

	proposals := &cobra.Command{Use: "proposals", Short: "List non-binding proposals for one collision", Args: cobra.NoArgs}
	rl := newRemote(proposals, deps, streams)
	var listProject, listCollision uuid.UUID
	requiredUUID(proposals, &listProject, "project", "project ID")
	requiredUUID(proposals, &listCollision, "collision", "collision ID")
	proposals.RunE = func(cmd *cobra.Command, _ []string) error {
		client, err := rl.client()
		if err != nil {
			return err
		}
		result, err := client.ResolutionProposals(cmd.Context(), listProject, listCollision)
		if err != nil {
			return err
		}
		return printResult(rl, result, func(out io.Writer, list protocol.ResolutionProposalList) {
			if len(list.Proposals) == 0 {
				fmt.Fprintln(out, "No resolution proposals are recorded for this collision.")
			}
			for _, proposal := range list.Proposals {
				fmt.Fprintf(out, "%s  %s  non-binding  by %s  %s\n  %s\n", proposal.ProposalID, proposal.Kind, proposalAuthor(proposal), proposal.CreatedAt, clean(proposal.RationaleMarkdown))
			}
			fmt.Fprintf(out, "As of event %d.\n", result.AsOfEvent)
		})
	}

	resolution.AddCommand(decide, propose, proposals)
	return resolution
}

// proposalAuthor names the participant or human that authored a proposal.
func proposalAuthor(proposal protocol.ResolutionProposal) string {
	switch {
	case proposal.ProposedByParticipantID != nil:
		return "participant " + proposal.ProposedByParticipantID.String()
	case proposal.ProposedByHumanID != nil:
		return "human " + proposal.ProposedByHumanID.String()
	}
	return "unknown author"
}

func newEvidenceCommand(deps Dependencies, streams Streams) *cobra.Command {
	evidence := group("evidence", "Run reviewed checks and attach reference-only evidence to your Intents")
	attach := &cobra.Command{
		Use:   "attach",
		Short: "Attach reference-only evidence from JSON to an active Intent of yours",
		Long: "Attach an evidence-attachment-input document to an active Intent owned by any of your\n" +
			"Participants, recorded as your CLI participant. Conn stores the locator, state, optional\n" +
			"hash, and summary only, never logs or artifacts. Evidence without an observation is labelled\n" +
			"reported; use `conn evidence run` for observed check runs.",
		Args: cobra.NoArgs,
	}
	r := newRemote(attach, deps, streams)
	var projectID uuid.UUID
	var file string
	requiredUUID(attach, &projectID, "project", "project ID")
	requiredString(attach, &file, "file", "evidence-attachment-input JSON document, or - for standard input")
	attach.RunE = func(cmd *cobra.Command, _ []string) error {
		input, err := readInput[protocol.EvidenceAttachmentInput](streams.In, file)
		if err != nil {
			return err
		}
		client, err := r.client()
		if err != nil {
			return err
		}
		result, err := client.AttachEvidence(cmd.Context(), projectID, input.IntentID, input)
		if err != nil {
			return err
		}
		return printResult(r, result, func(out io.Writer, item protocol.EvidenceReference) {
			key := "no acceptance key"
			if item.AcceptanceKey != "" {
				key = "acceptance key " + item.AcceptanceKey
			}
			fmt.Fprintf(out, "Attached %s %s evidence %s to Intent %s (%s).\n", item.State, item.Kind, item.EvidenceID, input.IntentID, key)
		})
	}
	evidence.AddCommand(attach, newEvidenceRunCommand(deps, streams))
	return evidence
}

func decisionKindLabel(decision protocol.ResolutionDecision) string {
	if decision.Binding {
		return "binding"
	}
	return "non-binding"
}
