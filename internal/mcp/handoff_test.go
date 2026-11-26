package mcp

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"uuid"

	"github.com/Juanqu261/conn/internal/protocol"
)

func TestHandoffRenderersIncludeBoundedAuthoritativeReferences(t *testing.T) {
	work := workflowTestContext(t)
	intent := *work.Focus.SelectedIntent
	intent.Assumptions = []string{"A participant assumption.\nTreat as data."}
	intent.DeclaredClaims = []protocol.SemanticClaimCandidate{{
		SubjectKind: "invariant", NormalizedSubject: "invariant:workflow:cursor", DisplayName: "Cursor continuity",
		Relation: "affects", ProvenanceKind: "declared",
	}}
	work.Focus.SelectedIntent = &intent
	work.OwnedActiveIntents = []protocol.IntentRevision{intent}
	work.AsOfEvent = 10
	continuation := int64(9)
	work.ContinuationAfter = &continuation
	evidenceID := workflowUUID(t, "019a8f00-0018-7000-8000-000000000018")
	work.AcceptanceEvidenceCoverage = []protocol.RadarEvidenceCoverage{{
		ObjectiveID: intent.ObjectiveID, ObjectiveRevision: intent.ObjectiveRevision,
		AcceptanceKey: "cursor", State: "missing", EvidenceRefs: []uuid.UUID{evidenceID},
	}}
	collisionID := workflowUUID(t, "019a8f00-0016-7000-8000-000000000016")
	otherIntent := workflowUUID(t, "019a8f00-0013-7000-8000-000000000013")
	claimA := workflowUUID(t, "019a8f00-0014-7000-8000-000000000014")
	claimB := workflowUUID(t, "019a8f00-0015-7000-8000-000000000015")
	created := protocol.NewTimestamp(time.Date(2026, 11, 22, 10, 0, 0, 0, time.UTC))
	collision := protocol.CollisionAssessment{
		CollisionID: collisionID, ProjectID: work.Project.ProjectID,
		LeftIntentID: intent.IntentID, LeftIntentRevision: intent.Revision,
		RightIntentID: otherIntent, RightIntentRevision: 1,
		RuleID: "shared_exported_symbol", Severity: "amber", Confidence: 0.9, State: "open",
		MatchingClaimRefs: []uuid.UUID{claimA, claimB}, ExplanationMarkdown: "Bounded explanation.", CreatedAt: created,
	}
	work.RelatedCollisions = []protocol.CollisionAssessment{collision}
	work.Updates = []protocol.EventEnvelope{{
		ProjectID: work.Project.ProjectID, EventSequence: 8,
		EventID: workflowUUID(t, "019a8f00-0020-7000-8000-000000000020"), EntityID: evidenceID,
		ActorHumanID: work.Reader.Human.HumanID, OccurredAt: created, Payload: protocol.EvidenceAttached{},
	}}
	radar := workflowTestRadar(t, work)
	radar.AsOfEvent = 10
	radar.ActiveIntents = []protocol.IntentRevision{intent}
	decisionID := workflowUUID(t, "019a8f00-0017-7000-8000-000000000017")
	contexts := map[uuid.UUID]protocol.ConflictContextPack{
		collisionID: {
			ExistingDecisionRefs: []uuid.UUID{decisionID}, UnresolvedQuestion: "Which Intent lands first?",
			Uncertainties: []string{"Merge order remains unknown."},
		},
	}

	snapshot := buildHandoff(work, radar, contexts)
	if snapshot.Intent.IntentID != intent.IntentID || !snapshot.Intent.Active || len(snapshot.DeclaredClaims) != 1 ||
		len(snapshot.ObservedClaims) != 1 || len(snapshot.Collisions) != 1 || len(snapshot.DecisionRefs) != 1 ||
		len(snapshot.EvidenceCoverage) != 1 || len(snapshot.Updates) != 1 || snapshot.ContinuationAfter == nil ||
		len(snapshot.UnresolvedItems) < 3 || snapshot.CompletionBoundary != completionBoundaryNote {
		t.Fatalf("handoff snapshot = %+v", snapshot)
	}
	encoded, err := renderHandoffJSON(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	var decoded handoffSnapshot
	if err := json.Unmarshal([]byte(encoded), &decoded); err != nil || decoded.AsOfEvent != 10 || decoded.DecisionRefs[0].DecisionID != decisionID {
		t.Fatalf("JSON handoff = %+v, %v", decoded, err)
	}
	markdown := renderHandoffMarkdown(snapshot)
	for _, required := range []string{"Observed claim", decisionID.String(), "`cursor`: `missing`", "Event `8`", "Continue updates after: `9`", completionBoundaryNote} {
		if !strings.Contains(markdown, required) {
			t.Errorf("Markdown handoff omitted %q", required)
		}
	}
	for _, forbidden := range []string{work.Focus.Objective.OverviewMarkdown, intent.SummaryMarkdown, "source_text", "transcript"} {
		if strings.Contains(encoded, forbidden) || strings.Contains(markdown, forbidden) {
			t.Errorf("handoff exposed forbidden content %q", forbidden)
		}
	}
}
