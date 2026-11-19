package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Juanqu261/conn/internal/application"
	"github.com/Juanqu261/conn/internal/protocol"
)

// ProjectStore implements application.Store.
type ProjectStore struct {
	Pool *pgxpool.Pool
}

var _ application.Store = ProjectStore{}

// Write runs fn at READ COMMITTED. Conflicting inserts use ON CONFLICT DO
// NOTHING or conditional updates, so a lost race leaves the transaction
// usable and later statements see the winner's committed rows.
func (s ProjectStore) Write(ctx context.Context, fn func(application.Tx) error) error {
	return pgx.BeginTxFunc(ctx, s.Pool, pgx.TxOptions{}, func(tx pgx.Tx) error {
		return fn(projectTx{tx})
	})
}

// Read runs fn in a read-only REPEATABLE READ transaction, so every
// statement and the reported event cursor observe one snapshot.
func (s ProjectStore) Read(ctx context.Context, fn func(application.Tx) error) error {
	options := pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly}
	return pgx.BeginTxFunc(ctx, s.Pool, options, func(tx pgx.Tx) error {
		return fn(projectTx{tx})
	})
}

type projectTx struct {
	tx pgx.Tx
}

var _ application.Tx = projectTx{}

func (t projectTx) Role(ctx context.Context, projectID, humanID uuid.UUID) (protocol.Role, error) {
	var role string
	err := t.tx.QueryRow(ctx, `
		SELECT role FROM project_memberships WHERE project_id = $1 AND human_id = $2`,
		projectID, humanID,
	).Scan(&role)
	return protocol.Role(role), notFound(err)
}

