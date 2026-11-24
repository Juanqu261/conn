package tui

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
	"uuid"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Juanqu261/conn/internal/httpclient"
	"github.com/Juanqu261/conn/internal/protocol"
)

// recordedRequest is one request the fake Conn server received.
type recordedRequest struct {
	method, path, query string
	body                []byte
}

// fakeConn answers the app's writes with contract-valid documents built from
// the request, and records every request for assertions.
type fakeConn struct {
	mu       sync.Mutex
	requests []recordedRequest
}

func (f *fakeConn) last(t *testing.T) recordedRequest {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.requests) == 0 {
		t.Fatal("the app sent no request")
	}
	return f.requests[len(f.requests)-1]
}

func newFakeConn(t *testing.T, respond func(r *http.Request, body []byte) protocol.Document) (*fakeConn, *httpclient.Client) {
	t.Helper()
	fake := &fakeConn{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		fake.mu.Lock()
		fake.requests = append(fake.requests, recordedRequest{r.Method, r.URL.Path, r.URL.RawQuery, body})
		fake.mu.Unlock()
		doc := respond(r, body)
		data, err := protocol.Encode(doc)
		if err != nil {
			t.Errorf("fake response is not contract-valid: %v", err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"data":%s,"as_of_event":9}`, data)
	}))
	t.Cleanup(server.Close)
	client, err := httpclient.New(server.URL, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	return fake, client
}

func decodeBody[T protocol.Document](t *testing.T, body []byte) T {
	t.Helper()
	doc, err := protocol.Decode[T](body)
	if err != nil {
		t.Fatalf("request body %s: %v", body, err)
	}
	return doc
}

// semanticModel is a connected app on one project with one objective, one
// repository, and the given role.
func semanticModel(client *httpclient.Client, role protocol.Role) model {
	m := newModel(context.Background(), func() (*httpclient.Client, error) { return client, nil })
	human := protocol.Human{HumanID: uuid.NewV7(), GitHubLogin: "alice"}
	projectID := uuid.NewV7()
	m.loading, m.client, m.human = false, client, human
	m.selected = protocol.ProjectListEntry{Project: protocol.Project{ProjectID: projectID, Name: "Checkout"}, Role: role}
	m.projects.Projects = []protocol.ProjectListEntry{m.selected}
	m.status.Role = role
	m.members.Members = []protocol.ProjectMember{{Human: human, Membership: protocol.ProjectMembership{ProjectID: projectID, HumanID: human.HumanID, Role: role}}}
	m.radar.Objectives = []protocol.RadarObjective{{ObjectiveID: uuid.NewV7(), Revision: 1, Title: "Safe checkout retries"}}
	m.radar.Repositories = []protocol.RepositoryConnection{{RepositoryID: uuid.NewV7(), ProjectID: projectID, HostKind: "github", CanonicalRemote: "github.com/acme/payments", AnalysisEnabled: true}}
	// These tests read whole reviews; layout_test.go covers scrolling.
	m.width, m.height = 100, 80
	return m
}

func pressKeys(t *testing.T, m model, keys ...string) model {
	t.Helper()
	for _, key := range keys {
		var msg tea.KeyMsg
		switch key {
		case "enter":
			msg = tea.KeyMsg{Type: tea.KeyEnter}
		case "esc":
			msg = tea.KeyMsg{Type: tea.KeyEsc}
		case "down":
			msg = tea.KeyMsg{Type: tea.KeyDown}
		case "up":
			msg = tea.KeyMsg{Type: tea.KeyUp}
		case "ctrl+s":
			msg = tea.KeyMsg{Type: tea.KeyCtrlS}
		default:
			msg = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(key)}
		}
		updated, _ := m.Update(msg)
		m = updated.(model)
	}
	return m
}

// submit confirms the review on screen and runs the resulting write.
func submit(t *testing.T, m model) (model, actionDone) {
	t.Helper()
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatalf("Enter on screen %d started no write", m.screen)
	}
	done, ok := cmd().(actionDone)
	if !ok {
		t.Fatal("the write did not finish with actionDone")
	}
	return updated.(model), done
}

func TestWorkSectionShowsRepositoriesAndIntents(t *testing.T) {
	m := semanticModel(&httpclient.Client{}, protocol.RoleOwner)
	m.intents.Intents = []protocol.IntentView{{Status: "released", IntentRevision: protocol.IntentRevision{IntentID: uuid.NewV7(), Revision: 2, SummaryMarkdown: "Retry safely."}}}
	m = pressKeys(t, m, "4")
	if m.section != workSection {
		t.Fatalf("4 opened section %d, want Work", m.section)
	}
	view := m.work() + "\n" + m.normalFooter()
	for _, want := range []string{"github.com/acme/payments", "[released]", "Retry safely.", "n publish Intent", "g connect repository"} {
		if !strings.Contains(view, want) {
			t.Fatalf("Work view lacks %q:\n%s", want, view)
		}
	}
	if m = pressKeys(t, m, "5"); m.section != teamSection {
		t.Fatalf("5 opened section %d, want Team", m.section)
	}
}

func TestPublishIntentSendsCharterRepositoryAssumptionsAndClaims(t *testing.T) {
	fake, client := newFakeConn(t, func(r *http.Request, body []byte) protocol.Document {
		input, _ := protocol.Decode[protocol.IntentInput](body)
		return protocol.IntentView{Status: "active", AdvisoryObservations: []string{"Accepted Charter is associated."}, IntentRevision: protocol.IntentRevision{
			IntentID: input.IntentID, ProjectID: uuid.NewV7(), ParticipantID: uuid.NewV7(), Revision: 1, ObjectiveID: input.ObjectiveID, ObjectiveRevision: input.ObjectiveRevision,
			BlueprintID: input.BlueprintID, BlueprintRevision: input.BlueprintRevision, CharterID: input.CharterID, RepositoryID: input.RepositoryID,
			SummaryMarkdown: input.SummaryMarkdown, Assumptions: input.Assumptions, DeclaredClaims: input.DeclaredClaims, CreatedAt: protocol.NewTimestamp(time.Now()),
		}}
	})
	m := semanticModel(client, protocol.RoleMember)
	objective, repository := m.radar.Objectives[0], m.radar.Repositories[0]
	blueprintID, cellID := uuid.NewV7(), uuid.NewV7()
	m.status.Blueprints = []protocol.BlueprintSummary{{BlueprintID: blueprintID, Revisions: []protocol.RevisionSummary{{
		Revision: 1, Status: protocol.BlueprintActive, ObjectiveID: objective.ObjectiveID, ObjectiveRevision: 1,
		Cells: []protocol.CellSummary{{CellID: cellID, AccountableHumanID: m.human.HumanID, Title: "Gateway retries", Decision: protocol.CellAccepted}},
	}}}}
	m.section = workSection

	m = pressKeys(t, m, "n", "enter") // the only objective
	if m.screen != intentCharterScreen {
		t.Fatalf("after the objective the app is on screen %d, want the charter selector", m.screen)
	}
	m = pressKeys(t, m, "down", "enter", "down", "enter") // the accepted charter, then the repository
	if m.screen != intentSummaryScreen {
		t.Fatalf("after the repository the app is on screen %d, want the summary", m.screen)
	}
	m.area.SetValue("Retry gateway charges with the order idempotency key.")
	m = pressKeys(t, m, "ctrl+s")
	m.area.SetValue("Gateway retries stay below three attempts.\n\nOrders reuse the gateway key.")
	m = pressKeys(t, m, "ctrl+s")
	m.area.SetValue("invariant changes")
	if m = pressKeys(t, m, "ctrl+s"); m.screen != intentClaimsScreen || m.err == nil {
		t.Fatal("an incomplete claim line did not keep the claims form open with an error")
	}
	m.area.SetValue("invariant changes invariant:checkout:one-idempotency-key-one-successful-charge One key, one charge")
	if m = pressKeys(t, m, "ctrl+s"); m.screen != intentConfirmScreen || !strings.Contains(m.intentReview(), "One key, one charge") {
		t.Fatalf("claims did not open the review: screen %d\n%s", m.screen, m.View())
	}

	m, done := submit(t, m)
	if done.err != nil {
		t.Fatalf("publish failed: %v", done.err)
	}
	request := fake.last(t)
	if request.method != http.MethodPost || !strings.HasSuffix(request.path, "/intents") || strings.Contains(string(request.body), "human_id") {
		t.Fatalf("publish request = %s %s %s", request.method, request.path, request.body)
	}
	sent := decodeBody[protocol.IntentInput](t, request.body)
	if sent.CharterID == nil || *sent.CharterID != cellID || *sent.BlueprintID != blueprintID || sent.RepositoryID == nil || *sent.RepositoryID != repository.RepositoryID ||
		len(sent.Assumptions) != 2 || len(sent.DeclaredClaims) != 1 || sent.DeclaredClaims[0].DisplayName != "One key, one charge" || sent.DeclaredClaims[0].ProvenanceKind != "declared" {
		t.Fatalf("published Intent = %#v", sent)
	}
}

func TestReviseReleaseAndEvidenceUseTheSelectedIntent(t *testing.T) {
	fake, client := newFakeConn(t, func(r *http.Request, body []byte) protocol.Document {
		if strings.HasSuffix(r.URL.Path, "/evidence") {
			input, _ := protocol.Decode[protocol.EvidenceAttachmentInput](body)
			return protocol.EvidenceReference{EvidenceID: uuid.NewV7(), ProjectID: uuid.NewV7(), IntentID: &input.IntentID, ObjectiveID: input.ObjectiveID, ObjectiveRevision: input.ObjectiveRevision,
				AcceptanceKey: input.AcceptanceKey, Kind: input.Kind, State: input.State, Locator: input.Locator, ObservedAt: protocol.NewTimestamp(time.Now())}
		}
		return protocol.IntentView{Status: "active", AdvisoryObservations: []string{"Unpartitioned."}, IntentRevision: protocol.IntentRevision{
			IntentID: uuid.NewV7(), ProjectID: uuid.NewV7(), ParticipantID: uuid.NewV7(), Revision: 3, ObjectiveID: uuid.NewV7(), ObjectiveRevision: 1,
			SummaryMarkdown: "Revised.", CreatedAt: protocol.NewTimestamp(time.Now())}}
	})
	m := semanticModel(client, protocol.RoleMember)
	objective := m.radar.Objectives[0]
	evidenceRef := protocol.ClaimEvidenceRef{Kind: "decision", Locator: "note:retry"}
	active := protocol.IntentRevision{IntentID: uuid.NewV7(), Revision: 2, ObjectiveID: objective.ObjectiveID, ObjectiveRevision: 1, SummaryMarkdown: "Retry safely.",
		DeclaredClaims: []protocol.SemanticClaimCandidate{{SubjectKind: "invariant", Relation: "changes", NormalizedSubject: "invariant:checkout:one-charge", DisplayName: "One charge", ProvenanceKind: "declared", EvidenceRefs: []protocol.ClaimEvidenceRef{evidenceRef}}}}
	m.intents.Intents = []protocol.IntentView{{Status: "active", IntentRevision: active}}
	m.section = workSection
	m = pressKeys(t, m, "i")
	if m.screen != intentListScreen || !strings.Contains(m.View(), "e revise") {
		t.Fatalf("i did not open the Intent drill-down with actions:\n%s", m.View())
	}

	// Revise keeps unchanged claims, including evidence references the form does not edit.
	m = pressKeys(t, m, "e", "enter", "enter", "ctrl+s", "ctrl+s", "ctrl+s") // objective, no repository, unchanged fields
	if m.screen != intentConfirmScreen || !strings.Contains(m.View(), "appends revision 3 after revision 2") {
		t.Fatalf("revise did not reach its review: screen %d\n%s", m.screen, m.View())
	}
	_, done := submit(t, m)
	request := fake.last(t)
	if done.err != nil || request.query != "expected_revision=2" || !strings.Contains(request.path, active.IntentID.String()+"/revisions") {
		t.Fatalf("revise request = %+v, err %v", request, done.err)
	}
	if sent := decodeBody[protocol.IntentInput](t, request.body); len(sent.DeclaredClaims) != 1 || len(sent.DeclaredClaims[0].EvidenceRefs) != 1 {
		t.Fatalf("revise dropped the claim evidence: %#v", sent.DeclaredClaims)
	}

	m.screen = intentListScreen
	m = pressKeys(t, m, "x")
	if m.screen != intentReleaseScreen {
		t.Fatalf("x opened screen %d, want release review", m.screen)
	}
	_, done = submit(t, m)
	if request := fake.last(t); done.err != nil || !strings.HasSuffix(request.path, active.IntentID.String()+"/release") {
		t.Fatalf("release request = %+v, err %v", request, done.err)
	}

	m.screen = intentListScreen
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("v")})
	if cmd == nil {
		t.Fatal("v did not load the Objective's acceptance criteria")
	}
	updated, _ = updated.(model).Update(evidenceKeysLoaded{keys: []protocol.KeyPoint{{Key: "retry-safe", Kind: protocol.KeyPointAcceptanceCriterion, Statement: "No double charge."}}})
	m = pressKeys(t, updated.(model), "down", "enter", "down", "enter", "enter") // retry-safe, pull_request, passing
	m.input.SetValue("github.com/acme/payments/pull/7")
	m = pressKeys(t, m, "enter", "enter")
	if m.screen != evidenceConfirmScreen || !strings.Contains(m.View(), "Acceptance key: retry-safe") {
		t.Fatalf("evidence did not reach its review: screen %d\n%s", m.screen, m.View())
	}
	_, done = submit(t, m)
	if done.err != nil {
		t.Fatalf("attach evidence failed: %v", done.err)
	}
	sent := decodeBody[protocol.EvidenceAttachmentInput](t, fake.last(t).body)
	if sent.IntentID != active.IntentID || sent.AcceptanceKey != "retry-safe" || sent.Kind != "pull_request" || sent.State != "passing" {
		t.Fatalf("evidence = %#v", sent)
	}
}

func TestDecisionFlowEnforcesOwnerAuthorityAndAdoptsAProposal(t *testing.T) {
	fake, client := newFakeConn(t, func(r *http.Request, body []byte) protocol.Document {
		input, _ := protocol.Decode[protocol.ResolutionDecisionInput](body)
		return protocol.ResolutionDecision{DecisionID: uuid.NewV7(), ProjectID: uuid.NewV7(), CollisionID: uuid.NewV7(), SelectedProposalID: input.SelectedProposalID,
			DecidedByHumanID: uuid.NewV7(), Kind: input.Kind, Binding: input.Binding, RationaleMarkdown: input.RationaleMarkdown, References: input.References, CreatedAt: protocol.NewTimestamp(time.Now())}
	})
	collision := protocol.CollisionAssessment{CollisionID: uuid.NewV7(), RuleID: "shared_declared_invariant", Severity: "amber", State: "open", Confidence: 1,
		LeftIntentID: uuid.NewV7(), LeftIntentRevision: 1, RightIntentID: uuid.NewV7(), RightIntentRevision: 1, ExplanationMarkdown: "Shared invariant."}
	participant := uuid.NewV7()
	proposal := protocol.ResolutionProposal{ProposalID: uuid.NewV7(), CollisionID: collision.CollisionID, ProposedByParticipantID: &participant, Kind: "sequencing", RationaleMarkdown: "Land the gateway change first."}
	withCollision := func(role protocol.Role) model {
		m := semanticModel(client, role)
		m.radar.OpenCollisions = []protocol.CollisionAssessment{collision}
		m.screen = radarCollisionScreen
		m.semantic.proposalsFor, m.semantic.proposals = collision.CollisionID, []protocol.ResolutionProposal{proposal}
		return m
	}

	member := withCollision(protocol.RoleMember)
	if !strings.Contains(member.View(), "Land the gateway change first.") {
		t.Fatalf("drill-down does not list the proposal:\n%s", member.View())
	}
	member = pressKeys(t, member, "d", "enter", "down", "down", "down", "enter") // no proposal, accepted_risk
	if member.screen != decisionKindScreen || member.err == nil || !strings.Contains(member.err.Error(), "owner") {
		t.Fatalf("a member chose an owner-only kind: screen %d err %v", member.screen, member.err)
	}
	member = pressKeys(t, member, "down", "enter", "down", "enter") // false_positive, binding
	if member.screen != decisionBindingScreen || member.err == nil {
		t.Fatalf("a member chose a binding decision: screen %d err %v", member.screen, member.err)
	}

	owner := withCollision(protocol.RoleOwner)
	owner = pressKeys(t, owner, "d", "down", "enter") // adopt the proposal
	if owner.screen != decisionBindingScreen || owner.semantic.decision.Kind != "sequencing" {
		t.Fatalf("adopting the proposal led to screen %d kind %q", owner.screen, owner.semantic.decision.Kind)
	}
	owner = pressKeys(t, owner, "down", "enter")
	owner.area.SetValue("The team agreed in the retry sync.")
	owner = pressKeys(t, owner, "ctrl+s")
	owner.input.SetValue("meeting:retry-sync, note:checkout")
	owner = pressKeys(t, owner, "enter")
	if view := owner.View(); owner.screen != decisionConfirmScreen || !strings.Contains(view, "sequencing proposal by a participant: Land the gateway change first.") ||
		strings.Contains(view, proposal.ProposalID.String()) {
		t.Fatalf("decision review must name the selected proposal, not its ID:\n%s", view)
	}
	_, done := submit(t, owner)
	request := fake.last(t)
	sent := decodeBody[protocol.ResolutionDecisionInput](t, request.body)
	if done.err != nil || !strings.HasSuffix(request.path, collision.CollisionID.String()+"/decisions") || sent.SelectedProposalID == nil ||
		*sent.SelectedProposalID != proposal.ProposalID || !sent.Binding || sent.Kind != "sequencing" || len(sent.References) != 2 {
		t.Fatalf("decision request = %s %s, err %v", request.path, request.body, done.err)
	}
}

func TestProposalFlowRecordsANonBindingProposal(t *testing.T) {
	fake, client := newFakeConn(t, func(r *http.Request, body []byte) protocol.Document {
		input, _ := protocol.Decode[protocol.ResolutionProposalInput](body)
		participant := uuid.NewV7()
		return protocol.ResolutionProposal{ProposalID: uuid.NewV7(), ProjectID: uuid.NewV7(), CollisionID: uuid.NewV7(), ProposedByParticipantID: &participant,
			Kind: input.Kind, RationaleMarkdown: input.RationaleMarkdown, CreatedAt: protocol.NewTimestamp(time.Now())}
	})
	m := semanticModel(client, protocol.RoleMember)
	collision := protocol.CollisionAssessment{CollisionID: uuid.NewV7(), RuleID: "shared_exported_symbol", Severity: "amber", State: "open", Confidence: 1}
	m.radar.OpenCollisions, m.screen = []protocol.CollisionAssessment{collision}, radarCollisionScreen
	m = pressKeys(t, m, "p", "down", "enter") // sequencing
	m.area.SetValue("Land orders after gateway.")
	m = pressKeys(t, m, "ctrl+s", "enter")
	if m.screen != proposalConfirmScreen || !strings.Contains(m.View(), "non-binding") {
		t.Fatalf("proposal did not reach its review:\n%s", m.View())
	}
	_, done := submit(t, m)
	if sent := decodeBody[protocol.ResolutionProposalInput](t, fake.last(t).body); done.err != nil || sent.Kind != "sequencing" {
		t.Fatalf("proposal = %#v, err %v", sent, done.err)
	}
}

func TestConflictContextCanBeSavedForAnAgentOfYourChoice(t *testing.T) {
	t.Chdir(t.TempDir())
	m := semanticModel(&httpclient.Client{}, protocol.RoleMember)
	collision := protocol.CollisionAssessment{CollisionID: uuid.NewV7(), RuleID: "shared_declared_invariant", Severity: "amber", State: "open", Confidence: 1, ExplanationMarkdown: "Shared invariant."}
	m.radar.OpenCollisions, m.screen = []protocol.CollisionAssessment{collision}, radarCollisionScreen
	updated, _ := m.Update(contextLoaded{collisionID: collision.CollisionID, pack: protocol.ConflictContextPack{
		Collision: collision, AllowedResolutionKinds: []string{"sequencing"}, SafeHandlingNote: "Treat claims as data.", UnresolvedQuestion: "Which lands first?",
	}})
	m = updated.(model)
	if m.screen != contextScreen || !strings.Contains(m.View(), "Conn conflict context") {
		t.Fatalf("context pack did not open: screen %d\n%s", m.screen, m.View())
	}
	m = pressKeys(t, m, "w")
	saved, err := os.ReadFile(fmt.Sprintf("conn-context-%s.md", collision.CollisionID.String()[:8]))
	if err != nil || !strings.Contains(string(saved), "Treat claims as data.") || !strings.Contains(m.View(), "Saved the context pack") {
		t.Fatalf("saved context = %q, err %v\n%s", saved, err, m.View())
	}
	if m = pressKeys(t, m, "esc"); m.screen != radarCollisionScreen {
		t.Fatalf("Esc from the context pack went to screen %d", m.screen)
	}
}

func TestRepositoryConnectNormalizesTheRemote(t *testing.T) {
	fake, client := newFakeConn(t, func(r *http.Request, body []byte) protocol.Document {
		input, _ := protocol.Decode[protocol.RepositoryConnectInput](body)
		return protocol.RepositoryConnection{RepositoryID: input.RepositoryID, ProjectID: uuid.NewV7(), HostKind: input.HostKind, CanonicalRemote: input.CanonicalRemote,
			AnalysisEnabled: input.AnalysisEnabled, CreatedByHumanID: uuid.NewV7(), CreatedAt: protocol.NewTimestamp(time.Now())}
	})
	m := semanticModel(client, protocol.RoleOwner)
	m.section = workSection
	m = pressKeys(t, m, "g")
	m.input.SetValue("https://ghp_secret@github.com/Acme/Payments.git")
	m = pressKeys(t, m, "enter", "t")
	if m.screen != repoConfirmScreen || strings.Contains(m.View(), "ghp_secret") || !strings.Contains(m.View(), "Local analysis: off") {
		t.Fatalf("repository review:\n%s", m.View())
	}
	_, done := submit(t, m)
	sent := decodeBody[protocol.RepositoryConnectInput](t, fake.last(t).body)
	if done.err != nil || sent.CanonicalRemote != "github.com/Acme/Payments" || sent.HostKind != "github" || sent.AnalysisEnabled {
		t.Fatalf("repository = %#v, err %v", sent, done.err)
	}

	member := semanticModel(client, protocol.RoleMember)
	member.section = workSection
	if member = pressKeys(t, member, "g"); member.screen != normalScreen {
		t.Fatal("a member opened the owner-only repository form")
	}
}

func TestKiroSpecImportHashesLocallyAndSendsCriteria(t *testing.T) {
	t.Chdir(t.TempDir())
	spec := []byte("# Checkout retries\n\nPRIVATE_SPEC_BODY_MUST_NOT_UPLOAD\n")
	if err := os.MkdirAll(filepath.Join(".kiro", "specs", "checkout"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(".kiro", "specs", "checkout", "requirements.md"), spec, 0o600); err != nil {
		t.Fatal(err)
	}
	fake, client := newFakeConn(t, func(r *http.Request, body []byte) protocol.Document {
		input, _ := protocol.Decode[protocol.ObjectiveImportInput](body)
		now := protocol.NewTimestamp(time.Now())
		return protocol.Objective{ObjectiveID: input.ObjectiveID, ProjectID: uuid.NewV7(), Revision: 1, Title: input.Title, OverviewMarkdown: input.OverviewMarkdown,
			AccountableHumanID: input.AccountableHumanID, KeyPoints: input.KeyPoints, CreatedAt: now,
			ImportProvenance: &protocol.ObjectiveImportProvenance{SourceKind: input.SourceKind, SourceRef: input.SourceRef, ContentHash: input.ContentHash, ImportedAt: now}}
	})
	m := semanticModel(client, protocol.RoleOwner)
	m.section = planSection
	m = pressKeys(t, m, "k")
	m.input.SetValue(".kiro/specs/checkout/requirements.md")
	m = pressKeys(t, m, "enter", "enter") // hash locally, then create a new Objective
	if m.screen != importTitleScreen || m.input.Value() != "Checkout retries" {
		t.Fatalf("title step: screen %d value %q err %v", m.screen, m.input.Value(), m.err)
	}
	m = pressKeys(t, m, "enter")
	m.area.SetValue("Retries never double charge.")
	m = pressKeys(t, m, "ctrl+s")
	m.area.SetValue("Retry-Safe: bad key")
	if m = pressKeys(t, m, "ctrl+s"); m.screen != importCriteriaScreen || m.err == nil {
		t.Fatal("an invalid criterion key was accepted")
	}
	m.area.SetValue("retry-safe: A retried charge never charges twice.")
	m = pressKeys(t, m, "ctrl+s", "enter")
	if m.screen != importConfirmScreen || !strings.Contains(m.View(), "retry-safe (acceptance_criterion)") {
		t.Fatalf("import did not reach its review:\n%s", m.View())
	}
	_, done := submit(t, m)
	request := fake.last(t)
	sent := decodeBody[protocol.ObjectiveImportInput](t, request.body)
	if done.err != nil || request.query != "expected_revision=0" || sent.SourceRef != ".kiro/specs/checkout/requirements.md" ||
		sent.ContentHash != fmt.Sprintf("%x", sha256.Sum256(spec)) || len(sent.KeyPoints) != 1 || strings.Contains(string(request.body), "PRIVATE_SPEC_BODY") {
		t.Fatalf("import request = %s %s, err %v", request.query, request.body, done.err)
	}
}

func TestClaimParsingRejectsUnknownVocabulary(t *testing.T) {
	for _, line := range []string{"invariant edits invariant:checkout:x", "widget changes invariant:checkout:x", "invariant changes Not-Namespaced"} {
		if _, err := parseClaims(line, nil); err == nil {
			t.Fatalf("claim line %q was accepted", line)
		}
	}
	claims, err := parseClaims("\ncode_symbol affects symbol:typescript:src/a.ts/charge\n", nil)
	if err != nil || len(claims) != 1 || claims[0].DisplayName != "symbol:typescript:src/a.ts/charge" {
		t.Fatalf("claims = %#v, %v", claims, err)
	}
	if encoded, _ := json.Marshal(claims); strings.Contains(string(encoded), "human_id") {
		t.Fatal("claim carried an authority field")
	}
}
