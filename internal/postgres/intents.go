package postgres

import (
	"context"
	"fmt"
	"time"

	"uuid"

	"github.com/Juanqu261/conn/internal/application"
	"github.com/Juanqu261/conn/internal/protocol"
)

func (t projectTx) ParticipantByID(ctx context.Context, projectID, participantID uuid.UUID) (protocol.Participant, error) {
	var p protocol.Participant
	var localRef, label *string
	var createdAt time.Time
	err := t.tx.QueryRow(ctx, `
		SELECT participant_id, project_id, human_id, adapter_kind, local_session_ref, display_label, created_at
		FROM participants WHERE project_id = $1 AND participant_id = $2`, projectID, participantID).
		Scan(&p.ParticipantID, &p.ProjectID, &p.HumanID, &p.AdapterKind, &localRef, &label, &createdAt)
	if localRef != nil {
		p.LocalSessionRef = *localRef
	}
	if label != nil {
		p.DisplayLabel = *label
	}
	p.CreatedAt = protocol.NewTimestamp(createdAt)
	return p, notFound(err)
}

func (t projectTx) InsertIntentRevision(ctx context.Context, revision protocol.IntentRevision) (bool, error) {
	document, err := protocol.Encode(revision)
	if err != nil {
		return false, err
	}
	var applied bool
	if revision.Revision == 1 {
		tag, err := t.tx.Exec(ctx, `
			INSERT INTO intents (intent_id, project_id, participant_id, status, current_revision, created_at)
			VALUES ($1, $2, $3, 'active', 1, $4) ON CONFLICT (intent_id) DO NOTHING`,
			revision.IntentID, revision.ProjectID, revision.ParticipantID, revision.CreatedAt.Time())
		if err != nil {
			return false, err
		}
		applied = tag.RowsAffected() == 1
	} else {
		tag, err := t.tx.Exec(ctx, `
			UPDATE intents SET current_revision = $4
			WHERE intent_id = $1 AND project_id = $2 AND participant_id = $3
			  AND status = 'active' AND current_revision = $4 - 1`,
			revision.IntentID, revision.ProjectID, revision.ParticipantID, revision.Revision)
		if err != nil {
			return false, err
		}
		applied = tag.RowsAffected() == 1
	}
	if !applied {
		return false, nil
	}
	_, err = t.tx.Exec(ctx, `
		INSERT INTO intent_revisions
			(intent_id, revision, project_id, participant_id, objective_id, objective_revision,
			 blueprint_id, blueprint_revision, charter_id, repository_id, base_revision_ref,
			 head_revision_ref, branch_ref, created_at, document)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,NULLIF($11,''),NULLIF($12,''),NULLIF($13,''),$14,$15)`,
		revision.IntentID, revision.Revision, revision.ProjectID, revision.ParticipantID, revision.ObjectiveID, revision.ObjectiveRevision,
		revision.BlueprintID, revision.BlueprintRevision, revision.CharterID, revision.RepositoryID, revision.BaseRevisionRef,
		revision.HeadRevisionRef, revision.BranchRef, revision.CreatedAt.Time(), document)
	return err == nil, err
}

func (t projectTx) Intent(ctx context.Context, projectID, intentID uuid.UUID) (protocol.IntentView, error) {
	var status string
	var owner uuid.UUID
	var document []byte
	err := t.tx.QueryRow(ctx, `
		SELECT i.status, p.human_id, r.document FROM intents i
		JOIN intent_revisions r ON r.intent_id = i.intent_id AND r.revision = i.current_revision
		JOIN participants p ON p.participant_id = i.participant_id
		WHERE i.project_id = $1 AND i.intent_id = $2`, projectID, intentID).Scan(&status, &owner, &document)
	if err != nil {
		return protocol.IntentView{}, notFound(err)
	}
	revision, err := protocol.Decode[protocol.IntentRevision](document)
	if err != nil {
		return protocol.IntentView{}, fmt.Errorf("stored intent revision: %w", err)
	}
	return intentView(revision, status, owner), nil
}

func (t projectTx) Intents(ctx context.Context, projectID uuid.UUID) ([]protocol.IntentView, error) {
	rows, err := t.tx.Query(ctx, `
		SELECT i.status, p.human_id, r.document FROM intents i
		JOIN intent_revisions r ON r.intent_id = i.intent_id AND r.revision = i.current_revision
		JOIN participants p ON p.participant_id = i.participant_id
		WHERE i.project_id = $1 ORDER BY i.created_at, i.intent_id`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []protocol.IntentView{}
	for rows.Next() {
		var status string
		var owner uuid.UUID
		var document []byte
		if err := rows.Scan(&status, &owner, &document); err != nil {
			return nil, err
		}
		revision, err := protocol.Decode[protocol.IntentRevision](document)
		if err != nil {
			return nil, fmt.Errorf("stored intent revision: %w", err)
		}
		items = append(items, intentView(revision, status, owner))
	}
	return items, rows.Err()
}

func (t projectTx) IntentRevision(ctx context.Context, projectID, intentID uuid.UUID, revision int) (protocol.IntentRevision, error) {
	var document []byte
	err := t.tx.QueryRow(ctx, `SELECT document FROM intent_revisions WHERE project_id = $1 AND intent_id = $2 AND revision = $3`, projectID, intentID, revision).Scan(&document)
	if err != nil {
		return protocol.IntentRevision{}, notFound(err)
	}
	item, err := protocol.Decode[protocol.IntentRevision](document)
	if err != nil {
		return protocol.IntentRevision{}, fmt.Errorf("stored intent revision: %w", err)
	}
	return item, nil
}

func (t projectTx) ReleaseIntent(ctx context.Context, projectID, intentID, participantID uuid.UUID, expectedRevision int) (bool, error) {
	tag, err := t.tx.Exec(ctx, `
		UPDATE intents SET status = 'released'
		WHERE project_id = $1 AND intent_id = $2 AND participant_id = $3
		  AND status = 'active' AND current_revision = $4`, projectID, intentID, participantID, expectedRevision)
	return err == nil && tag.RowsAffected() == 1, err
}

func intentView(revision protocol.IntentRevision, status string, owner uuid.UUID) protocol.IntentView {
	return protocol.IntentView{IntentRevision: revision, Status: status, OwnerHumanID: &owner, AdvisoryObservations: []string{application.IntentCharterObservation(revision)}}
}

var _ application.Tx = projectTx{}
