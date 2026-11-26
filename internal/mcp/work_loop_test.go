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
	"strconv"
	"strings"
	"testing"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"uuid"

	"github.com/Juanqu261/conn/internal/account"
	"github.com/Juanqu261/conn/internal/analyzer"
	"github.com/Juanqu261/conn/internal/credentials"
	"github.com/Juanqu261/conn/internal/httpclient"
	"github.com/Juanqu261/conn/internal/protocol"
	"github.com/Juanqu261/conn/internal/repositorygit"
	"github.com/Juanqu261/conn/internal/secret"
)

func TestParticipantWorkLoopEndToEnd(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("Node.js is required for the complete MCP work-loop scenario")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	fixture := newWorkLoopBackend(t)
	defer fixture.server.Close()
	serverClient, err := httpclient.New(fixture.server.URL, fixture.server.Client())
	if err != nil {
		t.Fatal(err)
	}
	store := &credentials.Memory{}
	service := &account.Service{
		Server: serverClient, Store: store, LookupEnv: func(string) (string, bool) { return "", false },
	}
	worktree := t.TempDir()
	if err := os.MkdirAll(filepath.Join(worktree, "src"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(worktree, "src", "workflow.ts"), []byte("export const run = true;"), 0o600); err != nil {
		t.Fatal(err)
	}
	script := writeAnalyzerScript(t, `
let input = "";
process.stdin.setEncoding("utf8");
process.stdin.on("data", chunk => input += chunk);
process.stdin.on("end", () => {
  const request = JSON.parse(input);
  console.log(JSON.stringify({report_version:2,mode:"observed",repository:{canonical_remote:request.canonical_remote},claims:[{
    subject_kind:"code_symbol",normalized_subject:"symbol:typescript:src/workflow.ts/run",display_name:"run",relation:"changes",provenance_kind:"static_analysis"
  }],diagnostics:[]}));
});`)

	server := sdk.NewServer(&sdk.Implementation{Name: "conn-work-loop-test", Version: "0"}, nil)
	interactions := registerTools(server, Config{
		Account: service, Client: service.Client, AnalyzerAvailable: func() bool { return true },
		Analyzer: analyzer.Config{NodePath: node, AnalyzerPath: script, Timeout: 2 * time.Second}, WorkspaceRoot: worktree,
		DiscoverRepositoryAt: func(context.Context, string) (repositorygit.Metadata, error) {
			return repositorygit.Metadata{HostKind: "github", CanonicalRemote: fixture.repository.CanonicalRemote}, nil
		},
	})
	defer interactions.clear()
	registerWorkflows(server, Config{Client: service.Client})
	client := sdk.NewClient(&sdk.Implementation{Name: "kiro-work-loop-test", Version: "0"}, nil)
	serverTransport, clientTransport := sdk.NewInMemoryTransports()
	serverSession, err := server.Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer serverSession.Close()
	session, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()

	// /conn-start begins with diagnostics. The same MCP process observes a
	// credential added by an interactive login without a restart.
	startPrompt, err := session.GetPrompt(ctx, &sdk.GetPromptParams{Name: "conn-start", Arguments: map[string]string{"focus": fixture.objective.ObjectiveID.String()}})
	if err != nil || len(startPrompt.Messages) != 1 {
		t.Fatalf("conn-start prompt: %+v, %v", startPrompt, err)
	}
	unauthenticated := callAccountTool[sessionStatusOutput](t, ctx, session, "session_status", nil)
	if unauthenticated.Credential.Valid || !hasString(unauthenticated.Problems, "not_authenticated") {
		t.Fatalf("unauthenticated start status = %+v", unauthenticated)
	}
	token := secret.New("conn_sess_" + strings.Repeat("A", 43))
	if err := store.Set(token); err != nil {
		t.Fatal(err)
	}
	resolvedClient, err := service.Client()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := resolvedClient.Me(ctx); err != nil {
		t.Fatalf("fixture authenticated Me: %v", err)
	}
	authenticated := callAccountTool[sessionStatusOutput](t, ctx, session, "session_status", nil)
	if !authenticated.Credential.Valid || authenticated.Human == nil || authenticated.Human.HumanID != fixture.human.HumanID.String() {
		t.Fatalf("authenticated start status = %+v", authenticated)
	}

	projects := callWorkLoopTool[projectListOutput](t, ctx, session, "list_projects", nil)
	if len(projects.Projects) != 1 || projects.Projects[0].ProjectID != fixture.project.ProjectID.String() {
		t.Fatalf("visible Projects = %+v", projects)
	}
	selected := callWorkLoopTool[projectSelectionOutput](t, ctx, session, "select_project", map[string]any{"project_id": fixture.project.ProjectID.String()})
	if selected.Selected.ProjectID != fixture.project.ProjectID.String() {
		t.Fatalf("selected Project = %+v", selected)
	}
	start := callWorkLoopEnvelope[protocol.WorkContext](t, ctx, session, "read_work_context", map[string]any{"objective_id": fixture.objective.ObjectiveID.String()})
	if start.Data.Focus == nil || start.Data.Focus.SelectedIntent != nil || len(start.Data.Focus.OwnCharters) != 1 ||
		start.Data.Focus.OwnCharters[0].Decision != protocol.CellAccepted {
		t.Fatalf("start context = %+v", start.Data)
	}

	published := callWorkLoopEnvelope[protocol.IntentView](t, ctx, session, "publish_intent", map[string]any{"intent": map[string]any{
		"intent_id": fixture.intentID.String(), "objective_id": fixture.objective.ObjectiveID.String(), "objective_revision": 1,
		"blueprint_id": fixture.blueprintID.String(), "blueprint_revision": 1, "charter_id": fixture.cellID.String(),
		"repository_id": fixture.repository.RepositoryID.String(), "summary_markdown": "Implement the exact accepted work cell.",
		"assumptions": []any{"The local worktree matches the selected repository."},
	}})
	if published.Data.IntentRevision.Revision != 1 || published.Data.IntentRevision.ParticipantID != fixture.participant.ParticipantID {
		t.Fatalf("published Intent = %+v", published.Data)
	}
	initialCollisions := callWorkLoopEnvelopeRaw(t, ctx, session, "check_collisions", map[string]any{"intent_id": fixture.intentID.String()})
	var initialGroups collisionGroups
	if err := json.Unmarshal(initialCollisions.Data, &initialGroups); err != nil || len(initialGroups.NeedsAttention) != 0 || len(initialGroups.Decided) != 0 {
		t.Fatalf("initial collisions = %s, %v", initialCollisions.Data, err)
	}

	// /conn-sync follows a bounded continuation and selectively rereads the
	// focused context. No timer or background operation is involved.
	if _, err := session.GetPrompt(ctx, &sdk.GetPromptParams{Name: "conn-sync", Arguments: map[string]string{"last_event": "1"}}); err != nil {
		t.Fatal(err)
	}
	pageOne := callWorkLoopEnvelopeRaw(t, ctx, session, "check_updates", map[string]any{"after": 1, "limit": 1})
	var firstEvents []json.RawMessage
	if err := json.Unmarshal(pageOne.Data, &firstEvents); err != nil || len(firstEvents) != 1 {
		t.Fatalf("first sync page = %s, %v", pageOne.Data, err)
	}
	first, err := protocol.Decode[protocol.EventEnvelope](firstEvents[0])
	if err != nil || first.EventSequence != 2 {
		t.Fatalf("first sync event = %+v, %v", first, err)
	}
	pageTwo := callWorkLoopEnvelopeRaw(t, ctx, session, "check_updates", map[string]any{"after": 2, "limit": 10})
	var laterEvents []json.RawMessage
	if err := json.Unmarshal(pageTwo.Data, &laterEvents); err != nil || len(laterEvents) < 2 || pageTwo.AsOfEvent != 4 {
		t.Fatalf("second sync page = %s at %d, %v", pageTwo.Data, pageTwo.AsOfEvent, err)
	}
	_ = callWorkLoopEnvelope[protocol.WorkContext](t, ctx, session, "read_work_context", map[string]any{"intent_id": fixture.intentID.String(), "after": pageTwo.AsOfEvent})

	// /conn-finish verifies the exact revision, runs local source-free analysis,
	// rechecks collisions, and attaches only explicit reference-first evidence.
	if _, err := session.GetPrompt(ctx, &sdk.GetPromptParams{Name: "conn-finish", Arguments: map[string]string{"intent_id": fixture.intentID.String()}}); err != nil {
		t.Fatal(err)
	}
	analysis := callWorkLoopEnvelopeRaw(t, ctx, session, "analyze_intent", map[string]any{
		"intent_id": fixture.intentID.String(), "expected_intent_revision": 1, "paths": []any{"src/workflow.ts"},
	})
	var analysisData analyzeIntentOutput
	if err := json.Unmarshal(analysis.Data, &analysisData); err != nil || len(analysisData.ObservedClaims) != 1 || len(analysisData.Collisions) != 1 {
		t.Fatalf("analysis result = %s, %v", analysis.Data, err)
	}
	collisions := callWorkLoopEnvelopeRaw(t, ctx, session, "check_collisions", map[string]any{"intent_id": fixture.intentID.String()})
	var collisionData collisionGroups
	if err := json.Unmarshal(collisions.Data, &collisionData); err != nil || len(collisionData.NeedsAttention) != 1 {
		t.Fatalf("finish collisions = %s, %v", collisions.Data, err)
	}
	evidence := callWorkLoopEnvelope[protocol.EvidenceReference](t, ctx, session, "attach_evidence", map[string]any{
		"intent_id": fixture.intentID.String(), "evidence": map[string]any{
			"objective_id": fixture.objective.ObjectiveID.String(), "objective_revision": 1, "acceptance_key": "work-loop",
			"kind": "test_case", "state": "passing", "locator": "test:participant-work-loop",
		},
	})
	if evidence.Data.State != "passing" || evidence.Data.Locator != "test:participant-work-loop" {
		t.Fatalf("attached evidence = %+v", evidence.Data)
	}
	finished := callWorkLoopEnvelope[protocol.WorkContext](t, ctx, session, "read_work_context", map[string]any{"intent_id": fixture.intentID.String()})
	if len(finished.Data.AcceptanceEvidenceCoverage) != 1 || finished.Data.AcceptanceEvidenceCoverage[0].State != "passing" {
		t.Fatalf("finish context evidence = %+v", finished.Data.AcceptanceEvidenceCoverage)
	}

	// /conn-handoff generates both bounded formats without a persisted handoff.
	if _, err := session.GetPrompt(ctx, &sdk.GetPromptParams{Name: "conn-handoff", Arguments: map[string]string{"intent_id": fixture.intentID.String(), "output": "json"}}); err != nil {
		t.Fatal(err)
	}
	mutationsBeforeHandoff := fixture.mutationCount
	baseURI := "conn://projects/" + fixture.project.ProjectID.String() + "/intents/" + fixture.intentID.String() + "/handoff"
	jsonResource, err := session.ReadResource(ctx, &sdk.ReadResourceParams{URI: baseURI + "?format=json&after=0"})
	if err != nil || len(jsonResource.Contents) != 1 {
		t.Fatalf("JSON handoff: %+v, %v", jsonResource, err)
	}
	var handoff handoffSnapshot
	if err := json.Unmarshal([]byte(jsonResource.Contents[0].Text), &handoff); err != nil || handoff.Intent.Revision != 1 ||
		len(handoff.ObservedClaims) != 1 || len(handoff.Collisions) != 1 || len(handoff.EvidenceCoverage) != 1 ||
		handoff.EvidenceCoverage[0].State != "passing" || handoff.AsOfEvent != 7 {
		t.Fatalf("generated JSON handoff = %+v, %v", handoff, err)
	}
	markdownResource, err := session.ReadResource(ctx, &sdk.ReadResourceParams{URI: baseURI})
	if err != nil || len(markdownResource.Contents) != 1 || !strings.Contains(markdownResource.Contents[0].Text, completionBoundaryNote) ||
		strings.Contains(markdownResource.Contents[0].Text, "export const run") {
		t.Fatalf("generated Markdown handoff: %+v, %v", markdownResource, err)
	}
	if fixture.mutationCount != mutationsBeforeHandoff {
		t.Fatalf("generated handoff changed mutation count from %d to %d", mutationsBeforeHandoff, fixture.mutationCount)
	}

	released := callWorkLoopEnvelope[protocol.IntentView](t, ctx, session, "release_intent", map[string]any{"intent_id": fixture.intentID.String()})
	if released.Data.Status != "released" || !strings.Contains(completionBoundaryNote, "does not prove") {
		t.Fatalf("released Intent = %+v", released.Data)
	}
}

type workLoopBackend struct {
	server        *httptest.Server
	human         protocol.Human
	participant   protocol.Participant
	project       protocol.Project
	objective     protocol.Objective
	repository    protocol.RepositoryConnection
	blueprintID   uuid.UUID
	cellID        uuid.UUID
	intentID      uuid.UUID
	otherIntent   protocol.IntentRevision
	intent        *protocol.IntentRevision
	active        bool
	analyzed      bool
	evidence      bool
	asOf          int64
	mutationCount int
}

func newWorkLoopBackend(t *testing.T) *workLoopBackend {
	t.Helper()
	created := protocol.NewTimestamp(time.Date(2026, 11, 23, 9, 0, 0, 0, time.UTC))
	projectID := workflowUUID(t, "019a8f10-0001-7000-8000-000000000001")
	humanID := workflowUUID(t, "019a8f10-0000-7000-8000-00000000000b")
	participantID := workflowUUID(t, "019a8f10-0011-7000-8000-000000000011")
	objectiveID := workflowUUID(t, "019a8f10-0002-7000-8000-000000000002")
	repositoryID := workflowUUID(t, "019a8f10-0010-7000-8000-000000000010")
	blueprintID := workflowUUID(t, "019a8f10-0003-7000-8000-000000000003")
	cellID := workflowUUID(t, "019a8f10-0004-7000-8000-000000000004")
	intentID := workflowUUID(t, "019a8f10-0012-7000-8000-000000000012")
	backend := &workLoopBackend{
		human:       protocol.Human{HumanID: humanID, GitHubUserID: 4242, GitHubLogin: "octo-worker"},
		participant: protocol.Participant{ParticipantID: participantID, ProjectID: projectID, HumanID: humanID, AdapterKind: "kiro", CreatedAt: created},
		project:     protocol.Project{ProjectID: projectID, Name: "Work loop", OwnerHumanID: humanID, CreatedAt: created},
		objective: protocol.Objective{
			ObjectiveID: objectiveID, ProjectID: projectID, Revision: 1, Title: "Complete Kiro work loop",
			OverviewMarkdown: "Exercise the participant work loop.", AccountableHumanID: humanID,
			KeyPoints: []protocol.KeyPoint{{Key: "work-loop", Kind: protocol.KeyPointAcceptanceCriterion, Statement: "The complete work loop passes."}}, CreatedAt: created,
		},
		repository: protocol.RepositoryConnection{
			RepositoryID: repositoryID, ProjectID: projectID, HostKind: "github", CanonicalRemote: "github.com/acme/work-loop",
			AnalysisEnabled: true, CreatedByHumanID: humanID, CreatedAt: created,
		},
		blueprintID: blueprintID, cellID: cellID, intentID: intentID, active: false, asOf: 3,
		otherIntent: protocol.IntentRevision{
			IntentID: workflowUUID(t, "019a8f10-0013-7000-8000-000000000013"), ProjectID: projectID,
			ParticipantID: workflowUUID(t, "019a8f10-0019-7000-8000-000000000019"), Revision: 1,
			ObjectiveID: objectiveID, ObjectiveRevision: 1, SummaryMarkdown: "Coordinate another work cell.", CreatedAt: created,
		},
	}
	backend.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { backend.handle(t, w, r) }))
	return backend
}

