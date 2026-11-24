// Package tui provides Conn's interactive terminal presentation.
package tui

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"unicode"
	"uuid"

	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/term"

	"github.com/Juanqu261/conn/internal/httpclient"
	"github.com/Juanqu261/conn/internal/protocol"
)

// ErrNotInteractive reports that conn app was started without an interactive
// standard input and output terminal.
var ErrNotInteractive = errors.New("conn app needs an interactive terminal; use the regular conn commands for scripts")

// Config supplies the app's process streams and authenticated backend client.
type Config struct {
	In         io.Reader
	Out        io.Writer
	Client     func() (*httpclient.Client, error)
	IsTerminal func(io.Reader, io.Writer) bool // injectable for tests
}

// Run starts the interactive terminal application.
func Run(ctx context.Context, cfg Config) error {
	isTerminal := cfg.IsTerminal
	if isTerminal == nil {
		isTerminal = interactive
	}
	if cfg.Client == nil || !isTerminal(cfg.In, cfg.Out) {
		return ErrNotInteractive
	}
	m := newModel(ctx, cfg.Client)
	if output, ok := cfg.Out.(descriptor); ok {
		m.measure = func() (int, int, bool) {
			width, height, err := term.GetSize(output.Fd())
			return width, height, err == nil && width > 0 && height > 0
		}
	}
	program := tea.NewProgram(m, tea.WithInput(cfg.In), tea.WithOutput(cfg.Out), tea.WithAltScreen(), tea.WithContext(ctx))
	_, err := program.Run()
	return err
}

type descriptor interface{ Fd() uintptr }

func interactive(in io.Reader, out io.Writer) bool {
	input, inputOK := in.(descriptor)
	output, outputOK := out.(descriptor)
	return inputOK && outputOK && term.IsTerminal(input.Fd()) && term.IsTerminal(output.Fd())
}

type section int

const (
	overviewSection section = iota
	planSection
	radarSection
	workSection
	teamSection
	updatesSection
	accountSection
)

var sections = []string{"Overview", "Plan", "Radar", "Work", "Team", "Updates", "Account"}

type screen int

const (
	normalScreen screen = iota
	projectCreateScreen
	projectConfirmScreen
	memberEntryScreen
	memberConfirmScreen
	objectiveTitleScreen
	objectiveOverviewScreen
	objectiveMemberScreen
	objectiveConfirmScreen
	blueprintObjectiveScreen
	cellMemberScreen
	cellTitleScreen
	cellOverviewScreen
	cellMoreScreen
	blueprintConfirmScreen
	charterPickScreen
	charterActionScreen
	charterRationaleScreen
	charterConfirmScreen
	radarCollisionScreen
	// Semantic coordination screens, handled in semantic.go.
	repoRemoteScreen
	repoConfirmScreen
	intentListScreen
	intentObjectiveScreen
	intentCharterScreen
	intentRepositoryScreen
	intentSummaryScreen
	intentAssumptionsScreen
	intentClaimsScreen
	intentConfirmScreen
	intentReleaseScreen
	evidenceKeyScreen
	evidenceKindScreen
	evidenceStateScreen
	evidenceLocatorScreen
	evidenceSummaryScreen
	evidenceConfirmScreen
	analysisWorktreeScreen
	analysisScriptScreen
	analysisPathsScreen
	analysisConfirmScreen
	contextScreen
	proposalKindScreen
	proposalRationaleScreen
	proposalReferencesScreen
	proposalConfirmScreen
	decisionProposalScreen
	decisionKindScreen
	decisionBindingScreen
	decisionRationaleScreen
	decisionReferencesScreen
	decisionConfirmScreen
	importSourceScreen
	importTargetScreen
	importTitleScreen
	importOverviewScreen
	importCriteriaScreen
	importMemberScreen
	importConfirmScreen
)

type projectItem struct{ entry protocol.ProjectListEntry }

func (p projectItem) Title() string       { return clean(p.entry.Project.Name) }
func (p projectItem) Description() string { return "Your role: " + string(p.entry.Role) }
func (p projectItem) FilterValue() string { return p.entry.Project.Name }

type memberItem struct{ member protocol.ProjectMember }

func (i memberItem) Title() string       { return "@" + clean(i.member.Human.GitHubLogin) }
func (i memberItem) Description() string { return string(i.member.Membership.Role) }
func (i memberItem) FilterValue() string { return i.member.Human.GitHubLogin }

type objectiveItem struct {
	objective protocol.ObjectiveSummary
	title     string
	blueprint bool // the Objective already has a blueprint
}

func (i objectiveItem) Title() string { return i.title }
func (i objectiveItem) Description() string {
	if i.blueprint {
		return fmt.Sprintf("current revision %d · proposes the next revision of its blueprint", i.objective.CurrentRevision)
	}
	return fmt.Sprintf("current revision %d · no blueprint yet", i.objective.CurrentRevision)
}
func (i objectiveItem) FilterValue() string { return i.title }

type charterItem struct {
	blueprintID uuid.UUID
	revision    protocol.RevisionSummary
	cell        protocol.CellSummary
	objective   string // the title of the Objective the blueprint divides
}

func (i charterItem) Title() string { return clean(i.cell.Title) }
func (i charterItem) Description() string {
	return fmt.Sprintf("%s · revision %d", blueprintLabel(i.objective), i.revision.Revision)
}
func (i charterItem) FilterValue() string { return i.cell.Title + " " + i.objective }

type projectsLoaded struct {
	client   *httpclient.Client
	human    protocol.Human
	projects protocol.ProjectList
	err      error
}

type projectLoaded struct {
	projectID uuid.UUID
	status    protocol.ProjectStatus
	radar     protocol.RadarView
	intents   protocol.IntentList
	members   protocol.ProjectMemberList
	events    []protocol.EventEnvelope
	asOfEvent int64
	err       error
}

type projectCreated struct {
	project protocol.Project
	err     error
}

type charterLoaded struct {
	item charterItem
	view protocol.BlueprintRevisionView
	err  error
}

type actionDone struct {
	message string
	err     error
}

// revisionBaseLoaded carries the cells of the revision a new blueprint
// revision starts from.
type revisionBaseLoaded struct {
	cells []protocol.CellCharter
	err   error
}

// submitMarkdown finishes a multi-line Markdown field, where Enter starts a
// new line.
const submitMarkdown = "ctrl+s"

