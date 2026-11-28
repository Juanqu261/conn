package board_test

import (
	"strings"
	"testing"
	"time"

	"uuid"

	"github.com/Juanqu261/conn/internal/board"
	"github.com/Juanqu261/conn/internal/protocol"
)

// TestLanes protects the board's one rule set (web-lab-board requirement 1):
// each rule, their precedence, Objectives without acceptance criteria,
// stale evidence by the review packet's definition, the release window,
// and the exclusion of superseded Intents. No Intent is ever in two lanes.
func TestLanes(t *testing.T) {
	now := time.Date(2026, 11, 28, 12, 0, 0, 0, time.UTC)
	project, objective, other := uuid.NewV7(), uuid.NewV7(), uuid.NewV7()
	withCriteria := protocol.Objective{ObjectiveID: objective, Revision: 2, KeyPoints: []protocol.KeyPoint{
		{Key: "fr-001", Kind: protocol.KeyPointAcceptanceCriterion, Statement: "Retries are capped."},
		{Key: "fr-002", Kind: protocol.KeyPointAcceptanceCriterion, Statement: "Backoff is exponential."},
		{Key: "q-1", Kind: protocol.KeyPointQuestion, Statement: "Which gateway?"},
	}}
	withoutCriteria := protocol.Objective{ObjectiveID: other, Revision: 1}

	type intentSpec struct {
		status    string
		objective protocol.Objective
		head      string
		analyzed  int // 0: never analyzed
		evidence  []evidenceSpec
		released  time.Duration
		collision string
	}
	cases := []struct {
		name     string
		intent   intentSpec
		wantLane string // "" when excluded
		wantRule string
		wantFact string
	}{
		{"every criterion passing at the head is ready for review",
			intentSpec{objective: withCriteria, head: "c3d4e5f", analyzed: 3, evidence: []evidenceSpec{{"fr-001", "passing", "c3d4e5f", 1}, {"fr-002", "passing", "", 2}}},
			protocol.LaneAlive, protocol.RuleReady, "All 2 acceptance criteria"},
		{"a missing criterion gathers proof and names the key",
			intentSpec{objective: withCriteria, head: "c3d4e5f", analyzed: 3, evidence: []evidenceSpec{{"fr-001", "passing", "c3d4e5f", 1}}},
			protocol.LaneCharging, protocol.RuleAcceptanceIncomplete, "1 of 2 acceptance criteria passing; missing `fr-002`"},
		{"passing evidence at another commit is stale and does not count",
			intentSpec{objective: withCriteria, head: "c3d4e5f", analyzed: 3, evidence: []evidenceSpec{{"fr-001", "passing", "c3d4e5f", 1}, {"fr-002", "passing", "a1b2c3d", 2}}},
			protocol.LaneCharging, protocol.RuleAcceptanceIncomplete, "stale `fr-002`"},
		{"the latest evidence for a key decides, as in the review packet",
			intentSpec{objective: withCriteria, head: "c3d4e5f", analyzed: 3, evidence: []evidenceSpec{{"fr-001", "passing", "c3d4e5f", 1}, {"fr-002", "passing", "c3d4e5f", 2}, {"fr-002", "failing", "c3d4e5f", 3}}},
			protocol.LaneCharging, protocol.RuleAcceptanceIncomplete, "failing `fr-002`"},
		{"an Objective without acceptance criteria skips the proof rule and says so",
			intentSpec{objective: withoutCriteria, head: "c3d4e5f", analyzed: 3},
			protocol.LaneAlive, protocol.RuleNoAcceptanceCriteria, "no acceptance criteria"},
		{"no head revision is still in progress",
			intentSpec{objective: withCriteria, analyzed: 3},
			protocol.LaneAssembling, protocol.RuleNoHeadRevision, "no head commit"},
		{"an analysis of an earlier revision is not an observed analysis of this one",
			intentSpec{objective: withCriteria, head: "c3d4e5f", analyzed: 2},
			protocol.LaneAssembling, protocol.RuleNoObservedAnalysis, "covers revision 2"},
		{"an open collision outranks progress and proof",
			intentSpec{objective: withCriteria, collision: protocol.CollisionOpen},
			protocol.LaneStitching, protocol.RuleOpenCollision, "collides with “Refund flow”"},
		{"a collision under review still needs a decision",
			intentSpec{objective: withoutCriteria, head: "c3d4e5f", analyzed: 3, collision: protocol.CollisionUnderReview},
			protocol.LaneStitching, protocol.RuleOpenCollision, "(under review)"},
		{"a resolved collision no longer holds the card",
			intentSpec{objective: withoutCriteria, head: "c3d4e5f", analyzed: 3, collision: protocol.CollisionResolved},
			protocol.LaneAlive, protocol.RuleNoAcceptanceCriteria, ""},
		{"a release inside the window outranks an open collision",
			intentSpec{status: "released", objective: withCriteria, released: 3 * 24 * time.Hour, collision: protocol.CollisionOpen},
			protocol.LaneBuried, protocol.RuleReleasedRecently, "Released 3 days ago"},
		{"a release before the window leaves the board",
			intentSpec{status: "released", objective: withCriteria, released: 15 * 24 * time.Hour},
			"", "", ""},
		{"a superseded Intent never appears",
			intentSpec{status: "superseded", objective: withoutCriteria, head: "c3d4e5f", analyzed: 3},
			"", "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			spec := tc.intent
			if spec.status == "" {
				spec.status = "active"
			}
			intentID, counterpart := uuid.NewV7(), uuid.NewV7()
			revision := protocol.IntentRevision{
				IntentID: intentID, ProjectID: project, Revision: 3, ObjectiveID: spec.objective.ObjectiveID,
				ObjectiveRevision: spec.objective.Revision, HeadRevisionRef: spec.head,
				SummaryMarkdown: "# Retry failed payments\n\nDetails.", CreatedAt: protocol.NewTimestamp(now.Add(-time.Hour)),
			}
			snapshot := board.Snapshot{
				Now: now, WindowDays: board.DefaultWindowDays,
				Intents: []protocol.IntentView{
					{IntentRevision: revision, Status: spec.status},
					{IntentRevision: protocol.IntentRevision{IntentID: counterpart, ProjectID: project, Revision: 1, ObjectiveID: other, ObjectiveRevision: 1,
						HeadRevisionRef: "f00d", SummaryMarkdown: "Refund flow", CreatedAt: protocol.NewTimestamp(now)}, Status: "active"},
				},
				ReleasedAt: map[uuid.UUID]time.Time{},
				Analyzed:   map[uuid.UUID]int{counterpart: 1},
				Objectives: map[board.ObjectiveRef]protocol.Objective{
					{ID: objective, Revision: 2}: withCriteria,
					{ID: other, Revision: 1}:     withoutCriteria,
				},
			}
			if spec.analyzed > 0 {
				snapshot.Analyzed[intentID] = spec.analyzed
			}
			if spec.released > 0 {
				snapshot.ReleasedAt[intentID] = now.Add(-spec.released)
			}
			if spec.collision != "" {
				snapshot.Collisions = []protocol.CollisionAssessment{{
					CollisionID: uuid.NewV7(), ProjectID: project, LeftIntentID: intentID, LeftIntentRevision: 3,
					RightIntentID: counterpart, RightIntentRevision: 1, RuleID: "contract_producer_consumer",
					State: spec.collision, MatchedSubjects: []string{"contract:PaymentGateway"},
				}}
			}
			for _, item := range spec.evidence {
				snapshot.Evidence = append(snapshot.Evidence, item.reference(project, intentID, spec.objective, now))
			}

			view := board.Build(project, 42, snapshot)
			if _, err := protocol.Encode(view); err != nil {
				t.Fatalf("board view breaks its contract: %v", err)
			}
			found := 0
			for _, lane := range view.Lanes {
				for _, card := range lane.Cards {
					if card.IntentID != intentID {
						continue
					}
					found++
					if lane.Lane != tc.wantLane || card.Reason.Rule != tc.wantRule {
						t.Fatalf("got %s/%s, want %s/%s (%s)", lane.Lane, card.Reason.Rule, tc.wantLane, tc.wantRule, card.Reason.FactsMarkdown)
					}
					if !strings.Contains(card.Reason.FactsMarkdown, tc.wantFact) {
						t.Fatalf("facts %q do not say %q", card.Reason.FactsMarkdown, tc.wantFact)
					}
					if lane.Lane != protocol.LaneAlive && lane.Lane != protocol.LaneBuried && card.Reason.NextStep == "" {
						t.Fatal("a card outside Alive must say what would move it")
					}
				}
			}
			if want := map[bool]int{true: 1, false: 0}[tc.wantLane != ""]; found != want {
				t.Fatalf("the Intent is on the board %d times, want %d", found, want)
			}
		})
	}
}

type evidenceSpec struct {
	key, state, commit string
	order              int
}

func (e evidenceSpec) reference(project, intentID uuid.UUID, objective protocol.Objective, now time.Time) protocol.EvidenceReference {
	reference := protocol.EvidenceReference{
		EvidenceID: uuid.NewV7(), ProjectID: project, IntentID: &intentID, ObjectiveID: objective.ObjectiveID,
		ObjectiveRevision: objective.Revision, AcceptanceKey: e.key, Kind: "test_case", State: e.state,
		Locator: "https://ci.example.com/run", ObservedAt: protocol.NewTimestamp(now.Add(time.Duration(e.order-10) * time.Minute)),
	}
	if e.commit != "" {
		reference.Observation = &protocol.EvidenceObservation{Method: protocol.ObservationMethodCheck, Runner: protocol.ObservationRunnerLocal, Commit: e.commit}
	}
	return reference
}
