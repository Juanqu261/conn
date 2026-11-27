package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5/stdlib"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/Juanqu261/conn/internal/application"
	"github.com/Juanqu261/conn/internal/hookstate"
	"github.com/Juanqu261/conn/internal/httpapi"
	"github.com/Juanqu261/conn/internal/migrations"
	"github.com/Juanqu261/conn/internal/postgres"
	"github.com/Juanqu261/conn/internal/postgres/postgrestest"
	"github.com/Juanqu261/conn/internal/protocol"
	"github.com/Juanqu261/conn/internal/secret"
)

const preEditRemote = "github.com/conn-fixtures/pre-edit-two-sessions"

// TestPreEditTwoSessions is the milestone acceptance scenario (requirement
// 16). Two Kiro participants who never exchange subject strings list
// concepts, suggest claims for planned paths, and publish. Conn opens a
// contract_producer_consumer and a shared_declared_invariant assessment
// before either writes a file. The second participant's pre-write hook
// warns without blocking, the first participant's next prompt reports the
// new collisions once, and a dismissal survives a revision that adds no
// matched subject.
func TestPreEditTwoSessions(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("Node.js is required for planned analysis")
	}
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is required for the two worktrees")
	}
	repositoryRoot := repositoryRoot(t)
	analyzerScript := filepath.Join(repositoryRoot, "packages", "typescript-analyzer", "dist", "cli.js")
	if _, err := os.Stat(analyzerScript); err != nil {
		t.Skip("build the analyzer with `pnpm build` to run the pre-edit scenario")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	pool := postgrestest.NewDatabase(t)
	db := stdlib.OpenDBFromPool(pool)
	t.Cleanup(func() { _ = db.Close() })
	quiet := slog.New(slog.NewTextHandler(io.Discard, nil))
	if _, err := migrations.Up(ctx, db, quiet); err != nil {
		t.Fatal(err)
	}
	auth := &application.Auth{
		Store: postgres.AuthStore{Pool: pool},
		VerifyToken: func(_ context.Context, token secret.String) (application.GitHubIdentity, error) {
			if token.Reveal() == "gh-alice" {
				return application.GitHubIdentity{UserID: 11, Login: "alice"}, nil
			}
			return application.GitHubIdentity{UserID: 12, Login: "bob"}, nil
		},
	}
	projects := &application.Projects{Store: postgres.ProjectStore{Pool: pool}}
	backend := httptest.NewServer(httpapi.NewHandler(httpapi.Config{Ready: func(context.Context) error { return nil }, Auth: auth, Projects: projects, Log: quiet}))
	t.Cleanup(backend.Close)

	// The owner sets up an Objective and a repository; no Blueprint exists.
	alice, bob := login(t, auth, "gh-alice"), login(t, auth, "gh-bob")
	projectID, objectiveID, repositoryID := uuid.NewV7(), uuid.NewV7(), uuid.NewV7()
	if _, err := projects.CreateProject(ctx, alice.HumanID, protocol.ProjectCreate{ProjectID: projectID, Name: "Checkout"}); err != nil {
		t.Fatal(err)
	}
	if _, err := projects.AddMember(ctx, alice.HumanID, projectID, bob.HumanID); err != nil {
		t.Fatal(err)
	}
	if _, err := projects.CreateObjectiveRevision(ctx, alice.HumanID, projectID, protocol.ObjectiveRevisionInput{
		ObjectiveID: objectiveID, Key: "checkout", Title: "Reliable checkout retries", OverviewMarkdown: "Retry transport failures safely.", AccountableHumanID: alice.HumanID,
		KeyPoints: []protocol.KeyPoint{
			{Key: "one-charge-per-attempt", Kind: protocol.KeyPointInvariant, Statement: "A retried checkout creates at most one charge per attempt key."},
			{Key: "retry-evidence", Kind: protocol.KeyPointAcceptanceCriterion, Statement: "Retry behavior has passing automated evidence."},
		},
	}, 0); err != nil {
		t.Fatal(err)
	}
	if _, err := projects.ConnectRepository(ctx, alice.HumanID, projectID, protocol.RepositoryConnectInput{
		RepositoryID: repositoryID, HostKind: "github", CanonicalRemote: preEditRemote, DefaultBranch: "main", AnalysisEnabled: true,
	}); err != nil {
		t.Fatal(err)
	}

	fixture := filepath.Join(repositoryRoot, "fixtures", "pre-edit-two-sessions")
	aliceTree, bobTree := cloneFixture(t, fixture, "alice-retry"), cloneFixture(t, fixture, "bob-checkout")
	env := func(token secret.String) []string {
		return append(os.Environ(), runMainEnv+"=1", "CONN_SERVER_URL="+backend.URL, "CONN_TOKEN="+token.Reveal(),
			"CONN_PROJECT="+projectID.String(), "CONN_ANALYZER_PATH="+analyzerScript, "CONN_NODE_PATH="+node)
	}
	aliceMCP := startMCP(t, ctx, aliceTree, env(alice.Token))
	bobMCP := startMCP(t, ctx, bobTree, env(bob.Token))

	// Each participant reads the catalog and asks for claim suggestions for
	// the files they plan to change. Neither types a subject string.
	concepts := protocol.ConceptList{}
	decodeData(t, aliceMCP.call(t, ctx, "list_concepts", map[string]any{"q": "retry charge attempt"}), &concepts)
	if !slices.ContainsFunc(concepts.Concepts, func(entry protocol.ConceptEntry) bool {
		return entry.NormalizedSubject == "invariant:checkout:one-charge-per-attempt"
	}) {
		t.Fatalf("catalog = %+v", concepts.Concepts)
	}
	aliceClaims := suggested(t, aliceMCP.call(t, ctx, "suggest_claims", map[string]any{
		"paths": []any{"src/payments/retry.ts"}, "objective_id": objectiveID.String(), "focus": "retry charge",
	}), "contract:typescript:src/payments/retry.ts/shouldRetryCharge changes", "invariant:checkout:one-charge-per-attempt affects")
	bobClaims := suggested(t, bobMCP.call(t, ctx, "suggest_claims", map[string]any{
		"paths": []any{"src/orders/checkout.ts", "src/orders/refund.ts"}, "objective_id": objectiveID.String(),
	}), "contract:typescript:src/payments/retry.ts/shouldRetryCharge consumes", "invariant:checkout:one-charge-per-attempt affects")

	aliceIntent := publish(t, aliceMCP.call(t, ctx, "publish_intent", map[string]any{"intent": map[string]any{
		"objective_id": objectiveID.String(), "objective_revision": 1, "summary_markdown": "Change retry eligibility for charges.", "declared_claims": aliceClaims,
	}}), repositoryID, "alice-retry")
	bobIntent := publish(t, bobMCP.call(t, ctx, "publish_intent", map[string]any{"intent": map[string]any{
		"objective_id": objectiveID.String(), "objective_revision": 1, "summary_markdown": "Change the checkout retry step.", "declared_claims": bobClaims,
	}}), repositoryID, "bob-checkout")

	// Both assessments exist before either worktree changes.
	groups := collisionGroups(t, bobMCP.call(t, ctx, "check_collisions", map[string]any{"intent_id": bobIntent.IntentID.String()}))
	rules := map[string]protocol.CollisionAssessment{}
	for _, collision := range groups.NeedsAttention {
		rules[collision.RuleID] = collision
	}
	if len(groups.NeedsAttention) != 2 || rules["contract_producer_consumer"].State != "open" || rules["shared_declared_invariant"].State != "open" {
		t.Fatalf("collisions before any write = %+v", groups)
	}
	for _, tree := range []string{aliceTree, bobTree} {
		if status := gitOutput(t, tree, "status", "--porcelain"); status != "" {
			t.Fatalf("a worktree changed before the warning: %s", status)
		}
	}

	// The MCP adapters recorded each worktree's focus without credentials.
	for _, tree := range []struct {
		dir    string
		intent uuid.UUID
		token  secret.String
	}{{aliceTree, aliceIntent.IntentID, alice.Token}, {bobTree, bobIntent.IntentID, bob.Token}} {
		store, err := hookstate.Open(ctx, tree.dir)
		if err != nil {
			t.Fatal(err)
		}
		focus := store.Focus()
		data, _ := os.ReadFile(filepath.Join(store.Dir, hookstate.FocusFile))
		if !focus.HasIntent() || *focus.IntentID != tree.intent || strings.Contains(string(data), tree.token.Reveal()) {
			t.Fatalf("focus record = %s", data)
		}
	}

	// The second participant's agent is warned, and not blocked, before it
	// writes the conflicting file.
	code, _, stderr := runHook(t, bobTree, env(bob.Token), "pre-write", map[string]any{"tool_name": "fs_write", "tool_input": map[string]any{"path": "src/orders/checkout.ts"}})
	if code != 1 || !strings.Contains(stderr, "Collision "+rules["contract_producer_consumer"].CollisionID.String()[:8]+" (contract_producer_consumer)") {
		t.Fatalf("bob pre-write on checkout.ts = %d %q", code, stderr)
	}
	code, _, stderr = runHook(t, bobTree, env(bob.Token), "pre-write", map[string]any{"tool_name": "fs_write", "tool_input": map[string]any{"path": filepath.Join(bobTree, "src", "payments", "retry.ts")}})
	if code != 1 || !strings.Contains(stderr, aliceIntent.IntentID.String()[:8]+"@1 changes contract:typescript:src/payments/retry.ts/shouldRetryCharge") {
		t.Fatalf("bob pre-write on retry.ts = %d %q", code, stderr)
	}

	// The first participant's next prompt reports the new collisions once.
	_, stdout, _ := runHook(t, aliceTree, env(alice.Token), "prompt-submit", map[string]any{"session_id": "alice-session"})
	if strings.Count(stdout, "new open collision") != 2 || !strings.Contains(stdout, "contract_producer_consumer") || !strings.Contains(stdout, "shared_declared_invariant") {
		t.Fatalf("alice prompt-submit = %q", stdout)
	}
	if _, stdout, _ = runHook(t, aliceTree, env(alice.Token), "prompt-submit", map[string]any{"session_id": "alice-session"}); stdout != "" {
		t.Fatalf("alice second prompt-submit = %q, want nothing", stdout)
	}

	// Bob, the Human behind one Intent, dismisses the invariant collision.
	// His revision without new subjects keeps it dismissed; the contract
	// collision stays open because nobody decided it.
	invariant := rules["shared_declared_invariant"]
	decision, err := projects.DecideResolution(ctx, bob.HumanID, projectID, invariant.CollisionID, protocol.ResolutionDecisionInput{
		Kind: "false_positive", RationaleMarkdown: "The provider guarantees one charge per attempt key.",
	})
	if err != nil {
		t.Fatal(err)
	}
	revised := publish(t, bobMCP.call(t, ctx, "revise_intent", map[string]any{"intent_id": bobIntent.IntentID.String(), "expected_revision": 1, "intent": map[string]any{
		"objective_id": objectiveID.String(), "objective_revision": 1, "summary_markdown": "Change the checkout retry step after review.", "declared_claims": bobClaims,
	}}), repositoryID, "bob-checkout")
	groups = collisionGroups(t, bobMCP.call(t, ctx, "check_collisions", map[string]any{"intent_id": bobIntent.IntentID.String()}))
	if revised.Revision != 2 || len(groups.Decided) != 1 || groups.Decided[0].RuleID != "shared_declared_invariant" || groups.Decided[0].State != "dismissed" ||
		groups.Decided[0].DecisionRef == nil || *groups.Decided[0].DecisionRef != decision.Data.DecisionID ||
		len(groups.NeedsAttention) != 1 || groups.NeedsAttention[0].RuleID != "contract_producer_consumer" {
		t.Fatalf("collisions after the dismissal and revision = %+v", groups)
	}
	_, stdout, _ = runHook(t, aliceTree, env(alice.Token), "prompt-submit", map[string]any{"session_id": "alice-session"})
	if !strings.Contains(stdout, "is now dismissed (false_positive)") || strings.Contains(stdout, "new open collision") ||
		!strings.Contains(stdout, "Counterpart Intent "+bobIntent.IntentID.String()[:8]+" is now at revision 2") {
		t.Fatalf("alice prompt-submit after the dismissal = %q", stdout)
	}

	// Releasing clears the focus record.
	bobMCP.call(t, ctx, "release_intent", map[string]any{"intent_id": bobIntent.IntentID.String()})
	store, err := hookstate.Open(ctx, bobTree)
	if err != nil || store.Focus().HasIntent() {
		t.Fatalf("focus after release = %+v, %v", store.Focus(), err)
	}
	for _, output := range []string{aliceMCP.stderr.String(), bobMCP.stderr.String()} {
		if strings.Contains(output, alice.Token.Reveal()) || strings.Contains(output, bob.Token.Reveal()) || strings.Contains(output, "PRE_EDIT_FIXTURE_SOURCE") {
			t.Fatal("a credential or fixture source appeared in conn mcp diagnostics")
		}
	}
}

