package application

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"uuid"

	"github.com/Juanqu261/conn/internal/protocol"
)

// PublishIntent creates revision 1 under a participant that must belong to
// the authenticated human. Participant selection is an adapter concern; this
// use case verifies it again inside the write transaction.
func (p *Projects) PublishIntent(ctx context.Context, actor, projectID, participantID uuid.UUID, input protocol.IntentInput) (Result[protocol.IntentView], error) {
	return p.writeIntent(ctx, actor, projectID, participantID, input, 0, true)
}

// ReviseIntent appends an immutable revision after expectedRevision. A stale
// writer never overwrites the snapshot that won the race.
func (p *Projects) ReviseIntent(ctx context.Context, actor, projectID, participantID uuid.UUID, expectedRevision int, input protocol.IntentInput) (Result[protocol.IntentView], error) {
	if expectedRevision < 1 {
		return Result[protocol.IntentView]{}, &InvalidError{Message: "The expected revision must be positive."}
	}
	return p.writeIntent(ctx, actor, projectID, participantID, input, expectedRevision, false)
}

func (p *Projects) writeIntent(ctx context.Context, actor, projectID, participantID uuid.UUID, input protocol.IntentInput, expectedRevision int, publish bool) (Result[protocol.IntentView], error) {
	now := p.now()
	revision := protocol.IntentRevision{
		IntentID: input.IntentID, ProjectID: projectID, ParticipantID: participantID,
		Revision: expectedRevision + 1, ObjectiveID: input.ObjectiveID, ObjectiveRevision: input.ObjectiveRevision,
		BlueprintID: input.BlueprintID, BlueprintRevision: input.BlueprintRevision, CharterID: input.CharterID,
		RepositoryID: input.RepositoryID, BaseRevisionRef: input.BaseRevisionRef, HeadRevisionRef: input.HeadRevisionRef,
		BranchRef: input.BranchRef, SummaryMarkdown: input.SummaryMarkdown, Assumptions: input.Assumptions,
		DeclaredClaims: input.DeclaredClaims, CreatedAt: protocol.NewTimestamp(now),
	}
	if err := validateRecord(revision); err != nil {
		return Result[protocol.IntentView]{}, err
	}

	var result Result[protocol.IntentView]
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
		if err := validateIntentReferences(ctx, tx, participant, revision); err != nil {
			return err
		}

		existing, existingErr := tx.Intent(ctx, projectID, input.IntentID)
		if publish {
			if existingErr == nil {
				if existing.IntentRevision.ParticipantID != participantID {
					return ErrForbidden
				}
				if existing.IntentRevision.Revision == 1 && sameIntentContent(existing.IntentRevision, revision) {
					if existing.AdvisoryObservations, err = intentObservationsWithAdvice(ctx, tx, existing.IntentRevision); err != nil {
						return err
					}
					result.Data = existing
					result.AsOfEvent, err = tx.LastEvent(ctx, projectID)
					return err
				}
				return ErrConflict
			}
			if !errors.Is(existingErr, ErrNotFound) {
				return existingErr
			}
		} else {
			if existingErr != nil {
				return existingErr
			}
			if existing.IntentRevision.ParticipantID != participantID {
				return ErrForbidden
			}
			if existing.Status != "active" || existing.IntentRevision.Revision != expectedRevision {
				return ErrStaleRevision
			}
		}

		inserted, err := tx.InsertIntentRevision(ctx, revision)
		if err != nil {
			return err
		}
		if !inserted {
			current, loadErr := tx.Intent(ctx, projectID, input.IntentID)
			if loadErr == nil && current.IntentRevision.ParticipantID == participantID && current.IntentRevision.Revision == revision.Revision && sameIntentContent(current.IntentRevision, revision) {
				if current.AdvisoryObservations, err = intentObservationsWithAdvice(ctx, tx, current.IntentRevision); err != nil {
					return err
				}
				result.Data = current
				result.AsOfEvent, err = tx.LastEvent(ctx, projectID)
				return err
			}
			return ErrStaleRevision
		}
		claims, err := persistDeclaredClaims(ctx, tx, revision)
		if err != nil {
			return err
		}
		if !publish {
			if err := p.staleIntentCollisions(ctx, tx, actor, projectID, revision.IntentID, expectedRevision, revision.CreatedAt); err != nil {
				return err
			}
		}
		observations, err := intentObservationsWithAdvice(ctx, tx, revision)
		if err != nil {
			return err
		}
		owner := actor
		result.Data = protocol.IntentView{IntentRevision: revision, Status: "active", OwnerHumanID: &owner, AdvisoryObservations: observations}
		payload := protocol.EventPayload(protocol.IntentPublished{Revision: revision.Revision})
		if !publish {
			payload = protocol.IntentRevised{Revision: revision.Revision}
		}
		if _, err := tx.AppendEvent(ctx, newEvent(projectID, input.IntentID, actor, now, payload)); err != nil {
			return err
		}
		if err := p.appendClaimEvents(ctx, tx, actor, claims, revision.CreatedAt); err != nil {
			return err
		}
		if err := p.assessActiveIntentCollisions(ctx, tx, actor, revision, revision.CreatedAt); err != nil {
			return err
		}
		result.AsOfEvent, err = tx.LastEvent(ctx, projectID)
		return err
	})
	return result, err
}

