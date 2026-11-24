package tui

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"testing"
	"time"
	"uuid"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/Juanqu261/conn/internal/httpclient"
	"github.com/Juanqu261/conn/internal/protocol"
)

var uuidPattern = regexp.MustCompile(`[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}`)

// teamModel is a connected owner, alice, on a project with a teammate, two
// objectives, a blueprint, a repository, two colliding Intents, and events.
// extra adds that many more Intents and collisions for long screens.
func teamModel(t *testing.T, extra int) model {
	t.Helper()
	client, err := httpclient.New("http://127.0.0.1:8080", nil)
	if err != nil {
		t.Fatal(err)
	}
	m := newModel(context.Background(), func() (*httpclient.Client, error) { return client, nil })
	alice := protocol.Human{HumanID: uuid.NewV7(), GitHubLogin: "alice"}
	bob := protocol.Human{HumanID: uuid.NewV7(), GitHubLogin: "bob"}
	project := protocol.Project{ProjectID: uuid.NewV7(), Name: "Checkout reliability", OwnerHumanID: alice.HumanID}
	m.loading, m.client, m.human = false, client, alice
	m.selected = protocol.ProjectListEntry{Project: project, Role: protocol.RoleOwner}
	m.projects.Projects = []protocol.ProjectListEntry{m.selected}
	m.projectList.SetItems([]list.Item{projectItem{m.selected}})
	m.members.Members = []protocol.ProjectMember{
		{Human: alice, Membership: protocol.ProjectMembership{HumanID: alice.HumanID, Role: protocol.RoleOwner}},
		{Human: bob, Membership: protocol.ProjectMembership{HumanID: bob.HumanID, Role: protocol.RoleMember}},
	}

	retries, ledger := uuid.NewV7(), uuid.NewV7()
	m.status = protocol.ProjectStatus{Project: project, Role: protocol.RoleOwner,
		Objectives: []protocol.ObjectiveSummary{{ObjectiveID: retries, CurrentRevision: 2}, {ObjectiveID: ledger, CurrentRevision: 1}}}
	m.radar = protocol.RadarView{Project: project, AsOfEvent: 9, Objectives: []protocol.RadarObjective{
		{ObjectiveID: retries, Revision: 2, Title: "Safe checkout retries"}, {ObjectiveID: ledger, Revision: 1, Title: "Ledger export"}}}

	blueprint, gateway, orders, docs := uuid.NewV7(), uuid.NewV7(), uuid.NewV7(), uuid.NewV7()
	m.status.Blueprints = []protocol.BlueprintSummary{{BlueprintID: blueprint, CurrentRevision: 2, Revisions: []protocol.RevisionSummary{
		{Revision: 1, Status: protocol.BlueprintActive, ObjectiveID: retries, ObjectiveRevision: 2, Cells: []protocol.CellSummary{
			{CellID: gateway, AccountableHumanID: alice.HumanID, Title: "Gateway retries", Decision: protocol.CellAccepted},
			{CellID: orders, AccountableHumanID: bob.HumanID, Title: "Order retries", Decision: protocol.CellAccepted}}},
		{Revision: 2, Status: protocol.BlueprintProposed, ObjectiveID: retries, ObjectiveRevision: 2, Cells: []protocol.CellSummary{
			{CellID: docs, AccountableHumanID: alice.HumanID, Title: "Retry documentation", Decision: protocol.CellPending}}},
	}}}
	m.radar.ActiveBlueprint = &protocol.RadarBlueprint{BlueprintID: blueprint, Revision: 1}

	repository := uuid.NewV7()
	m.radar.Repositories = []protocol.RepositoryConnection{{RepositoryID: repository, ProjectID: project.ProjectID, HostKind: "github", CanonicalRemote: "github.com/acme/payments", AnalysisEnabled: true}}

	intent := func(summary string, charter *uuid.UUID) protocol.IntentRevision {
		return protocol.IntentRevision{IntentID: uuid.NewV7(), ProjectID: project.ProjectID, ParticipantID: uuid.NewV7(), Revision: 1,
			ObjectiveID: retries, ObjectiveRevision: 2, CharterID: charter, RepositoryID: &repository, SummaryMarkdown: summary}
	}
	left, right := intent("# Retry gateway charges with the order key.\n\nDetails.", &gateway), intent("Change order retry behavior.", &orders)
	revisions := []protocol.IntentRevision{left, right}
	for i := range extra {
		revisions = append(revisions, intent(fmt.Sprintf("Intent number %02d", i), nil))
	}
	for _, revision := range revisions {
		m.intents.Intents = append(m.intents.Intents, protocol.IntentView{IntentRevision: revision, Status: "active"})
	}
	m.radar.ActiveIntents = revisions

	claim := func(revision protocol.IntentRevision) protocol.SemanticClaim {
		return protocol.SemanticClaim{ClaimID: uuid.NewV7(), IntentID: revision.IntentID, IntentRevision: 1, SubjectKind: "invariant", Relation: "changes",
			NormalizedSubject: "invariant:checkout:one-charge", DisplayName: "One charge", ProvenanceKind: "declared"}
	}
	leftClaim, rightClaim := claim(left), claim(right)
	m.radar.SemanticClaims = []protocol.SemanticClaim{leftClaim, rightClaim}
	collision := func(rule string) protocol.CollisionAssessment {
		return protocol.CollisionAssessment{CollisionID: uuid.NewV7(), LeftIntentID: left.IntentID, LeftIntentRevision: 1, RightIntentID: right.IntentID, RightIntentRevision: 1,
			RuleID: rule, Severity: "amber", Confidence: 1, State: "open", MatchingClaimRefs: []uuid.UUID{leftClaim.ClaimID, rightClaim.ClaimID}, ExplanationMarkdown: "Both change one invariant."}
	}
	opened := collision("shared_declared_invariant")
	m.radar.OpenCollisions = []protocol.CollisionAssessment{opened}
	for i := range extra {
		m.radar.OpenCollisions = append(m.radar.OpenCollisions, collision(fmt.Sprintf("extra_rule_%02d", i)))
	}
	m.radar.EvidenceCoverage = []protocol.RadarEvidenceCoverage{{ObjectiveID: retries, ObjectiveRevision: 2, AcceptanceKey: "retry-safe", State: "missing"}}
	m.radar.KnownUnknowns = []string{"The last line of the Radar names what Conn cannot know."}

	event := func(sequence int64, entity uuid.UUID, payload protocol.EventPayload) protocol.EventEnvelope {
		return protocol.EventEnvelope{ProjectID: project.ProjectID, EventSequence: sequence, EventID: uuid.NewV7(), EntityID: entity,
			ActorHumanID: alice.HumanID, OccurredAt: protocol.NewTimestamp(time.Now()), Payload: payload}
	}
	m.events = []protocol.EventEnvelope{
		event(1, project.ProjectID, protocol.ProjectCreated{}),
		event(2, project.ProjectID, protocol.ProjectMemberAdded{HumanID: bob.HumanID, Role: protocol.RoleMember}),
		event(3, retries, protocol.ObjectiveRevisionCreated{Revision: 1}),
		event(4, blueprint, protocol.BlueprintRevisionProposed{Revision: 1, ObjectiveID: retries, ObjectiveRevision: 2}),
		event(5, uuid.NewV7(), protocol.CharterAccepted{BlueprintID: blueprint, BlueprintRevision: 1, CellID: gateway}),
		event(6, repository, protocol.RepositoryConnected{}),
		event(7, left.IntentID, protocol.IntentPublished{Revision: 1}),
		event(8, opened.CollisionID, protocol.CollisionOpened{}),
	}
	m.asOfEvent = 8
	return m
}

