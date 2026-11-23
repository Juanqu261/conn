package protocol

import "uuid"

// RepositoryConnection binds a Project to a credential-free source repository.
type RepositoryConnection struct {
	RepositoryID         uuid.UUID `json:"repository_id"`
	ProjectID            uuid.UUID `json:"project_id"`
	HostKind             string    `json:"host_kind"`
	CanonicalRemote      string    `json:"canonical_remote"`
	HostRepositoryNodeID string    `json:"host_repository_node_id,omitempty"`
	DefaultBranch        string    `json:"default_branch,omitempty"`
	AnalysisEnabled      bool      `json:"analysis_enabled"`
	CreatedByHumanID     uuid.UUID `json:"created_by_human_id"`
	CreatedAt            Timestamp `json:"created_at"`
}

func (RepositoryConnection) SchemaID() string { return RepositoryConnectionSchema }

// RepositoryConnectInput is owner-authored connection content. The backend
// supplies Project, Human, and creation-time fields.
type RepositoryConnectInput struct {
	RepositoryID         uuid.UUID `json:"repository_id"`
	HostKind             string    `json:"host_kind"`
	CanonicalRemote      string    `json:"canonical_remote"`
	HostRepositoryNodeID string    `json:"host_repository_node_id,omitempty"`
	DefaultBranch        string    `json:"default_branch,omitempty"`
	AnalysisEnabled      bool      `json:"analysis_enabled"`
}

func (RepositoryConnectInput) SchemaID() string { return RepositoryConnectInputSchema }

// RepositoryConnectionList lists a member-visible Project repository scope.
type RepositoryConnectionList struct {
	ProjectID    uuid.UUID              `json:"project_id"`
	Repositories []RepositoryConnection `json:"repositories"`
}

func (RepositoryConnectionList) SchemaID() string { return RepositoryConnectionListSchema }

// Participant is an attributed operating identity under one Conn Human.
type Participant struct {
	ParticipantID   uuid.UUID  `json:"participant_id"`
	ProjectID       uuid.UUID  `json:"project_id"`
	HumanID         uuid.UUID  `json:"human_id"`
	AdapterKind     string     `json:"adapter_kind"`
	LocalSessionRef string     `json:"local_session_ref,omitempty"`
	DisplayLabel    string     `json:"display_label,omitempty"`
	CreatedAt       Timestamp  `json:"created_at"`
	RetiredAt       *Timestamp `json:"retired_at,omitempty"`
}

func (Participant) SchemaID() string { return ParticipantSchema }

// ClaimEvidenceRef is a bounded reference supporting a semantic claim.
type ClaimEvidenceRef struct {
	Kind        string `json:"kind"`
	Locator     string `json:"locator"`
	Path        string `json:"path,omitempty"`
	Line        *int   `json:"line,omitempty"`
	ContentHash string `json:"content_hash,omitempty"`
}

// SemanticClaimCandidate is a participant or analyzer assertion before Conn
// assigns claim identity and ties it to an exact IntentRevision.
type SemanticClaimCandidate struct {
	SubjectKind       string             `json:"subject_kind"`
	NormalizedSubject string             `json:"normalized_subject"`
	DisplayName       string             `json:"display_name"`
	Relation          string             `json:"relation"`
	ProvenanceKind    string             `json:"provenance_kind"`
	Confidence        *float64           `json:"confidence,omitempty"`
	EvidenceRefs      []ClaimEvidenceRef `json:"evidence_refs,omitempty"`
}

func (SemanticClaimCandidate) SchemaID() string { return SemanticClaimCandidateSchema }

// SemanticClaim is an immutable, attributed claim for one IntentRevision.
type SemanticClaim struct {
	ClaimID               uuid.UUID          `json:"claim_id"`
	ProjectID             uuid.UUID          `json:"project_id"`
	IntentID              uuid.UUID          `json:"intent_id"`
	IntentRevision        int                `json:"intent_revision"`
	SubjectKind           string             `json:"subject_kind"`
	NormalizedSubject     string             `json:"normalized_subject"`
	DisplayName           string             `json:"display_name"`
	Relation              string             `json:"relation"`
	ProvenanceKind        string             `json:"provenance_kind"`
	Confidence            *float64           `json:"confidence,omitempty"`
	EvidenceRefs          []ClaimEvidenceRef `json:"evidence_refs,omitempty"`
	ProducerParticipantID *uuid.UUID         `json:"producer_participant_id,omitempty"`
	ObservedAt            Timestamp          `json:"observed_at"`
}

