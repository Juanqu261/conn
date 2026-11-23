package migrations_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"

	"github.com/Juanqu261/conn/internal/migrations"
	"github.com/Juanqu261/conn/internal/postgres/postgrestest"
)

const (
	semanticHuman       = "019a8f00-0100-7000-8000-000000000100"
	semanticProject     = "019a8f00-0101-7000-8000-000000000101"
	semanticObjective   = "019a8f00-0102-7000-8000-000000000102"
	semanticRepository  = "019a8f00-0103-7000-8000-000000000103"
	semanticParticipant = "019a8f00-0104-7000-8000-000000000104"
	semanticIntent      = "019a8f00-0105-7000-8000-000000000105"
	semanticClaim       = "019a8f00-0106-7000-8000-000000000106"
	semanticCollision   = "019a8f00-0107-7000-8000-000000000107"
	semanticProposal    = "019a8f00-0108-7000-8000-000000000108"
	semanticDecision    = "019a8f00-0109-7000-8000-000000000109"
	semanticEvidence    = "019a8f00-0110-7000-8000-000000000110"
)

// TestSemanticLedgerSchema protects the semantic-ledger database invariants.
// It needs a running local PostgreSQL instance and is therefore skipped by
// postgrestest outside CI when one is unavailable.
func TestSemanticLedgerSchema(t *testing.T) {
	ctx := context.Background()
	pool := postgrestest.NewDatabase(t)
	db := stdlib.OpenDBFromPool(pool)
	t.Cleanup(func() { _ = db.Close() })
	if _, err := migrations.Up(ctx, db, slog.New(slog.NewTextHandler(io.Discard, nil))); err != nil {
		t.Fatal(err)
	}
	seedSemanticLedger(t, ctx, pool)

	assertRejects := func(name, rejectedBy, sql string) {
		t.Helper()
		t.Run(name, func(t *testing.T) {
			tx, err := pool.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = tx.Rollback(ctx) }()
			_, err = tx.Exec(ctx, sql)
			if err == nil {
				_, err = tx.Exec(ctx, "SET CONSTRAINTS ALL IMMEDIATE")
			}
			var pgErr *pgconn.PgError
			if !errors.As(err, &pgErr) || !strings.HasPrefix(pgErr.Code, "23") {
				t.Fatalf("expected integrity violation, got %v", err)
			}
			if pgErr.ConstraintName != rejectedBy && !strings.Contains(pgErr.Message, rejectedBy) {
				t.Fatalf("rejected by %q (%s), want %q", pgErr.ConstraintName, pgErr.Message, rejectedBy)
			}
		})
	}

	assertRejects("claims cannot be rewritten", "rows in semantic_claims are append-only",
		`UPDATE semantic_claims SET display_name = 'rewritten'`)
	assertRejects("a resolution proposal has exactly one proposer", "resolution_proposals_check",
		`INSERT INTO resolution_proposals (proposal_id, project_id, collision_id, kind, rationale_markdown, created_at)
		 VALUES ('019a8f00-0111-7000-8000-000000000111', '`+semanticProject+`', '`+semanticCollision+`', 'dependency', 'Missing author.', now())`)
	assertRejects("accepted risk is binding", "resolution_decisions_check",
		`INSERT INTO resolution_decisions (decision_id, project_id, collision_id, decided_by_human_id, kind, binding, rationale_markdown, created_at)
		 VALUES ('019a8f00-0112-7000-8000-000000000112', '`+semanticProject+`', '`+semanticCollision+`', '`+semanticHuman+`', 'accepted_risk', false, 'Risk is accepted.', now())`)
	assertRejects("collision matching claims stay inside its project", "collision_claim_matches_collision_id_project_id_fkey",
		`INSERT INTO collision_claim_matches (collision_id, claim_id, project_id)
		 VALUES ('019a8f00-0117-7000-8000-000000000117', '`+semanticClaim+`', '019a8f00-0199-7000-8000-000000000199')`)
	assertRejects("semantic event types fix their entity", "events_check",
		`INSERT INTO events (project_id, event_sequence, event_id, event_type, entity_type, entity_id, actor_human_id, occurred_at, payload)
		 VALUES ('`+semanticProject+`', 2, '019a8f00-0113-7000-8000-000000000113', 'intent.published', 'repository', '`+semanticRepository+`', '`+semanticHuman+`', now(), '{}')`)

	t.Run("semantic state and event roll back together", func(t *testing.T) {
		tx, err := pool.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = tx.Rollback(ctx) }()
		if _, err := tx.Exec(ctx, `
			INSERT INTO evidence_references
				(evidence_id, project_id, intent_id, objective_id, objective_revision, kind, state, locator, observed_at, document)
			VALUES
				('019a8f00-0114-7000-8000-000000000114', $1, $2, $3, 1, 'ci_run', 'passing', 'run-114', now(),
				 '{"evidence_id":"019a8f00-0114-7000-8000-000000000114","project_id":"019a8f00-0101-7000-8000-000000000101","objective_id":"019a8f00-0102-7000-8000-000000000102","objective_revision":1,"observed_at":"2026-11-22T09:00:00Z"}');
			UPDATE project_event_counters SET last_event_sequence = 2 WHERE project_id = $1;
			INSERT INTO events (project_id, event_sequence, event_id, event_type, entity_type, entity_id, actor_human_id, occurred_at, payload)
			VALUES ($1, 2, '019a8f00-0115-7000-8000-000000000115', 'evidence.attached', 'evidence_reference',
				'019a8f00-0114-7000-8000-000000000114', $4, now(), '{}');
			INSERT INTO repository_connections (repository_id, project_id, host_kind, canonical_remote, analysis_enabled, created_by_human_id, created_at)
			VALUES ('019a8f00-0116-7000-8000-000000000116', $1, 'github', 'github.com/acme/payments', true, $4, now())`,
			semanticProject, semanticIntent, semanticObjective, semanticHuman); err == nil {
			t.Fatal("expected duplicate repository failure")
		}
		if err := tx.Rollback(ctx); err != nil {
			t.Fatal(err)
		}
		var evidenceCount, eventCount, cursor int
		if err := pool.QueryRow(ctx, `
			SELECT
				(SELECT count(*) FROM evidence_references WHERE evidence_id = '019a8f00-0114-7000-8000-000000000114'),
				(SELECT count(*) FROM events WHERE event_id = '019a8f00-0115-7000-8000-000000000115'),
				(SELECT last_event_sequence FROM project_event_counters WHERE project_id = $1)`, semanticProject).
			Scan(&evidenceCount, &eventCount, &cursor); err != nil {
			t.Fatal(err)
		}
		if evidenceCount != 0 || eventCount != 0 || cursor != 1 {
			t.Fatalf("rollback retained semantic state: evidence=%d event=%d cursor=%d", evidenceCount, eventCount, cursor)
		}
	})
}

