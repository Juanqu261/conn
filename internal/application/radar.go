package application

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	"uuid"

	"github.com/Juanqu261/conn/internal/protocol"
)

// Radar assembles one member-authorized coordination snapshot. All reads run
// in the Store's repeatable-read transaction, including its event cursor.
func (p *Projects) Radar(ctx context.Context, actor, projectID uuid.UUID) (Result[protocol.RadarView], error) {
	var result Result[protocol.RadarView]
	err := p.Store.Read(ctx, func(tx Tx) error {
		if _, err := tx.Role(ctx, projectID, actor); err != nil {
			return err
		}
		project, err := tx.Project(ctx, projectID)
		if err != nil {
			return err
		}
		objectives, err := tx.CurrentObjectives(ctx, projectID)
		if err != nil {
			return err
		}
		repositories, err := tx.RepositoryConnections(ctx, projectID)
		if err != nil {
			return err
		}
		intents, err := tx.Intents(ctx, projectID)
		if err != nil {
			return err
		}
		claims, err := tx.ActiveSemanticClaims(ctx, projectID)
		if err != nil {
			return err
		}
		collisions, err := tx.OpenCollisions(ctx, projectID)
		if err != nil {
			return err
		}
		decided, err := tx.DecidedCollisions(ctx, projectID, maxRadarDecidedCollisions)
		if err != nil {
			return err
		}
		decisionRefs, err := tx.ResolutionDecisionRefs(ctx, projectID)
		if err != nil {
			return err
		}
		decisions, err := tx.ResolutionDecisions(ctx, projectID)
		if err != nil {
			return err
		}
		evidence, err := tx.ActiveEvidenceReferences(ctx, projectID)
		if err != nil {
			return err
		}
		var activeBlueprint *protocol.BlueprintRevision
		if blueprint, err := tx.ActiveBlueprint(ctx, projectID); err == nil {
			activeBlueprint = &blueprint
		} else if !errors.Is(err, ErrNotFound) {
			return err
		}

		view := buildRadar(project, objectives, activeBlueprint, repositories, intents, claims, collisions, decided, decisionRefs, decisions, evidence)
		result.AsOfEvent, err = tx.LastEvent(ctx, projectID)
		if err != nil {
			return err
		}
		view.AsOfEvent = result.AsOfEvent
		if err := validateRecord(view); err != nil {
			return err
		}
		result.Data = view
		return nil
	})
	return result, err
}