func (SemanticClaim) SchemaID() string { return SemanticClaimSchema }

// IntentInput is participant-authored intent content. Server-derived fields
// such as Project, Participant, revision, status, and timestamp are absent.
type IntentInput struct {
	IntentID          uuid.UUID                `json:"intent_id"`
	ObjectiveID       uuid.UUID                `json:"objective_id"`
	ObjectiveRevision int                      `json:"objective_revision"`
	BlueprintID       *uuid.UUID               `json:"blueprint_id,omitempty"`
	BlueprintRevision *int                     `json:"blueprint_revision,omitempty"`
	CharterID         *uuid.UUID               `json:"charter_id,omitempty"`
	RepositoryID      *uuid.UUID               `json:"repository_id,omitempty"`
	BaseRevisionRef   string                   `json:"base_revision_ref,omitempty"`
	HeadRevisionRef   string                   `json:"head_revision_ref,omitempty"`
	BranchRef         string                   `json:"branch_ref,omitempty"`
	SummaryMarkdown   string                   `json:"summary_markdown"`
	Assumptions       []string                 `json:"assumptions,omitempty"`
	DeclaredClaims    []SemanticClaimCandidate `json:"declared_claims,omitempty"`
}

func (IntentInput) SchemaID() string { return IntentInputSchema }

// IntentRevision is the immutable, attributable form of an IntentInput.
type IntentRevision struct {
	IntentID          uuid.UUID                `json:"intent_id"`
	ProjectID         uuid.UUID                `json:"project_id"`
	ParticipantID     uuid.UUID                `json:"participant_id"`
	Revision          int                      `json:"revision"`
	ObjectiveID       uuid.UUID                `json:"objective_id"`
	ObjectiveRevision int                      `json:"objective_revision"`
	BlueprintID       *uuid.UUID               `json:"blueprint_id,omitempty"`
	BlueprintRevision *int                     `json:"blueprint_revision,omitempty"`
	CharterID         *uuid.UUID               `json:"charter_id,omitempty"`
	RepositoryID      *uuid.UUID               `json:"repository_id,omitempty"`
	BaseRevisionRef   string                   `json:"base_revision_ref,omitempty"`
	HeadRevisionRef   string                   `json:"head_revision_ref,omitempty"`
	BranchRef         string                   `json:"branch_ref,omitempty"`
	SummaryMarkdown   string                   `json:"summary_markdown"`
	Assumptions       []string                 `json:"assumptions,omitempty"`
	DeclaredClaims    []SemanticClaimCandidate `json:"declared_claims,omitempty"`
	CreatedAt         Timestamp                `json:"created_at"`
}

func (IntentRevision) SchemaID() string { return IntentRevisionSchema }

// IntentView combines an immutable snapshot with its current lifecycle state.
// Advisory observations deliberately do not alter a Charter or an Intent.
type IntentView struct {
	IntentRevision       IntentRevision `json:"intent_revision"`
	Status               string         `json:"status"`
	AdvisoryObservations []string       `json:"advisory_observations"`
}

func (IntentView) SchemaID() string { return IntentViewSchema }

// IntentList lists current snapshots for every Intent visible to a member.
type IntentList struct {
	ProjectID uuid.UUID    `json:"project_id"`
	Intents   []IntentView `json:"intents"`
}

func (IntentList) SchemaID() string { return IntentListSchema }

// LocalAnalyzerReport is the bounded source-free result of local analysis.
type LocalAnalyzerReport struct {
	ReportVersion int                      `json:"report_version"`
	Repository    AnalyzerRepository       `json:"repository"`
	Claims        []SemanticClaimCandidate `json:"claims"`
	Diagnostics   []AnalyzerDiagnostic     `json:"diagnostics"`
}

