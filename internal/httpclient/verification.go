package httpclient

import (
	"context"
	"fmt"
	"net/http"

	"uuid"

	"github.com/Juanqu261/conn/internal/protocol"
)

// PreviewCollisions returns the collisions candidate claims would open
// against active Intent revisions. The backend persists nothing.
func (c *Client) PreviewCollisions(ctx context.Context, projectID uuid.UUID, input protocol.CollisionPreviewInput) (Result[protocol.CollisionPreview], error) {
	if input.Claims == nil {
		input.Claims = []protocol.SemanticClaimCandidate{}
	}
	return post[protocol.CollisionPreview](ctx, c, projectPath(projectID)+"/collision-previews", input)
}

// RecordCombinationCheck records an advisory combination check as the
// caller's participant.
func (c *Client) RecordCombinationCheck(ctx context.Context, projectID, collisionID uuid.UUID, input protocol.CombinationCheckInput) (Result[protocol.CombinationCheck], error) {
	return post[protocol.CombinationCheck](ctx, c, fmt.Sprintf("%s/collisions/%s/combination-checks", projectPath(projectID), collisionID), input)
}

// CreateCICredential creates a CI credential. Only the returned grant ever
// carries its token.
func (c *Client) CreateCICredential(ctx context.Context, projectID uuid.UUID, input protocol.CICredentialCreate) (Result[protocol.CICredentialGrant], error) {
	return post[protocol.CICredentialGrant](ctx, c, projectPath(projectID)+"/ci-credentials", input)
}

// CICredentials lists a Project's CI credentials to its owner.
func (c *Client) CICredentials(ctx context.Context, projectID uuid.UUID) (Result[protocol.CICredentialList], error) {
	return read[protocol.CICredentialList](ctx, c, projectPath(projectID)+"/ci-credentials")
}

// RevokeCICredential revokes one CI credential immediately.
func (c *Client) RevokeCICredential(ctx context.Context, projectID, credentialID uuid.UUID) (Result[protocol.CICredential], error) {
	body, err := c.send(ctx, http.MethodPost, fmt.Sprintf("%s/ci-credentials/%s/revoke", projectPath(projectID), credentialID), c.token, nil)
	if err != nil {
		return Result[protocol.CICredential]{}, err
	}
	return decodeResult[protocol.CICredential](body)
}

// CICredentialContext describes the Project and repository a CI credential
// may read.
func (c *Client) CICredentialContext(ctx context.Context) (Result[protocol.CICredentialContext], error) {
	return read[protocol.CICredentialContext](ctx, c, "/v1/ci/credential")
}

// Insights reads Project-level insights for the last sinceDays days.
func (c *Client) Insights(ctx context.Context, projectID uuid.UUID, sinceDays int) (Result[protocol.ProjectInsights], error) {
	return read[protocol.ProjectInsights](ctx, c, fmt.Sprintf("%s/insights?since_days=%d", projectPath(projectID), sinceDays))
}

// ImportGitHubIssueObjective records a human-reviewed Objective revision
// imported from a GitHub issue. No GitHub token is sent.
func (c *Client) ImportGitHubIssueObjective(ctx context.Context, projectID uuid.UUID, expectedRevision int, input protocol.ObjectiveImportInput) (Result[protocol.Objective], error) {
	path := fmt.Sprintf("%s/objective-imports/github-issue?expected_revision=%d", projectPath(projectID), expectedRevision)
	return post[protocol.Objective](ctx, c, path, input)
}
