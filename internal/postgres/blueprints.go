package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/Juanqu261/conn/internal/application"
	"github.com/Juanqu261/conn/internal/protocol"
)

// uniqueViolation is PostgreSQL's SQLSTATE for unique_violation.
const uniqueViolation = "23505"

func (t projectTx) ObjectiveBlueprint(ctx context.Context, projectID, objectiveID uuid.UUID) (uuid.UUID, int, error) {
	var blueprintID uuid.UUID
	var current int
	err := t.tx.QueryRow(ctx, `
		SELECT blueprint_id, current_revision FROM blueprints
		WHERE project_id = $1 AND objective_id = $2`,
		projectID, objectiveID,
	).Scan(&blueprintID, &current)
	return blueprintID, current, notFound(err)
}

func (t projectTx) BlueprintObjective(ctx context.Context, projectID, blueprintID uuid.UUID) (uuid.UUID, error) {
	var objectiveID uuid.UUID
	err := t.tx.QueryRow(ctx, `
		SELECT objective_id FROM blueprints
		WHERE project_id = $1 AND blueprint_id = $2`,
		projectID, blueprintID,
	).Scan(&objectiveID)
	return objectiveID, notFound(err)
}

func (t projectTx) InsertBlueprintRevision(ctx context.Context, r protocol.BlueprintRevision) (bool, error) {
	document, err := protocol.Encode(r)
	if err != nil {
		return false, err
	}
	// As for Objectives, the conditional update serializes writers of the
	// same next revision. Every revision divides the blueprint's Objective.
	query := `
		UPDATE blueprints SET current_revision = $3
		WHERE blueprint_id = $1 AND project_id = $2 AND current_revision = $3 - 1
		  AND objective_id = $4`
	if r.Revision == 1 {
		query = `
			INSERT INTO blueprints (blueprint_id, project_id, current_revision, objective_id)
			VALUES ($1, $2, $3, $4)
			ON CONFLICT (blueprint_id) DO NOTHING`
	}
	tag, err := t.tx.Exec(ctx, query, r.BlueprintID, r.ProjectID, r.Revision, r.ObjectiveID)
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == uniqueViolation && pgErr.ConstraintName == "blueprints_objective_id_key" {
		// A concurrent proposal created the Objective's blueprint first.
		return false, application.ErrObjectiveHasBlueprint
	}
	if err != nil || tag.RowsAffected() == 0 {
		return false, err
	}
	// Supersede first: a blueprint may hold only one proposed revision.
	if _, err := t.tx.Exec(ctx, `
		UPDATE blueprint_revisions SET status = 'superseded'
		WHERE blueprint_id = $1 AND status = 'proposed'`, r.BlueprintID); err != nil {
		return false, err
	}
	if _, err := t.tx.Exec(ctx, `
		INSERT INTO blueprint_revisions
			(blueprint_id, revision, project_id, objective_id, objective_revision,
			 proposed_by_human_id, status, created_at, document)
		VALUES ($1, $2, $3, $4, $5, $6, 'proposed', $7, $8)`,
		r.BlueprintID, r.Revision, r.ProjectID, r.ObjectiveID, r.ObjectiveRevision,
		r.ProposedByHumanID, r.CreatedAt.Time(), document); err != nil {
		return false, err
	}
	for _, charter := range r.CellCharters {
		if _, err := t.tx.Exec(ctx, `
			INSERT INTO blueprint_cells
				(blueprint_id, blueprint_revision, cell_id, project_id, accountable_human_id)
			VALUES ($1, $2, $3, $4, $5)`,
			r.BlueprintID, r.Revision, charter.CellID, r.ProjectID, charter.AccountableHumanID); err != nil {
			return false, err
		}
	}
	return true, nil
}

func (t projectTx) BlueprintRevision(ctx context.Context, projectID, blueprintID uuid.UUID, revision int) (protocol.BlueprintRevisionView, error) {
	var status string
	var document []byte
	err := t.tx.QueryRow(ctx, `
		SELECT status, document FROM blueprint_revisions
		WHERE project_id = $1 AND blueprint_id = $2 AND revision = $3`,
		projectID, blueprintID, revision,
	).Scan(&status, &document)
	if err != nil {
		return protocol.BlueprintRevisionView{}, notFound(err)
	}
	snapshot, err := protocol.Decode[protocol.BlueprintRevision](document)
	if err != nil {
		return protocol.BlueprintRevisionView{}, fmt.Errorf("stored blueprint revision: %w", err)
	}
	return protocol.BlueprintRevisionView{BlueprintRevision: snapshot, Status: protocol.BlueprintStatus(status)}, nil
}

