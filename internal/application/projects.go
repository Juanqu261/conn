package application

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"
	"uuid"

	"github.com/Juanqu261/conn/internal/protocol"
)

// Errors returned by project-scoped use cases. Adapters map them to their
// own error contracts.
var (
	ErrNotFound      = errors.New("not found")
	ErrForbidden     = errors.New("the project role does not permit this operation")
	ErrConflict      = errors.New("the identity is already used by a different record")
	ErrStaleRevision = errors.New("the expected revision is no longer current")
	ErrInvalid       = errors.New("invalid input")
)

// InvalidError is input that matches its schema but breaks an application
// rule, such as a duplicate key.
type InvalidError struct {
	// Pointer is a JSON Pointer into the input document, or empty when
	// the problem is a request parameter such as an expected revision.
	Pointer string
	// Message is safe to show to the human who sent the input.
	Message string
}

func (e *InvalidError) Error() string { return e.Message }
func (e *InvalidError) Unwrap() error { return ErrInvalid }

// MaxEventPage bounds one page of project events.
const MaxEventPage = 500

// Result is a project-scoped value with the project event cursor it
// reflects.
type Result[T any] struct {
	Data      T
	AsOfEvent int64
}

// Store runs project-scoped use cases in database transactions.
type Store interface {
	// Write runs fn in one read-write transaction. State changes and
	// appended events commit together, and nothing commits when fn returns
	// an error.
	Write(ctx context.Context, fn func(Tx) error) error
	// Read runs fn in one read-only transaction over a single snapshot.
	Read(ctx context.Context, fn func(Tx) error) error
}

