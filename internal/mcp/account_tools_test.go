package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"uuid"

	"github.com/Juanqu261/conn/internal/account"
	"github.com/Juanqu261/conn/internal/auth/github"
	"github.com/Juanqu261/conn/internal/auth/github/githubtest"
	"github.com/Juanqu261/conn/internal/credentials"
	"github.com/Juanqu261/conn/internal/httpclient"
	"github.com/Juanqu261/conn/internal/protocol"
)

func TestMCPAccountLifecycle(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	githubToken := "gho_mcp_device_token"
	githubFake := &githubtest.Server{
		ClientID: "conn-mcp-client",
		Token:    githubToken,
		Users:    map[string]github.Identity{githubToken: {UserID: 4242, Login: "octo-worker"}},
	}
	githubEndpoints := githubFake.Start(t)
	backend := newMCPAccountBackend(t, githubToken)
	store := &credentials.Memory{}
	environment := &testEnvironment{}
	serverClient, err := httpclient.New(backend.server.URL, backend.server.Client())
	if err != nil {
		t.Fatal(err)
	}
	service := &account.Service{
		Server:    serverClient,
		Store:     store,
		LookupEnv: environment.Lookup,
		GitHub:    githubEndpoints,
		HTTP:      http.DefaultClient,
		Sleep:     func(context.Context, time.Duration) error { return nil },
	}

	mcpServer := sdk.NewServer(&sdk.Implementation{Name: "conn-account-test", Version: "0"}, nil)
	interactions := registerTools(mcpServer, Config{
		Account:           service,
		Client:            service.Client,
		DefaultProject:    "019a8f00-0001-7000-8000-000000000001",
		AnalyzerAvailable: func() bool { return true },
	})
	defer interactions.clear()

	var mu sync.Mutex
	var directMessages []string
	urlAction := "cancel"
	formConfirm := false
	client := sdk.NewClient(&sdk.Implementation{Name: "kiro-account-test", Version: "0"}, &sdk.ClientOptions{
		Capabilities: &sdk.ClientCapabilities{Elicitation: &sdk.ElicitationCapabilities{
			Form: &sdk.FormElicitationCapabilities{},
			URL:  &sdk.URLElicitationCapabilities{},
		}},
		ElicitationHandler: func(_ context.Context, req *sdk.ElicitRequest) (*sdk.ElicitResult, error) {
			mu.Lock()
			directMessages = append(directMessages, req.Params.Message, req.Params.URL)
			action, confirm := urlAction, formConfirm
			mu.Unlock()
			switch req.Params.Mode {
			case "url":
				return &sdk.ElicitResult{Action: action}, nil
			case "form":
				return &sdk.ElicitResult{Action: "accept", Content: map[string]any{"confirm": confirm}}, nil
			default:
				return nil, fmt.Errorf("unexpected input mode %q", req.Params.Mode)
			}
		},
	})
	session := connectAccountClient(t, ctx, mcpServer, client)
	defer session.Close()

	status := callAccountTool[sessionStatusOutput](t, ctx, session, "session_status", nil)
	if status.Human != nil || status.Credential.Valid || status.Server.URL != backend.server.URL || !status.Server.Reachable || !status.Server.Ready ||
		status.ProjectSelection.Source != "env" || !status.Analyzer.Available || !status.Capabilities.InteractiveLogin || !status.Capabilities.ConfirmedLogout {
		t.Fatalf("initial session_status = %+v", status)
	}

	// A direct cancellation consumes the pending Device Flow without polling,
	// exchanging, or storing a credential.
	cancelled := callAccountTool[loginOutput](t, ctx, session, "login", nil)
	if cancelled.LoggedIn || githubFake.PollCount() != 0 {
		t.Fatalf("cancelled login = %+v, polls = %d", cancelled, githubFake.PollCount())
	}
	if _, err := store.Get(); !errors.Is(err, credentials.ErrNotFound) {
		t.Fatalf("cancelled login stored a credential: %v", err)
	}

	// The accepted URL interaction polls GitHub, exchanges once, and stores
	// only a Conn token. The one-time user code appears only in direct input.
	mu.Lock()
	urlAction = "accept"
	mu.Unlock()
	loggedInResult := callAccountToolResult(t, ctx, session, "login", nil)
	var loggedIn loginOutput
	decodeStructured(t, loggedInResult, &loggedIn)
	if !loggedIn.LoggedIn || loggedIn.Human == nil || loggedIn.Human.GitHubLogin != "octo-worker" || loggedIn.EnvOverride {
		t.Fatalf("login = %+v", loggedIn)
	}
	storedToken, err := store.Get()
	if err != nil || !credentials.ValidToken(storedToken) || backend.exchangeCount() != 1 {
		t.Fatalf("stored token valid = %t, exchange count = %d, error = %v", credentials.ValidToken(storedToken), backend.exchangeCount(), err)
	}
	mu.Lock()
	direct := strings.Join(directMessages, "\n")
	mu.Unlock()
	if !strings.Contains(direct, "WDJB-MJHT") || !strings.Contains(direct, "https://github.com/login/device") {
		t.Fatalf("direct login interaction omitted code or URL: %q", direct)
	}
	assertAccountSecretsAbsent(t, "final login result", toolResultText(loggedInResult), "WDJB-MJHT", githubToken, storedToken.Reveal(), "fake-device-code")

	status = callAccountTool[sessionStatusOutput](t, ctx, session, "session_status", nil)
	if status.Human == nil || status.Human.GitHubLogin != "octo-worker" || status.Credential.Source != string(credentials.SourceStore) || !status.Credential.Valid {
		t.Fatalf("authenticated session_status = %+v", status)
	}

	// Logout confirmation cannot be supplied as a model argument.
	result, err := session.CallTool(ctx, &sdk.CallToolParams{Name: "logout", Arguments: map[string]any{"confirm": true}})
	if err != nil || result == nil || !result.IsError {
		t.Fatalf("model-supplied logout confirmation was accepted: %v, %+v", err, result)
	}

	// Declining the direct form leaves both local and server state unchanged.
	cancelledLogout := callAccountTool[logoutOutput](t, ctx, session, "logout", nil)
	if cancelledLogout.LoggedOut || backend.logoutCount() != 0 {
		t.Fatalf("cancelled logout = %+v, revocations = %d", cancelledLogout, backend.logoutCount())
	}
	if _, err := store.Get(); err != nil {
		t.Fatalf("cancelled logout removed the credential: %v", err)
	}

	// An environment token remains authoritative. Re-login still stores the
	// new session but warns that the environment override wins.
	envToken := backend.addSession('Z')
	environment.Set(credentials.TokenEnv, envToken)
	secondLoginResult := callAccountToolResult(t, ctx, session, "login", nil)
	var secondLogin loginOutput
	decodeStructured(t, secondLoginResult, &secondLogin)
	if !secondLogin.LoggedIn || !secondLogin.EnvOverride || !strings.Contains(secondLogin.Message, credentials.TokenEnv) {
		t.Fatalf("login with environment override = %+v", secondLogin)
	}
	mu.Lock()
	directWithOverride := strings.Join(directMessages, "\n")
	mu.Unlock()
	if !strings.Contains(directWithOverride, "CONN_TOKEN is set and will remain the active credential") {
		t.Fatalf("direct login interaction omitted environment warning: %q", directWithOverride)
	}
	secondStoredToken, err := store.Get()
	if err != nil || secondStoredToken.Reveal() == envToken {
		t.Fatalf("environment token was persisted or store failed: %v", err)
	}
	status = callAccountTool[sessionStatusOutput](t, ctx, session, "session_status", nil)
	if status.Credential.Source != string(credentials.SourceEnv) || !status.Credential.Valid {
		t.Fatalf("environment session_status = %+v", status)
	}

	// Direct confirmation revokes and deletes the stored session while leaving
	// CONN_TOKEN untouched.
	mu.Lock()
	formConfirm = true
	mu.Unlock()
	logoutResult := callAccountToolResult(t, ctx, session, "logout", nil)
	var loggedOut logoutOutput
	decodeStructured(t, logoutResult, &loggedOut)
	if !loggedOut.LoggedOut || !loggedOut.ServerRevoked || !loggedOut.EnvOverrideRemains || backend.logoutCount() < 2 {
		t.Fatalf("logout = %+v, revocations = %d", loggedOut, backend.logoutCount())
	}
	if _, err := store.Get(); !errors.Is(err, credentials.ErrNotFound) {
		t.Fatalf("logout retained stored credential: %v", err)
	}
	if value, ok := environment.Lookup(credentials.TokenEnv); !ok || value != envToken {
		t.Fatal("logout changed CONN_TOKEN")
	}
	assertAccountSecretsAbsent(t, "logout result", toolResultText(logoutResult), githubToken, secondStoredToken.Reveal(), envToken, "fake-device-code")

	backend.setReady(false)
	notReady := callAccountTool[sessionStatusOutput](t, ctx, session, "session_status", nil)
	if !notReady.Server.Reachable || notReady.Server.Ready || !hasString(notReady.Problems, "backend_not_ready") {
		t.Fatalf("not-ready session_status = %+v", notReady)
	}
	backend.setReady(true)

	// A client without elicitation cannot start Device Flow or model-confirm
	// logout. It receives the explicit non-interactive path instead.
	headless := sdk.NewClient(&sdk.Implementation{Name: "headless-account-test", Version: "0"}, &sdk.ClientOptions{Capabilities: &sdk.ClientCapabilities{}})
	headlessSession := connectAccountClient(t, ctx, mcpServer, headless)
	defer headlessSession.Close()
	headlessResult, err := headlessSession.CallTool(ctx, &sdk.CallToolParams{Name: "login"})
	if err != nil || headlessResult == nil || !headlessResult.IsError || !strings.Contains(toolResultText(headlessResult), "interactive login unavailable") ||
		!strings.Contains(toolResultText(headlessResult), credentials.TokenEnv) {
		t.Fatalf("headless login = %v, %s", err, toolResultText(headlessResult))
	}

	// An unavailable credential store is reported without attempting Device
	// Flow and without falling back to plaintext storage.
	pollsBeforeUnavailable := githubFake.PollCount()
	unavailableService := &account.Service{
		Server:    serverClient,
		Store:     &credentials.Memory{Err: errors.New("locked")},
		LookupEnv: func(string) (string, bool) { return "", false },
		GitHub:    githubEndpoints,
		HTTP:      http.DefaultClient,
		Sleep:     func(context.Context, time.Duration) error { return nil },
	}
	unavailableServer := sdk.NewServer(&sdk.Implementation{Name: "conn-unavailable-store-test", Version: "0"}, nil)
	unavailableInteractions := registerTools(unavailableServer, Config{Account: unavailableService, Client: unavailableService.Client})
	defer unavailableInteractions.clear()
	unavailableClient := sdk.NewClient(&sdk.Implementation{Name: "kiro-unavailable-store-test", Version: "0"}, &sdk.ClientOptions{
		Capabilities: &sdk.ClientCapabilities{Elicitation: &sdk.ElicitationCapabilities{URL: &sdk.URLElicitationCapabilities{}}},
		ElicitationHandler: func(context.Context, *sdk.ElicitRequest) (*sdk.ElicitResult, error) {
			t.Fatal("unavailable store unexpectedly started Device Flow")
			return nil, nil
		},
	})
	unavailableSession := connectAccountClient(t, ctx, unavailableServer, unavailableClient)
	defer unavailableSession.Close()
	unavailableStatus := callAccountTool[sessionStatusOutput](t, ctx, unavailableSession, "session_status", nil)
	if unavailableStatus.Credential.StoreAvailable || !hasString(unavailableStatus.Problems, "credential_store_unavailable") {
		t.Fatalf("unavailable-store session_status = %+v", unavailableStatus)
	}
	unavailableLogin, err := unavailableSession.CallTool(ctx, &sdk.CallToolParams{Name: "login"})
	if err != nil || unavailableLogin == nil || !unavailableLogin.IsError || !strings.Contains(toolResultText(unavailableLogin), "plaintext fallback") ||
		!strings.Contains(toolResultText(unavailableLogin), credentials.TokenEnv) || githubFake.PollCount() != pollsBeforeUnavailable {
		t.Fatalf("unavailable-store login = %v, %s", err, toolResultText(unavailableLogin))
	}

	backend.revokeSession(envToken)
	expired := callAccountTool[sessionStatusOutput](t, ctx, session, "session_status", nil)
	if expired.Credential.Valid || !hasString(expired.Problems, "session_expired_or_revoked") {
		t.Fatalf("expired session_status = %+v", expired)
	}

	backend.server.Close()
	unreachable := callAccountTool[sessionStatusOutput](t, ctx, session, "session_status", nil)
	if unreachable.Server.Reachable || unreachable.Server.Ready || !hasString(unreachable.Problems, "backend_unreachable") {
		t.Fatalf("unreachable session_status = %+v", unreachable)
	}
}

