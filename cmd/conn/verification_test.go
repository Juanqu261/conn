package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
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

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"

	"github.com/Juanqu261/conn/internal/analyzer"
	"github.com/Juanqu261/conn/internal/application"
	"github.com/Juanqu261/conn/internal/httpapi"
	"github.com/Juanqu261/conn/internal/httpclient"
	"github.com/Juanqu261/conn/internal/migrations"
	"github.com/Juanqu261/conn/internal/pairverify"
	"github.com/Juanqu261/conn/internal/postgres"
	"github.com/Juanqu261/conn/internal/postgres/postgrestest"
	"github.com/Juanqu261/conn/internal/protocol"
	"github.com/Juanqu261/conn/internal/secret"
)

// testBackend is a migrated database behind the real HTTP API, with an
// adjustable clock for expiry.
type testBackend struct {
	pool     *pgxpool.Pool
	auth     *application.Auth
	projects *application.Projects
	server   *httptest.Server
	mu       sync.Mutex
	offset   time.Duration
}

func newTestBackend(t *testing.T, ctx context.Context, wrap func(http.Handler) http.Handler) *testBackend {
	t.Helper()
	pool := postgrestest.NewDatabase(t)
	db := stdlib.OpenDBFromPool(pool)
	t.Cleanup(func() { _ = db.Close() })
	quiet := slog.New(slog.NewTextHandler(io.Discard, nil))
	if _, err := migrations.Up(ctx, db, quiet); err != nil {
		t.Fatal(err)
	}
	b := &testBackend{pool: pool}
	b.auth = &application.Auth{
		Store: postgres.AuthStore{Pool: pool},
		VerifyToken: func(_ context.Context, token secret.String) (application.GitHubIdentity, error) {
			login := strings.TrimPrefix(token.Reveal(), "gh-")
			return application.GitHubIdentity{UserID: int64(len(login)*1000 + int(login[0])), Login: login}, nil
		},
	}
	b.projects = &application.Projects{Store: postgres.ProjectStore{Pool: pool}, Now: func() time.Time {
		b.mu.Lock()
		defer b.mu.Unlock()
		return time.Now().Add(b.offset)
	}}
	var handler http.Handler = httpapi.NewHandler(httpapi.Config{Ready: func(context.Context) error { return nil }, Auth: b.auth, Projects: b.projects, Log: quiet})
	if wrap != nil {
		handler = wrap(handler)
	}
	b.server = httptest.NewServer(handler)
	t.Cleanup(b.server.Close)
	return b
}

func (b *testBackend) advance(d time.Duration) {
	b.mu.Lock()
	b.offset += d
	b.mu.Unlock()
}

