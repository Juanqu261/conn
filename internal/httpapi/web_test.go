package httpapi_test

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5/stdlib"

	"github.com/Juanqu261/conn/internal/application"
	"github.com/Juanqu261/conn/internal/eventstream"
	"github.com/Juanqu261/conn/internal/httpapi"
	"github.com/Juanqu261/conn/internal/migrations"
	"github.com/Juanqu261/conn/internal/postgres"
	"github.com/Juanqu261/conn/internal/postgres/postgrestest"
	"github.com/Juanqu261/conn/internal/secret"
)

const labOrigin = "https://lab.conn.test"

// fakeWebFlow is GitHub's web flow: one code per user, bound to the PKCE
// challenge GitHub received at authorization.
type fakeWebFlow struct {
	mu         sync.Mutex
	challenges map[string]string // state -> code challenge
	codes      map[string]string // code -> GitHub token
	issued     map[string]string // code -> state it was issued for
}

func (f *fakeWebFlow) AuthorizeURL(state, codeChallenge, redirectURI string) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.challenges[state] = codeChallenge
	return "https://github.test/login/oauth/authorize?state=" + url.QueryEscape(state) +
		"&redirect_uri=" + url.QueryEscape(redirectURI)
}

func (f *fakeWebFlow) ExchangeCode(_ context.Context, code string, verifier secret.String, redirectURI string) (secret.String, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	sum := sha256.Sum256([]byte(verifier.Reveal()))
	if redirectURI != labOrigin+"/v1/auth/github/web/callback" ||
		f.challenges[f.issued[code]] != base64.RawURLEncoding.EncodeToString(sum[:]) {
		return secret.String{}, fmt.Errorf("fake GitHub: verifier or redirect URI mismatch")
	}
	return secret.New(f.codes[code]), nil
}