func resize(m model, width, height int) model {
	updated, _ := m.Update(tea.WindowSizeMsg{Width: width, Height: height})
	return updated.(model)
}

// TestEveryScreenFitsTheTerminal protects against cut-off content: at any
// terminal size, the view uses at most the terminal's rows and columns and
// keeps the header on its first row.
func TestEveryScreenFitsTheTerminal(t *testing.T) {
	states := map[string]func(model) model{
		"project picker": func(m model) model { m.choosing = true; return m },
		"help and notice": func(m model) model {
			m.section, m.help, m.notice = planSection, true, "Published Intent “Retry gateway charges with the order key.” as your CLI participant."
			return m
		},
		"error": func(m model) model {
			m.err = fmt.Errorf("%s", strings.Repeat("The server could not be reached. ", 8))
			return m
		},
		"intent drill-down":     func(m model) model { m.screen = intentListScreen; return m },
		"collision drill-down":  func(m model) model { m.screen = radarCollisionScreen; return m },
		"objective selector":    func(m model) model { return m.startBlueprint() },
		"claims field":          func(m model) model { return m.startInput(intentClaimsScreen, "", claimsPlaceholder, "") },
		"project name field":    func(m model) model { return m.startProjectCreate() },
		"charter selector":      func(m model) model { return m.startCharterDecision() },
		"repository connection": func(m model) model { m.screen = repoConfirmScreen; return m },
	}
	for i, name := range sections {
		states["section "+name] = func(m model) model { m.section = section(i); return m }
	}
	for _, size := range [][2]int{{40, 16}, {80, 24}, {160, 50}} {
		width, height := size[0], size[1]
		for name, state := range states {
			m := resize(state(teamModel(t, 12)), width, height)
			view := m.View()
			lines := strings.Split(view, "\n")
			if len(lines) > height {
				t.Errorf("%s at %dx%d: %d rows, more than the terminal's %d:\n%s", name, width, height, len(lines), height, view)
			}
			for _, line := range lines {
				if lipgloss.Width(line) > width {
					t.Errorf("%s at %dx%d: a row is %d columns wide, so the terminal cuts it: %q", name, width, height, lipgloss.Width(line), line)
				}
			}
			if !strings.HasPrefix(lines[0], "CONN") {
				t.Errorf("%s at %dx%d: the header scrolled off screen:\n%s", name, width, height, view)
			}
		}
	}
}