// pairRepository builds a repository whose origin/main has the fixture and a
// default-branch .conn/checks.json, plus three feature branches:
// alice-retry requires a second shouldRetryCharge argument and updates its
// caller, bob-orders adds a one-argument call, and carol-limit edits the line
// alice changed. Each branch type-checks alone.
func pairRepository(t *testing.T) (string, map[string]string) {
	t.Helper()
	repo := cloneFixture(t, filepath.Join(repositoryRoot(t), "fixtures", "pre-edit-two-sessions"), "main-work")
	identity := []string{"-c", "user.name=Conn Test", "-c", "user.email=conn@example.invalid", "-c", "core.autocrlf=false"}
	// Only the written paths are staged: a checkout under core.autocrlf
	// would otherwise stage line-ending changes in every file.
	commit := func(message string, files map[string]string) string {
		t.Helper()
		for path, content := range files {
			full := filepath.Join(repo, filepath.FromSlash(path))
			if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		for path := range files {
			gitOutput(t, repo, append(identity, "add", "--", path)...)
		}
		gitOutput(t, repo, append(identity, "commit", "--quiet", "-m", message)...)
		return gitOutput(t, repo, "rev-parse", "HEAD")
	}
	read := func(path string) string {
		data, err := os.ReadFile(filepath.Join(repo, filepath.FromSlash(path)))
		if err != nil {
			t.Fatal(err)
		}
		return strings.ReplaceAll(string(data), "\r\n", "\n")
	}
	// The pair check passes only when no CONN_ variable reaches it, and the
	// setup sleeps for PAIR_SETUP_MS so a run can be cancelled during it.
	defaultChecks := `{"version":1,"checks":{"env-check":{"command":["node","-e","process.exit(Object.keys(process.env).some((k) => k.toUpperCase().startsWith('CONN_')) ? 3 : 0)"],"timeout_seconds":60}},` +
		`"pair_verification":{"setup":{"command":["node","-e","setTimeout(() => {}, Number(process.env.PAIR_SETUP_MS || 0))"],"timeout_seconds":120},"checks":["env-check"]}}`
	gitOutput(t, repo, "checkout", "--quiet", "-B", "main", "main-work")
	main := commit("Add reviewed checks", map[string]string{".conn/checks.json": defaultChecks})
	gitOutput(t, repo, "update-ref", "refs/remotes/origin/main", main)
	gitOutput(t, repo, "symbolic-ref", "refs/remotes/origin/HEAD", "refs/remotes/origin/main")

	retry, checkout := read("src/payments/retry.ts"), read("src/orders/checkout.ts")
	heads := map[string]string{"main": main}
	gitOutput(t, repo, "checkout", "--quiet", "-b", "alice-retry", main)
	aliceRetry := strings.Replace(strings.Replace(retry, "(attempt: ChargeAttempt): boolean", "(attempt: ChargeAttempt, budget: number): boolean", 1), "attempt.attempt < 3", "attempt.attempt < budget", 1)
	// The feature branch also changes the pair check so that it fails; only
	// the default branch's commands may run.
	featureChecks := strings.Replace(defaultChecks, "? 3 : 0", "? 3 : 1", 1)
	heads["alice-retry"] = commit("Budget retries", map[string]string{
		"src/payments/retry.ts":  aliceRetry,
		"src/orders/checkout.ts": strings.Replace(checkout, "shouldRetryCharge(attempt)", "shouldRetryCharge(attempt, 3)", 1),
		".conn/checks.json":      featureChecks,
	})
	gitOutput(t, repo, "checkout", "--quiet", "-b", "bob-orders", main)
	heads["bob-orders"] = commit("Retry refunds", map[string]string{
		"src/orders/refund.ts": "import { type ChargeAttempt, shouldRetryCharge } from \"../payments/retry.js\";\n\nexport function retryRefund(attempt: ChargeAttempt): boolean {\n  return shouldRetryCharge(attempt);\n}\n",
	})
	gitOutput(t, repo, "checkout", "--quiet", "-b", "carol-limit", main)
	heads["carol-limit"] = commit("Raise the retry limit", map[string]string{"src/payments/retry.ts": strings.Replace(retry, "attempt.attempt < 3", "attempt.attempt < 5", 1)})
	gitOutput(t, repo, "checkout", "--quiet", "main")
	if aliceRetry == retry {
		t.Fatal("the fixture edit did not apply")
	}
	return repo, heads
}

// TestPairVerificationMilestone is the milestone 3 acceptance scenario for
// pair verification (requirements 1, 2, and 14). Two branches type-check
// alone, but their merge introduces TS2554; conn verify-pair records a
// failing combination check that names it. The pair check comes from the
// default branch even though a feature branch changed it, and it passes only
// because no CONN_ variable reaches it. A member's Participant records the
// check, which leaves the collision state unchanged and appears in conflict
// context and Radar. A merge conflict runs no checks, and a cancelled run
// removes its temporary worktrees.
func TestPairVerificationMilestone(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("Node.js is required for pair verification")
	}
	analyzerScript := filepath.Join(repositoryRoot(t), "packages", "typescript-analyzer", "dist", "cli.js")
	if _, err := os.Stat(analyzerScript); err != nil {
		t.Skip("build the analyzer with `pnpm build` to run the pair-verification scenario")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 240*time.Second)
	defer cancel()
	backend := newTestBackend(t, ctx, nil)
	projects := backend.projects
	alice, bob, outsider := login(t, backend.auth, "gh-alice"), login(t, backend.auth, "gh-bob"), login(t, backend.auth, "gh-oscar")
	projectID, objectiveID, repositoryID := uuid.NewV7(), uuid.NewV7(), uuid.NewV7()
	must := func(_ any, err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(projects.CreateProject(ctx, alice.HumanID, protocol.ProjectCreate{ProjectID: projectID, Name: "Checkout"}))
	must(projects.AddMember(ctx, alice.HumanID, projectID, bob.HumanID))
	must(projects.CreateObjectiveRevision(ctx, alice.HumanID, projectID, protocol.ObjectiveRevisionInput{ObjectiveID: objectiveID, Title: "Reliable retries", OverviewMarkdown: "Retry safely.", AccountableHumanID: alice.HumanID}, 0))
	must(projects.ConnectRepository(ctx, alice.HumanID, projectID, protocol.RepositoryConnectInput{RepositoryID: repositoryID, HostKind: "github", CanonicalRemote: preEditRemote, DefaultBranch: "main", AnalysisEnabled: true}))

	repo, heads := pairRepository(t)
	subject := "contract:typescript:src/payments/retry.ts/shouldRetryCharge"
	symbol := "symbol:typescript:src/payments/retry.ts/shouldRetryCharge"
	publish := func(human uuid.UUID, branch string, claims ...protocol.SemanticClaimCandidate) uuid.UUID {
		t.Helper()
		participant, err := projects.ResolveParticipant(ctx, human, projectID, "cli", "")
		if err != nil {
			t.Fatal(err)
		}
		id := uuid.NewV7()
		must(projects.PublishIntent(ctx, human, projectID, participant.Data.ParticipantID, protocol.IntentInput{
			IntentID: id, ObjectiveID: objectiveID, ObjectiveRevision: 1, RepositoryID: &repositoryID, BranchRef: branch,
			BaseRevisionRef: heads["main"], HeadRevisionRef: heads[branch], SummaryMarkdown: "Change " + branch + ".", DeclaredClaims: claims,
		}))
		return id
	}
	declared := func(kind, subject, relation string) protocol.SemanticClaimCandidate {
		return protocol.SemanticClaimCandidate{SubjectKind: kind, NormalizedSubject: subject, DisplayName: "shouldRetryCharge", Relation: relation, ProvenanceKind: "declared"}
	}
	aliceIntent := publish(alice.HumanID, "alice-retry", declared("contract", subject, "changes"), declared("code_symbol", symbol, "changes"))
	bobIntent := publish(bob.HumanID, "bob-orders", declared("contract", subject, "consumes"))
	carolIntent := publish(bob.HumanID, "carol-limit", declared("code_symbol", symbol, "changes"))
	radar, err := projects.Radar(ctx, alice.HumanID, projectID)
	if err != nil {
		t.Fatal(err)
	}
	collisionBetween := func(one, two uuid.UUID, rule string) protocol.CollisionAssessment {
		t.Helper()
		for _, collision := range radar.Data.OpenCollisions {
			if collision.RuleID == rule && ((collision.LeftIntentID == one && collision.RightIntentID == two) || (collision.LeftIntentID == two && collision.RightIntentID == one)) {
				return collision
			}
		}
		t.Fatalf("no %s collision between %s and %s in %+v", rule, one, two, radar.Data.OpenCollisions)
		return protocol.CollisionAssessment{}
	}
	interaction := collisionBetween(aliceIntent, bobIntent, "contract_producer_consumer")
	conflict := collisionBetween(aliceIntent, carolIntent, "shared_exported_symbol")

	// Bob, a member, verifies the pair from his terminal.
	env := append(os.Environ(), runMainEnv+"=1", "CONN_SERVER_URL="+backend.server.URL, "CONN_TOKEN="+bob.Token.Reveal(),
		"CONN_PROJECT="+projectID.String(), "CONN_ANALYZER_PATH="+analyzerScript, "CONN_NODE_PATH="+node, "CONN_PAIR_SECRET=must-not-leak")
	cmd := exec.CommandContext(ctx, os.Args[0], "verify-pair", interaction.CollisionID.String(), "--yes")
	cmd.Dir, cmd.Env = repo, env
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("conn verify-pair: %v\n%s\n%s", err, stdout.String(), stderr.String())
	}
	output := stdout.String()
	for _, want := range []string{pairverify.Warning, "origin/main", "Combination outcome: failing", "TS2554 src/orders/refund.ts", "Pair check env-check: passing", "The collision state is unchanged"} {
		if !strings.Contains(output, want) {
			t.Errorf("verify-pair output omits %q:\n%s", want, output)
		}
	}

	pack, err := projects.ConflictContext(ctx, alice.HumanID, projectID, interaction.CollisionID)
	if err != nil {
		t.Fatal(err)
	}
	if len(pack.Data.CombinationChecks) != 1 || pack.Data.Collision.State != protocol.CollisionOpen || pack.Data.Collision.LatestCombinationOutcome != protocol.CombinationFailing {
		t.Fatalf("conflict context = %+v", pack.Data)
	}
	check := pack.Data.CombinationChecks[0]
	if check.Outcome != protocol.CombinationFailing || check.Diagnostics.NewCount != 1 || check.Diagnostics.New[0] != (protocol.CombinationDiagnostic{Code: "TS2554", Path: "src/orders/refund.ts"}) ||
		check.ConfigSource == nil || check.ConfigSource.Ref != "origin/main" || len(check.Checks) != 1 || check.Checks[0].State != protocol.EvidencePassing ||
		check.Setup == nil || check.Left.Commit == "" || !check.Left.HeadMatchesIntent || check.Runner != protocol.CombinationRunnerLocal {
		t.Fatalf("combination check = %+v", check)
	}
	if radar, err := projects.Radar(ctx, bob.HumanID, projectID); err != nil || outcomeIn(radar.Data.OpenCollisions, interaction.CollisionID) != protocol.CombinationFailing {
		t.Fatalf("radar outcome = %v, %v", radar.Data.OpenCollisions, err)
	}
	if lines := gitOutput(t, repo, "worktree", "list", "--porcelain"); strings.Count(lines, "worktree ") != 1 {
		t.Fatalf("temporary worktrees remain:\n%s", lines)
	}

	// Only members record, and only for the collision's own Intents.
	outsiderParticipant := uuid.NewV7()
	if _, err := projects.RecordCombinationCheck(ctx, outsider.HumanID, projectID, outsiderParticipant, interaction.CollisionID, combinationInput(check)); err == nil {
		t.Fatal("a non-member recorded a combination check")
	}
	bobParticipant, _ := projects.ResolveParticipant(ctx, bob.HumanID, projectID, "cli", "")
	wrong := combinationInput(check)
	wrong.Right.IntentID = carolIntent
	if _, err := projects.RecordCombinationCheck(ctx, bob.HumanID, projectID, bobParticipant.Data.ParticipantID, interaction.CollisionID, wrong); !errors.Is(err, application.ErrInvalid) {
		t.Fatalf("a combination check for another Intent = %v, want invalid", err)
	}

	// The conflicting pair records a merge conflict and runs nothing.
	client := httpClientFor(t, backend, bob.Token)
	location := analyzer.Locate(func(name string) (string, bool) {
		return map[string]string{analyzer.AnalyzerPathEnv: analyzerScript, analyzer.NodePathEnv: node}[name], name == analyzer.AnalyzerPathEnv || name == analyzer.NodePathEnv
	}, "", "")
	request := func(collisionID uuid.UUID) pairverify.Request {
		return pairverify.Request{
			Worktree: repo, ProjectID: projectID, CollisionID: collisionID, Environ: os.Environ(), Output: io.Discard,
			Analyzer: location.Config, AnalyzerAvailable: location.Available(), Confirm: func(pairverify.Plan) (bool, error) { return true, nil },
		}
	}
	merged, err := pairverify.Run(ctx, client, request(conflict.CollisionID))
	if err != nil || merged.RecordErr != nil || merged.Recorded == nil {
		t.Fatalf("merge-conflict run = %+v, %v", merged, err)
	}
	if merged.Recorded.Outcome != protocol.CombinationMergeConflict || len(merged.Recorded.ConflictedPaths) != 1 || merged.Recorded.ConflictedPaths[0] != "src/payments/retry.ts" ||
		len(merged.Recorded.Checks) != 0 || merged.Recorded.Setup != nil {
		t.Fatalf("merge-conflict record = %+v", merged.Recorded)
	}

	// Cancelling during setup removes both worktrees and records nothing.
	runCtx, stop := context.WithCancel(ctx)
	defer stop()
	slow := request(interaction.CollisionID)
	slow.Environ = append(os.Environ(), "PAIR_SETUP_MS=60000")
	slow.Output = cancelOn{text: "Running setup", cancel: stop}
	if _, err := pairverify.Run(runCtx, client, slow); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled run = %v, want context.Canceled", err)
	}
	if lines := gitOutput(t, repo, "worktree", "list", "--porcelain"); strings.Count(lines, "worktree ") != 1 {
		t.Fatalf("a cancelled run left worktrees:\n%s", lines)
	}
	if pack, err := projects.ConflictContext(ctx, alice.HumanID, projectID, interaction.CollisionID); err != nil || len(pack.Data.CombinationChecks) != 1 {
		t.Fatalf("a cancelled run recorded a check: %+v, %v", pack.Data.CombinationChecks, err)
	}
	if strings.Contains(output+stderr.String(), "must-not-leak") {
		t.Fatal("a CONN_ value appeared in verify-pair output")
	}
}