type model struct {
	clientFactory func() (*httpclient.Client, error)
	ctx           context.Context
	client        *httpclient.Client
	human         protocol.Human
	projects      protocol.ProjectList
	selected      protocol.ProjectListEntry
	status        protocol.ProjectStatus
	radar         protocol.RadarView
	intents       protocol.IntentList
	members       protocol.ProjectMemberList
	events        []protocol.EventEnvelope
	asOfEvent     int64

	projectList list.Model
	selector    list.Model
	input       textinput.Model
	area        textarea.Model // Markdown fields
	section     section
	screen      screen
	submitted   screen // the review a write was confirmed from, reopened if the write fails
	choosing    bool
	loading     bool
	help        bool
	err         error
	notice      string
	formNote    string
	width       int
	height      int
	scroll      int                     // first card body row on screen
	measure     func() (int, int, bool) // the terminal window's size, when known

	projectDraftID     uuid.UUID // fixed per draft, so retrying a create cannot make a second project
	projectName        string
	memberID           uuid.UUID
	objective          protocol.ObjectiveRevisionInput
	objectiveMember    protocol.ProjectMember
	blueprintObjective protocol.ObjectiveSummary
	blueprintID        uuid.UUID
	blueprintExpected  int // the Objective's blueprint revision to replace, or 0 for a new blueprint
	blueprintCells     []protocol.CellCharter
	cell               protocol.CellCharter
	editingCell        int // index in blueprintCells of the cell being edited, or -1 for a new cell
	cellCursor         int
	selectedCharter    charterItem
	charterView        protocol.BlueprintRevisionView
	charterOutcome     protocol.CharterOutcome
	radarCollision     int
	intentCursor       int
	semantic           semanticDraft  // drafts and reads for the semantic flows
	viewer             viewport.Model // scrollable conflict context pack
	rejectionRationale string
	createdProject     uuid.UUID
}

func newModel(ctx context.Context, clientFactory func() (*httpclient.Client, error)) model {
	projects := list.New(nil, list.NewDefaultDelegate(), 80, 18)
	projects.Title = "Project picker"
	projects.SetShowStatusBar(false)
	selector := list.New(nil, list.NewDefaultDelegate(), 80, 18)
	selector.SetShowStatusBar(false)
	input := textinput.New()
	input.CharLimit = 4000
	area := textarea.New()
	area.ShowLineNumbers = false
	area.MaxHeight = 0 // CharLimit bounds the text, not a row count
	m := model{ctx: ctx, clientFactory: clientFactory, projectList: projects, selector: selector, input: input, area: area, viewer: viewport.New(80, 16), loading: true, width: 80, height: 24, editingCell: -1}
	return m.fit(true)
}

func (m model) Init() tea.Cmd { return loadProjects(m.ctx, m.clientFactory) }

func loadProjects(ctx context.Context, factory func() (*httpclient.Client, error)) tea.Cmd {
	return func() tea.Msg {
		client, err := factory()
		if err != nil {
			return projectsLoaded{err: err}
		}
		human, err := client.Me(ctx)
		if err != nil {
			return projectsLoaded{err: err}
		}
		projects, err := client.Projects(ctx)
		return projectsLoaded{client: client, human: human, projects: projects, err: err}
	}
}

// recentEvents is how many of the latest project events Updates shows.
const recentEvents = 20

func loadProject(ctx context.Context, client *httpclient.Client, projectID uuid.UUID) tea.Cmd {
	return func() tea.Msg {
		status, err := client.Status(ctx, projectID)
		if err != nil {
			return projectLoaded{projectID: projectID, err: err}
		}
		radar, err := client.Radar(ctx, projectID)
		if err != nil {
			return projectLoaded{projectID: projectID, err: err}
		}
		intents, err := client.Intents(ctx, projectID)
		if err != nil {
			return projectLoaded{projectID: projectID, err: err}
		}
		members, err := client.ProjectMembers(ctx, projectID)
		if err != nil {
			return projectLoaded{projectID: projectID, err: err}
		}
		// The latest events, up to the state the status reflects.
		events, err := client.Events(ctx, projectID, max(0, status.AsOfEvent-recentEvents), recentEvents)
		if err != nil {
			return projectLoaded{projectID: projectID, err: err}
		}
		return projectLoaded{projectID: projectID, status: status.Data, radar: radar.Data, intents: intents.Data, members: members.Data, events: events.Data, asOfEvent: status.AsOfEvent}
	}
}

func createProject(ctx context.Context, client *httpclient.Client, projectID uuid.UUID, name string) tea.Cmd {
	return func() tea.Msg {
		result, err := client.CreateProject(ctx, protocol.ProjectCreate{ProjectID: projectID, Name: strings.TrimSpace(name)})
		if err != nil {
			return projectCreated{err: err}
		}
		return projectCreated{project: result.Data}
	}
}

func (m model) selectedProjectID() uuid.UUID { return m.selected.Project.ProjectID }

// multiline reports whether a form field is Markdown, edited in a
// multi-line area where Enter starts a new line.
func multiline(s screen) bool {
	switch s {
	case objectiveOverviewScreen, cellOverviewScreen, intentSummaryScreen, intentAssumptionsScreen, intentClaimsScreen,
		proposalRationaleScreen, decisionRationaleScreen, importOverviewScreen, importCriteriaScreen:
		return true
	}
	return false
}

func (m model) startInput(next screen, prompt, placeholder string, value string) model {
	m.screen, m.choosing, m.help, m.err = next, false, false, nil
	if multiline(next) {
		m.input.Blur()
		m.area.Placeholder, m.area.CharLimit = placeholder, inputLimit(next)
		m.area.SetValue(value)
		m.area.Focus()
		return m
	}
	m.area.Blur()
	m.input.Prompt, m.input.Placeholder = prompt, placeholder
	m.input.CharLimit = inputLimit(next)
	m.input.SetValue(value)
	m.input.Focus()
	return m
}

// inputLimit is the longest value, in code points, that a form field's
// contracts/v1 schema accepts, so drafts cannot fail on length after review.
func inputLimit(s screen) int {
	switch s {
	case projectCreateScreen, objectiveTitleScreen, cellTitleScreen, importTitleScreen:
		return 200 // common#/$defs/title
	case memberEntryScreen:
		return 36 // a canonical UUID
	case charterRationaleScreen, proposalRationaleScreen, decisionRationaleScreen:
		return 4000 // common#/$defs/rationale
	case evidenceLocatorScreen:
		return 256 // common#/$defs/opaqueReference
	case evidenceSummaryScreen:
		return 1000 // common#/$defs/statement
	case repoRemoteScreen, importSourceScreen, analysisWorktreeScreen, analysisScriptScreen:
		return 1024 // a remote or local path
	case proposalReferencesScreen, decisionReferencesScreen:
		return 20 * 257 // up to 20 comma-separated opaque references
	case analysisPathsScreen:
		return 64 << 10 // bounded by the analyzer request limit
	default:
		return 32768 // common#/$defs/markdown
	}
}

func (m model) startProjectCreate() model {
	m.projectDraftID = uuid.NewV7()
	return m.startInput(projectCreateScreen, "Project name: ", "e.g. Checkout reliability", "")
}

func (m model) startMemberAdd() model {
	return m.startInput(memberEntryScreen, "Conn human UUID: ", "paste an already authenticated human ID", "")
}

func (m model) startObjective() model {
	m.objective = protocol.ObjectiveRevisionInput{ObjectiveID: uuid.NewV7()}
	m.objectiveMember = protocol.ProjectMember{}
	return m.startInput(objectiveTitleScreen, "Objective title: ", "e.g. Deliver checkout reliability", "")
}

