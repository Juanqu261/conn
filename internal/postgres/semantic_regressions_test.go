package postgres_test

import (
	"fmt"
	"strings"
	"testing"

	"uuid"

	"github.com/Juanqu261/conn/internal/application"
	"github.com/Juanqu261/conn/internal/protocol"
)

// TestResolutionDecisionWithoutReferences proves that the optional references
// field can be omitted: the stored JSON must still satisfy the array CHECK.
func TestResolutionDecisionWithoutReferences(t *testing.T) {
	ctx, store, projects, owner, projectID, objectiveID, leftParticipant, rightParticipant := assessmentFixture(t)
	leftID, rightID := uuid.NewV7(), uuid.NewV7()
	shared := claim("invariant", "invariant:checkout:one-charge", "changes")
	publishAssessmentIntent(t, ctx, projects, owner, projectID, objectiveID, leftParticipant, leftID, shared)
	publishAssessmentIntent(t, ctx, projects, owner, projectID, objectiveID, rightParticipant, rightID, shared)
	collision := loadAssessments(t, ctx, store, leftID, rightID)[0]

	decision, err := projects.DecideResolution(ctx, owner, projectID, collision.CollisionID, protocol.ResolutionDecisionInput{
		Kind: "false_positive", RationaleMarkdown: "The invariant is guarded by the payment provider.",
	})
	if err != nil {
		t.Fatalf("decision without references: %v", err)
	}
	if _, err := projects.Radar(ctx, owner, projectID); err != nil {
		t.Fatalf("radar after decision without references: %v", err)
	}
	if decision.Data.References != nil {
		t.Fatalf("decision references = %#v, want omitted", decision.Data.References)
	}
}

// TestRadarBoundsUnboundedInputs proves that valid ledger content larger than
// a Radar contract bound is summarized rather than failing the whole Radar.
func TestRadarBoundsUnboundedInputs(t *testing.T) {
	ctx, store, projects, owner, projectID, _, leftParticipant, rightParticipant := assessmentFixture(t)
	objectiveID := uuid.NewV7()
	if _, err := projects.CreateObjectiveRevision(ctx, owner, projectID, protocol.ObjectiveRevisionInput{
		ObjectiveID: objectiveID, Title: "Prove retries", OverviewMarkdown: "Retries stay safe.", AccountableHumanID: owner,
		KeyPoints: []protocol.KeyPoint{{Key: "retry-safe", Kind: protocol.KeyPointAcceptanceCriterion, Statement: "Retries never double charge."}},
	}, 0); err != nil {
		t.Fatal(err)
	}
	leftID, rightID := uuid.NewV7(), uuid.NewV7()
	shared := claim("invariant", "invariant:checkout:one-charge", "changes")
	publishAssessmentIntent(t, ctx, projects, owner, projectID, objectiveID, leftParticipant, leftID, shared)
	publishAssessmentIntent(t, ctx, projects, owner, projectID, objectiveID, rightParticipant, rightID, shared)
	collision := loadAssessments(t, ctx, store, leftID, rightID)[0]

	// A human rationale may be up to 4000 characters; a Radar dependency
	// summary is a 1000-character statement.
	if _, err := projects.DecideResolution(ctx, owner, projectID, collision.CollisionID, protocol.ResolutionDecisionInput{
		Kind: "dependency", Binding: true, RationaleMarkdown: strings.Repeat("Sequence the charge writer first. ", 100),
		References: []string{"meeting:retry-sync"},
	}); err != nil {
		t.Fatal(err)
	}
	// Radar decision_refs are bounded at 200.
	for i := 0; i < 200; i++ {
		if _, err := projects.DecideResolution(ctx, owner, projectID, collision.CollisionID, protocol.ResolutionDecisionInput{
			Kind: "other", RationaleMarkdown: fmt.Sprintf("Follow-up note %d.", i),
		}); err != nil {
			t.Fatal(err)
		}
	}
	// Radar evidence_coverage[].evidence_refs is bounded at 50 per key.
	var last protocol.EvidenceReference
	for i := 0; i < 51; i++ {
		state := "failing"
		if i == 50 {
			state = "passing"
		}
		evidence, err := projects.AttachEvidence(ctx, owner, projectID, leftParticipant, protocol.EvidenceAttachmentInput{
			IntentID: leftID, ObjectiveID: objectiveID, ObjectiveRevision: 1, AcceptanceKey: "retry-safe",
			Kind: "ci_run", State: state, Locator: fmt.Sprintf("ci:run-%d", i),
		})
		if err != nil {
			t.Fatal(err)
		}
		last = evidence.Data
	}

	radar, err := projects.Radar(ctx, owner, projectID)
	if err != nil {
		t.Fatalf("radar with large ledger: %v", err)
	}
	if len(radar.Data.Dependencies) != 1 || len([]rune(radar.Data.Dependencies[0].Summary)) > 1000 {
		t.Fatalf("dependencies = %#v, want one bounded summary", radar.Data.Dependencies)
	}
	if len(radar.Data.DecisionRefs) > 200 {
		t.Fatalf("decision refs = %d, want at most 200", len(radar.Data.DecisionRefs))
	}
	if len(radar.Data.EvidenceCoverage) != 1 {
		t.Fatalf("coverage = %#v", radar.Data.EvidenceCoverage)
	}
	coverage := radar.Data.EvidenceCoverage[0]
	if coverage.State != "passing" || len(coverage.EvidenceRefs) != 50 || !containsID(coverage.EvidenceRefs, last.EvidenceID) {
		t.Fatalf("coverage = state %s, %d refs, latest included %v", coverage.State, len(coverage.EvidenceRefs), containsID(coverage.EvidenceRefs, last.EvidenceID))
	}
	unknowns := strings.Join(radar.Data.KnownUnknowns, "\n")
	if !strings.Contains(unknowns, "decision") || !strings.Contains(unknowns, "evidence") {
		t.Fatalf("known unknowns = %q, want the truncations labelled", unknowns)
	}

	// The context pack bounds existing decisions at 20 and evidence at 50.
	pack, err := projects.ConflictContext(ctx, owner, projectID, collision.CollisionID)
	if err != nil {
		t.Fatalf("context pack with many decisions: %v", err)
	}
	if len(pack.Data.ExistingDecisionRefs) != 20 || len(pack.Data.EvidenceRefs) != 50 || !containsID(pack.Data.EvidenceRefs, last.EvidenceID) {
		t.Fatalf("pack refs = %d decisions, %d evidence (latest included %v)", len(pack.Data.ExistingDecisionRefs), len(pack.Data.EvidenceRefs), containsID(pack.Data.EvidenceRefs, last.EvidenceID))
	}
}

