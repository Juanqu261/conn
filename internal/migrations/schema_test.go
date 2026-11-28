package migrations_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/stdlib"

	"github.com/Juanqu261/conn/internal/migrations"
	"github.com/Juanqu261/conn/internal/postgres/postgrestest"
)

// Identities used by the schema scenario. A and B are project members; C is
// authenticated but not a member.
const (
	humanA     = "019a8f00-0000-7000-8000-00000000000a"
	humanB     = "019a8f00-0000-7000-8000-00000000000b"
	humanC     = "019a8f00-0000-7000-8000-00000000000c"
	project    = "019a8f00-0001-7000-8000-000000000001"
	project2   = "019a8f00-0001-7000-8000-000000000002"
	objective  = "019a8f00-0002-7000-8000-000000000001"
	objective2 = "019a8f00-0002-7000-8000-000000000002"
	blueprint  = "019a8f00-0003-7000-8000-000000000001"
	blueprint2 = "019a8f00-0003-7000-8000-000000000002"
	cellA      = "019a8f00-0004-7000-8000-000000000001"
	cellB      = "019a8f00-0004-7000-8000-000000000002"
	cellC      = "019a8f00-0004-7000-8000-000000000003"
	decisionA  = "019a8f00-0005-7000-8000-000000000001"
	decision2  = "019a8f00-0005-7000-8000-000000000002"
)

