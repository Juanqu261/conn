package cli

import (
	"bytes"
	"strings"
	"testing"

	"uuid"

	"github.com/Juanqu261/conn/internal/protocol"
)

// TestExplainNamesClaimProvenance proves `conn explain` identifies each
// matching claim's subject, source, confidence, and evidence references
// rather than only opaque claim identifiers.
func TestExplainNamesClaimProvenance(t *testing.T) {
	declaredID, staticID, missingID := uuid.NewV7(), uuid.NewV7(), uuid.NewV7()
	confidence := 0.4
	view := protocol.RadarView{AsOfEvent: 7, SemanticClaims: []protocol.SemanticClaim{
		{ClaimID: declaredID, IntentID: uuid.NewV7(), IntentRevision: 2, SubjectKind: "code_symbol", Relation: "changes",
			NormalizedSubject: "symbol:typescript:src/payment.ts/charge", ProvenanceKind: "declared"},
		{ClaimID: staticID, IntentID: uuid.NewV7(), IntentRevision: 1, SubjectKind: "code_symbol", Relation: "affects",
			NormalizedSubject: "symbol:typescript:src/payment.ts/charge", ProvenanceKind: "static_analysis", Confidence: &confidence,
			EvidenceRefs: []protocol.ClaimEvidenceRef{{Kind: "source_location", Locator: "typescript:src/payment.ts:charge"}}},
	}}
	collision := protocol.CollisionAssessment{
		CollisionID: uuid.NewV7(), RuleID: "shared_exported_symbol", Severity: "amber", State: "open", Confidence: 1,
		MatchingClaimRefs: []uuid.UUID{declaredID, staticID, missingID}, ExplanationMarkdown: "Shared symbol.",
	}

	var out bytes.Buffer
	printCollision(&out, collision, view)

	text := out.String()
	for _, want := range []string{
		"provenance declared)",
		"provenance static_analysis, confidence 0.40)",
		"evidence source_location typescript:src/payment.ts:charge",
		missingID.String() + " (details unavailable",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("explain output lacks %q:\n%s", want, text)
		}
	}
}
