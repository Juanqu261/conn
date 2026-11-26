package hooks_test

import (
	"bytes"
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
	"sync"
	"testing"
	"time"

	"uuid"

	"github.com/Juanqu261/conn/internal/cli"
	"github.com/Juanqu261/conn/internal/hooks"
	"github.com/Juanqu261/conn/internal/hookstate"
	"github.com/Juanqu261/conn/internal/protocol"
)

const testToken = "conn_sess_AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"

// TestHooksWarnWithoutBlockingAndFailOpen protects requirements 9 through
// 12: the three pre-write warnings, one-time prompt-submit reporting, the
// written-path record, fail-open behavior on backend or credential failure,
// bounded labelled output, and that no hook exits 2, prints a permission
// decision, or writes to the backend.
func TestHooksWarnWithoutBlockingAndFailOpen(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is required for hook worktree resolution")
	}
	backend := newFakeBackend(t)
	worktree := gitWorktree(t, "https://github.com/acme/shop.git")
	store, err := hookstate.Open(context.Background(), worktree)
	if err != nil {
		t.Fatal(err)
	}
	env := map[string]string{"CONN_TOKEN": testToken, "CONN_SERVER_URL": backend.server.URL}
	run := func(name string, event map[string]any) (int, string, string) {
		t.Helper()
		event["cwd"] = worktree
		data, _ := json.Marshal(event)
		var stdout, stderr bytes.Buffer
		code := hooks.Run(context.Background(), name, hooks.Config{
			In: bytes.NewReader(data), Out: &stdout, Err: &stderr, WarnExitCode: hooks.DefaultWarnExitCode,
			LookupEnv: func(key string) (string, bool) { value, ok := env[key]; return value, ok },
		})
		for _, output := range []string{stdout.String(), stderr.String()} {
			if code == 2 || strings.Contains(output, "permissionDecision") || len(output) > hooks.MaxOutputChars {
				t.Fatalf("%s returned %d with output %q", name, code, output)
			}
			if output != "" && !strings.HasPrefix(output, hooks.Label) {
				t.Fatalf("%s output is not labelled as Project data: %q", name, output)
			}
		}
		return code, stdout.String(), stderr.String()
	}
	write := func(path string) map[string]any {
		return map[string]any{"session_id": "kiro-session-1", "tool_name": "fs_write", "tool_input": map[string]any{"path": path, "text": "SECRET_FILE_CONTENT"}}
	}

	// Without a focus record, a connected repository recommends /conn-start.
	if code, stdout, _ := run(hooks.SessionStart, map[string]any{"session_id": "kiro-session-1"}); code != 0 || !strings.Contains(stdout, "/conn-start") {
		t.Fatalf("session-start without focus = %d %q", code, stdout)
	}
	code, _, stderr := run(hooks.PreWrite, write("src/orders/checkout.ts"))
	if code != hooks.DefaultWarnExitCode || !strings.Contains(stderr, "No Conn Intent is published") || !strings.Contains(stderr, "the write continues") {
		t.Fatalf("pre-write without focus = %d %q", code, stderr)
	}
	_, stdout, _ := run(hooks.PromptSubmit, map[string]any{"session_id": "kiro-session-1"})
	if strings.Count(stdout, "No Conn Intent is published") != 2 || !strings.Contains(stdout, "Before a write:") {
		t.Fatalf("first prompt-submit without focus = %q, want the queued warning and one recommendation", stdout)
	}
	if _, stdout, _ = run(hooks.PromptSubmit, map[string]any{"session_id": "kiro-session-1"}); stdout != "" {
		t.Fatalf("second prompt-submit in the same session = %q, want nothing", stdout)
	}

	// The MCP adapter records the focus after publication.
	if err := store.UpdateFocus(func(focus *hookstate.Focus) {
		focus.ServerURL, focus.ProjectID, focus.RepositoryID = backend.server.URL, &backend.projectID, &backend.repositoryID
		focus.IntentID, focus.IntentRevision, focus.ParticipantID, focus.LastEvent = &backend.mine, 1, &backend.myParticipant, 10
	}); err != nil {
		t.Fatal(err)
	}
	if code, stdout, _ := run(hooks.SessionStart, map[string]any{}); code != 0 || !strings.Contains(stdout, "1 collision(s) need attention") || !strings.Contains(stdout, "/conn-sync 10") {
		t.Fatalf("session-start with focus = %d %q", code, stdout)
	}

	// Another participant's changes claim and an open collision both warn.
	code, _, stderr = run(hooks.PreWrite, write("src/payments/retry.ts"))
	if code != hooks.DefaultWarnExitCode || !strings.Contains(stderr, short(backend.other)+"@2 changes contract:typescript:src/payments/retry.ts/shouldRetryCharge") ||
		!strings.Contains(stderr, "Collision "+short(backend.collisionID)+" (contract_producer_consumer)") || strings.Contains(stderr, "No Conn Intent") {
		t.Fatalf("pre-write on a claimed path = %d %q", code, stderr)
	}
	if code, _, stderr = run(hooks.PreWrite, write("docs/notes.md")); code != 0 || stderr != "" {
		t.Fatalf("pre-write on an unclaimed path = %d %q", code, stderr)
	}
	if code, _, stderr = run(hooks.PreWrite, write(filepath.Join(t.TempDir(), "outside.ts"))); code != 0 || stderr != "" {
		t.Fatalf("pre-write outside the worktree = %d %q", code, stderr)
	}

	// prompt-submit reports the new collision once, then its state change.
	_, stdout, _ = run(hooks.PromptSubmit, map[string]any{"session_id": "kiro-session-2"})
	if !strings.Contains(stdout, "new open collision "+short(backend.collisionID)+" (contract_producer_consumer) on contract:typescript:src/payments/retry.ts/shouldRetryCharge with Intent "+short(backend.other)+"@2") ||
		!strings.Contains(stdout, "Counterpart Intent "+short(backend.other)+" is now at revision 2") || !strings.Contains(stdout, "Before a write:") ||
		!strings.Contains(stdout, "/conn-sync") || strings.Contains(stdout, "Retry contract prose") {
		t.Fatalf("prompt-submit with a new collision = %q", stdout)
	}
	if _, stdout, _ = run(hooks.PromptSubmit, map[string]any{"session_id": "kiro-session-2"}); stdout != "" {
		t.Fatalf("repeated prompt-submit = %q, want nothing", stdout)
	}
	if focus := store.Focus(); focus.LastEvent != backend.asOf || focus.KnownCollisions[backend.collisionID.String()] != "open" {
		t.Fatalf("stored focus = %+v", focus)
	}
	backend.setState(protocol.CollisionDismissed)
	if _, stdout, _ = run(hooks.PromptSubmit, map[string]any{"session_id": "kiro-session-2"}); !strings.Contains(stdout, "is now dismissed (false_positive)") {
		t.Fatalf("prompt-submit after dismissal = %q", stdout)
	}

	// post-write records the path and never the content.
	if code, stdout, stderr := run(hooks.PostWrite, write("src/orders/checkout.ts")); code != 0 || stdout != "" || stderr != "" {
		t.Fatalf("post-write = %d %q %q", code, stdout, stderr)
	}
	written := store.WrittenPaths()
	if len(written) != 1 || written[0].Path != "src/orders/checkout.ts" || written[0].SessionID != "kiro-session-1" {
		t.Fatalf("written paths = %+v", written)
	}
	if state := readState(t, store.Dir); strings.Contains(state, "SECRET_FILE_CONTENT") || strings.Contains(state, testToken) || strings.Contains(state, "Retry contract prose") {
		t.Fatal("hook state stored content, a credential, or Project prose")
	}

	// Backend, credential, and input failures allow the event silently.
	before := len(store.Diagnostics())
	backend.fail = true
	if code, stdout, stderr := run(hooks.PromptSubmit, map[string]any{}); code != 0 || stdout != "" || stderr != "" {
		t.Fatalf("prompt-submit with a failing backend = %d %q %q", code, stdout, stderr)
	}
	backend.fail = false
	delete(env, "CONN_TOKEN")
	if err := os.Remove(filepath.Join(store.Dir, hookstate.IndexFile)); err != nil {
		t.Fatal(err)
	}
	if code, _, stderr := run(hooks.PreWrite, write("src/payments/retry.ts")); code != 0 || stderr != "" {
		t.Fatalf("pre-write without a credential = %d %q", code, stderr)
	}
	if diagnostics := store.Diagnostics(); len(diagnostics) != before+2 || diagnostics[len(diagnostics)-1].Code != "not_authenticated" {
		t.Fatalf("diagnostics = %+v", diagnostics)
	}
	env["CONN_TOKEN"] = testToken

	// The CLI never turns a hook failure into exit code 2.
	for _, args := range [][]string{{"hook", "pre-write", "--unknown-flag"}, {"hook", "no-such-hook"}, {"hook", "pre-write", "--warn-exit-code", "2"}} {
		var stdout, stderr bytes.Buffer
		event, _ := json.Marshal(map[string]any{"cwd": worktree, "tool_input": map[string]any{"path": "src/payments/retry.ts"}})
		code := cli.Execute(context.Background(), args, cli.Streams{In: bytes.NewReader(event), Out: &stdout, Err: &stderr}, cli.Dependencies{
			LookupEnv: func(key string) (string, bool) { value, ok := env[key]; return value, ok },
		})
		if code == 2 || code > 1 || strings.Contains(stdout.String()+stderr.String(), "permissionDecision") {
			t.Fatalf("conn %v = %d", args, code)
		}
	}

	// Output stays bounded with many warnings.
	backend.manyClaims = 80
	if err := os.Remove(filepath.Join(store.Dir, hookstate.IndexFile)); err != nil {
		t.Fatal(err)
	}
	if _, _, stderr = run(hooks.PreWrite, write("src/payments/retry.ts")); !strings.Contains(stderr, "more item(s) omitted") {
		t.Fatalf("bounded pre-write output = %q", stderr)
	}
	if backend.mutations() != 0 {
		t.Fatalf("hooks sent %d non-GET requests", backend.mutations())
	}
}

