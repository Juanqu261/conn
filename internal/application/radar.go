package application

import (
	"context"
	"errors"
	"fmt"
	"sort"

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

		view := buildRadar(project, objectives, activeBlueprint, repositories, intents, claims, collisions, decisionRefs, decisions, evidence)
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

func buildRadar(project protocol.Project, objectives []protocol.Objective, activeBlueprint *protocol.BlueprintRevision, repositories []protocol.RepositoryConnection, intents []protocol.IntentView, claims []protocol.SemanticClaim, collisions []protocol.CollisionAssessment, decisionRefs []uuid.UUID, decisions []protocol.ResolutionDecision, evidence []protocol.EvidenceReference) protocol.RadarView {
	view := protocol.RadarView{
		Project: project, Objectives: make([]protocol.RadarObjective, 0, len(objectives)),
		Repositories: repositories, ActiveIntents: []protocol.IntentRevision{}, SemanticClaims: claims,
		OpenCollisions: collisions, DecisionRefs: decisionRefs, Dependencies: []protocol.RadarDependency{},
		OpenQuestions: []string{}, Assumptions: []string{}, EvidenceCoverage: []protocol.RadarEvidenceCoverage{}, KnownUnknowns: []string{},
	}
	for _, objective := range objectives {
		view.Objectives = append(view.Objectives, protocol.RadarObjective{ObjectiveID: objective.ObjectiveID, Revision: objective.Revision, Title: objective.Title})
		for _, point := range objective.KeyPoints {
			switch point.Kind {
			case protocol.KeyPointQuestion:
				view.OpenQuestions = append(view.OpenQuestions, point.Statement)
			case protocol.KeyPointAcceptanceCriterion:
				view.EvidenceCoverage = append(view.EvidenceCoverage, coverageFor(objective, point.Key, evidence))
			}
		}
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
	view.Dependencies, view.KnownUnknowns = resolutionDependencies(view.Dependencies, view.KnownUnknowns, collisions, decisions)
	if len(objectives) == 0 {
		view.KnownUnknowns = append(view.KnownUnknowns, "No governing Objective has been recorded.")
	}
	if activeBlueprint == nil {
		view.KnownUnknowns = append(view.KnownUnknowns, "No active Blueprint is in force; work boundaries are not ratified.")
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

// coverageFor distinguishes absent evidence from current, stale, inaccessible,
// or unknown evidence. The most recently observed record supplies the state;
// every matching record remains referenced for an explainable audit trail.
func coverageFor(objective protocol.Objective, key string, evidence []protocol.EvidenceReference) protocol.RadarEvidenceCoverage {
	coverage := protocol.RadarEvidenceCoverage{ObjectiveID: objective.ObjectiveID, ObjectiveRevision: objective.Revision, AcceptanceKey: key, State: "missing", EvidenceRefs: []uuid.UUID{}}
	var latest *protocol.EvidenceReference
	for i := range evidence {
		item := &evidence[i]
		if item.ObjectiveID != objective.ObjectiveID || item.ObjectiveRevision != objective.Revision || item.AcceptanceKey != key {
			continue
		}
		coverage.EvidenceRefs = append(coverage.EvidenceRefs, item.EvidenceID)
		if latest == nil || item.ObservedAt.Time().After(latest.ObservedAt.Time()) || (item.ObservedAt.Time().Equal(latest.ObservedAt.Time()) && item.EvidenceID.String() > latest.EvidenceID.String()) {
			latest = item
		}
	}
	if latest != nil {
		coverage.State = latest.State
	}
	sort.Slice(coverage.EvidenceRefs, func(i, j int) bool { return coverage.EvidenceRefs[i].String() < coverage.EvidenceRefs[j].String() })
	return coverage
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

func resolutionDependencies(existing []protocol.RadarDependency, unknowns []string, collisions []protocol.CollisionAssessment, decisions []protocol.ResolutionDecision) ([]protocol.RadarDependency, []string) {
	byID := make(map[uuid.UUID]protocol.CollisionAssessment, len(collisions))
	for _, collision := range collisions {
		byID[collision.CollisionID] = collision
	}
	for _, decision := range decisions {
		if decision.Kind != "dependency" {
			continue
		}
		collision, ok := byID[decision.CollisionID]
		if !ok {
			unknowns = append(unknowns, fmt.Sprintf("Dependency decision %s references a collision that is no longer open; review its historical collision context.", decision.DecisionID))
			continue
		}
		existing = append(existing, protocol.RadarDependency{FromIntentID: collision.LeftIntentID, ToIntentID: collision.RightIntentID, Summary: decision.RationaleMarkdown})
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
