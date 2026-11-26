package postgres_test

import (
	"fmt"
	"strings"
	"testing"

	"uuid"

	"github.com/Juanqu261/conn/internal/application"
	"github.com/Juanqu261/conn/internal/protocol"
)

// TestConceptCatalogAndNearMatchAdvice protects requirements 2 and 3:
// Objective-derived subjects stay stable across revisions, active claim
// subjects are listed with their Intents, ranking and paging are
// deterministic and bounded, and near-match advice is bounded and changes no
// Intent, claim, or collision.
func TestConceptCatalogAndNearMatchAdvice(t *testing.T) {
	ctx, store, projects, owner, projectID, _, leftParticipant, rightParticipant := assessmentFixture(t)
	keyed, unkeyed := uuid.NewV7(), uuid.NewV7()
	points := []protocol.KeyPoint{
		{Key: "one-charge-per-attempt", Kind: protocol.KeyPointInvariant, Statement: "A retried checkout creates at most one charge per attempt key."},
		{Key: "retry-evidence", Kind: protocol.KeyPointAcceptanceCriterion, Statement: "Retry behavior has passing automated evidence."},
		{Key: "retry-question", Kind: protocol.KeyPointQuestion, Statement: "Which provider limits apply?"},
	}
	objective := protocol.ObjectiveRevisionInput{ObjectiveID: keyed, Key: "checkout", Title: "Reliable checkout", OverviewMarkdown: "Survive outages.", AccountableHumanID: owner, KeyPoints: points}
	if _, err := projects.CreateObjectiveRevision(ctx, owner, projectID, objective, 0); err != nil {
		t.Fatal(err)
	}
	if _, err := projects.CreateObjectiveRevision(ctx, owner, projectID, protocol.ObjectiveRevisionInput{
		ObjectiveID: unkeyed, Title: "Settlement", OverviewMarkdown: "Reconcile.", AccountableHumanID: owner,
		KeyPoints: []protocol.KeyPoint{{Key: "balanced-ledger", Kind: protocol.KeyPointInvariant, Statement: "Settlement ledgers balance."}},
	}, 0); err != nil {
		t.Fatal(err)
	}
	invariant, acceptance := "invariant:checkout:one-charge-per-attempt", "acceptance:checkout:retry-evidence"
	unkeyedSubject := "invariant:objective-" + unkeyed.String() + ":balanced-ledger"

	list := func(query application.ConceptQuery) protocol.ConceptList {
		t.Helper()
		result, err := projects.Concepts(ctx, owner, projectID, query)
		if err != nil {
			t.Fatal(err)
		}
		return result.Data
	}
	bySubject := func(list protocol.ConceptList) map[string]protocol.ConceptEntry {
		entries := map[string]protocol.ConceptEntry{}
		for _, entry := range list.Concepts {
			entries[entry.NormalizedSubject] = entry
		}
		return entries
	}
	derived := bySubject(list(application.ConceptQuery{}))
	if len(derived) != 3 || derived[invariant].SubjectKind != "invariant" || derived[acceptance].SubjectKind != "acceptance_criterion" ||
		derived[unkeyedSubject].ObjectiveRefs[0].ObjectiveID != unkeyed || derived[invariant].ObjectiveRefs[0].ObjectiveRevision != 1 {
		t.Fatalf("Objective-derived catalog = %+v", derived)
	}

	// A later revision that keeps both keys derives the same subjects.
	objective.OverviewMarkdown = "Survive provider outages."
	if _, err := projects.CreateObjectiveRevision(ctx, owner, projectID, objective, 1); err != nil {
		t.Fatal(err)
	}
	revised := bySubject(list(application.ConceptQuery{ObjectiveID: keyed}))
	if len(revised) != 2 || revised[invariant].ObjectiveRefs[0].ObjectiveRevision != 2 || revised[acceptance].ObjectiveRefs[0].ObjectiveRevision != 2 {
		t.Fatalf("catalog after the Objective revision = %+v", revised)
	}

	// Active claim subjects are listed with their Intents and relations.
	contract := "contract:typescript:src/payments/retry.ts/shouldRetryCharge"
	leftID := uuid.NewV7()
	if _, err := projects.PublishIntent(ctx, owner, projectID, leftParticipant, protocol.IntentInput{
		IntentID: leftID, ObjectiveID: keyed, ObjectiveRevision: 2, SummaryMarkdown: "Change the retry contract.",
		DeclaredClaims: []protocol.SemanticClaimCandidate{
			{SubjectKind: "contract", NormalizedSubject: contract, DisplayName: "shouldRetryCharge", Relation: "changes", ProvenanceKind: "declared"},
			{SubjectKind: "invariant", NormalizedSubject: invariant, DisplayName: "One charge", Relation: "affects", ProvenanceKind: "declared"},
		},
	}); err != nil {
		t.Fatal(err)
	}
	claimed := bySubject(list(application.ConceptQuery{}))
	if entry := claimed[contract]; entry.ClaimCount != 1 || len(entry.Claims) != 1 || entry.Claims[0].IntentID != leftID || entry.Claims[0].Relation != "changes" ||
		!slicesEqual(entry.Sources, []string{"active_claim"}) {
		t.Fatalf("claimed contract entry = %+v", entry)
	}
	if entry := claimed[invariant]; !slicesEqual(entry.Sources, []string{"active_claim", "objective"}) || entry.ClaimCount != 1 {
		t.Fatalf("claimed invariant entry = %+v", entry)
	}

	// Ranking is deterministic, explains itself, and pages with a bound.
	ranked := list(application.ConceptQuery{Query: "retry charge"})
	if len(ranked.Concepts) == 0 || ranked.Concepts[0].NormalizedSubject != contract || ranked.Concepts[0].Score == nil ||
		!strings.Contains(ranked.Concepts[0].Reason, "charge") || !strings.Contains(ranked.Concepts[0].Reason, "retry") {
		t.Fatalf("ranked catalog = %+v", ranked.Concepts)
	}
	for i := 1; i < len(ranked.Concepts); i++ {
		if *ranked.Concepts[i].Score > *ranked.Concepts[i-1].Score {
			t.Fatalf("ranking is not ordered by score: %+v", ranked.Concepts)
		}
	}
	if again := list(application.ConceptQuery{Query: "retry charge"}); ranking(again) != ranking(ranked) {
		t.Fatal("the same query ranked differently")
	}
	if exact := list(application.ConceptQuery{Query: contract}); *exact.Concepts[0].Score != 1 || exact.Concepts[0].NormalizedSubject != contract {
		t.Fatalf("exact match = %+v", exact.Concepts[0])
	}
	page := list(application.ConceptQuery{Limit: 1})
	if len(page.Concepts) != 1 || !page.HasMore || page.ContinuationAfter != "1" {
		t.Fatalf("first page = %+v", page)
	}
	next := list(application.ConceptQuery{Limit: 1, After: page.ContinuationAfter})
	if len(next.Concepts) != 1 || next.Concepts[0].NormalizedSubject == page.Concepts[0].NormalizedSubject {
		t.Fatalf("second page = %+v", next)
	}
	if kinds := list(application.ConceptQuery{Kind: "contract"}); len(kinds.Concepts) != 1 || kinds.Concepts[0].NormalizedSubject != contract {
		t.Fatalf("kind filter = %+v", kinds.Concepts)
	}

	// A near-miss subject is advised but stored exactly as declared and opens
	// nothing. Advice is bounded to 20 observations per revision.
	nearMisses := make([]protocol.SemanticClaimCandidate, 0, 25)
	for i := 0; i < 25; i++ {
		nearMisses = append(nearMisses, protocol.SemanticClaimCandidate{
			SubjectKind: "contract", NormalizedSubject: fmt.Sprintf("contract:typescript:src/payment/%c%d/retry.ts/shouldRetryCharge", 'a'+i%26, i%10),
			DisplayName: "shouldRetryCharge", Relation: "consumes", ProvenanceKind: "declared",
		})
	}
	rightID := uuid.NewV7()
	published, err := projects.PublishIntent(ctx, owner, projectID, rightParticipant, protocol.IntentInput{
		IntentID: rightID, ObjectiveID: keyed, ObjectiveRevision: 2, SummaryMarkdown: "Consume the retry contract.", DeclaredClaims: nearMisses,
	})
	if err != nil {
		t.Fatal(err)
	}
	advice := nearMatches(published.Data.AdvisoryObservations)
	if len(advice) != 20 || !strings.Contains(advice[0], nearMisses[0].NormalizedSubject) || !strings.Contains(advice[0], contract) || !strings.Contains(advice[0], leftID.String()) {
		t.Fatalf("near-match observations = %d %q", len(advice), advice)
	}
	read, err := projects.Intent(ctx, owner, projectID, rightID)
	if err != nil || len(nearMatches(read.Data.AdvisoryObservations)) != 20 {
		t.Fatalf("recomputed observations = %+v, %v", read.Data.AdvisoryObservations, err)
	}
	var stored, collisions int
	if err := store.Pool.QueryRow(ctx, `SELECT count(*) FROM semantic_claims WHERE intent_id = $1 AND normalized_subject LIKE 'contract:typescript:src/payment/%'`, rightID).Scan(&stored); err != nil || stored != 25 {
		t.Fatalf("stored declared claims = %d, %v", stored, err)
	}
	if err := store.Pool.QueryRow(ctx, `SELECT count(*) FROM collision_assessments WHERE project_id = $1`, projectID).Scan(&collisions); err != nil || collisions != 0 {
		t.Fatalf("near-match advice opened %d collisions, %v", collisions, err)
	}
	// A subject already in the catalog gets no advice.
	sameID := uuid.NewV7()
	same, err := projects.PublishIntent(ctx, owner, projectID, rightParticipant, protocol.IntentInput{
		IntentID: sameID, ObjectiveID: keyed, ObjectiveRevision: 2, SummaryMarkdown: "Reuse the retry contract.",
		DeclaredClaims: []protocol.SemanticClaimCandidate{{SubjectKind: "contract", NormalizedSubject: contract, DisplayName: "shouldRetryCharge", Relation: "consumes", ProvenanceKind: "declared"}},
	})
	if err != nil || len(nearMatches(same.Data.AdvisoryObservations)) != 0 {
		t.Fatalf("exact subject advice = %+v, %v", same.Data.AdvisoryObservations, err)
	}
}

func ranking(list protocol.ConceptList) string {
	var out strings.Builder
	for _, entry := range list.Concepts {
		fmt.Fprintf(&out, "%s=%.6f;", entry.NormalizedSubject, *entry.Score)
	}
	return out.String()
}

func nearMatches(observations []string) []string {
	var advice []string
	for _, observation := range observations {
		if strings.HasPrefix(observation, "Near match:") {
			advice = append(advice, observation)
		}
	}
	return advice
}

func slicesEqual(left, right []string) bool {
	return fmt.Sprint(left) == fmt.Sprint(right)
}
