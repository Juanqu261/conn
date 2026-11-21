package postgres_test

import (
	"errors"
	"slices"
	"testing"
	"uuid"

	"github.com/Juanqu261/conn/internal/application"
	"github.com/Juanqu261/conn/internal/protocol"
)

// TestCharterRatification is the three-human authorization and ratification
// scenario (requirements 4 and 5, decisions 0024 and 0025):
//   - proposals are validated against the Objective revision, project
//     membership, and their own cells;
//   - only the accountable human decides a charter, whatever their role;
//   - two of three acceptances leave a revision proposed, and the third
//     activates it in the same transaction;
//   - acceptance of one revision does not carry over to the next;
//   - a rejection is final and keeps the revision inactive;
//   - an active revision stays in force until a successor is ratified, and
//     a newer proposal supersedes a pending one.
func TestCharterRatification(t *testing.T) {
	ctx, _, projects, newHuman := newProjects(t)
	alice, bob, carol, dave := newHuman(1, "alice"), newHuman(2, "bob"), newHuman(3, "carol"), newHuman(4, "dave")
	projectID, objectiveID, blueprintID := uuid.NewV7(), uuid.NewV7(), uuid.NewV7()
	cellA, cellB, cellC := uuid.NewV7(), uuid.NewV7(), uuid.NewV7()

	// Events 1 to 4: Alice creates the project, adds Bob and Carol, and
	// creates the Objective. Dave is authenticated but not a member.
	must(t, func() error {
		_, err := projects.CreateProject(ctx, alice, protocol.ProjectCreate{ProjectID: projectID, Name: "Checkout reliability"})
		return err
	})
	for _, human := range []uuid.UUID{bob, carol} {
		must(t, func() error { _, err := projects.AddMember(ctx, alice, projectID, human); return err })
	}
	must(t, func() error {
		_, err := projects.CreateObjectiveRevision(ctx, alice, projectID, protocol.ObjectiveRevisionInput{
			ObjectiveID:        objectiveID,
			Title:              "Reliable checkout",
			OverviewMarkdown:   "Checkout must survive a payment-provider outage.",
			AccountableHumanID: alice,
			KeyPoints: []protocol.KeyPoint{
				{Key: "no-double-charge", Kind: protocol.KeyPointInvariant, Statement: "A customer is never charged twice."},
			},
		}, 0)
		return err
	})

	// proposal returns a three-cell partition; carolOverview distinguishes
	// revisions whose only change is Carol's charter.
	proposal := func(carolOverview string) protocol.BlueprintProposal {
		return protocol.BlueprintProposal{
			BlueprintID:       blueprintID,
			ObjectiveID:       objectiveID,
			ObjectiveRevision: 1,
			CellCharters: []protocol.CellCharter{
				{CellID: cellA, AccountableHumanID: alice, Title: "Payment gateway", OverviewMarkdown: "Own provider failover.",
					BoundaryPoints: []protocol.BoundaryPoint{
						{Key: "failover", Kind: protocol.BoundaryResponsibility, Statement: "Switch providers on outage."},
					}},
				{CellID: cellB, AccountableHumanID: bob, Title: "Order service", OverviewMarkdown: "Own order state.",
					Dependencies: []protocol.CellDependency{{CellID: cellA, Summary: "Charges through the gateway."}}},
				{CellID: cellC, AccountableHumanID: carol, Title: "Ledger", OverviewMarkdown: carolOverview,
					ObjectivePointRefs: []string{"no-double-charge"}},
			},
		}
	}
	revision1 := proposal("Own idempotent charge records.")

	// Each invalid proposal must be rejected at the named input location.
	invalid := []struct {
		name    string
		edit    func(*protocol.BlueprintProposal)
		pointer string
	}{
		{"a duplicate cell", func(p *protocol.BlueprintProposal) {
			p.CellCharters[1].CellID = cellA
			p.CellCharters[1].Dependencies = nil
		}, "/cell_charters/1/cell_id"},
		{"a duplicate boundary key", func(p *protocol.BlueprintProposal) {
			p.CellCharters[0].BoundaryPoints = append(p.CellCharters[0].BoundaryPoints, p.CellCharters[0].BoundaryPoints[0])
		}, "/cell_charters/0/boundary_points/1/key"},
		{"a dependency outside the revision", func(p *protocol.BlueprintProposal) {
			p.CellCharters[1].Dependencies[0].CellID = uuid.NewV7()
		}, "/cell_charters/1/dependencies/0/cell_id"},
		{"a dependency on its own cell", func(p *protocol.BlueprintProposal) {
			p.CellCharters[1].Dependencies[0].CellID = cellB
		}, "/cell_charters/1/dependencies/0/cell_id"},
		{"a missing Objective revision", func(p *protocol.BlueprintProposal) {
			p.ObjectiveRevision = 2
		}, "/objective_revision"},
		{"an unknown Objective point", func(p *protocol.BlueprintProposal) {
			p.CellCharters[2].ObjectivePointRefs = []string{"fast-checkout"}
		}, "/cell_charters/2/objective_point_refs/0"},
		{"a non-member accountable human", func(p *protocol.BlueprintProposal) {
			p.CellCharters[2].AccountableHumanID = dave
		}, "/cell_charters/2/accountable_human_id"},
	}
	for _, tc := range invalid {
		p := proposal("Own idempotent charge records.")
		tc.edit(&p)
		_, err := projects.ProposeBlueprintRevision(ctx, alice, projectID, p, 0)
		var invalidErr *application.InvalidError
		if !errors.As(err, &invalidErr) || invalidErr.Pointer != tc.pointer {
			t.Fatalf("%s: got %v, want invalid input at %s", tc.name, err, tc.pointer)
		}
	}

	propose := func(actor uuid.UUID, p protocol.BlueprintProposal, expected int) func() (int64, error) {
		return func() (int64, error) {
			r, err := projects.ProposeBlueprintRevision(ctx, actor, projectID, p, expected)
			return r.AsOfEvent, err
		}
	}
	accept := func(actor uuid.UUID, revision int, cell uuid.UUID) func() (int64, error) {
		return func() (int64, error) {
			r, err := projects.AcceptCharter(ctx, actor, projectID, blueprintID, revision, cell)
			return r.AsOfEvent, err
		}
	}
	reject := func(actor uuid.UUID, revision int, cell uuid.UUID, rationale string) func() (int64, error) {
		return func() (int64, error) {
			r, err := projects.RejectCharter(ctx, actor, projectID, blueprintID, revision, cell, rationale)
			return r.AsOfEvent, err
		}
	}
	const (
		proposed   = protocol.BlueprintProposed
		active     = protocol.BlueprintActive
		rejected   = protocol.BlueprintRejected
		superseded = protocol.BlueprintSuperseded
	)

	// Steps run in order. Each names the event cursor after a success, or
	// the error, and the status of every revision afterwards.
	steps := []struct {
		name      string
		run       func() (int64, error)
		wantEvent int64
		wantErr   error
		statuses  []protocol.BlueprintStatus // indexed by revision - 1
	}{
		{"a member cannot propose", propose(bob, revision1, 0), 0, application.ErrForbidden, nil},
		{"the owner proposes revision 1", propose(alice, revision1, 0), 5, nil, []protocol.BlueprintStatus{proposed}},
		{"retrying the proposal appends no event", propose(alice, revision1, 0), 5, nil, []protocol.BlueprintStatus{proposed}},
		{"a non-member learns nothing", accept(dave, 1, cellC), 0, application.ErrNotFound, nil},
		{"the owner cannot accept another human's charter", accept(alice, 1, cellB), 0, application.ErrForbidden, nil},
		{"Alice accepts her charter", accept(alice, 1, cellA), 6, nil, []protocol.BlueprintStatus{proposed}},
		{"two of three acceptances leave it proposed", accept(bob, 1, cellB), 7, nil, []protocol.BlueprintStatus{proposed}},
		{"accepting again is idempotent", accept(bob, 1, cellB), 7, nil, []protocol.BlueprintStatus{proposed}},
		{"the final acceptance activates it", accept(carol, 1, cellC), 9, nil, []protocol.BlueprintStatus{active}},
		{"an acceptance cannot become a rejection", reject(bob, 1, cellB, "Changed my mind."), 0, application.ErrCharterDecided, nil},

		{"revision 2 is reviewed while revision 1 stays active", propose(alice, proposal("Own the ledger and refunds."), 1), 10, nil,
			[]protocol.BlueprintStatus{active, proposed}},
		{"Alice accepts revision 2", accept(alice, 2, cellA), 11, nil, nil},
		{"earlier acceptances do not carry over", accept(bob, 2, cellB), 12, nil, []protocol.BlueprintStatus{active, proposed}},
		{"a rejection requires a rationale", reject(carol, 2, cellC, " "), 0, application.ErrInvalid, nil},
		{"Carol rejects revision 2", reject(carol, 2, cellC, "Refunds belong to the order service."), 14, nil,
			[]protocol.BlueprintStatus{active, rejected}},
		{"a rejection cannot be overwritten", accept(carol, 2, cellC), 0, application.ErrCharterDecided, nil},

		{"revision 3 is proposed", propose(alice, proposal("Own the ledger; refunds stay with orders."), 2), 15, nil,
			[]protocol.BlueprintStatus{active, rejected, proposed}},
		{"revision 4 supersedes the pending revision 3", propose(alice, proposal("Own the ledger and reconciliation."), 3), 16, nil,
			[]protocol.BlueprintStatus{active, rejected, superseded, proposed}},
		{"a proposal from revision 2 is stale", propose(alice, proposal("A concurrent edit."), 2), 0, application.ErrStaleRevision, nil},
		{"a superseded revision takes no decisions", accept(alice, 3, cellA), 0, application.ErrStaleRevision, nil},
		{"Alice accepts revision 4", accept(alice, 4, cellA), 17, nil, nil},
		{"Bob accepts revision 4", accept(bob, 4, cellB), 18, nil, nil},
		{"activating revision 4 supersedes revision 1", accept(carol, 4, cellC), 20, nil,
			[]protocol.BlueprintStatus{superseded, rejected, superseded, active}},
	}
	for _, step := range steps {
		asOf, err := step.run()
		if !errors.Is(err, step.wantErr) {
			t.Fatalf("%s: error %v, want %v", step.name, err, step.wantErr)
		}
		if asOf != step.wantEvent {
			t.Fatalf("%s: as of event %d, want %d", step.name, asOf, step.wantEvent)
		}
		for i, want := range step.statuses {
			view, err := projects.BlueprintRevision(ctx, bob, projectID, blueprintID, i+1)
			if err != nil {
				t.Fatalf("%s: read revision %d: %v", step.name, i+1, err)
			}
			if view.Data.Status != want {
				t.Fatalf("%s: revision %d is %s, want %s", step.name, i+1, view.Data.Status, want)
			}
		}
	}

	// Every member reads the same complete snapshot that was proposed.
	for _, human := range []uuid.UUID{alice, bob, carol} {
		view, err := projects.BlueprintRevision(ctx, human, projectID, blueprintID, 1)
		if err != nil {
			t.Fatal(err)
		}
		if len(view.Data.BlueprintRevision.CellCharters) != 3 ||
			view.Data.BlueprintRevision.CellCharters[2].OverviewMarkdown != revision1.CellCharters[2].OverviewMarkdown ||
			!slices.Equal(view.Data.BlueprintRevision.CellCharters[1].Dependencies, revision1.CellCharters[1].Dependencies) {
			t.Fatalf("revision 1 snapshot changed: %+v", view.Data.BlueprintRevision.CellCharters)
		}
	}

	// The final acceptance and the activation are consecutive events.
	page, err := projects.Events(ctx, carol, projectID, 7, 2)
	if err != nil {
		t.Fatal(err)
	}
	var types []protocol.EventType
	for _, event := range page.Data {
		types = append(types, event.EventType())
	}
	if want := []protocol.EventType{protocol.EventCharterAccepted, protocol.EventBlueprintRevisionActivated}; !slices.Equal(types, want) {
		t.Fatalf("events 8 and 9: %v, want %v", types, want)
	}
}

