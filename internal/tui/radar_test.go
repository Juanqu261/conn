package tui

import (
	"context"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"uuid"

	"github.com/Juanqu261/conn/internal/httpclient"
	"github.com/Juanqu261/conn/internal/protocol"
)

func TestRadarSectionAndCollisionDrillDownStayKeyboardFirst(t *testing.T) {
	m := newModel(context.Background(), func() (*httpclient.Client, error) { return nil, nil })
	m.loading, m.client, m.section = false, &httpclient.Client{}, radarSection
	m.radar = protocol.RadarView{AsOfEvent: 4, OpenCollisions: []protocol.CollisionAssessment{{
		CollisionID: uuid.NewV7(), RuleID: "shared_declared_invariant", Severity: "amber", State: "open", Confidence: 1,
		LeftIntentID: uuid.NewV7(), LeftIntentRevision: 1, RightIntentID: uuid.NewV7(), RightIntentRevision: 1,
		ExplanationMarkdown: "Two declared invariants overlap.",
	}}, KnownUnknowns: []string{"No active work Intent is published. This is absence of reported work, not project completion."}}
	if view := m.radarView(); !strings.Contains(view, "[amber]") || !strings.Contains(view, "absence of reported work") || !strings.Contains(view, "Press x to inspect") {
		t.Fatalf("radar view must expose text statuses and uncertainty: %q", view)
	}
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("x")})
	m = updated.(model)
	if m.screen != radarCollisionScreen || !strings.Contains(m.View(), "Status: open (advisory)") {
		t.Fatalf("x did not open text-labeled collision drill-down: screen %d view %q", m.screen, m.View())
	}
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if updated.(model).screen != normalScreen {
		t.Fatal("Esc did not return from collision drill-down to Radar")
	}
}
