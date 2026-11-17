package protocol

import "uuid"

// Schema identifiers for the v1 documents mirrored by this package.
const (
	ProjectSchema                = "urn:conn:schema:v1:project"
	ProjectCreateSchema          = "urn:conn:schema:v1:project-create"
	ProjectMembershipSchema      = "urn:conn:schema:v1:project-membership"
	ObjectiveSchema              = "urn:conn:schema:v1:objective"
	ObjectiveRevisionInputSchema = "urn:conn:schema:v1:objective-revision-input"
	BlueprintRevisionSchema      = "urn:conn:schema:v1:blueprint-revision"
	BlueprintRevisionViewSchema  = "urn:conn:schema:v1:blueprint-revision-view"
	BlueprintProposalSchema      = "urn:conn:schema:v1:blueprint-proposal"
	CharterDecisionSchema        = "urn:conn:schema:v1:charter-decision"
	EventEnvelopeSchema          = "urn:conn:schema:v1:event-envelope"
)

// Project is the authorization and event-cursor boundary for one team effort.
type Project struct {
	ProjectID    uuid.UUID `json:"project_id"`
	Name         string    `json:"name"`
	OwnerHumanID uuid.UUID `json:"owner_human_id"`
	CreatedAt    Timestamp `json:"created_at"`
}

func (Project) SchemaID() string { return ProjectSchema }

// ProjectCreate is the human-authored input for creating a Project.
type ProjectCreate struct {
	ProjectID uuid.UUID `json:"project_id"`
	Name      string    `json:"name"`
}

func (ProjectCreate) SchemaID() string { return ProjectCreateSchema }

// ProjectMembership grants one human a role in one Project.
type ProjectMembership struct {
	ProjectID      uuid.UUID `json:"project_id"`
	HumanID        uuid.UUID `json:"human_id"`
	Role           Role      `json:"role"`
	AddedByHumanID uuid.UUID `json:"added_by_human_id"`
	AddedAt        Timestamp `json:"added_at"`
}

func (ProjectMembership) SchemaID() string { return ProjectMembershipSchema }

// Objective is one immutable revision of a narrative Objective.
type Objective struct {
	ObjectiveID        uuid.UUID   `json:"objective_id"`
	ProjectID          uuid.UUID   `json:"project_id"`
	Revision           int         `json:"revision"`
	Key                string      `json:"key,omitempty"`
	Title              string      `json:"title"`
	OverviewMarkdown   string      `json:"overview_markdown"`
	AccountableHumanID uuid.UUID   `json:"accountable_human_id"`
	SourceRefs         []SourceRef `json:"source_refs,omitempty"`
	KeyPoints          []KeyPoint  `json:"key_points,omitempty"`
	CreatedAt          Timestamp   `json:"created_at"`
}

func (Objective) SchemaID() string { return ObjectiveSchema }

// ObjectiveRevisionInput is the human-authored content of one Objective
// revision. The server supplies the Project, revision number, and time.
type ObjectiveRevisionInput struct {
	ObjectiveID        uuid.UUID   `json:"objective_id"`
	Key                string      `json:"key,omitempty"`
	Title              string      `json:"title"`
	OverviewMarkdown   string      `json:"overview_markdown"`
	AccountableHumanID uuid.UUID   `json:"accountable_human_id"`
	SourceRefs         []SourceRef `json:"source_refs,omitempty"`
	KeyPoints          []KeyPoint  `json:"key_points,omitempty"`
}

func (ObjectiveRevisionInput) SchemaID() string { return ObjectiveRevisionInputSchema }

// SourceRef is optional external context for an Objective.
type SourceRef struct {
	URI   string `json:"uri"`
	Title string `json:"title,omitempty"`
}

// KeyPoint is an Objective statement that needs a stable reference.
type KeyPoint struct {
	Key       string       `json:"key"`
	Kind      KeyPointKind `json:"kind"`
	Statement string       `json:"statement"`
}

// BlueprintRevision is an immutable division of one exact Objective revision
// into complete CellCharter snapshots.
type BlueprintRevision struct {
	BlueprintID       uuid.UUID     `json:"blueprint_id"`
	ProjectID         uuid.UUID     `json:"project_id"`
	Revision          int           `json:"revision"`
	ObjectiveID       uuid.UUID     `json:"objective_id"`
	ObjectiveRevision int           `json:"objective_revision"`
	ProposedByHumanID uuid.UUID     `json:"proposed_by_human_id"`
	CreatedAt         Timestamp     `json:"created_at"`
	CellCharters      []CellCharter `json:"cell_charters"`
}

func (BlueprintRevision) SchemaID() string { return BlueprintRevisionSchema }

// BlueprintRevisionView pairs an immutable snapshot with its current status.
type BlueprintRevisionView struct {
	BlueprintRevision BlueprintRevision `json:"blueprint_revision"`
	Status            BlueprintStatus   `json:"status"`
}

func (BlueprintRevisionView) SchemaID() string { return BlueprintRevisionViewSchema }

// BlueprintProposal is the human-authored content of a proposed
// BlueprintRevision. The server supplies the Project, revision number,
// proposer, and time.
type BlueprintProposal struct {
	BlueprintID       uuid.UUID     `json:"blueprint_id"`
	ObjectiveID       uuid.UUID     `json:"objective_id"`
	ObjectiveRevision int           `json:"objective_revision"`
	CellCharters      []CellCharter `json:"cell_charters"`
}

func (BlueprintProposal) SchemaID() string { return BlueprintProposalSchema }

// CellCharter is one work cell's accountable boundary within a blueprint.
type CellCharter struct {
	CellID             uuid.UUID        `json:"cell_id"`
	AccountableHumanID uuid.UUID        `json:"accountable_human_id"`
	Title              string           `json:"title"`
	OverviewMarkdown   string           `json:"overview_markdown"`
	ObjectivePointRefs []string         `json:"objective_point_refs,omitempty"`
	BoundaryPoints     []BoundaryPoint  `json:"boundary_points,omitempty"`
	Dependencies       []CellDependency `json:"dependencies,omitempty"`
}

// BoundaryPoint is a charter statement that needs a stable reference.
type BoundaryPoint struct {
	Key       string            `json:"key"`
	Kind      BoundaryPointKind `json:"kind"`
	Statement string            `json:"statement"`
}

// CellDependency references another cell in the same BlueprintRevision.
type CellDependency struct {
	CellID  uuid.UUID `json:"cell_id"`
	Summary string    `json:"summary"`
}

// CharterDecision is one append-only acceptance or rejection of a
// CellCharter snapshot by its accountable human.
type CharterDecision struct {
	DecisionID        uuid.UUID      `json:"decision_id"`
	ProjectID         uuid.UUID      `json:"project_id"`
	BlueprintID       uuid.UUID      `json:"blueprint_id"`
	BlueprintRevision int            `json:"blueprint_revision"`
	CellID            uuid.UUID      `json:"cell_id"`
	HumanID           uuid.UUID      `json:"human_id"`
	Outcome           CharterOutcome `json:"outcome"`
	Rationale         string         `json:"rationale,omitempty"`
	DecidedAt         Timestamp      `json:"decided_at"`
}

func (CharterDecision) SchemaID() string { return CharterDecisionSchema }
