package application

import (
	"context"
	"errors"
	"fmt"
	"sort"

	"uuid"

	"github.com/Juanqu261/conn/internal/protocol"
)

const (
	maxWorkContextIntents    = 50
	maxWorkContextCollisions = 100
	maxWorkContextChoices    = 20
)

// WorkContextQuery selects one participant focus and an optional bounded page
// of later Project events. Human and Participant authority never come from it.
type WorkContextQuery struct {
	ObjectiveID uuid.UUID
	IntentID    uuid.UUID
	After       *int64
	Limit       int
	AdapterKind string
}

// WorkContext assembles one member-authorized focused snapshot and its event
// cursor in a single repeatable-read transaction.
func (p *Projects) WorkContext(ctx context.Context, human protocol.Human, projectID uuid.UUID, query WorkContextQuery) (Result[protocol.WorkContext], error) {
	if query.After != nil && *query.After < 0 {
		return Result[protocol.WorkContext]{}, &InvalidError{Message: "The event cursor cannot be negative."}
	}
	if query.Limit < 1 || query.Limit > MaxEventPage {
		return Result[protocol.WorkContext]{}, &InvalidError{Message: fmt.Sprintf("The event limit must be between 1 and %d.", MaxEventPage)}
	}
	if query.AdapterKind == "" {
		query.AdapterKind = "cli"
	}

	var result Result[protocol.WorkContext]
	err := p.Store.Read(ctx, func(tx Tx) error {
		role, err := tx.Role(ctx, projectID, human.HumanID)
		if err != nil {
			return err
		}
		project, err := tx.Project(ctx, projectID)
		if err != nil {
			return err
		}
		currentObjectives, err := tx.CurrentObjectives(ctx, projectID)
		if err != nil {
			return err
		}
		intents, err := tx.Intents(ctx, projectID)
		if err != nil {
			return err
		}
		blueprints, err := tx.BlueprintSummaries(ctx, projectID)
		if err != nil {
			return err
		}
		collisions, err := tx.OpenCollisions(ctx, projectID)
		if err != nil {
			return err
		}

		var participant *protocol.Participant
		if item, err := tx.Participant(ctx, projectID, human.HumanID, query.AdapterKind, ""); err == nil {
			participant = &item
		} else if !errors.Is(err, ErrNotFound) {
			return err
		}

		view := protocol.WorkContext{
			Project:                    protocol.WorkContextProject{ProjectID: project.ProjectID, Name: project.Name, Role: role},
			Reader:                     protocol.WorkContextReader{Human: human, Participant: participant},
			Selection:                  protocol.WorkContextSelection{Source: protocol.WorkContextSelectionExplicit},
			OwnedActiveIntents:         []protocol.IntentRevision{},
			RelatedCollisions:          []protocol.CollisionAssessment{},
			AcceptanceEvidenceCoverage: []protocol.RadarEvidenceCoverage{},
			Updates:                    []protocol.EventEnvelope{},
			KnownUnknowns:              []string{},
		}
		if participant == nil {
			view.KnownUnknowns = append(view.KnownUnknowns, "No Participant identity exists for this adapter yet, so no participant-owned Intent can be selected.")
		}

		currentByID := make(map[uuid.UUID]protocol.Objective, len(currentObjectives))
		for _, objective := range currentObjectives {
			currentByID[objective.ObjectiveID] = objective
		}

		selectedObjective, selectedIntent, choices, focusIncomplete, err := selectWorkFocus(ctx, tx, projectID, human.HumanID, participant, query, currentObjectives, intents, blueprints)
		if err != nil {
			return err
		}
		if len(choices) > 0 {
			view.FocusChoices = choices
			view.KnownUnknowns = append(view.KnownUnknowns, "Several independent work contexts are available; select one Objective or owned Intent before relying on a governing focus.")
			if focusIncomplete {
				view.KnownUnknowns = append(view.KnownUnknowns, "The bounded focus choices omit additional Objectives or owned active Intent references.")
			}
		}

		if selectedObjective != nil {
			focus := protocol.WorkContextFocus{Objective: *selectedObjective, OwnCharters: []protocol.OwnCharter{}, SelectedIntent: selectedIntent}
			if current, ok := currentByID[selectedObjective.ObjectiveID]; ok && current.Revision != selectedObjective.Revision {
				view.KnownUnknowns = append(view.KnownUnknowns, fmt.Sprintf("Objective %s revision %d is stale; current revision is %d.", selectedObjective.ObjectiveID, selectedObjective.Revision, current.Revision))
			}

			blueprint, ownCharters, err := workFocusBlueprint(ctx, tx, projectID, human.HumanID, *selectedObjective, selectedIntent, blueprints)
			if err != nil {
				return err
			}
			focus.Blueprint, focus.OwnCharters = blueprint, ownCharters
			blueprintAligned := blueprint != nil && blueprint.ObjectiveID == selectedObjective.ObjectiveID && blueprint.ObjectiveRevision == selectedObjective.Revision
			if blueprint == nil {
				view.KnownUnknowns = append(view.KnownUnknowns, "No Blueprint revision is available for this exact Objective revision. Blueprints and Charters are optional planning context; collision warnings and evidence work without them.")
			} else if !blueprintAligned {
				view.KnownUnknowns = append(view.KnownUnknowns, fmt.Sprintf("Blueprint %s revision %d belongs to Objective %s revision %d, not the selected Objective revision; its Charters are not governing boundaries for this focus.", blueprint.BlueprintID, blueprint.Revision, blueprint.ObjectiveID, blueprint.ObjectiveRevision))
			} else if blueprint.Status != protocol.BlueprintActive {
				view.KnownUnknowns = append(view.KnownUnknowns, fmt.Sprintf("Blueprint %s revision %d is %s and is not an accepted work boundary.", blueprint.BlueprintID, blueprint.Revision, blueprint.Status))
			}
			if blueprint != nil {
				for _, summary := range blueprints {
					if summary.BlueprintID != blueprint.BlueprintID {
						continue
					}
					for _, revision := range summary.Revisions {
						if revision.Revision != blueprint.Revision && revision.ObjectiveID == selectedObjective.ObjectiveID {
							view.KnownUnknowns = append(view.KnownUnknowns, fmt.Sprintf("Blueprint %s revision %d is %s and does not replace the exact revision shown for this focus.", summary.BlueprintID, revision.Revision, revision.Status))
						}
					}
				}
			}
			acceptedCharter := false
			for _, own := range ownCharters {
				if own.Decision == protocol.CellAccepted && blueprintAligned && blueprint.Status == protocol.BlueprintActive {
					acceptedCharter = true
				}
			}
			if !acceptedCharter && blueprint != nil {
				view.KnownUnknowns = append(view.KnownUnknowns, "No accepted active Charter governs the signed-in Human for this focus. Charters are optional planning context.")
			}

			if selectedIntent != nil {
				for _, intent := range intents {
					if intent.IntentRevision.IntentID == selectedIntent.IntentID && intent.Status != "active" {
						view.KnownUnknowns = append(view.KnownUnknowns, fmt.Sprintf("Selected Intent %s is %s; it is retained for context but is not active work.", selectedIntent.IntentID, intent.Status))
						break
					}
				}
			}

			if participant != nil {
				for _, intent := range intents {
					revision := intent.IntentRevision
					if intent.Status == "active" && revision.ParticipantID == participant.ParticipantID &&
						revision.ObjectiveID == selectedObjective.ObjectiveID && revision.ObjectiveRevision == selectedObjective.Revision {
						view.OwnedActiveIntents = append(view.OwnedActiveIntents, revision)
					}
				}
			}
			if len(view.OwnedActiveIntents) > maxWorkContextIntents {
				view.KnownUnknowns = append(view.KnownUnknowns, fmt.Sprintf("Only %d of %d owned active Intents are included.", maxWorkContextIntents, len(view.OwnedActiveIntents)))
				view.OwnedActiveIntents = view.OwnedActiveIntents[:maxWorkContextIntents]
			}
			if focus.SelectedIntent == nil && len(view.OwnedActiveIntents) == 1 {
				focus.SelectedIntent = &view.OwnedActiveIntents[0]
			} else if focus.SelectedIntent == nil && len(view.OwnedActiveIntents) > 1 {
				view.KnownUnknowns = append(view.KnownUnknowns, "Several owned active Intents share this Objective revision; select one Intent for an exact work focus.")
			}
			if len(view.OwnedActiveIntents) == 0 {
				view.KnownUnknowns = append(view.KnownUnknowns, "No owned active Intent is published for this focus. This is absence of reported work, not completion.")
			}
			if focus.SelectedIntent != nil {
				selected := *focus.SelectedIntent
				for _, intent := range intents {
					if intent.IntentRevision.IntentID == selected.IntentID && intent.Status == "active" {
						focus.CommitTrailer = protocol.CommitTrailer(selected.IntentID, selected.Revision)
					}
				}
				if analysis, err := tx.LatestAnalysis(ctx, projectID, selected.IntentID, selected.Revision); err == nil {
					focus.Analysis = &analysis
				} else if !errors.Is(err, ErrNotFound) {
					return err
				}
			}

			ownedIDs := make(map[uuid.UUID]bool, len(view.OwnedActiveIntents))
			for _, intent := range view.OwnedActiveIntents {
				ownedIDs[intent.IntentID] = true
			}
			// The focused Intent's current revision shows every state, so a
			// participant sees decided and stale collisions as well as open
			// ones. Other owned Intents contribute collisions needing attention.
			included := map[uuid.UUID]bool{}
			if focus.SelectedIntent != nil {
				focused, err := tx.CollisionsForIntentRevision(ctx, projectID, focus.SelectedIntent.IntentID, focus.SelectedIntent.Revision)
				if err != nil {
					return err
				}
				for _, collision := range focused {
					included[collision.CollisionID] = true
					view.RelatedCollisions = append(view.RelatedCollisions, collision)
				}
			}
			for _, collision := range collisions {
				if !included[collision.CollisionID] && (ownedIDs[collision.LeftIntentID] || ownedIDs[collision.RightIntentID]) {
					view.RelatedCollisions = append(view.RelatedCollisions, collision)
				}
			}
			if len(view.RelatedCollisions) > maxWorkContextCollisions {
				view.KnownUnknowns = append(view.KnownUnknowns, fmt.Sprintf("Only %d of %d related collisions are included.", maxWorkContextCollisions, len(view.RelatedCollisions)))
				view.RelatedCollisions = view.RelatedCollisions[:maxWorkContextCollisions]
			}
			if err := withCombinationOutcomes(ctx, tx, projectID, view.RelatedCollisions); err != nil {
				return err
			}
			for _, collision := range view.RelatedCollisions {
				if collision.State == protocol.CollisionOpen || collision.State == protocol.CollisionUnderReview {
					view.KnownUnknowns = append(view.KnownUnknowns, "Related collision assessments are advisory until a Human records a decision.")
					break
				}
			}

			evidence, err := tx.EvidenceReferences(ctx, projectID, selectedObjective.ObjectiveID, selectedObjective.Revision)
			if err != nil {
				return err
			}
			truncatedCoverage := false
			heads := intentHeads(intents)
			for _, point := range selectedObjective.KeyPoints {
				if point.Kind != protocol.KeyPointAcceptanceCriterion {
					continue
				}
				coverage, truncated := coverageFor(*selectedObjective, point.Key, evidence, heads)
				truncatedCoverage = truncatedCoverage || truncated
				view.AcceptanceEvidenceCoverage = append(view.AcceptanceEvidenceCoverage, coverage)
			}
			if truncatedCoverage {
				view.KnownUnknowns = append(view.KnownUnknowns, fmt.Sprintf("Some acceptance criteria have more than %d evidence references; only the most recent are included.", maxRadarCoverageRefs))
			}
			view.KnownUnknowns = append(view.KnownUnknowns, coverageNotes(view.AcceptanceEvidenceCoverage)...)
			if len(view.AcceptanceEvidenceCoverage) == 0 {
				view.KnownUnknowns = append(view.KnownUnknowns, "This Objective revision has no keyed acceptance criteria available for evidence coverage.")
			}
			view.Focus = &focus
		} else if len(choices) == 0 {
			view.KnownUnknowns = append(view.KnownUnknowns, "No governing Objective is available, so focused work context cannot be assembled.")
		}

		result.AsOfEvent, err = tx.LastEvent(ctx, projectID)
		if err != nil {
			return err
		}
		view.AsOfEvent = result.AsOfEvent
		if query.After != nil {
			view.Updates, err = tx.Events(ctx, projectID, *query.After, query.Limit)
			if err != nil {
				return err
			}
			if len(view.Updates) > 0 {
				last := view.Updates[len(view.Updates)-1].EventSequence
				if last < result.AsOfEvent {
					view.ContinuationAfter = &last
					view.KnownUnknowns = append(view.KnownUnknowns, fmt.Sprintf("More Project updates remain after event %d.", last))
				}
			}
		}
		view.KnownUnknowns = uniqueBounded(view.KnownUnknowns, 100)
		if err := validateRecord(view); err != nil {
			return err
		}
		result.Data = view
		return nil
	})
	return result, err
}

