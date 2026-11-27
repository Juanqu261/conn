package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"uuid"

	"github.com/Juanqu261/conn/internal/cicheck"
	"github.com/Juanqu261/conn/internal/protocol"
)

// TestCICredentialAndPullRequestCheck is the milestone 3 acceptance scenario
// for the CI add-on (requirements 4, 5, 13, and 14). Only an owner creates a
// CI credential, only from the CLI, and only with a delivery option; Conn
// stores its hash and audit events. The credential reads the allowlisted
// routes of its own Project, nothing else, and stops working when it expires
// or is revoked. conn ci check reports a trailer-named Intent with its review
// packet, unregistered work with a previewed collision, a released Intent,
// and a missing credential, always exits 0, and writes nothing to Conn.
//
// It also reproduces a defect: a trailer is typed by the commit author, and
// a pull request that named a teammate's conflicting Intent had that Intent
// excluded from the preview, hiding the collision, and borrowed its review
// packet. Only an Intent that declares the pull request's head branch counts.
func TestCICredentialAndPullRequestCheck(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 240*time.Second)
	defer cancel()
	backend := newTestBackend(t, ctx, nil)
	projects := backend.projects
	alice, bob := login(t, backend.auth, "gh-alice"), login(t, backend.auth, "gh-bob")
	projectID, otherProject, objectiveID, repositoryID := uuid.NewV7(), uuid.NewV7(), uuid.NewV7(), uuid.NewV7()
	must := func(_ any, err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(projects.CreateProject(ctx, alice.HumanID, protocol.ProjectCreate{ProjectID: projectID, Name: "Checkout"}))
	must(projects.CreateProject(ctx, alice.HumanID, protocol.ProjectCreate{ProjectID: otherProject, Name: "Other"}))
	must(projects.AddMember(ctx, alice.HumanID, projectID, bob.HumanID))
	must(projects.CreateObjectiveRevision(ctx, alice.HumanID, projectID, protocol.ObjectiveRevisionInput{
		ObjectiveID: objectiveID, Title: "Reliable retries", OverviewMarkdown: "Retry safely.", AccountableHumanID: alice.HumanID,
		KeyPoints: []protocol.KeyPoint{{Key: "retry-evidence", Kind: protocol.KeyPointAcceptanceCriterion, Statement: "Retries have passing evidence."}},
	}, 0))
	must(projects.ConnectRepository(ctx, alice.HumanID, projectID, protocol.RepositoryConnectInput{RepositoryID: repositoryID, HostKind: "github", CanonicalRemote: preEditRemote, DefaultBranch: "main", AnalysisEnabled: true}))

	repo := cloneFixture(t, filepath.Join(repositoryRoot(t), "fixtures", "pre-edit-two-sessions"), "alice-retry")
	mainCommit := gitOutput(t, repo, "rev-parse", "main")
	subject := "contract:typescript:src/payments/retry.ts/shouldRetryCharge"
	publish := func(human uuid.UUID, branch, relation string) uuid.UUID {
		t.Helper()
		participant, err := projects.ResolveParticipant(ctx, human, projectID, "kiro", "")
		if err != nil {
			t.Fatal(err)
		}
		id := uuid.NewV7()
		must(projects.PublishIntent(ctx, human, projectID, participant.Data.ParticipantID, protocol.IntentInput{
			IntentID: id, ObjectiveID: objectiveID, ObjectiveRevision: 1, RepositoryID: &repositoryID, BranchRef: branch, BaseRevisionRef: mainCommit,
			SummaryMarkdown: "Change " + branch + ".", DeclaredClaims: []protocol.SemanticClaimCandidate{
				{SubjectKind: "contract", NormalizedSubject: subject, DisplayName: "shouldRetryCharge", Relation: relation, ProvenanceKind: "declared"},
			},
		}))
		return id
	}
	bobIntent := publish(bob.HumanID, "bob-orders", "consumes")
	aliceIntent := publish(alice.HumanID, "alice-retry", "changes")
	radar, err := projects.Radar(ctx, alice.HumanID, projectID)
	if err != nil || len(radar.Data.OpenCollisions) != 1 {
		t.Fatalf("radar = %+v, %v", radar.Data.OpenCollisions, err)
	}
	collisionID := radar.Data.OpenCollisions[0].CollisionID

	runConn := func(token string, extra []string, args ...string) (int, string, string) {
		t.Helper()
		env := append(os.Environ(), runMainEnv+"=1", "CONN_SERVER_URL="+backend.server.URL, "CONN_TOKEN="+token, "CONN_PROJECT="+projectID.String())
		cmd := exec.CommandContext(ctx, os.Args[0], args...)
		cmd.Dir, cmd.Env = repo, append(env, extra...)
		var stdout, stderr bytes.Buffer
		cmd.Stdout, cmd.Stderr = &stdout, &stderr
		err := cmd.Run()
		if exit, ok := err.(*exec.ExitError); ok {
			return exit.ExitCode(), stdout.String(), stderr.String()
		} else if err != nil {
			t.Fatal(err)
		}
		return 0, stdout.String(), stderr.String()
	}
	count := func(query string, args ...any) int {
		t.Helper()
		var n int
		if err := backend.pool.QueryRow(ctx, query, args...).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}

	// Creation needs an owner, a delivery option, and a human-direct surface.
	if code, _, stderr := runConn(alice.Token.Reveal(), nil, "ci", "credential", "create", "--repository", repositoryID.String()); code != 2 || !strings.Contains(stderr, "--github-secret") {
		t.Fatalf("create without delivery exited %d: %s", code, stderr)
	}
	if code, _, stderr := runConn(bob.Token.Reveal(), nil, "ci", "credential", "create", "--repository", repositoryID.String(), "--print"); code != 4 {
		t.Fatalf("member create exited %d: %s", code, stderr)
	}
	agent := apiRequest(t, backend, alice.Token.Reveal(), http.MethodPost, "/v1/projects/"+projectID.String()+"/ci-credentials", `{"repository_id":"`+repositoryID.String()+`","expires_in_days":90}`, "kiro")
	if agent.status != http.StatusForbidden || !strings.Contains(agent.body, "human_direct_required") {
		t.Fatalf("MCP-surface creation = %d %s", agent.status, agent.body)
	}
	if n := count(`SELECT count(*) FROM ci_credentials`); n != 0 {
		t.Fatalf("refused creations stored %d credentials", n)
	}
	code, stdout, stderr := runConn(alice.Token.Reveal(), nil, "ci", "credential", "create", "--repository", repositoryID.String(), "--print", "--label", "GitHub Actions")
	token := strings.TrimSpace(stdout)
	if code != 0 || !strings.HasPrefix(token, protocol.CITokenPrefix) || strings.Contains(stderr, token) {
		t.Fatalf("create --print exited %d: %q %q", code, stdout, stderr)
	}
	hash := sha256.Sum256([]byte(token))
	if count(`SELECT count(*) FROM ci_credentials WHERE token_sha256 = $1`, hash[:]) != 1 || count(`SELECT count(*) FROM events WHERE event_type = 'ci_credential.created'`) != 1 ||
		count(`SELECT count(*) FROM events WHERE payload::text LIKE '%conn_ci_%'`) != 0 {
		t.Fatal("the credential is not stored as a hash with one audit event")
	}

	// The allowlist, parameterized over routes.
	p, i, o := "/v1/projects/"+projectID.String(), aliceIntent.String(), objectiveID.String()
	for _, route := range []struct {
		method, path, body string
		want               int
	}{
		{http.MethodGet, "/v1/ci/credential", "", http.StatusOK},
		{http.MethodGet, p + "/radar", "", http.StatusOK},
		{http.MethodGet, p + "/intents", "", http.StatusOK},
		{http.MethodGet, p + "/intents/" + i, "", http.StatusOK},
		{http.MethodGet, p + "/intents/" + i + "/evidence", "", http.StatusOK},
		{http.MethodGet, p + "/objectives/" + o + "/revisions/1", "", http.StatusOK},
		{http.MethodGet, p + "/collisions/" + collisionID.String() + "/context", "", http.StatusOK},
		{http.MethodGet, p + "/members", "", http.StatusOK},
		{http.MethodGet, p + "/concepts", "", http.StatusOK},
		{http.MethodPost, p + "/collision-previews", `{"claims":[]}`, http.StatusOK},
		{http.MethodGet, "/v1/projects/" + otherProject.String() + "/radar", "", http.StatusForbidden},
		{http.MethodGet, "/v1/projects", "", http.StatusForbidden},
		{http.MethodGet, "/v1/humans/me", "", http.StatusForbidden},
		{http.MethodGet, p + "/status", "", http.StatusForbidden},
		{http.MethodGet, p + "/work-context", "", http.StatusForbidden},
		{http.MethodGet, p + "/events", "", http.StatusForbidden},
		{http.MethodGet, p + "/insights", "", http.StatusForbidden},
		{http.MethodGet, p + "/ci-credentials", "", http.StatusForbidden},
		{http.MethodPost, p + "/intents", `{}`, http.StatusForbidden},
		{http.MethodPost, p + "/intents/" + i + "/evidence", `{}`, http.StatusForbidden},
		{http.MethodPost, p + "/collisions/" + collisionID.String() + "/decisions", `{}`, http.StatusForbidden},
		{http.MethodPost, p + "/collisions/" + collisionID.String() + "/combination-checks", `{}`, http.StatusForbidden},
		{http.MethodPost, p + "/ci-credentials", `{}`, http.StatusForbidden},
	} {
		t.Run(route.method+" "+strings.ReplaceAll(route.path, "/", "_"), func(t *testing.T) {
			response := apiRequest(t, backend, token, route.method, route.path, route.body, "")
			if response.status != route.want || (route.want != http.StatusOK && !strings.Contains(response.body, `"error"`)) {
				t.Fatalf("%s %s = %d %s, want %d", route.method, route.path, response.status, response.body, route.want)
			}
		})
	}
	if session := apiRequest(t, backend, alice.Token.Reveal(), http.MethodGet, "/v1/ci/credential", "", ""); session.status != http.StatusForbidden {
		t.Fatalf("a session read the CI credential route: %d", session.status)
	}

	// The pull-request check with a trailer-named Intent.
	node, nodeErr := exec.LookPath("node")
	analyzerScript := filepath.Join(repositoryRoot(t), "packages", "typescript-analyzer", "dist", "cli.js")
	_, scriptErr := os.Stat(analyzerScript)
	analyzed := nodeErr == nil && scriptErr == nil
	retry := filepath.Join(repo, "src", "payments", "retry.ts")
	original, err := os.ReadFile(retry)
	if err != nil {
		t.Fatal(err)
	}
	identity := []string{"-c", "user.name=Conn Test", "-c", "user.email=conn@example.invalid"}
	branchCommit := func(branch, message string) string {
		t.Helper()
		gitOutput(t, repo, "checkout", "--quiet", "-B", branch, mainCommit)
		if err := os.WriteFile(retry, []byte(strings.Replace(strings.ReplaceAll(string(original), "\r\n", "\n"), "attempt.attempt < 3", "attempt.attempt < 5", 1)), 0o644); err != nil {
			t.Fatal(err)
		}
		gitOutput(t, repo, append(identity, "-c", "core.autocrlf=false", "add", "--", "src/payments/retry.ts")...)
		gitOutput(t, repo, append(identity, "commit", "--quiet", "-m", message)...)
		return gitOutput(t, repo, "rev-parse", "HEAD")
	}
	check := func(branch, head string, env ...string) (string, int) {
		t.Helper()
		directory := t.TempDir()
		event, summary := filepath.Join(directory, "event.json"), filepath.Join(directory, "summary.md")
		document, _ := json.Marshal(map[string]any{
			"pull_request": map[string]any{"number": 7, "base": map[string]any{"sha": mainCommit}, "head": map[string]any{"sha": head, "ref": branch}},
			"repository":   map[string]any{"full_name": "conn-fixtures/pre-edit-two-sessions"},
		})
		if err := os.WriteFile(event, document, 0o644); err != nil {
			t.Fatal(err)
		}
		if analyzed {
			env = append(env, "CONN_ANALYZER_PATH="+analyzerScript, "CONN_NODE_PATH="+node)
		}
		before := count(`SELECT count(*) FROM events`)
		code, stdout, stderr := runConn("", env, "ci", "check", "--worktree", repo, "--event", event, "--summary", summary)
		if code != 0 || count(`SELECT count(*) FROM events`) != before {
			t.Fatalf("conn ci check exited %d or wrote events: %s %s", code, stdout, stderr)
		}
		content, err := os.ReadFile(summary)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(content)+stdout+stderr, token) {
			t.Fatal("the CI token appeared in the check output")
		}
		return string(content), code
	}
	ci := "CONN_CI_TOKEN=" + token
	named := branchCommit("alice-retry", "Widen retries\n\n"+protocol.CommitTrailer(aliceIntent, 1))
	report, _ := check("alice-retry", named, ci)
	for _, want := range []string{cicheck.Marker, "| 1 | 1 |", "Review packet: Intent <code>" + aliceIntent.String() + "</code> revision 1", "contract_producer_consumer", "by @alice", "Drift compares declared claims with analysis observed in CI"} {
		if want == "Drift compares declared claims with analysis observed in CI" && !analyzed {
			continue
		}
		if !strings.Contains(report, want) {
			t.Errorf("trailer report omits %q:\n%s", want, report)
		}
	}

	// Unregistered work that changes a contract an active Intent consumes.
	unregistered := branchCommit("dave-retry", "Widen retries without an Intent")
	report, _ = check("dave-retry", unregistered, ci)
	if !strings.Contains(report, "Unregistered work") {
		t.Errorf("unregistered report:\n%s", report)
	}
	if analyzed && (!strings.Contains(report, "### Previewed collisions") || !strings.Contains(report, "with Intent `"+bobIntent.String()+"`")) {
		t.Errorf("unregistered report omits the previewed collision with Intent %s:\n%s", bobIntent, report)
	}

	// The same change naming Bob's conflicting Intent from another branch.
	borrowed := branchCommit("dave-borrow", "Widen retries\n\n"+protocol.CommitTrailer(bobIntent, 1))
	report, _ = check("dave-borrow", borrowed, ci)
	if !strings.Contains(report, "not this pull request's head branch `dave-borrow`") || strings.Contains(report, "Review packet: Intent") {
		t.Errorf("a trailer naming another branch's Intent was trusted:\n%s", report)
	}
	if analyzed && !strings.Contains(report, "with Intent `"+bobIntent.String()+"`") {
		t.Errorf("a trailer naming Bob's Intent hid its collision:\n%s", report)
	}

	// Missing credential, unreachable backend, and a released Intent.
	if report, _ = check("alice-retry", named); !strings.Contains(report, cicheck.SkippedMessage) {
		t.Errorf("report without a credential:\n%s", report)
	}
	if report, _ = check("alice-retry", named, ci, "CONN_SERVER_URL=http://127.0.0.1:1"); !strings.Contains(report, "unreachable") {
		t.Errorf("report with an unreachable backend:\n%s", report)
	}
	aliceParticipant, _ := projects.ResolveParticipant(ctx, alice.HumanID, projectID, "kiro", "")
	must(projects.ReleaseIntent(ctx, alice.HumanID, projectID, aliceParticipant.Data.ParticipantID, aliceIntent))
	if report, _ = check("alice-retry", named, ci); !strings.Contains(report, "which is released") {
		t.Errorf("report for a released Intent:\n%s", report)
	}

	// Expiry and revocation take effect immediately.
	backend.advance(91 * 24 * time.Hour)
	if expired := apiRequest(t, backend, token, http.MethodGet, "/v1/ci/credential", "", ""); expired.status != http.StatusUnauthorized {
		t.Fatalf("an expired credential = %d", expired.status)
	}
	backend.advance(-91 * 24 * time.Hour)
	credentialID := ""
	if err := backend.pool.QueryRow(ctx, `SELECT credential_id::text FROM ci_credentials`).Scan(&credentialID); err != nil {
		t.Fatal(err)
	}
	if code, stdout, stderr := runConn(alice.Token.Reveal(), nil, "ci", "credential", "revoke", credentialID); code != 0 {
		t.Fatalf("revoke exited %d: %s %s", code, stdout, stderr)
	}
	if revoked := apiRequest(t, backend, token, http.MethodGet, "/v1/ci/credential", "", ""); revoked.status != http.StatusUnauthorized {
		t.Fatalf("a revoked credential = %d", revoked.status)
	}
	code, stdout, _ = runConn(alice.Token.Reveal(), nil, "ci", "credential", "list")
	if code != 0 || !strings.Contains(stdout, "revoked") || !strings.Contains(stdout, "last used") || strings.Contains(stdout, token) ||
		count(`SELECT count(*) FROM events WHERE event_type = 'ci_credential.revoked'`) != 1 {
		t.Fatalf("list after revocation exited %d: %s", code, stdout)
	}
}

type apiResponse struct {
	status int
	body   string
}

func apiRequest(t *testing.T, backend *testBackend, token, method, path, body, adapter string) apiResponse {
	t.Helper()
	var reader *strings.Reader
	if body != "" {
		reader = strings.NewReader(body)
	}
	var req *http.Request
	var err error
	if reader != nil {
		req, err = http.NewRequest(method, backend.server.URL+path, reader)
	} else {
		req, err = http.NewRequest(method, backend.server.URL+path, nil)
	}
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if adapter != "" {
		req.Header.Set("X-Conn-Participant-Adapter", adapter)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var buffer bytes.Buffer
	_, _ = buffer.ReadFrom(resp.Body)
	return apiResponse{status: resp.StatusCode, body: fmt.Sprint(buffer.String())}
}