func (b *workLoopBackend) handle(t *testing.T, w http.ResponseWriter, r *http.Request) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	if r.Method == http.MethodPost {
		b.mutationCount++
	}
	projectPath := "/v1/projects/" + b.project.ProjectID.String()
	switch {
	case r.Method == http.MethodGet && (r.URL.Path == "/healthz" || r.URL.Path == "/readyz"):
		_, _ = w.Write([]byte(`{"ok":true}`))
	case r.Method == http.MethodGet && r.URL.Path == "/v1/humans/me":
		writeWorkLoopDirect(t, w, b.human)
	case r.Method == http.MethodGet && r.URL.Path == "/v1/projects":
		writeWorkLoopDirect(t, w, protocol.ProjectList{Projects: []protocol.ProjectListEntry{{Project: b.project, Role: protocol.RoleOwner}}})
	case r.Method == http.MethodGet && r.URL.Path == projectPath+"/repositories":
		writeAnalysisDocument(t, w, protocol.RepositoryConnectionList{ProjectID: b.project.ProjectID, Repositories: []protocol.RepositoryConnection{b.repository}}, b.asOf)
	case r.Method == http.MethodGet && r.URL.Path == projectPath+"/work-context":
		after := int64(-1)
		if raw := r.URL.Query().Get("after"); raw != "" {
			after, _ = strconv.ParseInt(raw, 10, 64)
		}
		writeAnalysisDocument(t, w, b.workContext(after, queryLimit(r, 100)), b.asOf)
	case r.Method == http.MethodPost && r.URL.Path == projectPath+"/intents":
		var raw json.RawMessage
		_ = json.NewDecoder(r.Body).Decode(&raw)
		input, err := protocol.Decode[protocol.IntentInput](raw)
		if err != nil {
			t.Fatal(err)
		}
		revision := protocol.IntentRevision{
			IntentID: input.IntentID, ProjectID: b.project.ProjectID, ParticipantID: b.participant.ParticipantID, Revision: 1,
			ObjectiveID: input.ObjectiveID, ObjectiveRevision: input.ObjectiveRevision, BlueprintID: input.BlueprintID,
			BlueprintRevision: input.BlueprintRevision, CharterID: input.CharterID, RepositoryID: input.RepositoryID,
			SummaryMarkdown: input.SummaryMarkdown, Assumptions: input.Assumptions, DeclaredClaims: input.DeclaredClaims,
			CreatedAt: protocol.NewTimestamp(time.Date(2026, 11, 23, 9, 5, 0, 0, time.UTC)),
		}
		b.intent, b.active, b.asOf = &revision, true, 4
		writeAnalysisDocument(t, w, protocol.IntentView{IntentRevision: revision, Status: "active", AdvisoryObservations: []string{}}, b.asOf)
	case r.Method == http.MethodGet && r.URL.Path == projectPath+"/events":
		b.writeEvents(t, w, r)
	case r.Method == http.MethodGet && r.URL.Path == projectPath+"/radar":
		writeAnalysisDocument(t, w, b.radar(), b.asOf)
	case r.Method == http.MethodPost && r.URL.Path == projectPath+"/intents/"+b.intentID.String()+"/analysis":
		if r.URL.Query().Get("expected_revision") != "1" {
			t.Fatalf("analysis expected_revision = %q", r.URL.Query().Get("expected_revision"))
		}
		var raw json.RawMessage
		_ = json.NewDecoder(r.Body).Decode(&raw)
		if _, err := protocol.Decode[protocol.LocalAnalyzerReport](raw); err != nil {
			t.Fatal(err)
		}
		b.analyzed, b.asOf = true, 6
		writeAnalysisDocument(t, w, protocol.IntentView{IntentRevision: *b.intent, Status: "active", AdvisoryObservations: []string{}}, b.asOf)
	case r.Method == http.MethodPost && r.URL.Path == projectPath+"/intents/"+b.intentID.String()+"/evidence":
		var raw json.RawMessage
		_ = json.NewDecoder(r.Body).Decode(&raw)
		input, err := protocol.Decode[protocol.EvidenceAttachmentInput](raw)
		if err != nil {
			t.Fatal(err)
		}
		b.evidence, b.asOf = true, 7
		intentID := b.intentID
		writeAnalysisDocument(t, w, protocol.EvidenceReference{
			EvidenceID: workflowUUID(t, "019a8f10-0018-7000-8000-000000000018"), ProjectID: b.project.ProjectID,
			IntentID: &intentID, ObjectiveID: input.ObjectiveID, ObjectiveRevision: input.ObjectiveRevision,
			AcceptanceKey: input.AcceptanceKey, Kind: input.Kind, State: input.State, Locator: input.Locator,
			ProducerParticipantID: &b.participant.ParticipantID, ObservedAt: protocol.NewTimestamp(time.Date(2026, 11, 23, 9, 10, 0, 0, time.UTC)),
		}, b.asOf)
	case r.Method == http.MethodGet && b.analyzed && r.URL.Path == projectPath+"/collisions/"+b.collision().CollisionID.String()+"/context":
		writeAnalysisDocument(t, w, b.conflictContext(), b.asOf)
	case r.Method == http.MethodPost && r.URL.Path == projectPath+"/intents/"+b.intentID.String()+"/release":
		b.active, b.asOf = false, 8
		writeAnalysisDocument(t, w, protocol.IntentView{IntentRevision: *b.intent, Status: "released", AdvisoryObservations: []string{}}, b.asOf)
	default:
		http.NotFound(w, r)
	}
}

