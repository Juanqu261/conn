package review

import (
	"context"
	"errors"

	"uuid"

	"github.com/Juanqu261/conn/internal/httpclient"
	"github.com/Juanqu261/conn/internal/protocol"
)

// MaxCollisionDetails bounds the collisions whose decision and proposals a
// packet loads.
const MaxCollisionDetails = 10

// ErrStaleContext means the Project changed between the reads that build one
// view, twice in a row.
var ErrStaleContext = errors.New("the Project changed while Conn records were being read; retry from the latest cursor")

var errCursorMismatch = errors.New("cursor mismatch")

// LoadOptions selects what Load reads beyond work context and Radar.
type LoadOptions struct {
	// After and UpdateLimit request a bounded page of later events.
	After       *int64
	UpdateLimit int
	// ConflictContexts loads the context pack of every collision on the
	// current Intent revision, as handoffs need.
	ConflictContexts bool
	// Review loads the Intent's evidence, Project members, and the decision
	// and proposal count of up to MaxCollisionDetails collisions.
	Review bool
}

// Records are authoritative Conn records read at one Project event cursor.
type Records struct {
	Work       protocol.WorkContext
	Radar      protocol.RadarView
	Collisions []protocol.CollisionAssessment
	Contexts   map[uuid.UUID]protocol.ConflictContextPack
	Evidence   []protocol.EvidenceReference
	Logins     map[uuid.UUID]string
	Proposals  map[uuid.UUID]int
	Decisions  map[uuid.UUID]protocol.ResolutionDecision
	AsOfEvent  int64
}

// Intent returns the exact focused Intent revision.
func (r Records) Intent() protocol.IntentRevision { return *r.Work.Focus.SelectedIntent }

// Source is the authoritative reads Load needs. The CLI and MCP adapter use
// *httpclient.Client; conn server reads the same use cases in process.
type Source interface {
	WorkContext(ctx context.Context, projectID, objectiveID, intentID uuid.UUID, after *int64, limit int) (httpclient.Result[protocol.WorkContext], error)
	Radar(ctx context.Context, projectID uuid.UUID) (httpclient.Result[protocol.RadarView], error)
	ConflictContext(ctx context.Context, projectID, collisionID uuid.UUID) (httpclient.Result[protocol.ConflictContextPack], error)
	IntentEvidence(ctx context.Context, projectID, intentID uuid.UUID) (httpclient.Result[protocol.EvidenceReferenceList], error)
	ProjectMembers(ctx context.Context, projectID uuid.UUID) (httpclient.Result[protocol.ProjectMemberList], error)
	ResolutionProposals(ctx context.Context, projectID, collisionID uuid.UUID) (httpclient.Result[protocol.ResolutionProposalList], error)
	ResolutionDecision(ctx context.Context, projectID, decisionID uuid.UUID) (httpclient.Result[protocol.ResolutionDecision], error)
}

// Load reads the records for one Intent and confirms that every response
// reflects the same Project event cursor. When the Project changes between
// reads it retries once, then returns ErrStaleContext.
func Load(ctx context.Context, client Source, projectID, intentID uuid.UUID, options LoadOptions) (Records, error) {
	for attempt := 0; attempt < 2; attempt++ {
		records, err := loadOnce(ctx, client, projectID, intentID, options)
		if errors.Is(err, errCursorMismatch) {
			continue
		}
		return records, err
	}
	return Records{}, ErrStaleContext
}

func loadOnce(ctx context.Context, client Source, projectID, intentID uuid.UUID, options LoadOptions) (Records, error) {
	limit := options.UpdateLimit
	if options.After == nil {
		limit = 1
	}
	work, err := client.WorkContext(ctx, projectID, uuid.UUID{}, intentID, options.After, limit)
	if err != nil {
		return Records{}, err
	}
	if work.Data.Focus == nil || work.Data.Focus.SelectedIntent == nil || work.Data.Focus.SelectedIntent.IntentID != intentID {
		return Records{}, errors.New("the selected Intent does not resolve to one exact focus")
	}
	asOf := work.AsOfEvent
	same := func(other int64) error {
		if other != asOf {
			return errCursorMismatch
		}
		return nil
	}
	radar, err := client.Radar(ctx, projectID)
	if err != nil {
		return Records{}, err
	}
	if err := same(radar.AsOfEvent); err != nil {
		return Records{}, err
	}
	if err := same(radar.Data.AsOfEvent); err != nil {
		return Records{}, err
	}
	intent := *work.Data.Focus.SelectedIntent
	records := Records{
		Work: work.Data, Radar: radar.Data, AsOfEvent: asOf,
		Collisions: IntentCollisions(work.Data.RelatedCollisions, intentID, intent.Revision),
		Contexts:   map[uuid.UUID]protocol.ConflictContextPack{}, Logins: map[uuid.UUID]string{},
		Proposals: map[uuid.UUID]int{}, Decisions: map[uuid.UUID]protocol.ResolutionDecision{},
	}
	if options.ConflictContexts {
		for _, collision := range records.Collisions {
			pack, err := client.ConflictContext(ctx, projectID, collision.CollisionID)
			if err != nil {
				return Records{}, err
			}
			if err := same(pack.AsOfEvent); err != nil {
				return Records{}, err
			}
			records.Contexts[collision.CollisionID] = pack.Data
		}
	}
	if !options.Review {
		return records, nil
	}
	evidence, err := client.IntentEvidence(ctx, projectID, intentID)
	if err != nil {
		return Records{}, err
	}
	if err := same(evidence.AsOfEvent); err != nil {
		return Records{}, err
	}
	records.Evidence = evidence.Data.Evidence
	members, err := client.ProjectMembers(ctx, projectID)
	if err != nil {
		return Records{}, err
	}
	if err := same(members.AsOfEvent); err != nil {
		return Records{}, err
	}
	for _, member := range members.Data.Members {
		records.Logins[member.Human.HumanID] = member.Human.GitHubLogin
	}
	for i, collision := range records.Collisions {
		if i == MaxCollisionDetails {
			break
		}
		proposals, err := client.ResolutionProposals(ctx, projectID, collision.CollisionID)
		if err != nil {
			return Records{}, err
		}
		if err := same(proposals.AsOfEvent); err != nil {
			return Records{}, err
		}
		records.Proposals[collision.CollisionID] = len(proposals.Data.Proposals)
		if collision.DecisionRef == nil {
			continue
		}
		decision, err := client.ResolutionDecision(ctx, projectID, *collision.DecisionRef)
		if err != nil {
			return Records{}, err
		}
		if err := same(decision.AsOfEvent); err != nil {
			return Records{}, err
		}
		records.Decisions[decision.Data.DecisionID] = decision.Data
	}
	return records, nil
}

// IntentCollisions returns the collisions that involve one exact Intent
// revision.
func IntentCollisions(collisions []protocol.CollisionAssessment, intentID uuid.UUID, revision int) []protocol.CollisionAssessment {
	result := make([]protocol.CollisionAssessment, 0, len(collisions))
	for _, collision := range collisions {
		if (collision.LeftIntentID == intentID && collision.LeftIntentRevision == revision) ||
			(collision.RightIntentID == intentID && collision.RightIntentRevision == revision) {
			result = append(result, collision)
		}
	}
	return result
}
