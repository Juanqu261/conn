package application

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"uuid"

	"github.com/Juanqu261/conn/internal/protocol"
)

// ErrCharterDecided means the charter already has a different decision in
// this BlueprintRevision. Decisions are final; changes need a new revision.
var ErrCharterDecided = errors.New("the charter already has a different decision")

var ErrObjectiveHasBlueprint = errors.New("the objective already has a blueprint")

// ObjectiveBlueprintError names the Blueprint to revise instead.
type ObjectiveBlueprintError struct {
	ObjectiveID     uuid.UUID
	BlueprintID     uuid.UUID
	CurrentRevision int
}

func (e *ObjectiveBlueprintError) Error() string {
	return fmt.Sprintf("objective %s already has blueprint %s at revision %d", e.ObjectiveID, e.BlueprintID, e.CurrentRevision)
}

func (e *ObjectiveBlueprintError) Unwrap() error { return ErrObjectiveHasBlueprint }

// ProposeBlueprintRevision lets the owner propose the next immutable
// BlueprintRevision. expectedRevision is the latest revision the proposer
// saw, or 0 to create the Blueprint. The new revision supersedes a pending
// proposal, while an active revision stays in force until the new one is
// ratified. Repeating a request that already succeeded returns the stored
// revision without a new event.
//
// An Objective has one Blueprint and every revision divides that Objective:
// creating a second Blueprint for it returns an ObjectiveBlueprintError,
// and a revision naming another Objective is invalid.
func (p *Projects) ProposeBlueprintRevision(ctx context.Context, actor, projectID uuid.UUID, proposal protocol.BlueprintProposal, expectedRevision int) (Result[protocol.BlueprintRevisionView], error) {
	if expectedRevision < 0 {
		return Result[protocol.BlueprintRevisionView]{}, &InvalidError{Message: "The expected revision cannot be negative."}
	}
	if err := validatePartition(proposal.CellCharters); err != nil {
		return Result[protocol.BlueprintRevisionView]{}, err
	}
	now := p.now()
	revision := protocol.BlueprintRevision{
		BlueprintID:       proposal.BlueprintID,
		ProjectID:         projectID,
		Revision:          expectedRevision + 1,
		ObjectiveID:       proposal.ObjectiveID,
		ObjectiveRevision: proposal.ObjectiveRevision,
		ProposedByHumanID: actor,
		CreatedAt:         protocol.NewTimestamp(now),
		CellCharters:      proposal.CellCharters,
	}
	if err := validateRecord(revision); err != nil {
		return Result[protocol.BlueprintRevisionView]{}, err
	}

	var result Result[protocol.BlueprintRevisionView]
	err := p.Store.Write(ctx, func(tx Tx) error {
		if err := requireOwner(ctx, tx, projectID, actor); err != nil {
			return err
		}
		if err := validateCharterReferences(ctx, tx, revision); err != nil {
			return err
		}
		if err := requireOneBlueprintPerObjective(ctx, tx, revision); err != nil {
			return err
		}

		inserted, err := tx.InsertBlueprintRevision(ctx, revision)
		if err != nil {
			return err
		}
		if !inserted {
			load := func(n int) (protocol.BlueprintRevisionView, error) {
				return tx.BlueprintRevision(ctx, projectID, revision.BlueprintID, n)
			}
			same := func(existing protocol.BlueprintRevisionView) bool {
				return sameBlueprintContent(existing.BlueprintRevision, revision)
			}
			if result.Data, err = replay(revision.Revision, load, same); err != nil {
				return err
			}
			result.AsOfEvent, err = tx.LastEvent(ctx, projectID)
			return err
		}
		result.Data = protocol.BlueprintRevisionView{BlueprintRevision: revision, Status: protocol.BlueprintProposed}
		result.AsOfEvent, err = tx.AppendEvent(ctx, newEvent(projectID, revision.BlueprintID, actor, now,
			protocol.BlueprintRevisionProposed{
				Revision:          revision.Revision,
				ObjectiveID:       revision.ObjectiveID,
				ObjectiveRevision: revision.ObjectiveRevision,
			}))
		return err
	})
	return result, err
}

// BlueprintRevision returns one immutable BlueprintRevision and its current
// status to a member.
func (p *Projects) BlueprintRevision(ctx context.Context, actor, projectID, blueprintID uuid.UUID, revision int) (Result[protocol.BlueprintRevisionView], error) {
	var result Result[protocol.BlueprintRevisionView]
	err := p.Store.Read(ctx, func(tx Tx) error {
		if _, err := tx.Role(ctx, projectID, actor); err != nil {
			return err
		}
		var err error
		if result.Data, err = tx.BlueprintRevision(ctx, projectID, blueprintID, revision); err != nil {
			return err
		}
		result.AsOfEvent, err = tx.LastEvent(ctx, projectID)
		return err
	})
	return result, err
}

