package protocol

import (
	"encoding/json"
	"uuid"

	"github.com/Juanqu261/conn/internal/secret"
)

// Schema identifiers for verification, CI, and adoption documents.
const (
	CollisionPreviewInputSchema = "urn:conn:schema:v1:collision-preview-input"
	CollisionPreviewSchema      = "urn:conn:schema:v1:collision-preview"
	CombinationCheckSchema      = "urn:conn:schema:v1:combination-check"
	CombinationCheckInputSchema = "urn:conn:schema:v1:combination-check-input"
	CICredentialSchema          = "urn:conn:schema:v1:ci-credential"
	CICredentialListSchema      = "urn:conn:schema:v1:ci-credential-list"
	CICredentialCreateSchema    = "urn:conn:schema:v1:ci-credential-create"
	CICredentialGrantSchema     = "urn:conn:schema:v1:ci-credential-grant"
	CICredentialContextSchema   = "urn:conn:schema:v1:ci-credential-context"
	ProjectInsightsSchema       = "urn:conn:schema:v1:project-insights"
)

// Collision preview bounds.
const (
	MaxPreviewClaims     = 500
	MaxPreviewExclusions = 20
	MaxPreviewCollisions = 200
)

// CollisionPreviewInput asks which collisions candidate claims would open
// against active Intent revisions. Nothing is persisted. AsIntentID treats
// the candidates as the next revision of that active Intent.
type CollisionPreviewInput struct {
	Claims           []SemanticClaimCandidate `json:"claims"`
	ExcludeIntentIDs []uuid.UUID              `json:"exclude_intent_ids,omitempty"`
	AsIntentID       *uuid.UUID               `json:"as_intent_id,omitempty"`
}

func (CollisionPreviewInput) SchemaID() string { return CollisionPreviewInputSchema }

// CollisionPreview lists the collisions the assessor would open.
type CollisionPreview struct {
	ProjectID  uuid.UUID            `json:"project_id"`
	ClaimCount int                  `json:"claim_count"`
	Collisions []PreviewedCollision `json:"collisions"`
	Truncated  bool                 `json:"truncated"`
}

func (CollisionPreview) SchemaID() string { return CollisionPreviewSchema }

// PreviewedCollision is one collision a preview would open. State is open
// unless a human decision would be inherited, which DecisionRef names.
type PreviewedCollision struct {
	RuleID                   string          `json:"rule_id"`
	Severity                 string          `json:"severity"`
	State                    string          `json:"state"`
	MatchedSubjects          []string        `json:"matched_subjects"`
	Counterpart              ReviewIntentRef `json:"counterpart"`
	DecisionRef              *uuid.UUID      `json:"decision_ref,omitempty"`
	InheritedFromCollisionID *uuid.UUID      `json:"inherited_from_collision_id,omitempty"`
}

// Combination check outcomes, merge results, and check states.
const (
	CombinationPassing       = "passing"
	CombinationFailing       = "failing"
	CombinationMergeConflict = "merge_conflict"
	CombinationIncomplete    = "incomplete"
	MergeClean               = "clean"
	CombinationCheckTimedOut = "timed_out"
	CombinationRunnerLocal   = "local"
	MaxCombinationPaths      = 50
	MaxCombinationNew        = 50
	MaxCombinationChecks     = 20
	MaxContextCombinations   = 5
)

// CombinationSide is one branch of a tested collision.
type CombinationSide struct {
	IntentID          uuid.UUID `json:"intent_id"`
	IntentRevision    int       `json:"intent_revision"`
	BranchRef         string    `json:"branch_ref"`
	Commit            string    `json:"commit"`
	HeadMatchesIntent bool      `json:"head_matches_intent"`
}

// CombinationDiagnostics is the diagnostic delta of the merged tree.
type CombinationDiagnostics struct {
	Ran       bool                    `json:"ran"`
	NewCount  int                     `json:"new_count"`
	New       []CombinationDiagnostic `json:"new"`
	Truncated bool                    `json:"truncated"`
}

// CombinationDiagnostic is a (code, path) pair whose merged count exceeds
// the larger count of either branch alone.
type CombinationDiagnostic struct {
	Code string `json:"code"`
	Path string `json:"path,omitempty"`
}

// CombinationRun is one command run without its output.
type CombinationRun struct {
	ExitCode     int64  `json:"exit_code"`
	TimedOut     bool   `json:"timed_out"`
	DurationMS   int64  `json:"duration_ms"`
	OutputSHA256 string `json:"output_sha256"`
}