type mcpProcess struct {
	session *sdk.ClientSession
	stderr  *bytes.Buffer
}

func startMCP(t *testing.T, ctx context.Context, dir string, env []string) *mcpProcess {
	t.Helper()
	cmd := exec.Command(os.Args[0], "mcp")
	cmd.Dir, cmd.Env = dir, env
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	session, err := sdk.NewClient(&sdk.Implementation{Name: "kiro-pre-edit-test", Version: "0"}, nil).Connect(ctx, &sdk.CommandTransport{Command: cmd}, nil)
	if err != nil {
		t.Fatalf("start conn mcp: %v\n%s", err, stderr.String())
	}
	t.Cleanup(func() { _ = session.Close() })
	return &mcpProcess{session: session, stderr: &stderr}
}

func (p *mcpProcess) call(t *testing.T, ctx context.Context, name string, args map[string]any) json.RawMessage {
	t.Helper()
	result, err := p.session.CallTool(ctx, &sdk.CallToolParams{Name: name, Arguments: args})
	if err != nil || result.IsError {
		t.Fatalf("%s: %v %s\nstderr:\n%s", name, err, text(result), p.stderr.String())
	}
	raw, err := json.Marshal(result.StructuredContent)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "PRE_EDIT_FIXTURE_SOURCE") {
		t.Fatalf("%s returned fixture source", name)
	}
	return raw
}