// AcceptCharter records the actor's acceptance of their own charter. The
// final required acceptance activates the BlueprintRevision in the same
// transaction. Accepting again returns the existing decision.
func (p *Projects) AcceptCharter(ctx context.Context, actor, projectID, blueprintID uuid.UUID, revision int, cellID uuid.UUID) (Result[protocol.CharterDecision], error) {
	return p.decideCharter(ctx, actor, projectID, blueprintID, revision, cellID, protocol.CharterAccept, "")
}

// RejectCharter records the actor's rejection of their own charter with a
// required rationale and makes the BlueprintRevision rejected. Resolving it
// requires a new BlueprintRevision.
func (p *Projects) RejectCharter(ctx context.Context, actor, projectID, blueprintID uuid.UUID, revision int, cellID uuid.UUID, rationale string) (Result[protocol.CharterDecision], error) {
	if strings.TrimSpace(rationale) == "" {
		return Result[protocol.CharterDecision]{}, &InvalidError{Pointer: "/rationale", Message: "A rejection requires a rationale."}
	}
	return p.decideCharter(ctx, actor, projectID, blueprintID, revision, cellID, protocol.CharterReject, rationale)
}

func (p *Projects) decideCharter(ctx context.Context, actor, projectID, blueprintID uuid.UUID, revision int, cellID uuid.UUID, outcome protocol.CharterOutcome, rationale string) (Result[protocol.CharterDecision], error) {
	now := p.now()
	decision := protocol.CharterDecision{
		DecisionID:        uuid.NewV7(),
		ProjectID:         projectID,
		BlueprintID:       blueprintID,
		BlueprintRevision: revision,
		CellID:            cellID,
		HumanID:           actor,
		Outcome:           outcome,
		Rationale:         rationale,
		DecidedAt:         protocol.NewTimestamp(now),
	}
	if err := validateRecord(decision); err != nil {
		return Result[protocol.CharterDecision]{}, err
	}

	var result Result[protocol.CharterDecision]
	err := p.Store.Write(ctx, func(tx Tx) error {
		if _, err := tx.Role(ctx, projectID, actor); err != nil {
			return err
		}
		status, err := tx.LockBlueprintRevision(ctx, projectID, blueprintID, revision)
		if err != nil {
			return err
		}
		accountable, err := tx.CellAccountable(ctx, blueprintID, revision, cellID)
		if err != nil {
			return err
		}
		// No role, including owner, may decide another human's charter.
		if accountable != actor {
			return ErrForbidden
		}

		existing, err := tx.CharterDecision(ctx, blueprintID, revision, cellID)
		switch {
		case err == nil && existing.Outcome == outcome && existing.Rationale == rationale:
			result.Data = existing
			result.AsOfEvent, err = tx.LastEvent(ctx, projectID)
			return err
		case err == nil:
			return ErrCharterDecided
		case !errors.Is(err, ErrNotFound):
			return err
		}
		// Only a proposed revision takes new decisions. A rejected or
		// superseded one is no longer current.
		if status != protocol.BlueprintProposed {
			return ErrStaleRevision
		}

		if err := tx.InsertCharterDecision(ctx, decision); err != nil {
			return err
		}
		ref := protocol.CharterAccepted{BlueprintID: blueprintID, BlueprintRevision: revision, CellID: cellID}
		var event protocol.EventPayload = ref
		if outcome == protocol.CharterReject {
			event = protocol.CharterRejected(ref)
		}
		result.Data = decision
		if result.AsOfEvent, err = tx.AppendEvent(ctx, newEvent(projectID, decision.DecisionID, actor, now, event)); err != nil {
			return err
		}

		if outcome == protocol.CharterReject {
			if err := tx.RejectBlueprintRevision(ctx, blueprintID, revision); err != nil {
				return err
			}
			result.AsOfEvent, err = tx.AppendEvent(ctx, newEvent(projectID, blueprintID, actor, now,
				protocol.BlueprintRevisionRejected{Revision: revision, CellID: cellID}))
			return err
		}
		remaining, err := tx.UnacceptedCells(ctx, blueprintID, revision)
		if err != nil || remaining > 0 {
			return err
		}
		if err := tx.ActivateBlueprintRevision(ctx, blueprintID, revision); err != nil {
			return err
		}
		result.AsOfEvent, err = tx.AppendEvent(ctx, newEvent(projectID, blueprintID, actor, now,
			protocol.BlueprintRevisionActivated{Revision: revision}))
		return err
	})
	return result, err
}

