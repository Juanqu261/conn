package protocol

import "uuid"

const BoardViewSchema = "urn:conn:schema:v1:board-view"

// BoardVersion is the board-view read-model version.
const BoardVersion = 1

// Board lanes, in display order. The backend derives every lane; nobody
// sets one.
const (
	LaneAssembling = "assembling"
	LaneStitching  = "stitching"
	LaneCharging   = "charging"
	LaneAlive      = "alive"
	LaneBuried     = "buried"
)

// BoardLanes lists the lanes in the order the board-view contract fixes.
var BoardLanes = []string{LaneAssembling, LaneStitching, LaneCharging, LaneAlive, LaneBuried}

// Board rules: the reason a card is in its lane.
const (
	RuleReleasedRecently     = "released_recently"
	RuleOpenCollision        = "open_collision"
	RuleNoHeadRevision       = "no_head_revision"
	RuleNoObservedAnalysis   = "no_observed_analysis"
	RuleAcceptanceIncomplete = "acceptance_incomplete"
	RuleNoAcceptanceCriteria = "no_acceptance_criteria"
	RuleReady                = "ready"
)

// BoardView is the derived Intent board of one Project. Lane totals are
// Project-level only.
type BoardView struct {
	BoardVersion int         `json:"board_version"`
	ProjectID    uuid.UUID   `json:"project_id"`
	AsOfEvent    int64       `json:"as_of_event"`
	WindowDays   int         `json:"window_days"`
	Lanes        []BoardLane `json:"lanes"`
	Omitted      int         `json:"omitted"`
}

func (BoardView) SchemaID() string { return BoardViewSchema }

// BoardLane holds the cards of one lane and its total.
type BoardLane struct {
	Lane  string      `json:"lane"`
	Count int         `json:"count"`
	Cards []BoardCard `json:"cards"`
}

// BoardCard is one Intent's current revision with the rule that placed it.
type BoardCard struct {
	IntentID          uuid.UUID       `json:"intent_id"`
	IntentRevision    int             `json:"intent_revision"`
	Status            string          `json:"status"`
	SummaryLine       string          `json:"summary_line"`
	OwnerHumanID      *uuid.UUID      `json:"owner_human_id,omitempty"`
	ObjectiveID       uuid.UUID       `json:"objective_id"`
	ObjectiveRevision int             `json:"objective_revision"`
	RepositoryID      *uuid.UUID      `json:"repository_id,omitempty"`
	BranchRef         string          `json:"branch_ref,omitempty"`
	BaseRevisionRef   string          `json:"base_revision_ref,omitempty"`
	HeadRevisionRef   string          `json:"head_revision_ref,omitempty"`
	OpenCollisionIDs  []uuid.UUID     `json:"open_collision_ids"`
	Acceptance        BoardAcceptance `json:"acceptance"`
	LatestEvidenceAt  *Timestamp      `json:"latest_evidence_at,omitempty"`
	EvidenceStale     bool            `json:"evidence_stale"`
	ReleasedAt        *Timestamp      `json:"released_at,omitempty"`
	Reason            BoardReason     `json:"reason"`
}

// BoardAcceptance counts the Objective revision's acceptance criteria that
// have passing, non-stale evidence for this Intent.
type BoardAcceptance struct {
	Required    int      `json:"required"`
	Passing     int      `json:"passing"`
	MissingKeys []string `json:"missing_keys"`
}

// BoardReason names the rule that placed a card, the facts behind it, and
// what would move it.
type BoardReason struct {
	Rule          string `json:"rule"`
	FactsMarkdown string `json:"facts_markdown"`
	NextStep      string `json:"next_step,omitempty"`
}

const CollisionListSchema = "urn:conn:schema:v1:collision-list"

// MaxCollisionList bounds the collision-list contract.
const MaxCollisionList = 100

// CollisionList lists the most recent collisions of a Project in one state,
// newest first.
type CollisionList struct {
	ProjectID  uuid.UUID             `json:"project_id"`
	State      string                `json:"state"`
	Collisions []CollisionAssessment `json:"collisions"`
	Truncated  bool                  `json:"truncated"`
}

func (CollisionList) SchemaID() string { return CollisionListSchema }
