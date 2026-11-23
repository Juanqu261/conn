package postgres

import (
	"context"
	"fmt"

	"uuid"

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
			 confidence, state, explanation_markdown, created_at, superseded_at, document)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14)
		ON CONFLICT (project_id, left_intent_id, left_intent_revision,
			right_intent_id, right_intent_revision, rule_id) DO NOTHING`,
		assessment.CollisionID, assessment.ProjectID, assessment.LeftIntentID, assessment.LeftIntentRevision,
		assessment.RightIntentID, assessment.RightIntentRevision, assessment.RuleID, assessment.Severity,
		assessment.Confidence, assessment.State, assessment.ExplanationMarkdown, assessment.CreatedAt.Time(),
		assessment.SupersededAt, document)
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