func connectAccountClient(t *testing.T, ctx context.Context, server *sdk.Server, client *sdk.Client) *sdk.ClientSession {
	t.Helper()
	serverTransport, clientTransport := sdk.NewInMemoryTransports()
	serverSession, err := server.Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = serverSession.Close() })
	session, err := client.Connect(ctx, clientTransport, &sdk.ClientSessionOptions{ProtocolVersion: "2026-07-28"})
	if err != nil {
		t.Fatal(err)
	}
	return session
}

func callAccountTool[T any](t *testing.T, ctx context.Context, session *sdk.ClientSession, name string, arguments map[string]any) T {
	t.Helper()
	result := callAccountToolResult(t, ctx, session, name, arguments)
	var output T
	decodeStructured(t, result, &output)
	return output
}

func callAccountToolResult(t *testing.T, ctx context.Context, session *sdk.ClientSession, name string, arguments map[string]any) *sdk.CallToolResult {
	t.Helper()
	result, err := session.CallTool(ctx, &sdk.CallToolParams{Name: name, Arguments: arguments})
	if err != nil || result == nil || result.IsError {
		t.Fatalf("%s: %v, %s", name, err, toolResultText(result))
	}
	return result
}

func decodeStructured(t *testing.T, result *sdk.CallToolResult, output any) {
	t.Helper()
	data, err := json.Marshal(result.StructuredContent)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, output); err != nil {
		t.Fatalf("decode structured content %s: %v", data, err)
	}
}