// CombinationCheckRun is one pair check run in the merged tree.
type CombinationCheckRun struct {
	Key          string `json:"key"`
	ExitCode     int64  `json:"exit_code"`
	TimedOut     bool   `json:"timed_out"`
	DurationMS   int64  `json:"duration_ms"`
	OutputSHA256 string `json:"output_sha256"`
	State        string `json:"state"`
}

// CombinationConfigSource is the default-branch check configuration the pair
// commands came from.
type CombinationConfigSource struct {
	Ref    string `json:"ref"`
	SHA256 string `json:"sha256"`
}

// CombinationCheckInput is the result `conn verify-pair` submits. Conn
// derives the identity, Project, collision, producer, and time.
type CombinationCheckInput struct {
	Left            CombinationSide          `json:"left"`
	Right           CombinationSide          `json:"right"`
	MergeResult     string                   `json:"merge_result"`
	ConflictedPaths []string                 `json:"conflicted_paths"`
	Diagnostics     CombinationDiagnostics   `json:"diagnostics"`
	Setup           *CombinationRun          `json:"setup,omitempty"`
	Checks          []CombinationCheckRun    `json:"checks"`
	Outcome         string                   `json:"outcome"`
	ConfigSource    *CombinationConfigSource `json:"config_source"`
	Runner          string                   `json:"runner"`
}

func (CombinationCheckInput) SchemaID() string { return CombinationCheckInputSchema }

// CombinationCheck is an immutable advisory record of testing the two
// branches of one collision together. It never changes the collision state.
type CombinationCheck struct {
	CheckID               uuid.UUID                `json:"check_id"`
	ProjectID             uuid.UUID                `json:"project_id"`
	CollisionID           uuid.UUID                `json:"collision_id"`
	Left                  CombinationSide          `json:"left"`
	Right                 CombinationSide          `json:"right"`
	MergeResult           string                   `json:"merge_result"`
	ConflictedPaths       []string                 `json:"conflicted_paths"`
	Diagnostics           CombinationDiagnostics   `json:"diagnostics"`
	Setup                 *CombinationRun          `json:"setup,omitempty"`
	Checks                []CombinationCheckRun    `json:"checks"`
	Outcome               string                   `json:"outcome"`
	ConfigSource          *CombinationConfigSource `json:"config_source"`
	Runner                string                   `json:"runner"`
	ProducerParticipantID uuid.UUID                `json:"producer_participant_id"`
	CreatedAt             Timestamp                `json:"created_at"`
}

func (CombinationCheck) SchemaID() string { return CombinationCheckSchema }

// CI credential bounds.
const (
	CITokenPrefix          = "conn_ci_"
	DefaultCICredentialTTL = 90
	MaxCICredentialDays    = 365
	MaxCICredentialsListed = 200
)

// CICredential describes a CI credential without its token.
type CICredential struct {
	CredentialID     uuid.UUID  `json:"credential_id"`
	ProjectID        uuid.UUID  `json:"project_id"`
	RepositoryID     uuid.UUID  `json:"repository_id"`
	Label            string     `json:"label,omitempty"`
	CreatedByHumanID uuid.UUID  `json:"created_by_human_id"`
	CreatedAt        Timestamp  `json:"created_at"`
	ExpiresAt        Timestamp  `json:"expires_at"`
	LastUsedAt       *Timestamp `json:"last_used_at,omitempty"`
	RevokedAt        *Timestamp `json:"revoked_at,omitempty"`
}

func (CICredential) SchemaID() string { return CICredentialSchema }

// CICredentialList lists a Project's CI credentials, newest first.
type CICredentialList struct {
	ProjectID   uuid.UUID      `json:"project_id"`
	Credentials []CICredential `json:"credentials"`
}

func (CICredentialList) SchemaID() string { return CICredentialListSchema }

// CICredentialCreate is the owner's creation request.
type CICredentialCreate struct {
	RepositoryID  uuid.UUID `json:"repository_id"`
	Label         string    `json:"label,omitempty"`
	ExpiresInDays int       `json:"expires_in_days"`
}

func (CICredentialCreate) SchemaID() string { return CICredentialCreateSchema }

// CICredentialGrant is the one response that carries a raw CI token.
type CICredentialGrant struct {
	Token      secret.String
	Credential CICredential
}