func buildRadar(project protocol.Project, objectives []protocol.Objective, activeBlueprint *protocol.BlueprintRevision, repositories []protocol.RepositoryConnection, intents []protocol.IntentView, claims []protocol.SemanticClaim, collisions, decided []protocol.CollisionAssessment, decisionRefs []uuid.UUID, decisions []protocol.ResolutionDecision, evidence []protocol.EvidenceReference) protocol.RadarView {
	view := protocol.RadarView{
		Project: project, Objectives: make([]protocol.RadarObjective, 0, len(objectives)),
		Repositories: repositories, ActiveIntents: []protocol.IntentRevision{}, SemanticClaims: claims,
		OpenCollisions: collisions, DecidedCollisions: decided, DecisionRefs: decisionRefs, Dependencies: []protocol.RadarDependency{},
		OpenQuestions: []string{}, Assumptions: []string{}, EvidenceCoverage: []protocol.RadarEvidenceCoverage{}, KnownUnknowns: []string{},
	}
	// The ledger is append-only and unbounded; the Radar contract is not.
	// Keep the most recent references and label every truncation.
	if len(view.DecisionRefs) > maxRadarDecisionRefs {
		view.KnownUnknowns = append(view.KnownUnknowns, fmt.Sprintf("Only the %d most recent of %d resolution decisions are referenced; older decisions remain in the event history.", maxRadarDecisionRefs, len(view.DecisionRefs)))
		view.DecisionRefs = view.DecisionRefs[len(view.DecisionRefs)-maxRadarDecisionRefs:]
	}
	truncatedEvidence := false
	heads := intentHeads(intents)
	for _, objective := range objectives {
		view.Objectives = append(view.Objectives, protocol.RadarObjective{ObjectiveID: objective.ObjectiveID, Revision: objective.Revision, Title: objective.Title})
		for _, point := range objective.KeyPoints {
			switch point.Kind {
			case protocol.KeyPointQuestion:
				view.OpenQuestions = append(view.OpenQuestions, point.Statement)
			case protocol.KeyPointAcceptanceCriterion:
				coverage, truncated := coverageFor(objective, point.Key, evidence, heads)
				truncatedEvidence = truncatedEvidence || truncated
				view.EvidenceCoverage = append(view.EvidenceCoverage, coverage)
			}
		}
	}
	if truncatedEvidence {
		view.KnownUnknowns = append(view.KnownUnknowns, fmt.Sprintf("Some acceptance criteria have more than %d evidence references; only the most recent are listed, and coverage state uses the latest.", maxRadarCoverageRefs))
	}
	view.KnownUnknowns = append(view.KnownUnknowns, coverageNotes(view.EvidenceCoverage)...)
	if len(view.EvidenceCoverage) > maxRadarCoverage {
		view.KnownUnknowns = append(view.KnownUnknowns, fmt.Sprintf("Only %d of %d keyed acceptance criteria are shown in evidence coverage.", maxRadarCoverage, len(view.EvidenceCoverage)))
		view.EvidenceCoverage = view.EvidenceCoverage[:maxRadarCoverage]
	}
	if activeBlueprint != nil {
		view.ActiveBlueprint = &protocol.RadarBlueprint{BlueprintID: activeBlueprint.BlueprintID, Revision: activeBlueprint.Revision}
	}
	for _, intent := range intents {
		if intent.Status != "active" {
			continue
		}
		view.ActiveIntents = append(view.ActiveIntents, intent.IntentRevision)
		view.Assumptions = append(view.Assumptions, intent.IntentRevision.Assumptions...)
	}
	for _, claim := range claims {
		if claim.SubjectKind == "question" || claim.Relation == "questions" {
			view.OpenQuestions = append(view.OpenQuestions, claim.DisplayName)
		}
	}
	view.SemanticConcepts = radarConcepts(claims)
	view.OpenQuestions = uniqueBounded(view.OpenQuestions, 200)
	view.Assumptions = uniqueBounded(view.Assumptions, 200)
	view.Dependencies, view.KnownUnknowns = radarDependencies(activeBlueprint, view.ActiveIntents, view.KnownUnknowns)
	view.Dependencies, view.KnownUnknowns = resolutionDependencies(view.Dependencies, view.KnownUnknowns, append(append([]protocol.CollisionAssessment{}, collisions...), decided...), decisions)
	if len(objectives) == 0 {
		view.KnownUnknowns = append(view.KnownUnknowns, "No governing Objective has been recorded.")
	}
	if activeBlueprint == nil {
		view.KnownUnknowns = append(view.KnownUnknowns, optionalBlueprintNote)
	}
	if len(repositories) == 0 {
		view.KnownUnknowns = append(view.KnownUnknowns, "No repository connections are recorded, so repository scope is unknown.")
	}
	if len(view.ActiveIntents) == 0 {
		view.KnownUnknowns = append(view.KnownUnknowns, "No active work Intent is published. This is absence of reported work, not project completion.")
	}
	if len(claims) == 0 {
		view.KnownUnknowns = append(view.KnownUnknowns, "No active semantic claims are available for collision assessment.")
	}
	if len(view.EvidenceCoverage) == 0 {
		view.KnownUnknowns = append(view.KnownUnknowns, "No keyed acceptance criteria are available for evidence coverage.")
	}
	for _, claim := range claims {
		if claim.ProvenanceKind == "model_inference" {
			view.KnownUnknowns = append(view.KnownUnknowns, "Model-inferred claim information is present and requires human review.")
			break
		}
	}
	if len(collisions) > 0 {
		view.KnownUnknowns = append(view.KnownUnknowns, "Open collision assessments are advisory and disputed until a human records a decision.")
	}
	view.KnownUnknowns = uniqueBounded(view.KnownUnknowns, 200)
	return view
}

// Radar contract bounds for lists whose ledger source is unbounded.
const (
	maxRadarDecisionRefs      = 200
	maxRadarDecidedCollisions = 50
	maxRadarCoverage          = 200
	maxRadarCoverageRefs      = 50
	maxRadarStatementRunes    = 1000
	truncatedStatementMarker  = "…"
)

// optionalBlueprintNote describes a missing Blueprint as optional planning
// context. Collision warnings and evidence never require one.
const optionalBlueprintNote = "No active Blueprint is in force. Blueprints and Charters are optional planning context; collision warnings and evidence work without them."