func (b *workLoopBackend) workContext(after int64, limit int) protocol.WorkContext {
	view := protocol.WorkContext{
		Project:   protocol.WorkContextProject{ProjectID: b.project.ProjectID, Name: b.project.Name, Role: protocol.RoleOwner},
		Reader:    protocol.WorkContextReader{Human: b.human, Participant: &b.participant},
		Selection: protocol.WorkContextSelection{Source: protocol.WorkContextSelectionExplicit},
		Focus: &protocol.WorkContextFocus{
			Objective: b.objective,
			Blueprint: &protocol.WorkContextBlueprint{BlueprintID: b.blueprintID, Revision: 1, Status: protocol.BlueprintActive, ObjectiveID: b.objective.ObjectiveID, ObjectiveRevision: 1},
			OwnCharters: []protocol.OwnCharter{{Charter: protocol.CellCharter{
				CellID: b.cellID, AccountableHumanID: b.human.HumanID, Title: "Work loop", OverviewMarkdown: "Own the work loop.",
			}, Decision: protocol.CellAccepted}},
		},
		OwnedActiveIntents: []protocol.IntentRevision{}, RelatedCollisions: []protocol.CollisionAssessment{},
		AcceptanceEvidenceCoverage: []protocol.RadarEvidenceCoverage{{
			ObjectiveID: b.objective.ObjectiveID, ObjectiveRevision: 1, AcceptanceKey: "work-loop",
			State: map[bool]string{true: "passing", false: "missing"}[b.evidence], EvidenceRefs: []uuid.UUID{},
		}},
		Updates: []protocol.EventEnvelope{}, KnownUnknowns: []string{}, AsOfEvent: b.asOf,
	}
	if b.evidence {
		view.AcceptanceEvidenceCoverage[0].EvidenceRefs = []uuid.UUID{workflowUUIDNoTest("019a8f10-0018-7000-8000-000000000018")}
	}
	if b.intent != nil {
		view.Focus.SelectedIntent = b.intent
		if b.active {
			view.OwnedActiveIntents = []protocol.IntentRevision{*b.intent}
		}
	}
	if b.analyzed && b.active {
		view.RelatedCollisions = []protocol.CollisionAssessment{b.collision()}
	}
	if after >= 0 {
		all := b.events()
		for _, event := range all {
			if event.EventSequence > after && len(view.Updates) < limit {
				view.Updates = append(view.Updates, event)
			}
		}
		if len(view.Updates) > 0 && view.Updates[len(view.Updates)-1].EventSequence < b.asOf {
			cursor := view.Updates[len(view.Updates)-1].EventSequence
			view.ContinuationAfter = &cursor
		}
	}
	return view
}