// Tx is the persistence available inside one Store transaction. Lookups
// return ErrNotFound for missing records.
type Tx interface {
	// Role returns the human's role in the Project, or ErrNotFound when the
	// Project does not exist or the human is not a member.
	Role(ctx context.Context, projectID, humanID uuid.UUID) (protocol.Role, error)
	HumanExists(ctx context.Context, humanID uuid.UUID) (bool, error)

	// InsertProject stores a Project with its owner membership and event
	// counter. It reports false, changing nothing, when the ID is taken.
	InsertProject(ctx context.Context, project protocol.Project) (bool, error)
	Project(ctx context.Context, projectID uuid.UUID) (protocol.Project, error)
	// ProjectsForHuman lists the Projects that contain humanID, in creation order.
	ProjectsForHuman(ctx context.Context, humanID uuid.UUID) ([]protocol.ProjectListEntry, error)

	// InsertMembership reports false, changing nothing, when the human is
	// already a member.
	InsertMembership(ctx context.Context, membership protocol.ProjectMembership) (bool, error)
	Membership(ctx context.Context, projectID, humanID uuid.UUID) (protocol.ProjectMembership, error)
	// ProjectMembers lists every Project membership with its human identity.
	ProjectMembers(ctx context.Context, projectID uuid.UUID) ([]protocol.ProjectMember, error)

	// InsertRepositoryConnection stores a Project repository connection. It
	// reports false, changing nothing, when any unique identity is taken.
	InsertRepositoryConnection(ctx context.Context, repository protocol.RepositoryConnection) (bool, error)
	RepositoryConnection(ctx context.Context, projectID, repositoryID uuid.UUID) (protocol.RepositoryConnection, error)
	RepositoryConnectionByRemote(ctx context.Context, projectID uuid.UUID, canonicalRemote string) (protocol.RepositoryConnection, error)
	RepositoryConnections(ctx context.Context, projectID uuid.UUID) ([]protocol.RepositoryConnection, error)

	// Participant resolves a human-bound operating identity. A missing local
	// session reference is matched as a deliberate null-equivalent value.
	Participant(ctx context.Context, projectID, humanID uuid.UUID, adapterKind, localSessionRef string) (protocol.Participant, error)
	ParticipantByID(ctx context.Context, projectID, participantID uuid.UUID) (protocol.Participant, error)
	InsertParticipant(ctx context.Context, participant protocol.Participant) (bool, error)

	// InsertIntentRevision atomically creates revision 1 or advances an active
	// caller-owned Intent from revision n-1. It reports false when either
	// expected state was no longer current.
	InsertIntentRevision(ctx context.Context, revision protocol.IntentRevision) (bool, error)
	Intent(ctx context.Context, projectID, intentID uuid.UUID) (protocol.IntentView, error)
	Intents(ctx context.Context, projectID uuid.UUID) ([]protocol.IntentView, error)
	ReleaseIntent(ctx context.Context, projectID, intentID, participantID uuid.UUID, expectedRevision int) (bool, error)

	// InsertSemanticClaims records immutable claims for an exact immutable
	// IntentRevision. Claim IDs are assigned by the application.
	InsertSemanticClaims(ctx context.Context, claims []protocol.SemanticClaim) error
	// AnalyzerReportAttached identifies an immutable local analysis report already
	// attached to this exact IntentRevision.
	AnalyzerReportAttached(ctx context.Context, projectID, intentID uuid.UUID, revision int) (bool, error)
	// InsertAnalyzerReport records only bounded analyzer diagnostics and report
	// metadata, never analyzer input or repository source.
	InsertAnalyzerReport(ctx context.Context, projectID, intentID uuid.UUID, revision int, report protocol.LocalAnalyzerReport, observedAt protocol.Timestamp) error
	// ActiveSemanticClaims returns claims belonging to each current active
	// Intent revision in a Project.
	ActiveSemanticClaims(ctx context.Context, projectID uuid.UUID) ([]protocol.SemanticClaim, error)
	// StaleOpenCollisionsForIntent transitions open or under-review
	// assessments involving this exact Intent revision to stale. It returns the
	// affected assessment IDs for append-only lifecycle events.
	StaleOpenCollisionsForIntent(ctx context.Context, projectID, intentID uuid.UUID, revision int, staleAt protocol.Timestamp) ([]uuid.UUID, error)
	// InsertCollisionAssessment stores one new assessment and its immutable
	// matching-claim references. It returns false when that exact assessment
	// identity was already recorded.
	InsertCollisionAssessment(ctx context.Context, assessment protocol.CollisionAssessment) (bool, error)

	// InsertObjectiveRevision stores objective and makes it current. Revision
	// 1 creates the Objective; revision n requires current revision n-1 in
	// the same Project. It reports false, changing nothing, otherwise.
	InsertObjectiveRevision(ctx context.Context, objective protocol.Objective) (bool, error)
	ObjectiveRevision(ctx context.Context, projectID, objectiveID uuid.UUID, revision int) (protocol.Objective, error)

	// InsertBlueprintRevision stores revision as proposed, with one cell
	// projection per charter, and makes it current. A pending proposal of
	// the same Blueprint becomes superseded. Revision 1 creates the
	// Blueprint; revision n requires current revision n-1 in the same
	// Project. It reports false, changing nothing, otherwise.
	// It returns ErrObjectiveHasBlueprint when revision 1 names an
	// Objective that another Blueprint already divides.
	InsertBlueprintRevision(ctx context.Context, revision protocol.BlueprintRevision) (bool, error)
	// ObjectiveBlueprint returns the Blueprint that divides an Objective and
	// its current revision, or ErrNotFound.
	ObjectiveBlueprint(ctx context.Context, projectID, objectiveID uuid.UUID) (uuid.UUID, int, error)
	// BlueprintObjective returns the Objective a Blueprint divides, or
	// ErrNotFound.
	BlueprintObjective(ctx context.Context, projectID, blueprintID uuid.UUID) (uuid.UUID, error)
	BlueprintRevision(ctx context.Context, projectID, blueprintID uuid.UUID, revision int) (protocol.BlueprintRevisionView, error)
	// LockBlueprintRevision returns the revision's status and serializes
	// charter decisions on it until the transaction ends.
	LockBlueprintRevision(ctx context.Context, projectID, blueprintID uuid.UUID, revision int) (protocol.BlueprintStatus, error)
	// CellAccountable returns the human accountable for a cell's charter in
	// one BlueprintRevision.
	CellAccountable(ctx context.Context, blueprintID uuid.UUID, revision int, cellID uuid.UUID) (uuid.UUID, error)
	CharterDecision(ctx context.Context, blueprintID uuid.UUID, revision int, cellID uuid.UUID) (protocol.CharterDecision, error)
	InsertCharterDecision(ctx context.Context, decision protocol.CharterDecision) error
	// UnacceptedCells counts the revision's cells without an acceptance.
	UnacceptedCells(ctx context.Context, blueprintID uuid.UUID, revision int) (int, error)
	// ActivateBlueprintRevision moves a proposed revision to active and the
	// Blueprint's previously active revision, if any, to superseded.
	ActivateBlueprintRevision(ctx context.Context, blueprintID uuid.UUID, revision int) error
	// RejectBlueprintRevision moves a proposed revision to rejected.
	RejectBlueprintRevision(ctx context.Context, blueprintID uuid.UUID, revision int) error

	// ObjectiveSummaries lists the Project's Objectives in creation order.
	ObjectiveSummaries(ctx context.Context, projectID uuid.UUID) ([]protocol.ObjectiveSummary, error)
	// BlueprintSummaries lists the Project's Blueprints in creation order,
	// each with its revisions in force and their cell decisions.
	BlueprintSummaries(ctx context.Context, projectID uuid.UUID) ([]protocol.BlueprintSummary, error)

	// CurrentObjectives returns every current immutable objective snapshot.
	CurrentObjectives(ctx context.Context, projectID uuid.UUID) ([]protocol.Objective, error)
	// ActiveBlueprint returns the current active blueprint revision, if any.
	ActiveBlueprint(ctx context.Context, projectID uuid.UUID) (protocol.BlueprintRevision, error)
	// OpenCollisions returns advisory assessments that remain open or under review.
	OpenCollisions(ctx context.Context, projectID uuid.UUID) ([]protocol.CollisionAssessment, error)
	// ResolutionDecisionRefs returns immutable decision identifiers visible in the Project.
	ResolutionDecisionRefs(ctx context.Context, projectID uuid.UUID) ([]uuid.UUID, error)
	// ActiveEvidenceReferences returns evidence for current Objective revisions.
	ActiveEvidenceReferences(ctx context.Context, projectID uuid.UUID) ([]protocol.EvidenceReference, error)

	// AppendEvent assigns the Project's next event sequence to event, stores
	// it, and returns the sequence.
	AppendEvent(ctx context.Context, event protocol.EventEnvelope) (int64, error)
	// LastEvent returns the Project's latest event sequence.
	LastEvent(ctx context.Context, projectID uuid.UUID) (int64, error)
	// Events returns at most limit events after the sequence, ascending.
	Events(ctx context.Context, projectID uuid.UUID, after int64, limit int) ([]protocol.EventEnvelope, error)
}