type fakeBackend struct {
	server                                  *httptest.Server
	projectID, repositoryID, objectiveID    uuid.UUID
	mine, other, myParticipant, otherPerson uuid.UUID
	collisionID, theirClaim, myClaim        uuid.UUID
	human                                   protocol.Human
	asOf                                    int64
	mu                                      sync.Mutex
	state                                   string
	fail                                    bool
	manyClaims                              int
	nonGet                                  int
}

func newFakeBackend(t *testing.T) *fakeBackend {
	t.Helper()
	b := &fakeBackend{
		projectID: uuid.NewV7(), repositoryID: uuid.NewV7(), objectiveID: uuid.NewV7(),
		mine: uuid.NewV7(), other: uuid.NewV7(), myParticipant: uuid.NewV7(), otherPerson: uuid.NewV7(),
		collisionID: uuid.NewV7(), theirClaim: uuid.NewV7(), myClaim: uuid.NewV7(), asOf: 14, state: protocol.CollisionOpen,
	}
	b.human = protocol.Human{HumanID: uuid.NewV7(), GitHubUserID: 7, GitHubLogin: "hook-tester"}
	b.server = httptest.NewServer(http.HandlerFunc(b.handle(t)))
	t.Cleanup(b.server.Close)
	return b
}

func (b *fakeBackend) setState(state string) { b.mu.Lock(); b.state = state; b.mu.Unlock() }

