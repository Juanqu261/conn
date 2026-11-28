package application

import (
	"context"
	"fmt"
	"time"

	"uuid"

	"github.com/Juanqu261/conn/internal/board"
	"github.com/Juanqu261/conn/internal/protocol"
)

// BoardAs derives the Intent board for an authorized reader from one
// repeatable-read snapshot, including its event cursor. The lanes follow
// from board.Card only; this use case loads and never decides.
func (p *Projects) BoardAs(ctx context.Context, reader Reader, projectID uuid.UUID, windowDays int) (Result[protocol.BoardView], error) {
	if windowDays < 1 || windowDays > board.MaxWindowDays {
		return Result[protocol.BoardView]{}, &InvalidError{Message: fmt.Sprintf("The board window must be between 1 and %d days.", board.MaxWindowDays)}
	}
	now := p.now()
	var result Result[protocol.BoardView]
	err := p.Store.Read(ctx, func(tx Tx) error {
		if err := reader.authorize(ctx, tx, projectID); err != nil {
			return err
		}
		intents, err := tx.Intents(ctx, projectID)
		if err != nil {
			return err
		}
		collisions, err := tx.OpenCollisions(ctx, projectID)
		if err != nil {
			return err
		}
		analyzed, err := tx.AnalyzedRevisions(ctx, projectID)
		if err != nil {
			return err
		}
		released, err := tx.IntentReleases(ctx, projectID, now.Add(-time.Duration(windowDays)*24*time.Hour))
		if err != nil {
			return err
		}
		snapshot := board.Snapshot{
			Now: now, WindowDays: windowDays, Intents: intents, ReleasedAt: released,
			Collisions: collisions, Analyzed: analyzed, Objectives: map[board.ObjectiveRef]protocol.Objective{},
		}
		for _, intent := range intents {
			if intent.Status == "superseded" {
				continue
			}
			ref := board.ObjectiveRef{ID: intent.IntentRevision.ObjectiveID, Revision: intent.IntentRevision.ObjectiveRevision}
			if _, loaded := snapshot.Objectives[ref]; loaded {
				continue
			}
			objective, err := tx.ObjectiveRevision(ctx, projectID, ref.ID, ref.Revision)
			if err != nil {
				return err
			}
			snapshot.Objectives[ref] = objective
			evidence, err := tx.EvidenceReferences(ctx, projectID, ref.ID, ref.Revision)
			if err != nil {
				return err
			}
			snapshot.Evidence = append(snapshot.Evidence, evidence...)
		}
		if result.AsOfEvent, err = tx.LastEvent(ctx, projectID); err != nil {
			return err
		}
		result.Data = board.Build(projectID, result.AsOfEvent, snapshot)
		return validateRecord(result.Data)
	})
	return result, err
}

// CollisionsInStateAs lists the most recent collisions of one state to an
// authorized reader, newest first, and says when more exist.
func (p *Projects) CollisionsInStateAs(ctx context.Context, reader Reader, projectID uuid.UUID, state string, limit int) (Result[protocol.CollisionList], error) {
	if limit < 1 || limit > protocol.MaxCollisionList {
		return Result[protocol.CollisionList]{}, &InvalidError{Message: fmt.Sprintf("The collision limit must be between 1 and %d.", protocol.MaxCollisionList)}
	}
	var result Result[protocol.CollisionList]
	err := p.Store.Read(ctx, func(tx Tx) error {
		if err := reader.authorize(ctx, tx, projectID); err != nil {
			return err
		}
		collisions, err := tx.CollisionsInState(ctx, projectID, state, limit+1)
		if err != nil {
			return err
		}
		list := protocol.CollisionList{ProjectID: projectID, State: state, Collisions: collisions}
		if len(collisions) > limit {
			list.Collisions, list.Truncated = collisions[:limit], true
		}
		result.Data = list
		result.AsOfEvent, err = tx.LastEvent(ctx, projectID)
		return err
	})
	return result, err
}
