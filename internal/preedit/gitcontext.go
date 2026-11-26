// Package preedit holds the local, worktree-bound steps of Conn's pre-edit
// loop that the MCP adapter and CLI share: filling an Intent's Git context
// and suggesting claims for planned paths. It calls the authenticated backend
// client and the bounded local analyzer, and never touches the database.
package preedit

import (
	"context"
	"errors"
	"fmt"
	"uuid"

	"github.com/Juanqu261/conn/internal/httpclient"
	"github.com/Juanqu261/conn/internal/protocol"
	"github.com/Juanqu261/conn/internal/repositorygit"
)

// FilledField reports one Intent field filled from the trusted worktree.
type FilledField struct {
	Field string `json:"field"`
	Value string `json:"value"`
}

// GitContext reports what FillGitContext filled and why anything stayed empty.
type GitContext struct {
	Filled []FilledField `json:"filled_fields"`
	Notes  []string      `json:"notes"`
	// Repository is the RepositoryConnection that matches the worktree, when
	// the Intent names it.
	Repository *protocol.RepositoryConnection `json:"-"`
}

// Discover reads normalized credential-free repository identity from a
// worktree. Nil uses repositorygit.Discover.
type Discover func(context.Context, string) (repositorygit.Metadata, error)

func defaultDiscover(ctx context.Context, worktree string) (repositorygit.Metadata, error) {
	return repositorygit.Discover(ctx, worktree)
}

// FillGitContext fills missing repository_id, branch_ref, and
// base_revision_ref values from the trusted worktree. Explicit input always
// wins. Missing or ambiguous Git metadata leaves fields empty with a note.
func FillGitContext(ctx context.Context, client *httpclient.Client, projectID uuid.UUID, worktree string, discover Discover, input *protocol.IntentInput) GitContext {
	result := GitContext{Filled: []FilledField{}, Notes: []string{}}
	if discover == nil {
		discover = repositorygit.Discover
	}
	metadata, err := discover(ctx, worktree)
	if err != nil {
		result.Notes = append(result.Notes, "Git context was not filled: the worktree has no usable origin remote.")
		return result
	}
	repositories, err := client.RepositoryConnections(ctx, projectID)
	if err != nil {
		result.Notes = append(result.Notes, "Git context was not filled: the Project repository connections could not be read.")
		return result
	}
	matches := make([]protocol.RepositoryConnection, 0, 1)
	for _, repository := range repositories.Data.Repositories {
		if repository.CanonicalRemote == metadata.CanonicalRemote {
			matches = append(matches, repository)
		}
	}
	if input.RepositoryID == nil {
		switch len(matches) {
		case 0:
			result.Notes = append(result.Notes, fmt.Sprintf("repository_id was not filled: no RepositoryConnection in this Project matches %s.", metadata.CanonicalRemote))
			return result
		case 1:
			id := matches[0].RepositoryID
			input.RepositoryID = &id
			result.Filled = append(result.Filled, FilledField{Field: "repository_id", Value: id.String()})
		default:
			result.Notes = append(result.Notes, "repository_id was not filled: several RepositoryConnections match the local remote.")
			return result
		}
	}
	var repository *protocol.RepositoryConnection
	for i := range matches {
		if matches[i].RepositoryID == *input.RepositoryID {
			repository = &matches[i]
		}
	}
	if repository == nil {
		result.Notes = append(result.Notes, "branch_ref and base_revision_ref were not filled: the local worktree does not match the Intent repository.")
		return result
	}
	result.Repository = repository
	if input.BranchRef != "" && input.BaseRevisionRef != "" {
		return result
	}
	branch, branchErr := repositorygit.CurrentBranch(ctx, worktree)
	if input.BranchRef == "" {
		if branchErr != nil {
			result.Notes = append(result.Notes, "branch_ref was not filled: HEAD is detached or the branch name cannot be represented.")
		} else {
			input.BranchRef = branch
			result.Filled = append(result.Filled, FilledField{Field: "branch_ref", Value: branch})
		}
	}
	if input.BaseRevisionRef == "" {
		base, err := BaseRevision(ctx, worktree, repository.DefaultBranch)
		if err != nil {
			result.Notes = append(result.Notes, "base_revision_ref was not filled: "+err.Error()+".")
		} else {
			input.BaseRevisionRef = base
			result.Filled = append(result.Filled, FilledField{Field: "base_revision_ref", Value: base})
		}
	}
	return result
}

// BaseRevision computes an Intent base revision from the worktree: HEAD on
// the default branch, otherwise the merge base with origin/<default>.
func BaseRevision(ctx context.Context, worktree, configuredDefault string) (string, error) {
	defaultBranch, err := repositorygit.DefaultBranch(ctx, worktree, configuredDefault)
	if err != nil {
		return "", errors.New("the repository default branch is unknown; set default_branch on the RepositoryConnection or origin/HEAD")
	}
	branch, _ := repositorygit.CurrentBranch(ctx, worktree)
	base, err := repositorygit.BaseRevision(ctx, worktree, defaultBranch, branch)
	if err != nil {
		return "", fmt.Errorf("no merge base with origin/%s is available", defaultBranch)
	}
	return base, nil
}