// validatePartition checks the rules that need only the proposal: unique
// cells, unique boundary keys within a charter, and dependencies on other
// cells of the same revision.
func validatePartition(charters []protocol.CellCharter) error {
	cells := make(map[uuid.UUID]bool, len(charters))
	for i, charter := range charters {
		if cells[charter.CellID] {
			return &InvalidError{
				Pointer: fmt.Sprintf("/cell_charters/%d/cell_id", i),
				Message: "Each cell may appear only once in a blueprint revision.",
			}
		}
		cells[charter.CellID] = true
	}
	for i, charter := range charters {
		pointer := fmt.Sprintf("/cell_charters/%d", i)
		if err := uniqueKeys(charter.BoundaryPoints, func(b protocol.BoundaryPoint) string { return b.Key }, pointer+"/boundary_points", "Boundary-point"); err != nil {
			return err
		}
		for j, dependency := range charter.Dependencies {
			if dependency.CellID == charter.CellID || !cells[dependency.CellID] {
				return &InvalidError{
					Pointer: fmt.Sprintf("%s/dependencies/%d/cell_id", pointer, j),
					Message: "A dependency must name another cell in the same blueprint revision.",
				}
			}
		}
	}
	return nil
}

// validateCharterReferences checks the rules that need stored state: the
// exact Objective revision, its point keys, and accountable members.
func validateCharterReferences(ctx context.Context, tx Tx, revision protocol.BlueprintRevision) error {
	objective, err := tx.ObjectiveRevision(ctx, revision.ProjectID, revision.ObjectiveID, revision.ObjectiveRevision)
	if errors.Is(err, ErrNotFound) {
		return &InvalidError{Pointer: "/objective_revision", Message: "The Objective revision does not exist in this project."}
	}
	if err != nil {
		return err
	}
	points := make(map[string]bool, len(objective.KeyPoints))
	for _, point := range objective.KeyPoints {
		points[point.Key] = true
	}
	for i, charter := range revision.CellCharters {
		for j, ref := range charter.ObjectivePointRefs {
			if !points[ref] {
				return &InvalidError{
					Pointer: fmt.Sprintf("/cell_charters/%d/objective_point_refs/%d", i, j),
					Message: fmt.Sprintf("Objective revision %d has no key point %q.", revision.ObjectiveRevision, ref),
				}
			}
		}
		if _, err := tx.Role(ctx, revision.ProjectID, charter.AccountableHumanID); errors.Is(err, ErrNotFound) {
			return &InvalidError{
				Pointer: fmt.Sprintf("/cell_charters/%d/accountable_human_id", i),
				Message: "The accountable human is not a member of this project.",
			}
		} else if err != nil {
			return err
		}
	}
	return nil
}

// requireOneBlueprintPerObjective keeps one Blueprint per Objective. A
// Blueprint keeps the Objective of its first revision, and no other
// Blueprint may divide that Objective. Retrying a stored request passes.
func requireOneBlueprintPerObjective(ctx context.Context, tx Tx, revision protocol.BlueprintRevision) error {
	objectiveID, err := tx.BlueprintObjective(ctx, revision.ProjectID, revision.BlueprintID)
	switch {
	case err == nil && objectiveID != revision.ObjectiveID:
		return &InvalidError{
			Pointer: "/objective_id",
			Message: fmt.Sprintf("This blueprint divides Objective %s. Propose a plan for another Objective as a new blueprint.", objectiveID),
		}
	case err == nil:
		return nil
	case !errors.Is(err, ErrNotFound):
		return err
	}
	// A new Blueprint. Another one may already divide the Objective.
	blueprintID, current, err := tx.ObjectiveBlueprint(ctx, revision.ProjectID, revision.ObjectiveID)
	switch {
	case err == nil:
		return &ObjectiveBlueprintError{ObjectiveID: revision.ObjectiveID, BlueprintID: blueprintID, CurrentRevision: current}
	case errors.Is(err, ErrNotFound):
		return nil
	default:
		return err
	}
}

// sameBlueprintContent compares everything except the creation time.
// Charters are compared in their canonical encoding, where absent and
// empty optional lists are the same.
func sameBlueprintContent(a, b protocol.BlueprintRevision) bool {
	if a.BlueprintID != b.BlueprintID || a.ProjectID != b.ProjectID || a.Revision != b.Revision ||
		a.ObjectiveID != b.ObjectiveID || a.ObjectiveRevision != b.ObjectiveRevision ||
		a.ProposedByHumanID != b.ProposedByHumanID {
		return false
	}
	encodedA, errA := json.Marshal(a.CellCharters)
	encodedB, errB := json.Marshal(b.CellCharters)
	return errA == nil && errB == nil && string(encodedA) == string(encodedB)
}