// ReleaseIntent closes a current Intent without removing any immutable
// revision. A repeated release is idempotent; a concurrent revision conflicts.
func (p *Projects) ReleaseIntent(ctx context.Context, actor, projectID, participantID, intentID uuid.UUID) (Result[protocol.IntentView], error) {
	var result Result[protocol.IntentView]
	now := p.now()
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
		current, err := tx.Intent(ctx, projectID, intentID)
		if err != nil {
			return err
		}
		if current.IntentRevision.ParticipantID != participantID {
			return ErrForbidden
		}
		if current.Status == "released" {
			result.Data = current
			result.AsOfEvent, err = tx.LastEvent(ctx, projectID)
			return err
		}
		if current.Status != "active" {
			return ErrStaleRevision
		}
		changed, err := tx.ReleaseIntent(ctx, projectID, intentID, participantID, current.IntentRevision.Revision)
		if err != nil {
			return err
		}
		if !changed {
			return ErrStaleRevision
		}
		current.Status = "released"
		result.Data = current
		if _, err := tx.AppendEvent(ctx, newEvent(projectID, intentID, actor, now, protocol.IntentReleased{Revision: current.IntentRevision.Revision})); err != nil {
			return err
		}
		if err := p.staleIntentCollisions(ctx, tx, actor, projectID, intentID, current.IntentRevision.Revision, protocol.NewTimestamp(now)); err != nil {
			return err
		}
		result.AsOfEvent, err = tx.LastEvent(ctx, projectID)
		return err
	})
	return result, err
}

// IntentAs reads one current snapshot to an authorized reader.
func (p *Projects) IntentAs(ctx context.Context, reader Reader, projectID, intentID uuid.UUID) (Result[protocol.IntentView], error) {
	var result Result[protocol.IntentView]
	err := p.Store.Read(ctx, func(tx Tx) error {
		if err := reader.authorize(ctx, tx, projectID); err != nil {
			return err
		}
		var err error
		if result.Data, err = tx.Intent(ctx, projectID, intentID); err != nil {
			return err
		}
		if result.Data.AdvisoryObservations, err = intentObservationsWithAdvice(ctx, tx, result.Data.IntentRevision); err != nil {
			return err
		}
		result.AsOfEvent, err = tx.LastEvent(ctx, projectID)
		return err
	})
	return result, err
}

// IntentRevisionAs returns one exact immutable revision of an Intent with the
// Intent's current lifecycle state, so a reader can review earlier revisions.
func (p *Projects) IntentRevisionAs(ctx context.Context, reader Reader, projectID, intentID uuid.UUID, revision int) (Result[protocol.IntentView], error) {
	var result Result[protocol.IntentView]
	err := p.Store.Read(ctx, func(tx Tx) error {
		if err := reader.authorize(ctx, tx, projectID); err != nil {
			return err
		}
		current, err := tx.Intent(ctx, projectID, intentID)
		if err != nil {
			return err
		}
		if current.IntentRevision.Revision != revision {
			if current.IntentRevision, err = tx.IntentRevision(ctx, projectID, intentID, revision); err != nil {
				return err
			}
		}
		result.Data = current
		if result.Data.AdvisoryObservations, err = intentObservationsWithAdvice(ctx, tx, result.Data.IntentRevision); err != nil {
			return err
		}
		result.AsOfEvent, err = tx.LastEvent(ctx, projectID)
		return err
	})
	return result, err
}

// IntentsAs lists active and released current snapshots to an authorized reader.
func (p *Projects) IntentsAs(ctx context.Context, reader Reader, projectID uuid.UUID) (Result[protocol.IntentList], error) {
	var result Result[protocol.IntentList]
	err := p.Store.Read(ctx, func(tx Tx) error {
		if err := reader.authorize(ctx, tx, projectID); err != nil {
			return err
		}
		items, err := tx.Intents(ctx, projectID)
		if err != nil {
			return err
		}
		result.Data = protocol.IntentList{ProjectID: projectID, Intents: items}
		result.AsOfEvent, err = tx.LastEvent(ctx, projectID)
		return err
	})
	return result, err
}

