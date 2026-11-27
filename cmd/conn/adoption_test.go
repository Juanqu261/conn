package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"uuid"

	"github.com/Juanqu261/conn/internal/account"
	"github.com/Juanqu261/conn/internal/analyzer"
	"github.com/Juanqu261/conn/internal/cli"
	"github.com/Juanqu261/conn/internal/hookstate"
	"github.com/Juanqu261/conn/internal/protocol"
)

// TestGitHubIssueImportAndStart protects requirements 10 and 11. An issue is
// imported with provenance and suggested criteria, pull requests and
// oversized bodies are refused, and the GitHub token reaches GitHub but never
// Conn. conn start reuses the unchanged import, previews collisions for the
// planned paths, publishes a CLI Intent with the local focus, offers a new
// revision when the issue changed, and requires --publish or --import-only
// without a terminal.
func TestGitHubIssueImportAndStart(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()
	const githubToken = "gh-secret-token-for-import"
	var connMu sync.Mutex
	var connTraffic bytes.Buffer
	backend := newTestBackend(t, ctx, func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			body, _ := io.ReadAll(r.Body)
			connMu.Lock()
			for name, values := range r.Header {
				connTraffic.WriteString(name + ": " + strings.Join(values, ",") + "\n")
			}
			connTraffic.Write(body)
			connMu.Unlock()
			r.Body = io.NopCloser(bytes.NewReader(body))
			next.ServeHTTP(w, r)
		})
	})
	alice, bob := login(t, backend.auth, "gh-alice"), login(t, backend.auth, "gh-bob")
	projectID, objectiveID, repositoryID := uuid.NewV7(), uuid.NewV7(), uuid.NewV7()
	projects := backend.projects
	must := func(_ any, err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(projects.CreateProject(ctx, alice.HumanID, protocol.ProjectCreate{ProjectID: projectID, Name: "Shop"}))
	must(projects.AddMember(ctx, alice.HumanID, projectID, bob.HumanID))
	must(projects.ConnectRepository(ctx, alice.HumanID, projectID, protocol.RepositoryConnectInput{RepositoryID: repositoryID, HostKind: "github", CanonicalRemote: preEditRemote, DefaultBranch: "main", AnalysisEnabled: true}))
	must(projects.CreateObjectiveRevision(ctx, alice.HumanID, projectID, protocol.ObjectiveRevisionInput{ObjectiveID: objectiveID, Title: "Orders", OverviewMarkdown: "Orders work.", AccountableHumanID: alice.HumanID}, 0))
	bobParticipant, err := projects.ResolveParticipant(ctx, bob.HumanID, projectID, "kiro", "")
	if err != nil {
		t.Fatal(err)
	}
	bobIntent := uuid.NewV7()
	must(projects.PublishIntent(ctx, bob.HumanID, projectID, bobParticipant.Data.ParticipantID, protocol.IntentInput{
		IntentID: bobIntent, ObjectiveID: objectiveID, ObjectiveRevision: 1, RepositoryID: &repositoryID, SummaryMarkdown: "Use retries in checkout.",
		DeclaredClaims: []protocol.SemanticClaimCandidate{{SubjectKind: "contract", NormalizedSubject: "contract:typescript:src/payments/retry.ts/shouldRetryCharge", DisplayName: "shouldRetryCharge", Relation: "consumes", ProvenanceKind: "declared"}},
	}))

	var githubMu sync.Mutex
	issueBody := "Charges must not repeat.\n\n## Acceptance criteria\n\n- One charge per idempotency key\n- [ ] Retries stop after three attempts\n\n## Notes\n\n- not a criterion\n"
	updated := "2026-11-20T09:00:00Z"
	var githubAuthorizations []string
	github := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		githubMu.Lock()
		defer githubMu.Unlock()
		githubAuthorizations = append(githubAuthorizations, r.Header.Get("Authorization"))
		document := map[string]any{"title": "Prevent duplicate checkout charges", "body": issueBody, "updated_at": updated}
		switch r.URL.Path {
		case "/repos/acme/shop/issues/218":
		case "/repos/acme/shop/issues/219":
			document["pull_request"] = map[string]any{"url": "https://api.github.com/repos/acme/shop/pulls/219"}
		case "/repos/acme/shop/issues/220":
			document["body"] = strings.Repeat("x", 40_000)
		default:
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(document)
	}))
	defer github.Close()

	repo := cloneFixture(t, filepath.Join(repositoryRoot(t), "fixtures", "pre-edit-two-sessions"), "alice-charges")
	analyzerScript := filepath.Join(repositoryRoot(t), "packages", "typescript-analyzer", "dist", "cli.js")
	node, nodeErr := exec.LookPath("node")
	env := map[string]string{
		"CONN_SERVER_URL": backend.server.URL, "CONN_TOKEN": alice.Token.Reveal(), "CONN_PROJECT": projectID.String(),
		"GH_TOKEN": githubToken, analyzer.AnalyzerPathEnv: analyzerScript, analyzer.NodePathEnv: node,
	}
	lookup := func(name string) (string, bool) { value, ok := env[name]; return value, ok && value != "" }
	deps := dependencies(nil)
	deps.LookupEnv, deps.GitHubAPIURL = lookup, github.URL
	deps.Account = func(serverURL string) (*account.Service, error) {
		service, err := accountService(serverURL)
		if err == nil {
			service.LookupEnv = lookup
		}
		return service, err
	}
	location := analyzer.Locate(lookup, "", "")
	deps.Analyzer = func() analyzer.Location { return location }
	run := func(args ...string) (int, string, string) {
		t.Helper()
		var stdout, stderr bytes.Buffer
		code := cli.Execute(ctx, args, cli.Streams{In: strings.NewReader(""), Out: &stdout, Err: &stderr}, deps)
		return code, stdout.String(), stderr.String()
	}

	code, stdout, stderr := run("objective", "import-github-issue", "acme/shop#218", "--yes", "--json")
	if code != 0 || !strings.Contains(stderr, "is not a RepositoryConnection") {
		t.Fatalf("import exited %d: %s %s", code, stdout, stderr)
	}
	var imported struct {
		Data protocol.Objective `json:"data"`
	}
	if err := json.Unmarshal([]byte(stdout), &imported); err != nil {
		t.Fatalf("import output %q: %v", stdout, err)
	}
	hash := sha256.Sum256([]byte("Prevent duplicate checkout charges\n\n" + issueBody))
	provenance := imported.Data.ImportProvenance
	if provenance == nil || provenance.SourceKind != "github_issue" || provenance.SourceRef != "github.com/acme/shop/issues/218" ||
		provenance.SourceRevisionRef != updated || provenance.ContentHash != hex.EncodeToString(hash[:]) || len(imported.Data.KeyPoints) != 2 ||
		imported.Data.KeyPoints[0].Key != "one-charge-per-idempotency-key" || imported.Data.KeyPoints[1].Statement != "Retries stop after three attempts" {
		t.Fatalf("imported objective = %+v", imported.Data)
	}
	if code, _, stderr := run("objective", "import-github-issue", "https://github.com/acme/shop/issues/219", "--yes"); code != cli.ExitUsage || !strings.Contains(stderr, "pull request") {
		t.Fatalf("pull request import exited %d: %s", code, stderr)
	}
	if code, _, stderr := run("objective", "import-github-issue", "acme/shop#220", "--yes"); code != cli.ExitUsage || !strings.Contains(stderr, "shorten") {
		t.Fatalf("oversized import exited %d: %s", code, stderr)
	}

	// conn start reuses the import, previews collisions, and publishes.
	if code, _, stderr := run("start", "acme/shop#218", "--worktree", repo); code != cli.ExitUsage || !strings.Contains(stderr, "--publish or --import-only") {
		t.Fatalf("non-interactive start exited %d: %s", code, stderr)
	}
	code, stdout, stderr = run("start", "acme/shop#218", "--publish", "--paths", "src/payments/retry.ts", "--worktree", repo)
	if code != 0 || !strings.Contains(stderr, "it is reused") || !strings.Contains(stdout, "Objective: Prevent duplicate checkout charges (github.com/acme/shop/issues/218, revision 1)") ||
		!strings.Contains(stdout, "Published Intent") {
		t.Fatalf("start exited %d:\n%s\n%s", code, stdout, stderr)
	}
	if nodeErr == nil && location.Available() && (!strings.Contains(stdout, "Previewed collisions: AMBER") || !strings.Contains(stdout, bobIntent.String()[:8]+"@1")) {
		t.Errorf("start did not preview the collision with Intent %s:\n%s", bobIntent, stdout)
	}
	store, err := hookstate.Open(ctx, repo)
	if err != nil {
		t.Fatal(err)
	}
	focus := store.Focus()
	if !focus.HasIntent() || *focus.ProjectID != projectID || focus.IntentRevision != 1 {
		t.Fatalf("focus = %+v", focus)
	}
	published, err := projects.Intent(ctx, alice.HumanID, projectID, *focus.IntentID)
	if err != nil || published.Data.IntentRevision.ObjectiveID != imported.Data.ObjectiveID || published.Data.IntentRevision.RepositoryID == nil {
		t.Fatalf("published Intent = %+v, %v", published.Data, err)
	}

	// A changed issue becomes a new revision of the same Objective.
	githubMu.Lock()
	issueBody += "- Refunds are not retried\n"
	updated = "2026-11-21T09:00:00Z"
	githubMu.Unlock()
	code, stdout, stderr = run("start", "acme/shop#218", "--import-only", "--worktree", repo)
	if code != 0 || !strings.Contains(stderr, "changed since") || !strings.Contains(stdout, "revision 2") || !strings.Contains(stdout, "/conn-start "+imported.Data.ObjectiveID.String()) {
		t.Fatalf("start after a change exited %d:\n%s\n%s", code, stdout, stderr)
	}

	// A Kiro spec directory imports its requirements with a safe reference.
	spec := filepath.Join(repo, ".kiro", "specs", "checkout-retries")
	if err := os.MkdirAll(spec, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(spec, "requirements.md"), []byte("# Requirements\n\nRetries never charge twice.\n\n### Acceptance criteria\n\n1. WHEN a retry runs THE SYSTEM SHALL reuse the idempotency key\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	code, stdout, stderr = run("start", spec, "--import-only", "--worktree", repo)
	if code != 0 || !strings.Contains(stdout, "Objective: Checkout retries (Kiro spec .kiro/specs/checkout-retries/requirements.md, revision 1)") {
		t.Fatalf("start from a Kiro spec exited %d:\n%s\n%s", code, stdout, stderr)
	}

	githubMu.Lock()
	for _, header := range githubAuthorizations {
		if header != "Bearer "+githubToken {
			t.Errorf("GitHub request authorization = %q", header)
		}
	}
	githubMu.Unlock()
	connMu.Lock()
	defer connMu.Unlock()
	if connTraffic.Len() == 0 || strings.Contains(connTraffic.String(), githubToken) {
		t.Fatal("the GitHub token reached the Conn backend")
	}
}
