package cli_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"uuid"

	"github.com/jackc/pgx/v5/stdlib"

	"github.com/Juanqu261/conn/internal/account"
	"github.com/Juanqu261/conn/internal/application"
	"github.com/Juanqu261/conn/internal/cli"
	"github.com/Juanqu261/conn/internal/credentials"
	"github.com/Juanqu261/conn/internal/httpapi"
	"github.com/Juanqu261/conn/internal/httpclient"
	"github.com/Juanqu261/conn/internal/migrations"
	"github.com/Juanqu261/conn/internal/postgres"
	"github.com/Juanqu261/conn/internal/postgres/postgrestest"
	"github.com/Juanqu261/conn/internal/secret"
)

// TestCLI protects the CLI contract (requirement 6): --json prints the API
// envelope on standard output, failures print only to standard error and
// return stable exit categories, input files are validated before upload,
// help offers no database administration, and no session token ever
// appears in output.
func TestCLI(t *testing.T) {
	ctx := context.Background()
	pool := postgrestest.NewDatabase(t)
	db := stdlib.OpenDBFromPool(pool)
	t.Cleanup(func() { _ = db.Close() })
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	if _, err := migrations.Up(ctx, db, log); err != nil {
		t.Fatal(err)
	}

	auth := &application.Auth{
		Store: postgres.AuthStore{Pool: pool},
		VerifyToken: func(_ context.Context, token secret.String) (application.GitHubIdentity, error) {
			if token.Reveal() == "gh-alice" {
				return application.GitHubIdentity{UserID: 1, Login: "alice"}, nil
			}
			return application.GitHubIdentity{UserID: 2, Login: "bob"}, nil
		},
	}
	var requests atomic.Int64
	handler := httpapi.NewHandler(httpapi.Config{
		Ready:    func(context.Context) error { return nil },
		Auth:     auth,
		Projects: &application.Projects{Store: postgres.ProjectStore{Pool: pool}},
		Log:      log,
	})
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		handler.ServeHTTP(w, r)
	}))
	t.Cleanup(backend.Close)

	session := func(githubToken string) (secret.String, string) {
		grant, err := auth.Exchange(ctx, secret.New(githubToken))
		if err != nil {
			t.Fatal(err)
		}
		return grant.Token, grant.Human.HumanID.String()
	}
	alice, aliceID := session("gh-alice")
	bob, _ := session("gh-bob")
	tokens := []string{alice.Reveal(), bob.Reveal()}

	run := func(token secret.String, stdin string, args ...string) (int, string, string) {
		store := &credentials.Memory{}
		if !token.IsZero() {
			if err := store.Set(token); err != nil {
				t.Fatal(err)
			}
		}
		var stdout, stderr bytes.Buffer
		code := cli.Execute(ctx, args, cli.Streams{In: strings.NewReader(stdin), Out: &stdout, Err: &stderr}, cli.Dependencies{
			Version:   "test",
			LookupEnv: func(string) (string, bool) { return "", false },
			Account: func(serverURL string) (*account.Service, error) {
				server, err := httpclient.New(serverURL, httpclient.NewHTTPClient())
				if err != nil {
					return nil, err
				}
				return &account.Service{Server: server, Store: store, LookupEnv: func(string) (string, bool) { return "", false }}, nil
			},
		})
		return code, stdout.String(), stderr.String()
	}

	dir := t.TempDir()
	file := func(name, content string) string {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		return path
	}
	project, objective := uuid.NewV7().String(), uuid.NewV7().String()
	projectFile := file("project.json", `{"project_id": "`+project+`", "name": "Checkout reliability"}`)
	blankName := file("blank.json", `{"project_id": "`+project+`", "name": " "}`)
	objectiveJSON := `{"objective_id": "` + objective + `", "title": "Reliable checkout",
		"overview_markdown": "Survive provider outages.\u001b]0;pwned\u0007", "accountable_human_id": "` + aliceID + `"}`
	server := []string{"--server", backend.URL}

	steps := []struct {
		name       string
		token      secret.String
		stdin      string
		args       []string
		wantCode   int
		wantOut    string // substring of stdout; empty means stdout must be empty
		wantErr    string // substring of stderr; empty means stderr must be empty
		noRequests bool   // the command must fail before contacting the server
		check      func(t *testing.T, stdout string)
	}{
		{name: "help offers no database administration", args: []string{"--help"},
			wantCode: cli.ExitOK, wantOut: "Exit codes:", check: func(t *testing.T, stdout string) {
				if strings.Contains(strings.ToLower(stdout), "migrat") {
					t.Fatalf("help mentions migrations:\n%s", stdout)
				}
			}},
		{name: "--json prints the API envelope", token: alice, args: append([]string{"project", "create", "--json", "--file", projectFile}, server...),
			wantCode: cli.ExitOK, wantOut: `"as_of_event":1`, check: func(t *testing.T, stdout string) {
				var envelope struct {
					Data struct {
						ProjectID string `json:"project_id"`
					} `json:"data"`
				}
				if err := json.Unmarshal([]byte(stdout), &envelope); err != nil || envelope.Data.ProjectID != project {
					t.Fatalf("stdout is not the project envelope: %v\n%s", err, stdout)
				}
			}},
		{name: "human output describes the result", token: alice, args: append([]string{"project", "create", "--file", projectFile}, server...),
			wantCode: cli.ExitOK, wantOut: "You are its owner."},
		{name: "invalid files fail before upload", token: alice, args: append([]string{"project", "create", "--file", blankName}, server...),
			wantCode: cli.ExitUsage, wantErr: "invalid at /name", noRequests: true},
		{name: "unknown flags are usage errors", token: alice, args: append([]string{"status", "--nope"}, server...),
			wantCode: cli.ExitUsage, wantErr: "unknown flag", noRequests: true},
		{name: "unknown subcommands are usage errors", token: alice, args: []string{"project", "frobnicate"},
			wantCode: cli.ExitUsage, wantErr: "unknown command", noRequests: true},
		{name: "malformed IDs are usage errors", token: alice, args: append([]string{"status", "--project", "not-a-uuid"}, server...),
			wantCode: cli.ExitUsage, wantErr: "not a UUID", noRequests: true},
		{name: "a missing login is an authentication error", args: append([]string{"status", "--project", project}, server...),
			wantCode: cli.ExitAuth, wantErr: "conn login", noRequests: true},
		{name: "a non-member gets an access error", token: bob, args: append([]string{"status", "--project", project}, server...),
			wantCode: cli.ExitAccess, wantErr: "project you belong to"},
		{name: "standard input supplies a file", token: alice, stdin: objectiveJSON,
			args:     append([]string{"objective", "create", "--project", project, "--file", "-"}, server...),
			wantCode: cli.ExitOK, wantOut: "revision 1"},
		{name: "a stale revision is a conflict", token: alice, stdin: objectiveJSON,
			args:     append([]string{"objective", "revise", "--project", project, "--objective", objective, "--expected-revision", "5", "--file", "-"}, server...),
			wantCode: cli.ExitConflict, wantErr: "no longer current"},
		{name: "untrusted text cannot drive the terminal", token: alice,
			args:     append([]string{"objective", "show", "--project", project, "--objective", objective, "--revision", "1"}, server...),
			wantCode: cli.ExitOK, wantOut: "Survive provider outages.\uFFFD]0;pwned\uFFFD"},
		{name: "sync --json lists events", token: alice, args: append([]string{"sync", "--project", project, "--json"}, server...),
			wantCode: cli.ExitOK, wantOut: `"as_of_event":2`, check: func(t *testing.T, stdout string) {
				var envelope struct {
					Data []json.RawMessage `json:"data"`
				}
				if err := json.Unmarshal([]byte(stdout), &envelope); err != nil || len(envelope.Data) != 2 {
					t.Fatalf("stdout is not a two-event page: %v\n%s", err, stdout)
				}
			}},
		{name: "an unreachable server is unavailable", token: alice, args: []string{"status", "--project", project, "--server", "http://127.0.0.1:1"},
			wantCode: cli.ExitUnavailable, wantErr: "could not be reached"},
	}
	for _, step := range steps {
		before := requests.Load()
		code, stdout, stderr := run(step.token, step.stdin, step.args...)
		if code != step.wantCode {
			t.Fatalf("%s: exit %d, want %d\nstdout: %s\nstderr: %s", step.name, code, step.wantCode, stdout, stderr)
		}
		if step.wantOut == "" && stdout != "" || !strings.Contains(stdout, step.wantOut) {
			t.Fatalf("%s: stdout %q, want it to contain %q", step.name, stdout, step.wantOut)
		}
		if step.wantErr == "" && stderr != "" || !strings.Contains(stderr, step.wantErr) {
			t.Fatalf("%s: stderr %q, want it to contain %q", step.name, stderr, step.wantErr)
		}
		if step.noRequests && requests.Load() != before {
			t.Fatalf("%s: the command contacted the server", step.name)
		}
		for _, token := range tokens {
			if strings.Contains(stdout+stderr, token) {
				t.Fatalf("%s: a session token appeared in the output", step.name)
			}
		}
		if step.check != nil {
			step.check(t, stdout)
		}
	}
}
