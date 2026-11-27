package application

import (
	"context"

	"uuid"

	"github.com/Juanqu261/conn/internal/protocol"
)

// RecordCombinationCheck appends one immutable combination check for a
// collision and its combination.checked event in one transaction. Any
// member's Participant may record one. The record is advisory: it never
// changes the collision state.
func (p *Projects) RecordCombinationCheck(ctx context.Context, actor, projectID, participantID, collisionID uuid.UUID, input protocol.CombinationCheckInput) (Result[protocol.CombinationCheck], error) {
	if err := validateRecord(input); err != nil {
		return Result[protocol.CombinationCheck]{}, err
	}
	if err := validateCombinationOutcome(input); err != nil {
		return Result[protocol.CombinationCheck]{}, err
	}
	now := p.now()
	check := protocol.CombinationCheck{
		CheckID: uuid.NewV7(), ProjectID: projectID, CollisionID: collisionID,
		Left: input.Left, Right: input.Right, MergeResult: input.MergeResult, ConflictedPaths: input.ConflictedPaths,
		Diagnostics: input.Diagnostics, Setup: input.Setup, Checks: input.Checks, Outcome: input.Outcome,
		ConfigSource: input.ConfigSource, Runner: input.Runner, ProducerParticipantID: participantID,
		CreatedAt: protocol.NewTimestamp(now),
	}
	if err := validateRecord(check); err != nil {
		return Result[protocol.CombinationCheck]{}, err
	}
	var result Result[protocol.CombinationCheck]
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
		collision, err := tx.CollisionAssessment(ctx, projectID, collisionID)
		if err != nil {
			return err
		}
		sameOrder := input.Left.IntentID == collision.LeftIntentID && input.Right.IntentID == collision.RightIntentID
		swapped := input.Left.IntentID == collision.RightIntentID && input.Right.IntentID == collision.LeftIntentID
		if !sameOrder && !swapped {
			return &InvalidError{Pointer: "/left/intent_id", Message: "The left and right Intents must be the two Intents of the collision."}
		}
		if err := tx.InsertCombinationCheck(ctx, check); err != nil {
			return err
		}
		result.Data = check
		result.AsOfEvent, err = tx.AppendEvent(ctx, newEvent(projectID, check.CheckID, actor, now,
			protocol.CombinationChecked{CollisionID: collisionID, Outcome: check.Outcome}))
		return err
	})
	return result, err
}

// validateCombinationOutcome keeps the outcome consistent with the facts the
// record states, so a submitted record cannot claim more than it shows.
func validateCombinationOutcome(input protocol.CombinationCheckInput) error {
	failingCheck := false
	for _, check := range input.Checks {
		passing := check.ExitCode == 0 && !check.TimedOut
		if (check.State == protocol.EvidencePassing) != passing || (check.State == protocol.CombinationCheckTimedOut) != check.TimedOut {
			return &InvalidError{Pointer: "/checks", Message: "A pair check is passing only when it exited 0 in time, timed_out when it timed out, and failing otherwise."}
		}
		failingCheck = failingCheck || check.State == protocol.EvidenceFailing
	}
	conflict := input.MergeResult == protocol.CombinationMergeConflict
	switch {
	case conflict != (input.Outcome == protocol.CombinationMergeConflict):
		return &InvalidError{Pointer: "/outcome", Message: "The outcome is merge_conflict exactly when the merge had conflicts."}
	case conflict && (len(input.Checks) > 0 || input.Setup != nil || input.Diagnostics.Ran):
		return &InvalidError{Pointer: "/checks", Message: "A merge conflict runs no diagnostics, setup, or checks."}
	case !conflict && len(input.ConflictedPaths) > 0:
		return &InvalidError{Pointer: "/conflicted_paths", Message: "Only a merge conflict lists conflicted paths."}
	case input.Outcome == protocol.CombinationPassing && (failingCheck || input.Diagnostics.NewCount > 0):
		return &InvalidError{Pointer: "/outcome", Message: "A combination check with a new diagnostic or a failing check is failing."}
	case input.Diagnostics.NewCount < len(input.Diagnostics.New):
		return &InvalidError{Pointer: "/diagnostics/new_count", Message: "new_count cannot be smaller than the listed new diagnostics."}
	}
	return nil
}

// withCombinationOutcomes fills each collision's latest combination outcome
// for a read view. Stored assessments never carry it.
func withCombinationOutcomes(ctx context.Context, tx Tx, projectID uuid.UUID, groups ...[]protocol.CollisionAssessment) error {
	var ids []uuid.UUID
	for _, group := range groups {
		for _, collision := range group {
			ids = append(ids, collision.CollisionID)
		}
	}
	if len(ids) == 0 {
		return nil
	}
	outcomes, err := tx.LatestCombinationOutcomes(ctx, projectID, ids)
	if err != nil {
		return err
	}
	for _, group := range groups {
		for i := range group {
			group[i].LatestCombinationOutcome = outcomes[group[i].CollisionID]
		}
	}
	return nil
}
