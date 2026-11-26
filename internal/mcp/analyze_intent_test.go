package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"uuid"

	"github.com/Juanqu261/conn/internal/analyzer"
	"github.com/Juanqu261/conn/internal/httpclient"
	"github.com/Juanqu261/conn/internal/protocol"
	"github.com/Juanqu261/conn/internal/repositorygit"
)

func TestAnalyzeIntentRunsBoundedSubprocessAndReturnsPostCommitState(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("Node.js is required for the MCP analyzer subprocess scenario")
	}
	script := writeAnalyzerScript(t, `
let input = "";
process.stdin.setEncoding("utf8");
process.stdin.on("data", chunk => input += chunk);
process.stdin.on("end", () => {
  const request = JSON.parse(input);
  console.log(JSON.stringify({
    report_version: 1,
    repository: {canonical_remote: request.canonical_remote},
    claims: [{subject_kind:"code_symbol", normalized_subject:"symbol:typescript:src/payment.ts/charge", display_name:"charge", relation:"affects", provenance_kind:"static_analysis"}],
    diagnostics: [{code:"TS2322", message:"TypeScript diagnostic TS2322", path:"src/payment.ts", line:1}]
  }));
});`)
	harness := newAnalysisHarness(t, node, script, false)
	t.Setenv("CONN_TOKEN", "conn_sess_must_not_reach_analyzer")

	_, body, err := harness.tools.analyzeIntent(context.Background(), nil, analyzeIntentInput{
		ProjectID: harness.projectID.String(), IntentID: harness.intentID.String(), ExpectedRevision: 1,
		Paths: []string{"src/payment.ts"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if harness.attachCalls.Load() != 1 || harness.attachedReport.ReportVersion != 1 {
		t.Fatalf("analysis attachment = calls %d, report %#v", harness.attachCalls.Load(), harness.attachedReport)
	}
	if len(harness.attachedReport.Claims) != 1 || strings.Contains(harness.attachedJSON, "conn_sess_") || strings.Contains(harness.attachedJSON, "source_text") {
		t.Fatalf("unsafe or missing attached report: %s", harness.attachedJSON)
	}
	var envelope struct {
		Data      analyzeIntentOutput `json:"data"`
		AsOfEvent int64               `json:"as_of_event"`
	}
	raw, ok := body.(json.RawMessage)
	if !ok {
		t.Fatalf("structured result type = %T", body)
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.AsOfEvent != 6 || envelope.Data.IntentRevision != 1 || len(envelope.Data.ObservedClaims) != 1 ||
		envelope.Data.ObservedClaims[0].ClaimID == (uuid.UUID{}) || !envelope.Data.Incomplete ||
		len(envelope.Data.Diagnostics) != 1 || len(envelope.Data.Collisions) != 1 {
		t.Fatalf("analysis output = %#v at event %d", envelope.Data, envelope.AsOfEvent)
	}
	if harness.expectedRevision != "1" {
		t.Fatalf("attachment expected_revision = %q, want 1", harness.expectedRevision)
	}
}

func TestAnalyzeIntentStopsBeforeAttachmentOnUnsafeOrStaleInput(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("Node.js is required for the MCP analyzer failure scenarios")
	}
	validScript := writeAnalyzerScript(t, `console.log(JSON.stringify({report_version:1,repository:{canonical_remote:"github.com/acme/payments"},claims:[],diagnostics:[]}));`)
	forbiddenScript := writeAnalyzerScript(t, `console.log(JSON.stringify({report_version:1,repository:{canonical_remote:"github.com/acme/payments"},claims:[],diagnostics:[],source:"forbidden"}));`)
	failedScript := writeAnalyzerScript(t, `process.exit(2);`)

	tests := []struct {
		name             string
		script           string
		disabled         bool
		expectedRevision int
		want             string
	}{
		{name: "stale revision", script: validScript, expectedRevision: 2, want: "not expected revision"},
		{name: "disabled analysis", script: validScript, disabled: true, expectedRevision: 1, want: "disabled"},
		{name: "forbidden analyzer output", script: forbiddenScript, expectedRevision: 1, want: "invalid report"},
		{name: "analyzer process failure", script: failedScript, expectedRevision: 1, want: "process failed"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			harness := newAnalysisHarness(t, node, tc.script, tc.disabled)
			_, _, err := harness.tools.analyzeIntent(context.Background(), nil, analyzeIntentInput{
				ProjectID: harness.projectID.String(), IntentID: harness.intentID.String(), ExpectedRevision: tc.expectedRevision,
			})
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want text %q", err, tc.want)
			}
			if harness.attachCalls.Load() != 0 {
				t.Fatalf("unsafe analysis made %d attachment request(s)", harness.attachCalls.Load())
			}
		})
	}
}

