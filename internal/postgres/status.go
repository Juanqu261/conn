package postgres

import (
	"context"
	"fmt"
	"uuid"

	"github.com/Juanqu261/conn/internal/protocol"
)

func (t projectTx) ObjectiveSummaries(ctx context.Context, projectID uuid.UUID) ([]protocol.ObjectiveSummary, error) {
	rows, err := t.tx.Query(ctx, `
		SELECT objective_id, current_revision FROM objectives
		WHERE project_id = $1
		ORDER BY objective_id`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	summaries := []protocol.ObjectiveSummary{}
	for rows.Next() {
		var s protocol.ObjectiveSummary
		if err := rows.Scan(&s.ObjectiveID, &s.CurrentRevision); err != nil {
			return nil, err
		}
		summaries = append(summaries, s)
	}
	return summaries, rows.Err()
}

// inForce selects the revisions in force: the active one, and the latest.
// The latest revision is never superseded, because only a newer revision
// supersedes another.
const inForce = `(r.status = 'active' OR r.revision = b.current_revision)`

type cellKey struct {
	blueprintID uuid.UUID
	revision    int
	cellID      uuid.UUID
}

func (t projectTx) BlueprintSummaries(ctx context.Context, projectID uuid.UUID) ([]protocol.BlueprintSummary, error) {
	decisions, err := t.decisionsInForce(ctx, projectID)
	if err != nil {
		return nil, err
	}
	rows, err := t.tx.Query(ctx, `
		SELECT b.blueprint_id, b.current_revision, r.status, r.document
		FROM blueprints b
		JOIN blueprint_revisions r USING (blueprint_id)
		WHERE b.project_id = $1 AND `+inForce+`
		ORDER BY b.blueprint_id, r.revision`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	summaries := []protocol.BlueprintSummary{}
	for rows.Next() {
		var blueprintID uuid.UUID
		var current int
		var status string
		var document []byte
		if err := rows.Scan(&blueprintID, &current, &status, &document); err != nil {
			return nil, err
		}
		snapshot, err := protocol.Decode[protocol.BlueprintRevision](document)
		if err != nil {
			return nil, fmt.Errorf("stored blueprint revision: %w", err)
		}
		revision := protocol.RevisionSummary{
			Revision:          snapshot.Revision,
			Status:            protocol.BlueprintStatus(status),
			ObjectiveID:       snapshot.ObjectiveID,
			ObjectiveRevision: snapshot.ObjectiveRevision,
			Cells:             make([]protocol.CellSummary, 0, len(snapshot.CellCharters)),
		}
		for _, charter := range snapshot.CellCharters {
			decision, ok := decisions[cellKey{blueprintID, snapshot.Revision, charter.CellID}]
			if !ok {
				decision = protocol.CellPending
			}
			revision.Cells = append(revision.Cells, protocol.CellSummary{
				CellID:             charter.CellID,
				AccountableHumanID: charter.AccountableHumanID,
				Title:              charter.Title,
				Decision:           decision,
			})
		}
		if n := len(summaries); n > 0 && summaries[n-1].BlueprintID == blueprintID {
			summaries[n-1].Revisions = append(summaries[n-1].Revisions, revision)
			continue
		}
		summaries = append(summaries, protocol.BlueprintSummary{
			BlueprintID:     blueprintID,
			CurrentRevision: current,
			Revisions:       []protocol.RevisionSummary{revision},
		})
	}
	return summaries, rows.Err()
}

func (t projectTx) decisionsInForce(ctx context.Context, projectID uuid.UUID) (map[cellKey]protocol.CellDecision, error) {
	rows, err := t.tx.Query(ctx, `
		SELECT d.blueprint_id, d.blueprint_revision, d.cell_id, d.outcome
		FROM blueprints b
		JOIN blueprint_revisions r USING (blueprint_id)
		JOIN charter_decisions d ON d.blueprint_id = r.blueprint_id AND d.blueprint_revision = r.revision
		WHERE b.project_id = $1 AND `+inForce, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	decisions := map[cellKey]protocol.CellDecision{}
	for rows.Next() {
		var key cellKey
		var outcome string
		if err := rows.Scan(&key.blueprintID, &key.revision, &key.cellID, &outcome); err != nil {
			return nil, err
		}
		decisions[key] = protocol.CellDecision(outcome)
	}
	return decisions, rows.Err()
}