// blueprintFor returns the Blueprint that divides an Objective. An Objective
// has at most one, and its in-force revisions all name that Objective.
func blueprintFor(status protocol.ProjectStatus, objectiveID uuid.UUID) (protocol.BlueprintSummary, bool) {
	for _, blueprint := range status.Blueprints {
		for _, revision := range blueprint.Revisions {
			if revision.ObjectiveID == objectiveID {
				return blueprint, true
			}
		}
	}
	return protocol.BlueprintSummary{}, false
}

// chooseBlueprintObjective starts the cell set for an Objective. A new
// blueprint starts empty. The next revision of an existing blueprint starts
// from its latest revision's cells, so unchanged cells keep their IDs and
// the fields this app does not edit.
func (m model) chooseBlueprintObjective(objective protocol.ObjectiveSummary) (model, tea.Cmd) {
	m.blueprintObjective, m.blueprintCells, m.cell = objective, nil, protocol.CellCharter{}
	m.editingCell, m.cellCursor, m.formNote = -1, 0, ""
	blueprint, ok := blueprintFor(m.status, objective.ObjectiveID)
	if !ok {
		m.blueprintID, m.blueprintExpected = uuid.NewV7(), 0
		return m.chooseCellMember(), nil
	}
	m.blueprintID, m.blueprintExpected, m.loading, m.err = blueprint.BlueprintID, blueprint.CurrentRevision, true, nil
	client, ctx, projectID := m.client, m.ctx, m.selectedProjectID()
	return m, func() tea.Msg {
		result, err := client.BlueprintRevision(ctx, projectID, blueprint.BlueprintID, blueprint.CurrentRevision)
		return revisionBaseLoaded{cells: result.Data.BlueprintRevision.CellCharters, err: err}
	}
}

// showCells returns to the draft's cell list.
func (m model) showCells() model {
	m.cell, m.editingCell, m.screen, m.err = protocol.CellCharter{}, -1, cellMoreScreen, nil
	m.cellCursor = max(0, min(m.cellCursor, len(m.blueprintCells)-1))
	return m
}

// chooseCellMember opens the member selector for the cell being drafted.
func (m model) chooseCellMember() model {
	n := len(m.blueprintCells) + 1
	if m.editingCell >= 0 {
		n = m.editingCell + 1
	}
	return m.chooseMember(cellMemberScreen, fmt.Sprintf("Accountable member selector for cell %d", n), m.cell.AccountableHumanID)
}

// removeCell drops the selected cell and every dependency on it, because a
// dependency must name a cell of the same revision.
func (m model) removeCell() model {
	removed := m.blueprintCells[m.cellCursor]
	cells := make([]protocol.CellCharter, 0, len(m.blueprintCells)-1)
	dropped := 0
	for i, cell := range m.blueprintCells {
		if i == m.cellCursor {
			continue
		}
		var kept []protocol.CellDependency
		for _, dependency := range cell.Dependencies {
			if dependency.CellID == removed.CellID {
				dropped++
				continue
			}
			kept = append(kept, dependency)
		}
		cell.Dependencies = kept
		cells = append(cells, cell)
	}
	m.blueprintCells = cells
	m.formNote = fmt.Sprintf("Removed cell %q.", clean(removed.Title))
	if dropped > 0 {
		m.formNote += fmt.Sprintf(" Also removed %d dependency on it from other cells.", dropped)
	}
	return m.showCells()
}

func (m model) startBlueprint() model {
	m.blueprintCells, m.cell, m.editingCell, m.formNote = nil, protocol.CellCharter{}, -1, ""
	items := make([]list.Item, 0, len(m.status.Objectives))
	for _, objective := range m.status.Objectives {
		_, revises := blueprintFor(m.status, objective.ObjectiveID)
		items = append(items, objectiveItem{objective, m.objectiveTitle(objective.ObjectiveID), revises})
	}
	m.selector.Title = "Objective selector for this blueprint"
	m.selector.ResetFilter()
	m.selector.SetItems(items)
	m.selector.ResetSelected()
	m.screen, m.err = blueprintObjectiveScreen, nil
	return m
}

func (m model) startCharterDecision() model {
	items := make([]list.Item, 0)
	for _, blueprint := range m.status.Blueprints {
		for _, revision := range blueprint.Revisions {
			if revision.Status != protocol.BlueprintProposed {
				continue
			}
			for _, cell := range revision.Cells {
				if cell.AccountableHumanID == m.human.HumanID && cell.Decision == protocol.CellPending {
					items = append(items, charterItem{blueprint.BlueprintID, revision, cell, m.objectiveTitle(revision.ObjectiveID)})
				}
			}
		}
	}
	m.selector.Title = "Pending charter selector"
	m.selector.ResetFilter()
	m.selector.SetItems(items)
	m.selector.ResetSelected()
	m.screen, m.err = charterPickScreen, nil
	return m
}

// chooseMember opens the member selector with current, if set, selected.
func (m model) chooseMember(next screen, title string, current uuid.UUID) model {
	items := make([]list.Item, 0, len(m.members.Members))
	for _, member := range m.members.Members {
		items = append(items, memberItem{member})
	}
	m.selector.Title = title
	m.selector.ResetFilter()
	m.selector.SetItems(items)
	m.selector.ResetSelected()
	for i, member := range m.members.Members {
		if member.Human.HumanID == current {
			m.selector.Select(i)
		}
	}
	m.screen, m.err = next, nil
	return m
}

// confirm leaves a review screen to run its write. If the write fails, the
// review reopens with the error and the draft intact.
func (m model) confirm() model {
	m.submitted, m.screen, m.loading, m.err = m.screen, normalScreen, true, nil
	return m
}

func (m model) failed(err error) model {
	m.loading, m.err = false, err
	if m.submitted != normalScreen {
		m.screen = m.submitted
	}
	m.submitted = normalScreen
	return m
}

// Update handles a message, then fits the result to the terminal.
func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	before := m.position()
	updated, cmd := m.update(msg)
	next := updated.(model)
	return next.fit(next.position() != before), cmd
}

