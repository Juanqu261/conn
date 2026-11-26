package main

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"uuid"

	"github.com/jackc/pgx/v5/stdlib"

	"github.com/Juanqu261/conn/internal/application"
	"github.com/Juanqu261/conn/internal/httpapi"
	"github.com/Juanqu261/conn/internal/migrations"
	"github.com/Juanqu261/conn/internal/postgres"
	"github.com/Juanqu261/conn/internal/postgres/postgrestest"
	"github.com/Juanqu261/conn/internal/protocol"
	"github.com/Juanqu261/conn/internal/secret"
)

// TestReviewProofMilestone is the milestone 2 acceptance scenario
// (requirement 10). A Kiro participant declares a change to
// shouldRetryCharge, also changes the undeclared ChargeAttempt export and
// adds .conn/checks.json, commits through the Conn Git hook, records the head,
// and runs observed analysis. A terminal check run attaches observed evidence
// to the Kiro-owned Intent. The packet shows both drift directions, the
// observed evidence, the checks-configuration flag, and the trailer; after a
// later commit the same evidence is stale.
func TestReviewProofMilestone(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("Node.js is required for analysis")
	}
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is required")
	}
	repositoryRoot := repositoryRoot(t)
	analyzerScript := filepath.Join(repositoryRoot, "packages", "typescript-analyzer", "dist", "cli.js")
	if _, err := os.Stat(analyzerScript); err != nil {
		t.Skip("build the analyzer with `pnpm build` to run the review-proof scenario")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
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
		VerifyToken: func(context.Context, secret.String) (application.GitHubIdentity, error) {
			return application.GitHubIdentity{UserID: 21, Login: "alice"}, nil
		},
	}
	projects := &application.Projects{Store: postgres.ProjectStore{Pool: pool}}
	backend := httptest.NewServer(httpapi.NewHandler(httpapi.Config{Ready: func(context.Context) error { return nil }, Auth: auth, Projects: projects, Log: quiet}))
	t.Cleanup(backend.Close)

	alice := login(t, auth, "gh-alice")
	projectID, objectiveID, repositoryID := uuid.NewV7(), uuid.NewV7(), uuid.NewV7()
	if _, err := projects.CreateProject(ctx, alice.HumanID, protocol.ProjectCreate{ProjectID: projectID, Name: "Checkout"}); err != nil {
		t.Fatal(err)
	}
	if _, err := projects.CreateObjectiveRevision(ctx, alice.HumanID, projectID, protocol.ObjectiveRevisionInput{
		ObjectiveID: objectiveID, Key: "checkout", Title: "Reliable checkout retries", OverviewMarkdown: "Retry transport failures safely.", AccountableHumanID: alice.HumanID,
		KeyPoints: []protocol.KeyPoint{{Key: "retry-evidence", Kind: protocol.KeyPointAcceptanceCriterion, Statement: "Retry behavior has passing automated evidence."}},
	}, 0); err != nil {
		t.Fatal(err)
	}
	if _, err := projects.ConnectRepository(ctx, alice.HumanID, projectID, protocol.RepositoryConnectInput{
		RepositoryID: repositoryID, HostKind: "github", CanonicalRemote: preEditRemote, DefaultBranch: "main", AnalysisEnabled: true,
	}); err != nil {
		t.Fatal(err)
	}

	tree := cloneFixture(t, filepath.Join(repositoryRoot, "fixtures", "pre-edit-two-sessions"), "alice-retry")
	env := append(os.Environ(), runMainEnv+"=1", "CONN_SERVER_URL="+backend.URL, "CONN_TOKEN="+alice.Token.Reveal(),
		"CONN_PROJECT="+projectID.String(), "CONN_ANALYZER_PATH="+analyzerScript, "CONN_NODE_PATH="+node,
		"GIT_AUTHOR_NAME=Conn Test", "GIT_AUTHOR_EMAIL=conn@example.invalid", "GIT_COMMITTER_NAME=Conn Test", "GIT_COMMITTER_EMAIL=conn@example.invalid")
	run := func(name string, args ...string) (int, string, string) {
		t.Helper()
		cmd := exec.Command(name, args...)
		cmd.Dir, cmd.Env = tree, env
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
	mustRun := func(name string, args ...string) string {
		t.Helper()
		code, stdout, stderr := run(name, args...)
		if code != 0 {
			t.Fatalf("%s %v exited %d: %s %s", name, args, code, stdout, stderr)
		}
		return strings.TrimSpace(stdout)
	}
	session := startMCP(t, ctx, tree, env)

	// The agent declares a change to shouldRetryCharge only.
	claims := suggested(t, session.call(t, ctx, "suggest_claims", map[string]any{
		"paths": []any{"src/payments/retry.ts"}, "symbols": map[string]any{"src/payments/retry.ts": []any{"shouldRetryCharge"}},
	}), "contract:typescript:src/payments/retry.ts/shouldRetryCharge changes")
	content := func(summary string) map[string]any {
		return map[string]any{"objective_id": objectiveID.String(), "objective_revision": 1, "summary_markdown": summary, "declared_claims": claims}
	}
	intent := publish(t, session.call(t, ctx, "publish_intent", map[string]any{"intent": content("Change retry eligibility for charges.")}), repositoryID, "alice-retry")

	// It changes the declared export, an undeclared export, and the proof
	// definition, then commits through the Conn Git hook.
	retry := filepath.Join(tree, "src", "payments", "retry.ts")
	source, err := os.ReadFile(retry)
	if err != nil {
		t.Fatal(err)
	}
	changed := strings.Replace(string(source), "attempt.attempt < 3", "attempt.attempt < 5", 1)
	changed = strings.Replace(changed, "  attempt: number;\n", "  attempt: number;\n  reason?: string;\n", 1)
	if changed == string(source) || !strings.Contains(changed, "reason?: string") {
		t.Fatal("the fixture edit did not apply")
	}
	checks := `{"version":1,"checks":{"retry-check":{"command":["git","--version"],"timeout_seconds":60,"acceptance_keys":["retry-evidence"]}}}`
	for path, data := range map[string]string{retry: changed, filepath.Join(tree, ".conn", "checks.json"): checks} {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	mustRun(os.Args[0], "git-hook", "install")
	mustRun("git", "-c", "core.autocrlf=false", "add", "--all")
	mustRun("git", "commit", "--quiet", "-m", "Widen retry eligibility")
	head := mustRun("git", "rev-parse", "HEAD")
	if message := mustRun("git", "log", "-1", "--format=%B"); !strings.Contains(message, protocol.CommitTrailer(intent.IntentID, 1)) {
		t.Fatalf("commit message = %q, want the Conn-Intent trailer", message)
	}

	// Finish order: record the head, then analyze that exact revision.
	revise := content("Change retry eligibility for charges.")
	revise["head_revision_ref"] = head
	revised := publish(t, session.call(t, ctx, "revise_intent", map[string]any{"intent_id": intent.IntentID.String(), "expected_revision": 1, "intent": revise}), repositoryID, "alice-retry")
	if revised.Revision != 2 || revised.HeadRevisionRef != head {
		t.Fatalf("revised Intent = %+v", revised)
	}
	session.call(t, ctx, "analyze_intent", map[string]any{"intent_id": intent.IntentID.String(), "expected_intent_revision": 2})

	// A terminal check run attaches observed evidence to the Kiro-owned Intent.
	code, stdout, stderr := run(os.Args[0], "evidence", "run", "retry-check")
	if code != 0 || !strings.Contains(stdout, "git version") || !strings.Contains(stdout, "Attached passing check_run evidence") {
		t.Fatalf("conn evidence run exited %d: %s %s", code, stdout, stderr)
	}

	var packet protocol.ReviewPacket
	decodeData(t, session.call(t, ctx, "read_review_packet", map[string]any{"format": "json"}), &packet)
	subjects := func(items []protocol.DriftSubject) []string {
		var result []string
		for _, item := range items {
			result = append(result, item.NormalizedSubject)
		}
		return result
	}
	acceptance := packet.Acceptance[0]
	if packet.Intent.Revision != 2 || packet.Drift.Analysis.Status == protocol.AnalysisNotAnalyzed ||
		!slices.Contains(subjects(packet.Drift.Matched), "contract:typescript:src/payments/retry.ts/shouldRetryCharge") ||
		!slices.Contains(subjects(packet.Drift.ObservedNotDeclared), "contract:typescript:src/payments/retry.ts/ChargeAttempt") ||
		acceptance.State != "passing" || acceptance.Provenance != protocol.ProvenanceObservedRun || acceptance.Evidence[0].Stale ||
		!packet.Git.ChecksConfigChanged || packet.Git.Head != head {
		t.Fatalf("review packet = %+v", packet)
	}
	if !slices.ContainsFunc(packet.IntegrityNotes, func(note string) bool { return strings.Contains(note, ".conn/checks.json changed") }) {
		t.Fatalf("integrity notes = %v", packet.IntegrityNotes)
	}
	var markdown struct {
		Markdown string `json:"markdown"`
	}
	decodeData(t, session.call(t, ctx, "read_review_packet", map[string]any{}), &markdown)
	for _, want := range []string{"ChargeAttempt", "observed run", "Changed without a declared claim", ".conn/checks.json"} {
		if !strings.Contains(markdown.Markdown, want) {
			t.Errorf("Markdown packet omits %q", want)
		}
	}

	// A later commit makes the observed evidence stale.
	if err := os.WriteFile(filepath.Join(tree, "README.md"), []byte("Changed after the check ran.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	mustRun("git", "commit", "--quiet", "--all", "-m", "Document the change")
	reviewed := mustRun(os.Args[0], "review", "--format", "json")
	stale, err := protocol.Decode[protocol.ReviewPacket]([]byte(reviewed))
	if err != nil {
		t.Fatalf("conn review JSON: %v\n%s", err, reviewed)
	}
	if !stale.Acceptance[0].Evidence[0].Stale || !slices.ContainsFunc(stale.IntegrityNotes, func(note string) bool { return strings.Contains(note, "not the reviewed head") }) {
		t.Fatalf("packet after a later commit = %+v", stale)
	}
	for _, output := range []string{reviewed, markdown.Markdown, session.stderr.String()} {
		if strings.Contains(output, "PRE_EDIT_FIXTURE_SOURCE") || strings.Contains(output, alice.Token.Reveal()) {
			t.Fatal("fixture source or a credential appeared in review output")
		}
	}
}