func hasString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func assertAccountSecretsAbsent(t *testing.T, where, text string, secrets ...string) {
	t.Helper()
	for _, value := range secrets {
		if value != "" && strings.Contains(text, value) {
			t.Fatalf("%s contains secret %q", where, value)
		}
	}
}

type testEnvironment struct {
	mu     sync.Mutex
	values map[string]string
}

func (e *testEnvironment) Lookup(key string) (string, bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	value := e.values[key]
	return value, value != ""
}

func (e *testEnvironment) Set(key, value string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.values == nil {
		e.values = make(map[string]string)
	}
	e.values[key] = value
}

type mcpAccountBackend struct {
	server        *httptest.Server
	githubToken   string
	human         protocol.Human
	mu            sync.Mutex
	active        map[string]bool
	ready         bool
	exchanges     int
	logouts       int
	nextTokenByte byte
}

func newMCPAccountBackend(t *testing.T, githubToken string) *mcpAccountBackend {
	t.Helper()
	backend := &mcpAccountBackend{
		githubToken:   githubToken,
		human:         protocol.Human{HumanID: mustTestUUID(t, "019a8f00-0000-7000-8000-00000000000b"), GitHubUserID: 4242, GitHubLogin: "octo-worker"},
		active:        make(map[string]bool),
		ready:         true,
		nextTokenByte: 'A',
	}
	backend.server = httptest.NewServer(http.HandlerFunc(backend.serveHTTP))
	t.Cleanup(backend.server.Close)
	return backend
}

