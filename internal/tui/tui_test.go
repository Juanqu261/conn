package tui

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Juanqu261/conn/internal/httpclient"
	"github.com/Juanqu261/conn/internal/protocol"
	"uuid"
)

func TestModelMovesBetweenSectionsWithKeyboard(t *testing.T) {
	m := newModel(context.Background(), func() (*httpclient.Client, error) { return nil, nil })
	m.loading = false
	m.client = &httpclient.Client{} // connected

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyTab})
	if got := updated.(model).section; got != planSection {
		t.Fatalf("section after Tab = %d, want %d", got, planSection)
	}

	updated, _ = updated.(model).Update(tea.KeyMsg{Type: tea.KeyLeft})
	if got := updated.(model).section; got != overviewSection {
		t.Fatalf("section after Left = %d, want %d", got, overviewSection)
	}
}

func TestProjectPickerOpensForSingleProject(t *testing.T) {
	m := newModel(context.Background(), func() (*httpclient.Client, error) { return nil, nil })
	m.loading = false
	m.client = &httpclient.Client{} // connected
	m.projects.Projects = []protocol.ProjectListEntry{{Project: protocol.Project{ProjectID: uuid.NewV7(), Name: "Only"}}}

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("p")})
	if !updated.(model).choosing {
		t.Fatal("p did not open the project picker for one project")
	}
}

func TestObjectiveFormRequiresFieldsThenReviews(t *testing.T) {
	m := newModel(context.Background(), func() (*httpclient.Client, error) { return nil, nil })
	m.loading = false
	m = m.startObjective()

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	invalid := updated.(model)
	if invalid.err == nil || invalid.screen != objectiveTitleScreen {
		t.Fatal("blank objective title did not keep its form open with an error")
	}
	if !strings.Contains(invalid.View(), "Create objective: title") {
		t.Fatal("objective validation error hid the form")
	}

	invalid.input.SetValue("Deliver reliability")
	updated, _ = invalid.Update(tea.KeyMsg{Type: tea.KeyEnter})
	overview := updated.(model)
	if overview.screen != objectiveOverviewScreen {
		t.Fatalf("title entry moved to screen %d, want overview", overview.screen)
	}

	// Markdown is multi-line: Enter starts a new line, and Ctrl+S continues.
	overview.area.SetValue("Deliver the agreed change.")
	updated, _ = overview.Update(tea.KeyMsg{Type: tea.KeyEnter})
	overview = updated.(model)
	overview.area.InsertString("- keep checkout up")
	if overview.screen != objectiveOverviewScreen {
		t.Fatalf("Enter in the Markdown overview moved to screen %d instead of starting a new line", overview.screen)
	}
	updated, _ = overview.Update(tea.KeyMsg{Type: tea.KeyCtrlS})
	member := updated.(model)
	if member.screen != objectiveMemberScreen {
		t.Fatalf("Ctrl+S in the overview moved to screen %d, want member selector", member.screen)
	}
	if want := "Deliver the agreed change.\n- keep checkout up"; member.objective.OverviewMarkdown != want {
		t.Fatalf("overview = %q, want %q", member.objective.OverviewMarkdown, want)
	}
}

func TestCharterRejectionRequiresRationale(t *testing.T) {
	m := newModel(context.Background(), func() (*httpclient.Client, error) { return nil, nil })
	m.loading = false
	m.screen = charterRationaleScreen
	m.input.Focus()

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	invalid := updated.(model)
	if invalid.err == nil || invalid.screen != charterRationaleScreen {
		t.Fatal("blank rejection rationale did not keep the rationale form open")
	}

	invalid.input.SetValue("The boundary needs clarification.")
	updated, _ = invalid.Update(tea.KeyMsg{Type: tea.KeyEnter})
	confirmed := updated.(model)
	if confirmed.screen != charterConfirmScreen || confirmed.charterOutcome != protocol.CharterReject {
		t.Fatal("rationale did not open a rejection confirmation")
	}
}

func press(m model, key tea.KeyMsg) model {
	updated, _ := m.Update(key)
	return updated.(model)
}

var enter, esc, ctrlS = tea.KeyMsg{Type: tea.KeyEnter}, tea.KeyMsg{Type: tea.KeyEsc}, tea.KeyMsg{Type: tea.KeyCtrlS}

func keys(s string) tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)} }

