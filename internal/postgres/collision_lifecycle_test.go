package postgres_test

import (
	"context"
	"errors"
	"testing"

	"uuid"

	"github.com/Juanqu261/conn/internal/application"
	"github.com/Juanqu261/conn/internal/postgres"
	"github.com/Juanqu261/conn/internal/protocol"
)

// TestCollisionLifecycleFollowsHumanDecisions protects requirement 8: each
// decision kind sets its collision state and decision reference with its
// events in one transaction; a proposal changes nothing; a non-owner may
// dismiss only a collision on their own Intent; a later revision inherits a
// dismissed or resolved state only while its matched subjects stay covered,
// and a new matched subject reopens it.
func TestCollisionLifecycleFollowsHumanDecisions(t *testing.T) {
	tests := []struct {
		kind    string
		binding bool
		member  bool
		want    string
	}{
		{"false_positive", false, true, protocol.CollisionDismissed},
		{"dependency", false, false, protocol.CollisionResolved},
		{"sequencing", false, false, protocol.CollisionResolved},
		{"contract_agreement", true, false, protocol.CollisionResolved},
		{"accepted_risk", true, false, protocol.CollisionResolved},
		{"revise_objective", true, false, protocol.CollisionUnderReview},
		{"revise_blueprint", true, false, protocol.CollisionUnderReview},
		{"other", false, false, protocol.CollisionUnderReview},
	}
	for _, tc := range tests {
		t.Run(tc.kind, func(t *testing.T) {
			f := newLifecycleFixture(t)
			first, second := "invariant:checkout:one-charge", "invariant:checkout:one-refund"
			f.publish(t, f.owner, f.ownerParticipant, f.leftID, first, second)
			f.publish(t, f.member, f.memberParticipant, f.rightID, first)
			collision := f.only(t, 1)
			if collision.State != protocol.CollisionOpen || len(collision.MatchedSubjects) != 1 || collision.MatchedSubjects[0] != first {
				t.Fatalf("opened collision = %+v", collision)
			}

			// A non-binding proposal never changes the state.
			if _, err := f.projects.ProposeResolution(f.ctx, f.member, f.projectID, f.memberParticipant, collision.CollisionID, protocol.ResolutionProposalInput{Kind: tc.kind, RationaleMarkdown: "Proposed handling."}); err != nil {
				t.Fatal(err)
			}
			if state := f.collision(t, collision.CollisionID).State; state != protocol.CollisionOpen {
				t.Fatalf("state after proposal = %s, want open", state)
			}

			input := protocol.ResolutionDecisionInput{Kind: tc.kind, Binding: tc.binding, RationaleMarkdown: "Recorded by the accountable Human."}
			decider := f.owner
			if tc.member {
				// A member behind neither Intent cannot dismiss it; the
				// member behind one of them can.
				if _, err := f.projects.DecideResolution(f.ctx, f.outsider, f.projectID, collision.CollisionID, input); !errors.Is(err, application.ErrForbidden) {
					t.Fatalf("outsider dismissal = %v, want forbidden", err)
				}
				decider = f.member
			}
			decision, err := f.projects.DecideResolution(f.ctx, decider, f.projectID, collision.CollisionID, input)
			if err != nil {
				t.Fatal(err)
			}
			decided := f.collision(t, collision.CollisionID)
			if decided.State != tc.want || decided.DecisionRef == nil || *decided.DecisionRef != decision.Data.DecisionID {
				t.Fatalf("decided collision = %+v, want state %s and decision %s", decided, tc.want, decision.Data.DecisionID)
			}
			events, err := f.projects.Events(f.ctx, f.owner, f.projectID, decision.AsOfEvent-2, 10)
			if err != nil || len(events.Data) != 2 ||
				events.Data[0].EventType() != protocol.EventResolutionDecided || events.Data[0].EntityID != decision.Data.DecisionID ||
				events.Data[1].EventType() != protocol.EventCollisionUpdated || events.Data[1].EntityID != collision.CollisionID ||
				events.Data[1].EventSequence != decision.AsOfEvent {
				t.Fatalf("decision events = %+v, %v", events.Data, err)
			}

			// A revision with the same matched subjects carries a dismissed or
			// resolved decision forward; other states leave the new one open.
			f.revise(t, 1, first)
			carried := f.only(t, 2)
			inherits := tc.want == protocol.CollisionDismissed || tc.want == protocol.CollisionResolved
			if inherits {
				if carried.State != tc.want || carried.DecisionRef == nil || *carried.DecisionRef != decision.Data.DecisionID {
					t.Fatalf("carried collision = %+v, want inherited %s", carried, tc.want)
				}
				if f.eventFor(t, carried.CollisionID) != protocol.EventCollisionUpdated {
					t.Fatal("a carried-forward decision appended collision.opened")
				}
				if state := f.collision(t, collision.CollisionID).State; state != tc.want {
					t.Fatalf("decided history became %s, want %s", state, tc.want)
				}
				radar, err := f.projects.Radar(f.ctx, f.owner, f.projectID)
				if err != nil || len(radar.Data.OpenCollisions) != 0 || len(radar.Data.DecidedCollisions) != 1 || radar.Data.DecidedCollisions[0].CollisionID != carried.CollisionID {
					t.Fatalf("radar after carry-forward = open %v decided %v, %v", radar.Data.OpenCollisions, radar.Data.DecidedCollisions, err)
				}
			} else {
				if carried.State != protocol.CollisionOpen || carried.DecisionRef != nil || f.eventFor(t, carried.CollisionID) != protocol.EventCollisionOpened {
					t.Fatalf("collision after %s = %+v, want a newly opened assessment", tc.kind, carried)
				}
				if state := f.collision(t, collision.CollisionID).State; state != protocol.CollisionStale {
					t.Fatalf("under-review history = %s, want stale", state)
				}
			}

			// A new matched subject the decision did not cover reopens it.
			f.revise(t, 2, first, second)
			reopened := f.only(t, 3)
			if reopened.State != protocol.CollisionOpen || reopened.DecisionRef != nil || len(reopened.MatchedSubjects) != 2 || f.eventFor(t, reopened.CollisionID) != protocol.EventCollisionOpened {
				t.Fatalf("superset collision = %+v, want open with two matched subjects", reopened)
			}
			work, err := f.projects.WorkContext(f.ctx, f.memberHuman, f.projectID, application.WorkContextQuery{IntentID: f.rightID, Limit: 1, AdapterKind: "kiro"})
			if err != nil {
				t.Fatal(err)
			}
			if !containsCollision(work.Data.RelatedCollisions, reopened.CollisionID) {
				t.Fatalf("work context related collisions = %+v, want the reopened collision", work.Data.RelatedCollisions)
			}
		})
	}
}