func (m model) update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		// On Windows a resize reports the console buffer, which can be far
		// taller than the window, so prefer the window's own size.
		if m.measure != nil {
			if width, height, ok := m.measure(); ok {
				m.width, m.height = width, height
			}
		}
		return m, nil
	case projectsLoaded:
		m.loading, m.err = false, msg.err
		if msg.err != nil {
			return m, nil
		}
		m.client, m.human, m.projects = msg.client, msg.human, msg.projects
		items := make([]list.Item, 0, len(msg.projects.Projects))
		for _, entry := range msg.projects.Projects {
			items = append(items, projectItem{entry})
			if entry.Project.ProjectID == m.createdProject {
				m.selected = entry
			}
		}
		m.projectList.SetItems(items)
		if len(m.projects.Projects) == 1 && m.selected.Project.ProjectID == (uuid.UUID{}) {
			m.selected = m.projects.Projects[0]
		}
		if m.selected.Project.ProjectID != (uuid.UUID{}) {
			m.createdProject, m.loading = uuid.UUID{}, true
			return m, loadProject(m.ctx, m.client, m.selectedProjectID())
		}
		m.choosing = len(msg.projects.Projects) > 1
		return m, nil
	case projectCreated:
		if msg.err != nil {
			return m.failed(msg.err), nil
		}
		m.submitted, m.err = normalScreen, nil
		m.createdProject, m.loading, m.notice = msg.project.ProjectID, true, "Project created."
		return m, loadProjects(m.ctx, m.clientFactory)
	case projectLoaded:
		m.loading, m.err = false, msg.err
		if msg.err == nil {
			m.status, m.radar, m.intents, m.members, m.events, m.asOfEvent = msg.status, msg.radar, msg.intents, msg.members, msg.events, msg.asOfEvent
			// A refresh can shrink the open collision or Intent list under a drill-down.
			m.radarCollision = max(0, min(m.radarCollision, len(m.radar.OpenCollisions)-1))
			m.intentCursor = max(0, min(m.intentCursor, len(m.intents.Intents)-1))
		}
		return m, nil
	case charterLoaded:
		m.loading, m.err = false, msg.err
		if msg.err != nil {
			return m, nil
		}
		m.selectedCharter, m.charterView, m.screen = msg.item, msg.view, charterActionScreen
		return m, nil
	case actionDone:
		if msg.err != nil {
			return m.failed(msg.err), nil
		}
		m.notice, m.screen, m.submitted, m.err = msg.message, normalScreen, normalScreen, nil
		return m, loadProject(m.ctx, m.client, m.selectedProjectID())
	case proposalsLoaded, contextLoaded, evidenceKeysLoaded, importBaseLoaded:
		return m.updateSemanticMessage(msg)
	case revisionBaseLoaded:
		m.loading, m.err = false, msg.err
		if msg.err != nil {
			return m, nil // stays on the objective selector, which shows the error
		}
		m.blueprintCells = append([]protocol.CellCharter(nil), msg.cells...)
		return m.showCells(), nil
	case tea.KeyMsg:
		if msg.String() == "ctrl+c" {
			return m, tea.Quit
		}
		if m.loading {
			return m, nil
		}
		if step, ok := m.scrollStep(msg.String()); ok {
			m.scroll = max(0, m.scroll+step) // fit keeps it in range
			return m, nil
		}
		if semanticScreen(m.screen) {
			return m.updateSemanticScreen(msg)
		}
		if m.screen != normalScreen {
			return m.updateScreen(msg)
		}
		return m.updateNormal(msg)
	}
	return m, nil
}

func (m model) updateNormal(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := msg.String()
	if m.client == nil {
		// Not connected: nothing can be read or written, so only retry,
		// help, and quit apply.
		switch key {
		case "q":
			return m, tea.Quit
		case "r", "enter":
			m.loading, m.err = true, nil
			return m, loadProjects(m.ctx, m.clientFactory)
		case "esc":
			m.err = nil
		case "?":
			m.help = !m.help
		}
		return m, nil
	}
	if m.choosing {
		if key == "c" && !m.projectList.SettingFilter() {
			return m.startProjectCreate(), nil
		}
		if key == "q" && !m.projectList.SettingFilter() {
			return m, tea.Quit
		}
		if key == "esc" {
			m.choosing = false
			return m, nil
		}
		if key == "enter" {
			if item, ok := m.projectList.SelectedItem().(projectItem); ok {
				m.selected, m.choosing, m.loading, m.err = item.entry, false, true, nil
				return m, loadProject(m.ctx, m.client, m.selectedProjectID())
			}
		}
		var cmd tea.Cmd
		m.projectList, cmd = m.projectList.Update(msg)
		return m, cmd
	}
	if len(m.projects.Projects) == 0 && (key == "c" || key == "enter") {
		return m.startProjectCreate(), nil
	}
	switch key {
	case "q":
		return m, tea.Quit
	case "esc":
		m.err = nil
	case "?":
		m.help = !m.help
	case "1":
		m.section = overviewSection
	case "2":
		m.section = planSection
	case "3":
		m.section = radarSection
	case "4":
		m.section = workSection
	case "5":
		m.section = teamSection
	case "6":
		m.section = updatesSection
	case "7":
		m.section = accountSection
	case "tab", "right", "l":
		m.section = (m.section + 1) % section(len(sections))
	case "shift+tab", "left", "h":
		m.section = (m.section + section(len(sections)) - 1) % section(len(sections))
	case "p":
		m.choosing = true // The picker remains useful even for one project.
	case "c":
		return m.startProjectCreate(), nil
	case "a":
		if m.section == teamSection && m.status.Role == protocol.RoleOwner {
			return m.startMemberAdd(), nil
		}
	case "o":
		if m.section == planSection && m.status.Role == protocol.RoleOwner {
			return m.startObjective(), nil
		}
	case "b":
		if m.section == planSection && m.status.Role == protocol.RoleOwner {
			if len(m.status.Objectives) == 0 {
				m.err = errors.New("Create an objective before proposing a blueprint.")
				return m, nil
			}
			return m.startBlueprint(), nil
		}
	case "d":
		if m.section == planSection {
			return m.startCharterDecision(), nil
		}
	case "x":
		if m.section == radarSection && len(m.radar.OpenCollisions) > 0 {
			m.radarCollision, m.screen, m.err = 0, radarCollisionScreen, nil
			return m.loadProposals()
		}
	case "g", "n", "i", "k":
		return m.startSemanticAction(key)
	case "r", "enter":
		m.loading, m.err = true, nil
		if m.client == nil || m.selectedProjectID() == (uuid.UUID{}) {
			return m, loadProjects(m.ctx, m.clientFactory)
		}
		return m, loadProject(m.ctx, m.client, m.selectedProjectID())
	}
	return m, nil
}