func validateIntentReferences(ctx context.Context, tx Tx, participant protocol.Participant, revision protocol.IntentRevision) error {
	if _, err := tx.ObjectiveRevision(ctx, revision.ProjectID, revision.ObjectiveID, revision.ObjectiveRevision); errors.Is(err, ErrNotFound) {
		return &InvalidError{Pointer: "/objective_revision", Message: "The Objective revision does not exist in this project."}
	} else if err != nil {
		return err
	}
	if (revision.BlueprintID == nil) != (revision.BlueprintRevision == nil) {
		return &InvalidError{Pointer: "/blueprint_revision", Message: "blueprint_id and blueprint_revision must be supplied together."}
	}
	if revision.CharterID != nil && revision.BlueprintID == nil {
		return &InvalidError{Pointer: "/charter_id", Message: "A Charter requires its Blueprint revision."}
	}
	if revision.RepositoryID != nil {
		if _, err := tx.RepositoryConnection(ctx, revision.ProjectID, *revision.RepositoryID); errors.Is(err, ErrNotFound) {
			return &InvalidError{Pointer: "/repository_id", Message: "The repository does not exist in this project."}
		} else if err != nil {
			return err
		}
	}
	if revision.BlueprintID == nil {
		return nil
	}
	blueprint, err := tx.BlueprintRevision(ctx, revision.ProjectID, *revision.BlueprintID, *revision.BlueprintRevision)
	if errors.Is(err, ErrNotFound) {
		return &InvalidError{Pointer: "/blueprint_revision", Message: "The Blueprint revision does not exist in this project."}
	}
	if err != nil {
		return err
	}
	if blueprint.BlueprintRevision.ObjectiveID != revision.ObjectiveID || blueprint.BlueprintRevision.ObjectiveRevision != revision.ObjectiveRevision {
		return &InvalidError{Pointer: "/blueprint_revision", Message: "The Blueprint revision must govern the same exact Objective revision."}
	}
	if revision.CharterID == nil {
		return nil
	}
	accountable, err := tx.CellAccountable(ctx, *revision.BlueprintID, *revision.BlueprintRevision, *revision.CharterID)
	if errors.Is(err, ErrNotFound) {
		return &InvalidError{Pointer: "/charter_id", Message: "The Charter does not belong to the Blueprint revision."}
	}
	if err != nil {
		return err
	}
	if accountable != participant.HumanID {
		return ErrForbidden
	}
	decision, err := tx.CharterDecision(ctx, *revision.BlueprintID, *revision.BlueprintRevision, *revision.CharterID)
	if errors.Is(err, ErrNotFound) || decision.Outcome != protocol.CharterAccept {
		return &InvalidError{Pointer: "/charter_id", Message: "The referenced Charter has not been accepted."}
	}
	return err
}

// sameIntentContent compares two revisions' canonical JSON, ignoring only the
// server-assigned CreatedAt. Comparing encodings rather than Go values makes a
// client's explicit empty list equal to the omitted list read back from jsonb.
func sameIntentContent(a, b protocol.IntentRevision) bool {
	b.CreatedAt = a.CreatedAt
	left, leftErr := json.Marshal(a)
	right, rightErr := json.Marshal(b)
	return leftErr == nil && rightErr == nil && bytes.Equal(left, right)
}

// IntentCharterObservation describes the optional Charter context of one
// Intent revision. Charters are optional planning context; warnings and
// evidence never require one.
func IntentCharterObservation(revision protocol.IntentRevision) string {
	if revision.CharterID == nil {
		return "No Charter is associated with this Intent. Charters are optional planning context; collision warnings and evidence work without one."
	}
	return fmt.Sprintf("Accepted Charter %s is associated. Conn cannot deterministically map this Intent's summary and claims to Charter boundaries, so possible scope divergence is advisory and does not alter that Charter or authorize a boundary change.", *revision.CharterID)
}

// intentObservationsWithAdvice recomputes advisory observations for one
// revision: its Charter context and near-match subject advice against the
// current catalog, excluding the Intent's own claims. It reads only and never
// changes the Intent, its claims, or a collision.
func intentObservationsWithAdvice(ctx context.Context, tx Tx, revision protocol.IntentRevision) ([]string, error) {
	observations := []string{IntentCharterObservation(revision)}
	if len(revision.DeclaredClaims) == 0 {
		return observations, nil
	}
	objectives, err := tx.CurrentObjectives(ctx, revision.ProjectID)
	if err != nil {
		return nil, err
	}
	claims, err := tx.ActiveSemanticClaims(ctx, revision.ProjectID)
	if err != nil {
		return nil, err
	}
	return append(observations, nearMatchObservations(buildCatalog(objectives, claims, revision.IntentID), revision)...), nil
}
