package application

import (
	"context"
	"errors"

	"uuid"

	"github.com/Juanqu261/conn/internal/analyzer"
	"github.com/Juanqu261/conn/internal/protocol"
)

// AttachStaticAnalysis stores source-free static-analysis claims against the
// caller's current active IntentRevision. Existing callers that do not hold an
// exact revision use this compatibility entry point.
func (p *Projects) AttachStaticAnalysis(ctx context.Context, actor, projectID, participantID, intentID uuid.UUID, report protocol.LocalAnalyzerReport) (Result[protocol.IntentView], error) {
	return p.AttachStaticAnalysisAtRevision(ctx, actor, projectID, participantID, intentID, 0, report)
}

// stores source-free static-analysis claims
// against the caller's exact active IntentRevision
func (p *Projects) AttachStaticAnalysisAtRevision(ctx context.Context, actor, projectID, participantID, intentID uuid.UUID, expectedRevision int, report protocol.LocalAnalyzerReport) (Result[protocol.IntentView], error) {
	if expectedRevision < 0 {
		return Result[protocol.IntentView]{}, &InvalidError{Message: "The expected revision cannot be negative."}
	}
	if err := validateRecord(report); err != nil {
		return Result[protocol.IntentView]{}, err
	}
	if report.Mode == protocol.AnalyzerModeDiagnostics {
		return Result[protocol.IntentView]{}, &InvalidError{Pointer: "/mode", Message: "Diagnostics-mode reports are for pair verification and are never attached to an Intent."}
	}
	if err := analyzer.ValidateReport(report); err != nil {
		return Result[protocol.IntentView]{}, &InvalidError{Message: "The analyzer report contains unsafe source, prompt, credential-shaped, or oversized content."}
	}
	now := p.now()
	var result Result[protocol.IntentView]
	err := p.Store.Write(ctx, func(tx Tx) error {
		participant, err := tx.ParticipantByID(ctx, projectID, participantID)
		if err != nil {
			return err
		}
		if participant.HumanID != actor {
			return ErrForbidden
		}
		if _, err := tx.Role(ctx, projectID, actor); err != nil {
			return err
		}
		current, err := tx.Intent(ctx, projectID, intentID)
		if err != nil {
			return err
		}
		if current.Status != "active" || (expectedRevision > 0 && current.IntentRevision.Revision != expectedRevision) {
			return ErrStaleRevision
		}
		revision := current.IntentRevision
		if revision.ParticipantID != participantID {
			return ErrForbidden
		}
		if revision.RepositoryID == nil {
			return &InvalidError{Pointer: "/repository", Message: "The Intent must reference a repository before local analysis can be attached."}
		}
		repository, err := tx.RepositoryConnection(ctx, projectID, *revision.RepositoryID)
		if err != nil {
			return err
		}
		if !repository.AnalysisEnabled {
			return &InvalidError{Pointer: "/repository", Message: "Local analysis is disabled for this repository connection."}
		}
		if report.Repository.CanonicalRemote != repository.CanonicalRemote {
			return &InvalidError{Pointer: "/repository/canonical_remote", Message: "The analyzer report remote does not match the Intent repository."}
		}
		attached, err := tx.AnalyzerReportAttached(ctx, projectID, intentID, revision.Revision)
		if err != nil {
			return err
		}
		if attached {
			result.Data = current
			result.AsOfEvent, err = tx.LastEvent(ctx, projectID)
			return err
		}
		claims := make([]protocol.SemanticClaim, 0, len(report.Claims))
		for _, candidate := range report.Claims {
			if candidate.ProvenanceKind != "static_analysis" {
				return &InvalidError{Pointer: "/claims", Message: "Analyzer claims must use static_analysis provenance."}
			}
			claim := protocol.SemanticClaim{ClaimID: uuid.NewV7(), ProjectID: projectID, IntentID: intentID, IntentRevision: revision.Revision,
				SubjectKind: candidate.SubjectKind, NormalizedSubject: candidate.NormalizedSubject, DisplayName: candidate.DisplayName,
				Relation: candidate.Relation, ProvenanceKind: "static_analysis", Confidence: candidate.Confidence,
				EvidenceRefs: candidate.EvidenceRefs, ProducerParticipantID: &participantID, ObservedAt: protocol.NewTimestamp(now)}
			if err := validateRecord(claim); err != nil {
				return err
			}
			claims = append(claims, claim)
		}
		if err := tx.InsertAnalyzerReport(ctx, projectID, intentID, revision.Revision, report, protocol.NewTimestamp(now)); err != nil {
			return err
		}
		if len(claims) > 0 {
			if err := tx.InsertSemanticClaims(ctx, claims); err != nil {
				return err
			}
			if err := p.appendClaimEvents(ctx, tx, actor, claims, protocol.NewTimestamp(now)); err != nil {
				return err
			}
			if err := p.assessActiveIntentCollisions(ctx, tx, actor, revision, protocol.NewTimestamp(now)); err != nil {
				return err
			}
		}
		result.Data = current
		result.AsOfEvent, err = tx.LastEvent(ctx, projectID)
		return err
	})
	if errors.Is(err, ErrNotFound) {
		return Result[protocol.IntentView]{}, err
	}
	return result, err
}