// Projects holds project-scoped use cases. Every operation authorizes the
// actor against the Project inside the same transaction as its effect.
type Projects struct {
	Store Store
	// Now defaults to the real clock.
	Now func() time.Time
}

// CreateProject creates a Project owned by the actor. Repeating the same
// input returns the existing Project without a new event.
func (p *Projects) CreateProject(ctx context.Context, actor uuid.UUID, input protocol.ProjectCreate) (Result[protocol.Project], error) {
	now := p.now()
	project := protocol.Project{
		ProjectID:    input.ProjectID,
		Name:         input.Name,
		OwnerHumanID: actor,
		CreatedAt:    protocol.NewTimestamp(now),
	}
	if err := validateRecord(project); err != nil {
		return Result[protocol.Project]{}, err
	}

	var result Result[protocol.Project]
	err := p.Store.Write(ctx, func(tx Tx) error {
		inserted, err := tx.InsertProject(ctx, project)
		if err != nil {
			return err
		}
		if !inserted {
			existing, err := tx.Project(ctx, project.ProjectID)
			if err != nil {
				return err
			}
			if existing.OwnerHumanID != actor || existing.Name != project.Name {
				return ErrConflict
			}
			result.Data = existing
			result.AsOfEvent, err = tx.LastEvent(ctx, project.ProjectID)
			return err
		}
		result.Data = project
		result.AsOfEvent, err = tx.AppendEvent(ctx, newEvent(project.ProjectID, project.ProjectID, actor, now, protocol.ProjectCreated{}))
		return err
	})
	return result, err
}

// Project returns a Project to one of its members.
func (p *Projects) Project(ctx context.Context, actor, projectID uuid.UUID) (Result[protocol.Project], error) {
	var result Result[protocol.Project]
	err := p.Store.Read(ctx, func(tx Tx) error {
		if _, err := tx.Role(ctx, projectID, actor); err != nil {
			return err
		}
		var err error
		if result.Data, err = tx.Project(ctx, projectID); err != nil {
			return err
		}
		result.AsOfEvent, err = tx.LastEvent(ctx, projectID)
		return err
	})
	return result, err
}

