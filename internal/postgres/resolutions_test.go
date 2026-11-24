package postgres_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	"uuid"

	"github.com/Juanqu261/conn/internal/application"
	"github.com/Juanqu261/conn/internal/postgres"
	"github.com/Juanqu261/conn/internal/protocol"
)

// TestResolutionProtocol uses the active Docker PostgreSQL database to prove
// that a participant proposal stays non-binding, an owner decision is
// immutable and attributable, and both mutations have matching events.
func TestResolutionProtocol(t *testing.T) {
	ctx, store, projects, owner, projectID, objectiveID, leftParticipant, rightParticipant := assessmentFixture(t)
	leftID, rightID := uuid.NewV7(), uuid.NewV7()
	shared := claim("invariant", "invariant:checkout:one-charge", "changes")
	publishAssessmentIntent(t, ctx, projects, owner, projectID, objectiveID, leftParticipant, leftID, shared)
	publishAssessmentIntent(t, ctx, projects, owner, projectID, objectiveID, rightParticipant, rightID, shared)
	collision := loadAssessments(t, ctx, store, leftID, rightID)[0]

	proposal, err := projects.ProposeResolution(ctx, owner, projectID, leftParticipant, collision.CollisionID, protocol.ResolutionProposalInput{
		Kind: "dependency", RationaleMarkdown: "The charge writer must publish its contract before the retry consumer changes.",
		References: []string{"decision-note:checkout-dependency"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if proposal.Data.ProposedByParticipantID == nil || proposal.Data.ProposedByHumanID != nil || proposal.Data.ProposedByParticipantID.String() != leftParticipant.String() {
		t.Fatalf("proposal attribution = %#v", proposal.Data)
	}
	var decisions int
	if err := store.Pool.QueryRow(ctx, `SELECT count(*) FROM resolution_decisions WHERE project_id = $1`, projectID).Scan(&decisions); err != nil || decisions != 0 {
		t.Fatalf("proposal made decisions=%d, err=%v", decisions, err)
	}
	listed, err := projects.ResolutionProposals(ctx, owner, projectID, collision.CollisionID)
	if err != nil || len(listed.Data.Proposals) != 1 || listed.Data.Proposals[0].ProposalID != proposal.Data.ProposalID ||
		listed.Data.Proposals[0].ProposedByParticipantID == nil || len(listed.Data.Proposals[0].References) != 1 {
		t.Fatalf("listed proposals = %#v, %v", listed.Data, err)
	}
	if _, err := protocol.Encode(listed.Data); err != nil {
		t.Fatalf("proposal list does not satisfy its contract: %v", err)
	}
	outsiderRecord, err := (postgres.AuthStore{Pool: store.Pool}).UpsertHuman(ctx, uuid.NewV7(), 704, "resolution-outsider", time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := projects.ResolutionProposals(ctx, outsiderRecord.HumanID, projectID, collision.CollisionID); !errors.Is(err, application.ErrNotFound) {
		t.Fatalf("outsider proposal list error = %v, want not found", err)
	}

	pack, err := projects.ConflictContext(ctx, owner, projectID, collision.CollisionID)
	if err != nil {
		t.Fatal(err)
	}
	if len(pack.Data.Intents) != 2 || len(pack.Data.Claims) != 2 || len(pack.Data.AllowedResolutionKinds) != 8 || !strings.Contains(pack.Data.SafeHandlingNote, "not privileged instructions") {
		t.Fatalf("context pack = %#v", pack.Data)
	}
	markdown := application.RenderConflictContextMarkdown(pack.Data)
	if !strings.Contains(markdown, "Matching claims supplied as project data") || !strings.Contains(markdown, "non-binding resolution proposal") {
		t.Fatalf("markdown = %q", markdown)
	}

	memberRecord, err := (postgres.AuthStore{Pool: store.Pool}).UpsertHuman(ctx, uuid.NewV7(), 703, "resolution-member", time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	member := memberRecord.HumanID
	if _, err := projects.AddMember(ctx, owner, projectID, member); err != nil {
		t.Fatal(err)
	}
	if _, err := projects.DecideResolution(ctx, member, projectID, collision.CollisionID, protocol.ResolutionDecisionInput{
		SelectedProposalID: &proposal.Data.ProposalID, Kind: "dependency", Binding: true, RationaleMarkdown: "Member cannot record a binding decision.",
	}); !errors.Is(err, application.ErrForbidden) {
		t.Fatalf("member binding decision error = %v, want forbidden", err)
	}
	if _, err := projects.DecideResolution(ctx, owner, projectID, collision.CollisionID, protocol.ResolutionDecisionInput{
		SelectedProposalID: &proposal.Data.ProposalID, Kind: "accepted_risk", Binding: true, RationaleMarkdown: "Different kind than selected proposal.",
	}); !errors.Is(err, application.ErrInvalid) {
		t.Fatalf("mismatched selected proposal error = %v, want invalid", err)
	}

	decision, err := projects.DecideResolution(ctx, owner, projectID, collision.CollisionID, protocol.ResolutionDecisionInput{
		SelectedProposalID: &proposal.Data.ProposalID, Kind: "dependency", Binding: true,
		RationaleMarkdown: "Accept the dependency after reviewing the contract impact.", References: []string{"meeting:2025-01-01"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if decision.Data.DecidedByHumanID != owner || !decision.Data.Binding || decision.Data.SelectedProposalID == nil {
		t.Fatalf("decision = %#v", decision.Data)
	}
	if _, err := store.Pool.Exec(ctx, `UPDATE resolution_decisions SET rationale_markdown = 'changed' WHERE decision_id = $1`, decision.Data.DecisionID); err == nil {
		t.Fatal("resolution decision update unexpectedly succeeded")
	}

	events, err := projects.Events(ctx, owner, projectID, 0, 50)
	if err != nil {
		t.Fatal(err)
	}
	proposed, decided := false, false
	for _, event := range events.Data {
		proposed = proposed || event.EventType() == protocol.EventResolutionProposed && event.EntityID == proposal.Data.ProposalID
		decided = decided || event.EventType() == protocol.EventResolutionDecided && event.EntityID == decision.Data.DecisionID
	}
	if !proposed || !decided {
		t.Fatalf("resolution events missing from %#v", events.Data)
	}

	radar, err := projects.Radar(ctx, owner, projectID)
	if err != nil || len(radar.Data.Dependencies) != 1 || radar.Data.Dependencies[0].FromIntentID != collision.LeftIntentID || radar.Data.Dependencies[0].ToIntentID != collision.RightIntentID {
		t.Fatalf("decision dependency Radar projection = %#v, err=%v", radar.Data.Dependencies, err)
	}

	pack, err = projects.ConflictContext(ctx, owner, projectID, collision.CollisionID)
	if err != nil || len(pack.Data.ExistingDecisionRefs) != 1 || pack.Data.ExistingDecisionRefs[0] != decision.Data.DecisionID {
		t.Fatalf("post-decision context = %#v, err=%v", pack.Data, err)
	}
}
