// Package application holds Conn's backend use cases. It depends on
// protocol types and on small interfaces implemented by adapters; it never
// imports HTTP, SQL, or GitHub client packages.
package application

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"time"
	"uuid"

	"github.com/Juanqu261/conn/internal/protocol"
	"github.com/Juanqu261/conn/internal/secret"
)

// SessionLifetime is the absolute expiry of a CLI session.
const SessionLifetime = 30 * 24 * time.Hour

// BrowserSessionLifetime is the absolute expiry of a Conn Lab browser
// session (decision 0013).
const BrowserSessionLifetime = 7 * 24 * time.Hour

const (
	tokenPrefix        = "conn_sess_"
	browserTokenPrefix = "conn_web_"
	csrfLabel          = "conn-lab-csrf-v1"
)

// SessionKind separates bearer sessions from browser cookie sessions. A
// session of one kind is never accepted where the other is expected.
type SessionKind string

const (
	SessionCLI     SessionKind = "cli"
	SessionBrowser SessionKind = "browser"
)

var (
	ErrUnauthenticated     = errors.New("authentication required")
	ErrGitHubTokenRejected = errors.New("GitHub rejected the token")
	ErrGitHubUnavailable   = errors.New("GitHub could not be reached")
)

// GitHubIdentity is a verified GitHub user.
type GitHubIdentity struct {
	UserID int64
	Login  string
}

// VerifyGitHubToken returns the identity owning a GitHub token, or
// ErrGitHubTokenRejected or ErrGitHubUnavailable.
type VerifyGitHubToken func(ctx context.Context, token secret.String) (GitHubIdentity, error)

// Session is an authenticated Conn session.
type Session struct {
	SessionID uuid.UUID
	Kind      SessionKind
	Human     protocol.Human
	ExpiresAt time.Time
}

// NewSession is a session to persist. Only the token hash is stored.
type NewSession struct {
	SessionID uuid.UUID
	Kind      SessionKind
	HumanID   uuid.UUID
	TokenHash [sha256.Size]byte
	CreatedAt time.Time
	ExpiresAt time.Time
}

// AuthStore persists humans and sessions.
type AuthStore interface {
	// UpsertHuman returns the human for githubUserID, creating it with
	// candidateID if needed and recording the current login.
	UpsertHuman(ctx context.Context, candidateID uuid.UUID, githubUserID int64, login string, now time.Time) (protocol.Human, error)
	CreateSession(ctx context.Context, session NewSession) error
	// FindSession returns the unrevoked, unexpired session of kind with
	// tokenHash, or ErrUnauthenticated.
	FindSession(ctx context.Context, tokenHash [sha256.Size]byte, kind SessionKind, now time.Time) (Session, error)
	RevokeSession(ctx context.Context, sessionID uuid.UUID, now time.Time) error
}

// Auth exchanges GitHub identity for Conn sessions.
type Auth struct {
	Store       AuthStore
	VerifyToken VerifyGitHubToken
	// Now defaults to the real clock. Tests advance it to cross expiry.
	Now func() time.Time
}

// BrowserGrant is a new browser session. The raw token leaves the server
// only in the HttpOnly session cookie.
type BrowserGrant struct {
	Token     secret.String
	ExpiresAt time.Time
	Human     protocol.Human
}

// Exchange verifies a GitHub token, maps its immutable user ID to a Conn
// human, and issues a new CLI session. The GitHub token is not retained,
// and the raw Conn token exists only in the returned grant.
func (a *Auth) Exchange(ctx context.Context, githubToken secret.String) (protocol.SessionGrant, error) {
	token, expiresAt, human, err := a.issue(ctx, githubToken, SessionCLI)
	if err != nil {
		return protocol.SessionGrant{}, err
	}
	return protocol.SessionGrant{Token: token, ExpiresAt: protocol.NewTimestamp(expiresAt), Human: human}, nil
}

// ExchangeBrowser is Exchange for a Conn Lab browser session. The same
// GitHub account maps to the same Conn human as conn login.
func (a *Auth) ExchangeBrowser(ctx context.Context, githubToken secret.String) (BrowserGrant, error) {
	token, expiresAt, human, err := a.issue(ctx, githubToken, SessionBrowser)
	if err != nil {
		return BrowserGrant{}, err
	}
	return BrowserGrant{Token: token, ExpiresAt: expiresAt, Human: human}, nil
}