// ProjectsForHuman lists the Projects that the authenticated human may access.
func (p *Projects) ProjectsForHuman(ctx context.Context, actor uuid.UUID) (protocol.ProjectList, error) {
	var result protocol.ProjectList
	err := p.Store.Read(ctx, func(tx Tx) error {
		projects, err := tx.ProjectsForHuman(ctx, actor)
		if err != nil {
			return err
		}
		result.Projects = projects
		return nil
	})
	return result, err
}

// ProjectMembers returns every Project member to a current member.
func (p *Projects) ProjectMembers(ctx context.Context, actor, projectID uuid.UUID) (Result[protocol.ProjectMemberList], error) {
	var result Result[protocol.ProjectMemberList]
	err := p.Store.Read(ctx, func(tx Tx) error {
		if _, err := tx.Role(ctx, projectID, actor); err != nil {
			return err
		}
		members, err := tx.ProjectMembers(ctx, projectID)
		if err != nil {
			return err
		}
		result.Data = protocol.ProjectMemberList{ProjectID: projectID, Members: members}
		result.AsOfEvent, err = tx.LastEvent(ctx, projectID)
		return err
	})
	return result, err
}

// AddMember lets the owner add an existing Conn human as a member. Adding
// a current member returns the existing membership without a new event.
func (p *Projects) AddMember(ctx context.Context, actor, projectID, humanID uuid.UUID) (Result[protocol.ProjectMembership], error) {
	now := p.now()
	membership := protocol.ProjectMembership{
		ProjectID:      projectID,
		HumanID:        humanID,
		Role:           protocol.RoleMember,
		AddedByHumanID: actor,
		AddedAt:        protocol.NewTimestamp(now),
	}
	if err := validateRecord(membership); err != nil {
		return Result[protocol.ProjectMembership]{}, err
	}

	var result Result[protocol.ProjectMembership]
	err := p.Store.Write(ctx, func(tx Tx) error {
		if err := requireOwner(ctx, tx, projectID, actor); err != nil {
			return err
		}
		exists, err := tx.HumanExists(ctx, humanID)
		if err != nil {
			return err
		}
		if !exists {
			return &InvalidError{Pointer: "/human_id", Message: "No Conn human has this ID. The human must run conn login first."}
		}
		inserted, err := tx.InsertMembership(ctx, membership)
		if err != nil {
			return err
		}
		if !inserted {
			if result.Data, err = tx.Membership(ctx, projectID, humanID); err != nil {
				return err
			}
			result.AsOfEvent, err = tx.LastEvent(ctx, projectID)
			return err
		}
		result.Data = membership
		result.AsOfEvent, err = tx.AppendEvent(ctx, newEvent(projectID, projectID, actor, now,
			protocol.ProjectMemberAdded{HumanID: humanID, Role: protocol.RoleMember}))
		return err
	})
	return result, err
}

// ConnectRepository lets a Project owner add a credential-free repository
// scope. Repeating the same connection returns the stored record without a
// new event. A different connection using an occupied ID or remote conflicts.
func (p *Projects) ConnectRepository(ctx context.Context, actor, projectID uuid.UUID, input protocol.RepositoryConnectInput) (Result[protocol.RepositoryConnection], error) {
	now := p.now()
	repository := protocol.RepositoryConnection{
		RepositoryID:         input.RepositoryID,
		ProjectID:            projectID,
		HostKind:             input.HostKind,
		CanonicalRemote:      input.CanonicalRemote,
		HostRepositoryNodeID: input.HostRepositoryNodeID,
		DefaultBranch:        input.DefaultBranch,
		AnalysisEnabled:      input.AnalysisEnabled,
		CreatedByHumanID:     actor,
		CreatedAt:            protocol.NewTimestamp(now),
	}
	if err := validateRecord(repository); err != nil {
		return Result[protocol.RepositoryConnection]{}, err
	}

	var result Result[protocol.RepositoryConnection]
	err := p.Store.Write(ctx, func(tx Tx) error {
		if err := requireOwner(ctx, tx, projectID, actor); err != nil {
			return err
		}
		inserted, err := tx.InsertRepositoryConnection(ctx, repository)
		if err != nil {
			return err
		}
		if !inserted {
			existing, err := tx.RepositoryConnection(ctx, projectID, repository.RepositoryID)
			if errors.Is(err, ErrNotFound) {
				existing, err = tx.RepositoryConnectionByRemote(ctx, projectID, repository.CanonicalRemote)
			}
			if err != nil {
				return err
			}
			if !sameRepositoryContent(existing, repository) {
				return ErrConflict
			}
			result.Data = existing
			result.AsOfEvent, err = tx.LastEvent(ctx, projectID)
			return err
		}
		result.Data = repository
		result.AsOfEvent, err = tx.AppendEvent(ctx, newEvent(projectID, repository.RepositoryID, actor, now, protocol.RepositoryConnected{}))
		return err
	})
	return result, err
}

