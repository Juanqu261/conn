package application

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"time"

	"uuid"

	"github.com/Juanqu261/conn/internal/protocol"
	"github.com/Juanqu261/conn/internal/secret"
)

// ErrHumanDirectOnly means an agent surface asked for an operation that only
// a human-direct surface, such as the Conn CLI, may perform.
var ErrHumanDirectOnly = errors.New("only a human-direct surface may perform this operation")

// ciTouchInterval throttles last-use updates of one CI credential.
const ciTouchInterval = time.Minute

// CreateCICredential lets a Project owner create a CI credential bound to one
// RepositoryConnection. agentSurface reports that the request came from an
// MCP adapter, which may never create credentials. The raw token exists only
// in the returned grant; Conn stores its SHA-256 hash.
func (p *Projects) CreateCICredential(ctx context.Context, actor, projectID uuid.UUID, input protocol.CICredentialCreate, agentSurface bool) (Result[protocol.CICredentialGrant], error) {
	if agentSurface {
		return Result[protocol.CICredentialGrant]{}, ErrHumanDirectOnly
	}
	if err := validateRecord(input); err != nil {
		return Result[protocol.CICredentialGrant]{}, err
	}
	now := p.now()
	token, hash, err := newCIToken()
	if err != nil {
		return Result[protocol.CICredentialGrant]{}, err
	}
	credential := protocol.CICredential{
		CredentialID: uuid.NewV7(), ProjectID: projectID, RepositoryID: input.RepositoryID, Label: input.Label,
		CreatedByHumanID: actor, CreatedAt: protocol.NewTimestamp(now),
		ExpiresAt: protocol.NewTimestamp(now.Add(time.Duration(input.ExpiresInDays) * 24 * time.Hour)),
	}
	if err := validateRecord(credential); err != nil {
		return Result[protocol.CICredentialGrant]{}, err
	}
	var result Result[protocol.CICredentialGrant]
	err = p.Store.Write(ctx, func(tx Tx) error {
		if err := requireOwner(ctx, tx, projectID, actor); err != nil {
			return err
		}
		if _, err := tx.RepositoryConnection(ctx, projectID, input.RepositoryID); errors.Is(err, ErrNotFound) {
			return &InvalidError{Pointer: "/repository_id", Message: "The repository is not connected to this Project."}
		} else if err != nil {
			return err
		}
		if err := tx.InsertCICredential(ctx, credential, hash); err != nil {
			return err
		}
		result.Data = protocol.CICredentialGrant{Token: token, Credential: credential}
		result.AsOfEvent, err = tx.AppendEvent(ctx, newEvent(projectID, credential.CredentialID, actor, now, protocol.CICredentialCreated{RepositoryID: input.RepositoryID}))
		return err
	})
	return result, err
}

// CICredentials lists a Project's CI credentials, newest first, to its owner.
func (p *Projects) CICredentials(ctx context.Context, actor, projectID uuid.UUID) (Result[protocol.CICredentialList], error) {
	var result Result[protocol.CICredentialList]
	err := p.Store.Read(ctx, func(tx Tx) error {
		if err := requireOwner(ctx, tx, projectID, actor); err != nil {
			return err
		}
		credentials, err := tx.CICredentials(ctx, projectID, protocol.MaxCICredentialsListed)
		if err != nil {
			return err
		}
		result.Data = protocol.CICredentialList{ProjectID: projectID, Credentials: credentials}
		result.AsOfEvent, err = tx.LastEvent(ctx, projectID)
		return err
	})
	return result, err
}

// RevokeCICredential lets the owner revoke a credential immediately.
// Revoking a revoked credential returns it without a new event.
func (p *Projects) RevokeCICredential(ctx context.Context, actor, projectID, credentialID uuid.UUID) (Result[protocol.CICredential], error) {
	now := p.now()
	var result Result[protocol.CICredential]
	err := p.Store.Write(ctx, func(tx Tx) error {
		if err := requireOwner(ctx, tx, projectID, actor); err != nil {
			return err
		}
		changed, err := tx.RevokeCICredential(ctx, projectID, credentialID, now)
		if err != nil {
			return err
		}
		if result.Data, err = tx.CICredential(ctx, projectID, credentialID); err != nil {
			return err
		}
		if !changed {
			result.AsOfEvent, err = tx.LastEvent(ctx, projectID)
			return err
		}
		result.AsOfEvent, err = tx.AppendEvent(ctx, newEvent(projectID, credentialID, actor, now, protocol.CICredentialRevoked{RepositoryID: result.Data.RepositoryID}))
		return err
	})
	return result, err
}

// AuthenticateCI resolves a CI token to its principal, or returns
// ErrUnauthenticated when it is unknown, expired, or revoked. Last use is
// recorded at most once per minute.
func (p *Projects) AuthenticateCI(ctx context.Context, token secret.String) (CIPrincipal, error) {
	hash := HashToken(token)
	now := p.now()
	var principal CIPrincipal
	err := p.Store.Write(ctx, func(tx Tx) error {
		credential, repository, err := tx.CICredentialByTokenHash(ctx, hash)
		if errors.Is(err, ErrNotFound) {
			return ErrUnauthenticated
		}
		if err != nil {
			return err
		}
		if credential.RevokedAt != nil || !now.Before(credential.ExpiresAt.Time()) {
			return ErrUnauthenticated
		}
		if err := tx.TouchCICredential(ctx, credential.CredentialID, now, now.Add(-ciTouchInterval)); err != nil {
			return err
		}
		principal = CIPrincipal{
			CredentialID: credential.CredentialID, ProjectID: credential.ProjectID, RepositoryID: credential.RepositoryID,
			CanonicalRemote: repository.CanonicalRemote, DefaultBranch: repository.DefaultBranch, ExpiresAt: credential.ExpiresAt.Time(),
		}
		return nil
	})
	return principal, err
}

// CICredentialContext describes what an authenticated CI credential may read.
func (p *Projects) CICredentialContext(ctx context.Context, principal CIPrincipal) (Result[protocol.CICredentialContext], error) {
	var result Result[protocol.CICredentialContext]
	err := p.Store.Read(ctx, func(tx Tx) error {
		project, err := tx.Project(ctx, principal.ProjectID)
		if err != nil {
			return err
		}
		result.Data = protocol.CICredentialContext{
			CredentialID: principal.CredentialID, ProjectID: principal.ProjectID, ProjectName: project.Name,
			RepositoryID: principal.RepositoryID, CanonicalRemote: principal.CanonicalRemote,
			DefaultBranch: principal.DefaultBranch, ExpiresAt: protocol.NewTimestamp(principal.ExpiresAt),
		}
		result.AsOfEvent, err = tx.LastEvent(ctx, principal.ProjectID)
		return err
	})
	return result, err
}

func newCIToken() (secret.String, [32]byte, error) {
	random := make([]byte, 32)
	if _, err := rand.Read(random); err != nil {
		return secret.String{}, [32]byte{}, err
	}
	token := secret.New(protocol.CITokenPrefix + base64.RawURLEncoding.EncodeToString(random))
	return token, HashToken(token), nil
}