func (LocalAnalyzerReport) SchemaID() string { return LocalAnalyzerReportSchema }

type AnalyzerRepository struct {
	CanonicalRemote string `json:"canonical_remote"`
	HeadRevisionRef string `json:"head_revision_ref,omitempty"`
	BranchRef       string `json:"branch_ref,omitempty"`
}

type AnalyzerDiagnostic struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Path    string `json:"path,omitempty"`
	Line    *int   `json:"line,omitempty"`
}

// CollisionAssessment is an advisory interaction between exact revisions.
type CollisionAssessment struct {
	CollisionID           uuid.UUID   `json:"collision_id"`
	ProjectID             uuid.UUID   `json:"project_id"`
	LeftIntentID          uuid.UUID   `json:"left_intent_id"`
	LeftIntentRevision    int         `json:"left_intent_revision"`
	RightIntentID         uuid.UUID   `json:"right_intent_id"`
	RightIntentRevision   int         `json:"right_intent_revision"`
	RuleID                string      `json:"rule_id"`
	Severity              string      `json:"severity"`
	Confidence            float64     `json:"confidence"`
	State                 string      `json:"state"`
	MatchingClaimRefs     []uuid.UUID `json:"matching_claim_refs"`
	SupportingContextRefs []uuid.UUID `json:"supporting_context_refs,omitempty"`
	ExplanationMarkdown   string      `json:"explanation_markdown"`
	CreatedAt             Timestamp   `json:"created_at"`
	SupersededAt          *Timestamp  `json:"superseded_at,omitempty"`
}

func (CollisionAssessment) SchemaID() string { return CollisionAssessmentSchema }

// ResolutionProposal is non-binding until an authorized human decides it.
type ResolutionProposal struct {
	ProposalID              uuid.UUID  `json:"proposal_id"`
	ProjectID               uuid.UUID  `json:"project_id"`
	CollisionID             uuid.UUID  `json:"collision_id"`
	ProposedByParticipantID *uuid.UUID `json:"proposed_by_participant_id,omitempty"`
	ProposedByHumanID       *uuid.UUID `json:"proposed_by_human_id,omitempty"`
	Kind                    string     `json:"kind"`
	RationaleMarkdown       string     `json:"rationale_markdown"`
	References              []string   `json:"references,omitempty"`
	CreatedAt               Timestamp  `json:"created_at"`
}

func (ResolutionProposal) SchemaID() string { return ResolutionProposalSchema }

// ResolutionDecision is an immutable human-authorized outcome.
type ResolutionDecision struct {
	DecisionID         uuid.UUID  `json:"decision_id"`
	ProjectID          uuid.UUID  `json:"project_id"`
	CollisionID        uuid.UUID  `json:"collision_id"`
	SelectedProposalID *uuid.UUID `json:"selected_proposal_id,omitempty"`
	DecidedByHumanID   uuid.UUID  `json:"decided_by_human_id"`
	Kind               string     `json:"kind"`
	Binding            bool       `json:"binding"`
	RationaleMarkdown  string     `json:"rationale_markdown"`
	References         []string   `json:"references,omitempty"`
	CreatedAt          Timestamp  `json:"created_at"`
}

func (ResolutionDecision) SchemaID() string { return ResolutionDecisionSchema }

// EvidenceReference links an objective revision to externally held evidence.
type EvidenceReference struct {
	EvidenceID            uuid.UUID  `json:"evidence_id"`
	ProjectID             uuid.UUID  `json:"project_id"`
	IntentID              *uuid.UUID `json:"intent_id,omitempty"`
	ObjectiveID           uuid.UUID  `json:"objective_id"`
	ObjectiveRevision     int        `json:"objective_revision"`
	AcceptanceKey         string     `json:"acceptance_key,omitempty"`
	Kind                  string     `json:"kind"`
	State                 string     `json:"state"`
	Locator               string     `json:"locator"`
	ContentHash           string     `json:"content_hash,omitempty"`
	Summary               string     `json:"summary,omitempty"`
	ProducerParticipantID *uuid.UUID `json:"producer_participant_id,omitempty"`
	ObservedAt            Timestamp  `json:"observed_at"`
}