// RepositoryConnections lists the repository scopes visible to a Project
// member. The returned cursor reflects the same database snapshot.
func (p *Projects) RepositoryConnections(ctx context.Context, actor, projectID uuid.UUID) (Result[protocol.RepositoryConnectionList], error) {
	var result Result[protocol.RepositoryConnectionList]
	err := p.Store.Read(ctx, func(tx Tx) error {
		if _, err := tx.Role(ctx, projectID, actor); err != nil {
			return err
		}
		repositories, err := tx.RepositoryConnections(ctx, projectID)
		if err != nil {
			return err
		}
		result.Data = protocol.RepositoryConnectionList{ProjectID: projectID, Repositories: repositories}
		result.AsOfEvent, err = tx.LastEvent(ctx, projectID)
		return err
	})
	return result, err
}

// ResolveParticipant returns the caller's bounded operating identity for an
// adapter and optional opaque local-session reference. It is deliberately not
// an HTTP authority selector: callers derive actor from the Conn session.
func (p *Projects) ResolveParticipant(ctx context.Context, actor, projectID uuid.UUID, adapterKind, localSessionRef string) (Result[protocol.Participant], error) {
	now := p.now()
	participant := protocol.Participant{
		ParticipantID:   uuid.NewV7(),
		ProjectID:       projectID,
		HumanID:         actor,
		AdapterKind:     adapterKind,
		LocalSessionRef: localSessionRef,
		CreatedAt:       protocol.NewTimestamp(now),
	}
	if err := validateRecord(participant); err != nil {
		return Result[protocol.Participant]{}, err
	}

	var result Result[protocol.Participant]
	err := p.Store.Write(ctx, func(tx Tx) error {
		if _, err := tx.Role(ctx, projectID, actor); err != nil {
			return err
		}
		existing, err := tx.Participant(ctx, projectID, actor, adapterKind, localSessionRef)
		if err == nil {
			result.Data = existing
			result.AsOfEvent, err = tx.LastEvent(ctx, projectID)
			return err
		}
		if !errors.Is(err, ErrNotFound) {
			return err
		}
		inserted, err := tx.InsertParticipant(ctx, participant)
		if err != nil {
			return err
		}
		if !inserted {
			result.Data, err = tx.Participant(ctx, projectID, actor, adapterKind, localSessionRef)
			if err != nil {
				return err
			}
		} else {
			result.Data = participant
		}
		result.AsOfEvent, err = tx.LastEvent(ctx, projectID)
		return err
	})
	return result, err
}

func sameRepositoryContent(a, b protocol.RepositoryConnection) bool {
	return a.ProjectID == b.ProjectID &&
		a.HostKind == b.HostKind &&
		a.CanonicalRemote == b.CanonicalRemote &&
		a.HostRepositoryNodeID == b.HostRepositoryNodeID &&
		a.DefaultBranch == b.DefaultBranch &&
		a.AnalysisEnabled == b.AnalysisEnabled &&
		a.CreatedByHumanID == b.CreatedByHumanID
}