func TestResolveAnalysisWorkspaceRejectsTraversalAndUnrelatedWorktrees(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "src"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "src", "payment.ts"), []byte("export const charge = 1;"), 0o600); err != nil {
		t.Fatal(err)
	}
	worktree, paths, err := resolveAnalysisWorkspace(root, "", []string{"src/payment.ts"})
	if err != nil || worktree == "" || len(paths) != 1 || paths[0] != "src/payment.ts" {
		t.Fatalf("valid workspace = %q %v, %v", worktree, paths, err)
	}
	for _, path := range []string{"../secret.ts", "src/../payment.ts", filepath.Join(root, "src", "payment.ts")} {
		if _, _, err := resolveAnalysisWorkspace(root, "", []string{path}); err == nil {
			t.Errorf("unsafe path %q was accepted", path)
		}
	}
	if _, _, err := resolveAnalysisWorkspace(root, t.TempDir(), nil); err == nil {
		t.Error("unrelated worktree was accepted")
	}

	outside := t.TempDir()
	link := filepath.Join(root, "outside-link")
	if err := os.Symlink(outside, link); err == nil {
		if _, _, err := resolveAnalysisWorkspace(root, "", []string{"outside-link"}); err == nil {
			t.Error("symlink escape was accepted")
		}
	}
}

type analysisHarness struct {
	tools            tools
	projectID        uuid.UUID
	intentID         uuid.UUID
	attachCalls      atomic.Int64
	attachedReport   protocol.LocalAnalyzerReport
	attachedJSON     string
	expectedRevision string
}