func decodeData(t *testing.T, raw json.RawMessage, target any) {
	t.Helper()
	var envelope struct {
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil || json.Unmarshal(envelope.Data, target) != nil {
		t.Fatalf("decode %s: %v", raw, err)
	}
}

// suggested returns every candidate claim and checks that the wanted
// "subject relation" pairs are present.
func suggested(t *testing.T, raw json.RawMessage, want ...string) []any {
	t.Helper()
	var suggestions struct {
		Candidates []struct {
			Claim  map[string]any `json:"claim"`
			Source string         `json:"source"`
		} `json:"candidates"`
		NewPaths []string `json:"new_paths"`
		Analysis struct {
			Available bool `json:"available"`
		} `json:"analysis"`
	}
	decodeData(t, raw, &suggestions)
	if !suggestions.Analysis.Available {
		t.Fatalf("planned analysis was unavailable: %s", raw)
	}
	have := map[string]bool{}
	claims := make([]any, 0, len(suggestions.Candidates))
	for _, candidate := range suggestions.Candidates {
		if candidate.Claim["provenance_kind"] != "declared" {
			t.Fatalf("candidate is not a declared claim: %+v", candidate)
		}
		have[candidate.Claim["normalized_subject"].(string)+" "+candidate.Claim["relation"].(string)] = true
		claims = append(claims, candidate.Claim)
	}
	for _, pair := range want {
		if !have[pair] {
			t.Fatalf("suggestions omitted %q: %s", pair, raw)
		}
	}
	return claims
}

func publish(t *testing.T, raw json.RawMessage, repositoryID uuid.UUID, branch string) protocol.IntentRevision {
	t.Helper()
	var envelope struct {
		Data   json.RawMessage `json:"data"`
		Filled []struct {
			Field string `json:"field"`
			Value string `json:"value"`
		} `json:"filled_fields"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		t.Fatal(err)
	}
	view, err := protocol.Decode[protocol.IntentView](envelope.Data)
	if err != nil {
		t.Fatal(err)
	}
	intent := view.IntentRevision
	if intent.RepositoryID == nil || *intent.RepositoryID != repositoryID || intent.BranchRef != branch || len(intent.BaseRevisionRef) != 40 || len(envelope.Filled) != 3 {
		t.Fatalf("Git context = repository %v branch %q base %q filled %+v", intent.RepositoryID, intent.BranchRef, intent.BaseRevisionRef, envelope.Filled)
	}
	return intent
}

func collisionGroups(t *testing.T, raw json.RawMessage) struct {
	NeedsAttention []protocol.CollisionAssessment `json:"needs_attention"`
	Decided        []protocol.CollisionAssessment `json:"decided"`
} {
	t.Helper()
	var groups struct {
		NeedsAttention []protocol.CollisionAssessment `json:"needs_attention"`
		Decided        []protocol.CollisionAssessment `json:"decided"`
	}
	decodeData(t, raw, &groups)
	return groups
}

func runHook(t *testing.T, dir string, env []string, name string, event map[string]any) (int, string, string) {
	t.Helper()
	event["cwd"] = dir
	input, _ := json.Marshal(event)
	cmd := exec.Command(os.Args[0], "hook", name)
	cmd.Dir, cmd.Env, cmd.Stdin = dir, env, bytes.NewReader(input)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	start := time.Now()
	err := cmd.Run()
	code := 0
	if exit, ok := err.(*exec.ExitError); ok {
		code = exit.ExitCode()
	} else if err != nil {
		t.Fatal(err)
	}
	if code == 2 || strings.Contains(stdout.String()+stderr.String(), "permissionDecision") || time.Since(start) > 10*time.Second {
		t.Fatalf("hook %s exited %d after %s: %q %q", name, code, time.Since(start), stdout.String(), stderr.String())
	}
	return code, stdout.String(), stderr.String()
}

// cloneFixture copies the fixture into a new Git worktree whose origin is
// the fixture remote and whose origin/main is the initial commit.
func cloneFixture(t *testing.T, fixture, branch string) string {
	t.Helper()
	dir := t.TempDir()
	if err := filepath.Walk(fixture, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		relative, _ := filepath.Rel(fixture, path)
		target := filepath.Join(dir, relative)
		if info.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, 0o644)
	}); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"init", "--quiet", "-b", "main"},
		{"-c", "core.autocrlf=false", "add", "--all"},
		{"-c", "user.name=Conn Test", "-c", "user.email=conn@example.invalid", "commit", "--quiet", "-m", "fixture"},
		{"remote", "add", "origin", "https://" + preEditRemote + ".git"},
		{"update-ref", "refs/remotes/origin/main", "HEAD"},
		{"checkout", "--quiet", "-b", branch},
	} {
		gitOutput(t, dir, args...)
	}
	return dir
}

func gitOutput(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, output)
	}
	return strings.TrimSpace(string(output))
}

func repositoryRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate the repository root")
	}
	return filepath.Dir(filepath.Dir(filepath.Dir(file)))
}
