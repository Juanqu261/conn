package protocol

import (
	"encoding/json"
	"fmt"
	"uuid"
)

// EventType names one kind of committed authoritative mutation.
type EventType string

const (
	EventProjectCreated             EventType = "project.created"
	EventProjectMemberAdded         EventType = "project.member_added"
	EventObjectiveRevisionCreated   EventType = "objective.revision_created"
	EventBlueprintRevisionProposed  EventType = "blueprint.revision_proposed"
	EventBlueprintRevisionActivated EventType = "blueprint.revision_activated"
	EventBlueprintRevisionRejected  EventType = "blueprint.revision_rejected"
	EventCharterAccepted            EventType = "charter.accepted"
	EventCharterRejected            EventType = "charter.rejected"
	EventRepositoryConnected        EventType = "repository.connected"
	EventIntentPublished            EventType = "intent.published"
	EventIntentRevised              EventType = "intent.revised"
	EventIntentReleased             EventType = "intent.released"
	EventClaimsObserved             EventType = "claims.observed"
	EventCollisionOpened            EventType = "collision.opened"
	EventCollisionUpdated           EventType = "collision.updated"
	EventResolutionProposed         EventType = "resolution.proposed"
	EventResolutionDecided          EventType = "resolution.decided"
	EventEvidenceAttached           EventType = "evidence.attached"
	EventObjectiveImported          EventType = "objective.imported"
)

// EntityType names the kind of entity an event's EntityID refers to.
type EntityType string

const (
	EntityProject            EntityType = "project"
	EntityObjective          EntityType = "objective"
	EntityBlueprint          EntityType = "blueprint"
	EntityCharterDecision    EntityType = "charter_decision"
	EntityRepository         EntityType = "repository"
	EntityIntent             EntityType = "intent"
	EntitySemanticClaim      EntityType = "semantic_claim"
	EntityCollision          EntityType = "collision"
	EntityResolutionProposal EntityType = "resolution_proposal"
	EntityResolutionDecision EntityType = "resolution_decision"
	EntityEvidenceReference  EntityType = "evidence_reference"
)

// EventPayload is implemented by exactly one payload type per EventType.
// The payload type determines the envelope's event and entity types, so an
// envelope cannot pair a payload with the wrong event type.
type EventPayload interface {
	EventType() EventType
	EntityType() EntityType
}

// EventEnvelope is one append-only event within a Project.
type EventEnvelope struct {
	ProjectID     uuid.UUID
	EventSequence int64
	EventID       uuid.UUID
	EntityID      uuid.UUID
	ActorHumanID  uuid.UUID
	OccurredAt    Timestamp
	Payload       EventPayload
}

func (EventEnvelope) SchemaID() string { return EventEnvelopeSchema }

// EventType returns the payload's event type.
func (e EventEnvelope) EventType() EventType { return e.Payload.EventType() }

// EntityType returns the payload's entity type.
func (e EventEnvelope) EntityType() EntityType { return e.Payload.EntityType() }

type eventWire struct {
	ProjectID     uuid.UUID       `json:"project_id"`
	EventSequence int64           `json:"event_sequence"`
	EventID       uuid.UUID       `json:"event_id"`
	EventType     EventType       `json:"event_type"`
	EntityType    EntityType      `json:"entity_type"`
	EntityID      uuid.UUID       `json:"entity_id"`
	ActorHumanID  uuid.UUID       `json:"actor_human_id"`
	OccurredAt    Timestamp       `json:"occurred_at"`
	Payload       json.RawMessage `json:"payload"`
}

func (e EventEnvelope) MarshalJSON() ([]byte, error) {
	if e.Payload == nil {
		return nil, fmt.Errorf("event payload is unset")
	}
	payload, err := json.Marshal(e.Payload)
	if err != nil {
		return nil, err
	}
	return json.Marshal(eventWire{
		ProjectID:     e.ProjectID,
		EventSequence: e.EventSequence,
		EventID:       e.EventID,
		EventType:     e.Payload.EventType(),
		EntityType:    e.Payload.EntityType(),
		EntityID:      e.EntityID,
		ActorHumanID:  e.ActorHumanID,
		OccurredAt:    e.OccurredAt,
		Payload:       payload,
	})
}