func (b *mcpAccountBackend) serveHTTP(w http.ResponseWriter, r *http.Request) {
	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/healthz":
		w.WriteHeader(http.StatusOK)
	case r.Method == http.MethodGet && r.URL.Path == "/readyz":
		b.mu.Lock()
		ready := b.ready
		b.mu.Unlock()
		if !ready {
			writeAccountError(w, http.StatusServiceUnavailable, "not_ready", "Backend not ready.")
			return
		}
		w.WriteHeader(http.StatusOK)
	case r.Method == http.MethodGet && r.URL.Path == "/v1/auth/github/client":
		writeAccountData(w, map[string]any{"client_id": "conn-mcp-client"})
	case r.Method == http.MethodPost && r.URL.Path == "/v1/auth/github/exchange":
		if bearer(r) != b.githubToken {
			writeAccountError(w, http.StatusUnauthorized, "github_token_rejected", "GitHub token rejected.")
			return
		}
		b.mu.Lock()
		token := b.newTokenLocked()
		b.exchanges++
		b.mu.Unlock()
		writeAccountData(w, map[string]any{
			"token": token, "expires_at": "2026-12-22T09:00:00Z",
			"human": map[string]any{"human_id": b.human.HumanID.String(), "github_user_id": b.human.GitHubUserID, "github_login": b.human.GitHubLogin},
		})
	case r.Method == http.MethodGet && r.URL.Path == "/v1/humans/me":
		if !b.isActive(bearer(r)) {
			writeAccountError(w, http.StatusUnauthorized, "unauthenticated", "Session expired or revoked.")
			return
		}
		writeAccountData(w, map[string]any{"human_id": b.human.HumanID.String(), "github_user_id": b.human.GitHubUserID, "github_login": b.human.GitHubLogin})
	case r.Method == http.MethodDelete && r.URL.Path == "/v1/session":
		token := bearer(r)
		b.mu.Lock()
		active := b.active[token]
		delete(b.active, token)
		if active {
			b.logouts++
		}
		b.mu.Unlock()
		if !active {
			writeAccountError(w, http.StatusUnauthorized, "unauthenticated", "Session expired or revoked.")
			return
		}
		w.WriteHeader(http.StatusNoContent)
	default:
		http.NotFound(w, r)
	}
}

func (b *mcpAccountBackend) revokeSession(token string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.active, token)
}

func (b *mcpAccountBackend) setReady(ready bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.ready = ready
}

func (b *mcpAccountBackend) addSession(value byte) string {
	b.mu.Lock()
	defer b.mu.Unlock()
	token := "conn_sess_" + strings.Repeat(string(value), 43)
	b.active[token] = true
	return token
}

func (b *mcpAccountBackend) newTokenLocked() string {
	token := "conn_sess_" + strings.Repeat(string(b.nextTokenByte), 43)
	b.nextTokenByte++
	b.active[token] = true
	return token
}

func (b *mcpAccountBackend) isActive(token string) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.active[token]
}

func (b *mcpAccountBackend) exchangeCount() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.exchanges
}

func (b *mcpAccountBackend) logoutCount() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.logouts
}

func bearer(r *http.Request) string {
	return strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
}

func writeAccountData(w http.ResponseWriter, data any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"data": data})
}

func writeAccountError(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"code": code, "message": message}})
}

func mustTestUUID(t *testing.T, raw string) uuid.UUID {
	t.Helper()
	id, err := parseID("test UUID", raw)
	if err != nil {
		t.Fatal(err)
	}
	return id
}
