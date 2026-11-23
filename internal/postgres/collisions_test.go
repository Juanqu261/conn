package postgres_test

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"uuid"

	"github.com/Juanqu261/conn/internal/application"
	"github.com/Juanqu261/conn/internal/postgres"
	"github.com/Juanqu261/conn/internal/protocol"
)

// TestDeclaredClaimAssessment exercises Task 7 against Docker PostgreSQL. It
// proves each supported rule is driven by normalized claims, not shared file
// paths, and that the stored explanation points back to immutable claim IDs.
func TestDeclaredClaimAssessment(t *testing.T) {
	tests := []struct {
		name     string
		left     protocol.SemanticClaimCandidate
		right    protocol.SemanticClaimCandidate
		wantRule string
	}{
		{"shared declared invariant", claim("invariant", "invariant:checkout:one-charge", "changes"), claim("invariant", "invariant:checkout:one-charge", "affects"), "shared_declared_invariant"},
		{"shared exported symbol", claim("code_symbol", "symbol:typescript:payment-service-charge", "affects"), claim("code_symbol", "symbol:typescript:payment-service-charge", "changes"), "shared_exported_symbol"},
		{"contract producer consumer", claim("contract", "openapi:POST:/payments", "changes"), claim("contract", "openapi:POST:/payments", "consumes"), "contract_producer_consumer"},
		{"schema resource overlap", claim("data_resource", "data:postgres:payment_attempts:idempotency_key", "changes"), claim("data_resource", "data:postgres:payment_attempts:idempotency_key", "removes"), "schema_resource_overlap"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ctx, store, projects, owner, projectID, objectiveID, leftParticipant, rightParticipant := assessmentFixture(t)
			leftID, rightID := uuid.NewV7(), uuid.NewV7()
			publishAssessmentIntent(t, ctx, projects, owner, projectID, objectiveID, leftParticipant, leftID, test.left)
			publishAssessmentIntent(t, ctx, projects, owner, projectID, objectiveID, rightParticipant, rightID, test.right)

			assessments := loadAssessments(t, ctx, store, leftID, rightID)
			if len(assessments) != 1 {
				t.Fatalf("assessments = %#v, want one", assessments)
			}
			got := assessments[0]
			if got.RuleID != test.wantRule || got.State != "open" || got.Severity != "amber" || got.Confidence != 1 {
				t.Fatalf("assessment = %#v", got)
			}
			if len(got.MatchingClaimRefs) != 2 || !strings.Contains(got.ExplanationMarkdown, "declared claims") || !strings.Contains(got.ExplanationMarkdown, test.wantRule) {
				t.Fatalf("matching explanation = %#v", got)
			}
			var claimRows int
			if err := store.Pool.QueryRow(ctx, `SELECT count(*) FROM semantic_claims WHERE project_id = $1`, got.ProjectID).Scan(&claimRows); err != nil || claimRows != 2 {
				t.Fatalf("persisted claims = %d, %v", claimRows, err)
			}
			var matches int
			if err := store.Pool.QueryRow(ctx, `SELECT count(*) FROM collision_claim_matches WHERE collision_id = $1`, got.CollisionID).Scan(&matches); err != nil || matches != 2 {
				t.Fatalf("claim matches = %d, %v", matches, err)
			}
			events, err := projects.Events(ctx, owner, got.ProjectID, 0, 50)
			if err != nil {
				t.Fatal(err)
			}
			found := false
			for _, event := range events.Data {
				found = found || event.EventType() == protocol.EventCollisionOpened
			}
			if !found {
				t.Fatalf("collision.opened missing from %#v", events.Data)
			}
		})
	}
}

func TestSameFileEvidenceDoesNotCreateCollision(t *testing.T) {
	ctx, store, projects, owner, projectID, objectiveID, leftParticipant, rightParticipant := assessmentFixture(t)
	path := "src/payments/service.ts"
	left := claim("invariant", "invariant:checkout:one-charge", "changes")
	left.EvidenceRefs = []protocol.ClaimEvidenceRef{{Kind: "source_location", Locator: "payment-charge", Path: path}}
	right := claim("invariant", "invariant:checkout:one-refund", "changes")
	right.EvidenceRefs = []protocol.ClaimEvidenceRef{{Kind: "source_location", Locator: "payment-refund", Path: path}}
	publishAssessmentIntent(t, ctx, projects, owner, projectID, objectiveID, leftParticipant, uuid.NewV7(), left)
	publishAssessmentIntent(t, ctx, projects, owner, projectID, objectiveID, rightParticipant, uuid.NewV7(), right)
	var count int
	if err := store.Pool.QueryRow(ctx, `SELECT count(*) FROM collision_assessments`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("same-file collision count = %d, %v", count, err)
	}
}

