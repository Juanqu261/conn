package postgres_test

import (
	"errors"
	"testing"

	"uuid"

	"github.com/Juanqu261/conn/internal/application"
	"github.com/Juanqu261/conn/internal/protocol"
)

// TestRepositoryConnectionsAndParticipants protects the first semantic
// operations: owners establish repository scope, members can observe it, and
// a Participant is always resolved from the authenticated human rather than
// caller-supplied authority.
func TestRepositoryConnectionsAndParticipants(t *testing.T) {
	ctx, _, projects, newHuman := newProjects(t)
	owner, member, outsider := newHuman(101, "owner"), newHuman(102, "member"), newHuman(103, "outsider")
	projectID, repositoryID := uuid.NewV7(), uuid.NewV7()
	if _, err := projects.CreateProject(ctx, owner, protocol.ProjectCreate{ProjectID: projectID, Name: "Repository scope"}); err != nil {
		t.Fatal(err)
	}
	if _, err := projects.AddMember(ctx, owner, projectID, member); err != nil {
		t.Fatal(err)
	}

	input := protocol.RepositoryConnectInput{
		RepositoryID:    repositoryID,
		HostKind:        "github",
		CanonicalRemote: "github.com/acme/payments",
		AnalysisEnabled: true,
	}
	connected, err := projects.ConnectRepository(ctx, owner, projectID, input)
	if err != nil || connected.AsOfEvent != 3 || connected.Data.CreatedByHumanID != owner {
		t.Fatalf("connect = %#v, %v", connected, err)
	}
	listed, err := projects.RepositoryConnections(ctx, member, projectID)
	if err != nil || listed.AsOfEvent != 3 || len(listed.Data.Repositories) != 1 || listed.Data.Repositories[0].CanonicalRemote != input.CanonicalRemote {
		t.Fatalf("list = %#v, %v", listed, err)
	}
	retryInput := input
	retryInput.RepositoryID = uuid.NewV7()
	repeatedConnection, err := projects.ConnectRepository(ctx, owner, projectID, retryInput)
	if err != nil || repeatedConnection.AsOfEvent != 3 || repeatedConnection.Data.RepositoryID != repositoryID {
		t.Fatalf("repeated connection = %#v, %v", repeatedConnection, err)
	}
	if _, err := projects.ConnectRepository(ctx, member, projectID, protocol.RepositoryConnectInput{
		RepositoryID: uuid.NewV7(), HostKind: "github", CanonicalRemote: "github.com/acme/orders", AnalysisEnabled: true,
	}); !errors.Is(err, application.ErrForbidden) {
		t.Fatalf("member connect error = %v, want forbidden", err)
	}
	if _, err := projects.RepositoryConnections(ctx, outsider, projectID); !errors.Is(err, application.ErrNotFound) {
		t.Fatalf("outsider list error = %v, want not found", err)
	}

	participant, err := projects.ResolveParticipant(ctx, member, projectID, "kiro", "session-82")
	if err != nil || participant.AsOfEvent != 3 || participant.Data.HumanID != member || participant.Data.AdapterKind != "kiro" {
		t.Fatalf("participant = %#v, %v", participant, err)
	}
	repeated, err := projects.ResolveParticipant(ctx, member, projectID, "kiro", "session-82")
	if err != nil || repeated.AsOfEvent != 3 || repeated.Data.ParticipantID != participant.Data.ParticipantID {
		t.Fatalf("repeated participant = %#v, %v", repeated, err)
	}
	if _, err := projects.ResolveParticipant(ctx, outsider, projectID, "kiro", "session-82"); !errors.Is(err, application.ErrNotFound) {
		t.Fatalf("outsider participant error = %v, want not found", err)
	}
	if _, err := projects.ResolveParticipant(ctx, member, projectID, "untrusted", "session-82"); !errors.Is(err, application.ErrInvalid) {
		t.Fatalf("invalid adapter error = %v, want invalid", err)
	}
}
