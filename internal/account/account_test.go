package account_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/stdlib"

	"github.com/Juanqu261/conn/internal/account"
	"github.com/Juanqu261/conn/internal/application"
	"github.com/Juanqu261/conn/internal/auth/github"
	"github.com/Juanqu261/conn/internal/auth/github/githubtest"
	"github.com/Juanqu261/conn/internal/credentials"
	"github.com/Juanqu261/conn/internal/httpapi"
	"github.com/Juanqu261/conn/internal/httpclient"
	"github.com/Juanqu261/conn/internal/migrations"
	"github.com/Juanqu261/conn/internal/postgres"
	"github.com/Juanqu261/conn/internal/postgres/postgrestest"
	"github.com/Juanqu261/conn/internal/secret"
)

// TestSessionLifecycle protects:
// device login yields a Conn session whose token is stored locally and only
// hashed on the server; the GitHub user ID keeps one identity across
// renames; re-login replaces the old session; expiry and logout revoke
// access; and no token reaches the server log.
func TestSessionLifecycle(t *testing.T) {
	ctx := context.Background()
	pool := postgrestest.NewDatabase(t)
	db := stdlib.OpenDBFromPool(pool)
	t.Cleanup(func() { _ = db.Close() })
	if _, err := migrations.Up(ctx, db, slog.New(slog.NewTextHandler(io.Discard, nil))); err != nil {
		t.Fatal(err)
	}

	fake := &githubtest.Server{
		ClientID: "client-1",
		Polls:    []githubtest.Poll{{Error: "authorization_pending"}},
		Token:    "gho_alice_device_token",
		Users:    map[string]github.Identity{"gho_alice_device_token": {UserID: 1001, Login: "alice"}},
	}
	endpoints := fake.Start(t)

	clock := &serverClock{now: time.Date(2026, 11, 17, 9, 0, 0, 0, time.UTC)}
	var serverLog bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&lockedWriter{w: &serverLog}, nil))
	auth := &application.Auth{
		Store:       postgres.AuthStore{Pool: pool},
		VerifyToken: (&github.API{HTTP: http.DefaultClient, Endpoints: endpoints}).Verify,
		Now:         clock.Now,
	}
	backend := httptest.NewServer(httpapi.NewHandler(httpapi.Config{
		Ready:          func(context.Context) error { return nil },
		Auth:           auth,
		GitHubClientID: "client-1",
		Log:            logger,
	}))
	t.Cleanup(backend.Close)

	server, err := httpclient.New(backend.URL, httpclient.NewHTTPClient())
	if err != nil {
		t.Fatal(err)
	}
	store := &credentials.Memory{}
	service := &account.Service{
		Server:    server,
		Store:     store,
		LookupEnv: func(string) (string, bool) { return "", false },
		GitHub:    endpoints,
		HTTP:      http.DefaultClient,
		Sleep:     func(context.Context, time.Duration) error { return nil },
	}

	// Login shows the device code, then stores a Conn token.
	var shownCode string
	first, err := service.Login(ctx, func(a *github.DeviceAuthorization) { shownCode = a.UserCode })
	if err != nil {
		t.Fatal(err)
	}
	if shownCode != "WDJB-MJHT" || first.Human.GitHubLogin != "alice" {
		t.Fatalf("unexpected login: code %q, %+v", shownCode, first.Human)
	}
	if !first.ExpiresAt.Equal(clock.Now().Add(application.SessionLifetime)) {
		t.Fatalf("session expires at %v, want 30 days from now", first.ExpiresAt)
	}
	firstToken, err := store.Get()
	if err != nil || !credentials.ValidToken(firstToken) {
		t.Fatalf("stored credential: %v", err)
	}

	// The server keeps only the token hash and never the GitHub token.
	var hashed int
	hash := application.HashToken(firstToken)
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM sessions WHERE token_sha256 = $1`, hash[:]).Scan(&hashed); err != nil || hashed != 1 {
		t.Fatalf("hashed sessions: %d, %v", hashed, err)
	}
	var persisted string
	if err := pool.QueryRow(ctx, `
		SELECT coalesce(string_agg(row_to_json(s)::text, ''), '') || coalesce((SELECT string_agg(row_to_json(h)::text, '') FROM humans h), '')
		FROM sessions s`).Scan(&persisted); err != nil {
		t.Fatal(err)
	}
	assertNoSecrets(t, "database", persisted, firstToken.Reveal(), fake.Token)

	human, source, err := service.WhoAmI(ctx)
	if err != nil || human.HumanID != first.Human.HumanID || source != credentials.SourceStore {
		t.Fatalf("whoami: %+v from %q, %v", human, source, err)
	}

	// A GitHub rename keeps the Conn identity; logging in again replaces
	// and revokes the previous session.
	fake.Users[fake.Token] = github.Identity{UserID: 1001, Login: "alice-renamed"}
	second, err := service.Login(ctx, func(*github.DeviceAuthorization) {})
	if err != nil {
		t.Fatal(err)
	}
	if second.Human.HumanID != first.Human.HumanID || second.Human.GitHubLogin != "alice-renamed" {
		t.Fatalf("identity changed across rename: %+v", second.Human)
	}
	assertUnauthenticated(t, ctx, server.WithToken(firstToken), "replaced session")
	secondToken, _ := store.Get()

	// A GitHub token Conn cannot verify is refused.
	if _, err := server.Exchange(ctx, secret.New("gho_unknown")); !httpclient.IsCode(err, "github_token_rejected") {
		t.Fatalf("unverified GitHub token: %v", err)
	}

	// Sessions end 30 days after issue, with no renewal.
	clock.Advance(application.SessionLifetime + time.Second)
	assertUnauthenticated(t, ctx, server.WithToken(secondToken), "expired session")
	clock.Advance(-application.SessionLifetime - time.Second)

	// Logout revokes the session on the server and deletes it locally.
	result, err := service.Logout(ctx)
	if err != nil || result.RevokeErr != nil {
		t.Fatalf("logout: %v, revoke: %v", err, result.RevokeErr)
	}
	if _, err := store.Get(); !errors.Is(err, credentials.ErrNotFound) {
		t.Fatalf("local credential survived logout: %v", err)
	}
	assertUnauthenticated(t, ctx, server.WithToken(secondToken), "revoked session")
	if _, _, err := service.WhoAmI(ctx); !errors.Is(err, credentials.ErrNotLoggedIn) {
		t.Fatalf("whoami after logout: %v", err)
	}

	assertNoSecrets(t, "server log", serverLog.String(), firstToken.Reveal(), secondToken.Reveal(), fake.Token)
}

func assertUnauthenticated(t *testing.T, ctx context.Context, client *httpclient.Client, what string) {
	t.Helper()
	if _, err := client.Me(ctx); !httpclient.IsCode(err, "unauthenticated") {
		t.Fatalf("%s still authenticates: %v", what, err)
	}
}

func assertNoSecrets(t *testing.T, where, text string, secrets ...string) {
	t.Helper()
	for _, s := range secrets {
		if strings.Contains(text, s) {
			t.Fatalf("%s contains a credential", where)
		}
	}
}

type serverClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *serverClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *serverClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

type lockedWriter struct {
	mu sync.Mutex
	w  io.Writer
}

func (l *lockedWriter) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.w.Write(p)
}