func (b *fakeBackend) mutations() int { b.mu.Lock(); defer b.mu.Unlock(); return b.nonGet }

func (b *fakeBackend) handle(t *testing.T) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		b.mu.Lock()
		defer b.mu.Unlock()
		if r.Method != http.MethodGet {
			b.nonGet++
		}
		if b.fail || r.Header.Get("Authorization") != "Bearer "+testToken {
			http.Error(w, `{"error":{"code":"internal","message":"unavailable"}}`, http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		project := "/v1/projects/" + b.projectID.String()
		switch r.URL.Path {
		case "/v1/projects":
			b.write(t, w, protocol.ProjectList{Projects: []protocol.ProjectListEntry{{Project: b.project(), Role: protocol.RoleMember}}}, false)
		case project + "/repositories":
			b.write(t, w, protocol.RepositoryConnectionList{ProjectID: b.projectID, Repositories: []protocol.RepositoryConnection{b.repository()}}, true)
		case project + "/work-context":
			after, _ := strconv.ParseInt(r.URL.Query().Get("after"), 10, 64)
			b.write(t, w, b.workContext(after), true)
		case project + "/radar":
			b.write(t, w, b.radar(), true)
		default:
			http.NotFound(w, r)
		}
	}
}

func (b *fakeBackend) write(t *testing.T, w http.ResponseWriter, document protocol.Document, cursor bool) {
	data, err := protocol.Encode(document)
	if err != nil {
		t.Errorf("encode %T: %v", document, err)
		return
	}
	if cursor {
		_, _ = fmt.Fprintf(w, `{"data":%s,"as_of_event":%d}`, data, b.asOf)
		return
	}
	_, _ = fmt.Fprintf(w, `{"data":%s}`, data)
}

