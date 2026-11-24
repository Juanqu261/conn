package postgres_test

import (
	"errors"
	"testing"

	"uuid"

	"github.com/Juanqu261/conn/internal/application"
	"github.com/Juanqu261/conn/internal/protocol"
)

func TestKiroSpecImportIsImmutableAndEvidenceCoversAcceptance(t *testing.T) {
	ctx, _, projects, newHuman := newProjects(t)
	owner, member := newHuman(801, "import-owner"), newHuman(802, "import-member")
	projectID, objectiveID, intentID := uuid.NewV7(), uuid.NewV7(), uuid.NewV7()
	if _, err := projects.CreateProject(ctx, owner, protocol.ProjectCreate{ProjectID: projectID, Name: "Imported objective"}); err != nil {
		t.Fatal(err)
	}
	if _, err := projects.AddMember(ctx, owner, projectID, member); err != nil {
		t.Fatal(err)
	}
	imported := protocol.ObjectiveImportInput{
		ObjectiveID: objectiveID, SourceKind: "kiro_spec", SourceRef: ".kiro/specs/checkout/requirements.md",
		ContentHash: "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		Title:       "Safe checkout", OverviewMarkdown: "Keep retries safe.", AccountableHumanID: owner,
		KeyPoints: []protocol.KeyPoint{{Key: "retry-works", Kind: protocol.KeyPointAcceptanceCriterion, Statement: "A retry completes without double charging."}},
	}
	if _, err := projects.ImportKiroSpecObjective(ctx, member, projectID, imported, 0); !errors.Is(err, application.ErrForbidden) {
		t.Fatalf("member import error = %v, want forbidden", err)
	}
	first, err := projects.ImportKiroSpecObjective(ctx, owner, projectID, imported, 0)
	if err != nil || first.AsOfEvent != 3 || first.Data.ImportProvenance == nil || first.Data.ImportProvenance.ContentHash != imported.ContentHash {
		t.Fatalf("initial import = %#v, %v", first, err)
	}
	// Retrying identical human authorization adds no revision or event.
	retried, err := projects.ImportKiroSpecObjective(ctx, owner, projectID, imported, 0)
	if err != nil || retried.AsOfEvent != 3 || retried.Data.Revision != 1 {
		t.Fatalf("retried import = %#v, %v", retried, err)
	}
	// A changed local source has no path into Conn until this explicit refresh.
	stored, err := projects.ObjectiveRevision(ctx, owner, projectID, objectiveID, 1)
	if err != nil || stored.Data.ImportProvenance.ContentHash != imported.ContentHash {
		t.Fatalf("stored first import = %#v, %v", stored, err)
	}
	refreshed := imported
	refreshed.ContentHash = "abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789"
	refreshed.OverviewMarkdown = "Keep retries safe after a reviewed refresh."
	second, err := projects.ImportKiroSpecObjective(ctx, owner, projectID, refreshed, 1)
	if err != nil || second.AsOfEvent != 4 || second.Data.Revision != 2 || second.Data.ImportProvenance.ContentHash != refreshed.ContentHash {
		t.Fatalf("refreshed import = %#v, %v", second, err)
	}
	stored, err = projects.ObjectiveRevision(ctx, owner, projectID, objectiveID, 1)
	if err != nil || stored.Data.ImportProvenance.ContentHash != imported.ContentHash {
		t.Fatalf("refresh mutated revision one: %#v, %v", stored, err)
	}

	participant, err := projects.ResolveParticipant(ctx, member, projectID, "kiro", "import-evidence")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := projects.PublishIntent(ctx, member, projectID, participant.Data.ParticipantID, protocol.IntentInput{
		IntentID: intentID, ObjectiveID: objectiveID, ObjectiveRevision: 2, SummaryMarkdown: "Verify retries.",
	}); err != nil {
		t.Fatal(err)
	}
	invalidEvidence := protocol.EvidenceAttachmentInput{
		IntentID: intentID, ObjectiveID: objectiveID, ObjectiveRevision: 2, AcceptanceKey: "unknown",
		Kind: "test_case", State: "passing", Locator: "test:checkout-retry",
	}
	if _, err := projects.AttachEvidence(ctx, member, projectID, participant.Data.ParticipantID, invalidEvidence); !errors.Is(err, application.ErrInvalid) {
		t.Fatalf("unknown acceptance key error = %v, want invalid", err)
	}
	invalidEvidence.AcceptanceKey = "retry-works"
	evidence, err := projects.AttachEvidence(ctx, member, projectID, participant.Data.ParticipantID, invalidEvidence)
	if err != nil || evidence.AsOfEvent != 6 || evidence.Data.ProducerParticipantID == nil || *evidence.Data.ProducerParticipantID != participant.Data.ParticipantID {
		t.Fatalf("evidence = %#v, %v", evidence, err)
	}
	radar, err := projects.Radar(ctx, owner, projectID)
	if err != nil {
		t.Fatal(err)
	}
	if radar.AsOfEvent != 6 || len(radar.Data.EvidenceCoverage) != 1 || radar.Data.EvidenceCoverage[0].State != "passing" || len(radar.Data.EvidenceCoverage[0].EvidenceRefs) != 1 || radar.Data.EvidenceCoverage[0].EvidenceRefs[0] != evidence.Data.EvidenceID {
		t.Fatalf("evidence coverage = %#v", radar.Data.EvidenceCoverage)
	}
}