type workFocusCandidate struct {
	objective        protocol.Objective
	intents          []protocol.WorkContextIntentRef
	intentsTruncated bool
}

func selectWorkFocus(ctx context.Context, tx Tx, projectID, humanID uuid.UUID, participant *protocol.Participant, query WorkContextQuery, currentObjectives []protocol.Objective, intents []protocol.IntentView, blueprints []protocol.BlueprintSummary) (*protocol.Objective, *protocol.IntentRevision, []protocol.WorkContextFocusChoice, bool, error) {
	if query.IntentID != (uuid.UUID{}) {
		intent, err := tx.Intent(ctx, projectID, query.IntentID)
		if err != nil {
			return nil, nil, nil, false, err
		}
		// Any Participant of the signed-in Human may focus the Human's
		// Intent, so terminal check runs and review packets can address an
		// Intent that the Human's Kiro participant owns.
		if participant == nil || intent.IntentRevision.ParticipantID != participant.ParticipantID {
			owner, err := tx.ParticipantByID(ctx, projectID, intent.IntentRevision.ParticipantID)
			if err != nil {
				return nil, nil, nil, false, err
			}
			if owner.HumanID != humanID {
				return nil, nil, nil, false, ErrNotFound
			}
		}
		if query.ObjectiveID != (uuid.UUID{}) && intent.IntentRevision.ObjectiveID != query.ObjectiveID {
			return nil, nil, nil, false, &InvalidError{Message: "The selected Intent belongs to a different Objective."}
		}
		objective, err := tx.ObjectiveRevision(ctx, projectID, intent.IntentRevision.ObjectiveID, intent.IntentRevision.ObjectiveRevision)
		if err != nil {
			return nil, nil, nil, false, err
		}
		return &objective, &intent.IntentRevision, nil, false, nil
	}
	if query.ObjectiveID != (uuid.UUID{}) {
		for _, objective := range currentObjectives {
			if objective.ObjectiveID == query.ObjectiveID {
				return &objective, nil, nil, false, nil
			}
		}
		return nil, nil, nil, false, ErrNotFound
	}

	candidates := map[string]*workFocusCandidate{}
	add := func(objective protocol.Objective) *workFocusCandidate {
		key := fmt.Sprintf("%s/%d", objective.ObjectiveID, objective.Revision)
		if candidates[key] == nil {
			candidates[key] = &workFocusCandidate{objective: objective, intents: []protocol.WorkContextIntentRef{}}
		}
		return candidates[key]
	}
	if participant != nil {
		for _, intent := range intents {
			if intent.Status != "active" || intent.IntentRevision.ParticipantID != participant.ParticipantID {
				continue
			}
			objective, err := tx.ObjectiveRevision(ctx, projectID, intent.IntentRevision.ObjectiveID, intent.IntentRevision.ObjectiveRevision)
			if err != nil {
				return nil, nil, nil, false, err
			}
			candidate := add(objective)
			if len(candidate.intents) < maxWorkContextChoices {
				candidate.intents = append(candidate.intents, protocol.WorkContextIntentRef{IntentID: intent.IntentRevision.IntentID, Revision: intent.IntentRevision.Revision})
			} else {
				candidate.intentsTruncated = true
			}
		}
	}
	for _, blueprint := range blueprints {
		for _, revision := range blueprint.Revisions {
			for _, cell := range revision.Cells {
				if cell.AccountableHumanID != humanID {
					continue
				}
				objective, err := tx.ObjectiveRevision(ctx, projectID, revision.ObjectiveID, revision.ObjectiveRevision)
				if err != nil {
					return nil, nil, nil, false, err
				}
				add(objective)
				break
			}
		}
	}
	if len(candidates) == 0 {
		for _, objective := range currentObjectives {
			add(objective)
		}
	}
	ordered := make([]*workFocusCandidate, 0, len(candidates))
	for _, candidate := range candidates {
		ordered = append(ordered, candidate)
	}
	sort.Slice(ordered, func(i, j int) bool {
		if ordered[i].objective.ObjectiveID != ordered[j].objective.ObjectiveID {
			return ordered[i].objective.ObjectiveID.String() < ordered[j].objective.ObjectiveID.String()
		}
		return ordered[i].objective.Revision < ordered[j].objective.Revision
	})
	if len(ordered) == 1 {
		return &ordered[0].objective, nil, nil, false, nil
	}
	if len(ordered) == 0 {
		return nil, nil, nil, false, nil
	}
	limit := min(len(ordered), maxWorkContextChoices)
	incomplete := len(ordered) > limit
	choices := make([]protocol.WorkContextFocusChoice, 0, limit)
	for _, candidate := range ordered[:limit] {
		incomplete = incomplete || candidate.intentsTruncated
		choices = append(choices, protocol.WorkContextFocusChoice{
			ObjectiveID: candidate.objective.ObjectiveID, ObjectiveRevision: candidate.objective.Revision,
			Title: candidate.objective.Title, IntentRefs: candidate.intents,
		})
	}
	return nil, nil, choices, incomplete, nil
}

