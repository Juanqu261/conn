package application

import (
	"context"
	"errors"

	"uuid"

	"github.com/Juanqu261/conn/internal/protocol"
)

// AttachEvidence records a bounded, reference-only evidence item against an
// active Intent owned by any Participant of the caller's Human. The calling
// Participant is recorded as producer. It does not accept producer or human
// identity, and it never stores the evidence artifact itself.
func (p *Projects) AttachEvidence(ctx context.Context, actor, projectID, participantID uuid.UUID, input protocol.EvidenceAttachmentInput) (Result[protocol.EvidenceReference], error) {
	if err := validateRecord(input); err != nil {
		return Result[protocol.EvidenceReference]{}, err
	}
	if err := validateObservation(input); err != nil {
		return Result[protocol.EvidenceReference]{}, err
	}
	now := p.now()
	evidence := protocol.EvidenceReference{
		EvidenceID: uuid.NewV7(), ProjectID: projectID, IntentID: &input.IntentID,
		ObjectiveID: input.ObjectiveID, ObjectiveRevision: input.ObjectiveRevision,
		AcceptanceKey: input.AcceptanceKey, Kind: input.Kind, State: input.State,
		Locator: input.Locator, ContentHash: input.ContentHash, Summary: input.Summary,
		ProducerParticipantID: &participantID, Observation: input.Observation, ObservedAt: protocol.NewTimestamp(now),
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
		if intent.Status != "active" {
			return ErrForbidden
		}
		owner, err := tx.ParticipantByID(ctx, projectID, intent.IntentRevision.ParticipantID)
		if err != nil {
			return err
		}
		if owner.HumanID != actor {
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

// validateObservation keeps an observed run's state consistent with its
// recorded outcome: passing exactly when the command exited 0 in time.
func validateObservation(input protocol.EvidenceAttachmentInput) error {
	observation := input.Observation
	if observation == nil {
		return nil
	}
	want := protocol.EvidenceFailing
	if observation.ExitCode == 0 && !observation.TimedOut {
		want = protocol.EvidencePassing
	}
	if input.State != want {
		return &InvalidError{Pointer: "/state", Message: "An observed check run is passing only when it exited 0 without timing out, and failing otherwise."}
	}
	return nil
}

// IntentEvidenceAs lists one Intent's most recent evidence references, newest
// first, to an authorized reader.
func (p *Projects) IntentEvidenceAs(ctx context.Context, reader Reader, projectID, intentID uuid.UUID) (Result[protocol.EvidenceReferenceList], error) {
	var result Result[protocol.EvidenceReferenceList]
	err := p.Store.Read(ctx, func(tx Tx) error {
		if err := reader.authorize(ctx, tx, projectID); err != nil {
			return err
		}
		if _, err := tx.Intent(ctx, projectID, intentID); err != nil {
			return err
		}
		evidence, err := tx.IntentEvidenceReferences(ctx, projectID, intentID, protocol.MaxIntentEvidenceListed)
		if err != nil {
			return err
		}
		result.Data = protocol.EvidenceReferenceList{ProjectID: projectID, IntentID: intentID, Evidence: evidence}
		if err := validateRecord(result.Data); err != nil {
			return err
		}
		result.AsOfEvent, err = tx.LastEvent(ctx, projectID)
		return err
	})
	return result, err
}

// ResolutionDecision reads one immutable human decision to a Project member.
func (p *Projects) ResolutionDecision(ctx context.Context, actor, projectID, decisionID uuid.UUID) (Result[protocol.ResolutionDecision], error) {
	var result Result[protocol.ResolutionDecision]
	err := p.Store.Read(ctx, func(tx Tx) error {
		if _, err := tx.Role(ctx, projectID, actor); err != nil {
			return err
		}
		decision, err := tx.ResolutionDecision(ctx, projectID, decisionID)
		if err != nil {
			return err
		}
		result.Data = decision
		result.AsOfEvent, err = tx.LastEvent(ctx, projectID)
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
