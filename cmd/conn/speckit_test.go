package main

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"uuid"

	"github.com/Juanqu261/conn/internal/account"
	"github.com/Juanqu261/conn/internal/analyzer"
	"github.com/Juanqu261/conn/internal/cli"
	"github.com/Juanqu261/conn/internal/protocol"
)

const specKitRemote = "github.com/conn-fixtures/speckit"

// TestSpecKitPlanTimeCollision protects speckit-coordination requirements
// 1, 3, 6.1, 7.1, and 8.2. Two Spec Kit features in one repository name the
// same exported function in their task lists. Feature A's check previews
// nothing and offers to publish; A is imported with fr- and constitution
// key points and published. Feature B's check previews the collision with A
// by name before B publishes, and after B publishes the persisted collision
// equals the preview. B's own Intent is then excluded from its preview. No
// task, plan, or source text reaches Conn.
func TestSpecKitPlanTimeCollision(t *testing.T) {
	node, err := exec.LookPath("node")
	analyzerScript := filepath.Join(repositoryRoot(t), "packages", "typescript-analyzer", "dist", "cli.js")
	if err != nil {
		t.Skip("Node.js is unavailable, so planned analysis cannot suggest claims")
	}
	probe := analyzer.Locate(func(name string) (string, bool) {
		return map[string]string{analyzer.AnalyzerPathEnv: analyzerScript, analyzer.NodePathEnv: node}[name], true
	}, "", "")
	if !probe.Available() {
		t.Skip("the TypeScript analyzer is not built; run pnpm run build:node")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()
	var trafficMu sync.Mutex
	var traffic bytes.Buffer
	backend := newTestBackend(t, ctx, func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			body, _ := io.ReadAll(r.Body)
			trafficMu.Lock()
			traffic.WriteString(r.URL.String() + "\n")
			traffic.Write(body)
			trafficMu.Unlock()
			r.Body = io.NopCloser(bytes.NewReader(body))
			next.ServeHTTP(w, r)
		})
	})
	alice, bob := login(t, backend.auth, "gh-alice"), login(t, backend.auth, "gh-bob")
	projectID, repositoryID := uuid.NewV7(), uuid.NewV7()
	must := func(_ any, err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(backend.projects.CreateProject(ctx, alice.HumanID, protocol.ProjectCreate{ProjectID: projectID, Name: "Shop"}))
	must(backend.projects.AddMember(ctx, alice.HumanID, projectID, bob.HumanID))
	must(backend.projects.ConnectRepository(ctx, alice.HumanID, projectID, protocol.RepositoryConnectInput{RepositoryID: repositoryID, HostKind: "github", CanonicalRemote: specKitRemote, DefaultBranch: "main", AnalysisEnabled: true}))

	fixture := filepath.Join(repositoryRoot(t), "fixtures", "speckit")
	aliceRepo, bobRepo := cloneSpecKit(t, fixture, "001-checkout-retries"), cloneSpecKit(t, fixture, "002-order-limits")
	runAs := func(token string) func(args ...string) (int, string, string) {
		env := map[string]string{
			"CONN_SERVER_URL": backend.server.URL, "CONN_TOKEN": token, "CONN_PROJECT": projectID.String(),
			analyzer.AnalyzerPathEnv: analyzerScript, analyzer.NodePathEnv: node,
		}
		lookup := func(name string) (string, bool) { value, ok := env[name]; return value, ok && value != "" }
		deps := dependencies(nil)
		deps.LookupEnv = lookup
		deps.Account = func(serverURL string) (*account.Service, error) {
			service, err := accountService(serverURL)
			if err == nil {
				service.LookupEnv = lookup
			}
			return service, err
		}
		location := analyzer.Locate(lookup, "", "")
		deps.Analyzer = func() analyzer.Location { return location }
		return func(args ...string) (int, string, string) {
			t.Helper()
			var stdout, stderr bytes.Buffer
			code := cli.Execute(ctx, args, cli.Streams{In: strings.NewReader(""), Out: &stdout, Err: &stderr}, deps)
			return code, stdout.String(), stderr.String()
		}
	}
	asAlice, asBob := runAs(alice.Token.Reveal()), runAs(bob.Token.Reveal())
	check := func(run func(...string) (int, string, string), repo, feature string) protocol.SpecKitCheck {
		t.Helper()
		code, stdout, stderr := run("speckit", "check", "specs/"+feature, "--format", "json", "--worktree", repo)
		if code != 0 {
			t.Fatalf("check %s exited %d: %s", feature, code, stderr)
		}
		document, err := protocol.Decode[protocol.SpecKitCheck]([]byte(stdout))
		if err != nil || len(document.Errors) != 0 {
			t.Fatalf("check %s = %s, %v", feature, stdout, err)
		}
		return document
	}
	retrySubject := "contract:typescript:src/payments/retry.ts/shouldRetryCharge"

	// Feature A previews nothing before anyone publishes, then publishes.
	first := check(asAlice, aliceRepo, "001-checkout-retries")
	if first.Objective != nil || !first.PublishOffered || len(first.Collisions) != 0 || !first.Analysis.Available ||
		!slices.Contains(first.Extraction.Paths, "src/payments/retry.ts") || !hasSubject(first.Candidates, retrySubject) {
		t.Fatalf("feature A check = %+v", first)
	}
	code, stdout, stderr := asAlice("start", filepath.Join(aliceRepo, "specs", "001-checkout-retries"), "--publish", "--worktree", aliceRepo)
	if code != 0 || !strings.Contains(stdout, "Objective: Checkout retries (Spec Kit feature specs/001-checkout-retries, revision 1)") ||
		!strings.Contains(stderr, "Planned paths from tasks.md: 3 (1 new)") || !strings.Contains(stdout, "Published Intent") {
		t.Fatalf("feature A start exited %d:\n%s\n%s", code, stdout, stderr)
	}
	aliceIntent := onlyIntent(t, backend, alice.HumanID, projectID)
	objective, err := backend.projects.ObjectiveRevision(ctx, alice.HumanID, projectID, aliceIntent.ObjectiveID, 1)
	if err != nil || objective.Data.SourceKind() != protocol.SourceKindSpecKitFeature || objective.Data.ImportProvenance.SourceRef != "specs/001-checkout-retries/spec.md" ||
		keyOf(objective.Data.KeyPoints, "fr-001") != protocol.KeyPointAcceptanceCriterion || keyOf(objective.Data.KeyPoints, "constitution-test-first") != protocol.KeyPointConstraint ||
		!strings.HasSuffix(objective.Data.OverviewMarkdown, "Constitution version 2.1.1.") {
		t.Fatalf("feature A objective = %+v, %v", objective.Data, err)
	}

	// Feature B previews the collision with A by name before B publishes.
	if code, _, stderr := asAlice("start", filepath.Join(bobRepo, "specs", "002-order-limits"), "--import-only", "--worktree", bobRepo); code != 0 {
		t.Fatalf("feature B import exited %d: %s", code, stderr)
	}
	second := check(asBob, bobRepo, "002-order-limits")
	if second.Objective == nil || second.Objective.Title != "Order limits" || second.OwnIntent != nil || second.PublishOffered || len(second.Collisions) == 0 {
		t.Fatalf("feature B check = %+v", second)
	}
	previewed := second.Collisions[0]
	if previewed.Counterpart.IntentID != aliceIntent.IntentID || previewed.Counterpart.Summary != "Work on Checkout retries" ||
		previewed.Counterpart.ObjectiveTitle != "Checkout retries" || !slices.Contains(previewed.MatchedSubjects, retrySubject) {
		t.Fatalf("previewed collision = %+v", previewed)
	}
	_, text, _ := asBob("speckit", "check", "specs/002-order-limits", "--worktree", bobRepo)
	if !strings.Contains(text, `with "Work on Checkout retries" (Objective: Checkout retries) on `) || strings.Contains(text, aliceIntent.IntentID.String()) {
		t.Errorf("the text check does not name the counterpart:\n%s", text)
	}

	// After B publishes, the persisted collision equals the preview, and
	// B's own Intent is excluded from B's next preview.
	if code, stdout, stderr := asBob("start", filepath.Join(bobRepo, "specs", "002-order-limits"), "--publish", "--worktree", bobRepo); code != 0 || !strings.Contains(stdout, "Published Intent") {
		t.Fatalf("feature B start exited %d:\n%s\n%s", code, stdout, stderr)
	}
	radar, err := backend.projects.Radar(ctx, bob.HumanID, projectID)
	if err != nil {
		t.Fatal(err)
	}
	persisted := []protocol.CollisionAssessment{}
	for _, collision := range radar.Data.OpenCollisions {
		if collision.LeftIntentID == aliceIntent.IntentID || collision.RightIntentID == aliceIntent.IntentID {
			persisted = append(persisted, collision)
		}
	}
	identity := func(rule, severity string, subjects []string) string {
		sorted := slices.Clone(subjects)
		slices.Sort(sorted)
		return rule + "|" + severity + "|" + strings.Join(sorted, ",")
	}
	var want, got []string
	for _, collision := range second.Collisions {
		want = append(want, identity(collision.RuleID, collision.Severity, collision.MatchedSubjects))
	}
	for _, collision := range persisted {
		got = append(got, identity(collision.RuleID, collision.Severity, collision.MatchedSubjects))
	}
	slices.Sort(want)
	slices.Sort(got)
	if !slices.Equal(want, got) {
		t.Fatalf("persisted collisions %v differ from the preview %v", got, want)
	}
	for _, objective := range radar.Data.Objectives {
		if objective.SourceKind != protocol.SourceKindSpecKitFeature {
			t.Errorf("Radar objective %q source kind = %q", objective.Title, objective.SourceKind)
		}
	}
	again := check(asBob, bobRepo, "002-order-limits")
	if again.OwnIntent == nil || again.Delta == nil || len(again.Delta.Added) != 0 || len(again.Delta.Dropped) != 0 || len(again.Collisions) != len(second.Collisions) {
		t.Fatalf("feature B check after publishing = %+v", again)
	}

	trafficMu.Lock()
	defer trafficMu.Unlock()
	for _, private := range []string{"SPECKIT_FIXTURE_SOURCE_MUST_NOT_BE_TRANSMITTED", "Skip retries for declined orders", "Change the retry rule", "ignored-because-tasks-exist", "Every feature starts as a standalone library. Libraries"} {
		if strings.Contains(traffic.String(), private) {
			t.Errorf("Conn received local Spec Kit or source text %q", private)
		}
	}
}

// cloneSpecKit copies the fixture into a Git worktree on the feature branch
// whose origin is the fixture remote.
func cloneSpecKit(t *testing.T, fixture, branch string) string {
	t.Helper()
	dir := cloneFixture(t, fixture, branch)
	gitOutput(t, dir, "remote", "set-url", "origin", "https://"+specKitRemote+".git")
	return dir
}

func onlyIntent(t *testing.T, backend *testBackend, human, projectID uuid.UUID) protocol.IntentRevision {
	t.Helper()
	intents, err := backend.projects.Intents(context.Background(), human, projectID)
	if err != nil || len(intents.Data.Intents) != 1 {
		t.Fatalf("intents = %+v, %v", intents.Data, err)
	}
	return intents.Data.Intents[0].IntentRevision
}

func hasSubject(claims []protocol.SemanticClaimCandidate, subject string) bool {
	for _, claim := range claims {
		if claim.NormalizedSubject == subject {
			return true
		}
	}
	return false
}

func keyOf(points []protocol.KeyPoint, key string) protocol.KeyPointKind {
	for _, point := range points {
		if point.Key == key {
			return point.Kind
		}
	}
	return ""
}