// TestWebLab protects Conn Lab's public guarantees (decision 0013): GitHub
// web sign-in bound to the browser by state and PKCE; browser sessions only
// in the HttpOnly cookie and never interchangeable with bearer sessions; a
// read-only browser surface in this version; sign-out that a cross-site
// request cannot trigger; Lab routes that reveal nothing before sign-in;
// and an event stream that resumes after Last-Event-ID with no gap.
func TestWebLab(t *testing.T) {
	ctx := context.Background()
	pool := postgrestest.NewDatabase(t)
	db := stdlib.OpenDBFromPool(pool)
	t.Cleanup(func() { _ = db.Close() })
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	if _, err := migrations.Up(ctx, db, log); err != nil {
		t.Fatal(err)
	}

	githubUsers := map[string]application.GitHubIdentity{
		"gh-alice": {UserID: 1, Login: "alice"},
		"gh-bob":   {UserID: 2, Login: "bob"},
	}
	verify := func(_ context.Context, token secret.String) (application.GitHubIdentity, error) {
		if identity, ok := githubUsers[token.Reveal()]; ok {
			return identity, nil
		}
		return application.GitHubIdentity{}, application.ErrGitHubTokenRejected
	}
	flow := &fakeWebFlow{challenges: map[string]string{}, codes: map[string]string{}, issued: map[string]string{}}
	publicURL, err := httpapi.ParsePublicURL(labOrigin)
	if err != nil {
		t.Fatal(err)
	}
	hub := eventstream.New(2, 10)
	listenCtx, stopListening := context.WithCancel(ctx)
	t.Cleanup(stopListening)
	go postgres.ListenEvents(listenCtx, pool, hub, log)

	newServer := func(backlog int64) *httptest.Server {
		server := httptest.NewServer(httpapi.NewHandler(httpapi.Config{
			Ready:    func(context.Context) error { return nil },
			Auth:     &application.Auth{Store: postgres.AuthStore{Pool: pool}, VerifyToken: verify},
			Projects: &application.Projects{Store: postgres.ProjectStore{Pool: pool}},
			Web: httpapi.WebConfig{
				PublicURL:     publicURL,
				GitHub:        flow,
				Assets:        fstest.MapFS{"index.html": {Data: []byte("<!doctype html><title>Conn Lab</title>")}},
				Streams:       hub,
				StreamBacklog: backlog,
			},
			Log: log,
		}))
		t.Cleanup(server.Close)
		return server
	}
	server := newServer(0)
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}

	type request struct {
		method, path, bearer, cookie, origin, csrf, body string
		extraCookies                                     []*http.Cookie
	}
	do := func(t *testing.T, srv *httptest.Server, req request) *http.Response {
		t.Helper()
		var body io.Reader
		if req.body != "" {
			body = strings.NewReader(req.body)
		}
		r, err := http.NewRequest(req.method, srv.URL+req.path, body)
		if err != nil {
			t.Fatal(err)
		}
		if req.body != "" {
			r.Header.Set("Content-Type", "application/json")
		}
		if req.bearer != "" {
			r.Header.Set("Authorization", "Bearer "+req.bearer)
		}
		if req.cookie != "" {
			r.AddCookie(&http.Cookie{Name: "__Host-conn_web", Value: req.cookie})
		}
		for _, c := range req.extraCookies {
			r.AddCookie(c)
		}
		if req.origin != "" {
			r.Header.Set("Origin", req.origin)
		}
		if req.csrf != "" {
			r.Header.Set("X-Conn-CSRF", req.csrf)
		}
		resp, err := client.Do(r)
		if err != nil {
			t.Fatal(err)
		}
		return resp
	}
	status := func(t *testing.T, req request) (int, string) {
		t.Helper()
		resp := do(t, server, req)
		defer resp.Body.Close()
		var envelope struct {
			Error struct {
				Code string `json:"code"`
			} `json:"error"`
		}
		_ = json.NewDecoder(resp.Body).Decode(&envelope)
		return resp.StatusCode, envelope.Error.Code
	}
	cookieNamed := func(resp *http.Response, name string) *http.Cookie {
		for _, c := range resp.Cookies() {
			if c.Name == name {
				return c
			}
		}
		return nil
	}

	// signIn runs the browser flow and returns the session cookie value.
	signIn := func(t *testing.T, githubToken, returnTo, wantRedirect string) string {
		t.Helper()
		start := do(t, server, request{method: "GET", path: "/v1/auth/github/web/start?return_to=" + url.QueryEscape(returnTo)})
		start.Body.Close()
		if start.StatusCode != http.StatusFound {
			t.Fatalf("start: status %d", start.StatusCode)
		}
		authorize, _ := url.Parse(start.Header.Get("Location"))
		state := authorize.Query().Get("state")
		oauth := cookieNamed(start, "__Host-conn_oauth")
		if state == "" || oauth == nil || !oauth.HttpOnly || !oauth.Secure {
			t.Fatalf("start must bind a state to an HttpOnly, Secure cookie: %+v", oauth)
		}
		code := "code-" + githubToken
		flow.mu.Lock()
		flow.codes[code], flow.issued[code] = githubToken, state
		flow.mu.Unlock()
		callback := do(t, server, request{method: "GET",
			path:         "/v1/auth/github/web/callback?state=" + url.QueryEscape(state) + "&code=" + code,
			extraCookies: []*http.Cookie{oauth}})
		callback.Body.Close()
		session := cookieNamed(callback, "__Host-conn_web")
		if callback.StatusCode != http.StatusFound || session == nil {
			t.Fatalf("callback: status %d, location %q", callback.StatusCode, callback.Header.Get("Location"))
		}
		if !session.HttpOnly || !session.Secure || session.SameSite != http.SameSiteLaxMode || session.Path != "/" {
			t.Fatalf("session cookie attributes: %+v", session)
		}
		if got := callback.Header.Get("Location"); got != wantRedirect {
			t.Fatalf("callback redirected to %q, want %q", got, wantRedirect)
		}
		return session.Value
	}
	cliLogin := func(t *testing.T, githubToken string) (string, string) {
		t.Helper()
		resp := do(t, server, request{method: "POST", path: "/v1/auth/github/exchange", bearer: githubToken})
		defer resp.Body.Close()
		var envelope struct {
			Data struct {
				Token string `json:"token"`
				Human struct {
					HumanID string `json:"human_id"`
				} `json:"human"`
			} `json:"data"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil || resp.StatusCode != http.StatusCreated {
			t.Fatalf("CLI login: %d %v", resp.StatusCode, err)
		}
		return envelope.Data.Token, envelope.Data.Human.HumanID
	}
	csrfOf := func(t *testing.T, cookie string) string {
		t.Helper()
		resp := do(t, server, request{method: "GET", path: "/v1/web/session", cookie: cookie})
		defer resp.Body.Close()
		var envelope struct {
			Data struct {
				CSRFToken string `json:"csrf_token"`
				Human     struct {
					GitHubLogin string `json:"github_login"`
				} `json:"human"`
			} `json:"data"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil || resp.StatusCode != http.StatusOK {
			t.Fatalf("web session: %d %v", resp.StatusCode, err)
		}
		return envelope.Data.CSRFToken
	}

	aliceWeb := signIn(t, "gh-alice", "/p/checkout~1", "/p/checkout~1")
	aliceCLI, _ := cliLogin(t, "gh-alice")
	_, bobID := cliLogin(t, "gh-bob")
	aliceCSRF := csrfOf(t, aliceWeb)

	t.Run("sign-in refuses a callback whose state the browser did not start", func(t *testing.T) {
		resp := do(t, server, request{method: "GET", path: "/v1/auth/github/web/callback?state=forged&code=code-gh-alice"})
		resp.Body.Close()
		if resp.Header.Get("Location") != "/sign-in?error=state_mismatch" || cookieNamed(resp, "__Host-conn_web") != nil {
			t.Fatalf("got %d %q", resp.StatusCode, resp.Header.Get("Location"))
		}
	})

	t.Run("sign-in never returns to another origin or the API", func(t *testing.T) {
		for _, returnTo := range []string{"//evil.test/x", "https://evil.test/", "/v1/projects", `/\evil.test`} {
			signIn(t, "gh-alice", returnTo, "/")
		}
	})

	cases := []struct {
		name       string
		req        request
		wantStatus int
		wantCode   string
	}{
		{"the browser cookie reads Project routes", request{method: "GET", path: "/v1/projects", cookie: aliceWeb}, 200, ""},
		{"a browser token is refused as a bearer credential", request{method: "GET", path: "/v1/projects", bearer: aliceWeb}, 401, "unauthenticated"},
		{"a CLI token is refused as the browser cookie", request{method: "GET", path: "/v1/projects", cookie: aliceCLI}, 401, "unauthenticated"},
		{"a CLI token is refused by the web session route", request{method: "GET", path: "/v1/web/session", cookie: aliceCLI}, 401, "unauthenticated"},
		{"the browser surface is read-only", request{method: "POST", path: "/v1/projects", cookie: aliceWeb, origin: labOrigin, csrf: aliceCSRF,
			body: fmt.Sprintf(`{"project_id": %q, "name": "From the browser"}`, uuid.NewV7())}, 403, "browser_read_only"},
		{"sign-out without Origin has no effect", request{method: "DELETE", path: "/v1/web/session", cookie: aliceWeb, csrf: aliceCSRF}, 403, "csrf_refused"},
		{"sign-out from another origin has no effect", request{method: "DELETE", path: "/v1/web/session", cookie: aliceWeb, origin: "https://evil.test", csrf: aliceCSRF}, 403, "csrf_refused"},
		{"sign-out without the CSRF token has no effect", request{method: "DELETE", path: "/v1/web/session", cookie: aliceWeb, origin: labOrigin}, 403, "csrf_refused"},
		{"sign-out with another session's CSRF token has no effect", request{method: "DELETE", path: "/v1/web/session", cookie: aliceWeb, origin: labOrigin,
			csrf: application.CSRFToken(secret.New(aliceCLI))}, 403, "csrf_refused"},
		{"a refused sign-out left the session valid", request{method: "GET", path: "/v1/web/session", cookie: aliceWeb}, 200, ""},
		{"the event stream serves browsers only", request{method: "GET", path: "/v1/projects/" + uuid.NewV7().String() + "/events/stream", bearer: aliceCLI}, 403, "browser_only"},
		{"the event stream authorizes membership", request{method: "GET", path: "/v1/projects/" + uuid.NewV7().String() + "/events/stream", cookie: aliceWeb}, 404, "not_found"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gotStatus, gotCode := status(t, tc.req)
			if gotStatus != tc.wantStatus || gotCode != tc.wantCode {
				t.Fatalf("got %d %q, want %d %q", gotStatus, gotCode, tc.wantStatus, tc.wantCode)
			}
		})
	}

	t.Run("a Lab route before sign-in serves only the static page", func(t *testing.T) {
		resp := do(t, server, request{method: "GET", path: "/p/checkout~1"})
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		if resp.StatusCode != 200 || string(body) != "<!doctype html><title>Conn Lab</title>" ||
			!strings.Contains(resp.Header.Get("Content-Security-Policy"), "default-src 'self'") {
			t.Fatalf("got %d %q, CSP %q", resp.StatusCode, body, resp.Header.Get("Content-Security-Policy"))
		}
	})

	// The stream test needs a Project with events: alice creates it from the
	// CLI (event 1) and later adds bob (event 2) while a stream is open.
	project := uuid.NewV7()
	createProject := do(t, server, request{method: "POST", path: "/v1/projects", bearer: aliceCLI,
		body: fmt.Sprintf(`{"project_id": %q, "name": "Checkout reliability"}`, project)})
	createProject.Body.Close()
	if createProject.StatusCode != http.StatusCreated {
		t.Fatalf("create project: %d", createProject.StatusCode)
	}
	streamPath := "/v1/projects/" + project.String() + "/events/stream"

	type sseMessage struct{ id, event, data string }
	open := func(t *testing.T, srv *httptest.Server, cookie, path, lastEventID string) (*http.Response, <-chan sseMessage) {
		t.Helper()
		r, err := http.NewRequest("GET", srv.URL+path, nil)
		if err != nil {
			t.Fatal(err)
		}
		r.AddCookie(&http.Cookie{Name: "__Host-conn_web", Value: cookie})
		if lastEventID != "" {
			r.Header.Set("Last-Event-ID", lastEventID)
		}
		resp, err := http.DefaultClient.Do(r)
		if err != nil {
			t.Fatal(err)
		}
		messages := make(chan sseMessage, 16)
		go func() {
			defer close(messages)
			scanner := bufio.NewScanner(resp.Body)
			var msg sseMessage
			for scanner.Scan() {
				line := scanner.Text()
				switch {
				case line == "":
					if msg.data != "" {
						messages <- msg
					}
					msg = sseMessage{}
				case strings.HasPrefix(line, "id: "):
					msg.id = strings.TrimPrefix(line, "id: ")
				case strings.HasPrefix(line, "event: "):
					msg.event = strings.TrimPrefix(line, "event: ")
				case strings.HasPrefix(line, "data: "):
					msg.data = strings.TrimPrefix(line, "data: ")
				}
			}
		}()
		return resp, messages
	}
	next := func(t *testing.T, messages <-chan sseMessage) sseMessage {
		t.Helper()
		select {
		case msg, ok := <-messages:
			if !ok {
				t.Fatal("stream closed")
			}
			return msg
		case <-time.After(10 * time.Second):
			t.Fatal("no stream message within 10 s")
		}
		return sseMessage{}
	}
	eventType := func(t *testing.T, msg sseMessage) string {
		t.Helper()
		var event struct {
			EventType string `json:"event_type"`
		}
		if err := json.Unmarshal([]byte(msg.data), &event); err != nil {
			t.Fatal(err)
		}
		return event.EventType
	}

	t.Run("the stream replays, then delivers a committed event live", func(t *testing.T) {
		resp, messages := open(t, server, aliceWeb, streamPath+"?after=0", "")
		defer resp.Body.Close()
		if resp.StatusCode != 200 || !strings.HasPrefix(resp.Header.Get("Content-Type"), "text/event-stream") {
			t.Fatalf("got %d %q", resp.StatusCode, resp.Header.Get("Content-Type"))
		}
		if msg := next(t, messages); msg.id != "1" || eventType(t, msg) != "project.created" {
			t.Fatalf("first message %+v", msg)
		}
		add := do(t, server, request{method: "POST", path: "/v1/projects/" + project.String() + "/members", bearer: aliceCLI,
			body: fmt.Sprintf(`{"human_id": %q}`, bobID)})
		add.Body.Close()
		if add.StatusCode != http.StatusCreated {
			t.Fatalf("add member: %d", add.StatusCode)
		}
		if msg := next(t, messages); msg.id != "2" || eventType(t, msg) != "project.member_added" {
			t.Fatalf("live message %+v", msg)
		}
	})

	t.Run("a reconnect resumes after Last-Event-ID without a gap or repeat", func(t *testing.T) {
		resp, messages := open(t, server, aliceWeb, streamPath+"?after=0", "1")
		defer resp.Body.Close()
		if msg := next(t, messages); msg.id != "2" {
			t.Fatalf("resumed at %+v, want event 2", msg)
		}
	})

	t.Run("one browser session holds a bounded number of streams", func(t *testing.T) {
		// A fresh session, so streams of earlier subtests that the server
		// has not yet seen close cannot hold its slots.
		tab := signIn(t, "gh-alice", "/", "/")
		first, _ := open(t, server, tab, streamPath+"?after=2", "")
		defer first.Body.Close()
		second, _ := open(t, server, tab, streamPath+"?after=2", "")
		defer second.Body.Close()
		third, _ := open(t, server, tab, streamPath+"?after=2", "")
		third.Body.Close()
		if first.StatusCode != 200 || second.StatusCode != 200 || third.StatusCode != http.StatusTooManyRequests {
			t.Fatalf("got %d, %d, %d", first.StatusCode, second.StatusCode, third.StatusCode)
		}
	})

	t.Run("a backlog beyond the bound sends resync instead", func(t *testing.T) {
		small := newServer(1)
		resp, messages := open(t, small, aliceWeb, streamPath+"?after=0", "")
		defer resp.Body.Close()
		msg := next(t, messages)
		if msg.event != "resync" || msg.data != `{"as_of_event":2}` {
			t.Fatalf("got %+v", msg)
		}
	})

	t.Run("sign-out from the Lab revokes the session", func(t *testing.T) {
		if gotStatus, _ := status(t, request{method: "DELETE", path: "/v1/web/session", cookie: aliceWeb, origin: labOrigin, csrf: aliceCSRF}); gotStatus != 204 {
			t.Fatalf("sign-out: %d", gotStatus)
		}
		if gotStatus, gotCode := status(t, request{method: "GET", path: "/v1/projects", cookie: aliceWeb}); gotStatus != 401 || gotCode != "unauthenticated" {
			t.Fatalf("after sign-out: %d %q", gotStatus, gotCode)
		}
	})
}