func (t projectTx) HumanExists(ctx context.Context, humanID uuid.UUID) (bool, error) {
	var exists bool
	err := t.tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM humans WHERE human_id = $1)`, humanID).Scan(&exists)
	return exists, err
}

func (t projectTx) InsertProject(ctx context.Context, project protocol.Project) (bool, error) {
	tag, err := t.tx.Exec(ctx, `
		INSERT INTO projects (project_id, name, owner_human_id, created_at)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (project_id) DO NOTHING`,
		project.ProjectID, project.Name, project.OwnerHumanID, project.CreatedAt.Time())
	if err != nil || tag.RowsAffected() == 0 {
		return false, err
	}
	if _, err := t.tx.Exec(ctx, `
		INSERT INTO project_memberships (project_id, human_id, role, added_by_human_id, added_at)
		VALUES ($1, $2, 'owner', $2, $3)`,
		project.ProjectID, project.OwnerHumanID, project.CreatedAt.Time()); err != nil {
		return false, err
	}
	if _, err := t.tx.Exec(ctx, `
		INSERT INTO project_event_counters (project_id) VALUES ($1)`, project.ProjectID); err != nil {
		return false, err
	}
	return true, nil
}

func (t projectTx) Project(ctx context.Context, projectID uuid.UUID) (protocol.Project, error) {
	var project protocol.Project
	var createdAt time.Time
	err := t.tx.QueryRow(ctx, `
		SELECT project_id, name, owner_human_id, created_at FROM projects WHERE project_id = $1`,
		projectID,
	).Scan(&project.ProjectID, &project.Name, &project.OwnerHumanID, &createdAt)
	project.CreatedAt = protocol.NewTimestamp(createdAt)
	return project, notFound(err)
}

func (t projectTx) ProjectsForHuman(ctx context.Context, humanID uuid.UUID) ([]protocol.ProjectListEntry, error) {
	rows, err := t.tx.Query(ctx, `
		SELECT p.project_id, p.name, p.owner_human_id, p.created_at, m.role
		FROM project_memberships m
		JOIN projects p ON p.project_id = m.project_id
		WHERE m.human_id = $1
		ORDER BY p.created_at, p.project_id`, humanID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	projects := []protocol.ProjectListEntry{}
	for rows.Next() {
		var entry protocol.ProjectListEntry
		var createdAt time.Time
		var role string
		if err := rows.Scan(&entry.Project.ProjectID, &entry.Project.Name, &entry.Project.OwnerHumanID, &createdAt, &role); err != nil {
			return nil, err
		}
		entry.Project.CreatedAt = protocol.NewTimestamp(createdAt)
		entry.Role = protocol.Role(role)
		projects = append(projects, entry)
	}
	return projects, rows.Err()
}

func (t projectTx) InsertMembership(ctx context.Context, m protocol.ProjectMembership) (bool, error) {
	tag, err := t.tx.Exec(ctx, `
		INSERT INTO project_memberships (project_id, human_id, role, added_by_human_id, added_at)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (project_id, human_id) DO NOTHING`,
		m.ProjectID, m.HumanID, string(m.Role), m.AddedByHumanID, m.AddedAt.Time())
	return err == nil && tag.RowsAffected() == 1, err
}

func (t projectTx) Membership(ctx context.Context, projectID, humanID uuid.UUID) (protocol.ProjectMembership, error) {
	var m protocol.ProjectMembership
	var role string
	var addedAt time.Time
	err := t.tx.QueryRow(ctx, `
		SELECT project_id, human_id, role, added_by_human_id, added_at
		FROM project_memberships WHERE project_id = $1 AND human_id = $2`,
		projectID, humanID,
	).Scan(&m.ProjectID, &m.HumanID, &role, &m.AddedByHumanID, &addedAt)
	m.Role = protocol.Role(role)
	m.AddedAt = protocol.NewTimestamp(addedAt)
	return m, notFound(err)
}

func (t projectTx) ProjectMembers(ctx context.Context, projectID uuid.UUID) ([]protocol.ProjectMember, error) {
	rows, err := t.tx.Query(ctx, `
		SELECT h.human_id, h.github_user_id, h.github_login,
			m.project_id, m.human_id, m.role, m.added_by_human_id, m.added_at
		FROM project_memberships m
		JOIN humans h ON h.human_id = m.human_id
		WHERE m.project_id = $1
		ORDER BY m.added_at, m.human_id`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	members := []protocol.ProjectMember{}
	for rows.Next() {
		var member protocol.ProjectMember
		var role string
		var addedAt time.Time
		if err := rows.Scan(
			&member.Human.HumanID, &member.Human.GitHubUserID, &member.Human.GitHubLogin,
			&member.Membership.ProjectID, &member.Membership.HumanID, &role, &member.Membership.AddedByHumanID, &addedAt,
		); err != nil {
			return nil, err
		}
		member.Membership.Role = protocol.Role(role)
		member.Membership.AddedAt = protocol.NewTimestamp(addedAt)
		members = append(members, member)
	}
	return members, rows.Err()
}

func (t projectTx) InsertObjectiveRevision(ctx context.Context, o protocol.Objective) (bool, error) {
	document, err := protocol.Encode(o)
	if err != nil {
		return false, err
	}
	// The conditional update locks the current-revision row, so concurrent
	// writers of the same next revision serialize and only one succeeds.
	query := `
		UPDATE objectives SET current_revision = $3
		WHERE objective_id = $1 AND project_id = $2 AND current_revision = $3 - 1`
	if o.Revision == 1 {
		query = `
			INSERT INTO objectives (objective_id, project_id, current_revision)
			VALUES ($1, $2, $3)
			ON CONFLICT (objective_id) DO NOTHING`
	}
	tag, err := t.tx.Exec(ctx, query, o.ObjectiveID, o.ProjectID, o.Revision)
	if err != nil || tag.RowsAffected() == 0 {
		return false, err
	}
	_, err = t.tx.Exec(ctx, `
		INSERT INTO objective_revisions
			(objective_id, revision, project_id, accountable_human_id, created_at, document)
		VALUES ($1, $2, $3, $4, $5, $6)`,
		o.ObjectiveID, o.Revision, o.ProjectID, o.AccountableHumanID, o.CreatedAt.Time(), document)
	return err == nil, err
}

func (t projectTx) ObjectiveRevision(ctx context.Context, projectID, objectiveID uuid.UUID, revision int) (protocol.Objective, error) {
	var document []byte
	err := t.tx.QueryRow(ctx, `
		SELECT document FROM objective_revisions
		WHERE project_id = $1 AND objective_id = $2 AND revision = $3`,
		projectID, objectiveID, revision,
	).Scan(&document)
	if err != nil {
		return protocol.Objective{}, notFound(err)
	}
	return protocol.Decode[protocol.Objective](document)
}

func (t projectTx) AppendEvent(ctx context.Context, event protocol.EventEnvelope) (int64, error) {
	// Advancing the counter locks it until commit, so sequences commit in
	// order and a rollback returns the sequence unused.
	err := t.tx.QueryRow(ctx, `
		UPDATE project_event_counters SET last_event_sequence = last_event_sequence + 1
		WHERE project_id = $1
		RETURNING last_event_sequence`,
		event.ProjectID,
	).Scan(&event.EventSequence)
	if err != nil {
		return 0, fmt.Errorf("reserve event sequence: %w", err)
	}
	if _, err := protocol.Encode(event); err != nil {
		return 0, err
	}
	payload, err := json.Marshal(event.Payload)
	if err != nil {
		return 0, err
	}
	_, err = t.tx.Exec(ctx, `
		INSERT INTO events (project_id, event_sequence, event_id, event_type, entity_type,
			entity_id, actor_human_id, occurred_at, payload)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
		event.ProjectID, event.EventSequence, event.EventID,
		string(event.EventType()), string(event.EntityType()),
		event.EntityID, event.ActorHumanID, event.OccurredAt.Time(), payload)
	if err != nil {
		return 0, err
	}
	return event.EventSequence, nil
}

