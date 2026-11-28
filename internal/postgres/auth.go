package postgres

import (
	"context"
	"crypto/sha256"
	"errors"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Juanqu261/conn/internal/application"
	"github.com/Juanqu261/conn/internal/protocol"
)

// AuthStore implements application.AuthStore.
type AuthStore struct {
	Pool *pgxpool.Pool
}

var _ application.AuthStore = AuthStore{}

func (s AuthStore) UpsertHuman(ctx context.Context, candidateID uuid.UUID, githubUserID int64, login string, now time.Time) (protocol.Human, error) {
	// The GitHub user ID is the stable key. A rename only updates the login.
	var human protocol.Human
	err := s.Pool.QueryRow(ctx, `
		INSERT INTO humans (human_id, github_user_id, github_login, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $4)
		ON CONFLICT (github_user_id) DO UPDATE
			SET github_login = EXCLUDED.github_login,
			    updated_at = GREATEST(humans.updated_at, EXCLUDED.updated_at)
		RETURNING human_id, github_user_id, github_login`,
		candidateID, githubUserID, login, now,
	).Scan(&human.HumanID, &human.GitHubUserID, &human.GitHubLogin)
	return human, err
}

func (s AuthStore) CreateSession(ctx context.Context, session application.NewSession) error {
	_, err := s.Pool.Exec(ctx, `
		INSERT INTO sessions (session_id, human_id, token_sha256, created_at, expires_at, kind)
		VALUES ($1, $2, $3, $4, $5, $6)`,
		session.SessionID, session.HumanID, session.TokenHash[:], session.CreatedAt, session.ExpiresAt, string(session.Kind))
	return err
}

func (s AuthStore) FindSession(ctx context.Context, tokenHash [sha256.Size]byte, kind application.SessionKind, now time.Time) (application.Session, error) {
	session := application.Session{Kind: kind}
	err := s.Pool.QueryRow(ctx, `
		SELECT s.session_id, s.expires_at, h.human_id, h.github_user_id, h.github_login
		FROM sessions s
		JOIN humans h USING (human_id)
		WHERE s.token_sha256 = $1 AND s.kind = $2 AND s.revoked_at IS NULL AND s.expires_at > $3`,
		tokenHash[:], string(kind), now,
	).Scan(&session.SessionID, &session.ExpiresAt,
		&session.Human.HumanID, &session.Human.GitHubUserID, &session.Human.GitHubLogin)
	if errors.Is(err, pgx.ErrNoRows) {
		return application.Session{}, application.ErrUnauthenticated
	}
	return session, err
}

func (s AuthStore) RevokeSession(ctx context.Context, sessionID uuid.UUID, now time.Time) error {
	_, err := s.Pool.Exec(ctx, `
		UPDATE sessions SET revoked_at = $2
		WHERE session_id = $1 AND revoked_at IS NULL`,
		sessionID, now)
	return err
}