// coverageFor distinguishes absent evidence from current, stale, inaccessible,
// or unknown evidence. The most recently observed record supplies the state,
// provenance, and commit. heads maps active Intents to their recorded head
// revision; observed evidence at another commit is labelled stale.
// At most maxRadarCoverageRefs of the most recent matching records are
// referenced; truncated reports whether older references were omitted.
func coverageFor(objective protocol.Objective, key string, evidence []protocol.EvidenceReference, heads map[uuid.UUID]string) (protocol.RadarEvidenceCoverage, bool) {
	coverage := protocol.RadarEvidenceCoverage{ObjectiveID: objective.ObjectiveID, ObjectiveRevision: objective.Revision, AcceptanceKey: key, State: "missing", EvidenceRefs: []uuid.UUID{}}
	var matching []protocol.EvidenceReference
	for _, item := range evidence {
		if item.ObjectiveID == objective.ObjectiveID && item.ObjectiveRevision == objective.Revision && item.AcceptanceKey == key {
			matching = append(matching, item)
		}
	}
	sort.Slice(matching, func(i, j int) bool {
		a, b := matching[i].ObservedAt.Time(), matching[j].ObservedAt.Time()
		if !a.Equal(b) {
			return a.Before(b)
		}
		return matching[i].EvidenceID.String() < matching[j].EvidenceID.String()
	})
	if len(matching) > 0 {
		latest := matching[len(matching)-1]
		coverage.State, coverage.Provenance = latest.State, latest.Provenance()
		if latest.Observation != nil {
			coverage.Commit = latest.Observation.Commit
			if latest.IntentID != nil {
				if head := heads[*latest.IntentID]; head != "" && !protocol.SameCommit(head, coverage.Commit) {
					coverage.Stale = true
				}
			}
		}
	}
	truncated := len(matching) > maxRadarCoverageRefs
	if truncated {
		matching = matching[len(matching)-maxRadarCoverageRefs:]
	}
	for _, item := range matching {
		coverage.EvidenceRefs = append(coverage.EvidenceRefs, item.EvidenceID)
	}
	sort.Slice(coverage.EvidenceRefs, func(i, j int) bool { return coverage.EvidenceRefs[i].String() < coverage.EvidenceRefs[j].String() })
	return coverage, truncated
}

// coverageNotes labels local observations and stale coverage.
func coverageNotes(coverage []protocol.RadarEvidenceCoverage) []string {
	var notes []string
	observed, stale := false, false
	for _, item := range coverage {
		observed = observed || item.Provenance == protocol.ProvenanceObservedRun
		stale = stale || item.Stale
	}
	if observed {
		notes = append(notes, localObservationNote)
	}
	if stale {
		notes = append(notes, "Some acceptance evidence was observed at a commit other than its Intent's recorded head revision and is labelled stale.")
	}
	return notes
}

// localObservationNote labels observed runs at their trust level.
const localObservationNote = "Observed check runs ran on the Intent owner's machine; they are stronger than reported states but are not independent CI proof."

// intentHeads maps each active Intent to its recorded head revision, so
// evidence coverage can label observations at other commits stale.
func intentHeads(intents []protocol.IntentView) map[uuid.UUID]string {
	heads := map[uuid.UUID]string{}
	for _, intent := range intents {
		if intent.Status == "active" && intent.IntentRevision.HeadRevisionRef != "" {
			heads[intent.IntentRevision.IntentID] = intent.IntentRevision.HeadRevisionRef
		}
	}
	return heads
}

// boundedStatement fits free text into the Radar statement contract: it trims
// surrounding whitespace and truncates on a rune boundary with a marker.
func boundedStatement(text string) string {
	text = strings.TrimSpace(text)
	runes := []rune(text)
	if len(runes) <= maxRadarStatementRunes {
		return text
	}
	return strings.TrimSpace(string(runes[:maxRadarStatementRunes-1])) + truncatedStatementMarker
}

func radarConcepts(claims []protocol.SemanticClaim) []protocol.RadarSemanticConcept {
	bySubject := map[string][]uuid.UUID{}
	for _, claim := range claims {
		bySubject[claim.NormalizedSubject] = append(bySubject[claim.NormalizedSubject], claim.ClaimID)
	}
	subjects := make([]string, 0, len(bySubject))
	for subject := range bySubject {
		subjects = append(subjects, subject)
	}
	sort.Strings(subjects)
	concepts := make([]protocol.RadarSemanticConcept, 0, len(subjects))
	for _, subject := range subjects {
		refs := bySubject[subject]
		sort.Slice(refs, func(i, j int) bool { return refs[i].String() < refs[j].String() })
		if len(refs) > 100 {
			refs = refs[:100]
		}
		concepts = append(concepts, protocol.RadarSemanticConcept{NormalizedSubject: subject, ClaimRefs: refs})
	}
	if len(concepts) > 500 {
		return concepts[:500]
	}
	return concepts
}