// TestSchemaIntegrity protects the database-level guarantees behind
// committed rows cannot break ownership, own-charter authority,
// revision order, append-only history, or
// gap-free event sequences, while the intended write paths stay possible.
func TestSchemaIntegrity(t *testing.T) {
	ctx := context.Background()
	pool := postgrestest.NewDatabase(t)
	db := stdlib.OpenDBFromPool(pool)
	t.Cleanup(func() { _ = db.Close() })
	if _, err := migrations.Up(ctx, db, slog.New(slog.NewTextHandler(io.Discard, nil))); err != nil {
		t.Fatal(err)
	}

	// The full write path of the first slice must commit, including deferred
	// ownership and current-revision foreign keys.
	if _, err := pool.Exec(ctx, expand(`
		BEGIN;
		INSERT INTO humans VALUES
			('{humanA}', 1, 'alice', now(), now()),
			('{humanB}', 2, 'bob', now(), now()),
			('{humanC}', 3, 'carol', now(), now());
		INSERT INTO projects (project_id, name, owner_human_id, created_at)
			VALUES ('{project}', 'Checkout reliability', '{humanA}', now());
		INSERT INTO project_memberships (project_id, human_id, role, added_by_human_id, added_at) VALUES
			('{project}', '{humanA}', 'owner', '{humanA}', now()),
			('{project}', '{humanB}', 'member', '{humanA}', now());
		INSERT INTO project_event_counters (project_id) VALUES ('{project}');
		INSERT INTO objectives VALUES ('{objective}', '{project}', 1);
		INSERT INTO objective_revisions
			(objective_id, revision, project_id, accountable_human_id, created_at, document)
			VALUES ('{objective}', 1, '{project}', '{humanA}', now(),
				'{"objective_id": "{objective}", "project_id": "{project}", "revision": 1,
				  "accountable_human_id": "{humanA}"}');
		INSERT INTO blueprints (blueprint_id, project_id, current_revision, objective_id)
			VALUES ('{blueprint}', '{project}', 1, '{objective}');
		INSERT INTO blueprint_revisions
			(blueprint_id, revision, project_id, objective_id, objective_revision,
			 proposed_by_human_id, status, created_at, document)
			VALUES ('{blueprint}', 1, '{project}', '{objective}', 1, '{humanA}', 'proposed', now(),
				'{"blueprint_id": "{blueprint}", "project_id": "{project}", "revision": 1,
				  "objective_id": "{objective}", "objective_revision": 1,
				  "proposed_by_human_id": "{humanA}", "cell_charters": []}');
		INSERT INTO blueprint_cells VALUES
			('{blueprint}', 1, '{cellA}', '{project}', '{humanA}'),
			('{blueprint}', 1, '{cellB}', '{project}', '{humanB}');
		INSERT INTO charter_decisions VALUES
			('{decisionA}', '{project}', '{blueprint}', 1, '{cellA}', '{humanA}', 'accept', NULL, now());
		INSERT INTO events (project_id, event_sequence, event_id, event_type, entity_type,
			entity_id, actor_human_id, occurred_at, payload) VALUES
			('{project}', 1, '019a8f00-0006-7000-8000-000000000001', 'project.created', 'project',
			 '{project}', '{humanA}', now(), '{}'),
			('{project}', 2, '019a8f00-0006-7000-8000-000000000002', 'charter.accepted', 'charter_decision',
			 '{decisionA}', '{humanA}', now(), '{}');
		COMMIT;`)); err != nil {
		t.Fatalf("valid write path: %v", err)
	}

	// Each rejected case names the constraint, or the trigger message, that
	// must stop it, so a case cannot pass by failing for an unrelated reason.
	cases := []struct {
		name     string
		rejectBy string // empty when the write must succeed
		sql      string
	}{
		{"a member accepts their own charter", "",
			`INSERT INTO charter_decisions VALUES ('{decision2}', '{project}', '{blueprint}', 1, '{cellB}', '{humanB}', 'accept', NULL, now())`},
		{"a proposed revision becomes active", "",
			`UPDATE blueprint_revisions SET status = 'active'`},
		{"a new proposal supersedes the pending one", "",
			`UPDATE blueprint_revisions SET status = 'superseded';
			 INSERT INTO blueprint_revisions (blueprint_id, revision, project_id, objective_id, objective_revision, proposed_by_human_id, status, created_at, document)
			 VALUES ('{blueprint}', 2, '{project}', '{objective}', 1, '{humanA}', 'proposed', now(), '{revision2}');
			 UPDATE blueprints SET current_revision = 2`},
		{"activating a revision supersedes the active one", "",
			`UPDATE blueprint_revisions SET status = 'active';
			 INSERT INTO blueprint_revisions (blueprint_id, revision, project_id, objective_id, objective_revision, proposed_by_human_id, status, created_at, document)
			 VALUES ('{blueprint}', 2, '{project}', '{objective}', 1, '{humanA}', 'proposed', now(), '{revision2}');
			 UPDATE blueprint_revisions SET status = 'superseded' WHERE revision = 1;
			 UPDATE blueprint_revisions SET status = 'active' WHERE revision = 2`},
		{"the next event sequence is appended", "",
			`INSERT INTO events (project_id, event_sequence, event_id, event_type, entity_type, entity_id, actor_human_id, occurred_at, payload)
			 VALUES ('{project}', 3, '019a8f00-0006-7000-8000-000000000003', 'project.member_added', 'project', '{project}', '{humanA}', now(), '{}')`},

		{"a human cannot decide another human's charter", "charter_decisions_blueprint_id_blueprint_revision_cell_id__fkey",
			`INSERT INTO charter_decisions VALUES ('{decision2}', '{project}', '{blueprint}', 1, '{cellB}', '{humanA}', 'accept', NULL, now())`},
		{"a rejection requires a rationale", "charter_decisions_check",
			`INSERT INTO charter_decisions VALUES ('{decision2}', '{project}', '{blueprint}', 1, '{cellB}', '{humanB}', 'reject', NULL, now())`},
		{"a cell has one decision per revision", "charter_decisions_blueprint_id_blueprint_revision_cell_id_key",
			`INSERT INTO charter_decisions VALUES ('{decision2}', '{project}', '{blueprint}', 1, '{cellA}', '{humanA}', 'reject', 'changed my mind', now())`},
		{"charter decisions are append-only", "rows in charter_decisions are append-only",
			`UPDATE charter_decisions SET outcome = 'reject', rationale = 'overwrite'`},
		{"a non-member cannot be accountable for a cell", "blueprint_cells_project_id_accountable_human_id_fkey",
			`INSERT INTO blueprint_cells VALUES ('{blueprint}', 1, '{cellC}', '{project}', '{humanC}')`},
		{"only an owner proposes a blueprint", "blueprint_revisions_proposer_owner_fkey",
			`UPDATE blueprint_revisions SET status = 'superseded';
			 INSERT INTO blueprint_revisions (blueprint_id, revision, project_id, objective_id, objective_revision, proposed_by_human_id, status, created_at, document)
			 VALUES ('{blueprint}', 2, '{project}', '{objective}', 1, '{humanB}', 'proposed', now(), '{revision2ByB}')`},
		{"a blueprint has one pending proposal", "blueprint_revisions_one_proposed_idx",
			`INSERT INTO blueprint_revisions (blueprint_id, revision, project_id, objective_id, objective_revision, proposed_by_human_id, status, created_at, document)
			 VALUES ('{blueprint}', 2, '{project}', '{objective}', 1, '{humanA}', 'proposed', now(), '{revision2}')`},
		{"a blueprint has one active revision", "blueprint_revisions_one_active_idx",
			`UPDATE blueprint_revisions SET status = 'active';
			 INSERT INTO blueprint_revisions (blueprint_id, revision, project_id, objective_id, objective_revision, proposed_by_human_id, status, created_at, document)
			 VALUES ('{blueprint}', 2, '{project}', '{objective}', 1, '{humanA}', 'proposed', now(), '{revision2}');
			 UPDATE blueprint_revisions SET status = 'active' WHERE revision = 2`},
		{"an objective has one blueprint", "blueprints_objective_id_key",
			`INSERT INTO blueprints (blueprint_id, project_id, current_revision, objective_id) VALUES ('{blueprint2}', '{project}', 1, '{objective}')`},
		{"every revision divides its blueprint's objective", "blueprint_revisions_blueprint_objective_fkey",
			`INSERT INTO objectives VALUES ('{objective2}', '{project}', 1);
			 INSERT INTO objective_revisions (objective_id, revision, project_id, accountable_human_id, created_at, document)
			 VALUES ('{objective2}', 1, '{project}', '{humanA}', now(), '{"objective_id": "{objective2}", "project_id": "{project}", "revision": 1, "accountable_human_id": "{humanA}"}');
			 UPDATE blueprint_revisions SET status = 'superseded';
			 INSERT INTO blueprint_revisions (blueprint_id, revision, project_id, objective_id, objective_revision, proposed_by_human_id, status, created_at, document)
			 VALUES ('{blueprint}', 2, '{project}', '{objective2}', 1, '{humanA}', 'proposed', now(), '{revision2ForObjective2}')`},
		{"blueprint snapshots are immutable", "blueprint revision snapshots are immutable",
			`UPDATE blueprint_revisions SET document = document || '{"cell_charters": [{}]}'`},
		{"blueprint status cannot leave a decided state", "cannot change from rejected to proposed",
			`UPDATE blueprint_revisions SET status = 'rejected'; UPDATE blueprint_revisions SET status = 'proposed'`},
		{"a superseded revision cannot become active", "cannot change from superseded to active",
			`UPDATE blueprint_revisions SET status = 'superseded'; UPDATE blueprint_revisions SET status = 'active'`},
		{"snapshot documents never carry status", "blueprint_revisions_document_check1",
			`UPDATE blueprint_revisions SET status = 'superseded';
			 INSERT INTO blueprint_revisions (blueprint_id, revision, project_id, objective_id, objective_revision, proposed_by_human_id, status, created_at, document)
			 VALUES ('{blueprint}', 2, '{project}', '{objective}', 1, '{humanA}', 'proposed', now(), '{revision2WithStatus}')`},
		{"objective revisions are contiguous", "objective_revisions_objective_id_previous_revision_fkey",
			`INSERT INTO objective_revisions (objective_id, revision, project_id, accountable_human_id, created_at, document)
			 VALUES ('{objective}', 3, '{project}', '{humanA}', now(), '{"objective_id": "{objective}", "project_id": "{project}", "revision": 3, "accountable_human_id": "{humanA}"}')`},
		{"objective revisions are append-only", "rows in objective_revisions are append-only",
			`DELETE FROM objective_revisions`},
		{"event sequences are gap-free", "events_project_id_previous_sequence_fkey",
			`INSERT INTO events (project_id, event_sequence, event_id, event_type, entity_type, entity_id, actor_human_id, occurred_at, payload)
			 VALUES ('{project}', 4, '019a8f00-0006-7000-8000-000000000004', 'project.member_added', 'project', '{project}', '{humanA}', now(), '{}')`},
		{"events are append-only", "rows in events are append-only",
			`DELETE FROM events`},
		{"an event type fixes its entity type", "events_check",
			`INSERT INTO events (project_id, event_sequence, event_id, event_type, entity_type, entity_id, actor_human_id, occurred_at, payload)
			 VALUES ('{project}', 3, '019a8f00-0006-7000-8000-000000000003', 'charter.accepted', 'blueprint', '{blueprint}', '{humanA}', now(), '{}')`},
		{"only an owner adds members", "project_memberships_project_id_added_by_human_id_adder_rol_fkey",
			`INSERT INTO project_memberships (project_id, human_id, role, added_by_human_id, added_at) VALUES ('{project}', '{humanC}', 'member', '{humanB}', now())`},
		{"a project has one owner", "project_memberships_one_owner_idx",
			`INSERT INTO project_memberships (project_id, human_id, role, added_by_human_id, added_at) VALUES ('{project}', '{humanC}', 'owner', '{humanA}', now())`},
		{"a project owner must hold the owner membership", "projects_project_id_owner_human_id_owner_role_fkey",
			`INSERT INTO projects (project_id, name, owner_human_id, created_at) VALUES ('{project2}', 'Orphan', '{humanC}', now())`},
		{"identities must be UUIDv7", "humans_human_id_check",
			`INSERT INTO humans VALUES ('019a8f00-0000-4000-8000-00000000000d', 4, 'dave', now(), now())`},
		{"sessions expire within 30 days", "sessions_check",
			`INSERT INTO sessions VALUES ('019a8f00-0007-7000-8000-000000000001', '{humanA}', decode(repeat('ab', 32), 'hex'), now(), now() + interval '31 days', NULL)`},
		{"browser sessions expire within 7 days", "sessions_browser_lifetime_check",
			`INSERT INTO sessions VALUES ('019a8f00-0007-7000-8000-000000000002', '{humanA}', decode(repeat('cd', 32), 'hex'), now(), now() + interval '8 days', NULL, 'browser')`},
		{"sessions have a known kind", "sessions_kind_check",
			`INSERT INTO sessions VALUES ('019a8f00-0007-7000-8000-000000000003', '{humanA}', decode(repeat('ef', 32), 'hex'), now(), now() + interval '1 day', NULL, 'ci')`},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// Each case runs in its own transaction, which is always rolled
			// back. Deferred constraints are forced to run before rollback.
			tx, err := pool.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = tx.Rollback(ctx) }()
			_, err = tx.Exec(ctx, expand(tc.sql))
			if err == nil {
				_, err = tx.Exec(ctx, "SET CONSTRAINTS ALL IMMEDIATE")
			}

			if tc.rejectBy == "" {
				if err != nil {
					t.Fatalf("expected the write to succeed: %v", err)
				}
				return
			}
			var pgErr *pgconn.PgError
			if !errors.As(err, &pgErr) || !strings.HasPrefix(pgErr.Code, "23") {
				t.Fatalf("expected an integrity constraint violation, got %v", err)
			}
			if pgErr.ConstraintName != tc.rejectBy && !strings.Contains(pgErr.Message, tc.rejectBy) {
				t.Fatalf("rejected by %q (%s), want %q", pgErr.ConstraintName, pgErr.Message, tc.rejectBy)
			}
		})
	}
}