// CreateObjectiveRevision lets the owner append the next immutable revision
// of an Objective. expectedRevision is the latest revision the author saw,
// or 0 to create the Objective. A stale expectation fails with
// ErrStaleRevision. Repeating a request that already succeeded returns the
// stored revision without a new event.
func (p *Projects) CreateObjectiveRevision(ctx context.Context, actor, projectID uuid.UUID, input protocol.ObjectiveRevisionInput, expectedRevision int) (Result[protocol.Objective], error) {
	if expectedRevision < 0 {
		return Result[protocol.Objective]{}, &InvalidError{Message: "The expected revision cannot be negative."}
	}
	if err := uniqueKeys(input.KeyPoints, func(k protocol.KeyPoint) string { return k.Key }, "/key_points", "Key-point"); err != nil {
		return Result[protocol.Objective]{}, err
	}
	now := p.now()
	objective := protocol.Objective{
		ObjectiveID:        input.ObjectiveID,
		ProjectID:          projectID,
		Revision:           expectedRevision + 1,
		Key:                input.Key,
		Title:              input.Title,
		OverviewMarkdown:   input.OverviewMarkdown,
		AccountableHumanID: input.AccountableHumanID,
		SourceRefs:         input.SourceRefs,
		KeyPoints:          input.KeyPoints,
		CreatedAt:          protocol.NewTimestamp(now),
	}
	if err := validateRecord(objective); err != nil {
		return Result[protocol.Objective]{}, err
	}

	var result Result[protocol.Objective]
	err := p.Store.Write(ctx, func(tx Tx) error {
		if err := requireOwner(ctx, tx, projectID, actor); err != nil {
			return err
		}
		if _, err := tx.Role(ctx, projectID, input.AccountableHumanID); errors.Is(err, ErrNotFound) {
			return &InvalidError{Pointer: "/accountable_human_id", Message: "The accountable human is not a member of this project."}
		} else if err != nil {
			return err
		}

		inserted, err := tx.InsertObjectiveRevision(ctx, objective)
		if err != nil {
			return err
		}
		if !inserted {
			load := func(revision int) (protocol.Objective, error) {
				return tx.ObjectiveRevision(ctx, projectID, objective.ObjectiveID, revision)
			}
			same := func(existing protocol.Objective) bool { return sameObjectiveContent(existing, objective) }
			if result.Data, err = replay(objective.Revision, load, same); err != nil {
				return err
			}
			result.AsOfEvent, err = tx.LastEvent(ctx, projectID)
			return err
		}
		result.Data = objective
		result.AsOfEvent, err = tx.AppendEvent(ctx, newEvent(projectID, objective.ObjectiveID, actor, now,
			protocol.ObjectiveRevisionCreated{Revision: objective.Revision}))
		return err
	})
	return result, err
}

// replay explains why inserting a revision changed nothing. load reads a
// stored revision of the same aggregate within the Project. When the same
// request already succeeded, replay returns the stored revision.
func replay[T any](revision int, load func(revision int) (T, error), same func(T) bool) (T, error) {
	var zero T
	creating := revision == 1
	existing, err := load(revision)
	switch {
	case err == nil && same(existing):
		return existing, nil
	case err == nil && creating:
		return zero, ErrConflict
	case err == nil:
		return zero, ErrStaleRevision
	case !errors.Is(err, ErrNotFound):
		return zero, err
	case creating:
		// The ID belongs to an aggregate in another Project.
		return zero, ErrConflict
	}
	// The aggregate does not exist in this Project, or the expected revision
	// is ahead of the current one.
	if _, err := load(1); err != nil {
		return zero, err
	}
	return zero, ErrStaleRevision
}

// ObjectiveRevision returns one immutable Objective revision to a member.
func (p *Projects) ObjectiveRevision(ctx context.Context, actor, projectID, objectiveID uuid.UUID, revision int) (Result[protocol.Objective], error) {
	var result Result[protocol.Objective]
	err := p.Store.Read(ctx, func(tx Tx) error {
		if _, err := tx.Role(ctx, projectID, actor); err != nil {
			return err
		}
		var err error
		if result.Data, err = tx.ObjectiveRevision(ctx, projectID, objectiveID, revision); err != nil {
			return err
		}
		result.AsOfEvent, err = tx.LastEvent(ctx, projectID)
		return err
	})
	return result, err
}