func radarDependencies(blueprint *protocol.BlueprintRevision, intents []protocol.IntentRevision, unknowns []string) ([]protocol.RadarDependency, []string) {
	if blueprint == nil {
		return []protocol.RadarDependency{}, unknowns
	}
	byCharter := map[uuid.UUID][]uuid.UUID{}
	for _, intent := range intents {
		if intent.CharterID != nil {
			byCharter[*intent.CharterID] = append(byCharter[*intent.CharterID], intent.IntentID)
		}
	}
	dependencies := []protocol.RadarDependency{}
	for _, cell := range blueprint.CellCharters {
		from := byCharter[cell.CellID]
		for _, dependency := range cell.Dependencies {
			to := byCharter[dependency.CellID]
			if len(from) == 0 || len(to) == 0 {
				unknowns = append(unknowns, fmt.Sprintf("Blueprint dependency %s → %s has no active Intent on one or both charter cells.", cell.CellID, dependency.CellID))
				continue
			}
			for _, source := range from {
				for _, target := range to {
					dependencies = append(dependencies, protocol.RadarDependency{FromIntentID: source, ToIntentID: target, Summary: dependency.Summary})
				}
			}
		}
	}
	sort.Slice(dependencies, func(i, j int) bool {
		if dependencies[i].FromIntentID != dependencies[j].FromIntentID {
			return dependencies[i].FromIntentID.String() < dependencies[j].FromIntentID.String()
		}
		return dependencies[i].ToIntentID.String() < dependencies[j].ToIntentID.String()
	})
	if len(dependencies) > 500 {
		dependencies = dependencies[:500]
	}
	return dependencies, unknowns
}

// resolutionDependencies projects dependency decisions onto Radar while the
// decided collision's two Intent revisions remain active. collisions contains
// the open, under-review, and decided assessments on active revisions.
func resolutionDependencies(existing []protocol.RadarDependency, unknowns []string, collisions []protocol.CollisionAssessment, decisions []protocol.ResolutionDecision) ([]protocol.RadarDependency, []string) {
	byID := make(map[uuid.UUID]protocol.CollisionAssessment, len(collisions))
	byDecision := make(map[uuid.UUID]protocol.CollisionAssessment, len(collisions))
	for _, collision := range collisions {
		byID[collision.CollisionID] = collision
		if collision.DecisionRef != nil {
			byDecision[*collision.DecisionRef] = collision
		}
	}
	for _, decision := range decisions {
		if decision.Kind != "dependency" {
			continue
		}
		// A later revision's assessment may carry the decision forward under
		// a new collision identity.
		collision, ok := byDecision[decision.DecisionID]
		if !ok {
			// Decisions recorded before collision states followed decisions
			// still project onto their open collision.
			collision, ok = byID[decision.CollisionID]
			ok = ok && collision.DecisionRef == nil
		}
		if !ok {
			unknowns = append(unknowns, fmt.Sprintf("Dependency decision %s references a collision whose Intent revisions are no longer active; review its historical collision context.", decision.DecisionID))
			continue
		}
		existing = append(existing, protocol.RadarDependency{FromIntentID: collision.LeftIntentID, ToIntentID: collision.RightIntentID, Summary: boundedStatement(decision.RationaleMarkdown)})
	}
	sort.Slice(existing, func(i, j int) bool {
		if existing[i].FromIntentID != existing[j].FromIntentID {
			return existing[i].FromIntentID.String() < existing[j].FromIntentID.String()
		}
		if existing[i].ToIntentID != existing[j].ToIntentID {
			return existing[i].ToIntentID.String() < existing[j].ToIntentID.String()
		}
		return existing[i].Summary < existing[j].Summary
	})
	if len(existing) > 500 {
		existing = existing[:500]
	}
	return existing, unknowns
}

func uniqueBounded(values []string, limit int) []string {
	seen := make(map[string]bool, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		if value != "" && !seen[value] {
			seen[value] = true
			result = append(result, value)
			if len(result) == limit {
				break
			}
		}
	}
	return result
}
