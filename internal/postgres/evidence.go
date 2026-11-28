package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"uuid"

	"github.com/Juanqu261/conn/internal/protocol"
)

func (t projectTx) InsertEvidenceReference(ctx context.Context, evidence protocol.EvidenceReference) error {
	document, err := protocol.Encode(evidence)
	if err != nil {
		return err
	}
	var observation any
	if evidence.Observation != nil {
		encoded, err := json.Marshal(evidence.Observation)
		if err != nil {
			return err
		}
		observation = encoded
	}
	_, err = t.tx.Exec(ctx, `
		INSERT INTO evidence_references
			(evidence_id, project_id, intent_id, objective_id, objective_revision,
			 acceptance_key, kind, state, locator, content_hash, summary,
			 producer_participant_id, observed_at, observation, document)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15)`,
		evidence.EvidenceID, evidence.ProjectID, evidence.IntentID, evidence.ObjectiveID,
		evidence.ObjectiveRevision, nullIfEmpty(evidence.AcceptanceKey), evidence.Kind,
		evidence.State, evidence.Locator, nullIfEmpty(evidence.ContentHash),
		nullIfEmpty(evidence.Summary), evidence.ProducerParticipantID, evidence.ObservedAt.Time(), observation, document)
	return err
}

func (t projectTx) IntentEvidenceReferences(ctx context.Context, projectID, intentID uuid.UUID, limit int) ([]protocol.EvidenceReference, error) {
	rows, err := t.tx.Query(ctx, `
		SELECT document FROM evidence_references
		WHERE project_id = $1 AND intent_id = $2
		ORDER BY observed_at DESC, evidence_id DESC
		LIMIT $3`, projectID, intentID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	evidence := []protocol.EvidenceReference{}
	for rows.Next() {
		var document []byte
		if err := rows.Scan(&document); err != nil {
			return nil, err
		}
		item, err := protocol.Decode[protocol.EvidenceReference](document)
		if err != nil {
			return nil, fmt.Errorf("stored evidence reference: %w", err)
		}
		evidence = append(evidence, item)
	}
	return evidence, rows.Err()
}

func (t projectTx) ResolutionDecision(ctx context.Context, projectID, decisionID uuid.UUID) (protocol.ResolutionDecision, error) {
	var item protocol.ResolutionDecision
	var references []byte
	var createdAt time.Time
	err := t.tx.QueryRow(ctx, `SELECT decision_id, project_id, collision_id, selected_proposal_id, decided_by_human_id, kind, binding, rationale_markdown, references_json, created_at
		FROM resolution_decisions WHERE project_id = $1 AND decision_id = $2`, projectID, decisionID).
		Scan(&item.DecisionID, &item.ProjectID, &item.CollisionID, &item.SelectedProposalID, &item.DecidedByHumanID, &item.Kind, &item.Binding, &item.RationaleMarkdown, &references, &createdAt)
	if err != nil {
		return protocol.ResolutionDecision{}, notFound(err)
	}
	item.CreatedAt = protocol.NewTimestamp(createdAt)
	if err := json.Unmarshal(references, &item.References); err != nil {
		return protocol.ResolutionDecision{}, fmt.Errorf("stored resolution decision references: %w", err)
	}
	if len(item.References) == 0 {
		item.References = nil
	}
	return item, nil
}

func (t projectTx) LatestAnalysis(ctx context.Context, projectID, intentID uuid.UUID, maxRevision int) (protocol.WorkContextAnalysis, error) {
	var analysis protocol.WorkContextAnalysis
	var diagnostics []byte
	var observedAt time.Time
	err := t.tx.QueryRow(ctx, `
		SELECT intent_revision, diagnostics, observed_at FROM intent_analysis_reports
		WHERE project_id = $1 AND intent_id = $2 AND intent_revision <= $3
		ORDER BY intent_revision DESC LIMIT 1`, projectID, intentID, maxRevision).
		Scan(&analysis.IntentRevision, &diagnostics, &observedAt)
	if err != nil {
		return protocol.WorkContextAnalysis{}, notFound(err)
	}
	var items []protocol.AnalyzerDiagnostic
	if err := json.Unmarshal(diagnostics, &items); err != nil {
		return protocol.WorkContextAnalysis{}, fmt.Errorf("stored analyzer diagnostics: %w", err)
	}
	analysis.ObservedAt = protocol.NewTimestamp(observedAt)
	analysis.Status = protocol.AnalysisComplete
	seen := map[string]bool{}
	for _, item := range items {
		analysis.Status = protocol.AnalysisIncomplete
		if !seen[item.Code] && len(analysis.DiagnosticCodes) < 20 {
			seen[item.Code] = true
			analysis.DiagnosticCodes = append(analysis.DiagnosticCodes, item.Code)
		}
	}
	return analysis, nil
}

func nullIfEmpty(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func (t projectTx) AnalyzedRevisions(ctx context.Context, projectID uuid.UUID) (map[uuid.UUID]int, error) {
	rows, err := t.tx.Query(ctx, `
		SELECT intent_id, max(intent_revision) FROM intent_analysis_reports
		WHERE project_id = $1
		GROUP BY intent_id`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	analyzed := map[uuid.UUID]int{}
	for rows.Next() {
		var intentID uuid.UUID
		var revision int
		if err := rows.Scan(&intentID, &revision); err != nil {
			return nil, err
		}
		analyzed[intentID] = revision
	}
	return analyzed, rows.Err()
}

func (t projectTx) IntentReleases(ctx context.Context, projectID uuid.UUID, since time.Time) (map[uuid.UUID]time.Time, error) {
	rows, err := t.tx.Query(ctx, `
		SELECT entity_id, max(occurred_at) FROM events
		WHERE project_id = $1 AND event_type = 'intent.released' AND occurred_at >= $2
		GROUP BY entity_id`, projectID, since)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	released := map[uuid.UUID]time.Time{}
	for rows.Next() {
		var intentID uuid.UUID
		var at time.Time
		if err := rows.Scan(&intentID, &at); err != nil {
			return nil, err
		}
		released[intentID] = at.UTC()
	}
	return released, rows.Err()
}