func newAnalysisHarness(t *testing.T, node, script string, disabled bool) *analysisHarness {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "src"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "src", "payment.ts"), []byte("export const charge = 1;"), 0o600); err != nil {
		t.Fatal(err)
	}
	projectID := workflowUUID(t, "019a8f00-0001-7000-8000-000000000001")
	intentID := workflowUUID(t, "019a8f00-0012-7000-8000-000000000012")
	repositoryID := workflowUUID(t, "019a8f00-0010-7000-8000-000000000010")
	otherIntentID := workflowUUID(t, "019a8f00-0013-7000-8000-000000000013")
	created := protocol.NewTimestamp(time.Date(2026, 11, 22, 9, 0, 0, 0, time.UTC))
	work := workflowTestContext(t)
	intent := *work.Focus.SelectedIntent
	intent.RepositoryID = &repositoryID
	work.Focus.SelectedIntent = &intent
	work.OwnedActiveIntents = []protocol.IntentRevision{intent}
	repository := protocol.RepositoryConnection{
		RepositoryID: repositoryID, ProjectID: projectID, HostKind: "github", CanonicalRemote: "github.com/acme/payments",
		AnalysisEnabled: !disabled, CreatedByHumanID: work.Reader.Human.HumanID, CreatedAt: created,
	}
	claim := protocol.SemanticClaim{
		ClaimID: workflowUUID(t, "019a8f00-0014-7000-8000-000000000014"), ProjectID: projectID,
		IntentID: intentID, IntentRevision: 1, SubjectKind: "code_symbol",
		NormalizedSubject: "symbol:typescript:src/payment.ts/charge", DisplayName: "charge", Relation: "affects",
		ProvenanceKind: "static_analysis", ProducerParticipantID: &intent.ParticipantID, ObservedAt: created,
	}
	collision := protocol.CollisionAssessment{
		CollisionID: workflowUUID(t, "019a8f00-0016-7000-8000-000000000016"), ProjectID: projectID,
		LeftIntentID: intentID, LeftIntentRevision: 1, RightIntentID: otherIntentID, RightIntentRevision: 1,
		RuleID: "shared_exported_symbol", Severity: "amber", Confidence: 1, State: "open",
		MatchingClaimRefs:   []uuid.UUID{workflowUUID(t, "019a8f00-0014-7000-8000-000000000014"), workflowUUID(t, "019a8f00-0015-7000-8000-000000000015")},
		ExplanationMarkdown: "Both Intents affect the same exported symbol.", CreatedAt: created,
	}
	radar := protocol.RadarView{
		Project:   protocol.Project{ProjectID: projectID, Name: work.Project.Name, OwnerHumanID: work.Reader.Human.HumanID, CreatedAt: created},
		AsOfEvent: 6, Objectives: []protocol.RadarObjective{}, Repositories: []protocol.RepositoryConnection{repository},
		ActiveIntents: []protocol.IntentRevision{intent}, SemanticClaims: []protocol.SemanticClaim{claim},
		OpenCollisions: []protocol.CollisionAssessment{collision}, OpenQuestions: []string{}, Assumptions: []string{},
		EvidenceCoverage: []protocol.RadarEvidenceCoverage{}, KnownUnknowns: []string{},
	}
	harness := &analysisHarness{projectID: projectID, intentID: intentID}
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/work-context"):
			if r.URL.Query().Get("intent_id") != intentID.String() {
				http.Error(w, `{"error":{"code":"invalid_input","message":"missing intent"}}`, http.StatusUnprocessableEntity)
				return
			}
			work.RelatedCollisions = []protocol.CollisionAssessment{}
			work.AsOfEvent = 4
			writeAnalysisDocument(t, w, work, 4)
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/radar"):
			writeAnalysisDocument(t, w, radar, 6)
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/repositories"):
			writeAnalysisDocument(t, w, protocol.RepositoryConnectionList{ProjectID: projectID, Repositories: []protocol.RepositoryConnection{repository}}, 4)
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/analysis"):
			harness.attachCalls.Add(1)
			harness.expectedRevision = r.URL.Query().Get("expected_revision")
			var raw json.RawMessage
			if err := json.NewDecoder(r.Body).Decode(&raw); err != nil {
				t.Fatal(err)
			}
			harness.attachedJSON = string(raw)
			harness.attachedReport, _ = protocol.Decode[protocol.LocalAnalyzerReport](raw)
			writeAnalysisDocument(t, w, protocol.IntentView{IntentRevision: intent, Status: "active", AdvisoryObservations: []string{}}, 5)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(backend.Close)
	client, err := httpclient.New(backend.URL, backend.Client())
	if err != nil {
		t.Fatal(err)
	}
	harness.tools = tools{cfg: Config{
		Client:        func() (*httpclient.Client, error) { return client, nil },
		Analyzer:      analyzer.Config{NodePath: node, AnalyzerPath: script, Timeout: 2 * time.Second},
		WorkspaceRoot: root,
		DiscoverRepositoryAt: func(_ context.Context, worktree string) (repositorygit.Metadata, error) {
			if worktree != root {
				return repositorygit.Metadata{}, fmt.Errorf("unexpected worktree %q", worktree)
			}
			return repositorygit.Metadata{HostKind: "github", CanonicalRemote: repository.CanonicalRemote}, nil
		},
	}, selection: newProjectSelection()}
	return harness
}

func writeAnalyzerScript(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "analyzer.mjs")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func writeAnalysisDocument[T protocol.Document](t *testing.T, w http.ResponseWriter, document T, cursor int64) {
	t.Helper()
	data, err := protocol.Encode(document)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = fmt.Fprintf(w, `{"data":%s,"as_of_event":%d}`, data, cursor)
}