// Status returns a member's view of the Project: its Objectives, and each
// Blueprint's revisions in force with their ratification progress.
func (p *Projects) Status(ctx context.Context, actor, projectID uuid.UUID) (Result[protocol.ProjectStatus], error) {
	var result Result[protocol.ProjectStatus]
	err := p.Store.Read(ctx, func(tx Tx) error {
		role, err := tx.Role(ctx, projectID, actor)
		if err != nil {
			return err
		}
		status := protocol.ProjectStatus{Role: role}
		if status.Project, err = tx.Project(ctx, projectID); err != nil {
			return err
		}
		if status.Objectives, err = tx.ObjectiveSummaries(ctx, projectID); err != nil {
			return err
		}
		if status.Blueprints, err = tx.BlueprintSummaries(ctx, projectID); err != nil {
			return err
		}
		result.Data = status
		result.AsOfEvent, err = tx.LastEvent(ctx, projectID)
		return err
	})
	return result, err
}

// Events returns to a member at most limit committed events after the
// cursor, in ascending sequence. AsOfEvent is the latest sequence in the
// same snapshot, so a client knows whether more events remain.
func (p *Projects) Events(ctx context.Context, actor, projectID uuid.UUID, after int64, limit int) (Result[[]protocol.EventEnvelope], error) {
	if after < 0 {
		return Result[[]protocol.EventEnvelope]{}, &InvalidError{Message: "The event cursor cannot be negative."}
	}
	if limit < 1 || limit > MaxEventPage {
		return Result[[]protocol.EventEnvelope]{}, &InvalidError{Message: fmt.Sprintf("The event limit must be between 1 and %d.", MaxEventPage)}
	}
	var result Result[[]protocol.EventEnvelope]
	err := p.Store.Read(ctx, func(tx Tx) error {
		if _, err := tx.Role(ctx, projectID, actor); err != nil {
			return err
		}
		var err error
		if result.Data, err = tx.Events(ctx, projectID, after, limit); err != nil {
			return err
		}
		result.AsOfEvent, err = tx.LastEvent(ctx, projectID)
		return err
	})
	return result, err
}

// requireOwner returns ErrNotFound for non-members and ErrForbidden for
// members who are not the owner.
func requireOwner(ctx context.Context, tx Tx, projectID, actor uuid.UUID) error {
	role, err := tx.Role(ctx, projectID, actor)
	if err != nil {
		return err
	}
	if role != protocol.RoleOwner {
		return ErrForbidden
	}
	return nil
}

// validateRecord checks a server-completed record against its schema.
// Every field that can fail came from the human's input.
func validateRecord[T protocol.Document](record T) error {
	if _, err := protocol.Encode(record); err != nil {
		return fmt.Errorf("%w: %w", ErrInvalid, err)
	}
	return nil
}

// uniqueKeys rejects the first item whose key repeats an earlier one.
func uniqueKeys[T any](items []T, key func(T) string, pointer, label string) error {
	seen := make(map[string]bool, len(items))
	for i, item := range items {
		k := key(item)
		if seen[k] {
			return &InvalidError{
				Pointer: fmt.Sprintf("%s/%d/key", pointer, i),
				Message: fmt.Sprintf("%s key %q is used more than once.", label, k),
			}
		}
		seen[k] = true
	}
	return nil
}

// sameObjectiveContent compares everything except the creation time.
func sameObjectiveContent(a, b protocol.Objective) bool {
	return a.ObjectiveID == b.ObjectiveID &&
		a.ProjectID == b.ProjectID &&
		a.Revision == b.Revision &&
		a.Key == b.Key &&
		a.Title == b.Title &&
		a.OverviewMarkdown == b.OverviewMarkdown &&
		a.AccountableHumanID == b.AccountableHumanID &&
		slices.Equal(a.SourceRefs, b.SourceRefs) &&
		slices.Equal(a.KeyPoints, b.KeyPoints)
}

func newEvent(projectID, entityID, actor uuid.UUID, at time.Time, payload protocol.EventPayload) protocol.EventEnvelope {
	return protocol.EventEnvelope{
		ProjectID:    projectID,
		EventID:      uuid.NewV7(),
		EntityID:     entityID,
		ActorHumanID: actor,
		OccurredAt:   protocol.NewTimestamp(at),
		Payload:      payload,
	}
}

// now returns the current time at PostgreSQL's microsecond precision.
func (p *Projects) now() time.Time {
	now := time.Now
	if p.Now != nil {
		now = p.Now
	}
	return now().UTC().Truncate(time.Microsecond)
}
