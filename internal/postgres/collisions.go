package postgres

import (
	"context"
	"encoding/json"
	"fmt"

	"uuid"

	"github.com/jackc/pgx/v5"

	"github.com/Juanqu261/conn/internal/application"
	"github.com/Juanqu261/conn/internal/protocol"
)

func (t projectTx) InsertSemanticClaims(ctx context.Context, claims []protocol.SemanticClaim) error {
	for _, claim := range claims {
		document, err := protocol.Encode(claim)
		if err != nil {
			return err
		}
		_, err = t.tx.Exec(ctx, `
			INSERT INTO semantic_claims
				(claim_id, project_id, intent_id, intent_revision, subject_kind,
				 normalized_subject, display_name, relation, provenance_kind,
				 confidence, producer_participant_id, observed_at, document)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)`,
			claim.ClaimID, claim.ProjectID, claim.IntentID, claim.IntentRevision,
			claim.SubjectKind, claim.NormalizedSubject, claim.DisplayName, claim.Relation,
			claim.ProvenanceKind, claim.Confidence, claim.ProducerParticipantID,
			claim.ObservedAt.Time(), document)
		if err != nil {
			return err
		}
	}
	return nil
}

func (t projectTx) AnalyzerReportAttached(ctx context.Context, projectID, intentID uuid.UUID, revision int) (bool, error) {
	var attached bool
	err := t.tx.QueryRow(ctx, `SELECT EXISTS (
		SELECT 1 FROM intent_analysis_reports
		WHERE project_id = $1 AND intent_id = $2 AND intent_revision = $3
	)`, projectID, intentID, revision).Scan(&attached)
	return attached, err
}

func (t projectTx) InsertAnalyzerReport(ctx context.Context, projectID, intentID uuid.UUID, revision int, report protocol.LocalAnalyzerReport, observedAt protocol.Timestamp) error {
	diagnostics, err := json.Marshal(report.Diagnostics)
	if err != nil {
		return err
	}
	_, err = t.tx.Exec(ctx, `
		INSERT INTO intent_analysis_reports
			(project_id, intent_id, intent_revision, canonical_remote, diagnostics, observed_at)
		VALUES ($1,$2,$3,$4,$5,$6)`,
		projectID, intentID, revision, report.Repository.CanonicalRemote, diagnostics, observedAt.Time())
	return err
}