func (CICredentialGrant) SchemaID() string { return CICredentialGrantSchema }

type ciCredentialGrantWire struct {
	Token      string       `json:"token"`
	Credential CICredential `json:"credential"`
}

// MarshalJSON reveals the token. Only this type and SessionGrant do.
func (g CICredentialGrant) MarshalJSON() ([]byte, error) {
	return json.Marshal(ciCredentialGrantWire{Token: g.Token.Reveal(), Credential: g.Credential})
}

func (g *CICredentialGrant) UnmarshalJSON(data []byte) error {
	var wire ciCredentialGrantWire
	if err := strictUnmarshal(data, &wire); err != nil {
		return err
	}
	*g = CICredentialGrant{Token: secret.New(wire.Token), Credential: wire.Credential}
	return nil
}

// CICredentialContext is what a CI credential may read.
type CICredentialContext struct {
	CredentialID    uuid.UUID `json:"credential_id"`
	ProjectID       uuid.UUID `json:"project_id"`
	ProjectName     string    `json:"project_name"`
	RepositoryID    uuid.UUID `json:"repository_id"`
	CanonicalRemote string    `json:"canonical_remote"`
	DefaultBranch   string    `json:"default_branch,omitempty"`
	ExpiresAt       Timestamp `json:"expires_at"`
}

func (CICredentialContext) SchemaID() string { return CICredentialContextSchema }

// Insight statuses and bounds.
const (
	InsightValue              = "value"
	InsightInsufficientData   = "insufficient_data"
	InsightMinimumDenominator = 5
	DefaultInsightDays        = 30
	MaxInsightDays            = 365
)

// ProjectInsights holds Project-level numbers only. No field can hold a
// Human, Participant, or login.
type ProjectInsights struct {
	ProjectID                  uuid.UUID                   `json:"project_id"`
	SinceDays                  int                         `json:"since_days"`
	WindowStart                Timestamp                   `json:"window_start"`
	WindowEnd                  Timestamp                   `json:"window_end"`
	MinimumDenominator         int                         `json:"minimum_denominator"`
	CollisionsOpened           []InsightRuleCount          `json:"collisions_opened"`
	Decisions                  []InsightDecisionCount      `json:"decisions"`
	FalsePositiveShare         []InsightRuleShare          `json:"false_positive_share"`
	MedianHoursToFirstDecision InsightMedian               `json:"median_hours_to_first_decision"`
	DecidedBeforeRelease       InsightRatio                `json:"decided_before_release"`
	UndeclaredObservedChanges  InsightRatio                `json:"undeclared_observed_changes"`
	AcceptanceByProvenance     InsightAcceptanceProvenance `json:"acceptance_by_provenance"`
	CombinationChecks          []InsightOutcomeCount       `json:"combination_checks"`
}

func (ProjectInsights) SchemaID() string { return ProjectInsightsSchema }

type InsightRuleCount struct {
	RuleID string `json:"rule_id"`
	Count  int    `json:"count"`
}

type InsightDecisionCount struct {
	RuleID string `json:"rule_id"`
	Kind   string `json:"kind"`
	Count  int    `json:"count"`
}

type InsightRuleShare struct {
	RuleID string       `json:"rule_id"`
	Share  InsightRatio `json:"share"`
}

type InsightOutcomeCount struct {
	Outcome string `json:"outcome"`
	Count   int    `json:"count"`
}

// InsightRatio reports insufficient data instead of Value when Denominator
// is below the minimum.
type InsightRatio struct {
	Numerator   int      `json:"numerator"`
	Denominator int      `json:"denominator"`
	Status      string   `json:"status"`
	Value       *float64 `json:"value,omitempty"`
}

// NewInsightRatio applies the small-denominator suppression.
func NewInsightRatio(numerator, denominator int) InsightRatio {
	ratio := InsightRatio{Numerator: numerator, Denominator: denominator, Status: InsightInsufficientData}
	if denominator >= InsightMinimumDenominator {
		value := float64(numerator) / float64(denominator)
		ratio.Status, ratio.Value = InsightValue, &value
	}
	return ratio
}

type InsightMedian struct {
	SampleSize int      `json:"sample_size"`
	Status     string   `json:"status"`
	Value      *float64 `json:"value,omitempty"`
}

type InsightAcceptanceProvenance struct {
	ObservedRun int `json:"observed_run"`
	Reported    int `json:"reported"`
	Missing     int `json:"missing"`
}