func (b *workLoopBackend) radar() protocol.RadarView {
	active := []protocol.IntentRevision{}
	claims := []protocol.SemanticClaim{}
	collisions := []protocol.CollisionAssessment{}
	if b.intent != nil && b.active {
		active = append(active, *b.intent)
	}
	if b.analyzed {
		claims = []protocol.SemanticClaim{b.analysisClaim(), b.otherClaim()}
		if b.active {
			collisions = []protocol.CollisionAssessment{b.collision()}
		}
	}
	return protocol.RadarView{
		Project: b.project, AsOfEvent: b.asOf,
		Objectives:      []protocol.RadarObjective{{ObjectiveID: b.objective.ObjectiveID, Revision: 1, Title: b.objective.Title}},
		ActiveBlueprint: &protocol.RadarBlueprint{BlueprintID: b.blueprintID, Revision: 1},
		Repositories:    []protocol.RepositoryConnection{b.repository}, ActiveIntents: active, SemanticClaims: claims,
		OpenCollisions: collisions, OpenQuestions: []string{}, Assumptions: []string{},
		EvidenceCoverage: b.workContext(-1, 100).AcceptanceEvidenceCoverage, KnownUnknowns: []string{},
	}
}

func (b *workLoopBackend) analysisClaim() protocol.SemanticClaim {
	return protocol.SemanticClaim{
		ClaimID: workflowUUIDNoTest("019a8f10-0014-7000-8000-000000000014"), ProjectID: b.project.ProjectID,
		IntentID: b.intentID, IntentRevision: 1, SubjectKind: "code_symbol",
		NormalizedSubject: "symbol:typescript:src/workflow.ts/run", DisplayName: "run", Relation: "affects",
		ProvenanceKind: "static_analysis", ProducerParticipantID: &b.participant.ParticipantID,
		ObservedAt: protocol.NewTimestamp(time.Date(2026, 11, 23, 9, 8, 0, 0, time.UTC)),
	}
}