func (t projectTx) ActiveSemanticClaims(ctx context.Context, projectID uuid.UUID) ([]protocol.SemanticClaim, error) {
	rows, err := t.tx.Query(ctx, `
		SELECT c.document
		FROM semantic_claims c
		JOIN intents i ON i.project_id = c.project_id
			AND i.intent_id = c.intent_id
			AND i.current_revision = c.intent_revision
		WHERE c.project_id = $1 AND i.status = 'active'
		ORDER BY c.normalized_subject, c.claim_id`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	claims := []protocol.SemanticClaim{}
	for rows.Next() {
		var document []byte
		if err := rows.Scan(&document); err != nil {
			return nil, err
		}
		claim, err := protocol.Decode[protocol.SemanticClaim](document)
		if err != nil {
			return nil, fmt.Errorf("stored semantic claim: %w", err)
		}
		claims = append(claims, claim)
	}
	return claims, rows.Err()
}

func (t projectTx) StaleOpenCollisionsForIntent(ctx context.Context, projectID, intentID uuid.UUID, revision int, staleAt protocol.Timestamp) ([]uuid.UUID, error) {
	rows, err := t.tx.Query(ctx, `
		UPDATE collision_assessments
		SET state = 'stale', superseded_at = $4,
			document = jsonb_set(
				jsonb_set(document, '{state}', '"stale"'::jsonb),
				'{superseded_at}',
				to_jsonb(to_char($4 AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS.US"Z"'))
			)
		WHERE project_id = $1 AND state IN ('open', 'under_review')
			AND ((left_intent_id = $2 AND left_intent_revision = $3)
				OR (right_intent_id = $2 AND right_intent_revision = $3))
		RETURNING collision_id`, projectID, intentID, revision, staleAt.Time())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var collisionIDs []uuid.UUID
	for rows.Next() {
		var collisionID uuid.UUID
		if err := rows.Scan(&collisionID); err != nil {
			return nil, err
		}
		collisionIDs = append(collisionIDs, collisionID)
	}
	return collisionIDs, rows.Err()
}

func (t projectTx) InsertCollisionAssessment(ctx context.Context, assessment protocol.CollisionAssessment) (bool, error) {
	document, err := protocol.Encode(assessment)
	if err != nil {
		return false, err
	}
	tag, err := t.tx.Exec(ctx, `
		INSERT INTO collision_assessments
			(collision_id, project_id, left_intent_id, left_intent_revision,
			 right_intent_id, right_intent_revision, rule_id, severity,
			 confidence, state, explanation_markdown, created_at, superseded_at,
			 matched_subjects, decision_id, document)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16)
		ON CONFLICT (project_id, left_intent_id, left_intent_revision,
			right_intent_id, right_intent_revision, rule_id) DO NOTHING`,
		assessment.CollisionID, assessment.ProjectID, assessment.LeftIntentID, assessment.LeftIntentRevision,
		assessment.RightIntentID, assessment.RightIntentRevision, assessment.RuleID, assessment.Severity,
		assessment.Confidence, assessment.State, assessment.ExplanationMarkdown, assessment.CreatedAt.Time(),
		assessment.SupersededAt, assessment.MatchedSubjects, assessment.DecisionRef, document)
	if err != nil {
		return false, err
	}
	if tag.RowsAffected() == 0 {
		return false, nil
	}
	for _, claimID := range assessment.MatchingClaimRefs {
		if _, err := t.tx.Exec(ctx, `INSERT INTO collision_claim_matches (collision_id, claim_id, project_id) VALUES ($1,$2,$3)`, assessment.CollisionID, claimID, assessment.ProjectID); err != nil {
			return false, err
		}
	}
	return true, nil
}

func (t projectTx) LatestDecidedCollision(ctx context.Context, projectID, leftIntentID, rightIntentID uuid.UUID, ruleID string) (protocol.CollisionAssessment, error) {
	var document []byte
	err := t.tx.QueryRow(ctx, `
		SELECT document FROM collision_assessments
		WHERE project_id = $1 AND left_intent_id = $2 AND right_intent_id = $3 AND rule_id = $4
			AND state IN ('resolved', 'dismissed')
		ORDER BY created_at DESC, collision_id DESC
		LIMIT 1`, projectID, leftIntentID, rightIntentID, ruleID).Scan(&document)
	if err != nil {
		return protocol.CollisionAssessment{}, notFound(err)
	}
	return decodeCollision(document)
}

func (t projectTx) SetCollisionDecision(ctx context.Context, projectID, collisionID uuid.UUID, state string, decisionID uuid.UUID) error {
	tag, err := t.tx.Exec(ctx, `
		UPDATE collision_assessments
		SET state = $3, decision_id = $4,
			document = jsonb_set(
				jsonb_set(document, '{state}', to_jsonb($3::text)),
				'{decision_ref}', to_jsonb($5::text))
		WHERE project_id = $1 AND collision_id = $2`, projectID, collisionID, state, decisionID, decisionID.String())
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return application.ErrNotFound
	}
	return nil
}

func (t projectTx) DecidedCollisions(ctx context.Context, projectID uuid.UUID, limit int) ([]protocol.CollisionAssessment, error) {
	rows, err := t.tx.Query(ctx, `
		SELECT a.document FROM collision_assessments a
		JOIN intents l ON l.project_id = a.project_id AND l.intent_id = a.left_intent_id
			AND l.status = 'active' AND l.current_revision = a.left_intent_revision
		JOIN intents r ON r.project_id = a.project_id AND r.intent_id = a.right_intent_id
			AND r.status = 'active' AND r.current_revision = a.right_intent_revision
		WHERE a.project_id = $1 AND a.state IN ('resolved', 'dismissed')
		ORDER BY a.created_at DESC, a.collision_id DESC
		LIMIT $2`, projectID, limit)
	if err != nil {
		return nil, err
	}
	return collectCollisions(rows)
}

func (t projectTx) CollisionsForIntentRevision(ctx context.Context, projectID, intentID uuid.UUID, revision int) ([]protocol.CollisionAssessment, error) {
	rows, err := t.tx.Query(ctx, `
		SELECT document FROM collision_assessments
		WHERE project_id = $1
			AND ((left_intent_id = $2 AND left_intent_revision = $3)
				OR (right_intent_id = $2 AND right_intent_revision = $3))
		ORDER BY created_at, collision_id`, projectID, intentID, revision)
	if err != nil {
		return nil, err
	}
	return collectCollisions(rows)
}

func collectCollisions(rows pgx.Rows) ([]protocol.CollisionAssessment, error) {
	defer rows.Close()
	collisions := []protocol.CollisionAssessment{}
	for rows.Next() {
		var document []byte
		if err := rows.Scan(&document); err != nil {
			return nil, err
		}
		collision, err := decodeCollision(document)
		if err != nil {
			return nil, err
		}
		collisions = append(collisions, collision)
	}
	return collisions, rows.Err()
}

func decodeCollision(document []byte) (protocol.CollisionAssessment, error) {
	collision, err := protocol.Decode[protocol.CollisionAssessment](document)
	if err != nil {
		return protocol.CollisionAssessment{}, fmt.Errorf("stored collision assessment: %w", err)
	}
	return collision, nil
}
