// Package account holds local login, logout, identity, and diagnostic
// operations behind Conn's CLI and MCP adapters. It combines GitHub device
// authorization, the backend client, and the credential store; interfaces only
// render or transport its safe results.
package account

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"time"

	"github.com/Juanqu261/conn/internal/auth/github"
	"github.com/Juanqu261/conn/internal/credentials"
	"github.com/Juanqu261/conn/internal/httpclient"
	"github.com/Juanqu261/conn/internal/protocol"
)

// Service performs account operations against one Conn server.
type Service struct {
	Server    *httpclient.Client
	Store     credentials.Store
	LookupEnv func(string) (string, bool)
	GitHub    github.Endpoints
	HTTP      *http.Client
	// Now and Sleep drive device-flow polling. They default to the real clock.
	Now   func() time.Time
	Sleep func(ctx context.Context, d time.Duration) error
}

// LoginResult describes a completed login.
type LoginResult struct {
	Human     protocol.Human
	ExpiresAt time.Time
	// EnvOverride reports that CONN_TOKEN is set and still takes precedence
	// over the credential just stored.
	EnvOverride bool
}

// LoginAuthorization is the safe, directly human-visible part of one pending
// GitHub Device Flow. It never contains GitHub's secret device code.
type LoginAuthorization struct {
	UserCode        string
	VerificationURI string
	ExpiresAt       time.Time
	EnvOverride     bool
}

// LoginAttempt holds one pending Device Flow in process memory. Its secret
// device code is never serializable.
type LoginAttempt struct {
	mu            sync.Mutex
	service       *Service
	flow          *github.DeviceFlow
	authorization *github.DeviceAuthorization
	envOverride   bool
	consumed      bool
}

// Authorization returns only the values that may be shown directly to the
// human authorizing the device.
func (a *LoginAttempt) Authorization() LoginAuthorization {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.authorization == nil {
		return LoginAuthorization{}
	}
	return LoginAuthorization{
		UserCode:        a.authorization.UserCode,
		VerificationURI: a.authorization.VerificationURI,
		ExpiresAt:       a.authorization.ExpiresAt,
		EnvOverride:     a.envOverride,
	}
}

// BeginLogin verifies that the credential store is usable and starts GitHub
// Device Flow without polling or exchanging a token.
func (s *Service) BeginLogin(ctx context.Context) (*LoginAttempt, error) {
	_, err := s.Store.Get()
	if err != nil && !errors.Is(err, credentials.ErrNotFound) {
		return nil, err
	}

	config, err := s.Server.GitHubLogin(ctx)
	if err != nil {
		return nil, err
	}
	flow := &github.DeviceFlow{
		HTTP:      s.HTTP,
		Endpoints: s.GitHub,
		ClientID:  config.ClientID,
		Now:       s.Now,
		Sleep:     s.Sleep,
	}
	authorization, err := flow.Start(ctx)
	if err != nil {
		return nil, err
	}
	_, envOverride := s.lookupEnv(credentials.TokenEnv)
	return &LoginAttempt{service: s, flow: flow, authorization: authorization, envOverride: envOverride}, nil
}

// CompleteLogin consumes one attempt, polls GitHub, exchanges the temporary
// GitHub token once, and stores only the resulting Conn session token.
func (s *Service) CompleteLogin(ctx context.Context, attempt *LoginAttempt) (LoginResult, error) {
	flow, authorization, err := attempt.consume(s)
	if err != nil {
		return LoginResult{}, err
	}
	githubToken, err := flow.Wait(ctx, authorization)
	if err != nil {
		return LoginResult{}, err
	}
	previous, err := s.Store.Get()
	if err != nil && !errors.Is(err, credentials.ErrNotFound) {
		return LoginResult{}, err
	}

	grant, err := s.Server.Exchange(ctx, githubToken)
	if err != nil {
		return LoginResult{}, err
	}
	if err := s.Store.Set(grant.Token); err != nil {
		// Do not leave a live session that nothing holds.
		_ = s.Server.WithToken(grant.Token).Logout(context.WithoutCancel(ctx))
		return LoginResult{}, err
	}
	// Logging in again replaces the previous session; end it on the server.
	if credentials.ValidToken(previous) && previous.Reveal() != grant.Token.Reveal() {
		_ = s.Server.WithToken(previous).Logout(ctx)
	}

	_, envSet := s.lookupEnv(credentials.TokenEnv)
	return LoginResult{Human: grant.Human, ExpiresAt: grant.ExpiresAt.Time(), EnvOverride: envSet}, nil
}

