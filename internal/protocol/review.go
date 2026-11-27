package protocol

import "uuid"

// Schema identifiers for review-proof documents.
const (
	EvidenceReferenceListSchema = "urn:conn:schema:v1:evidence-reference-list"
	ChecksConfigSchema          = "urn:conn:schema:v1:checks-config"
	ReviewPacketSchema          = "urn:conn:schema:v1:review-packet"
)

// ChecksConfig is the committed .conn/checks.json file.
type ChecksConfig struct {
	Version          int                        `json:"version"`
	Checks           map[string]CheckDefinition `json:"checks"`
	PairVerification *PairVerification          `json:"pair_verification,omitempty"`
}

// PairVerification names the commands `conn verify-pair` runs in a merged
// tree. Conn reads it only from the default branch.
type PairVerification struct {
	Setup  *CheckDefinition `json:"setup,omitempty"`
	Checks []string         `json:"checks"`
}

func (ChecksConfig) SchemaID() string { return ChecksConfigSchema }

// CheckDefinition is one reviewed command. Command is an argument array that
// no shell interprets; Env holds literal, non-secret values.
type CheckDefinition struct {
	Command          []string          `json:"command"`
	WorkingDirectory string            `json:"working_directory,omitempty"`
	TimeoutSeconds   int               `json:"timeout_seconds"`
	AcceptanceKeys   []string          `json:"acceptance_keys,omitempty"`
	Env              map[string]string `json:"env,omitempty"`
}

// ReviewPacketVersion is the packet_version this build produces.
const ReviewPacketVersion = 1

// ReviewPacket is the generated, non-persisted view of what one Intent set
// out to do, what it touched, and what was verified.
type ReviewPacket struct {
	PacketVersion    int                `json:"packet_version"`
	GeneratedAtEvent int64              `json:"generated_at_event"`
	Project          ReviewProject      `json:"project"`
	Intent           ReviewIntent       `json:"intent"`
	Objective        ReviewObjective    `json:"objective"`
	Blueprint        *ReviewRevisionRef `json:"blueprint,omitempty"`
	Charter          *ReviewCharter     `json:"charter,omitempty"`
	Git              ReviewGit          `json:"git"`
	Acceptance       []ReviewAcceptance `json:"acceptance"`
	Drift            ScopeDrift         `json:"drift"`
	Collisions       []ReviewCollision  `json:"collisions"`
	IntegrityNotes   []string           `json:"integrity_notes"`
	OpenQuestions    []string           `json:"open_questions"`
	KnownUnknowns    []string           `json:"known_unknowns"`
	Truncated        []string           `json:"truncated"`
}

func (ReviewPacket) SchemaID() string { return ReviewPacketSchema }

type ReviewProject struct {
	ID   uuid.UUID `json:"id"`
	Name string    `json:"name"`
}

type ReviewIntent struct {
	ID            uuid.UUID `json:"id"`
	Revision      int       `json:"revision"`
	ParticipantID uuid.UUID `json:"participant_id"`
	OwnerLogin    string    `json:"owner_login"`
	Active        bool      `json:"active"`
	Summary       string    `json:"summary"`
	Assumptions   []string  `json:"assumptions"`
}

type ReviewObjective struct {
	ID       uuid.UUID `json:"id"`
	Revision int       `json:"revision"`
	Title    string    `json:"title"`
}

type ReviewRevisionRef struct {
	ID       uuid.UUID `json:"id"`
	Revision int       `json:"revision"`
}

type ReviewCharter struct {
	ID       uuid.UUID `json:"id"`
	Accepted bool      `json:"accepted"`
}

// ReviewGit holds local Git facts. HeadIsWorktree means the reviewed head
// includes uncommitted changes.
type ReviewGit struct {
	Available           bool   `json:"available"`
	Branch              string `json:"branch,omitempty"`
	Base                string `json:"base,omitempty"`
	Head                string `json:"head,omitempty"`
	HeadIsWorktree      bool   `json:"head_is_worktree"`
	Dirty               bool   `json:"dirty"`
	ChangedFileCount    int    `json:"changed_file_count"`
	ChecksConfigChanged bool   `json:"checks_config_changed"`
}