func (EvidenceReference) SchemaID() string { return EvidenceReferenceSchema }

// ConflictContextPack is the portable, bounded factual context for one collision.
type ConflictContextPack struct {
	Collision              CollisionAssessment `json:"collision"`
	Objective              ConflictObjective   `json:"objective"`
	Intents                []IntentRevision    `json:"intents"`
	Claims                 []SemanticClaim     `json:"claims"`
	EvidenceRefs           []uuid.UUID         `json:"evidence_refs,omitempty"`
	Uncertainties          []string            `json:"uncertainties"`
	ExistingDecisionRefs   []uuid.UUID         `json:"existing_decision_refs,omitempty"`
	UnresolvedQuestion     string              `json:"unresolved_question,omitempty"`
	AllowedResolutionKinds []string            `json:"allowed_resolution_kinds"`
	SafeHandlingNote       string              `json:"safe_handling_note"`
}

func (ConflictContextPack) SchemaID() string { return ConflictContextPackSchema }

type ConflictObjective struct {
	ObjectiveID uuid.UUID   `json:"objective_id"`
	Revision    int         `json:"revision"`
	Title       string      `json:"title"`
	CharterRefs []uuid.UUID `json:"charter_refs,omitempty"`
}

// ObjectiveImportInput is human-authorized imported objective content.
type ObjectiveImportInput struct {
	SourceKind        string     `json:"source_kind"`
	SourceRef         string     `json:"source_ref"`
	SourceRevisionRef string     `json:"source_revision_ref,omitempty"`
	ContentHash       string     `json:"content_hash"`
	Title             string     `json:"title"`
	OverviewMarkdown  string     `json:"overview_markdown"`
	KeyPoints         []KeyPoint `json:"key_points,omitempty"`
}

func (ObjectiveImportInput) SchemaID() string { return ObjectiveImportInputSchema }

// RadarView is the explainable project coordination snapshot.
type RadarView struct {
	Project          Project                 `json:"project"`
	AsOfEvent        int64                   `json:"as_of_event"`
	Objectives       []RadarObjective        `json:"objectives"`
	ActiveBlueprint  *RadarBlueprint         `json:"active_blueprint,omitempty"`
	Repositories     []RepositoryConnection  `json:"repositories"`
	ActiveIntents    []IntentRevision        `json:"active_intents"`
	SemanticClaims   []SemanticClaim         `json:"semantic_claims"`
	SemanticConcepts []RadarSemanticConcept  `json:"semantic_concepts,omitempty"`
	OpenCollisions   []CollisionAssessment   `json:"open_collisions"`
	DecisionRefs     []uuid.UUID             `json:"decision_refs,omitempty"`
	Dependencies     []RadarDependency       `json:"dependencies,omitempty"`
	OpenQuestions    []string                `json:"open_questions"`
	Assumptions      []string                `json:"assumptions"`
	EvidenceCoverage []RadarEvidenceCoverage `json:"evidence_coverage"`
	KnownUnknowns    []string                `json:"known_unknowns"`
}

func (RadarView) SchemaID() string { return RadarViewSchema }

type RadarObjective struct {
	ObjectiveID uuid.UUID `json:"objective_id"`
	Revision    int       `json:"revision"`
	Title       string    `json:"title"`
}

type RadarBlueprint struct {
	BlueprintID uuid.UUID `json:"blueprint_id"`
	Revision    int       `json:"revision"`
}

type RadarSemanticConcept struct {
	NormalizedSubject string      `json:"normalized_subject"`
	ClaimRefs         []uuid.UUID `json:"claim_refs"`
}

type RadarDependency struct {
	FromIntentID uuid.UUID `json:"from_intent_id"`
	ToIntentID   uuid.UUID `json:"to_intent_id"`
	Summary      string    `json:"summary"`
}

type RadarEvidenceCoverage struct {
	ObjectiveID       uuid.UUID   `json:"objective_id"`
	ObjectiveRevision int         `json:"objective_revision"`
	AcceptanceKey     string      `json:"acceptance_key"`
	State             string      `json:"state"`
	EvidenceRefs      []uuid.UUID `json:"evidence_refs"`
}
