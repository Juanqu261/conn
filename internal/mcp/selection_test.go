package mcp

import (
	"strings"
	"testing"
	"uuid"

	"github.com/Juanqu261/conn/internal/protocol"
)

// TestReadMyCharterFindsTheCallersBlueprint protects read_my_charter without
// arguments once a project has several Objectives, each with its own
// blueprint: it selects the one blueprint holding the caller's charter.
func TestReadMyCharterFindsTheCallersBlueprint(t *testing.T) {
	me, other := uuid.NewV7(), uuid.NewV7()
	blueprint := func(revisions ...protocol.RevisionSummary) protocol.BlueprintSummary {
		return protocol.BlueprintSummary{BlueprintID: uuid.NewV7(), CurrentRevision: revisions[len(revisions)-1].Revision, Revisions: revisions}
	}
	revision := func(n int, status protocol.BlueprintStatus, humans ...uuid.UUID) protocol.RevisionSummary {
		r := protocol.RevisionSummary{Revision: n, Status: status, ObjectiveID: uuid.NewV7()}
		for _, h := range humans {
			r.Cells = append(r.Cells, protocol.CellSummary{CellID: uuid.NewV7(), AccountableHumanID: h})
		}
		return r
	}
	theirs := blueprint(revision(1, protocol.BlueprintActive, other))
	mine := blueprint(revision(1, protocol.BlueprintActive, other), revision(2, protocol.BlueprintProposed, other, me))

	got, rev, err := pickMyInForce(protocol.ProjectStatus{Blueprints: []protocol.BlueprintSummary{theirs, mine}}, blueprintInput{}, me)
	if err != nil || got.BlueprintID != mine.BlueprintID || rev.Revision != 2 {
		t.Fatalf("got blueprint %s revision %d, %v; want revision 2 of %s, the newest that includes the caller", got.BlueprintID, rev.Revision, err, mine.BlueprintID)
	}

	_, _, err = pickMyInForce(protocol.ProjectStatus{Blueprints: []protocol.BlueprintSummary{theirs, theirs}}, blueprintInput{}, me)
	if err == nil || !strings.Contains(err.Error(), "no charter") {
		t.Fatalf("error = %v, want that the caller has no charter", err)
	}

	also := blueprint(revision(1, protocol.BlueprintActive, me))
	_, _, err = pickMyInForce(protocol.ProjectStatus{Blueprints: []protocol.BlueprintSummary{theirs, mine, also}}, blueprintInput{}, me)
	if err == nil || !strings.Contains(err.Error(), mine.BlueprintID.String()) || !strings.Contains(err.Error(), also.BlueprintID.String()) ||
		strings.Contains(err.Error(), theirs.BlueprintID.String()) {
		t.Fatalf("error = %v, want a choice between only the caller's two blueprints", err)
	}
}
