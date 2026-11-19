package protocol

import (
	"encoding/json"
	"uuid"

	"github.com/Juanqu261/conn/internal/secret"
)

const (
	HumanSchema        = "urn:conn:schema:v1:human"
	SessionGrantSchema = "urn:conn:schema:v1:session-grant"
	GitHubLoginSchema  = "urn:conn:schema:v1:github-login"
	ErrorSchema        = "urn:conn:schema:v1:error"
)

// Human is a GitHub-authenticated person.
type Human struct {
	HumanID      uuid.UUID `json:"human_id"`
	GitHubUserID int64     `json:"github_user_id"`
	GitHubLogin  string    `json:"github_login"`
}

func (Human) SchemaID() string { return HumanSchema }

// SessionGrant is the one response that carries a raw Conn session token.
type SessionGrant struct {
	Token     secret.String
	ExpiresAt Timestamp
	Human     Human
}

func (SessionGrant) SchemaID() string { return SessionGrantSchema }

type sessionGrantWire struct {
	Token     string    `json:"token"`
	ExpiresAt Timestamp `json:"expires_at"`
	Human     Human     `json:"human"`
}

// MarshalJSON reveals the token. This is the only protocol type that does.
func (g SessionGrant) MarshalJSON() ([]byte, error) {
	return json.Marshal(sessionGrantWire{Token: g.Token.Reveal(), ExpiresAt: g.ExpiresAt, Human: g.Human})
}

func (g *SessionGrant) UnmarshalJSON(data []byte) error {
	var wire sessionGrantWire
	if err := strictUnmarshal(data, &wire); err != nil {
		return err
	}
	*g = SessionGrant{Token: secret.New(wire.Token), ExpiresAt: wire.ExpiresAt, Human: wire.Human}
	return nil
}

// GitHubLogin is the public configuration for GitHub device authorization.
type GitHubLogin struct {
	ClientID string `json:"client_id"`
}

func (GitHubLogin) SchemaID() string { return GitHubLoginSchema }

// Error is the body of every unsuccessful /v1 response.
type Error struct {
	Error ErrorDetail `json:"error"`
}

func (Error) SchemaID() string { return ErrorSchema }

// ErrorDetail carries a stable code and a safe human-readable message.
// Pointer locates the offending request field of an invalid_input error.
type ErrorDetail struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Pointer string `json:"pointer,omitempty"`
}
