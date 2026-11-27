package postgres

import (
	"context"
	"fmt"
	"time"

	"uuid"

	"github.com/Juanqu261/conn/internal/application"
	"github.com/Juanqu261/conn/internal/protocol"
)

func (t projectTx) InsertCombinationCheck(ctx context.Context, check protocol.CombinationCheck) error {
	document, err := protocol.Encode(check)
	if err != nil {
		return err
	}
	_, err = t.tx.Exec(ctx, `
		INSERT INTO combination_checks
			(check_id, project_id, collision_id, outcome, producer_participant_id, created_at, document)
		VALUES ($1,$2,$3,$4,$5,$6,$7)`,
		check.CheckID, check.ProjectID, check.CollisionID, check.Outcome, check.ProducerParticipantID, check.CreatedAt.Time(), document)
	return err
}

func (t projectTx) CombinationChecks(ctx context.Context, projectID, collisionID uuid.UUID, limit int) ([]protocol.CombinationCheck, error) {
	rows, err := t.tx.Query(ctx, `
		SELECT document FROM combination_checks
		WHERE project_id = $1 AND collision_id = $2
		ORDER BY created_at DESC, check_id DESC
		LIMIT $3`, projectID, collisionID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	checks := []protocol.CombinationCheck{}
	for rows.Next() {
		var document []byte
		if err := rows.Scan(&document); err != nil {
			return nil, err
		}
		check, err := protocol.Decode[protocol.CombinationCheck](document)
		if err != nil {
			return nil, fmt.Errorf("stored combination check: %w", err)
		}
		checks = append(checks, check)
	}
	return checks, rows.Err()
}

func (t projectTx) LatestCombinationOutcomes(ctx context.Context, projectID uuid.UUID, collisionIDs []uuid.UUID) (map[uuid.UUID]string, error) {
	rows, err := t.tx.Query(ctx, `
		SELECT DISTINCT ON (collision_id) collision_id, outcome
		FROM combination_checks
		WHERE project_id = $1 AND collision_id = ANY($2)
		ORDER BY collision_id, created_at DESC, check_id DESC`, projectID, collisionIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	outcomes := map[uuid.UUID]string{}
	for rows.Next() {
		var collisionID uuid.UUID
		var outcome string
		if err := rows.Scan(&collisionID, &outcome); err != nil {
			return nil, err
		}
		outcomes[collisionID] = outcome
	}
	return outcomes, rows.Err()
}

func (t projectTx) InsertCICredential(ctx context.Context, credential protocol.CICredential, tokenHash [32]byte) error {
	_, err := t.tx.Exec(ctx, `
		INSERT INTO ci_credentials
			(credential_id, project_id, repository_id, label, token_sha256, created_by_human_id, created_at, expires_at)
		VALUES ($1,$2,$3,NULLIF($4,''),$5,$6,$7,$8)`,
		credential.CredentialID, credential.ProjectID, credential.RepositoryID, credential.Label, tokenHash[:],
		credential.CreatedByHumanID, credential.CreatedAt.Time(), credential.ExpiresAt.Time())
	return err
}

const ciCredentialColumns = `c.credential_id, c.project_id, c.repository_id, c.label, c.created_by_human_id,
	c.created_at, c.expires_at, c.last_used_at, c.revoked_at`

type rowScanner interface {
	Scan(dest ...any) error
}

func scanCICredential(row rowScanner, extra ...any) (protocol.CICredential, error) {
	var credential protocol.CICredential
	var label *string
	var created, expires time.Time
	var lastUsed, revoked *time.Time
	dest := append([]any{&credential.CredentialID, &credential.ProjectID, &credential.RepositoryID, &label,
		&credential.CreatedByHumanID, &created, &expires, &lastUsed, &revoked}, extra...)
	if err := row.Scan(dest...); err != nil {
		return protocol.CICredential{}, err
	}
	if label != nil {
		credential.Label = *label
	}
	credential.CreatedAt, credential.ExpiresAt = protocol.NewTimestamp(created), protocol.NewTimestamp(expires)
	if lastUsed != nil {
		at := protocol.NewTimestamp(*lastUsed)
		credential.LastUsedAt = &at
	}
	if revoked != nil {
		at := protocol.NewTimestamp(*revoked)
		credential.RevokedAt = &at
	}
	return credential, nil
}

func (t projectTx) CICredentials(ctx context.Context, projectID uuid.UUID, limit int) ([]protocol.CICredential, error) {
	rows, err := t.tx.Query(ctx, `
		SELECT `+ciCredentialColumns+` FROM ci_credentials c
		WHERE c.project_id = $1
		ORDER BY c.created_at DESC, c.credential_id DESC
		LIMIT $2`, projectID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	credentials := []protocol.CICredential{}
	for rows.Next() {
		credential, err := scanCICredential(rows)
		if err != nil {
			return nil, err
		}
		credentials = append(credentials, credential)
	}
	return credentials, rows.Err()
}

func (t projectTx) CICredential(ctx context.Context, projectID, credentialID uuid.UUID) (protocol.CICredential, error) {
	credential, err := scanCICredential(t.tx.QueryRow(ctx, `
		SELECT `+ciCredentialColumns+` FROM ci_credentials c
		WHERE c.project_id = $1 AND c.credential_id = $2`, projectID, credentialID))
	return credential, notFound(err)
}

func (t projectTx) RevokeCICredential(ctx context.Context, projectID, credentialID uuid.UUID, at time.Time) (bool, error) {
	tag, err := t.tx.Exec(ctx, `
		UPDATE ci_credentials SET revoked_at = $3
		WHERE project_id = $1 AND credential_id = $2 AND revoked_at IS NULL`, projectID, credentialID, at)
	if err != nil {
		return false, err
	}
	if tag.RowsAffected() == 1 {
		return true, nil
	}
	if _, err := t.CICredential(ctx, projectID, credentialID); err != nil {
		return false, err
	}
	return false, nil
}

func (t projectTx) CICredentialByTokenHash(ctx context.Context, tokenHash [32]byte) (protocol.CICredential, protocol.RepositoryConnection, error) {
	var repository protocol.RepositoryConnection
	var defaultBranch *string
	credential, err := scanCICredential(t.tx.QueryRow(ctx, `
		SELECT `+ciCredentialColumns+`, r.canonical_remote, r.default_branch
		FROM ci_credentials c
		JOIN repository_connections r ON r.project_id = c.project_id AND r.repository_id = c.repository_id
		WHERE c.token_sha256 = $1`, tokenHash[:]), &repository.CanonicalRemote, &defaultBranch)
	if err != nil {
		return protocol.CICredential{}, protocol.RepositoryConnection{}, notFound(err)
	}
	repository.RepositoryID, repository.ProjectID = credential.RepositoryID, credential.ProjectID
	if defaultBranch != nil {
		repository.DefaultBranch = *defaultBranch
	}
	return credential, repository, nil
}

func (t projectTx) TouchCICredential(ctx context.Context, credentialID uuid.UUID, at, notBefore time.Time) error {
	_, err := t.tx.Exec(ctx, `
		UPDATE ci_credentials SET last_used_at = $2
		WHERE credential_id = $1 AND (last_used_at IS NULL OR last_used_at < $3)`, credentialID, at, notBefore)
	return err
}

func (t projectTx) InsightCounts(ctx context.Context, projectID uuid.UUID, since, until time.Time) (application.InsightCounts, error) {
	counts := application.InsightCounts{OpenedByRule: map[string]int{}, CombinationOutcomes: map[string]int{}}
	rows, err := t.tx.Query(ctx, `
		SELECT a.rule_id, count(*) FROM events e
		JOIN collision_assessments a ON a.project_id = e.project_id AND a.collision_id = e.entity_id
		WHERE e.project_id = $1 AND e.event_type = 'collision.opened' AND e.occurred_at >= $2 AND e.occurred_at <= $3
		GROUP BY a.rule_id`, projectID, since, until)
	if err != nil {
		return counts, err
	}
	for rows.Next() {
		var rule string
		var count int
		if err := rows.Scan(&rule, &count); err != nil {
			rows.Close()
			return counts, err
		}
		counts.OpenedByRule[rule] = count
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return counts, err
	}

	rows, err = t.tx.Query(ctx, `
		SELECT a.rule_id, d.kind, count(*) FROM resolution_decisions d
		JOIN collision_assessments a ON a.project_id = d.project_id AND a.collision_id = d.collision_id
		WHERE d.project_id = $1 AND d.created_at >= $2 AND d.created_at <= $3
		GROUP BY a.rule_id, d.kind`, projectID, since, until)
	if err != nil {
		return counts, err
	}
	for rows.Next() {
		var fact application.InsightDecisionFact
		if err := rows.Scan(&fact.RuleID, &fact.Kind, &fact.Count); err != nil {
			rows.Close()
			return counts, err
		}
		counts.Decisions = append(counts.Decisions, fact)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return counts, err
	}

	// The first decision of each collision, and whether either Intent was
	// released before it.
	rows, err = t.tx.Query(ctx, `
		WITH first AS (
			SELECT collision_id, min(created_at) AS decided_at
			FROM resolution_decisions WHERE project_id = $1
			GROUP BY collision_id
		)
		SELECT a.rule_id, a.created_at, f.decided_at, EXISTS (
			SELECT 1 FROM events e
			WHERE e.project_id = $1 AND e.event_type = 'intent.released'
				AND e.entity_id IN (a.left_intent_id, a.right_intent_id) AND e.occurred_at <= f.decided_at
		)
		FROM first f
		JOIN collision_assessments a ON a.project_id = $1 AND a.collision_id = f.collision_id
		WHERE f.decided_at >= $2 AND f.decided_at <= $3`, projectID, since, until)
	if err != nil {
		return counts, err
	}
	for rows.Next() {
		var fact application.InsightFirstDecision
		if err := rows.Scan(&fact.RuleID, &fact.AssessedAt, &fact.DecidedAt, &fact.ReleasedBeforeFirst); err != nil {
			rows.Close()
			return counts, err
		}
		counts.FirstDecisions = append(counts.FirstDecisions, fact)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return counts, err
	}

	// An analyzed revision has undeclared observed changes when a changed or
	// removed code-symbol or contract subject has no declared claim.
	if err := t.tx.QueryRow(ctx, `
		SELECT count(*), count(*) FILTER (WHERE EXISTS (
			SELECT 1 FROM semantic_claims o
			WHERE o.project_id = r.project_id AND o.intent_id = r.intent_id AND o.intent_revision = r.intent_revision
				AND o.provenance_kind = 'static_analysis' AND o.relation IN ('changes', 'removes')
				AND o.subject_kind IN ('code_symbol', 'contract')
				AND NOT EXISTS (
					SELECT 1 FROM semantic_claims d
					WHERE d.project_id = o.project_id AND d.intent_id = o.intent_id AND d.intent_revision = o.intent_revision
						AND d.provenance_kind = 'declared' AND d.normalized_subject = o.normalized_subject
				)
		))
		FROM intent_analysis_reports r
		WHERE r.project_id = $1 AND r.observed_at >= $2 AND r.observed_at <= $3`, projectID, since, until).
		Scan(&counts.AnalyzedRevisions, &counts.UndeclaredRevisions); err != nil {
		return counts, err
	}

	rows, err = t.tx.Query(ctx, `
		SELECT outcome, count(*) FROM combination_checks
		WHERE project_id = $1 AND created_at >= $2 AND created_at <= $3
		GROUP BY outcome`, projectID, since, until)
	if err != nil {
		return counts, err
	}
	defer rows.Close()
	for rows.Next() {
		var outcome string
		var count int
		if err := rows.Scan(&outcome, &count); err != nil {
			return counts, err
		}
		counts.CombinationOutcomes[outcome] = count
	}
	return counts, rows.Err()
}
