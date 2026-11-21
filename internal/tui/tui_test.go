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

	overview.input.SetValue("Deliver the agreed change.")
	updated, _ = overview.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if got := updated.(model).screen; got != objectiveMemberScreen {
		t.Fatalf("overview entry moved to screen %d, want member selector", got)
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

// TestBlueprintFormRevisesTheObjectivesBlueprint protects decision 0005 in
// the app: choosing an Objective that already has a blueprint builds that
// blueprint's next revision instead of a second blueprint, and Esc from the
// cell member selector returns to a usable previous step.
func TestBlueprintFormRevisesTheObjectivesBlueprint(t *testing.T) {
	m := newModel(context.Background(), func() (*httpclient.Client, error) { return nil, nil })
	m.loading = false
	objective, fresh := uuid.NewV7(), uuid.NewV7()
	blueprint := uuid.NewV7()
	m.status.Objectives = []protocol.ObjectiveSummary{{ObjectiveID: objective, CurrentRevision: 1}, {ObjectiveID: fresh, CurrentRevision: 1}}
	m.status.Blueprints = []protocol.BlueprintSummary{{BlueprintID: blueprint, CurrentRevision: 3,
		Revisions: []protocol.RevisionSummary{{Revision: 3, Status: protocol.BlueprintProposed, ObjectiveID: objective, ObjectiveRevision: 1}}}}
	m.members.Members = []protocol.ProjectMember{{Human: protocol.Human{HumanID: uuid.NewV7(), GitHubLogin: "alice"}}}
	press := func(m model, key tea.KeyMsg) model {
		updated, _ := m.Update(key)
		return updated.(model)
	}
	enter, esc := tea.KeyMsg{Type: tea.KeyEnter}, tea.KeyMsg{Type: tea.KeyEsc}

	m = press(m.startBlueprint(), enter) // the first Objective already has a blueprint
	if m.screen != cellMemberScreen || m.blueprintID != blueprint || m.blueprintExpected != 3 {
		t.Fatalf("objective with a blueprint: screen %d, blueprint %s expected %d; want revision 4 of %s", m.screen, m.blueprintID, m.blueprintExpected, blueprint)
	}

	m = press(m, esc)
	if _, ok := m.selector.SelectedItem().(objectiveItem); m.screen != blueprintObjectiveScreen || !ok {
		t.Fatal("Esc before any cell did not reopen the objective selector with objectives")
	}

	m = press(m, tea.KeyMsg{Type: tea.KeyDown})
	m = press(m, enter) // the second Objective has no blueprint
	if m.blueprintID == blueprint || m.blueprintExpected != 0 {
		t.Fatal("objective without a blueprint did not start a new blueprint")
	}
	m = press(m, enter)
	m.input.SetValue("Gateway")
	m = press(m, enter)
	m.input.SetValue("Own failover.")
	m = press(m, enter)
	m = press(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("a")})
	m = press(m, esc)
	if m.screen != cellMoreScreen || len(m.blueprintCells) != 1 {
		t.Fatalf("Esc from the second cell's member selector: screen %d with %d cells, want the cell list with 1", m.screen, len(m.blueprintCells))
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