// expand substitutes the scenario identities into SQL.
func expand(sql string) string {
	replacer := strings.NewReplacer(
		"{humanA}", humanA, "{humanB}", humanB, "{humanC}", humanC,
		"{project2}", project2, "{project}", project,
		"{objective2}", objective2, "{objective}", objective,
		"{blueprint2}", blueprint2, "{blueprint}", blueprint,
		"{cellA}", cellA, "{cellB}", cellB, "{cellC}", cellC,
		"{decisionA}", decisionA, "{decision2}", decision2,
		"{revision2WithStatus}", revisionDocument(2, humanA, `, "status": "proposed"`),
		"{revision2ByB}", revisionDocument(2, humanB, ""),
		"{revision2ForObjective2}", strings.Replace(revisionDocument(2, humanA, ""), objective, objective2, 1),
		"{revision2}", revisionDocument(2, humanA, ""),
	)
	out := replacer.Replace(sql)
	if strings.Contains(out, "{human") || strings.Contains(out, "{cell") {
		panic(fmt.Sprintf("unexpanded placeholder in %q", out))
	}
	return out
}

// revisionDocument returns a minimal BlueprintRevision snapshot whose
// identity fields match the typed columns.
func revisionDocument(revision int, proposer, extra string) string {
	return fmt.Sprintf(`{"blueprint_id": %q, "project_id": %q, "revision": %d, "objective_id": %q, "objective_revision": 1, "proposed_by_human_id": %q, "cell_charters": []%s}`,
		blueprint, project, revision, objective, proposer, extra)
}
