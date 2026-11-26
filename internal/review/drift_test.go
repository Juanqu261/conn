package review

import (
	"fmt"
	"testing"
	"time"

	"uuid"

	"github.com/Juanqu261/conn/internal/protocol"
)

// TestDrift protects requirement 1: declared and observed code-symbol and
// contract subjects are compared only through changes and removes; changed
// files without a declared claim carry the written-by-agent flag; claims that
// analysis cannot observe link to acceptance evidence; a missing or older
// analysis is reported as not analyzed, never as no drift; an incomplete
// analysis says so; and every list is bounded with an omitted count.
func TestDrift(t *testing.T) {
	intentID := uuid.NewV7()
	at := protocol.NewTimestamp(time.Date(2026, 11, 22, 9, 0, 0, 0, time.UTC))
	retry := "contract:typescript:src/payments/retry.ts/shouldRetryCharge"
	attempt := "contract:typescript:src/payments/retry.ts/ChargeAttempt"
	refund := "contract:typescript:src/payments/refund.ts/refund"
	declared := func(kind, subject, relation string, paths ...string) protocol.SemanticClaimCandidate {
		claim := protocol.SemanticClaimCandidate{SubjectKind: kind, NormalizedSubject: subject, DisplayName: subject, Relation: relation, ProvenanceKind: "declared"}
		for _, path := range paths {
			claim.EvidenceRefs = append(claim.EvidenceRefs, protocol.ClaimEvidenceRef{Kind: "source_location", Locator: path, Path: path})
		}
		return claim
	}
	observed := func(subject, relation string, revision int) protocol.SemanticClaim {
		return protocol.SemanticClaim{IntentID: intentID, IntentRevision: revision, SubjectKind: "contract", NormalizedSubject: subject, DisplayName: subject, Relation: relation, ProvenanceKind: "static_analysis"}
	}
	analysis := func(revision int, status string, codes ...string) *protocol.WorkContextAnalysis {
		return &protocol.WorkContextAnalysis{IntentRevision: revision, Status: status, ObservedAt: at, DiagnosticCodes: codes}
	}
	subjects := func(items []protocol.DriftSubject) []string {
		result := []string{}
		for _, item := range items {
			result = append(result, item.NormalizedSubject+" "+item.Relation)
		}
		return result
	}
	many := make([]protocol.SemanticClaim, 0, 150)
	for i := range 150 {
		many = append(many, observed(fmt.Sprintf("contract:typescript:src/gen.ts/export%03d", i), "changes", 2))
	}

	tests := []struct {
		name  string
		in    DriftInput
		check func(t *testing.T, drift protocol.ScopeDrift)
	}{
		{
			name: "matched and both differences",
			in: DriftInput{
				Declared: []protocol.SemanticClaimCandidate{declared("contract", retry, "changes"), declared("contract", refund, "removes")},
				Observed: []protocol.SemanticClaim{observed(retry, "changes", 2), observed(attempt, "changes", 2), observed(attempt, "changes", 1)},
				Analysis: analysis(2, protocol.AnalysisComplete),
			},
			check: func(t *testing.T, drift protocol.ScopeDrift) {
				if drift.Analysis.Status != protocol.AnalysisComplete || fmt.Sprint(subjects(drift.Matched)) != fmt.Sprint([]string{retry + " changes"}) ||
					fmt.Sprint(subjects(drift.DeclaredNotObserved)) != fmt.Sprint([]string{refund + " removes"}) ||
					fmt.Sprint(subjects(drift.ObservedNotDeclared)) != fmt.Sprint([]string{attempt + " changes"}) {
					t.Fatalf("drift = %+v", drift)
				}
			},
		},
		{
			name: "consumes and produces differences are not drift",
			in: DriftInput{
				Declared: []protocol.SemanticClaimCandidate{declared("contract", retry, "consumes")},
				Observed: []protocol.SemanticClaim{observed(attempt, "consumes", 2), observed(refund, "produces", 2)},
				Analysis: analysis(2, protocol.AnalysisComplete),
			},
			check: func(t *testing.T, drift protocol.ScopeDrift) {
				if len(drift.Matched)+len(drift.DeclaredNotObserved)+len(drift.ObservedNotDeclared) != 0 {
					t.Fatalf("drift = %+v", drift)
				}
			},
		},
		{
			name: "not analyzed reports no comparison",
			in: DriftInput{
				Declared: []protocol.SemanticClaimCandidate{declared("contract", retry, "changes")},
				Observed: []protocol.SemanticClaim{observed(attempt, "changes", 2)},
			},
			check: func(t *testing.T, drift protocol.ScopeDrift) {
				if drift.Analysis.Status != protocol.AnalysisNotAnalyzed || drift.Analysis.IntentRevision != nil ||
					len(drift.Matched)+len(drift.DeclaredNotObserved)+len(drift.ObservedNotDeclared) != 0 {
					t.Fatalf("drift = %+v", drift)
				}
			},
		},
		{
			name: "an analysis of an earlier revision is not current",
			in: DriftInput{
				Declared: []protocol.SemanticClaimCandidate{declared("contract", retry, "changes")},
				Analysis: analysis(1, protocol.AnalysisComplete),
			},
			check: func(t *testing.T, drift protocol.ScopeDrift) {
				if drift.Analysis.Status != protocol.AnalysisNotAnalyzed || drift.Analysis.IntentRevision == nil || *drift.Analysis.IntentRevision != 1 || len(drift.DeclaredNotObserved) != 0 {
					t.Fatalf("drift = %+v", drift)
				}
			},
		},
		{
			name: "incomplete analysis says so",
			in:   DriftInput{Analysis: analysis(2, protocol.AnalysisIncomplete, "compiler_error")},
			check: func(t *testing.T, drift protocol.ScopeDrift) {
				if drift.Analysis.Status != protocol.AnalysisIncomplete || fmt.Sprint(drift.Analysis.DiagnosticCodes) != "[compiler_error]" {
					t.Fatalf("drift = %+v", drift)
				}
			},
		},
		{
			name: "changed files without a declared claim carry the written flag",
			in: DriftInput{
				Declared:     []protocol.SemanticClaimCandidate{declared("contract", retry, "changes"), declared("invariant", "invariant:checkout:one-charge", "affects", "src/orders/checkout.ts")},
				ChangedFiles: []string{"src/payments/retry.ts", "src/orders/checkout.ts", ".conn/checks.json", "README.md"},
				Written:      map[string]bool{".conn/checks.json": true},
			},
			check: func(t *testing.T, drift protocol.ScopeDrift) {
				want := []protocol.DriftFile{{Path: ".conn/checks.json", WrittenByAgent: true}, {Path: "README.md"}}
				if fmt.Sprint(drift.ChangedFilesWithoutClaim) != fmt.Sprint(want) {
					t.Fatalf("files without claim = %+v, want %+v", drift.ChangedFilesWithoutClaim, want)
				}
			},
		},
		{
			name: "claims analysis cannot observe link to acceptance evidence",
			in: DriftInput{
				Declared: []protocol.SemanticClaimCandidate{
					declared("acceptance_criterion", "acceptance:checkout:retry-evidence", "verifies"),
					declared("invariant", "invariant:checkout:one-charge", "affects"),
					declared("contract", retry, "changes"),
				},
				Acceptance: map[string]string{"retry-evidence": "passing"},
			},
			check: func(t *testing.T, drift protocol.ScopeDrift) {
				if len(drift.NotObservable) != 2 || drift.NotObservable[0].AcceptanceKey != "retry-evidence" || drift.NotObservable[0].EvidenceState != "passing" ||
					drift.NotObservable[1].AcceptanceKey != "" {
					t.Fatalf("not observable = %+v", drift.NotObservable)
				}
			},
		},
		{
			name: "lists are bounded with omitted counts",
			in:   DriftInput{Observed: many, Analysis: analysis(2, protocol.AnalysisComplete)},
			check: func(t *testing.T, drift protocol.ScopeDrift) {
				if len(drift.ObservedNotDeclared) != MaxDriftItems || drift.Omitted.ObservedNotDeclared != 50 {
					t.Fatalf("observed = %d, omitted = %d", len(drift.ObservedNotDeclared), drift.Omitted.ObservedNotDeclared)
				}
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tc.in.IntentID, tc.in.CurrentRevision = intentID, 2
			tc.check(t, Drift(tc.in))
		})
	}
}
