package checkrun_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
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

	"github.com/Juanqu261/conn/internal/checkrun"
	"github.com/Juanqu261/conn/internal/httpclient"
	"github.com/Juanqu261/conn/internal/protocol"
)

const helperEnv = "CHECKRUN_HELPER"

// TestMain turns the test binary into the check command when helperEnv is
// set, so the scenario needs no shell and no external tool.
func TestMain(m *testing.M) {
	switch os.Getenv(helperEnv) {
	case "":
		os.Exit(m.Run())
	case "pass":
		for _, item := range os.Environ() {
			if strings.HasPrefix(strings.ToUpper(item), "CONN_") {
				fmt.Println("a CONN_ variable reached the check")
				os.Exit(3)
			}
		}
		// More than the local log keeps, so the bound and the full hash show.
		chunk := bytes.Repeat([]byte("x"), 1<<20)
		for range 10 {
			_, _ = os.Stdout.Write(chunk)
		}
		fmt.Fprint(os.Stderr, "no CONN variables\n")
		os.Exit(0)
	case "fail":
		fmt.Println("one test failed")
		os.Exit(7)
	case "sleep":
		fmt.Println("waiting")
		time.Sleep(time.Minute)
	}
	os.Exit(99)
}

// TestEvidenceRun protects requirements 2, 3, and 9: an invalid
// configuration is rejected before anything runs; a check runs as an
// argument array in the repository with its timeout and without CONN_
// variables; its output streams to the terminal, is hashed in full, and is
// kept locally up to 10 MiB; and each acceptance key receives one check_run
// evidence reference with the observation, the commit, and the dirty flag.
func TestEvidenceRun(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is required")
	}
	for name, config := range map[string]string{
		"shell string":       `{"version":1,"checks":{"unit":{"command":"go test ./... && rm -rf /","timeout_seconds":60}}}`,
		"outside repository": `{"version":1,"checks":{"unit":{"command":["go"],"working_directory":"../elsewhere","timeout_seconds":60}}}`,
		"reserved variable":  `{"version":1,"checks":{"unit":{"command":["go"],"timeout_seconds":60,"env":{"CONN_TOKEN":"x"}}}}`,
		"out-of-range value": `{"version":1,"checks":{"unit":{"command":["go"],"timeout_seconds":0}}}`,
	} {
		t.Run("rejects "+name, func(t *testing.T) {
			_, err := checkrun.ParseConfig([]byte(config))
			var configErr *checkrun.ConfigError
			if err == nil || !strings.Contains(err.Error(), "/checks/unit") || !errorsAs(err, &configErr) {
				t.Fatalf("ParseConfig = %v, want a precise configuration error", err)
			}
		})
	}

	fixture := newFixture(t)
	tests := []struct {
		name     string
		check    string
		dirty    bool
		state    string
		exitCode int64
		timedOut bool
	}{
		{"passing run attaches passing evidence", "pass", false, "passing", 0, false},
		{"failing run on uncommitted changes", "fail", true, "failing", 7, false},
		{"timed-out run is failing", "sleep", false, "failing", -1, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if tc.dirty {
				if err := os.WriteFile(filepath.Join(fixture.repo, "README.md"), []byte("changed\n"), 0o644); err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { git(t, fixture.repo, "checkout", "--", "README.md") })
			}
			var terminal bytes.Buffer
			started := time.Now()
			outcome, err := checkrun.RunAndAttach(context.Background(), fixture.client, checkrun.Request{
				Worktree: filepath.Join(fixture.repo, "src"), ProjectID: fixture.projectID, IntentID: fixture.intentID,
				CheckKey: tc.check, Output: &terminal, Environ: append(os.Environ(), "CONN_TOKEN=conn_sess_secret", "Conn_Project=x"),
			})
			if err != nil || outcome.AttachErr != nil {
				t.Fatalf("run = %v, attach = %v", err, outcome.AttachErr)
			}
			if tc.timedOut && time.Since(started) > 20*time.Second {
				t.Fatalf("the timeout did not stop the check promptly: %s", time.Since(started))
			}
			attached := fixture.take()
			if len(attached) != 2 || len(outcome.Evidence) != 2 {
				t.Fatalf("attached %d evidence references, want one per configured acceptance key", len(attached))
			}
			observation := attached[0].Observation
			if observation == nil || attached[0].Kind != protocol.EvidenceKindCheckRun || attached[0].State != tc.state ||
				attached[0].AcceptanceKey != "retry-tests" || attached[1].AcceptanceKey != "retry-smoke" ||
				attached[0].Locator != "conn-check:"+tc.check+"@"+fixture.head[:12] ||
				observation.Runner != "local" || observation.CheckKey != tc.check || observation.Commit != fixture.head ||
				observation.WorktreeDirty != tc.dirty || observation.ExitCode != tc.exitCode || observation.TimedOut != tc.timedOut ||
				observation.ConfigSHA256 != fixture.configSHA || observation.CommandSHA256 != checkrun.CommandSHA256(fixture.config.Checks[tc.check]) {
				t.Fatalf("attached evidence = %+v %+v", attached[0], observation)
			}
			logged, err := os.ReadFile(outcome.Result.LogPath)
			if err != nil || filepath.Base(outcome.Result.LogPath) != outcome.Evidence[0].EvidenceID.String()+".log" {
				t.Fatalf("local log %s: %v", outcome.Result.LogPath, err)
			}
			sum := sha256.Sum256(terminal.Bytes())
			if observation.OutputSHA256 != hex.EncodeToString(sum[:]) {
				t.Fatal("the output hash does not cover the streamed output")
			}
			if tc.check == "pass" {
				if len(logged) != checkrun.MaxLogBytes || !outcome.Result.LogTruncated || !strings.Contains(terminal.String(), "no CONN variables") {
					t.Fatalf("log = %d bytes, truncated %t; terminal tail %q", len(logged), outcome.Result.LogTruncated, terminal.String()[max(0, terminal.Len()-40):])
				}
			} else if !bytes.Equal(logged, terminal.Bytes()) {
				t.Fatalf("log = %q, terminal = %q", logged, terminal.String())
			}
			if strings.Contains(terminal.String(), "conn_sess_secret") {
				t.Fatal("a Conn credential reached the check output")
			}
		})
	}

	t.Run("unknown acceptance key fails before execution", func(t *testing.T) {
		var terminal bytes.Buffer
		_, err := checkrun.RunAndAttach(context.Background(), fixture.client, checkrun.Request{
			Worktree: fixture.repo, ProjectID: fixture.projectID, IntentID: fixture.intentID, CheckKey: "fail",
			AcceptanceKeys: []string{"not-on-the-objective"}, Output: &terminal, Environ: os.Environ(),
		})
		if err == nil || terminal.Len() != 0 || len(fixture.take()) != 0 {
			t.Fatalf("run = %v, output %q", err, terminal.String())
		}
	})
}

