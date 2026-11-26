package review

import (
	"context"

	"uuid"

	"github.com/Juanqu261/conn/internal/hookstate"
	"github.com/Juanqu261/conn/internal/httpclient"
	"github.com/Juanqu261/conn/internal/protocol"
	"github.com/Juanqu261/conn/internal/repositorygit"
)

// Request selects one packet. Worktree is a trusted, resolved local
// directory; an empty Base uses the Intent's base_revision_ref.
type Request struct {
	ProjectID uuid.UUID
	IntentID  uuid.UUID
	Worktree  string
	Base      string
	// Discover reads the worktree's repository identity. Nil uses
	// repositorygit.Discover.
	Discover func(context.Context, string) (repositorygit.Metadata, error)
}

// Generate loads the records, reads local Git facts and written paths, and
// builds the packet. Git facts are used only when the worktree's origin
// matches the Intent's repository. It writes nothing anywhere.
func Generate(ctx context.Context, client *httpclient.Client, request Request) (protocol.ReviewPacket, error) {
	records, err := Load(ctx, client, request.ProjectID, request.IntentID, LoadOptions{Review: true})
	if err != nil {
		return protocol.ReviewPacket{}, err
	}
	intent := records.Intent()
	base := request.Base
	if base == "" {
		base = intent.BaseRevisionRef
	}
	git := GitFacts{Notes: []string{"No local worktree was available, so Git facts are unknown."}}
	written := map[string]bool{}
	if request.Worktree != "" {
		if matchesRepository(ctx, request, records.Radar.Repositories, intent) {
			git = ReadGit(ctx, request.Worktree, base)
			written = WrittenPaths(ctx, request.Worktree)
		} else {
			git = GitFacts{Notes: []string{"The local worktree does not match the Intent's repository connection, so Git facts are unknown."}}
		}
	}
	return BuildPacket(PacketInput{Records: records, Git: git, Written: written})
}

func matchesRepository(ctx context.Context, request Request, repositories []protocol.RepositoryConnection, intent protocol.IntentRevision) bool {
	if intent.RepositoryID == nil {
		return false
	}
	discover := request.Discover
	if discover == nil {
		discover = repositorygit.Discover
	}
	metadata, err := discover(ctx, request.Worktree)
	if err != nil {
		return false
	}
	for _, repository := range repositories {
		if repository.RepositoryID == *intent.RepositoryID {
			return repository.CanonicalRemote == metadata.CanonicalRemote
		}
	}
	return false
}

// WrittenPaths returns the repository-relative paths that Conn-connected
// agent sessions recorded writing in this worktree.
func WrittenPaths(ctx context.Context, worktree string) map[string]bool {
	written := map[string]bool{}
	store, err := hookstate.Open(ctx, worktree)
	if err != nil {
		return written
	}
	for _, entry := range store.WrittenPaths() {
		written[cleanPath(entry.Path)] = true
	}
	return written
}