// TestOneBlueprintPerObjective protects decision 0005: an Objective has
// one Blueprint, so at most one partition of it is ever active.
//   - a second Blueprint for the same Objective is refused and names the
//     Blueprint to revise instead;
//   - a Blueprint's revisions keep dividing its first Objective;
//   - retrying a stored proposal still succeeds;
//   - concurrent first proposals for one Objective create one Blueprint.
func TestOneBlueprintPerObjective(t *testing.T) {
	ctx, _, projects, newHuman := newProjects(t)
	alice := newHuman(1, "alice")
	projectID := uuid.NewV7()
	must(t, func() error {
		_, err := projects.CreateProject(ctx, alice, protocol.ProjectCreate{ProjectID: projectID, Name: "Checkout reliability"})
		return err
	})
	newObjective := func() uuid.UUID {
		id := uuid.NewV7()
		must(t, func() error {
			_, err := projects.CreateObjectiveRevision(ctx, alice, projectID, protocol.ObjectiveRevisionInput{
				ObjectiveID: id, Title: "Reliable checkout", OverviewMarkdown: "Survive outages.", AccountableHumanID: alice,
			}, 0)
			return err
		})
		return id
	}
	proposal := func(blueprintID, objectiveID uuid.UUID) protocol.BlueprintProposal {
		return protocol.BlueprintProposal{
			BlueprintID: blueprintID, ObjectiveID: objectiveID, ObjectiveRevision: 1,
			CellCharters: []protocol.CellCharter{
				{CellID: uuid.NewV7(), AccountableHumanID: alice, Title: "Gateway", OverviewMarkdown: "Own failover."},
			},
		}
	}
	objective, otherObjective := newObjective(), newObjective()
	first, second := uuid.NewV7(), uuid.NewV7()

	firstProposal := proposal(first, objective)
	must(t, func() error {
		_, err := projects.ProposeBlueprintRevision(ctx, alice, projectID, firstProposal, 0)
		return err
	})
	if _, err := projects.ProposeBlueprintRevision(ctx, alice, projectID, firstProposal, 0); err != nil {
		t.Fatalf("retrying the stored proposal: %v", err)
	}

	_, err := projects.ProposeBlueprintRevision(ctx, alice, projectID, proposal(second, objective), 0)
	var taken *application.ObjectiveBlueprintError
	if !errors.As(err, &taken) || taken.BlueprintID != first || taken.CurrentRevision != 1 {
		t.Fatalf("second blueprint for one objective: got %v, want ObjectiveBlueprintError naming %s revision 1", err, first)
	}

	_, err = projects.ProposeBlueprintRevision(ctx, alice, projectID, proposal(first, otherObjective), 1)
	var invalid *application.InvalidError
	if !errors.As(err, &invalid) || invalid.Pointer != "/objective_id" {
		t.Fatalf("revision for another objective: got %v, want invalid input at /objective_id", err)
	}

	must(t, func() error {
		_, err := projects.ProposeBlueprintRevision(ctx, alice, projectID, proposal(second, otherObjective), 0)
		return err
	})

	// The database serializes concurrent first proposals for one Objective.
	contested := newObjective()
	const writers = 6
	errs := make(chan error, writers)
	for range writers {
		go func() {
			_, err := projects.ProposeBlueprintRevision(ctx, alice, projectID, proposal(uuid.NewV7(), contested), 0)
			errs <- err
		}()
	}
	created := 0
	for range writers {
		switch err := <-errs; {
		case err == nil:
			created++
		case !errors.Is(err, application.ErrObjectiveHasBlueprint):
			t.Fatalf("concurrent proposal: %v", err)
		}
	}
	if created != 1 {
		t.Fatalf("concurrent first proposals created %d blueprints, want 1", created)
	}
}

func must(t *testing.T, fn func() error) {
	t.Helper()
	if err := fn(); err != nil {
		t.Fatal(err)
	}
}
