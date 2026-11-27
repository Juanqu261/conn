package application

import (
	"context"
	"time"

	"uuid"

	"github.com/Juanqu261/conn/internal/protocol"
)

// CIPrincipal is an authenticated CI credential. It reads one Project and
// previews collisions for one RepositoryConnection, and it can write nothing.
type CIPrincipal struct {
	CredentialID    uuid.UUID
	ProjectID       uuid.UUID
	RepositoryID    uuid.UUID
	CanonicalRemote string
	DefaultBranch   string
	ExpiresAt       time.Time
}

// Reader is the principal of a Project read: a member Human, or a CI
// credential bound to one Project. Readers never write.
type Reader struct {
	humanID uuid.UUID
	ci      *CIPrincipal
}

// HumanReader reads as an authenticated Human, who must be a member.
func HumanReader(humanID uuid.UUID) Reader { return Reader{humanID: humanID} }

// CIReader reads as a CI credential, which sees only its own Project.
func CIReader(principal CIPrincipal) Reader { return Reader{ci: &principal} }

// authorize returns ErrNotFound when the reader may not read the Project, so
// a hidden Project answers like a missing one.
func (r Reader) authorize(ctx context.Context, tx Tx, projectID uuid.UUID) error {
	if r.ci != nil {
		if r.ci.ProjectID != projectID {
			return ErrNotFound
		}
		return nil
	}
	_, err := tx.Role(ctx, projectID, r.humanID)
	return err
}

// Radar assembles one member-authorized coordination snapshot.
func (p *Projects) Radar(ctx context.Context, actor, projectID uuid.UUID) (Result[protocol.RadarView], error) {
	return p.RadarAs(ctx, HumanReader(actor), projectID)
}

// ConflictContext returns one bounded, member-authorized context pack.
func (p *Projects) ConflictContext(ctx context.Context, actor, projectID, collisionID uuid.UUID) (Result[protocol.ConflictContextPack], error) {
	return p.ConflictContextAs(ctx, HumanReader(actor), projectID, collisionID)
}

// Intent reads one current snapshot to a Project member.
func (p *Projects) Intent(ctx context.Context, actor, projectID, intentID uuid.UUID) (Result[protocol.IntentView], error) {
	return p.IntentAs(ctx, HumanReader(actor), projectID, intentID)
}

// Intents lists active and released current snapshots to a Project member.
func (p *Projects) Intents(ctx context.Context, actor, projectID uuid.UUID) (Result[protocol.IntentList], error) {
	return p.IntentsAs(ctx, HumanReader(actor), projectID)
}

// IntentEvidence lists one Intent's most recent evidence to a Project member.
func (p *Projects) IntentEvidence(ctx context.Context, actor, projectID, intentID uuid.UUID) (Result[protocol.EvidenceReferenceList], error) {
	return p.IntentEvidenceAs(ctx, HumanReader(actor), projectID, intentID)
}

// ObjectiveRevision returns one immutable Objective revision to a member.
func (p *Projects) ObjectiveRevision(ctx context.Context, actor, projectID, objectiveID uuid.UUID, revision int) (Result[protocol.Objective], error) {
	return p.ObjectiveRevisionAs(ctx, HumanReader(actor), projectID, objectiveID, revision)
}

// ProjectMembers returns every Project member to a current member.
func (p *Projects) ProjectMembers(ctx context.Context, actor, projectID uuid.UUID) (Result[protocol.ProjectMemberList], error) {
	return p.ProjectMembersAs(ctx, HumanReader(actor), projectID)
}

// Concepts returns one bounded, member-authorized catalog page.
func (p *Projects) Concepts(ctx context.Context, actor, projectID uuid.UUID, query ConceptQuery) (Result[protocol.ConceptList], error) {
	return p.ConceptsAs(ctx, HumanReader(actor), projectID, query)
}