func (t projectTx) LastEvent(ctx context.Context, projectID uuid.UUID) (int64, error) {
	var sequence int64
	err := t.tx.QueryRow(ctx, `
		SELECT last_event_sequence FROM project_event_counters WHERE project_id = $1`,
		projectID,
	).Scan(&sequence)
	return sequence, notFound(err)
}

// storedEvent mirrors the event-envelope wire form, so stored rows are
// decoded and schema-validated by protocol.Decode.
type storedEvent struct {
	ProjectID     uuid.UUID          `json:"project_id"`
	EventSequence int64              `json:"event_sequence"`
	EventID       uuid.UUID          `json:"event_id"`
	EventType     string             `json:"event_type"`
	EntityType    string             `json:"entity_type"`
	EntityID      uuid.UUID          `json:"entity_id"`
	ActorHumanID  uuid.UUID          `json:"actor_human_id"`
	OccurredAt    protocol.Timestamp `json:"occurred_at"`
	Payload       json.RawMessage    `json:"payload"`
}

func (t projectTx) Events(ctx context.Context, projectID uuid.UUID, after int64, limit int) ([]protocol.EventEnvelope, error) {
	rows, err := t.tx.Query(ctx, `
		SELECT project_id, event_sequence, event_id, event_type, entity_type,
			entity_id, actor_human_id, occurred_at, payload
		FROM events
		WHERE project_id = $1 AND event_sequence > $2
		ORDER BY event_sequence
		LIMIT $3`,
		projectID, after, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	events := []protocol.EventEnvelope{}
	for rows.Next() {
		var row storedEvent
		var occurredAt time.Time
		if err := rows.Scan(&row.ProjectID, &row.EventSequence, &row.EventID, &row.EventType, &row.EntityType,
			&row.EntityID, &row.ActorHumanID, &occurredAt, &row.Payload); err != nil {
			return nil, err
		}
		row.OccurredAt = protocol.NewTimestamp(occurredAt)
		wire, err := json.Marshal(row)
		if err != nil {
			return nil, err
		}
		event, err := protocol.Decode[protocol.EventEnvelope](wire)
		if err != nil {
			return nil, fmt.Errorf("stored event %d: %w", row.EventSequence, err)
		}
		events = append(events, event)
	}
	return events, rows.Err()
}

// notFound maps a missing row to application.ErrNotFound.
func notFound(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return application.ErrNotFound
	}
	return err
}
