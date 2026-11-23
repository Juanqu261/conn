package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"uuid"

	"github.com/Juanqu261/conn/internal/application"
	"github.com/Juanqu261/conn/internal/protocol"
)

func (t projectTx) CollisionAssessment(ctx context.Context, projectID, collisionID uuid.UUID) (protocol.CollisionAssessment, error) {
	var document []byte
	err := t.tx.QueryRow(ctx, `SELECT document FROM collision_assessments WHERE project_id = $1 AND collision_id = $2`, projectID, collisionID).Scan(&document)
	if err != nil {
		return protocol.CollisionAssessment{}, notFound(err)
	}
	item, err := protocol.Decode[protocol.CollisionAssessment](document)
	if err != nil {
		return protocol.CollisionAssessment{}, fmt.Errorf("stored collision assessment: %w", err)
	}
	return item, nil
}

func (t projectTx) SemanticClaims(ctx context.Context, projectID uuid.UUID, claimIDs []uuid.UUID) ([]protocol.SemanticClaim, error) {
	if len(claimIDs) == 0 {
		return []protocol.SemanticClaim{}, nil
	}
	rows, err := t.tx.Query(ctx, `SELECT document FROM semantic_claims WHERE project_id = $1 AND claim_id = ANY($2) ORDER BY normalized_subject, claim_id`, projectID, claimIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	claims := make([]protocol.SemanticClaim, 0, len(claimIDs))
	for rows.Next() {
		var document []byte
		if err := rows.Scan(&document); err != nil {
			return nil, err
		}
		claim, err := protocol.Decode[protocol.SemanticClaim](document)
		if err != nil {
			return nil, fmt.Errorf("stored semantic claim: %w", err)
		}
		claims = append(claims, claim)
	}
	return claims, rows.Err()
}

func (t projectTx) InsertResolutionProposal(ctx context.Context, proposal protocol.ResolutionProposal) error {
	references := proposal.References
	if references == nil {
		references = []string{}
	}
	encodedReferences, err := json.Marshal(references)
	if err != nil {
		return err
	}
	_, err = t.tx.Exec(ctx, `INSERT INTO resolution_proposals
		(proposal_id, project_id, collision_id, proposed_by_participant_id, proposed_by_human_id, kind, rationale_markdown, references_json, created_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)`, proposal.ProposalID, proposal.ProjectID, proposal.CollisionID,
		proposal.ProposedByParticipantID, proposal.ProposedByHumanID, proposal.Kind, proposal.RationaleMarkdown, encodedReferences, proposal.CreatedAt.Time())
	return err
}

func (t projectTx) InsertResolutionDecision(ctx context.Context, decision protocol.ResolutionDecision) error {
	references, err := json.Marshal(decision.References)
	if err != nil {
		return err
	}
	_, err = t.tx.Exec(ctx, `INSERT INTO resolution_decisions
		(decision_id, project_id, collision_id, selected_proposal_id, decided_by_human_id, kind, binding, rationale_markdown, references_json, created_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`, decision.DecisionID, decision.ProjectID, decision.CollisionID,
		decision.SelectedProposalID, decision.DecidedByHumanID, decision.Kind, decision.Binding, decision.RationaleMarkdown, references, decision.CreatedAt.Time())
	return err
}

func (t projectTx) Proposal(ctx context.Context, projectID, collisionID, proposalID uuid.UUID) (protocol.ResolutionProposal, error) {
	var item protocol.ResolutionProposal
	var participantID, humanID *uuid.UUID
	var references []byte
	var createdAt time.Time
	err := t.tx.QueryRow(ctx, `SELECT proposal_id, project_id, collision_id, proposed_by_participant_id, proposed_by_human_id, kind, rationale_markdown, references_json, created_at
		FROM resolution_proposals WHERE project_id = $1 AND collision_id = $2 AND proposal_id = $3`, projectID, collisionID, proposalID).
		Scan(&item.ProposalID, &item.ProjectID, &item.CollisionID, &participantID, &humanID, &item.Kind, &item.RationaleMarkdown, &references, &createdAt)
	if err != nil {
		return protocol.ResolutionProposal{}, notFound(err)
	}
	item.ProposedByParticipantID, item.ProposedByHumanID, item.CreatedAt = participantID, humanID, protocol.NewTimestamp(createdAt)
	if err := json.Unmarshal(references, &item.References); err != nil {
		return protocol.ResolutionProposal{}, fmt.Errorf("stored resolution proposal references: %w", err)
	}
	return item, nil
}

func (t projectTx) ResolutionDecisionRefsForCollision(ctx context.Context, projectID, collisionID uuid.UUID) ([]uuid.UUID, error) {
	rows, err := t.tx.Query(ctx, `SELECT decision_id FROM resolution_decisions WHERE project_id = $1 AND collision_id = $2 ORDER BY created_at, decision_id`, projectID, collisionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ids := []uuid.UUID{}
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

var _ application.Tx = projectTx{}
