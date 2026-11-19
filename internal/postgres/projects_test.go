package postgres_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"slices"
	"testing"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5/stdlib"

	"github.com/Juanqu261/conn/internal/application"
	"github.com/Juanqu261/conn/internal/migrations"
	"github.com/Juanqu261/conn/internal/postgres"
	"github.com/Juanqu261/conn/internal/postgres/postgrestest"
	"github.com/Juanqu261/conn/internal/protocol"
)

// newProjects migrates an empty database and returns the project use cases
// over it, with a function that registers authenticated humans.
func newProjects(t *testing.T) (context.Context, postgres.ProjectStore, *application.Projects, func(int64, string) uuid.UUID) {
	t.Helper()
	ctx := context.Background()
	pool := postgrestest.NewDatabase(t)
	db := stdlib.OpenDBFromPool(pool)
	t.Cleanup(func() { _ = db.Close() })
	if _, err := migrations.Up(ctx, db, slog.New(slog.NewTextHandler(io.Discard, nil))); err != nil {
		t.Fatal(err)
	}
	auth := postgres.AuthStore{Pool: pool}
	newHuman := func(githubID int64, login string) uuid.UUID {
		human, err := auth.UpsertHuman(ctx, uuid.NewV7(), githubID, login, time.Now().UTC())
		if err != nil {
			t.Fatal(err)
		}
		return human.HumanID
	}
	store := postgres.ProjectStore{Pool: pool}
	return ctx, store, &application.Projects{Store: store}, newHuman
}

