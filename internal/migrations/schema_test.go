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
	humanA    = "019a8f00-0000-7000-8000-00000000000a"
	humanB    = "019a8f00-0000-7000-8000-00000000000b"
	humanC    = "019a8f00-0000-7000-8000-00000000000c"
	project   = "019a8f00-0001-7000-8000-000000000001"
	project2  = "019a8f00-0001-7000-8000-000000000002"
	objective = "019a8f00-0002-7000-8000-000000000001"
	blueprint = "019a8f00-0003-7000-8000-000000000001"
	cellA     = "019a8f00-0004-7000-8000-000000000001"
	cellB     = "019a8f00-0004-7000-8000-000000000002"
	cellC     = "019a8f00-0004-7000-8000-000000000003"
	decisionA = "019a8f00-0005-7000-8000-000000000001"
	decision2 = "019a8f00-0005-7000-8000-000000000002"
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
		INSERT INTO blueprints VALUES ('{blueprint}', '{project}', 1);
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

	cases := []struct {
		name    string
		allowed bool
		sql     string
	}{
		{"a member accepts their own charter", true,
			`INSERT INTO charter_decisions VALUES ('{decision2}', '{project}', '{blueprint}', 1, '{cellB}', '{humanB}', 'accept', NULL, now())`},
		{"a proposed revision becomes active", true,
			`UPDATE blueprint_revisions SET status = 'active'`},
		{"the next event sequence is appended", true,
			`INSERT INTO events (project_id, event_sequence, event_id, event_type, entity_type, entity_id, actor_human_id, occurred_at, payload)
			 VALUES ('{project}', 3, '019a8f00-0006-7000-8000-000000000003', 'project.member_added', 'project', '{project}', '{humanA}', now(), '{}')`},

		{"a human cannot decide another human's charter", false,
			`INSERT INTO charter_decisions VALUES ('{decision2}', '{project}', '{blueprint}', 1, '{cellB}', '{humanA}', 'accept', NULL, now())`},
		{"a rejection requires a rationale", false,
			`INSERT INTO charter_decisions VALUES ('{decision2}', '{project}', '{blueprint}', 1, '{cellB}', '{humanB}', 'reject', NULL, now())`},
		{"a cell has one decision per revision", false,
			`INSERT INTO charter_decisions VALUES ('{decision2}', '{project}', '{blueprint}', 1, '{cellA}', '{humanA}', 'reject', 'changed my mind', now())`},
		{"charter decisions are append-only", false,
			`UPDATE charter_decisions SET outcome = 'reject', rationale = 'overwrite'`},
		{"a non-member cannot be accountable for a cell", false,
			`INSERT INTO blueprint_cells VALUES ('{blueprint}', 1, '{cellC}', '{project}', '{humanC}')`},
		{"blueprint snapshots are immutable", false,
			`UPDATE blueprint_revisions SET document = document || '{"cell_charters": [{}]}'`},
		{"blueprint status cannot leave a decided state", false,
			`UPDATE blueprint_revisions SET status = 'rejected'; UPDATE blueprint_revisions SET status = 'proposed'`},
		{"snapshot documents never carry status", false,
			`INSERT INTO blueprint_revisions (blueprint_id, revision, project_id, objective_id, objective_revision, proposed_by_human_id, status, created_at, document)
			 VALUES ('{blueprint}', 2, '{project}', '{objective}', 1, '{humanA}', 'proposed', now(),
			 '{"blueprint_id": "{blueprint}", "project_id": "{project}", "revision": 2, "objective_id": "{objective}", "objective_revision": 1, "proposed_by_human_id": "{humanA}", "cell_charters": [], "status": "proposed"}')`},
		{"objective revisions are contiguous", false,
			`INSERT INTO objective_revisions (objective_id, revision, project_id, accountable_human_id, created_at, document)
			 VALUES ('{objective}', 3, '{project}', '{humanA}', now(), '{"objective_id": "{objective}", "project_id": "{project}", "revision": 3, "accountable_human_id": "{humanA}"}')`},
		{"objective revisions are append-only", false,
			`DELETE FROM objective_revisions`},
		{"event sequences are gap-free", false,
			`INSERT INTO events (project_id, event_sequence, event_id, event_type, entity_type, entity_id, actor_human_id, occurred_at, payload)
			 VALUES ('{project}', 4, '019a8f00-0006-7000-8000-000000000004', 'project.member_added', 'project', '{project}', '{humanA}', now(), '{}')`},
		{"events are append-only", false,
			`DELETE FROM events`},
		{"an event type fixes its entity type", false,
			`INSERT INTO events (project_id, event_sequence, event_id, event_type, entity_type, entity_id, actor_human_id, occurred_at, payload)
			 VALUES ('{project}', 3, '019a8f00-0006-7000-8000-000000000003', 'charter.accepted', 'blueprint', '{blueprint}', '{humanA}', now(), '{}')`},
		{"only an owner adds members", false,
			`INSERT INTO project_memberships (project_id, human_id, role, added_by_human_id, added_at) VALUES ('{project}', '{humanC}', 'member', '{humanB}', now())`},
		{"a project has one owner", false,
			`INSERT INTO project_memberships (project_id, human_id, role, added_by_human_id, added_at) VALUES ('{project}', '{humanC}', 'owner', '{humanA}', now())`},
		{"a project owner must hold the owner membership", false,
			`INSERT INTO projects (project_id, name, owner_human_id, created_at) VALUES ('{project2}', 'Orphan', '{humanC}', now())`},
		{"identities must be UUIDv7", false,
			`INSERT INTO humans VALUES ('019a8f00-0000-4000-8000-00000000000d', 4, 'dave', now(), now())`},
		{"sessions expire within 30 days", false,
			`INSERT INTO sessions VALUES ('019a8f00-0007-7000-8000-000000000001', '{humanA}', decode(repeat('ab', 32), 'hex'), now(), now() + interval '31 days', NULL)`},
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

			if tc.allowed {
				if err != nil {
					t.Fatalf("expected the write to succeed: %v", err)
				}
				return
			}
			var pgErr *pgconn.PgError
			if !errors.As(err, &pgErr) || !strings.HasPrefix(pgErr.Code, "23") {
				t.Fatalf("expected an integrity constraint violation, got %v", err)
			}
			t.Logf("rejected by %s: %s", pgErr.ConstraintName, pgErr.Message)
		})
	}
}

// expand substitutes the scenario identities into SQL.
func expand(sql string) string {
	replacer := strings.NewReplacer(
		"{humanA}", humanA, "{humanB}", humanB, "{humanC}", humanC,
		"{project2}", project2, "{project}", project,
		"{objective}", objective, "{blueprint}", blueprint,
		"{cellA}", cellA, "{cellB}", cellB, "{cellC}", cellC,
		"{decisionA}", decisionA, "{decision2}", decision2,
	)
	out := replacer.Replace(sql)
	if strings.Contains(out, "{human") || strings.Contains(out, "{cell") {
		panic(fmt.Sprintf("unexpanded placeholder in %q", out))
	}
	return out
}