func (e *EventEnvelope) UnmarshalJSON(data []byte) error {
	var wire eventWire
	if err := strictUnmarshal(data, &wire); err != nil {
		return err
	}
	decode, ok := payloadDecoders[wire.EventType]
	if !ok {
		return fmt.Errorf("unknown event type %q", wire.EventType)
	}
	payload, err := decode(wire.Payload)
	if err != nil {
		return fmt.Errorf("decode %s payload: %w", wire.EventType, err)
	}
	if payload.EntityType() != wire.EntityType {
		return fmt.Errorf("event type %s requires entity type %s, got %s",
			wire.EventType, payload.EntityType(), wire.EntityType)
	}
	*e = EventEnvelope{
		ProjectID:     wire.ProjectID,
		EventSequence: wire.EventSequence,
		EventID:       wire.EventID,
		EntityID:      wire.EntityID,
		ActorHumanID:  wire.ActorHumanID,
		OccurredAt:    wire.OccurredAt,
		Payload:       payload,
	}
	return nil
}

var payloadDecoders = map[EventType]func([]byte) (EventPayload, error){
	EventProjectCreated:             decodePayload[ProjectCreated],
	EventProjectMemberAdded:         decodePayload[ProjectMemberAdded],
	EventObjectiveRevisionCreated:   decodePayload[ObjectiveRevisionCreated],
	EventBlueprintRevisionProposed:  decodePayload[BlueprintRevisionProposed],
	EventBlueprintRevisionActivated: decodePayload[BlueprintRevisionActivated],
	EventBlueprintRevisionRejected:  decodePayload[BlueprintRevisionRejected],
	EventCharterAccepted:            decodePayload[CharterAccepted],
	EventCharterRejected:            decodePayload[CharterRejected],
	EventRepositoryConnected:        decodePayload[RepositoryConnected],
	EventIntentPublished:            decodePayload[IntentPublished],
	EventIntentRevised:              decodePayload[IntentRevised],
	EventIntentReleased:             decodePayload[IntentReleased],
	EventClaimsObserved:             decodePayload[ClaimsObserved],
	EventCollisionOpened:            decodePayload[CollisionOpened],
	EventCollisionUpdated:           decodePayload[CollisionUpdated],
	EventResolutionProposed:         decodePayload[ResolutionProposed],
	EventResolutionDecided:          decodePayload[ResolutionDecided],
	EventEvidenceAttached:           decodePayload[EvidenceAttached],
	EventObjectiveImported:          decodePayload[ObjectiveImported],
}

func decodePayload[T EventPayload](data []byte) (EventPayload, error) {
	var payload T
	if err := strictUnmarshal(data, &payload); err != nil {
		return nil, err
	}
	return payload, nil
}

// ProjectCreated reports a new Project. EntityID is the Project.
type ProjectCreated struct{}

func (ProjectCreated) EventType() EventType   { return EventProjectCreated }
func (ProjectCreated) EntityType() EntityType { return EntityProject }

// ProjectMemberAdded reports a new membership. EntityID is the Project.
type ProjectMemberAdded struct {
	HumanID uuid.UUID `json:"human_id"`
	Role    Role      `json:"role"`
}

func (ProjectMemberAdded) EventType() EventType   { return EventProjectMemberAdded }
func (ProjectMemberAdded) EntityType() EntityType { return EntityProject }

// ObjectiveRevisionCreated reports a new Objective revision. EntityID is the
// Objective.
type ObjectiveRevisionCreated struct {
	Revision int `json:"revision"`
}

func (ObjectiveRevisionCreated) EventType() EventType   { return EventObjectiveRevisionCreated }
func (ObjectiveRevisionCreated) EntityType() EntityType { return EntityObjective }

// BlueprintRevisionProposed reports a new BlueprintRevision. EntityID is the
// Blueprint.
type BlueprintRevisionProposed struct {
	Revision          int       `json:"revision"`
	ObjectiveID       uuid.UUID `json:"objective_id"`
	ObjectiveRevision int       `json:"objective_revision"`
}

func (BlueprintRevisionProposed) EventType() EventType   { return EventBlueprintRevisionProposed }
func (BlueprintRevisionProposed) EntityType() EntityType { return EntityBlueprint }

// BlueprintRevisionActivated reports that every charter was accepted.
// EntityID is the Blueprint.
type BlueprintRevisionActivated struct {
	Revision int `json:"revision"`
}

func (BlueprintRevisionActivated) EventType() EventType   { return EventBlueprintRevisionActivated }
func (BlueprintRevisionActivated) EntityType() EntityType { return EntityBlueprint }