func (m model) updateScreen(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := msg.String()
	if key == "esc" {
		switch m.screen {
		case projectConfirmScreen:
			return m.startInput(projectCreateScreen, "Project name: ", "e.g. Checkout reliability", m.projectName), nil
		case memberConfirmScreen:
			return m.startInput(memberEntryScreen, "Conn human UUID: ", "paste an already authenticated human ID", m.memberID.String()), nil
		case objectiveOverviewScreen:
			m.objective.OverviewMarkdown = m.area.Value() // going back keeps the draft
			return m.startInput(objectiveTitleScreen, "Objective title: ", "e.g. Deliver checkout reliability", m.objective.Title), nil
		case objectiveMemberScreen:
			return m.startInput(objectiveOverviewScreen, "", "Describe the objective in Markdown", m.objective.OverviewMarkdown), nil
		case objectiveConfirmScreen:
			return m.chooseMember(objectiveMemberScreen, "Accountable member selector", m.objective.AccountableHumanID), nil
		case cellMemberScreen:
			// Back to the cells so far, or to the objective selector with
			// its own items when there are none yet.
			if len(m.blueprintCells) > 0 {
				return m.showCells(), nil
			}
			return m.startBlueprint(), nil
		case cellTitleScreen:
			m.cell.Title = strings.TrimSpace(m.input.Value())
			return m.chooseCellMember(), nil
		case cellOverviewScreen:
			m.cell.OverviewMarkdown = m.area.Value()
			return m.startInput(cellTitleScreen, "Cell title: ", "e.g. Documentation", m.cell.Title), nil
		case blueprintConfirmScreen:
			m.screen = cellMoreScreen
			return m, nil
		case charterActionScreen:
			return m.startCharterDecision(), nil
		case charterRationaleScreen:
			m.screen = charterActionScreen
			return m, nil
		case charterConfirmScreen:
			if m.charterOutcome == protocol.CharterReject {
				return m.startInput(charterRationaleScreen, "Rejection rationale: ", "Explain the boundary problem", m.rejectionRationale), nil
			}
			m.screen = charterActionScreen
			return m, nil
		default:
			m.screen, m.err = normalScreen, nil
			return m, nil
		}
	}
	switch m.screen {
	case projectCreateScreen:
		if key == "enter" {
			if strings.TrimSpace(m.input.Value()) == "" {
				m.err = errors.New("Enter a project name before continuing.")
				return m, nil
			}
			m.projectName, m.screen, m.err = strings.TrimSpace(m.input.Value()), projectConfirmScreen, nil
			return m, nil
		}
	case projectConfirmScreen:
		if key == "enter" {
			m = m.confirm()
			return m, createProject(m.ctx, m.client, m.projectDraftID, m.projectName)
		}
	case memberEntryScreen:
		if key == "enter" {
			id, err := uuid.Parse(strings.TrimSpace(m.input.Value()))
			if err != nil || id[6]>>4 != 7 {
				m.err = errors.New("Paste a valid Conn human UUIDv7.")
				return m, nil
			}
			m.memberID, m.screen, m.err = id, memberConfirmScreen, nil
			return m, nil
		}
	case memberConfirmScreen:
		if key == "enter" {
			return m.submitAddMember()
		}
	case objectiveTitleScreen:
		if key == "enter" {
			if strings.TrimSpace(m.input.Value()) == "" {
				m.err = errors.New("Enter an objective title before continuing.")
				return m, nil
			}
			m.objective.Title = strings.TrimSpace(m.input.Value())
			return m.startInput(objectiveOverviewScreen, "", "Describe the objective in Markdown", m.objective.OverviewMarkdown), nil
		}
	case objectiveOverviewScreen:
		if key == submitMarkdown {
			if strings.TrimSpace(m.area.Value()) == "" {
				m.err = errors.New("Enter a Markdown overview before continuing.")
				return m, nil
			}
			m.objective.OverviewMarkdown = strings.TrimSpace(m.area.Value())
			return m.chooseMember(objectiveMemberScreen, "Accountable member selector", m.objective.AccountableHumanID), nil
		}
	case objectiveMemberScreen:
		if key == "enter" {
			if item, ok := m.selector.SelectedItem().(memberItem); ok {
				m.objectiveMember, m.objective.AccountableHumanID, m.screen = item.member, item.member.Human.HumanID, objectiveConfirmScreen
				return m, nil
			}
		}
		return m.updateSelector(msg)
	case objectiveConfirmScreen:
		if key == "enter" {
			return m.submitObjective()
		}
	case blueprintObjectiveScreen:
		if key == "enter" {
			if item, ok := m.selector.SelectedItem().(objectiveItem); ok {
				return m.chooseBlueprintObjective(item.objective)
			}
		}
		return m.updateSelector(msg)
	case cellMemberScreen:
		if key == "enter" {
			if item, ok := m.selector.SelectedItem().(memberItem); ok {
				m.cell.AccountableHumanID = item.member.Human.HumanID
				return m.startInput(cellTitleScreen, "Cell title: ", "e.g. Documentation", m.cell.Title), nil
			}
		}
		return m.updateSelector(msg)
	case cellTitleScreen:
		if key == "enter" {
			if strings.TrimSpace(m.input.Value()) == "" {
				m.err = errors.New("Enter a cell title before continuing.")
				return m, nil
			}
			m.cell.Title = strings.TrimSpace(m.input.Value())
			return m.startInput(cellOverviewScreen, "", "Describe this human's boundary in Markdown", m.cell.OverviewMarkdown), nil
		}
	case cellOverviewScreen:
		if key == submitMarkdown {
			if strings.TrimSpace(m.area.Value()) == "" {
				m.err = errors.New("Enter a cell Markdown overview before continuing.")
				return m, nil
			}
			m.cell.OverviewMarkdown = strings.TrimSpace(m.area.Value())
			cells := append([]protocol.CellCharter(nil), m.blueprintCells...)
			if m.editingCell >= 0 {
				cells[m.editingCell], m.cellCursor = m.cell, m.editingCell // keeps the cell ID
			} else {
				m.cell.CellID = uuid.NewV7()
				cells, m.cellCursor = append(cells, m.cell), len(cells)
			}
			m.blueprintCells = cells
			return m.showCells(), nil
		}
	case cellMoreScreen:
		m.formNote = ""
		switch key {
		case "up", "k":
			m.cellCursor = max(0, m.cellCursor-1)
		case "down", "j":
			m.cellCursor = min(len(m.blueprintCells)-1, m.cellCursor+1)
		case "a":
			m.cell, m.editingCell = protocol.CellCharter{}, -1
			return m.chooseCellMember(), nil
		case "e":
			if len(m.blueprintCells) > 0 {
				m.cell, m.editingCell = m.blueprintCells[m.cellCursor], m.cellCursor
				return m.chooseCellMember(), nil
			}
		case "x":
			if len(m.blueprintCells) > 0 {
				return m.removeCell(), nil
			}
		case "enter":
			if len(m.blueprintCells) == 0 {
				m.err = errors.New("Add at least one cell before reviewing the proposal.")
				return m, nil
			}
			m.screen, m.err = blueprintConfirmScreen, nil
		}
		return m, nil
	case blueprintConfirmScreen:
		if key == "enter" {
			return m.submitBlueprint()
		}
	case charterPickScreen:
		if key == "enter" {
			if item, ok := m.selector.SelectedItem().(charterItem); ok {
				m.loading, m.err = true, nil
				return m, func() tea.Msg {
					result, err := m.client.BlueprintRevision(m.ctx, m.selectedProjectID(), item.blueprintID, item.revision.Revision)
					return charterLoaded{item: item, view: result.Data, err: err}
				}
			}
		}
		return m.updateSelector(msg)
	case charterActionScreen:
		if key == "a" {
			m.charterOutcome, m.screen = protocol.CharterAccept, charterConfirmScreen
			return m, nil
		}
		if key == "x" {
			return m.startInput(charterRationaleScreen, "Rejection rationale: ", "Explain the boundary problem", ""), nil
		}
	case charterRationaleScreen:
		if key == "enter" {
			if strings.TrimSpace(m.input.Value()) == "" {
				m.err = errors.New("A rejection requires a rationale.")
				return m, nil
			}
			m.rejectionRationale, m.charterOutcome, m.screen, m.err = strings.TrimSpace(m.input.Value()), protocol.CharterReject, charterConfirmScreen, nil
			return m, nil
		}
	case charterConfirmScreen:
		if key == "enter" {
			return m.submitCharterDecision()
		}
	}
	var cmd tea.Cmd
	if multiline(m.screen) {
		m.area, cmd = m.area.Update(msg)
	} else {
		m.input, cmd = m.input.Update(msg)
	}
	m.err = nil
	return m, cmd
}

