package application

import (
	"context"
	"fmt"
	"sort"
	"time"

	"uuid"

	"github.com/Juanqu261/conn/internal/protocol"
)

// InsightCounts are the Project-level facts in one window. They hold rule
// IDs, kinds, outcomes, times, and counts, never a person.
type InsightCounts struct {
	// OpenedByRule counts collision.opened events in the window per rule.
	OpenedByRule map[string]int
	// Decisions counts decisions recorded in the window per rule and kind.
	Decisions []InsightDecisionFact
	// FirstDecisions lists, for every collision whose first decision falls
	// in the window, when it was assessed, first decided, and whether either
	// Intent was released before that decision.
	FirstDecisions []InsightFirstDecision
	// AnalyzedRevisions counts Intent revisions analyzed in the window;
	// UndeclaredRevisions counts those whose observed changes or removals
	// include a code-symbol or contract subject no declared claim names.
	AnalyzedRevisions   int
	UndeclaredRevisions int
	// CombinationOutcomes counts combination checks in the window per outcome.
	CombinationOutcomes map[string]int
}

type InsightDecisionFact struct {
	RuleID string
	Kind   string
	Count  int
}

type InsightFirstDecision struct {
	RuleID              string
	AssessedAt          time.Time
	DecidedAt           time.Time
	ReleasedBeforeFirst bool
}

var (
	insightRules    = []string{"shared_declared_invariant", "shared_exported_symbol", "contract_producer_consumer", "schema_resource_overlap"}
	insightOutcomes = []string{protocol.CombinationPassing, protocol.CombinationFailing, protocol.CombinationMergeConflict, protocol.CombinationIncomplete}
)

// Insights returns Project-level numbers for the last sinceDays days to a
// member. It never groups or ranks by person, and every ratio with a
// denominator below the minimum reports insufficient data.
func (p *Projects) Insights(ctx context.Context, actor, projectID uuid.UUID, sinceDays int) (Result[protocol.ProjectInsights], error) {
	if sinceDays < 1 || sinceDays > protocol.MaxInsightDays {
		return Result[protocol.ProjectInsights]{}, &InvalidError{Message: fmt.Sprintf("since_days must be from 1 to %d.", protocol.MaxInsightDays)}
	}
	until := p.now()
	since := until.Add(-time.Duration(sinceDays) * 24 * time.Hour)
	var result Result[protocol.ProjectInsights]
	err := p.Store.Read(ctx, func(tx Tx) error {
		if _, err := tx.Role(ctx, projectID, actor); err != nil {
			return err
		}
		counts, err := tx.InsightCounts(ctx, projectID, since, until)
		if err != nil {
			return err
		}
		objectives, err := tx.CurrentObjectives(ctx, projectID)
		if err != nil {
			return err
		}
		evidence, err := tx.ActiveEvidenceReferences(ctx, projectID)
		if err != nil {
			return err
		}
		insights := buildInsights(projectID, sinceDays, since, until, counts, objectives, evidence)
		if err := validateRecord(insights); err != nil {
			return err
		}
		result.Data = insights
		result.AsOfEvent, err = tx.LastEvent(ctx, projectID)
		return err
	})
	return result, err
}

func buildInsights(projectID uuid.UUID, sinceDays int, since, until time.Time, counts InsightCounts, objectives []protocol.Objective, evidence []protocol.EvidenceReference) protocol.ProjectInsights {
	insights := protocol.ProjectInsights{
		ProjectID: projectID, SinceDays: sinceDays, WindowStart: protocol.NewTimestamp(since), WindowEnd: protocol.NewTimestamp(until),
		MinimumDenominator: protocol.InsightMinimumDenominator, CollisionsOpened: []protocol.InsightRuleCount{},
		Decisions: []protocol.InsightDecisionCount{}, FalsePositiveShare: []protocol.InsightRuleShare{},
		CombinationChecks: []protocol.InsightOutcomeCount{},
	}
	decidedByRule, falsePositiveByRule := map[string]int{}, map[string]int{}
	facts := append([]InsightDecisionFact(nil), counts.Decisions...)
	sort.Slice(facts, func(i, j int) bool {
		if facts[i].RuleID != facts[j].RuleID {
			return facts[i].RuleID < facts[j].RuleID
		}
		return facts[i].Kind < facts[j].Kind
	})
	for _, fact := range facts {
		insights.Decisions = append(insights.Decisions, protocol.InsightDecisionCount{RuleID: fact.RuleID, Kind: fact.Kind, Count: fact.Count})
		decidedByRule[fact.RuleID] += fact.Count
		if fact.Kind == "false_positive" {
			falsePositiveByRule[fact.RuleID] += fact.Count
		}
	}
	for _, rule := range insightRules {
		if count := counts.OpenedByRule[rule]; count > 0 {
			insights.CollisionsOpened = append(insights.CollisionsOpened, protocol.InsightRuleCount{RuleID: rule, Count: count})
		}
		if decided := decidedByRule[rule]; decided > 0 {
			insights.FalsePositiveShare = append(insights.FalsePositiveShare, protocol.InsightRuleShare{RuleID: rule, Share: protocol.NewInsightRatio(falsePositiveByRule[rule], decided)})
		}
	}

	hours := make([]float64, 0, len(counts.FirstDecisions))
	beforeRelease := 0
	for _, decision := range counts.FirstDecisions {
		hours = append(hours, max(0, decision.DecidedAt.Sub(decision.AssessedAt).Hours()))
		if !decision.ReleasedBeforeFirst {
			beforeRelease++
		}
	}
	insights.MedianHoursToFirstDecision = protocol.InsightMedian{SampleSize: len(hours), Status: protocol.InsightInsufficientData}
	if len(hours) >= protocol.InsightMinimumDenominator {
		sort.Float64s(hours)
		median := hours[len(hours)/2]
		if len(hours)%2 == 0 {
			median = (hours[len(hours)/2-1] + hours[len(hours)/2]) / 2
		}
		insights.MedianHoursToFirstDecision.Status, insights.MedianHoursToFirstDecision.Value = protocol.InsightValue, &median
	}
	insights.DecidedBeforeRelease = protocol.NewInsightRatio(beforeRelease, len(counts.FirstDecisions))
	insights.UndeclaredObservedChanges = protocol.NewInsightRatio(counts.UndeclaredRevisions, counts.AnalyzedRevisions)

	heads := map[uuid.UUID]string{}
	for _, objective := range objectives {
		for _, point := range objective.KeyPoints {
			if point.Kind != protocol.KeyPointAcceptanceCriterion {
				continue
			}
			coverage, _ := coverageFor(objective, point.Key, evidence, heads)
			switch {
			case len(coverage.EvidenceRefs) == 0:
				insights.AcceptanceByProvenance.Missing++
			case coverage.Provenance == protocol.ProvenanceObservedRun:
				insights.AcceptanceByProvenance.ObservedRun++
			default:
				insights.AcceptanceByProvenance.Reported++
			}
		}
	}
	for _, outcome := range insightOutcomes {
		if count := counts.CombinationOutcomes[outcome]; count > 0 {
			insights.CombinationChecks = append(insights.CombinationChecks, protocol.InsightOutcomeCount{Outcome: outcome, Count: count})
		}
	}
	return insights
}