func (b *workLoopBackend) otherClaim() protocol.SemanticClaim {
	participantID := b.otherIntent.ParticipantID
	return protocol.SemanticClaim{
		ClaimID: workflowUUIDNoTest("019a8f10-0015-7000-8000-000000000015"), ProjectID: b.project.ProjectID,
		IntentID: b.otherIntent.IntentID, IntentRevision: 1, SubjectKind: "code_symbol",
		NormalizedSubject: "symbol:typescript:src/workflow.ts/run", DisplayName: "run", Relation: "affects",
		ProvenanceKind: "declared", ProducerParticipantID: &participantID,
		ObservedAt: protocol.NewTimestamp(time.Date(2026, 11, 23, 9, 1, 0, 0, time.UTC)),
	}
}

func (b *workLoopBackend) collision() protocol.CollisionAssessment {
	return protocol.CollisionAssessment{
		CollisionID: workflowUUIDNoTest("019a8f10-0016-7000-8000-000000000016"), ProjectID: b.project.ProjectID,
		LeftIntentID: b.intentID, LeftIntentRevision: 1, RightIntentID: b.otherIntent.IntentID, RightIntentRevision: 1,
		RuleID: "shared_exported_symbol", Severity: "amber", Confidence: 1, State: "open", MatchedSubjects: []string{"symbol:typescript:src/workflow.ts/run"},
		MatchingClaimRefs:   []uuid.UUID{b.analysisClaim().ClaimID, b.otherClaim().ClaimID},
		ExplanationMarkdown: "Both work cells affect the same exported symbol.",
		CreatedAt:           protocol.NewTimestamp(time.Date(2026, 11, 23, 9, 8, 0, 0, time.UTC)),
	}
}