func seedSemanticLedger(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	_, err := pool.Exec(ctx, `
		BEGIN;
		INSERT INTO humans VALUES ('`+semanticHuman+`', 100, 'semantic-owner', now(), now());
		INSERT INTO projects (project_id, name, owner_human_id, created_at)
		VALUES ('`+semanticProject+`', 'Semantic ledger', '`+semanticHuman+`', now());
		INSERT INTO project_memberships (project_id, human_id, role, added_by_human_id, added_at)
		VALUES ('`+semanticProject+`', '`+semanticHuman+`', 'owner', '`+semanticHuman+`', now());
		INSERT INTO project_event_counters (project_id, last_event_sequence) VALUES ('`+semanticProject+`', 1);
		INSERT INTO objectives VALUES ('`+semanticObjective+`', '`+semanticProject+`', 1);
		INSERT INTO objective_revisions (objective_id, revision, project_id, accountable_human_id, created_at, document)
		VALUES ('`+semanticObjective+`', 1, '`+semanticProject+`', '`+semanticHuman+`', now(),
			'{"objective_id":"`+semanticObjective+`","project_id":"`+semanticProject+`","revision":1,"accountable_human_id":"`+semanticHuman+`"}');
		INSERT INTO repository_connections (repository_id, project_id, host_kind, canonical_remote, analysis_enabled, created_by_human_id, created_at)
		VALUES ('`+semanticRepository+`', '`+semanticProject+`', 'github', 'github.com/acme/payments', true, '`+semanticHuman+`', now());
		INSERT INTO participants (participant_id, project_id, human_id, adapter_kind, created_at)
		VALUES ('`+semanticParticipant+`', '`+semanticProject+`', '`+semanticHuman+`', 'kiro', now());
		INSERT INTO intents (intent_id, project_id, participant_id, status, current_revision, created_at)
		VALUES ('`+semanticIntent+`', '`+semanticProject+`', '`+semanticParticipant+`', 'active', 1, now());
		INSERT INTO intent_revisions
			(intent_id, revision, project_id, participant_id, objective_id, objective_revision, repository_id, created_at, document)
		VALUES ('`+semanticIntent+`', 1, '`+semanticProject+`', '`+semanticParticipant+`', '`+semanticObjective+`', 1, '`+semanticRepository+`', now(),
			'{"intent_id":"`+semanticIntent+`","project_id":"`+semanticProject+`","participant_id":"`+semanticParticipant+`","revision":1,"objective_id":"`+semanticObjective+`","objective_revision":1,"summary_markdown":"Coordinate retries.","created_at":"2026-11-22T09:00:00Z"}');
		INSERT INTO intent_revisions
			(intent_id, revision, project_id, participant_id, objective_id, objective_revision, repository_id, created_at, document)
		VALUES ('`+semanticIntent+`', 2, '`+semanticProject+`', '`+semanticParticipant+`', '`+semanticObjective+`', 1, '`+semanticRepository+`', now(),
			'{"intent_id":"`+semanticIntent+`","project_id":"`+semanticProject+`","participant_id":"`+semanticParticipant+`","revision":2,"objective_id":"`+semanticObjective+`","objective_revision":1,"summary_markdown":"Coordinate retry cache.","created_at":"2026-11-22T09:01:00Z"}');
		INSERT INTO semantic_claims
			(claim_id, project_id, intent_id, intent_revision, subject_kind, normalized_subject, display_name, relation, provenance_kind, observed_at, document)
		VALUES ('`+semanticClaim+`', '`+semanticProject+`', '`+semanticIntent+`', 1, 'invariant', 'invariant:checkout:no-double-charge', 'No duplicate charge', 'affects', 'declared', now(),
			'{"claim_id":"`+semanticClaim+`","project_id":"`+semanticProject+`","intent_id":"`+semanticIntent+`","intent_revision":1,"subject_kind":"invariant","normalized_subject":"invariant:checkout:no-double-charge","display_name":"No duplicate charge","relation":"affects","provenance_kind":"declared","observed_at":"2026-11-22T09:00:00Z"}');
		INSERT INTO collision_assessments
			(collision_id, project_id, left_intent_id, left_intent_revision, right_intent_id, right_intent_revision, rule_id, severity, confidence, state, explanation_markdown, created_at, document)
		VALUES ('`+semanticCollision+`', '`+semanticProject+`', '`+semanticIntent+`', 1, '`+semanticIntent+`', 2, 'shared_declared_invariant', 'amber', 1, 'open', 'A later intent revision is used only to exercise the relation.', now(),
			'{"collision_id":"`+semanticCollision+`","project_id":"`+semanticProject+`","created_at":"2026-11-22T09:00:00Z"}');
		INSERT INTO collision_claim_matches (collision_id, claim_id, project_id)
		VALUES ('`+semanticCollision+`', '`+semanticClaim+`', '`+semanticProject+`');
		INSERT INTO resolution_proposals
			(proposal_id, project_id, collision_id, proposed_by_participant_id, kind, rationale_markdown, created_at)
		VALUES ('`+semanticProposal+`', '`+semanticProject+`', '`+semanticCollision+`', '`+semanticParticipant+`', 'dependency', 'Use a shared retry contract.', now());
		INSERT INTO resolution_decisions
			(decision_id, project_id, collision_id, selected_proposal_id, decided_by_human_id, kind, binding, rationale_markdown, created_at)
		VALUES ('`+semanticDecision+`', '`+semanticProject+`', '`+semanticCollision+`', '`+semanticProposal+`', '`+semanticHuman+`', 'dependency', false, 'Sequence the two changes.', now());
		INSERT INTO evidence_references
			(evidence_id, project_id, intent_id, objective_id, objective_revision, kind, state, locator, observed_at, document)
		VALUES ('`+semanticEvidence+`', '`+semanticProject+`', '`+semanticIntent+`', '`+semanticObjective+`', 1, 'ci_run', 'passing', 'run-110', now(),
			'{"evidence_id":"`+semanticEvidence+`","project_id":"`+semanticProject+`","objective_id":"`+semanticObjective+`","objective_revision":1,"observed_at":"2026-11-22T09:00:00Z"}');
		INSERT INTO events (project_id, event_sequence, event_id, event_type, entity_type, entity_id, actor_human_id, occurred_at, payload)
		VALUES ('`+semanticProject+`', 1, '019a8f00-0198-7000-8000-000000000198', 'repository.connected', 'repository', '`+semanticRepository+`', '`+semanticHuman+`', now(), '{}');
		COMMIT;`)
	if err != nil {
		t.Fatalf("seed semantic ledger: %v", err)
	}
}