// BlueprintRevisionRejected reports that a charter rejection made the
// revision inactive. EntityID is the Blueprint.
type BlueprintRevisionRejected struct {
	Revision int       `json:"revision"`
	CellID   uuid.UUID `json:"cell_id"`
}

func (BlueprintRevisionRejected) EventType() EventType   { return EventBlueprintRevisionRejected }
func (BlueprintRevisionRejected) EntityType() EntityType { return EntityBlueprint }

// CharterAccepted reports an acceptance. EntityID is the CharterDecision.
type CharterAccepted struct {
	BlueprintID       uuid.UUID `json:"blueprint_id"`
	BlueprintRevision int       `json:"blueprint_revision"`
	CellID            uuid.UUID `json:"cell_id"`
}

func (CharterAccepted) EventType() EventType   { return EventCharterAccepted }
func (CharterAccepted) EntityType() EntityType { return EntityCharterDecision }

// CharterRejected reports a rejection. EntityID is the CharterDecision.
type CharterRejected struct {
	BlueprintID       uuid.UUID `json:"blueprint_id"`
	BlueprintRevision int       `json:"blueprint_revision"`
	CellID            uuid.UUID `json:"cell_id"`
}

func (CharterRejected) EventType() EventType   { return EventCharterRejected }
func (CharterRejected) EntityType() EntityType { return EntityCharterDecision }

// RepositoryConnected reports that a Project gained a repository connection.
type RepositoryConnected struct{}

func (RepositoryConnected) EventType() EventType   { return EventRepositoryConnected }
func (RepositoryConnected) EntityType() EntityType { return EntityRepository }

// IntentPublished reports the first immutable revision of an Intent.
type IntentPublished struct {
	Revision int `json:"revision"`
}

func (IntentPublished) EventType() EventType   { return EventIntentPublished }
func (IntentPublished) EntityType() EntityType { return EntityIntent }

// IntentRevised reports a new immutable IntentRevision.
type IntentRevised struct {
	Revision int `json:"revision"`
}

func (IntentRevised) EventType() EventType   { return EventIntentRevised }
func (IntentRevised) EntityType() EntityType { return EntityIntent }

// IntentReleased reports that an active Intent no longer coordinates work.
type IntentReleased struct {
	Revision int `json:"revision"`
}

func (IntentReleased) EventType() EventType   { return EventIntentReleased }
func (IntentReleased) EntityType() EntityType { return EntityIntent }

// ClaimsObserved reports a claim observation tied to one exact IntentRevision.
type ClaimsObserved struct {
	IntentID       uuid.UUID `json:"intent_id"`
	IntentRevision int       `json:"intent_revision"`
}

func (ClaimsObserved) EventType() EventType   { return EventClaimsObserved }
func (ClaimsObserved) EntityType() EntityType { return EntitySemanticClaim }

// CollisionOpened reports a newly assessed advisory collision.
type CollisionOpened struct{}

func (CollisionOpened) EventType() EventType   { return EventCollisionOpened }
func (CollisionOpened) EntityType() EntityType { return EntityCollision }

// CollisionUpdated reports a change to an existing advisory collision state.
type CollisionUpdated struct{}

func (CollisionUpdated) EventType() EventType   { return EventCollisionUpdated }
func (CollisionUpdated) EntityType() EntityType { return EntityCollision }

// ResolutionProposed reports a non-binding collision response proposal.
type ResolutionProposed struct{}

func (ResolutionProposed) EventType() EventType   { return EventResolutionProposed }
func (ResolutionProposed) EntityType() EntityType { return EntityResolutionProposal }

// ResolutionDecided reports a human-authorized collision decision.
type ResolutionDecided struct{}

func (ResolutionDecided) EventType() EventType   { return EventResolutionDecided }
func (ResolutionDecided) EntityType() EntityType { return EntityResolutionDecision }

// EvidenceAttached reports a reference-first evidence attachment.
type EvidenceAttached struct{}

func (EvidenceAttached) EventType() EventType   { return EventEvidenceAttached }
func (EvidenceAttached) EntityType() EntityType { return EntityEvidenceReference }

// ObjectiveImported reports an imported Objective revision.
type ObjectiveImported struct {
	Revision int `json:"revision"`
}

func (ObjectiveImported) EventType() EventType   { return EventObjectiveImported }
func (ObjectiveImported) EntityType() EntityType { return EntityObjective }