// TestIntentPublishReplayWithEmptyArrays proves that an identical retried
// publish is idempotent even when the client sends explicit empty arrays.
func TestIntentPublishReplayWithEmptyArrays(t *testing.T) {
	ctx, _, projects, owner, projectID, objectiveID, participant, _ := assessmentFixture(t)
	input := protocol.IntentInput{
		IntentID: uuid.NewV7(), ObjectiveID: objectiveID, ObjectiveRevision: 1,
		SummaryMarkdown: "Retry safely.", Assumptions: []string{},
		DeclaredClaims: []protocol.SemanticClaimCandidate{{
			SubjectKind: "invariant", NormalizedSubject: "invariant:checkout:one-charge", DisplayName: "One charge",
			Relation: "changes", ProvenanceKind: "declared", EvidenceRefs: []protocol.ClaimEvidenceRef{},
		}},
	}
	first, err := projects.PublishIntent(ctx, owner, projectID, participant, input)
	if err != nil {
		t.Fatal(err)
	}
	again, err := projects.PublishIntent(ctx, owner, projectID, participant, input)
	if err != nil {
		t.Fatalf("identical replay: %v", err)
	}
	if again.AsOfEvent != first.AsOfEvent || again.Data.IntentRevision.Revision != 1 {
		t.Fatalf("replay = %#v, want the stored revision 1 without events", again)
	}
}

// TestConflictContextIsCollisionSpecific proves the pack asks the collision's
// own question, keeps project questions labelled as context, and that the
// Markdown names the rule, explanation, and each Intent's assumptions.
func TestConflictContextIsCollisionSpecific(t *testing.T) {
	ctx, store, projects, owner, projectID, _, leftParticipant, rightParticipant := assessmentFixture(t)
	objectiveID := uuid.NewV7()
	if _, err := projects.CreateObjectiveRevision(ctx, owner, projectID, protocol.ObjectiveRevisionInput{
		ObjectiveID: objectiveID, Title: "Refunds", OverviewMarkdown: "Support refunds.", AccountableHumanID: owner,
		KeyPoints: []protocol.KeyPoint{{Key: "refund-currency", Kind: protocol.KeyPointQuestion, Statement: "Which currency do refunds use?"}},
	}, 0); err != nil {
		t.Fatal(err)
	}
	leftID, rightID := uuid.NewV7(), uuid.NewV7()
	for _, item := range []struct {
		participant, intent uuid.UUID
		assumption          string
	}{{leftParticipant, leftID, "Gateway retries stay below three attempts."}, {rightParticipant, rightID, "Orders reuse the gateway idempotency key."}} {
		if _, err := projects.PublishIntent(ctx, owner, projectID, item.participant, protocol.IntentInput{
			IntentID: item.intent, ObjectiveID: objectiveID, ObjectiveRevision: 1, SummaryMarkdown: "Change retries.",
			Assumptions:    []string{item.assumption},
			DeclaredClaims: []protocol.SemanticClaimCandidate{claim("invariant", "invariant:checkout:one-charge", "changes")},
		}); err != nil {
			t.Fatal(err)
		}
	}
	collision := loadAssessments(t, ctx, store, leftID, rightID)[0]
	pack, err := projects.ConflictContext(ctx, owner, projectID, collision.CollisionID)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(pack.Data.UnresolvedQuestion, "currency") {
		t.Fatalf("unresolved question = %q, want the collision's own question", pack.Data.UnresolvedQuestion)
	}
	if !strings.Contains(strings.Join(pack.Data.Uncertainties, "\n"), "Which currency do refunds use?") {
		t.Fatalf("uncertainties = %q, want open project questions retained", pack.Data.Uncertainties)
	}
	markdown := application.RenderConflictContextMarkdown(pack.Data)
	for _, want := range []string{"shared_declared_invariant", "Gateway retries stay below three attempts.", "Orders reuse the gateway idempotency key.", "invariant:checkout:one-charge"} {
		if !strings.Contains(markdown, want) {
			t.Fatalf("markdown lacks %q:\n%s", want, markdown)
		}
	}
}

func containsID(values []uuid.UUID, target uuid.UUID) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