// TestFootersWrapInsteadOfCuttingKeys protects the key hints, the most
// likely text to be cut on a narrow terminal.
func TestFootersWrapInsteadOfCuttingKeys(t *testing.T) {
	m := resize(teamModel(t, 0), 50, 30)
	m.section = planSection
	view := strings.Join(strings.Fields(m.View()), " ")
	for _, hint := range strings.Split(strings.TrimSpace(m.normalFooter()), " · ") {
		if !strings.Contains(view, hint) {
			t.Errorf("footer hint %q is missing at 50 columns:\n%s", hint, m.View())
		}
	}
}

// TestLongSectionsScroll protects content below the fold: a long section
// shows a scroll hint, PgDn reaches its last line, PgUp returns to its top,
// and the arrows scroll a section.
func TestLongSectionsScroll(t *testing.T) {
	m := resize(teamModel(t, 30), 80, 20)
	m = pressKeys(t, m, "3")
	if view := m.View(); !strings.Contains(view, "↓ lines 1-") || strings.Contains(view, "names what Conn cannot know") {
		t.Fatalf("a long Radar must start at the top with a scroll hint:\n%s", view)
	}
	pgdown, pgup := tea.KeyMsg{Type: tea.KeyPgDown}, tea.KeyMsg{Type: tea.KeyPgUp}
	for range 20 {
		m = press(m, pgdown)
	}
	if view := m.View(); !strings.Contains(view, "names what Conn cannot know") || !strings.HasPrefix(view, "CONN") || !strings.Contains(view, "↑ lines") {
		t.Fatalf("PgDn did not reach the end of the Radar with the header kept:\n%s", view)
	}
	for range 20 {
		m = press(m, pgup)
	}
	if m.scroll != 0 || !strings.Contains(m.View(), "Coordination state at event") {
		t.Fatalf("PgUp did not return to the top: scroll %d", m.scroll)
	}
	if m = pressKeys(t, m, "down"); m.scroll != 1 {
		t.Fatalf("Down scrolled a section to %d, want 1", m.scroll)
	}
	if m = pressKeys(t, m, "2"); m.scroll != 0 {
		t.Fatalf("changing section kept scroll %d, want the top", m.scroll)
	}
}

// TestSelectedIntentStaysOnScreen protects in-card lists longer than the
// terminal: moving the cursor keeps the selected row visible.
func TestSelectedIntentStaysOnScreen(t *testing.T) {
	m := teamModel(t, 40)
	m.section = workSection
	m = resize(pressKeys(t, m, "i"), 80, 20)
	for range 35 {
		m = pressKeys(t, m, "down")
	}
	if want := cursorMarker + "[active] Intent number 33 · revision 1"; !strings.Contains(m.View(), want) {
		t.Fatalf("the selected Intent is off screen; want %q in:\n%s", want, m.View())
	}
}

