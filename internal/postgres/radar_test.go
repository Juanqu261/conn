package postgres_test

import (
	"errors"
	"strings"
	"testing"

	"uuid"

	"github.com/Juanqu261/conn/internal/application"
	"github.com/Juanqu261/conn/internal/protocol"
)

// TestRadarShowsAbsentWorkAndEvidence proves the read model does not turn an
// empty Intent ledger or missing acceptance evidence into project completion.
func TestRadarShowsAbsentWorkAndEvidence(t *testing.T) {
	ctx, _, projects, newHuman := newProjects(t)
	owner, outsider := newHuman(701, "radar-owner"), newHuman(702, "radar-outsider")
	projectID, objectiveID := uuid.NewV7(), uuid.NewV7()
	if _, err := projects.CreateProject(ctx, owner, protocol.ProjectCreate{ProjectID: projectID, Name: "Radar absence"}); err != nil {
		t.Fatal(err)
	}
	if _, err := projects.CreateObjectiveRevision(ctx, owner, projectID, protocol.ObjectiveRevisionInput{
		ObjectiveID: objectiveID, Title: "Prove checkout", OverviewMarkdown: "Keep checkout available.", AccountableHumanID: owner,
		KeyPoints: []protocol.KeyPoint{{Key: "checkout-works", Kind: protocol.KeyPointAcceptanceCriterion, Statement: "Checkout completes."}},
	}, 0); err != nil {
		t.Fatal(err)
	}

	radar, err := projects.Radar(ctx, owner, projectID)
	if err != nil {
		t.Fatal(err)
	}
	if radar.AsOfEvent != 2 || radar.Data.AsOfEvent != 2 || len(radar.Data.ActiveIntents) != 0 || len(radar.Data.SemanticClaims) != 0 {
		t.Fatalf("radar cursor/work = %#v, want event 2 with no active work or claims", radar)
	}
	if len(radar.Data.EvidenceCoverage) != 1 || radar.Data.EvidenceCoverage[0].State != "missing" || len(radar.Data.EvidenceCoverage[0].EvidenceRefs) != 0 {
		t.Fatalf("evidence coverage = %#v, want explicit missing evidence", radar.Data.EvidenceCoverage)
	}
	unknowns := strings.Join(radar.Data.KnownUnknowns, "\n")
	if !strings.Contains(unknowns, "absence of reported work, not project completion") || !strings.Contains(unknowns, "No active Blueprint") {
		t.Fatalf("known unknowns = %q, want explicit absence and unratified boundary", unknowns)
	}
	if _, err := projects.Radar(ctx, outsider, projectID); !errors.Is(err, application.ErrNotFound) {
		t.Fatalf("outsider radar error = %v, want not found", err)
	}
}