// CancelLogin consumes a pending attempt without polling or exchanging it.
func (s *Service) CancelLogin(attempt *LoginAttempt) {
	if attempt == nil {
		return
	}
	_, _, _ = attempt.consume(s)
}

func (a *LoginAttempt) consume(service *Service) (*github.DeviceFlow, *github.DeviceAuthorization, error) {
	if a == nil {
		return nil, nil, errors.New("login attempt is missing")
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.service != service || a.consumed || a.flow == nil || a.authorization == nil {
		return nil, nil, errors.New("login attempt is no longer active")
	}
	a.consumed = true
	flow, authorization := a.flow, a.authorization
	a.flow, a.authorization, a.envOverride = nil, nil, false
	return flow, authorization, nil
}

// Login runs GitHub device authorization, exchanges the GitHub token for a
// Conn session, and stores only the Conn token. show is called once with
// the code the human must enter.
func (s *Service) Login(ctx context.Context, show func(*github.DeviceAuthorization)) (LoginResult, error) {
	attempt, err := s.BeginLogin(ctx)
	if err != nil {
		return LoginResult{}, err
	}
	show(attempt.authorization)
	return s.CompleteLogin(ctx, attempt)
}

// LogoutResult describes a logout. The local credential is always removed;
// RevokeErr reports a server-side revocation that did not succeed.
type LogoutResult struct {
	RevokeErr   error
	EnvOverride bool
}

// Logout revokes the stored session on the server and deletes it locally.
// It does not affect CONN_TOKEN, which the human set and must unset.
func (s *Service) Logout(ctx context.Context) (LogoutResult, error) {
	token, err := s.Store.Get()
	if errors.Is(err, credentials.ErrNotFound) {
		return LogoutResult{}, credentials.ErrNotLoggedIn
	}
	if err != nil {
		return LogoutResult{}, err
	}

	revokeErr := s.Server.WithToken(token).Logout(ctx)
	if httpclient.IsCode(revokeErr, "unauthenticated") {
		revokeErr = nil // already expired or revoked
	}
	if err := s.Store.Delete(); err != nil {
		return LogoutResult{}, err
	}
	_, envSet := s.lookupEnv(credentials.TokenEnv)
	return LogoutResult{RevokeErr: revokeErr, EnvOverride: envSet}, nil
}

// Client returns a backend client authenticated with the resolved
// credential: CONN_TOKEN, then the credential store.
func (s *Service) Client() (*httpclient.Client, error) {
	token, _, err := credentials.Resolve(s.lookupEnv, s.Store)
	if err != nil {
		return nil, err
	}
	return s.Server.WithToken(token), nil
}

// WhoAmI returns the human behind the resolved credential.
func (s *Service) WhoAmI(ctx context.Context) (protocol.Human, credentials.Source, error) {
	token, source, err := credentials.Resolve(s.lookupEnv, s.Store)
	if err != nil {
		return protocol.Human{}, source, err
	}
	human, err := s.Server.WithToken(token).Me(ctx)
	return human, source, err
}

// StoredWhoAmI returns the human behind the stored credential, deliberately
// ignoring CONN_TOKEN. Confirmed logout uses it because logout changes only the
// operating-system credential store.
func (s *Service) StoredWhoAmI(ctx context.Context) (protocol.Human, error) {
	token, err := s.Store.Get()
	if errors.Is(err, credentials.ErrNotFound) {
		return protocol.Human{}, credentials.ErrNotLoggedIn
	}
	if err != nil {
		return protocol.Human{}, err
	}
	if !credentials.ValidToken(token) {
		return protocol.Human{}, credentials.ErrInvalid
	}
	return s.Server.WithToken(token).Me(ctx)
}

// Status is a safe account diagnostic. It contains no token, device code,
// credential-store value, request body, or backend implementation detail.
type Status struct {
	ServerURL                string
	ServerReachable          bool
	ServerReady              bool
	CredentialStoreAvailable bool
	StoredCredentialPresent  bool
	CredentialSource         credentials.Source
	CredentialValid          bool
	Human                    *protocol.Human
	Problems                 []string
}

// SessionStatus checks backend health, credential storage, and the resolved
// session while returning only stable, safe problem codes.
func (s *Service) SessionStatus(ctx context.Context) Status {
	status := Status{ServerURL: s.Server.URL()}
	if err := s.Server.Probe(ctx, "/healthz"); err != nil {
		status.Problems = append(status.Problems, "backend_unreachable")
	} else {
		status.ServerReachable = true
		if err := s.Server.Probe(ctx, "/readyz"); err != nil {
			status.Problems = append(status.Problems, "backend_not_ready")
		} else {
			status.ServerReady = true
		}
	}

	_, storeErr := s.Store.Get()
	switch {
	case storeErr == nil:
		status.CredentialStoreAvailable = true
		status.StoredCredentialPresent = true
	case errors.Is(storeErr, credentials.ErrNotFound):
		status.CredentialStoreAvailable = true
	default:
		status.Problems = append(status.Problems, "credential_store_unavailable")
	}

	token, source, err := credentials.Resolve(s.lookupEnv, s.Store)
	status.CredentialSource = source
	if err != nil {
		switch {
		case errors.Is(err, credentials.ErrNotLoggedIn):
			status.Problems = append(status.Problems, "not_authenticated")
		case errors.Is(err, credentials.ErrInvalid):
			status.Problems = append(status.Problems, "invalid_credential")
		case !errors.Is(storeErr, credentials.ErrUnavailable):
			status.Problems = append(status.Problems, "credential_unavailable")
		}
		return status
	}

	human, err := s.Server.WithToken(token).Me(ctx)
	if err != nil {
		if httpclient.IsCode(err, "unauthenticated") {
			status.Problems = append(status.Problems, "session_expired_or_revoked")
		} else if status.ServerReachable {
			status.Problems = append(status.Problems, "session_check_failed")
		}
		return status
	}
	status.CredentialValid = true
	status.Human = &human
	return status
}

// Check is one diagnostic result. An optional check that fails is reported
// as a warning and does not make the diagnosis unhealthy.
type Check struct {
	Name     string
	OK       bool
	Optional bool
	Detail   string
}

// Doctor checks the server, the credential store, and the session without
// revealing any credential.
func (s *Service) Doctor(ctx context.Context) []Check {
	checks := []Check{probe(ctx, s.Server, "Server reachable", "/healthz")}
	if checks[0].OK {
		checks = append(checks, probe(ctx, s.Server, "Server ready", "/readyz"))
	}

	_, storeErr := s.Store.Get()
	switch {
	case storeErr == nil || errors.Is(storeErr, credentials.ErrNotFound):
		checks = append(checks, Check{Name: "Credential store", OK: true, Detail: "available"})
	default:
		checks = append(checks, Check{Name: "Credential store", Detail: storeErr.Error()})
	}

	token, source, err := credentials.Resolve(s.lookupEnv, s.Store)
	if err != nil {
		return append(checks, Check{Name: "Credential", Detail: err.Error()})
	}
	checks = append(checks, Check{Name: "Credential", OK: true, Detail: "from " + string(source)})

	human, err := s.Server.WithToken(token).Me(ctx)
	if err != nil {
		return append(checks, Check{Name: "Session", Detail: err.Error()})
	}
	return append(checks, Check{Name: "Session", OK: true, Detail: "@" + human.GitHubLogin + " (" + human.HumanID.String() + ")"})
}

func probe(ctx context.Context, server *httpclient.Client, name, path string) Check {
	if err := server.Probe(ctx, path); err != nil {
		return Check{Name: name, Detail: err.Error()}
	}
	return Check{Name: name, OK: true, Detail: server.URL()}
}

func (s *Service) lookupEnv(key string) (string, bool) {
	value, ok := s.LookupEnv(key)
	return value, ok && value != ""
}