// TestRecordsAreShownByNameNotUUID protects the app's presentation rule:
// people see names, and UUIDs stay internal except the two in Account.
func TestRecordsAreShownByNameNotUUID(t *testing.T) {
	base := resize(teamModel(t, 0), 140, 200)
	screens := map[string]struct {
		state func(model) model
		want  []string
	}{
		"Overview": {func(m model) model { m.section = overviewSection; return m },
			[]string{"Project: Checkout reliability", "Blueprint for “Safe checkout retries” · revision 1 · active"}},
		"Plan": {func(m model) model { m.section = planSection; return m },
			[]string{"Safe checkout retries · revision 2", "Ledger export · revision 1", "[accept] Gateway retries · @alice", "[accept] Order retries · @bob"}},
		"Radar": {func(m model) model { m.section = radarSection; return m },
			[]string{"Active blueprint: Blueprint for “Safe checkout retries”, revision 1",
				"[amber] shared_declared_invariant between “Retry gateway charges with the order key.” and “Change order retry behavior.”",
				"[missing] retry-safe · Safe checkout retries"}},
		"Work": {func(m model) model { m.section = workSection; return m },
			[]string{"[active] Retry gateway charges with the order key. · revision 1 · Safe checkout retries"}},
		"Team": {func(m model) model { m.section = teamSection; return m }, []string{"@alice · owner (you)", "@bob · member"}},
		"Updates": {func(m model) model { m.section = updatesSection; return m },
			[]string{"@bob added as member", "Objective “Safe checkout retries” revision 1 created", "Blueprint for “Safe checkout retries” revision 1 proposed",
				"Charter “Gateway retries” accepted · @alice", "Repository github.com/acme/payments connected",
				"Intent “Retry gateway charges with the order key.” published", "Collision opened: shared_declared_invariant between"}},
		"Intent drill-down": {func(m model) model { m.screen = intentListScreen; return m },
			[]string{"Charter: Gateway retries", "Objective: Safe checkout retries (revision 2)", "Repository: github.com/acme/payments"}},
		"collision drill-down": {func(m model) model { m.screen = radarCollisionScreen; return m },
			[]string{"“Change order retry behavior.” · revision 1", "Intent “Retry gateway charges with the order key.” revision 1"}},
		"objective selector": {func(m model) model { return m.startBlueprint() },
			[]string{"Safe checkout retries", "proposes the next revision of its blueprint", "Ledger export", "no blueprint yet"}},
		"charter selector": {func(m model) model { return m.startCharterDecision() },
			[]string{"Retry documentation", "Blueprint for “Safe checkout retries” · revision 2"}},
		"project picker": {func(m model) model { m.choosing = true; return m }, []string{"Checkout reliability", "Your role: owner"}},
	}
	for name, screen := range screens {
		view := resize(screen.state(base), 140, 200).View()
		if id := uuidPattern.FindString(view); id != "" {
			t.Errorf("%s shows UUID %s:\n%s", name, id, view)
		}
		// Long lines wrap inside the card, so compare words across rows.
		words := strings.Join(strings.Fields(strings.ReplaceAll(view, "│", " ")), " ")
		for _, want := range screen.want {
			if !strings.Contains(words, want) {
				t.Errorf("%s lacks %q:\n%s", name, want, view)
			}
		}
	}

	account := base
	account.section = accountSection
	view := account.View()
	if !strings.Contains(view, base.human.HumanID.String()) || !strings.Contains(view, base.selectedProjectID().String()) {
		t.Fatalf("Account must show the human ID to share and the project ID for conn commands:\n%s", view)
	}
}

// TestResizeUsesTheWindowSize protects Windows terminals, whose resize
// events report the console buffer, which can be thousands of rows tall.
func TestResizeUsesTheWindowSize(t *testing.T) {
	m := teamModel(t, 0)
	m.measure = func() (int, int, bool) { return 100, 30, true }
	if m = resize(m, 100, 9001); m.width != 100 || m.height != 30 {
		t.Fatalf("size after a buffer-sized resize = %dx%d, want the window's 100x30", m.width, m.height)
	}
}

// TestContextPackWrapsLongLines protects the end of a context pack: the
// viewer truncates rather than wraps, which would hide its last lines.
func TestContextPackWrapsLongLines(t *testing.T) {
	m := resize(teamModel(t, 0), 60, 20)
	m.screen = radarCollisionScreen
	collision := m.radar.OpenCollisions[0]
	updated, _ := m.Update(contextLoaded{collisionID: collision.CollisionID, pack: protocol.ConflictContextPack{
		Collision: collision, AllowedResolutionKinds: []string{"sequencing"}, UnresolvedQuestion: strings.Repeat("Which change lands first? ", 30),
		SafeHandlingNote: "LAST LINE OF THE PACK",
	}})
	m = updated.(model)
	m.viewer.GotoBottom()
	if view := m.View(); !strings.Contains(view, "LAST LINE OF THE PACK") {
		t.Fatalf("the end of a context pack with long lines is unreachable:\n%s", view)
	}
}