func (b *fakeBackend) created() protocol.Timestamp {
	return protocol.NewTimestamp(time.Date(2026, 12, 1, 9, 0, 0, 0, time.UTC))
}

func (b *fakeBackend) project() protocol.Project {
	return protocol.Project{ProjectID: b.projectID, Name: "Shop", OwnerHumanID: b.human.HumanID, CreatedAt: b.created()}
}

func (b *fakeBackend) repository() protocol.RepositoryConnection {
	return protocol.RepositoryConnection{RepositoryID: b.repositoryID, ProjectID: b.projectID, HostKind: "github", CanonicalRemote: "github.com/acme/shop", AnalysisEnabled: true, CreatedByHumanID: b.human.HumanID, CreatedAt: b.created()}
}

func (b *fakeBackend) intent(id, participant uuid.UUID, revision int) protocol.IntentRevision {
	return protocol.IntentRevision{IntentID: id, ProjectID: b.projectID, ParticipantID: participant, Revision: revision, ObjectiveID: b.objectiveID, ObjectiveRevision: 1, SummaryMarkdown: "Retry contract prose that hooks must never print.", CreatedAt: b.created()}
}

func (b *fakeBackend) collision() protocol.CollisionAssessment {
	return protocol.CollisionAssessment{
		CollisionID: b.collisionID, ProjectID: b.projectID, LeftIntentID: b.mine, LeftIntentRevision: 1, RightIntentID: b.other, RightIntentRevision: 2,
		RuleID: "contract_producer_consumer", Severity: "amber", Confidence: 1, State: b.state,
		MatchingClaimRefs: []uuid.UUID{b.myClaim, b.theirClaim}, MatchedSubjects: []string{"contract:typescript:src/payments/retry.ts/shouldRetryCharge"},
		ExplanationMarkdown: "Retry contract prose that hooks must never print.", CreatedAt: b.created(),
	}
}