// cancelOn cancels a run when its output reaches a step.
type cancelOn struct {
	text   string
	cancel context.CancelFunc
}

func (c cancelOn) Write(data []byte) (int, error) {
	if strings.Contains(string(data), c.text) {
		c.cancel()
	}
	return len(data), nil
}

func outcomeIn(collisions []protocol.CollisionAssessment, id uuid.UUID) string {
	for _, collision := range collisions {
		if collision.CollisionID == id {
			return collision.LatestCombinationOutcome
		}
	}
	return ""
}

func combinationInput(check protocol.CombinationCheck) protocol.CombinationCheckInput {
	return protocol.CombinationCheckInput{
		Left: check.Left, Right: check.Right, MergeResult: check.MergeResult, ConflictedPaths: check.ConflictedPaths,
		Diagnostics: check.Diagnostics, Setup: check.Setup, Checks: check.Checks, Outcome: check.Outcome,
		ConfigSource: check.ConfigSource, Runner: check.Runner,
	}
}

func httpClientFor(t *testing.T, backend *testBackend, token secret.String) *httpclient.Client {
	t.Helper()
	client, err := httpclient.New(backend.server.URL, httpclient.NewHTTPClient())
	if err != nil {
		t.Fatal(err)
	}
	return client.WithToken(token)
}