func (b *workLoopBackend) conflictContext() protocol.ConflictContextPack {
	return protocol.ConflictContextPack{
		Collision: b.collision(), Objective: protocol.ConflictObjective{ObjectiveID: b.objective.ObjectiveID, Revision: 1, Title: b.objective.Title},
		Intents: []protocol.IntentRevision{*b.intent, b.otherIntent}, Claims: []protocol.SemanticClaim{b.analysisClaim(), b.otherClaim()},
		Uncertainties: []string{"Merge order is unresolved."}, UnresolvedQuestion: "Which work cell lands first?",
		AllowedResolutionKinds: []string{"sequencing"}, SafeHandlingNote: safeHandlingNote,
	}
}

func (b *workLoopBackend) events() []protocol.EventEnvelope {
	created := protocol.NewTimestamp(time.Date(2026, 11, 23, 9, 0, 0, 0, time.UTC))
	events := []protocol.EventEnvelope{
		b.event(1, b.project.ProjectID, created, protocol.ProjectCreated{}),
		b.event(2, b.objective.ObjectiveID, created, protocol.ObjectiveRevisionCreated{Revision: 1}),
		b.event(3, b.blueprintID, created, protocol.BlueprintRevisionActivated{Revision: 1}),
	}
	if b.intent != nil {
		events = append(events, b.event(4, b.intentID, b.intent.CreatedAt, protocol.IntentPublished{Revision: 1}))
	}
	if b.analyzed {
		events = append(events,
			b.event(5, b.analysisClaim().ClaimID, b.analysisClaim().ObservedAt, protocol.ClaimsObserved{IntentID: b.intentID, IntentRevision: 1}),
			b.event(6, b.collision().CollisionID, b.collision().CreatedAt, protocol.CollisionOpened{}))
	}
	if b.evidence {
		events = append(events, b.event(7, workflowUUIDNoTest("019a8f10-0018-7000-8000-000000000018"), created, protocol.EvidenceAttached{}))
	}
	if b.intent != nil && !b.active && b.asOf >= 8 {
		events = append(events, b.event(8, b.intentID, created, protocol.IntentReleased{Revision: 1}))
	}
	return events
}