func (b *fakeBackend) workContext(after int64) protocol.WorkContext {
	participant := protocol.Participant{ParticipantID: b.myParticipant, ProjectID: b.projectID, HumanID: b.human.HumanID, AdapterKind: "kiro", CreatedAt: b.created()}
	updates := []protocol.EventEnvelope{}
	if after < 12 {
		updates = append(updates, protocol.EventEnvelope{ProjectID: b.projectID, EventSequence: 12, EventID: uuid.NewV7(), EntityID: b.other, ActorHumanID: b.human.HumanID, OccurredAt: b.created(), Payload: protocol.IntentRevised{Revision: 2}})
	}
	return protocol.WorkContext{
		Project: protocol.WorkContextProject{ProjectID: b.projectID, Name: "Shop", Role: protocol.RoleMember},
		Reader:  protocol.WorkContextReader{Human: b.human, Participant: &participant}, Selection: protocol.WorkContextSelection{Source: protocol.WorkContextSelectionExplicit},
		OwnedActiveIntents: []protocol.IntentRevision{b.intent(b.mine, b.myParticipant, 1)}, RelatedCollisions: []protocol.CollisionAssessment{b.collision()},
		AcceptanceEvidenceCoverage: []protocol.RadarEvidenceCoverage{}, Updates: updates, KnownUnknowns: []string{"Retry contract prose that hooks must never print."}, AsOfEvent: b.asOf,
	}
}

func (b *fakeBackend) radar() protocol.RadarView {
	claim := func(id, intent uuid.UUID, revision int, subject, relation, path string) protocol.SemanticClaim {
		line := 1
		return protocol.SemanticClaim{ClaimID: id, ProjectID: b.projectID, IntentID: intent, IntentRevision: revision, SubjectKind: "contract", NormalizedSubject: subject, DisplayName: "Retry contract prose", Relation: relation, ProvenanceKind: "declared",
			EvidenceRefs: []protocol.ClaimEvidenceRef{{Kind: "source_location", Locator: "typescript:" + path, Path: path, Line: &line}}, ObservedAt: b.created()}
	}
	subject := "contract:typescript:src/payments/retry.ts/shouldRetryCharge"
	claims := []protocol.SemanticClaim{
		claim(b.theirClaim, b.other, 2, subject, "changes", "src/payments/retry.ts"),
		claim(b.myClaim, b.mine, 1, subject, "consumes", "src/orders/checkout.ts"),
	}
	for i := 0; i < b.manyClaims; i++ {
		claims = append(claims, claim(uuid.NewV7(), b.other, 2, fmt.Sprintf("contract:typescript:src/payments/retry.ts/export%02d", i), "changes", "src/payments/retry.ts"))
	}
	open := []protocol.CollisionAssessment{}
	if b.state == protocol.CollisionOpen {
		open = append(open, b.collision())
	}
	return protocol.RadarView{
		Project: b.project(), AsOfEvent: b.asOf, Objectives: []protocol.RadarObjective{}, Repositories: []protocol.RepositoryConnection{b.repository()},
		ActiveIntents:  []protocol.IntentRevision{b.intent(b.mine, b.myParticipant, 1), b.intent(b.other, b.otherPerson, 2)},
		SemanticClaims: claims, OpenCollisions: open, OpenQuestions: []string{}, Assumptions: []string{}, EvidenceCoverage: []protocol.RadarEvidenceCoverage{}, KnownUnknowns: []string{},
	}
}

func gitWorktree(t *testing.T, remote string) string {
	t.Helper()
	dir := t.TempDir()
	for _, args := range [][]string{{"init", "--quiet"}, {"remote", "add", "origin", remote}} {
		command := exec.Command("git", append([]string{"-C", dir}, args...)...)
		if output, err := command.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, output)
		}
	}
	return dir
}

func readState(t *testing.T, dir string) string {
	t.Helper()
	var all strings.Builder
	entries, _ := os.ReadDir(dir)
	for _, entry := range entries {
		data, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err == nil {
			all.Write(data)
		}
	}
	return all.String()
}

func short(id uuid.UUID) string { return id.String()[:8] }
