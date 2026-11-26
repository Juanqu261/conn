package protocol

import (
	"fmt"
	"uuid"
)

const WorkContextSchema = "urn:conn:schema:v1:work-context"

// WorkContext is a bounded view of the exact authoritative context governing
// one participant's focused work at a Project event cursor.
type WorkContext struct {
	Project                    WorkContextProject       `json:"project"`
	Reader                     WorkContextReader        `json:"reader"`
	Selection                  WorkContextSelection     `json:"selection"`
	Focus                      *WorkContextFocus        `json:"focus,omitempty"`
	FocusChoices               []WorkContextFocusChoice `json:"focus_choices,omitempty"`
	OwnedActiveIntents         []IntentRevision         `json:"owned_active_intents"`
	RelatedCollisions          []CollisionAssessment    `json:"related_collisions"`
	AcceptanceEvidenceCoverage []RadarEvidenceCoverage  `json:"acceptance_evidence_coverage"`
	Updates                    []EventEnvelope          `json:"updates"`
	ContinuationAfter          *int64                   `json:"continuation_after,omitempty"`
	KnownUnknowns              []string                 `json:"known_unknowns"`
	AsOfEvent                  int64                    `json:"as_of_event"`
}

func (WorkContext) SchemaID() string { return WorkContextSchema }

// WorkContextProject identifies the selected Project and the reader's role.
type WorkContextProject struct {
	ProjectID uuid.UUID `json:"project_id"`
	Name      string    `json:"name"`
	Role      Role      `json:"role"`
}

// WorkContextReader identifies the authenticated Human and their optional
// participant identity. A Participant attributes operations but grants no
// independent authority.
type WorkContextReader struct {
	Human       Human        `json:"human"`
	Participant *Participant `json:"participant,omitempty"`
}

// WorkContextSelection records how the process chose the Project. It is
// context only and never authorization.
type WorkContextSelection struct {
	Source          WorkContextSelectionSource  `json:"source"`
	RepositoryMatch *WorkContextRepositoryMatch `json:"repository_match,omitempty"`
}

type WorkContextSelectionSource string

const (
	WorkContextSelectionExplicit        WorkContextSelectionSource = "explicit"
	WorkContextSelectionEnv             WorkContextSelectionSource = "env"
	WorkContextSelectionSoleProject     WorkContextSelectionSource = "sole_project"
	WorkContextSelectionRepositoryMatch WorkContextSelectionSource = "repository_match"
)

// WorkContextRepositoryMatch is the credential-free repository identity that
// produced an exact local Git match.
type WorkContextRepositoryMatch struct {
	RepositoryID    uuid.UUID `json:"repository_id"`
	CanonicalRemote string    `json:"canonical_remote"`
}

// WorkContextFocus is one unambiguous Objective revision and the reader's
// governing records. OwnCharters never contains another human's charter.
type WorkContextFocus struct {
	Objective      Objective             `json:"objective"`
	Blueprint      *WorkContextBlueprint `json:"blueprint,omitempty"`
	OwnCharters    []OwnCharter          `json:"own_charters"`
	SelectedIntent *IntentRevision       `json:"selected_intent,omitempty"`
	// CommitTrailer names the selected active Intent revision of the
	// signed-in Human, for git commit --trailer.
	CommitTrailer string               `json:"commit_trailer,omitempty"`
	Analysis      *WorkContextAnalysis `json:"analysis,omitempty"`
}

// WorkContextAnalysis is the latest observed analysis attached to the
// selected Intent at or before its current revision.
type WorkContextAnalysis struct {
	IntentRevision  int       `json:"intent_revision"`
	Status          string    `json:"status"`
	ObservedAt      Timestamp `json:"observed_at"`
	DiagnosticCodes []string  `json:"diagnostic_codes,omitempty"`
}

// Analysis statuses. Any analyzer diagnostic makes an analysis incomplete.
const (
	AnalysisComplete    = "complete"
	AnalysisIncomplete  = "incomplete"
	AnalysisNotAnalyzed = "not_analyzed"
)

// CommitTrailerKey is the Git trailer key that names a Conn Intent.
const CommitTrailerKey = "Conn-Intent"

// CommitTrailer formats the trailer that names one exact Intent revision.
func CommitTrailer(intentID uuid.UUID, revision int) string {
	return fmt.Sprintf("%s: %s@%d", CommitTrailerKey, intentID, revision)
}

// WorkContextBlueprint identifies an exact Blueprint revision without
// exposing other humans' CellCharter content.
type WorkContextBlueprint struct {
	BlueprintID       uuid.UUID       `json:"blueprint_id"`
	Revision          int             `json:"revision"`
	Status            BlueprintStatus `json:"status"`
	ObjectiveID       uuid.UUID       `json:"objective_id"`
	ObjectiveRevision int             `json:"objective_revision"`
}

// WorkContextFocusChoice is one exact focus the caller may select when an
// unambiguous combined context cannot be formed.
type WorkContextFocusChoice struct {
	ObjectiveID       uuid.UUID              `json:"objective_id"`
	ObjectiveRevision int                    `json:"objective_revision"`
	Title             string                 `json:"title"`
	IntentRefs        []WorkContextIntentRef `json:"intent_refs"`
}

// WorkContextIntentRef identifies one exact owned active Intent revision.
type WorkContextIntentRef struct {
	IntentID uuid.UUID `json:"intent_id"`
	Revision int       `json:"revision"`
}
