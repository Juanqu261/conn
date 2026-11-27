package postgres_test

import (
	"slices"
	"testing"

	"uuid"

	"github.com/Juanqu261/conn/internal/application"
	"github.com/Juanqu261/conn/internal/protocol"
)

// TestCollisionPreviewMatchesTheAssessorAndWritesNothing protects
// requirement 3: a preview returns exactly what the persisted assessor opens
// for the same claims, including an inherited human decision, and it writes
// no row and appends no event.
func TestCollisionPreviewMatchesTheAssessorAndWritesNothing(t *testing.T) {
	f := newLifecycleFixture(t)
	subject := "contract:typescript:src/payments/retry.ts/shouldRetryCharge"
	producer := []protocol.SemanticClaimCandidate{claim("contract", subject, "changes")}
	consumer := []protocol.SemanticClaimCandidate{claim("contract", subject, "consumes"), claim("invariant", "invariant:checkout:one-charge", "affects")}
	input := func(intentID uuid.UUID, claims []protocol.SemanticClaimCandidate) protocol.IntentInput {
		return protocol.IntentInput{IntentID: intentID, ObjectiveID: f.objectiveID, ObjectiveRevision: 1, SummaryMarkdown: "Change retries.", DeclaredClaims: claims}
	}
	if _, err := f.projects.PublishIntent(f.ctx, f.owner, f.projectID, f.ownerParticipant, input(f.leftID, producer)); err != nil {
		t.Fatal(err)
	}

	counts := func() [3]int64 {
		t.Helper()
		var result [3]int64
		for i, table := range []string{"events", "collision_assessments", "semantic_claims"} {
			if err := f.store.Pool.QueryRow(f.ctx, "SELECT count(*) FROM "+table).Scan(&result[i]); err != nil {
				t.Fatal(err)
			}
		}
		return result
	}
	preview := func(as *uuid.UUID, reader application.Reader) protocol.CollisionPreview {
		t.Helper()
		before := counts()
		result, err := f.projects.PreviewCollisions(f.ctx, reader, f.projectID, protocol.CollisionPreviewInput{Claims: consumer, AsIntentID: as})
		if err != nil {
			t.Fatal(err)
		}
		if after := counts(); after != before {
			t.Fatalf("preview changed row counts from %v to %v", before, after)
		}
		return result.Data
	}

	opened := preview(nil, application.HumanReader(f.member))
	// A generic MCP client gets its own attributed Participant.
	if generic, err := f.projects.ResolveParticipant(f.ctx, f.member, f.projectID, "mcp", ""); err != nil || generic.Data.AdapterKind != "mcp" {
		t.Fatalf("mcp participant = %+v, %v", generic.Data, err)
	}
	if len(opened.Collisions) != 1 || opened.ClaimCount != 2 {
		t.Fatalf("preview = %+v", opened)
	}
	want := opened.Collisions[0]
	if want.RuleID != "contract_producer_consumer" || want.State != protocol.CollisionOpen || want.Counterpart.IntentID != f.leftID || want.Counterpart.Revision != 1 || !slices.Equal(want.MatchedSubjects, []string{subject}) {
		t.Fatalf("previewed collision = %+v", want)
	}
	// A non-member cannot preview.
	if _, err := f.projects.PreviewCollisions(f.ctx, application.HumanReader(uuid.NewV7()), f.projectID, protocol.CollisionPreviewInput{Claims: consumer}); err == nil {
		t.Fatal("a non-member previewed collisions")
	}

	// Publishing the same claims opens the same collision.
	if _, err := f.projects.PublishIntent(f.ctx, f.member, f.projectID, f.memberParticipant, input(f.rightID, consumer)); err != nil {
		t.Fatal(err)
	}
	persisted := f.only(t, 1)
	if persisted.RuleID != want.RuleID || persisted.State != want.State || !slices.Equal(persisted.MatchedSubjects, want.MatchedSubjects) || persisted.Severity != want.Severity {
		t.Fatalf("persisted = %+v, previewed %+v", persisted, want)
	}

	// After a dismissal, a preview standing for the next revision inherits
	// the decision exactly as the persisted revision does.
	decision, err := f.projects.DecideResolution(f.ctx, f.member, f.projectID, persisted.CollisionID, protocol.ResolutionDecisionInput{Kind: "false_positive", RationaleMarkdown: "The consumer pins the old behavior."})
	if err != nil {
		t.Fatal(err)
	}
	inherited := preview(&f.rightID, application.HumanReader(f.owner))
	if len(inherited.Collisions) != 1 {
		t.Fatalf("inherited preview = %+v", inherited)
	}
	got := inherited.Collisions[0]
	if got.State != protocol.CollisionDismissed || got.DecisionRef == nil || *got.DecisionRef != decision.Data.DecisionID || got.InheritedFromCollisionID == nil || *got.InheritedFromCollisionID != persisted.CollisionID {
		t.Fatalf("inherited preview = %+v", got)
	}
	if _, err := f.projects.ReviseIntent(f.ctx, f.member, f.projectID, f.memberParticipant, 1, input(f.rightID, consumer)); err != nil {
		t.Fatal(err)
	}
	carried := f.only(t, 2)
	if carried.State != got.State || carried.DecisionRef == nil || *carried.DecisionRef != *got.DecisionRef || !slices.Equal(carried.MatchedSubjects, got.MatchedSubjects) {
		t.Fatalf("persisted revision = %+v, previewed %+v", carried, got)
	}
}