func (t projectTx) LockBlueprintRevision(ctx context.Context, projectID, blueprintID uuid.UUID, revision int) (protocol.BlueprintStatus, error) {
	var status string
	err := t.tx.QueryRow(ctx, `
		SELECT status FROM blueprint_revisions
		WHERE project_id = $1 AND blueprint_id = $2 AND revision = $3
		FOR UPDATE`,
		projectID, blueprintID, revision,
	).Scan(&status)
	return protocol.BlueprintStatus(status), notFound(err)
}

func (t projectTx) CellAccountable(ctx context.Context, blueprintID uuid.UUID, revision int, cellID uuid.UUID) (uuid.UUID, error) {
	var human uuid.UUID
	err := t.tx.QueryRow(ctx, `
		SELECT accountable_human_id FROM blueprint_cells
		WHERE blueprint_id = $1 AND blueprint_revision = $2 AND cell_id = $3`,
		blueprintID, revision, cellID,
	).Scan(&human)
	return human, notFound(err)
}

func (t projectTx) CharterDecision(ctx context.Context, blueprintID uuid.UUID, revision int, cellID uuid.UUID) (protocol.CharterDecision, error) {
	var d protocol.CharterDecision
	var outcome string
	var rationale *string
	var decidedAt time.Time
	err := t.tx.QueryRow(ctx, `
		SELECT decision_id, project_id, blueprint_id, blueprint_revision, cell_id, human_id,
			outcome, rationale, decided_at
		FROM charter_decisions
		WHERE blueprint_id = $1 AND blueprint_revision = $2 AND cell_id = $3`,
		blueprintID, revision, cellID,
	).Scan(&d.DecisionID, &d.ProjectID, &d.BlueprintID, &d.BlueprintRevision, &d.CellID, &d.HumanID,
		&outcome, &rationale, &decidedAt)
	if err != nil {
		return protocol.CharterDecision{}, notFound(err)
	}
	d.Outcome = protocol.CharterOutcome(outcome)
	if rationale != nil {
		d.Rationale = *rationale
	}
	d.DecidedAt = protocol.NewTimestamp(decidedAt)
	return d, nil
}

func (t projectTx) InsertCharterDecision(ctx context.Context, d protocol.CharterDecision) error {
	var rationale *string
	if d.Rationale != "" {
		rationale = &d.Rationale
	}
	_, err := t.tx.Exec(ctx, `
		INSERT INTO charter_decisions
			(decision_id, project_id, blueprint_id, blueprint_revision, cell_id, human_id,
			 outcome, rationale, decided_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
		d.DecisionID, d.ProjectID, d.BlueprintID, d.BlueprintRevision, d.CellID, d.HumanID,
		string(d.Outcome), rationale, d.DecidedAt.Time())
	return err
}

func (t projectTx) UnacceptedCells(ctx context.Context, blueprintID uuid.UUID, revision int) (int, error) {
	var count int
	err := t.tx.QueryRow(ctx, `
		SELECT count(*) FROM blueprint_cells c
		WHERE c.blueprint_id = $1 AND c.blueprint_revision = $2
		  AND NOT EXISTS (
			SELECT 1 FROM charter_decisions d
			WHERE d.blueprint_id = c.blueprint_id
			  AND d.blueprint_revision = c.blueprint_revision
			  AND d.cell_id = c.cell_id
			  AND d.outcome = 'accept')`,
		blueprintID, revision,
	).Scan(&count)
	return count, err
}

func (t projectTx) ActivateBlueprintRevision(ctx context.Context, blueprintID uuid.UUID, revision int) error {
	// Supersede first: a blueprint may hold only one active revision.
	if _, err := t.tx.Exec(ctx, `
		UPDATE blueprint_revisions SET status = 'superseded'
		WHERE blueprint_id = $1 AND status = 'active'`, blueprintID); err != nil {
		return err
	}
	return t.setProposedStatus(ctx, blueprintID, revision, protocol.BlueprintActive)
}

func (t projectTx) RejectBlueprintRevision(ctx context.Context, blueprintID uuid.UUID, revision int) error {
	return t.setProposedStatus(ctx, blueprintID, revision, protocol.BlueprintRejected)
}

func (t projectTx) setProposedStatus(ctx context.Context, blueprintID uuid.UUID, revision int, status protocol.BlueprintStatus) error {
	tag, err := t.tx.Exec(ctx, `
		UPDATE blueprint_revisions SET status = $3
		WHERE blueprint_id = $1 AND revision = $2 AND status = 'proposed'`,
		blueprintID, revision, string(status))
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return fmt.Errorf("blueprint %s revision %d is not proposed", blueprintID, revision)
	}
	return nil
}
