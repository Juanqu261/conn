package postgres_test

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"uuid"

	"github.com/Juanqu261/conn/internal/protocol"
)

// TestInsightsAreProjectLevelAndSuppressSmallDenominators protects
// requirement 12: insights count collisions and decisions per rule, compute
// ratios only from at least five samples, and contain no Human,
// Participant, or login.
func TestInsightsAreProjectLevelAndSuppressSmallDenominators(t *testing.T) {
	f := newLifecycleFixture(t)
	publish := func(human, participant uuid.UUID, subject string) uuid.UUID {
		t.Helper()
		id := uuid.NewV7()
		if _, err := f.projects.PublishIntent(f.ctx, human, f.projectID, participant, protocol.IntentInput{
			IntentID: id, ObjectiveID: f.objectiveID, ObjectiveRevision: 1, SummaryMarkdown: "Change an invariant.",
			DeclaredClaims: []protocol.SemanticClaimCandidate{claim("invariant", subject, "changes")},
		}); err != nil {
			t.Fatal(err)
		}
		return id
	}
	read := func() protocol.ProjectInsights {
		t.Helper()
		result, err := f.projects.Insights(f.ctx, f.member, f.projectID, 30)
		if err != nil {
			t.Fatal(err)
		}
		return result.Data
	}

	// Four decided collisions are below the minimum denominator.
	var collisions []protocol.CollisionAssessment
	for i := range 5 {
		subject := fmt.Sprintf("invariant:checkout:rule-%d", i)
		left := publish(f.owner, f.ownerParticipant, subject)
		right := publish(f.member, f.memberParticipant, subject)
		found := loadAssessments(t, f.ctx, f.store, left, right)
		if len(found) != 1 {
			t.Fatalf("collision %d = %+v", i, found)
		}
		collisions = append(collisions, found[0])
	}
	decide := func(collision protocol.CollisionAssessment, kind string) {
		t.Helper()
		human := f.owner
		if kind == "false_positive" {
			human = f.member
		}
		if _, err := f.projects.DecideResolution(f.ctx, human, f.projectID, collision.CollisionID, protocol.ResolutionDecisionInput{Kind: kind, RationaleMarkdown: "Decided by a Human."}); err != nil {
			t.Fatal(err)
		}
	}
	for i, kind := range []string{"false_positive", "false_positive", "dependency", "sequencing"} {
		decide(collisions[i], kind)
	}
	early := read()
	share := early.FalsePositiveShare[0].Share
	if share.Status != protocol.InsightInsufficientData || share.Value != nil || share.Numerator != 2 || share.Denominator != 4 ||
		early.MedianHoursToFirstDecision.Status != protocol.InsightInsufficientData || early.DecidedBeforeRelease.Status != protocol.InsightInsufficientData {
		t.Fatalf("insights below the minimum = %+v", early)
	}

	decide(collisions[4], "false_positive")
	insights := read()
	share = insights.FalsePositiveShare[0].Share
	if len(insights.CollisionsOpened) != 1 || insights.CollisionsOpened[0] != (protocol.InsightRuleCount{RuleID: "shared_declared_invariant", Count: 5}) ||
		share.Status != protocol.InsightValue || share.Value == nil || *share.Value != 0.6 ||
		insights.MedianHoursToFirstDecision.Status != protocol.InsightValue || insights.MedianHoursToFirstDecision.SampleSize != 5 ||
		insights.DecidedBeforeRelease.Numerator != 5 || insights.DecidedBeforeRelease.Status != protocol.InsightValue ||
		insights.UndeclaredObservedChanges.Status != protocol.InsightInsufficientData || len(insights.Decisions) != 3 {
		t.Fatalf("insights = %+v", insights)
	}
	encoded, err := json.Marshal(insights)
	if err != nil {
		t.Fatal(err)
	}
	for _, person := range []string{f.owner.String(), f.member.String(), f.ownerParticipant.String(), f.memberParticipant.String(), "lifecycle-owner", "lifecycle-member"} {
		if strings.Contains(string(encoded), person) {
			t.Fatalf("insights mention a person: %s", encoded)
		}
	}
	if _, err := f.projects.Insights(f.ctx, f.member, f.projectID, 366); err == nil {
		t.Fatal("a 366-day window was accepted")
	}
}