func errorsAs(err error, target **checkrun.ConfigError) bool {
	configErr, ok := err.(*checkrun.ConfigError)
	if ok {
		*target = configErr
	}
	return ok
}

type fixture struct {
	repo, head, configSHA string
	config                protocol.ChecksConfig
	projectID, intentID   uuid.UUID
	client                *httpclient.Client
	mu                    sync.Mutex
	attached              []protocol.EvidenceAttachmentInput
}

func (f *fixture) take() []protocol.EvidenceAttachmentInput {
	f.mu.Lock()
	defer f.mu.Unlock()
	taken := f.attached
	f.attached = nil
	return taken
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	f := &fixture{repo: t.TempDir(), projectID: uuid.NewV7(), intentID: uuid.NewV7()}
	helper, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	check := func(mode string, timeout int) protocol.CheckDefinition {
		return protocol.CheckDefinition{Command: []string{helper}, WorkingDirectory: "src", TimeoutSeconds: timeout,
			AcceptanceKeys: []string{"retry-tests", "retry-smoke"}, Env: map[string]string{helperEnv: mode}}
	}
	f.config = protocol.ChecksConfig{Version: 1, Checks: map[string]protocol.CheckDefinition{"pass": check("pass", 60), "fail": check("fail", 60), "sleep": check("sleep", 1)}}
	configBytes, err := json.MarshalIndent(f.config, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(configBytes)
	f.configSHA = hex.EncodeToString(sum[:])
	for path, content := range map[string][]byte{".conn/checks.json": configBytes, "README.md": []byte("fixture\n"), "src/.keep": nil} {
		target := filepath.Join(f.repo, filepath.FromSlash(path))
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(target, content, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	git(t, f.repo, "init", "--quiet", "-b", "main")
	git(t, f.repo, "-c", "core.autocrlf=false", "add", "--all")
	git(t, f.repo, "-c", "user.name=Conn Test", "-c", "user.email=conn@example.invalid", "commit", "--quiet", "-m", "fixture")
	git(t, f.repo, "remote", "add", "origin", "https://github.com/acme/checks.git")
	f.head = git(t, f.repo, "rev-parse", "HEAD")

	at := protocol.NewTimestamp(time.Date(2026, 11, 22, 9, 0, 0, 0, time.UTC))
	humanID, objectiveID, repositoryID, participantID := uuid.NewV7(), uuid.NewV7(), uuid.NewV7(), uuid.NewV7()
	intent := protocol.IntentRevision{IntentID: f.intentID, ProjectID: f.projectID, ParticipantID: participantID, Revision: 1,
		ObjectiveID: objectiveID, ObjectiveRevision: 1, RepositoryID: &repositoryID, SummaryMarkdown: "Verify retries.", CreatedAt: at}
	work := protocol.WorkContext{
		Project:   protocol.WorkContextProject{ProjectID: f.projectID, Name: "Checks", Role: protocol.RoleOwner},
		Reader:    protocol.WorkContextReader{Human: protocol.Human{HumanID: humanID, GitHubUserID: 7, GitHubLogin: "alice"}},
		Selection: protocol.WorkContextSelection{Source: protocol.WorkContextSelectionExplicit},
		Focus: &protocol.WorkContextFocus{Objective: protocol.Objective{
			ObjectiveID: objectiveID, ProjectID: f.projectID, Revision: 1, Title: "Verified retries", OverviewMarkdown: "Verify retries.",
			AccountableHumanID: humanID, CreatedAt: at, KeyPoints: []protocol.KeyPoint{
				{Key: "retry-tests", Kind: protocol.KeyPointAcceptanceCriterion, Statement: "Retry tests pass."},
				{Key: "retry-smoke", Kind: protocol.KeyPointAcceptanceCriterion, Statement: "Retry smoke test passes."},
			}}, OwnCharters: []protocol.OwnCharter{}, SelectedIntent: &intent},
		OwnedActiveIntents: []protocol.IntentRevision{}, RelatedCollisions: []protocol.CollisionAssessment{},
		AcceptanceEvidenceCoverage: []protocol.RadarEvidenceCoverage{}, Updates: []protocol.EventEnvelope{}, KnownUnknowns: []string{}, AsOfEvent: 5,
	}
	repositories := protocol.RepositoryConnectionList{ProjectID: f.projectID, Repositories: []protocol.RepositoryConnection{{
		RepositoryID: repositoryID, ProjectID: f.projectID, HostKind: "github", CanonicalRemote: "github.com/acme/checks",
		AnalysisEnabled: true, CreatedByHumanID: humanID, CreatedAt: at,
	}}}
	projectPath := "/v1/projects/" + f.projectID.String()
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.Header.Get("Authorization"), "conn_sess_secret") {
			t.Error("the check-run token reached the backend unexpectedly")
		}
		switch {
		case r.Method == http.MethodGet && r.URL.Path == projectPath+"/work-context":
			respondDocument(t, w, work)
		case r.Method == http.MethodGet && r.URL.Path == projectPath+"/intents/"+f.intentID.String():
			respondDocument(t, w, protocol.IntentView{IntentRevision: intent, Status: "active", AdvisoryObservations: []string{}})
		case r.Method == http.MethodGet && r.URL.Path == projectPath+"/repositories":
			respondDocument(t, w, repositories)
		case r.Method == http.MethodPost && r.URL.Path == projectPath+"/intents/"+f.intentID.String()+"/evidence":
			raw, _ := io.ReadAll(r.Body)
			input, err := protocol.Decode[protocol.EvidenceAttachmentInput](raw)
			if err != nil {
				t.Errorf("evidence payload: %v", err)
				http.Error(w, "invalid", http.StatusBadRequest)
				return
			}
			f.mu.Lock()
			f.attached = append(f.attached, input)
			f.mu.Unlock()
			w.WriteHeader(http.StatusCreated)
			respondDocument(t, w, protocol.EvidenceReference{
				EvidenceID: uuid.NewV7(), ProjectID: f.projectID, IntentID: &f.intentID, ObjectiveID: objectiveID, ObjectiveRevision: 1,
				AcceptanceKey: input.AcceptanceKey, Kind: input.Kind, State: input.State, Locator: input.Locator,
				ProducerParticipantID: &participantID, Observation: input.Observation, ObservedAt: at,
			})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(backend.Close)
	client, err := httpclient.New(backend.URL, backend.Client())
	if err != nil {
		t.Fatal(err)
	}
	f.client = client
	return f
}

func respondDocument[T protocol.Document](t *testing.T, w http.ResponseWriter, doc T) {
	t.Helper()
	data, err := protocol.Encode(doc)
	if err != nil {
		t.Errorf("encode %s: %v", doc.SchemaID(), err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = fmt.Fprintf(w, `{"data":%s,"as_of_event":5}`, data)
}

func git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	output, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, output)
	}
	return strings.TrimSpace(string(output))
}