// TestBlueprintFormRevisesTheObjectivesBlueprint protects decision 0001 in
// the app: choosing an Objective that already has a blueprint builds that
// blueprint's next revision from its latest cells, so cell IDs and fields
// the app cannot edit survive, instead of starting a second blueprint.
func TestBlueprintFormRevisesTheObjectivesBlueprint(t *testing.T) {
	m := newModel(context.Background(), func() (*httpclient.Client, error) { return nil, nil })
	m.loading = false
	objective, fresh := uuid.NewV7(), uuid.NewV7()
	blueprint := uuid.NewV7()
	alice, bob := uuid.NewV7(), uuid.NewV7()
	m.status.Objectives = []protocol.ObjectiveSummary{{ObjectiveID: objective, CurrentRevision: 1}, {ObjectiveID: fresh, CurrentRevision: 1}}
	m.status.Blueprints = []protocol.BlueprintSummary{{BlueprintID: blueprint, CurrentRevision: 3,
		Revisions: []protocol.RevisionSummary{{Revision: 3, Status: protocol.BlueprintProposed, ObjectiveID: objective, ObjectiveRevision: 1}}}}
	m.members.Members = []protocol.ProjectMember{
		{Human: protocol.Human{HumanID: alice, GitHubLogin: "alice"}},
		{Human: protocol.Human{HumanID: bob, GitHubLogin: "bob"}},
	}
	api, web := uuid.NewV7(), uuid.NewV7()
	latest := []protocol.CellCharter{
		{CellID: api, AccountableHumanID: alice, Title: "API", OverviewMarkdown: "Own the API.",
			BoundaryPoints: []protocol.BoundaryPoint{{Key: "api", Kind: protocol.BoundaryResponsibility, Statement: "Serve orders."}}},
		{CellID: web, AccountableHumanID: bob, Title: "Web", OverviewMarkdown: "Own the UI.",
			Dependencies: []protocol.CellDependency{{CellID: api, Summary: "Orders API."}}},
	}

	m = press(m.startBlueprint(), enter) // the first Objective already has a blueprint
	if !m.loading || m.blueprintID != blueprint || m.blueprintExpected != 3 {
		t.Fatalf("objective with a blueprint: loading %t, blueprint %s expected %d; want revision 4 of %s loading", m.loading, m.blueprintID, m.blueprintExpected, blueprint)
	}
	updated, _ := m.Update(revisionBaseLoaded{cells: latest})
	m = updated.(model)
	if m.screen != cellMoreScreen || len(m.blueprintCells) != 2 {
		t.Fatalf("revision draft: screen %d with %d cells, want the cell list with the latest 2", m.screen, len(m.blueprintCells))
	}

	// Edit the first cell's title only. The member selector starts on its
	// current member, and the existing overview is kept.
	m = press(m, keys("e"))
	if item, _ := m.selector.SelectedItem().(memberItem); item.member.Human.HumanID != alice {
		t.Fatal("editing a cell did not preselect its accountable member")
	}
	m = press(m, enter)
	m.input.SetValue("Orders API")
	m = press(m, enter)
	m = press(m, ctrlS)
	edited := m.blueprintCells[0]
	if edited.CellID != api || edited.Title != "Orders API" || edited.OverviewMarkdown != "Own the API." || len(edited.BoundaryPoints) != 1 {
		t.Fatalf("edited cell = %+v, want the same cell ID, overview, and boundary point with the new title", edited)
	}

	// Removing a cell drops dependencies on it, which would otherwise make
	// the revision invalid.
	m = press(m, keys("x"))
	if len(m.blueprintCells) != 1 || m.blueprintCells[0].CellID != web || len(m.blueprintCells[0].Dependencies) != 0 {
		t.Fatalf("after removing the API cell: %+v, want only Web without its dependency on API", m.blueprintCells)
	}

	m = press(m, esc) // cancel this draft
	m = press(m.startBlueprint(), tea.KeyMsg{Type: tea.KeyDown})
	m = press(m, enter) // the second Objective has no blueprint
	if m.screen != cellMemberScreen || m.blueprintID == blueprint || m.blueprintExpected != 0 {
		t.Fatal("objective without a blueprint did not start a new, empty blueprint")
	}
	m = press(m, esc)
	if _, ok := m.selector.SelectedItem().(objectiveItem); m.screen != blueprintObjectiveScreen || !ok {
		t.Fatal("Esc before any cell did not reopen the objective selector with objectives")
	}
}

// TestFailedWriteKeepsTheDraft protects drafts from a failed submit: the
// review reopens with the error, and confirming again resends the same
// identities, so a retry cannot create a duplicate.
func TestFailedWriteKeepsTheDraft(t *testing.T) {
	m := newModel(context.Background(), func() (*httpclient.Client, error) { return nil, nil })
	m.loading = false
	m.client = &httpclient.Client{}
	m.projects.Projects = []protocol.ProjectListEntry{{}}
	m = m.startProjectCreate()
	draftID := m.projectDraftID
	m.input.SetValue("Checkout")
	m = press(m, enter)
	m = press(m, enter) // confirm
	updated, _ := m.Update(projectCreated{err: errors.New("server unreachable")})
	m = updated.(model)
	if m.screen != projectConfirmScreen || m.projectName != "Checkout" || !strings.Contains(m.View(), "server unreachable") {
		t.Fatalf("failed create: screen %d, name %q; want the review again with its error", m.screen, m.projectName)
	}
	m = press(m, esc)
	m = press(m, enter)
	if m.projectDraftID != draftID {
		t.Fatal("editing a failed draft changed its project ID, so a retry could create a second project")
	}
}

// TestDisconnectedAppOffersOnlyRetry protects against writing without a
// backend client after the initial load fails.
func TestDisconnectedAppOffersOnlyRetry(t *testing.T) {
	m := newModel(context.Background(), func() (*httpclient.Client, error) { return nil, errors.New("server unreachable") })
	updated, _ := m.Update(projectsLoaded{err: errors.New("server unreachable")})
	updated, _ = updated.(model).Update(tea.KeyMsg{Type: tea.KeyEsc})
	updated, _ = updated.(model).Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("c")})
	disconnected := updated.(model)
	if disconnected.screen != normalScreen {
		t.Fatal("a disconnected app opened a write form")
	}
	if view := disconnected.View(); !strings.Contains(view, "Not connected") || strings.Contains(view, "Start your first project") {
		t.Fatal("a disconnected app did not say it is not connected")
	}
}

// TestRunRejectsPipedStreams protects the interactive boundary: conn app must
// not emit terminal control output or block when invoked by a script or pipe.
func TestRunRejectsPipedStreams(t *testing.T) {
	err := Run(context.Background(), Config{
		In:  &bytes.Buffer{},
		Out: &bytes.Buffer{},
		Client: func() (*httpclient.Client, error) {
			t.Fatal("the backend client must not be resolved for a non-terminal run")
			return nil, nil
		},
	})
	if !errors.Is(err, ErrNotInteractive) {
		t.Fatalf("Run error = %v, want ErrNotInteractive", err)
	}
}
