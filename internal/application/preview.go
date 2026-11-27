package application

import (
	"context"
	"errors"
	"fmt"

	"uuid"

	"github.com/Juanqu261/conn/internal/protocol"
)

// PreviewCollisions returns the collisions the assessor would open for
// candidate claims against active Intent revisions, without persisting
// anything or appending an event. It runs the same rule core as the
// persisted assessor inside one read-only snapshot. The candidates get an
// Intent identity that is never stored; with AsIntentID they stand for the
// next revision of that active Intent, so its current claims are excluded and
// decisions on its collisions can be inherited.
func (p *Projects) PreviewCollisions(ctx context.Context, reader Reader, projectID uuid.UUID, input protocol.CollisionPreviewInput) (Result[protocol.CollisionPreview], error) {
	if input.Claims == nil {
		input.Claims = []protocol.SemanticClaimCandidate{}
	}
	if err := validateRecord(input); err != nil {
		return Result[protocol.CollisionPreview]{}, err
	}
	var result Result[protocol.CollisionPreview]
	err := p.Store.Read(ctx, func(tx Tx) error {
		if err := reader.authorize(ctx, tx, projectID); err != nil {
			return err
		}
		excluded := make(map[uuid.UUID]bool, len(input.ExcludeIntentIDs)+1)
		for _, id := range input.ExcludeIntentIDs {
			excluded[id] = true
		}
		intentID, revision := uuid.NewV7(), 1
		if input.AsIntentID != nil {
			current, err := tx.Intent(ctx, projectID, *input.AsIntentID)
			if errors.Is(err, ErrNotFound) {
				return &InvalidError{Pointer: "/as_intent_id", Message: "as_intent_id does not name an Intent in this Project."}
			}
			if err != nil {
				return err
			}
			if current.Status != "active" {
				return &InvalidError{Pointer: "/as_intent_id", Message: "as_intent_id must name an active Intent."}
			}
			intentID, revision = *input.AsIntentID, current.IntentRevision.Revision+1
			excluded[intentID] = true
		}
		active, err := tx.ActiveSemanticClaims(ctx, projectID)
		if err != nil {
			return err
		}
		if len(active) > maxAssessorClaims {
			return &InvalidError{Message: fmt.Sprintf("The active claim set exceeds the bounded assessor limit of %d claims.", maxAssessorClaims)}
		}
		others := make([]protocol.SemanticClaim, 0, len(active))
		for _, claim := range active {
			if !excluded[claim.IntentID] {
				others = append(others, claim)
			}
		}
		at := protocol.NewTimestamp(p.now())
		changed := make([]protocol.SemanticClaim, 0, len(input.Claims))
		for _, candidate := range input.Claims {
			changed = append(changed, protocol.SemanticClaim{
				ClaimID: uuid.NewV7(), ProjectID: projectID, IntentID: intentID, IntentRevision: revision,
				SubjectKind: candidate.SubjectKind, NormalizedSubject: candidate.NormalizedSubject, DisplayName: candidate.DisplayName,
				Relation: candidate.Relation, ProvenanceKind: candidate.ProvenanceKind, Confidence: candidate.Confidence, ObservedAt: at,
			})
		}
		candidates, err := assessCandidates(changed, others, txDecisionIndex(ctx, tx, projectID))
		if err != nil {
			return err
		}
		preview := protocol.CollisionPreview{ProjectID: projectID, ClaimCount: len(input.Claims), Collisions: []protocol.PreviewedCollision{}}
		for _, candidate := range candidates {
			if len(preview.Collisions) == protocol.MaxPreviewCollisions {
				preview.Truncated = true
				break
			}
			assessment := candidate.assessment(projectID, at)
			counterpart := protocol.ReviewIntentRef{IntentID: candidate.rightIntentID, Revision: candidate.rightRevision}
			if candidate.rightIntentID == intentID {
				counterpart = protocol.ReviewIntentRef{IntentID: candidate.leftIntentID, Revision: candidate.leftRevision}
			}
			item := protocol.PreviewedCollision{
				RuleID: assessment.RuleID, Severity: assessment.Severity, State: assessment.State,
				MatchedSubjects: assessment.MatchedSubjects, Counterpart: counterpart,
			}
			if candidate.inherit(&assessment) {
				prior := candidate.inherited.CollisionID
				item.State, item.DecisionRef, item.InheritedFromCollisionID = assessment.State, assessment.DecisionRef, &prior
			}
			preview.Collisions = append(preview.Collisions, item)
		}
		if err := validateRecord(preview); err != nil {
			return err
		}
		result.Data = preview
		result.AsOfEvent, err = tx.LastEvent(ctx, projectID)
		return err
	})
	return result, err
}
