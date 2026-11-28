package protocol

import "uuid"

// SpecKitCheckSchema identifies the `conn speckit check` JSON output.
const SpecKitCheckSchema = "urn:conn:schema:v1:speckit-check"

// Objective import source kinds.
const (
	SourceKindKiroSpec       = "kiro_spec"
	SourceKindSpecKitFeature = "speckit_feature"
	SourceKindGitHubIssue    = "github_issue"
)

// Spec Kit extraction sources.
const (
	SpecKitSourceTasks     = "tasks"
	SpecKitSourcePlan      = "plan"
	SpecKitSourcePathsFlag = "paths_flag"
	SpecKitSourceNone      = "none"
)

// SpecKitCheck is the plan-time collision check of one Spec Kit feature.
// Nothing in it was published. It carries paths and claim subjects only,
// never task, plan, research, contract, or data-model text.
type SpecKitCheck struct {
	Feature             SpecKitFeatureRef        `json:"feature"`
	ProjectID           *uuid.UUID               `json:"project_id,omitempty"`
	Objective           *SpecKitObjective        `json:"objective,omitempty"`
	Extraction          SpecKitExtraction        `json:"extraction"`
	Analysis            SpecKitAnalysis          `json:"analysis"`
	CandidateCount      int                      `json:"candidate_count"`
	Candidates          []SemanticClaimCandidate `json:"candidates"`
	OwnIntent           *SpecKitIntentRef        `json:"own_intent,omitempty"`
	Delta               *SpecKitDelta            `json:"delta,omitempty"`
	Collisions          []SpecKitCollision       `json:"collisions"`
	CollisionsTruncated bool                     `json:"collisions_truncated"`
	PublishOffered      bool                     `json:"publish_offered"`
	Errors              []string                 `json:"errors"`
}

func (SpecKitCheck) SchemaID() string { return SpecKitCheckSchema }

// SpecKitFeatureRef names the feature by repository-relative paths.
type SpecKitFeatureRef struct {
	Directory string `json:"directory"`
	SpecRef   string `json:"spec_ref"`
	Title     string `json:"title"`
}

// SpecKitObjective is the Objective imported from the feature.
type SpecKitObjective struct {
	ObjectiveID    uuid.UUID `json:"objective_id"`
	Revision       int       `json:"revision"`
	Title          string    `json:"title"`
	ContentChanged bool      `json:"content_changed"`
}

// SpecKitExtraction lists the planned paths and how they were found.
type SpecKitExtraction struct {
	Source           string   `json:"source"`
	Paths            []string `json:"paths"`
	NewPaths         []string `json:"new_paths"`
	Unresolved       []string `json:"unresolved"`
	TasksWithoutPath []string `json:"tasks_without_path"`
	Truncated        bool     `json:"truncated"`
}

// SpecKitAnalysis reports whether planned local analysis contributed.
type SpecKitAnalysis struct {
	Available bool   `json:"available"`
	Reason    string `json:"reason,omitempty"`
}

// SpecKitIntentRef is the caller's own active Intent for the feature.
type SpecKitIntentRef struct {
	IntentID uuid.UUID `json:"intent_id"`
	Revision int       `json:"revision"`
	Summary  string    `json:"summary"`
}

// SpecKitDelta compares the task list with the own Intent's claims.
type SpecKitDelta struct {
	Added   []SpecKitClaimRef `json:"added"`
	Dropped []SpecKitClaimRef `json:"dropped"`
}

// SpecKitClaimRef is one claim subject and relation.
type SpecKitClaimRef struct {
	NormalizedSubject string `json:"normalized_subject"`
	Relation          string `json:"relation"`
}

// SpecKitCollision is one previewed collision with its counterpart named.
type SpecKitCollision struct {
	RuleID          string                `json:"rule_id"`
	Severity        string                `json:"severity"`
	State           string                `json:"state"`
	MatchedSubjects []string              `json:"matched_subjects"`
	Counterpart     SpecKitCounterpartRef `json:"counterpart"`
	DecisionRef     *uuid.UUID            `json:"decision_ref,omitempty"`
}

// SpecKitCounterpartRef names the other Intent and its Objective.
type SpecKitCounterpartRef struct {
	IntentID       uuid.UUID `json:"intent_id"`
	Revision       int       `json:"revision"`
	Summary        string    `json:"summary,omitempty"`
	ObjectiveTitle string    `json:"objective_title,omitempty"`
}
