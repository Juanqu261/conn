// Package account holds the local login, logout, identity, and diagnostic
// operations behind the CLI. It combines GitHub device authorization, the
// backend client, and the credential store; commands only render results.
package account

import (
	"context"
	"errors"
	"net/http"
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

// Login runs GitHub device authorization, exchanges the GitHub token for a
// Conn session, and stores only the Conn token. show is called once with
// the code the human must enter.
func (s *Service) Login(ctx context.Context, show func(*github.DeviceAuthorization)) (LoginResult, error) {
	// Check the store first so the human never authorizes on GitHub only
	// to find the session cannot be kept.
	previous, err := s.Store.Get()
	if err != nil && !errors.Is(err, credentials.ErrNotFound) {
		return LoginResult{}, err
	}

	config, err := s.Server.GitHubLogin(ctx)
	if err != nil {
		return LoginResult{}, err
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
		return LoginResult{}, err
	}
	show(authorization)
	githubToken, err := flow.Wait(ctx, authorization)
	if err != nil {
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

// LogoutResult describes a logout. The local credential is always removed;
// RevokeErr reports a server-side revocation that did not succeed.
type LogoutResult struct {
	RevokeErr error
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
	return LogoutResult{RevokeErr: revokeErr}, nil
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

// Check is one diagnostic result.
type Check struct {
	Name   string
	OK     bool
	Detail string
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