func workFocusBlueprint(ctx context.Context, tx Tx, projectID, humanID uuid.UUID, objective protocol.Objective, intent *protocol.IntentRevision, summaries []protocol.BlueprintSummary) (*protocol.WorkContextBlueprint, []protocol.OwnCharter, error) {
	var blueprintID uuid.UUID
	var revision int
	if intent != nil && intent.BlueprintID != nil && intent.BlueprintRevision != nil {
		blueprintID, revision = *intent.BlueprintID, *intent.BlueprintRevision
	} else {
		var chosen *protocol.RevisionSummary
		for _, summary := range summaries {
			for _, candidate := range summary.Revisions {
				if candidate.ObjectiveID != objective.ObjectiveID || candidate.ObjectiveRevision != objective.Revision {
					continue
				}
				if chosen == nil || candidate.Status == protocol.BlueprintActive || chosen.Status != protocol.BlueprintActive && candidate.Revision > chosen.Revision {
					copy := candidate
					chosen, blueprintID = &copy, summary.BlueprintID
				}
			}
		}
		if chosen == nil {
			return nil, []protocol.OwnCharter{}, nil
		}
		revision = chosen.Revision
	}
	view, err := tx.BlueprintRevision(ctx, projectID, blueprintID, revision)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return nil, []protocol.OwnCharter{}, nil
		}
		return nil, nil, err
	}
	blueprint := &protocol.WorkContextBlueprint{
		BlueprintID: view.BlueprintRevision.BlueprintID, Revision: view.BlueprintRevision.Revision, Status: view.Status,
		ObjectiveID: view.BlueprintRevision.ObjectiveID, ObjectiveRevision: view.BlueprintRevision.ObjectiveRevision,
	}
	own := []protocol.OwnCharter{}
	for _, charter := range view.BlueprintRevision.CellCharters {
		if charter.AccountableHumanID != humanID {
			continue
		}
		decision := protocol.CellPending
		if item, err := tx.CharterDecision(ctx, blueprintID, revision, charter.CellID); err == nil {
			decision = protocol.CellDecision(item.Outcome)
		} else if !errors.Is(err, ErrNotFound) {
			return nil, nil, err
		}
		own = append(own, protocol.OwnCharter{Charter: charter, Decision: decision})
	}
	return blueprint, own, nil
}
