package postgres

import (
	"context"
	"fmt"

	"uuid"

	"github.com/Juanqu261/conn/internal/protocol"
)

func (t projectTx) CurrentObjectives(ctx context.Context, projectID uuid.UUID) ([]protocol.Objective, error) {
	rows, err := t.tx.Query(ctx, `
		SELECT r.document FROM objectives o
		JOIN objective_revisions r ON r.objective_id = o.objective_id AND r.revision = o.current_revision
		WHERE o.project_id = $1 ORDER BY r.created_at, r.objective_id`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	objectives := []protocol.Objective{}
	for rows.Next() {
		var document []byte
		if err := rows.Scan(&document); err != nil {
			return nil, err
		}
		objective, err := protocol.Decode[protocol.Objective](document)
		if err != nil {
			return nil, fmt.Errorf("stored objective: %w", err)
		}
		objectives = append(objectives, objective)
	}
	return objectives, rows.Err()
}

func (t projectTx) ActiveBlueprint(ctx context.Context, projectID uuid.UUID) (protocol.BlueprintRevision, error) {
	var document []byte
	err := t.tx.QueryRow(ctx, `
		SELECT document FROM blueprint_revisions
		WHERE project_id = $1 AND status = 'active'
		ORDER BY created_at, blueprint_id LIMIT 1`, projectID).Scan(&document)
	if err != nil {
		return protocol.BlueprintRevision{}, notFound(err)
	}
	blueprint, err := protocol.Decode[protocol.BlueprintRevision](document)
	if err != nil {
		return protocol.BlueprintRevision{}, fmt.Errorf("stored active blueprint: %w", err)
	}
	return blueprint, nil
}

func (t projectTx) OpenCollisions(ctx context.Context, projectID uuid.UUID) ([]protocol.CollisionAssessment, error) {
	rows, err := t.tx.Query(ctx, `
		SELECT document FROM collision_assessments
		WHERE project_id = $1 AND state IN ('open', 'under_review')
		ORDER BY severity DESC, created_at, collision_id`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	collisions := []protocol.CollisionAssessment{}
	for rows.Next() {
		var document []byte
		if err := rows.Scan(&document); err != nil {
			return nil, err
		}
		collision, err := protocol.Decode[protocol.CollisionAssessment](document)
		if err != nil {
			return nil, fmt.Errorf("stored collision assessment: %w", err)
		}
		collisions = append(collisions, collision)
	}
	return collisions, rows.Err()
}

func (t projectTx) ResolutionDecisionRefs(ctx context.Context, projectID uuid.UUID) ([]uuid.UUID, error) {
	rows, err := t.tx.Query(ctx, `SELECT decision_id FROM resolution_decisions WHERE project_id = $1 ORDER BY created_at, decision_id`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ids := []uuid.UUID{}
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func (t projectTx) ActiveEvidenceReferences(ctx context.Context, projectID uuid.UUID) ([]protocol.EvidenceReference, error) {
	rows, err := t.tx.Query(ctx, `
		SELECT e.document FROM evidence_references e
		JOIN objectives o ON o.project_id = e.project_id AND o.objective_id = e.objective_id
		WHERE e.project_id = $1 AND e.objective_revision = o.current_revision
		ORDER BY e.observed_at, e.evidence_id`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	evidence := []protocol.EvidenceReference{}
	for rows.Next() {
		var document []byte
		if err := rows.Scan(&document); err != nil {
			return nil, err
		}
		item, err := protocol.Decode[protocol.EvidenceReference](document)
		if err != nil {
			return nil, fmt.Errorf("stored evidence reference: %w", err)
		}
		evidence = append(evidence, item)
	}
	return evidence, rows.Err()
}
