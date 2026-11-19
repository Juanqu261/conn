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
