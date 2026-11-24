package postgres_test

import (
	"errors"
	"strings"
	"testing"

	"uuid"

	"github.com/Juanqu261/conn/internal/application"
	"github.com/Juanqu261/conn/internal/protocol"
)

// TestParticipantIntents verifies Task 6's authority boundary on a real
// PostgreSQL database: participants can publish, revise, and release only
// their own immutable Intent revisions. Exact Objective, Blueprint, and
// accepted Charter references are checked before an event is appended.
func TestParticipantIntents(t *testing.T) {
	ctx, _, projects, newHuman := newProjects(t)
	owner, agent, outsider := newHuman(201, "owner"), newHuman(202, "agent"), newHuman(203, "outsider")
	projectID, objectiveID, blueprintID, cellID := uuid.NewV7(), uuid.NewV7(), uuid.NewV7(), uuid.NewV7()
	if _, err := projects.CreateProject(ctx, owner, protocol.ProjectCreate{ProjectID: projectID, Name: "Intent authority"}); err != nil {
		t.Fatal(err)
	}
	if _, err := projects.AddMember(ctx, owner, projectID, agent); err != nil {
		t.Fatal(err)
	}
	if _, err := projects.CreateObjectiveRevision(ctx, owner, projectID, protocol.ObjectiveRevisionInput{
		ObjectiveID: objectiveID, Title: "Protect order creation", OverviewMarkdown: "One order per request.", AccountableHumanID: owner,
	}, 0); err != nil {
		t.Fatal(err)
	}
	if _, err := projects.ProposeBlueprintRevision(ctx, owner, projectID, protocol.BlueprintProposal{
		BlueprintID: blueprintID, ObjectiveID: objectiveID, ObjectiveRevision: 1,
		CellCharters: []protocol.CellCharter{{CellID: cellID, AccountableHumanID: agent, Title: "Agent cell", OverviewMarkdown: "Own retries."}},
	}, 0); err != nil {
		t.Fatal(err)
	}
	if _, err := projects.AcceptCharter(ctx, agent, projectID, blueprintID, 1, cellID); err != nil {
		t.Fatal(err)
	}

	agentParticipant, err := projects.ResolveParticipant(ctx, agent, projectID, "kiro", "session-a")
	if err != nil {
		t.Fatal(err)
	}
	ownerParticipant, err := projects.ResolveParticipant(ctx, owner, projectID, "kiro", "session-o")
	if err != nil {
		t.Fatal(err)
	}
	intentID := uuid.NewV7()
	input := protocol.IntentInput{
		IntentID: intentID, ObjectiveID: objectiveID, ObjectiveRevision: 1,
		BlueprintID: &blueprintID, BlueprintRevision: ptr(1), CharterID: &cellID,
		SummaryMarkdown: "Update retry behavior without changing the order boundary.",
	}
	published, err := projects.PublishIntent(ctx, agent, projectID, agentParticipant.Data.ParticipantID, input)
	if err != nil || published.Data.Status != "active" || published.Data.IntentRevision.Revision != 1 || len(published.Data.AdvisoryObservations) == 0 {
		t.Fatalf("publish = %#v, %v", published, err)
	}
	if !strings.Contains(published.Data.AdvisoryObservations[0], "advisory") {
		t.Fatalf("charter observation = %q", published.Data.AdvisoryObservations[0])
	}
	if _, err := projects.PublishIntent(ctx, agent, projectID, ownerParticipant.Data.ParticipantID, protocol.IntentInput{IntentID: uuid.NewV7(), ObjectiveID: objectiveID, ObjectiveRevision: 1, SummaryMarkdown: "Impersonate."}); !errors.Is(err, application.ErrForbidden) {
		t.Fatalf("impersonation = %v, want forbidden", err)
	}
	// A participant publication grant carries no authority to alter human-owned
	// Objectives or Blueprint/Charter decisions.
	if _, err := projects.CreateObjectiveRevision(ctx, agent, projectID, protocol.ObjectiveRevisionInput{ObjectiveID: uuid.NewV7(), Title: "Unauthorized", OverviewMarkdown: "No.", AccountableHumanID: agent}, 0); !errors.Is(err, application.ErrForbidden) {
		t.Fatalf("participant objective mutation = %v, want forbidden", err)
	}
	if _, err := projects.ProposeBlueprintRevision(ctx, agent, projectID, protocol.BlueprintProposal{BlueprintID: uuid.NewV7(), ObjectiveID: objectiveID, ObjectiveRevision: 1, CellCharters: []protocol.CellCharter{{CellID: uuid.NewV7(), AccountableHumanID: agent, Title: "Unauthorized", OverviewMarkdown: "No."}}}, 0); !errors.Is(err, application.ErrForbidden) {
		t.Fatalf("participant blueprint mutation = %v, want forbidden", err)
	}
	if _, err := projects.AcceptCharter(ctx, owner, projectID, blueprintID, 1, cellID); !errors.Is(err, application.ErrForbidden) {
		t.Fatalf("other human charter mutation = %v, want forbidden", err)
	}
	if _, err := projects.ReviseIntent(ctx, owner, projectID, ownerParticipant.Data.ParticipantID, 1, input); !errors.Is(err, application.ErrForbidden) {
		t.Fatalf("other participant revise = %v, want forbidden", err)
	}
	wrongObjective := input
	wrongObjective.IntentID = uuid.NewV7()
	wrongObjective.ObjectiveRevision = 2
	if _, err := projects.PublishIntent(ctx, agent, projectID, agentParticipant.Data.ParticipantID, wrongObjective); !errors.Is(err, application.ErrInvalid) {
		t.Fatalf("missing exact objective = %v, want invalid", err)
	}
	badBlueprint := input
	badBlueprint.IntentID = uuid.NewV7()
	otherObjective := uuid.NewV7()
	badBlueprint.ObjectiveID = otherObjective
	if _, err := projects.PublishIntent(ctx, agent, projectID, agentParticipant.Data.ParticipantID, badBlueprint); !errors.Is(err, application.ErrInvalid) {
		t.Fatalf("mismatched association = %v, want invalid", err)
	}

	revisedInput := input
	revisedInput.SummaryMarkdown = "Update retry behavior and emit safe retry metrics."
	revised, err := projects.ReviseIntent(ctx, agent, projectID, agentParticipant.Data.ParticipantID, 1, revisedInput)
	if err != nil || revised.Data.IntentRevision.Revision != 2 {
		t.Fatalf("revise = %#v, %v", revised, err)
	}
	if _, err := projects.ReviseIntent(ctx, agent, projectID, agentParticipant.Data.ParticipantID, 1, revisedInput); !errors.Is(err, application.ErrStaleRevision) {
		t.Fatalf("stale revise = %v, want stale", err)
	}
	released, err := projects.ReleaseIntent(ctx, agent, projectID, agentParticipant.Data.ParticipantID, intentID)
	if err != nil || released.Data.Status != "released" {
		t.Fatalf("release = %#v, %v", released, err)
	}
	if _, err := projects.ReleaseIntent(ctx, outsider, projectID, agentParticipant.Data.ParticipantID, intentID); !errors.Is(err, application.ErrForbidden) && !errors.Is(err, application.ErrNotFound) {
		t.Fatalf("outsider release = %v, want forbidden or not found", err)
	}
	listed, err := projects.Intents(ctx, owner, projectID)
	if err != nil || len(listed.Data.Intents) != 1 || listed.Data.Intents[0].IntentRevision.Revision != 2 || listed.Data.Intents[0].Status != "released" {
		t.Fatalf("list = %#v, %v", listed, err)
	}
}

func ptr(value int) *int { return &value }
