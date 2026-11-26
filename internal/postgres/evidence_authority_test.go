package postgres_test

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"uuid"

	"github.com/Juanqu261/conn/internal/application"
	"github.com/Juanqu261/conn/internal/protocol"
)

// TestEvidenceFromTheOwningHuman protects requirements 4 and 5: any
// Participant of the Human who owns an Intent may attach evidence and is
// recorded as producer, another Human may not, an observation is stored with
// its evidence, an observed state must match its recorded outcome, the
// Intent evidence list is newest first, Radar coverage reports provenance,
// commit, and staleness, and work context names the commit trailer.
func TestEvidenceFromTheOwningHuman(t *testing.T) {
	ctx, _, projects, newHuman := newProjects(t)
	owner, other := newHuman(901, "evidence-owner"), newHuman(902, "evidence-other")
	projectID, objectiveID, intentID := uuid.NewV7(), uuid.NewV7(), uuid.NewV7()
	if _, err := projects.CreateProject(ctx, owner, protocol.ProjectCreate{ProjectID: projectID, Name: "Evidence authority"}); err != nil {
		t.Fatal(err)
	}
	if _, err := projects.AddMember(ctx, owner, projectID, other); err != nil {
		t.Fatal(err)
	}
	if _, err := projects.CreateObjectiveRevision(ctx, owner, projectID, protocol.ObjectiveRevisionInput{
		ObjectiveID: objectiveID, Title: "Verified retries", OverviewMarkdown: "Retries are verified.", AccountableHumanID: owner,
		KeyPoints: []protocol.KeyPoint{{Key: "retry-tests", Kind: protocol.KeyPointAcceptanceCriterion, Statement: "Retry tests pass."}},
	}, 0); err != nil {
		t.Fatal(err)
	}
	participant := func(human uuid.UUID, adapter string) uuid.UUID {
		t.Helper()
		result, err := projects.ResolveParticipant(ctx, human, projectID, adapter, "")
		if err != nil {
			t.Fatal(err)
		}
		return result.Data.ParticipantID
	}
	kiro, cli, otherCLI := participant(owner, "kiro"), participant(owner, "cli"), participant(other, "cli")
	head := "4f1c2e9a7b3d4f1c2e9a7b3d4f1c2e9a7b3d4f1c"
	if _, err := projects.PublishIntent(ctx, owner, projectID, kiro, protocol.IntentInput{
		IntentID: intentID, ObjectiveID: objectiveID, ObjectiveRevision: 1, SummaryMarkdown: "Verify retries.", HeadRevisionRef: head,
	}); err != nil {
		t.Fatal(err)
	}

	observation := &protocol.EvidenceObservation{
		Method: protocol.ObservationMethodCheck, Runner: protocol.ObservationRunnerLocal, CheckKey: "retry-tests",
		ConfigSHA256: strings.Repeat("a", 64), CommandSHA256: strings.Repeat("b", 64), Commit: "0123456789ab0123456789ab0123456789ab0123",
		ExitCode: 0, DurationMS: 1200, OutputSHA256: strings.Repeat("c", 64), StartedAt: protocol.NewTimestamp(time.Now()),
	}
	reported := protocol.EvidenceAttachmentInput{
		IntentID: intentID, ObjectiveID: objectiveID, ObjectiveRevision: 1, AcceptanceKey: "retry-tests",
		Kind: "test_case", State: "passing", Locator: "ci:retry-test",
	}
	observed := reported
	observed.Kind, observed.Locator, observed.Observation = protocol.EvidenceKindCheckRun, "conn-check:retry-tests@0123456789ab", observation

	tests := []struct {
		name        string
		human       uuid.UUID
		participant uuid.UUID
		input       protocol.EvidenceAttachmentInput
		wantErr     error
	}{
		{"owning participant reports", owner, kiro, reported, nil},
		{"same Human's other participant observes", owner, cli, observed, nil},
		{"another Human is rejected", other, otherCLI, reported, application.ErrForbidden},
		{"observed state must match its outcome", owner, cli, func() protocol.EvidenceAttachmentInput {
			input := observed
			input.State = "failing"
			return input
		}(), application.ErrInvalid},
	}
	var attached []protocol.EvidenceReference
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result, err := projects.AttachEvidence(ctx, tc.human, projectID, tc.participant, tc.input)
			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("attach = %v, want %v", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if result.Data.ProducerParticipantID == nil || *result.Data.ProducerParticipantID != tc.participant {
				t.Fatalf("producer = %v, want the calling participant %s", result.Data.ProducerParticipantID, tc.participant)
			}
			attached = append(attached, result.Data)
		})
	}

	list, err := projects.IntentEvidence(ctx, other, projectID, intentID)
	if err != nil {
		t.Fatal(err)
	}
	if len(list.Data.Evidence) != 2 || list.Data.Evidence[0].EvidenceID != attached[1].EvidenceID || list.Data.Evidence[1].EvidenceID != attached[0].EvidenceID {
		t.Fatalf("evidence list = %+v, want newest first", list.Data.Evidence)
	}
	stored := list.Data.Evidence[0]
	if stored.Observation == nil || !sameJSON(t, stored.Observation, observation) || stored.Provenance() != protocol.ProvenanceObservedRun || list.Data.Evidence[1].Provenance() != protocol.ProvenanceReported {
		t.Fatalf("stored observation = %+v", stored)
	}

	radar, err := projects.Radar(ctx, owner, projectID)
	if err != nil {
		t.Fatal(err)
	}
	coverage := radar.Data.EvidenceCoverage
	if len(coverage) != 1 || coverage[0].State != "passing" || coverage[0].Provenance != protocol.ProvenanceObservedRun ||
		coverage[0].Commit != observation.Commit || !coverage[0].Stale {
		t.Fatalf("radar coverage = %+v, want observed, stale against the recorded head", coverage)
	}

	ownerHuman := protocol.Human{HumanID: owner, GitHubUserID: 901, GitHubLogin: "evidence-owner"}
	work, err := projects.WorkContext(ctx, ownerHuman, projectID, application.WorkContextQuery{IntentID: intentID, Limit: 1, AdapterKind: "cli"})
	if err != nil {
		t.Fatal(err)
	}
	if work.Data.Focus == nil || work.Data.Focus.CommitTrailer != protocol.CommitTrailer(intentID, 1) || work.Data.Focus.Analysis != nil {
		t.Fatalf("CLI work context for the Kiro-owned Intent = %+v", work.Data.Focus)
	}
	otherHuman := protocol.Human{HumanID: other, GitHubUserID: 902, GitHubLogin: "evidence-other"}
	if _, err := projects.WorkContext(ctx, otherHuman, projectID, application.WorkContextQuery{IntentID: intentID, Limit: 1}); !errors.Is(err, application.ErrNotFound) {
		t.Fatalf("another Human's work context = %v, want not found", err)
	}
}

func sameJSON(t *testing.T, a, b any) bool {
	t.Helper()
	left, err := json.Marshal(a)
	if err != nil {
		t.Fatal(err)
	}
	right, err := json.Marshal(b)
	if err != nil {
		t.Fatal(err)
	}
	return string(left) == string(right)
}