func (a *Auth) issue(ctx context.Context, githubToken secret.String, kind SessionKind) (secret.String, time.Time, protocol.Human, error) {
	identity, err := a.VerifyToken(ctx, githubToken)
	if err != nil {
		return secret.String{}, time.Time{}, protocol.Human{}, err
	}
	now := a.now()

	human, err := a.Store.UpsertHuman(ctx, uuid.NewV7(), identity.UserID, identity.Login, now)
	if err != nil {
		return secret.String{}, time.Time{}, protocol.Human{}, fmt.Errorf("record human: %w", err)
	}

	prefix, lifetime := tokenPrefix, SessionLifetime
	if kind == SessionBrowser {
		prefix, lifetime = browserTokenPrefix, BrowserSessionLifetime
	}
	token, hash, err := newToken(prefix)
	if err != nil {
		return secret.String{}, time.Time{}, protocol.Human{}, err
	}
	expiresAt := now.Add(lifetime)
	if err := a.Store.CreateSession(ctx, NewSession{
		SessionID: uuid.NewV7(),
		Kind:      kind,
		HumanID:   human.HumanID,
		TokenHash: hash,
		CreatedAt: now,
		ExpiresAt: expiresAt,
	}); err != nil {
		return secret.String{}, time.Time{}, protocol.Human{}, fmt.Errorf("create session: %w", err)
	}
	return token, expiresAt, human, nil
}

// Authenticate returns the CLI session for a bearer token, or
// ErrUnauthenticated when the token is unknown, expired, revoked, or a
// browser session.
func (a *Auth) Authenticate(ctx context.Context, token secret.String) (Session, error) {
	return a.Store.FindSession(ctx, HashToken(token), SessionCLI, a.now())
}

// AuthenticateBrowser returns the browser session for a cookie token, or
// ErrUnauthenticated when the token is unknown, expired, revoked, or a CLI
// session.
func (a *Auth) AuthenticateBrowser(ctx context.Context, token secret.String) (Session, error) {
	return a.Store.FindSession(ctx, HashToken(token), SessionBrowser, a.now())
}

// Logout revokes the session immediately.
func (a *Auth) Logout(ctx context.Context, session Session) error {
	return a.Store.RevokeSession(ctx, session.SessionID, a.now())
}

// HashToken returns the SHA-256 hash under which a token is stored.
func HashToken(token secret.String) [sha256.Size]byte {
	return sha256.Sum256([]byte(token.Reveal()))
}

// CSRFToken derives the CSRF token of a browser session from its session
// token. Only a holder of the HttpOnly cookie can compute it, it changes
// with every session, and nothing extra is stored.
func CSRFToken(sessionToken secret.String) string {
	mac := hmac.New(sha256.New, []byte(sessionToken.Reveal()))
	mac.Write([]byte(csrfLabel))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

// ValidCSRF reports in constant time whether presented is the CSRF token of
// the browser session sessionToken.
func ValidCSRF(sessionToken secret.String, presented string) bool {
	return presented != "" && hmac.Equal([]byte(CSRFToken(sessionToken)), []byte(presented))
}

// IsBrowserToken reports whether token has the browser-session shape, so
// it can be refused as a bearer credential without a database lookup.
func IsBrowserToken(token secret.String) bool {
	value := token.Reveal()
	return len(value) == len(browserTokenPrefix)+43 && value[:len(browserTokenPrefix)] == browserTokenPrefix
}

func newToken(prefix string) (secret.String, [sha256.Size]byte, error) {
	random := make([]byte, 32)
	if _, err := rand.Read(random); err != nil {
		return secret.String{}, [sha256.Size]byte{}, err
	}
	token := secret.New(prefix + base64.RawURLEncoding.EncodeToString(random))
	return token, HashToken(token), nil
}

// now returns the current time at the microsecond precision PostgreSQL
// stores, so persisted and returned timestamps agree.
func (a *Auth) now() time.Time {
	now := time.Now
	if a.Now != nil {
		now = a.Now
	}
	return now().UTC().Truncate(time.Microsecond)
}
