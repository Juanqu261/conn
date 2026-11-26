package protocol

import "uuid"

const ConceptListSchema = "urn:conn:schema:v1:concept-list"

// ConceptList is one bounded page of the Project concept catalog. The
// catalog is a read model over current Objective revisions and active claims;
// it is never stored.
type ConceptList struct {
	ProjectID         uuid.UUID      `json:"project_id"`
	Concepts          []ConceptEntry `json:"concepts"`
	HasMore           bool           `json:"has_more"`
	ContinuationAfter string         `json:"continuation_after,omitempty"`
}

func (ConceptList) SchemaID() string { return ConceptListSchema }

// Concept sources.
const (
	ConceptSourceObjective   = "objective"
	ConceptSourceActiveClaim = "active_claim"
)

// ConceptEntry is one normalized subject with the Objective key points and
// active claims that use it. Score and Reason are present only for a query.
type ConceptEntry struct {
	NormalizedSubject string                `json:"normalized_subject"`
	SubjectKind       string                `json:"subject_kind"`
	DisplayName       string                `json:"display_name"`
	Sources           []string              `json:"sources"`
	ObjectiveRefs     []ConceptObjectiveRef `json:"objective_refs"`
	Claims            []ConceptClaimRef     `json:"claims"`
	ClaimCount        int                   `json:"claim_count"`
	Score             *float64              `json:"score,omitempty"`
	Reason            string                `json:"reason,omitempty"`
}

// ConceptObjectiveRef names the current Objective key point that derives a
// catalog subject.
type ConceptObjectiveRef struct {
	ObjectiveID       uuid.UUID `json:"objective_id"`
	ObjectiveRevision int       `json:"objective_revision"`
	KeyPointKey       string    `json:"key_point_key"`
}

// ConceptClaimRef identifies one active claim on a catalog subject.
type ConceptClaimRef struct {
	ClaimID        uuid.UUID `json:"claim_id"`
	IntentID       uuid.UUID `json:"intent_id"`
	IntentRevision int       `json:"intent_revision"`
	Relation       string    `json:"relation"`
	ProvenanceKind string    `json:"provenance_kind"`
}