func (m model) updateSelector(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	m.selector, cmd = m.selector.Update(msg)
	return m, cmd
}

func (m model) submitAddMember() (tea.Model, tea.Cmd) {
	m = m.confirm()
	return m, func() tea.Msg {
		_, err := m.client.AddMember(m.ctx, m.selectedProjectID(), m.memberID)
		return actionDone{"Member added.", err}
	}
}

func (m model) submitObjective() (tea.Model, tea.Cmd) {
	m = m.confirm()
	input := m.objective
	return m, func() tea.Msg {
		_, err := m.client.CreateObjective(m.ctx, m.selectedProjectID(), input)
		return actionDone{"Objective created.", err}
	}
}

func (m model) submitBlueprint() (tea.Model, tea.Cmd) {
	proposal := protocol.BlueprintProposal{BlueprintID: m.blueprintID, ObjectiveID: m.blueprintObjective.ObjectiveID, ObjectiveRevision: m.blueprintObjective.CurrentRevision, CellCharters: m.blueprintCells}
	expected := m.blueprintExpected
	m = m.confirm()
	return m, func() tea.Msg {
		var err error
		if expected > 0 {
			_, err = m.client.ReviseBlueprint(m.ctx, m.selectedProjectID(), expected, proposal)
		} else {
			_, err = m.client.ProposeBlueprint(m.ctx, m.selectedProjectID(), proposal)
		}
		return actionDone{"Blueprint proposal created. Assigned humans must decide their own charters.", err}
	}
}

func (m model) submitCharterDecision() (tea.Model, tea.Cmd) {
	item, outcome, rationale := m.selectedCharter, m.charterOutcome, m.rejectionRationale
	m = m.confirm()
	return m, func() tea.Msg {
		var err error
		if outcome == protocol.CharterAccept {
			_, err = m.client.AcceptCharter(m.ctx, m.selectedProjectID(), item.blueprintID, item.revision.Revision, item.cell.CellID)
		} else {
			_, err = m.client.RejectCharter(m.ctx, m.selectedProjectID(), item.blueprintID, item.revision.Revision, item.cell.CellID, rationale)
		}
		verb := "Charter accepted."
		if outcome == protocol.CharterReject {
			verb = "Charter rejected."
		}
		return actionDone{verb, err}
	}
}

// page is the content of the current screen.
func (m model) page() page {
	if m.loading {
		return page{title: "Loading", body: "Loading shared team state..."}
	}
	if semanticScreen(m.screen) {
		return m.semanticPage()
	}
	if m.screen != normalScreen {
		return m.formPage()
	}
	if m.err != nil {
		return page{title: "Could not complete that action", body: errorStyle.Render(clean(m.err.Error())), footer: "Esc back · r refresh · q quit"}
	}
	if m.client == nil {
		return page{title: "Not connected", body: "Conn could not load your projects, so nothing can be read or changed yet.\n\nCheck the server and your session with conn doctor, then press r to try again.", footer: "r retry · q quit"}
	}
	if m.choosing {
		return page{list: projectsList, footer: "Project picker: Up/down select · / filter · Enter open · c new project · Esc back · q quit"}
	}
	if len(m.projects.Projects) == 0 {
		return page{title: "Start your first project", body: "No projects are available to this Conn account.\n\nPress c or Enter to create a project here.", footer: "c or Enter create project · r refresh · q quit"}
	}
	// The notice and help lead the card, so they scroll with it.
	body := m.sectionView()
	if m.help {
		body = helpStyle.Render(m.helpText()) + "\n\n" + body
	}
	if m.notice != "" {
		body = helpStyle.Render(clean(m.notice)) + "\n\n" + body
	}
	return page{title: sections[m.section], body: body, footer: m.normalFooter()}
}

func (m model) formPage() page {
	title, body, footer := "", "", ""
	switch m.screen {
	case projectCreateScreen:
		title, body, footer = "Create project", "Name the project your team will coordinate.\n\n"+m.input.View(), "  Enter review · Esc cancel"
	case projectConfirmScreen:
		title, body, footer = "Create this project?", "Project name: "+clean(m.projectName)+"\n\nYou will become the project owner.", "  Enter create project · Esc edit"
	case memberEntryScreen:
		title, body, footer = "Add project member", "Paste the Conn human UUID of a person who has already run conn login.\n\n"+m.input.View(), "  Enter review · Esc cancel"
	case memberConfirmScreen:
		title, body, footer = "Add this member?", "Conn human ID: "+m.memberID.String()+"\n\nThey will receive the member role. This does not accept a charter for them.", "  Enter add member · Esc edit"
	case objectiveTitleScreen:
		title, body, footer = "Create objective: title", m.input.View(), "  Enter next · Esc cancel"
	case objectiveOverviewScreen:
		title, body, footer = "Create objective: Markdown overview", m.area.View(), "  Ctrl+S choose accountable member · Enter new line · Esc back to title"
	case objectiveMemberScreen:
		return page{list: selectorList, footer: "  Form selector: Up/down select · / filter · Enter choose · Esc back to overview"}
	case objectiveConfirmScreen:
		title, body, footer = "Create this objective?", fmt.Sprintf("Title: %s\n\nMarkdown overview:\n%s\n\nAccountable human: @%s", clean(m.objective.Title), cleanBlock(m.objective.OverviewMarkdown), clean(m.objectiveMember.Human.GitHubLogin)), "  Enter create objective · Esc edit member"
	case blueprintObjectiveScreen:
		return page{list: selectorList, footer: "  Form selector: choose current objective · Enter choose · Esc cancel"}
	case cellMemberScreen:
		return page{list: selectorList, footer: "  Form selector: choose accountable member · Enter choose · Esc back"}
	case cellTitleScreen:
		title, body, footer = m.cellStep("title"), m.input.View(), "  Enter next · Esc back to member"
	case cellOverviewScreen:
		done := "add cell"
		if m.editingCell >= 0 {
			done = "save cell"
		}
		title, body, footer = m.cellStep("Markdown overview"), m.area.View(), "  Ctrl+S "+done+" · Enter new line · Esc back to title"
	case cellMoreScreen:
		title, body = "Blueprint cells", m.cellsReview()
		footer = "  a add cell · Enter review proposal · Esc cancel"
		if len(m.blueprintCells) > 0 {
			footer = "  Up/down select · e edit · x remove · a add cell · Enter review proposal · Esc cancel"
		}
	case blueprintConfirmScreen:
		title, body, footer = "Propose this blueprint?", m.blueprintReview(), "  Enter propose blueprint · Esc edit cells"
	case charterPickScreen:
		if len(m.selector.Items()) == 0 {
			title, body, footer = "No pending charter", "You have no assigned pending charters in a proposed blueprint.", "  Esc back"
		} else {
			return page{list: selectorList, footer: "  Charter selector: Up/down select · / filter · Enter inspect · Esc back"}
		}
	case charterActionScreen:
		title, body, footer = "Your pending charter", m.charterReview()+"\n\nOnly you can decide this charter. A decision is final for this revision.", "  a accept · x reject · Esc back"
	case charterRationaleScreen:
		title, body, footer = "Reject charter: rationale", m.input.View(), "  Enter review rejection · Esc back"
	case charterConfirmScreen:
		verb := "accept"
		if m.charterOutcome == protocol.CharterReject {
			verb = "reject"
		}
		body = m.charterReview() + "\n\nYou are about to " + verb + " your own charter. This decision is final for this revision."
		if m.charterOutcome == protocol.CharterReject {
			body += "\n\nRationale:\n" + clean(m.rejectionRationale)
		}
		title, footer = "Confirm charter decision", "  Enter confirm · Esc edit"
	}
	if m.err != nil {
		body += "\n\n" + errorStyle.Render(clean(m.err.Error()))
	}
	return page{title: title, body: body, footer: footer}
}