type ReviewAcceptance struct {
	Key        string           `json:"key"`
	Statement  string           `json:"statement"`
	State      string           `json:"state"`
	Provenance string           `json:"provenance,omitempty"`
	Evidence   []ReviewEvidence `json:"evidence"`
}

type ReviewEvidence struct {
	ID         uuid.UUID `json:"id"`
	Kind       string    `json:"kind"`
	State      string    `json:"state"`
	Locator    string    `json:"locator"`
	Provenance string    `json:"provenance"`
	Commit     string    `json:"commit,omitempty"`
	Stale      bool      `json:"stale"`
	Dirty      bool      `json:"dirty"`
	ExitCode   *int64    `json:"exit_code,omitempty"`
	DurationMS *int64    `json:"duration_ms,omitempty"`
	ObservedAt Timestamp `json:"observed_at"`
}

// ScopeDrift is the advisory comparison of declared and observed subjects.
type ScopeDrift struct {
	Analysis                 DriftAnalysis        `json:"analysis"`
	Matched                  []DriftSubject       `json:"matched"`
	DeclaredNotObserved      []DriftSubject       `json:"declared_not_observed"`
	ObservedNotDeclared      []DriftSubject       `json:"observed_not_declared"`
	ChangedFilesWithoutClaim []DriftFile          `json:"changed_files_without_claim"`
	NotObservable            []DriftNotObservable `json:"not_observable"`
	Omitted                  DriftOmitted         `json:"omitted"`
}

type DriftAnalysis struct {
	Status          string     `json:"status"`
	IntentRevision  *int       `json:"intent_revision,omitempty"`
	ObservedAt      *Timestamp `json:"observed_at,omitempty"`
	DiagnosticCodes []string   `json:"diagnostic_codes,omitempty"`
}

type DriftSubject struct {
	SubjectKind       string `json:"subject_kind"`
	NormalizedSubject string `json:"normalized_subject"`
	DisplayName       string `json:"display_name"`
	Relation          string `json:"relation"`
}

type DriftFile struct {
	Path           string `json:"path"`
	WrittenByAgent bool   `json:"written_by_agent"`
}

type DriftNotObservable struct {
	SubjectKind       string `json:"subject_kind"`
	NormalizedSubject string `json:"normalized_subject"`
	DisplayName       string `json:"display_name"`
	Relation          string `json:"relation"`
	AcceptanceKey     string `json:"acceptance_key,omitempty"`
	EvidenceState     string `json:"evidence_state,omitempty"`
}

type DriftOmitted struct {
	Matched                  int `json:"matched"`
	DeclaredNotObserved      int `json:"declared_not_observed"`
	ObservedNotDeclared      int `json:"observed_not_declared"`
	ChangedFilesWithoutClaim int `json:"changed_files_without_claim"`
	NotObservable            int `json:"not_observable"`
}

type ReviewCollision struct {
	ID              uuid.UUID       `json:"id"`
	RuleID          string          `json:"rule_id"`
	Severity        string          `json:"severity"`
	State           string          `json:"state"`
	MatchedSubjects []string        `json:"matched_subjects"`
	Counterpart     ReviewIntentRef `json:"counterpart"`
	Decision        *ReviewDecision `json:"decision,omitempty"`
	ProposalCount   *int            `json:"proposal_count,omitempty"`
	// LatestCombinationOutcome is the most recent combination check outcome.
	LatestCombinationOutcome string `json:"latest_combination_outcome,omitempty"`
}

type ReviewIntentRef struct {
	IntentID uuid.UUID `json:"intent_id"`
	Revision int       `json:"revision"`
}

type ReviewDecision struct {
	Kind             string `json:"kind"`
	Binding          bool   `json:"binding"`
	DecidedByLogin   string `json:"decided_by_login,omitempty"`
	RationaleExcerpt string `json:"rationale_excerpt"`
}