type lifecycleFixture struct {
	ctx                                 context.Context
	store                               postgres.ProjectStore
	projects                            *application.Projects
	owner, member, outsider             uuid.UUID
	memberHuman                         protocol.Human
	projectID, objectiveID              uuid.UUID
	ownerParticipant, memberParticipant uuid.UUID
	leftID, rightID                     uuid.UUID
}

func newLifecycleFixture(t *testing.T) *lifecycleFixture {
	t.Helper()
	ctx, store, projects, newHuman := newProjects(t)
	f := &lifecycleFixture{ctx: ctx, store: store, projects: projects, projectID: uuid.NewV7(), objectiveID: uuid.NewV7(), leftID: uuid.NewV7(), rightID: uuid.NewV7()}
	f.owner, f.member, f.outsider = newHuman(801, "lifecycle-owner"), newHuman(802, "lifecycle-member"), newHuman(803, "lifecycle-outsider")
	f.memberHuman = protocol.Human{HumanID: f.member, GitHubUserID: 802, GitHubLogin: "lifecycle-member"}
	if _, err := projects.CreateProject(ctx, f.owner, protocol.ProjectCreate{ProjectID: f.projectID, Name: "Collision lifecycle"}); err != nil {
		t.Fatal(err)
	}
	for _, human := range []uuid.UUID{f.member, f.outsider} {
		if _, err := projects.AddMember(ctx, f.owner, f.projectID, human); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := projects.CreateObjectiveRevision(ctx, f.owner, f.projectID, protocol.ObjectiveRevisionInput{
		ObjectiveID: f.objectiveID, Title: "Coordinate payment changes", OverviewMarkdown: "Keep payment behavior coordinated.", AccountableHumanID: f.owner,
	}, 0); err != nil {
		t.Fatal(err)
	}
	ownerParticipant, err := projects.ResolveParticipant(ctx, f.owner, f.projectID, "kiro", "")
	if err != nil {
		t.Fatal(err)
	}
	memberParticipant, err := projects.ResolveParticipant(ctx, f.member, f.projectID, "kiro", "")
	if err != nil {
		t.Fatal(err)
	}
	f.ownerParticipant, f.memberParticipant = ownerParticipant.Data.ParticipantID, memberParticipant.Data.ParticipantID
	return f
}

func (f *lifecycleFixture) publish(t *testing.T, human, participant, intentID uuid.UUID, subjects ...string) {
	t.Helper()
	if _, err := f.projects.PublishIntent(f.ctx, human, f.projectID, participant, f.input(intentID, subjects...)); err != nil {
		t.Fatal(err)
	}
}

func (f *lifecycleFixture) revise(t *testing.T, expected int, subjects ...string) {
	t.Helper()
	if _, err := f.projects.ReviseIntent(f.ctx, f.member, f.projectID, f.memberParticipant, expected, f.input(f.rightID, subjects...)); err != nil {
		t.Fatal(err)
	}
}

func (f *lifecycleFixture) input(intentID uuid.UUID, subjects ...string) protocol.IntentInput {
	claims := make([]protocol.SemanticClaimCandidate, 0, len(subjects))
	for _, subject := range subjects {
		claims = append(claims, claim("invariant", subject, "changes"))
	}
	return protocol.IntentInput{IntentID: intentID, ObjectiveID: f.objectiveID, ObjectiveRevision: 1, SummaryMarkdown: "Change payment invariants.", DeclaredClaims: claims}
}

// only returns the single assessment for the right Intent's revision.
func (f *lifecycleFixture) only(t *testing.T, rightRevision int) protocol.CollisionAssessment {
	t.Helper()
	var found []protocol.CollisionAssessment
	for _, assessment := range loadAssessments(t, f.ctx, f.store, f.leftID, f.rightID) {
		if (assessment.RightIntentID == f.rightID && assessment.RightIntentRevision == rightRevision) ||
			(assessment.LeftIntentID == f.rightID && assessment.LeftIntentRevision == rightRevision) {
			found = append(found, assessment)
		}
	}
	if len(found) != 1 {
		t.Fatalf("assessments for revision %d = %+v, want one", rightRevision, found)
	}
	return found[0]
}

func (f *lifecycleFixture) collision(t *testing.T, collisionID uuid.UUID) protocol.CollisionAssessment {
	t.Helper()
	for _, assessment := range loadAssessments(t, f.ctx, f.store, f.leftID, f.rightID) {
		if assessment.CollisionID == collisionID {
			return assessment
		}
	}
	t.Fatalf("collision %s is missing", collisionID)
	return protocol.CollisionAssessment{}
}

// eventFor returns the lifecycle event type first recorded for a collision.
func (f *lifecycleFixture) eventFor(t *testing.T, collisionID uuid.UUID) protocol.EventType {
	t.Helper()
	events, err := f.projects.Events(f.ctx, f.owner, f.projectID, 0, 500)
	if err != nil {
		t.Fatal(err)
	}
	for _, event := range events.Data {
		if event.EntityID == collisionID {
			return event.EventType()
		}
	}
	t.Fatalf("no event for collision %s", collisionID)
	return ""
}

func containsCollision(collisions []protocol.CollisionAssessment, collisionID uuid.UUID) bool {
	for _, collision := range collisions {
		if collision.CollisionID == collisionID {
			return true
		}
	}
	return false
}
