package postgres_test

import (
	"errors"
	"strings"
	"testing"

	"uuid"

	"github.com/Juanqu261/conn/internal/application"
	"github.com/Juanqu261/conn/internal/protocol"
)

// TestStaticAnalysisAttachment uses the active Docker PostgreSQL test database
// to prove a source-free report remains participant-owned, is immutable per
// revision, stores bounded diagnostics, and invokes Task 7's assessor.
func TestStaticAnalysisAttachment(t *testing.T) {
	ctx, store, projects, owner, projectID, objectiveID, left, right := assessmentFixture(t)
	repositoryID, leftIntent, rightIntent := uuid.NewV7(), uuid.NewV7(), uuid.NewV7()
	if _, err := projects.ConnectRepository(ctx, owner, projectID, protocol.RepositoryConnectInput{RepositoryID: repositoryID, HostKind: "github", CanonicalRemote: "github.com/acme/payments", AnalysisEnabled: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := projects.PublishIntent(ctx, owner, projectID, left, protocol.IntentInput{IntentID: leftIntent, ObjectiveID: objectiveID, ObjectiveRevision: 1, RepositoryID: &repositoryID, SummaryMarkdown: "Analyze the payment export."}); err != nil {
		t.Fatal(err)
	}
	publishAssessmentIntent(t, ctx, projects, owner, projectID, objectiveID, right, rightIntent, claim("code_symbol", "symbol:typescript:src/payment.ts/charge", "changes"))
	report := protocol.LocalAnalyzerReport{
		ReportVersion: 1,
		Repository:    protocol.AnalyzerRepository{CanonicalRemote: "github.com/acme/payments"},
		Claims: []protocol.SemanticClaimCandidate{{
			SubjectKind: "code_symbol", NormalizedSubject: "symbol:typescript:src/payment.ts/charge", DisplayName: "charge",
			Relation: "affects", ProvenanceKind: "static_analysis",
			EvidenceRefs: []protocol.ClaimEvidenceRef{{Kind: "source_location", Locator: "typescript:src/payment.ts:charge", Path: "src/payment.ts", Line: ptr(4)}},
		}},
		Diagnostics: []protocol.AnalyzerDiagnostic{{Code: "TS2322", Message: "TypeScript diagnostic TS2322", Path: "src/payment.ts", Line: ptr(7)}},
	}
	unsafe := report
	unsafe.Claims = append([]protocol.SemanticClaimCandidate(nil), report.Claims...)
	unsafe.Claims[0].DisplayName = "ghp_credential_value"
	if _, err := projects.AttachStaticAnalysis(ctx, owner, projectID, left, leftIntent, unsafe); !errors.Is(err, application.ErrInvalid) {
		t.Fatalf("unsafe report = %v, want invalid", err)
	}
	if _, err := projects.AttachStaticAnalysisAtRevision(ctx, owner, projectID, left, leftIntent, 2, report); !errors.Is(err, application.ErrStaleRevision) {
		t.Fatalf("stale analysis revision = %v, want stale revision", err)
	}
	attached, err := projects.AttachStaticAnalysisAtRevision(ctx, owner, projectID, left, leftIntent, 1, report)
	if err != nil || attached.Data.IntentRevision.IntentID != leftIntent {
		t.Fatalf("attach = %#v, %v", attached, err)
	}
	var claims, reports, diagnostics int
	if err := store.Pool.QueryRow(ctx, `SELECT count(*) FROM semantic_claims WHERE intent_id = $1 AND provenance_kind = 'static_analysis'`, leftIntent).Scan(&claims); err != nil || claims != 1 {
		t.Fatalf("static claims = %d, %v", claims, err)
	}
	if err := store.Pool.QueryRow(ctx, `SELECT count(*), jsonb_array_length(diagnostics) FROM intent_analysis_reports WHERE intent_id = $1 GROUP BY diagnostics`, leftIntent).Scan(&reports, &diagnostics); err != nil || reports != 1 || diagnostics != 1 {
		t.Fatalf("stored report = reports %d diagnostics %d err %v", reports, diagnostics, err)
	}
	if assessments := loadAssessments(t, ctx, store, leftIntent, rightIntent); len(assessments) != 1 || assessments[0].RuleID != "shared_exported_symbol" ||
		!strings.Contains(assessments[0].ExplanationMarkdown, "from declared and static_analysis claims") {
		t.Fatalf("static collision = %#v, want its explanation to name both provenances", assessments)
	}
	if _, err := projects.AttachStaticAnalysis(ctx, owner, projectID, left, leftIntent, report); err != nil {
		t.Fatalf("idempotent attach = %v", err)
	}
	if err := store.Pool.QueryRow(ctx, `SELECT count(*) FROM semantic_claims WHERE intent_id = $1 AND provenance_kind = 'static_analysis'`, leftIntent).Scan(&claims); err != nil || claims != 1 {
		t.Fatalf("duplicate static claims = %d, %v", claims, err)
	}
	if _, err := projects.AttachStaticAnalysis(ctx, owner, projectID, right, leftIntent, report); !errors.Is(err, application.ErrForbidden) {
		t.Fatalf("other participant attach = %v, want forbidden", err)
	}
	wrongRemote := report
	wrongRemote.Repository.CanonicalRemote = "github.com/acme/other"
	if _, err := projects.AttachStaticAnalysis(ctx, owner, projectID, left, leftIntent, wrongRemote); !errors.Is(err, application.ErrInvalid) {
		t.Fatalf("wrong report remote = %v, want invalid", err)
	}
}