func (m model) cellStep(field string) string {
	if m.editingCell >= 0 {
		return fmt.Sprintf("Edit cell %d: %s", m.editingCell+1, field)
	}
	return "Add cell: " + field
}

func (m model) normalFooter() string {
	footer := "  Tab sections · Enter refresh · p project picker · c new project"
	if m.section == radarSection && len(m.radar.OpenCollisions) > 0 {
		footer += " · x inspect collision"
	}
	if m.section == workSection {
		if m.status.Role == protocol.RoleOwner {
			footer += " · g connect repository"
		}
		footer += " · n publish Intent"
		if len(m.intents.Intents) > 0 {
			footer += " · i inspect Intents"
		}
	}
	if m.section == teamSection && m.status.Role == protocol.RoleOwner {
		footer += " · a add member"
	}
	if m.section == planSection {
		if m.status.Role == protocol.RoleOwner {
			footer += " · o objective · k import Kiro spec · b blueprint"
		}
		footer += " · d decide charter"
	}
	return footer + " · ? help · q quit"
}

func (m model) helpText() string {
	return "p opens the project picker, even when only one project is available. Project picker and form selectors are labeled separately. Owners can add existing Conn humans, create or import objectives, propose blueprints, and connect repositories. Assigned humans decide only their own pending charters. " +
		"In Work, this app publishes, revises, and releases Intents as your CLI participant; Intents published from Kiro belong to that Kiro participant. " +
		"In Radar, x opens a collision: c reads its context pack, p records a non-binding proposal, and d records a human decision. Binding decisions are owner-only. Escape backs out of the current form or review without writing. " +
		"PgUp and PgDn scroll a screen taller than the terminal; in sections and reviews, Up and Down scroll too. Account shows your Conn human ID and the project ID for conn commands."
}

func (m model) sectionView() string {
	switch m.section {
	case overviewSection:
		return m.overview()
	case planSection:
		return m.plan()
	case radarSection:
		return m.radarView()
	case workSection:
		return m.work()
	case teamSection:
		return m.team()
	case updatesSection:
		return m.updates()
	case accountSection:
		return m.account()
	}
	return ""
}

func (m model) overview() string {
	project := fmt.Sprintf("Project: %s\nYour role: %s", clean(m.selected.Project.Name), m.selected.Role)
	if len(m.status.Blueprints) == 0 {
		return project + "\n\nNo blueprint has been proposed for this project yet."
	}
	var lines []string
	for _, blueprint := range m.status.Blueprints {
		for _, revision := range blueprint.Revisions {
			accepted, pending, rejected := 0, 0, 0
			for _, cell := range revision.Cells {
				switch cell.Decision {
				case protocol.CellAccepted:
					accepted++
				case protocol.CellRejected:
					rejected++
				default:
					pending++
				}
			}
			lines = append(lines, fmt.Sprintf("%s · revision %d · %s\n  %d accepted · %d pending · %d rejected",
				blueprintLabel(m.objectiveTitle(revision.ObjectiveID)), revision.Revision, revision.Status, accepted, pending, rejected))
		}
	}
	return project + "\n\n" + strings.Join(lines, "\n\n") + fmt.Sprintf("\n\nProject state is current through event %d.", m.asOfEvent)
}

func (m model) plan() string {
	var lines []string
	if len(m.status.Objectives) == 0 {
		lines = append(lines, "No objectives have been created.")
	} else {
		lines = append(lines, "Objectives:")
		for _, objective := range m.status.Objectives {
			lines = append(lines, fmt.Sprintf("  %s · revision %d", m.objectiveTitle(objective.ObjectiveID), objective.CurrentRevision))
		}
	}
	if len(m.status.Blueprints) == 0 {
		lines = append(lines, "\nNo blueprints have been proposed.")
	} else {
		lines = append(lines, "\nBlueprints:")
		for _, blueprint := range m.status.Blueprints {
			lines = append(lines, "  "+m.blueprintName(blueprint.BlueprintID))
			for _, revision := range blueprint.Revisions {
				lines = append(lines, fmt.Sprintf("    revision %d · %s · divides objective revision %d", revision.Revision, revision.Status, revision.ObjectiveRevision))
				for _, cell := range revision.Cells {
					lines = append(lines, fmt.Sprintf("      [%s] %s · %s", cell.Decision, clean(cell.Title), m.memberName(cell.AccountableHumanID)))
				}
			}
		}
	}
	if m.status.Role == protocol.RoleOwner {
		lines = append(lines, "\nOwner actions: o create objective · k import Kiro spec · b propose blueprint")
	}
	lines = append(lines, "Charter action: d review and decide your own pending charter")
	return strings.Join(lines, "\n")
}

func (m model) radarView() string {
	lines := []string{fmt.Sprintf("Coordination state at event %d.", m.radar.AsOfEvent)}
	if len(m.radar.Objectives) == 0 {
		lines = append(lines, "\nObjectives: absent")
	} else {
		lines = append(lines, "\nObjectives:")
		for _, objective := range m.radar.Objectives {
			lines = append(lines, fmt.Sprintf("  %s · revision %d", clean(objective.Title), objective.Revision))
		}
	}
	if m.radar.ActiveBlueprint == nil {
		lines = append(lines, "\nActive blueprint: absent")
	} else {
		lines = append(lines, fmt.Sprintf("\nActive blueprint: %s, revision %d", m.blueprintName(m.radar.ActiveBlueprint.BlueprintID), m.radar.ActiveBlueprint.Revision))
	}
	lines = append(lines, fmt.Sprintf("\nRepositories: %d\nActive Intents: %d\nClaims: %d across %d concepts\nDependencies: %d\nDecisions: %d", len(m.radar.Repositories), len(m.radar.ActiveIntents), len(m.radar.SemanticClaims), len(m.radar.SemanticConcepts), len(m.radar.Dependencies), len(m.radar.DecisionRefs)))
	if len(m.radar.OpenCollisions) == 0 {
		lines = append(lines, "\nOpen collisions: none reported. This does not prove there are no conflicts.")
	} else {
		lines = append(lines, "\nOpen collisions (advisory):")
		for _, collision := range m.radar.OpenCollisions {
			lines = append(lines, fmt.Sprintf("  [%s] %s · confidence %.2f", collision.Severity, m.collisionName(collision), collision.Confidence))
		}
		lines = append(lines, "Press x to inspect collisions.")
	}
	if len(m.radar.EvidenceCoverage) == 0 {
		lines = append(lines, "\nEvidence coverage: absent (no keyed acceptance criteria)")
	} else {
		lines = append(lines, "\nEvidence coverage:")
		for _, coverage := range m.radar.EvidenceCoverage {
			lines = append(lines, fmt.Sprintf("  [%s] %s · %s · %d references", coverage.State, clean(coverage.AcceptanceKey), m.objectiveTitle(coverage.ObjectiveID), len(coverage.EvidenceRefs)))
		}
	}
	if len(m.radar.KnownUnknowns) > 0 {
		lines = append(lines, "\nKnown unknowns:")
		for _, unknown := range m.radar.KnownUnknowns {
			lines = append(lines, "  - "+clean(unknown))
		}
	}
	return strings.Join(lines, "\n")
}

