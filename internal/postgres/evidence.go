package postgres

import (
	"context"

	"github.com/Juanqu261/conn/internal/protocol"
)

func (t projectTx) InsertEvidenceReference(ctx context.Context, evidence protocol.EvidenceReference) error {
	document, err := protocol.Encode(evidence)
	if err != nil {
		return err
	}
	_, err = t.tx.Exec(ctx, `
		INSERT INTO evidence_references
			(evidence_id, project_id, intent_id, objective_id, objective_revision,
			 acceptance_key, kind, state, locator, content_hash, summary,
			 producer_participant_id, observed_at, document)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14)`,
		evidence.EvidenceID, evidence.ProjectID, evidence.IntentID, evidence.ObjectiveID,
		evidence.ObjectiveRevision, nullIfEmpty(evidence.AcceptanceKey), evidence.Kind,
		evidence.State, evidence.Locator, nullIfEmpty(evidence.ContentHash),
		nullIfEmpty(evidence.Summary), evidence.ProducerParticipantID, evidence.ObservedAt.Time(), document)
	return err
}

func nullIfEmpty(value string) any {
	if value == "" {
		return nil
	}
	return value
}
