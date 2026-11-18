// Package application holds Conn's backend use cases. It depends on
// protocol types and on small interfaces implemented by adapters; it never
// imports HTTP, SQL, or GitHub client packages.
package application

import (
	"context"
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

// SessionLifetime is the absolute session expiry
const SessionLifetime = 30 * 24 * time.Hour

const tokenPrefix = "conn_sess_"

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
	Human     protocol.Human
	ExpiresAt time.Time
}

// NewSession is a session to persist. Only the token hash is stored.
type NewSession struct {
	SessionID uuid.UUID
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
	// FindSession returns the unrevoked, unexpired session with tokenHash,
	// or ErrUnauthenticated.
	FindSession(ctx context.Context, tokenHash [sha256.Size]byte, now time.Time) (Session, error)
	RevokeSession(ctx context.Context, sessionID uuid.UUID, now time.Time) error
}

// Auth exchanges GitHub identity for Conn sessions.
type Auth struct {
	Store       AuthStore
	VerifyToken VerifyGitHubToken
	// Now defaults to the real clock. Tests advance it to cross expiry.
	Now func() time.Time
}

// Exchange verifies a GitHub token, maps its immutable user ID to a Conn
// human, and issues a new session. The GitHub token is not retained, and the
// raw Conn token exists only in the returned grant.
func (a *Auth) Exchange(ctx context.Context, githubToken secret.String) (protocol.SessionGrant, error) {
	identity, err := a.VerifyToken(ctx, githubToken)
	if err != nil {
		return protocol.SessionGrant{}, err
	}
	now := a.now()

	human, err := a.Store.UpsertHuman(ctx, uuid.NewV7(), identity.UserID, identity.Login, now)
	if err != nil {
		return protocol.SessionGrant{}, fmt.Errorf("record human: %w", err)
	}

	token, hash, err := newToken()
	if err != nil {
		return protocol.SessionGrant{}, err
	}
	expiresAt := now.Add(SessionLifetime)
	if err := a.Store.CreateSession(ctx, NewSession{
		SessionID: uuid.NewV7(),
		HumanID:   human.HumanID,
		TokenHash: hash,
		CreatedAt: now,
		ExpiresAt: expiresAt,
	}); err != nil {
		return protocol.SessionGrant{}, fmt.Errorf("create session: %w", err)
	}
	return protocol.SessionGrant{Token: token, ExpiresAt: protocol.NewTimestamp(expiresAt), Human: human}, nil
}

// Authenticate returns the session for token, or ErrUnauthenticated when
// the token is unknown, expired, or revoked.
func (a *Auth) Authenticate(ctx context.Context, token secret.String) (Session, error) {
	return a.Store.FindSession(ctx, HashToken(token), a.now())
}

// Logout revokes the session immediately.
func (a *Auth) Logout(ctx context.Context, session Session) error {
	return a.Store.RevokeSession(ctx, session.SessionID, a.now())
}

// HashToken returns the SHA-256 hash under which a token is stored.
func HashToken(token secret.String) [sha256.Size]byte {
	return sha256.Sum256([]byte(token.Reveal()))
}

func newToken() (secret.String, [sha256.Size]byte, error) {
	random := make([]byte, 32)
	if _, err := rand.Read(random); err != nil {
		return secret.String{}, [sha256.Size]byte{}, err
	}
	token := secret.New(tokenPrefix + base64.RawURLEncoding.EncodeToString(random))
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