func TestCollisionStalesOnRevisionAndRelease(t *testing.T) {
	for _, action := range []string{"revision", "release"} {
		t.Run(action, func(t *testing.T) {
			ctx, store, projects, owner, projectID, objectiveID, leftParticipant, rightParticipant := assessmentFixture(t)
			leftID, rightID := uuid.NewV7(), uuid.NewV7()
			shared := claim("invariant", "invariant:checkout:one-charge", "changes")
			publishAssessmentIntent(t, ctx, projects, owner, projectID, objectiveID, leftParticipant, leftID, shared)
			publishAssessmentIntent(t, ctx, projects, owner, projectID, objectiveID, rightParticipant, rightID, shared)
			original := loadAssessments(t, ctx, store, leftID, rightID)[0]

			if action == "revision" {
				input := protocol.IntentInput{IntentID: leftID, ObjectiveID: objectiveID, ObjectiveRevision: 1, SummaryMarkdown: "Revise without the shared invariant.", DeclaredClaims: []protocol.SemanticClaimCandidate{claim("invariant", "invariant:checkout:one-refund", "changes")}}
				if _, err := projects.ReviseIntent(ctx, owner, original.ProjectID, leftParticipant, 1, input); err != nil {
					t.Fatal(err)
				}
			} else if _, err := projects.ReleaseIntent(ctx, owner, original.ProjectID, leftParticipant, leftID); err != nil {
				t.Fatal(err)
			}

			assessments := loadAssessments(t, ctx, store, leftID, rightID)
			if len(assessments) != 1 || assessments[0].State != "stale" || assessments[0].SupersededAt == nil {
				t.Fatalf("stale assessment = %#v", assessments)
			}
			events, err := projects.Events(ctx, owner, original.ProjectID, 0, 50)
			if err != nil {
				t.Fatal(err)
			}
			found := false
			for _, event := range events.Data {
				found = found || (event.EventType() == protocol.EventCollisionUpdated && event.EntityID == original.CollisionID)
			}
			if !found {
				t.Fatalf("collision.updated missing from %#v", events.Data)
			}
		})
	}
}

func assessmentFixture(t *testing.T) (context.Context, postgres.ProjectStore, *application.Projects, uuid.UUID, uuid.UUID, uuid.UUID, uuid.UUID, uuid.UUID) {
	t.Helper()
	ctx, store, projects, newHuman := newProjects(t)
	owner := newHuman(701, "collision-owner")
	projectID, objectiveID := uuid.NewV7(), uuid.NewV7()
	if _, err := projects.CreateProject(ctx, owner, protocol.ProjectCreate{ProjectID: projectID, Name: "Collision assessment"}); err != nil {
		t.Fatal(err)
	}
	if _, err := projects.CreateObjectiveRevision(ctx, owner, projectID, protocol.ObjectiveRevisionInput{
		ObjectiveID: objectiveID, Title: "Coordinate payment changes", OverviewMarkdown: "Keep payment behavior coordinated.", AccountableHumanID: owner,
	}, 0); err != nil {
		t.Fatal(err)
	}
	left, err := projects.ResolveParticipant(ctx, owner, projectID, "kiro", "collision-left")
	if err != nil {
		t.Fatal(err)
	}
	right, err := projects.ResolveParticipant(ctx, owner, projectID, "kiro", "collision-right")
	if err != nil {
		t.Fatal(err)
	}
	return ctx, store, projects, owner, projectID, objectiveID, left.Data.ParticipantID, right.Data.ParticipantID
}

func publishAssessmentIntent(t *testing.T, ctx context.Context, projects *application.Projects, owner, projectID, objectiveID, participantID, intentID uuid.UUID, semanticClaim protocol.SemanticClaimCandidate) {
	t.Helper()
	_, err := projects.PublishIntent(ctx, owner, projectID, participantID, protocol.IntentInput{
		IntentID: intentID, ObjectiveID: objectiveID, ObjectiveRevision: 1,
		SummaryMarkdown: "Coordinate a bounded declared semantic change.", DeclaredClaims: []protocol.SemanticClaimCandidate{semanticClaim},
	})
	if err != nil {
		t.Fatal(err)
	}
}

func loadAssessments(t *testing.T, ctx context.Context, store postgres.ProjectStore, leftID, rightID uuid.UUID) []protocol.CollisionAssessment {
	t.Helper()
	rows, err := store.Pool.Query(ctx, `
		SELECT document FROM collision_assessments
		WHERE (left_intent_id = $1 AND right_intent_id = $2)
		   OR (left_intent_id = $2 AND right_intent_id = $1)
		ORDER BY collision_id`, leftID, rightID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var assessments []protocol.CollisionAssessment
	for rows.Next() {
		var document []byte
		if err := rows.Scan(&document); err != nil {
			t.Fatal(err)
		}
		assessment, err := protocol.Decode[protocol.CollisionAssessment](document)
		if err != nil {
			t.Fatal(err)
		}
		assessments = append(assessments, assessment)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return assessments
}

func claim(kind, subject, relation string) protocol.SemanticClaimCandidate {
	return protocol.SemanticClaimCandidate{
		SubjectKind: kind, NormalizedSubject: subject, DisplayName: fmt.Sprintf("Declared %s", kind),
		Relation: relation, ProvenanceKind: "declared",
	}
}