func (b *workLoopBackend) event(sequence int64, entityID uuid.UUID, at protocol.Timestamp, payload protocol.EventPayload) protocol.EventEnvelope {
	return protocol.EventEnvelope{
		ProjectID: b.project.ProjectID, EventSequence: sequence,
		EventID: uuid.NewV7(), EntityID: entityID, ActorHumanID: b.human.HumanID, OccurredAt: at, Payload: payload,
	}
}

func (b *workLoopBackend) writeEvents(t *testing.T, w http.ResponseWriter, r *http.Request) {
	t.Helper()
	after, _ := strconv.ParseInt(r.URL.Query().Get("after"), 10, 64)
	limit := queryLimit(r, 100)
	raw := make([]json.RawMessage, 0, limit)
	for _, event := range b.events() {
		if event.EventSequence <= after || len(raw) == limit {
			continue
		}
		data, err := protocol.Encode(event)
		if err != nil {
			t.Fatal(err)
		}
		raw = append(raw, data)
	}
	data, _ := json.Marshal(raw)
	_, _ = fmt.Fprintf(w, `{"data":%s,"as_of_event":%d}`, data, b.asOf)
}

func queryLimit(r *http.Request, fallback int) int {
	if raw := r.URL.Query().Get("limit"); raw != "" {
		if value, err := strconv.Atoi(raw); err == nil {
			return value
		}
	}
	return fallback
}

