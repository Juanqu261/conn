package application

import (
	"context"
	"errors"

	"uuid"

	"github.com/Juanqu261/conn/internal/protocol"
)

// AttachEvidence records a bounded, reference-only evidence item against the
// caller's own active Intent. It does not accept producer or human identity,
// and it never stores the evidence artifact itself.
func (p *Projects) AttachEvidence(ctx context.Context, actor, projectID, participantID uuid.UUID, input protocol.EvidenceAttachmentInput) (Result[protocol.EvidenceReference], error) {
	if err := validateRecord(input); err != nil {
		return Result[protocol.EvidenceReference]{}, err
	}
	now := p.now()
	evidence := protocol.EvidenceReference{
		EvidenceID: uuid.NewV7(), ProjectID: projectID, IntentID: &input.IntentID,
		ObjectiveID: input.ObjectiveID, ObjectiveRevision: input.ObjectiveRevision,
		AcceptanceKey: input.AcceptanceKey, Kind: input.Kind, State: input.State,
		Locator: input.Locator, ContentHash: input.ContentHash, Summary: input.Summary,
		ProducerParticipantID: &participantID, ObservedAt: protocol.NewTimestamp(now),
	}
	if err := validateRecord(evidence); err != nil {
		return Result[protocol.EvidenceReference]{}, err
	}

	var result Result[protocol.EvidenceReference]
	err := p.Store.Write(ctx, func(tx Tx) error {
		participant, err := tx.ParticipantByID(ctx, projectID, participantID)
		if err != nil {
			return err
		}
		if participant.HumanID != actor {
			return ErrForbidden
		}
		if _, err := tx.Role(ctx, projectID, actor); err != nil {
			return err
		}
		intent, err := tx.Intent(ctx, projectID, input.IntentID)
		if err != nil {
			return err
		}
		if intent.Status != "active" || intent.IntentRevision.ParticipantID != participantID {
			return ErrForbidden
		}
		if intent.IntentRevision.ObjectiveID != input.ObjectiveID || intent.IntentRevision.ObjectiveRevision != input.ObjectiveRevision {
			return &InvalidError{Pointer: "/objective_revision", Message: "Evidence must reference the active Intent's exact Objective revision."}
		}
		objective, err := tx.ObjectiveRevision(ctx, projectID, input.ObjectiveID, input.ObjectiveRevision)
		if errors.Is(err, ErrNotFound) {
			return &InvalidError{Pointer: "/objective_revision", Message: "The Objective revision does not exist in this project."}
		}
		if err != nil {
			return err
		}
		if input.AcceptanceKey != "" && !hasAcceptanceKey(objective, input.AcceptanceKey) {
			return &InvalidError{Pointer: "/acceptance_key", Message: "The acceptance key does not exist on the referenced Objective revision."}
		}
		if err := tx.InsertEvidenceReference(ctx, evidence); err != nil {
			return err
		}
		result.Data = evidence
		result.AsOfEvent, err = tx.AppendEvent(ctx, newEvent(projectID, evidence.EvidenceID, actor, now, protocol.EvidenceAttached{}))
		return err
	})
	return result, err
}

func hasAcceptanceKey(objective protocol.Objective, key string) bool {
	for _, point := range objective.KeyPoints {
		if point.Kind == protocol.KeyPointAcceptanceCriterion && point.Key == key {
			return true
		}
	}
	return false
}