func (m model) radarCollisionView() string {
	if len(m.radar.OpenCollisions) == 0 {
		return "No open collision assessment is available."
	}
	index := max(0, min(m.radarCollision, len(m.radar.OpenCollisions)-1))
	collision := m.radar.OpenCollisions[index]
	return fmt.Sprintf("Collision %d of %d\n\nRule: %s\nStatus: %s (advisory)\nSeverity: %s\nConfidence: %.2f\n\nIntents:\n  %s · revision %d\n  %s · revision %d\nMatching claims: %d\n\nExplanation:\n%s\n\nA human decision has not been recorded by this screen.",
		index+1, len(m.radar.OpenCollisions), clean(collision.RuleID), collision.State, collision.Severity, collision.Confidence,
		m.intentName(collision.LeftIntentID), collision.LeftIntentRevision, m.intentName(collision.RightIntentID), collision.RightIntentRevision,
		len(collision.MatchingClaimRefs), cleanBlock(collision.ExplanationMarkdown))
}

func (m model) team() string {
	if len(m.members.Members) == 0 {
		return "No members found."
	}
	var lines []string
	for _, member := range m.members.Members {
		you := ""
		if member.Human.HumanID == m.human.HumanID {
			you = " (you)"
		}
		lines = append(lines, fmt.Sprintf("@%s · %s%s", clean(member.Human.GitHubLogin), member.Membership.Role, you))
	}
	if m.status.Role == protocol.RoleOwner {
		lines = append(lines, "\nOwner action: a add an already authenticated member by their Conn human ID")
	}
	return strings.Join(lines, "\n")
}

func (m model) updates() string {
	if len(m.events) == 0 {
		return "No events have been recorded yet."
	}
	lines := []string{"Latest updates, oldest first:"}
	for _, event := range m.events {
		lines = append(lines, fmt.Sprintf("  %d · %s · %s · %s", event.EventSequence, m.describeEvent(event),
			m.memberName(event.ActorHumanID), event.OccurredAt.Time().Local().Format("2006-01-02 15:04")))
	}
	return strings.Join(lines, "\n") + fmt.Sprintf("\n\nProject state is current through event %d.", m.asOfEvent)
}

// account shows the only IDs the app displays: a person hands their human ID
// to an owner to be added to a project, and the conn commands take the
// project ID.
func (m model) account() string {
	text := fmt.Sprintf("Signed in as @%s\nServer: %s\n\nYour Conn human ID, for a project owner to add you:\n  %s",
		clean(m.human.GitHubLogin), m.client.URL(), m.human.HumanID)
	if m.selectedProjectID() != (uuid.UUID{}) {
		text += fmt.Sprintf("\nProject ID of %s, for conn commands:\n  %s", clean(m.selected.Project.Name), m.selectedProjectID())
	}
	return text + "\n\nRun conn doctor for detailed server and credential diagnostics."
}

func (m model) cellsReview() string {
	lines := []string{fmt.Sprintf("Objective: %s (revision %d)", m.objectiveTitle(m.blueprintObjective.ObjectiveID), m.blueprintObjective.CurrentRevision)}
	if m.blueprintExpected > 0 {
		lines = append(lines, fmt.Sprintf("Revising this objective's blueprint: these cells replace the whole cell set of revision %d as revision %d.",
			m.blueprintExpected, m.blueprintExpected+1))
	}
	if len(m.blueprintCells) == 0 {
		lines = append(lines, "\nNo cells yet. Press a to add one.")
	}
	for i, cell := range m.blueprintCells {
		marker := "  "
		if m.screen == cellMoreScreen && i == m.cellCursor {
			marker = cursorMarker
		}
		lines = append(lines, fmt.Sprintf("\n%sCell %d: %s\nAccountable human: %s\n%s", marker, i+1, clean(cell.Title), m.memberName(cell.AccountableHumanID), cleanBlock(cell.OverviewMarkdown)))
		if kept := keptFields(cell); kept != "" {
			lines = append(lines, "Kept as is (not editable here): "+kept)
		}
	}
	if m.formNote != "" {
		lines = append(lines, "\n"+m.formNote)
	}
	return strings.Join(lines, "\n")
}

// keptFields lists the charter fields the app carries into a revision
// without editing them.
func keptFields(cell protocol.CellCharter) string {
	var parts []string
	count := func(n int, singular, plural string) {
		if n == 1 {
			parts = append(parts, "1 "+singular)
		} else if n > 1 {
			parts = append(parts, fmt.Sprintf("%d %s", n, plural))
		}
	}
	count(len(cell.ObjectivePointRefs), "objective point reference", "objective point references")
	count(len(cell.BoundaryPoints), "boundary point", "boundary points")
	count(len(cell.Dependencies), "dependency", "dependencies")
	return strings.Join(parts, ", ")
}
func (m model) blueprintReview() string {
	review := m.cellsReview() + "\n\nThis creates a proposed, unapproved revision. Each assigned human must decide their own charter."
	if m.blueprintExpected > 0 {
		review += " A pending proposal of this blueprint is superseded; an active revision stays in force until every assigned human accepts the new one."
	}
	return review
}
func (m model) charterReview() string {
	for _, charter := range m.charterView.BlueprintRevision.CellCharters {
		if charter.CellID == m.selectedCharter.cell.CellID {
			return fmt.Sprintf("%s · revision %d\n\nCell: %s\n\nMarkdown overview:\n%s", blueprintLabel(m.selectedCharter.objective), m.selectedCharter.revision.Revision, clean(charter.Title), cleanBlock(charter.OverviewMarkdown))
		}
	}
	return "The selected charter could not be rendered."
}

func clean(value string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return '?'
		}
		return r
	}, value)
}

// cleanBlock is clean for multi-line text: it keeps line breaks and tabs.
func cleanBlock(value string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r == '\r':
			return -1
		case r == '\n', r == '\t':
			return r
		case unicode.IsControl(r):
			return '?'
		}
		return r
	}, value)
}

var (
	brandStyle     = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("39"))
	projectStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
	activeTabStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("39")).Underline(true)
	tabStyle       = lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
	cardStyle      = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("240")).Padding(1, 2)
	cardTitleStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("39"))
	footerStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
	helpStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("252"))
	errorStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("9"))
)