type workLoopEnvelope[T any] struct {
	Data      T
	AsOfEvent int64
}

type workLoopRawEnvelope struct {
	Data      json.RawMessage `json:"data"`
	AsOfEvent int64           `json:"as_of_event"`
}

func callWorkLoopTool[T any](t *testing.T, ctx context.Context, session *sdk.ClientSession, name string, arguments map[string]any) T {
	t.Helper()
	result, err := session.CallTool(ctx, &sdk.CallToolParams{Name: name, Arguments: arguments})
	if err != nil || result == nil || result.IsError {
		t.Fatalf("%s: %v, %s", name, err, toolResultText(result))
	}
	var output T
	decodeStructured(t, result, &output)
	return output
}

func callWorkLoopEnvelope[T protocol.Document](t *testing.T, ctx context.Context, session *sdk.ClientSession, name string, arguments map[string]any) workLoopEnvelope[T] {
	t.Helper()
	raw := callWorkLoopEnvelopeRaw(t, ctx, session, name, arguments)
	data, err := protocol.Decode[T](raw.Data)
	if err != nil {
		t.Fatalf("decode %s: %v", name, err)
	}
	return workLoopEnvelope[T]{Data: data, AsOfEvent: raw.AsOfEvent}
}

func callWorkLoopEnvelopeRaw(t *testing.T, ctx context.Context, session *sdk.ClientSession, name string, arguments map[string]any) workLoopRawEnvelope {
	t.Helper()
	result, err := session.CallTool(ctx, &sdk.CallToolParams{Name: name, Arguments: arguments})
	if err != nil || result == nil || result.IsError {
		t.Fatalf("%s: %v, %s", name, err, toolResultText(result))
	}
	data, err := json.Marshal(result.StructuredContent)
	if err != nil {
		t.Fatal(err)
	}
	var envelope workLoopRawEnvelope
	if err := json.Unmarshal(data, &envelope); err != nil {
		t.Fatalf("decode %s envelope: %v", name, err)
	}
	return envelope
}

func writeWorkLoopDirect[T protocol.Document](t *testing.T, w http.ResponseWriter, document T) {
	t.Helper()
	data, err := protocol.Encode(document)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = fmt.Fprintf(w, `{"data":%s}`, data)
}

func workflowUUIDNoTest(raw string) uuid.UUID {
	id, err := uuid.Parse(raw)
	if err != nil {
		panic(err)
	}
	return id
}