// TestProjectsAndObjectives protects project authorization and atomic
// state-and-event writes (requirements 2, 3, 8, and 9.1): non-members
// cannot tell a hidden Project from a missing one, members read but cannot
// perform owner-only operations, Objective revisions are immutable and
// guarded by expected revisions, retries create no duplicate events, and a
// failed transaction leaves neither state nor event behind.
func TestProjectsAndObjectives(t *testing.T) {
	ctx, store, projects, newHuman := newProjects(t)
	owner, member, outsider := newHuman(1, "alice"), newHuman(2, "bob"), newHuman(3, "carol")
	projectID, objectiveID := uuid.NewV7(), uuid.NewV7()

	objectiveV1 := protocol.ObjectiveRevisionInput{
		ObjectiveID:        objectiveID,
		Title:              "Reliable checkout",
		OverviewMarkdown:   "Checkout must survive a payment-provider outage.",
		AccountableHumanID: member,
	}
	objectiveV2 := objectiveV1
	objectiveV2.KeyPoints = []protocol.KeyPoint{
		{Key: "no-double-charge", Kind: protocol.KeyPointInvariant, Statement: "A customer is never charged twice."},
	}
	duplicateKeys := objectiveV2
	duplicateKeys.ObjectiveID = uuid.NewV7()
	duplicateKeys.KeyPoints = append(slices.Clone(objectiveV2.KeyPoints), objectiveV2.KeyPoints[0])
	outsiderAccountable := objectiveV1
	outsiderAccountable.ObjectiveID = uuid.NewV7()
	outsiderAccountable.AccountableHumanID = outsider
	staleV2 := objectiveV1
	staleV2.Title = "A concurrent edit"

	create := protocol.ProjectCreate{ProjectID: projectID, Name: "Checkout reliability"}
	renamed := protocol.ProjectCreate{ProjectID: projectID, Name: "Another name"}

	// Each step names the result it must produce: the event cursor after a
	// success, or the error. Steps run in order against one database.
	steps := []struct {
		name      string
		run       func() (int64, error)
		wantEvent int64
		wantErr   error
	}{
		{"the owner creates the project", func() (int64, error) {
			r, err := projects.CreateProject(ctx, owner, create)
			return r.AsOfEvent, err
		}, 1, nil},
		{"retrying project creation appends no event", func() (int64, error) {
			r, err := projects.CreateProject(ctx, owner, create)
			return r.AsOfEvent, err
		}, 1, nil},
		{"reusing a project ID for different content conflicts", func() (int64, error) {
			r, err := projects.CreateProject(ctx, owner, renamed)
			return r.AsOfEvent, err
		}, 0, application.ErrConflict},
		{"a non-member cannot read the project", func() (int64, error) {
			r, err := projects.Project(ctx, member, projectID)
			return r.AsOfEvent, err
		}, 0, application.ErrNotFound},
		{"a missing project looks the same", func() (int64, error) {
			r, err := projects.Project(ctx, member, uuid.NewV7())
			return r.AsOfEvent, err
		}, 0, application.ErrNotFound},
		{"a non-member's owner-only operation reveals nothing", func() (int64, error) {
			r, err := projects.AddMember(ctx, outsider, projectID, outsider)
			return r.AsOfEvent, err
		}, 0, application.ErrNotFound},
		{"the owner adds a member", func() (int64, error) {
			r, err := projects.AddMember(ctx, owner, projectID, member)
			return r.AsOfEvent, err
		}, 2, nil},
		{"re-adding a member appends no event", func() (int64, error) {
			r, err := projects.AddMember(ctx, owner, projectID, member)
			return r.AsOfEvent, err
		}, 2, nil},
		{"only existing humans can be added", func() (int64, error) {
			r, err := projects.AddMember(ctx, owner, projectID, uuid.NewV7())
			return r.AsOfEvent, err
		}, 0, application.ErrInvalid},
		{"a member reads the project", func() (int64, error) {
			r, err := projects.Project(ctx, member, projectID)
			return r.AsOfEvent, err
		}, 2, nil},
		{"a member cannot add members", func() (int64, error) {
			r, err := projects.AddMember(ctx, member, projectID, outsider)
			return r.AsOfEvent, err
		}, 0, application.ErrForbidden},
		{"a member cannot create objectives", func() (int64, error) {
			r, err := projects.CreateObjectiveRevision(ctx, member, projectID, objectiveV1, 0)
			return r.AsOfEvent, err
		}, 0, application.ErrForbidden},
		{"key-point keys are unique", func() (int64, error) {
			r, err := projects.CreateObjectiveRevision(ctx, owner, projectID, duplicateKeys, 0)
			return r.AsOfEvent, err
		}, 0, application.ErrInvalid},
		{"the accountable human must be a member", func() (int64, error) {
			r, err := projects.CreateObjectiveRevision(ctx, owner, projectID, outsiderAccountable, 0)
			return r.AsOfEvent, err
		}, 0, application.ErrInvalid},
		{"the owner creates a narrative-only objective", func() (int64, error) {
			r, err := projects.CreateObjectiveRevision(ctx, owner, projectID, objectiveV1, 0)
			return r.AsOfEvent, err
		}, 3, nil},
		{"retrying objective creation appends no event", func() (int64, error) {
			r, err := projects.CreateObjectiveRevision(ctx, owner, projectID, objectiveV1, 0)
			return r.AsOfEvent, err
		}, 3, nil},
		{"the owner appends revision 2", func() (int64, error) {
			r, err := projects.CreateObjectiveRevision(ctx, owner, projectID, objectiveV2, 1)
			return r.AsOfEvent, err
		}, 4, nil},
		{"a write from revision 1 is stale", func() (int64, error) {
			r, err := projects.CreateObjectiveRevision(ctx, owner, projectID, staleV2, 1)
			return r.AsOfEvent, err
		}, 0, application.ErrStaleRevision},
		{"an expected revision ahead of the current one is stale", func() (int64, error) {
			r, err := projects.CreateObjectiveRevision(ctx, owner, projectID, objectiveV2, 5)
			return r.AsOfEvent, err
		}, 0, application.ErrStaleRevision},
		{"a non-member cannot read the objective", func() (int64, error) {
			r, err := projects.ObjectiveRevision(ctx, outsider, projectID, objectiveID, 1)
			return r.AsOfEvent, err
		}, 0, application.ErrNotFound},
	}
	for _, step := range steps {
		asOf, err := step.run()
		if !errors.Is(err, step.wantErr) {
			t.Fatalf("%s: error %v, want %v", step.name, err, step.wantErr)
		}
		if asOf != step.wantEvent {
			t.Fatalf("%s: as of event %d, want %d", step.name, asOf, step.wantEvent)
		}
	}

	// Revision 1 is unchanged by revision 2, and members can read both.
	v1, err := projects.ObjectiveRevision(ctx, member, projectID, objectiveID, 1)
	if err != nil {
		t.Fatal(err)
	}
	if v1.Data.Title != objectiveV1.Title || len(v1.Data.KeyPoints) != 0 {
		t.Fatalf("revision 1 changed: %+v", v1.Data)
	}
	v2, err := projects.ObjectiveRevision(ctx, member, projectID, objectiveID, 2)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(v2.Data.KeyPoints, objectiveV2.KeyPoints) {
		t.Fatalf("revision 2 key points: %+v", v2.Data.KeyPoints)
	}

	// A transaction that writes state and an event and then fails commits
	// neither, and leaves no gap in the event sequence.
	failure := errors.New("failure after writing")
	err = store.Write(ctx, func(tx application.Tx) error {
		revision := v2.Data
		revision.Revision = 3
		if inserted, err := tx.InsertObjectiveRevision(ctx, revision); err != nil || !inserted {
			t.Fatalf("insert revision 3: %v, inserted %v", err, inserted)
		}
		if _, err := tx.AppendEvent(ctx, protocol.EventEnvelope{
			ProjectID: projectID, EventID: uuid.NewV7(), EntityID: objectiveID, ActorHumanID: owner,
			OccurredAt: protocol.NewTimestamp(time.Now()), Payload: protocol.ObjectiveRevisionCreated{Revision: 3},
		}); err != nil {
			t.Fatal(err)
		}
		return failure
	})
	if !errors.Is(err, failure) {
		t.Fatalf("write returned %v", err)
	}
	if _, err := projects.ObjectiveRevision(ctx, owner, projectID, objectiveID, 3); !errors.Is(err, application.ErrNotFound) {
		t.Fatalf("rolled-back revision is visible: %v", err)
	}
	v3, err := projects.CreateObjectiveRevision(ctx, owner, projectID, objectiveV1, 2)
	if err != nil || v3.Data.Revision != 3 || v3.AsOfEvent != 5 {
		t.Fatalf("revision after rollback: revision %d, event %d, %v", v3.Data.Revision, v3.AsOfEvent, err)
	}

	// The cursor returns committed events in order, and repeating it returns
	// the same range.
	wantTypes := []protocol.EventType{
		protocol.EventProjectCreated,
		protocol.EventProjectMemberAdded,
		protocol.EventObjectiveRevisionCreated,
		protocol.EventObjectiveRevisionCreated,
		protocol.EventObjectiveRevisionCreated,
	}
	for _, after := range []int64{0, 0, 3} {
		page, err := projects.Events(ctx, member, projectID, after, application.MaxEventPage)
		if err != nil {
			t.Fatal(err)
		}
		var got []protocol.EventType
		for i, event := range page.Data {
			if event.EventSequence != after+int64(i)+1 {
				t.Fatalf("events after %d: sequence %d at position %d", after, event.EventSequence, i)
			}
			got = append(got, event.EventType())
		}
		if !slices.Equal(got, wantTypes[after:]) || page.AsOfEvent != 5 {
			t.Fatalf("events after %d: %v as of %d", after, got, page.AsOfEvent)
		}
	}
}
