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
	"github.com/charmbracelet/bubbles/textinput"
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
	program := tea.NewProgram(newModel(ctx, cfg.Client), tea.WithInput(cfg.In), tea.WithOutput(cfg.Out), tea.WithAltScreen(), tea.WithContext(ctx))
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
	teamSection
	updatesSection
	accountSection
)

var sections = []string{"Overview", "Plan", "Team", "Updates", "Account"}

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
)

type projectItem struct{ entry protocol.ProjectListEntry }

func (p projectItem) Title() string { return clean(p.entry.Project.Name) }
func (p projectItem) Description() string {
	return fmt.Sprintf("%s · %s", p.entry.Role, p.entry.Project.ProjectID)
}
func (p projectItem) FilterValue() string {
	return p.entry.Project.Name + " " + p.entry.Project.ProjectID.String()
}

type memberItem struct{ member protocol.ProjectMember }

func (i memberItem) Title() string { return "@" + clean(i.member.Human.GitHubLogin) }
func (i memberItem) Description() string {
	return fmt.Sprintf("%s · %s", i.member.Membership.Role, i.member.Human.HumanID)
}
func (i memberItem) FilterValue() string {
	return i.member.Human.GitHubLogin + " " + i.member.Human.HumanID.String()
}

type objectiveItem struct{ objective protocol.ObjectiveSummary }

func (i objectiveItem) Title() string { return fmt.Sprintf("Objective %s", i.objective.ObjectiveID) }
func (i objectiveItem) Description() string {
	return fmt.Sprintf("current revision %d", i.objective.CurrentRevision)
}
func (i objectiveItem) FilterValue() string { return i.objective.ObjectiveID.String() }

type charterItem struct {
	blueprintID uuid.UUID
	revision    protocol.RevisionSummary
	cell        protocol.CellSummary
}

func (i charterItem) Title() string { return clean(i.cell.Title) }
func (i charterItem) Description() string {
	return fmt.Sprintf("Blueprint %s · revision %d", i.blueprintID, i.revision.Revision)
}
func (i charterItem) FilterValue() string { return i.cell.Title + " " + i.cell.CellID.String() }

type projectsLoaded struct {
	client   *httpclient.Client
	human    protocol.Human
	projects protocol.ProjectList
	err      error
}

type projectLoaded struct {
	projectID uuid.UUID
	status    protocol.ProjectStatus
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

type model struct {
	clientFactory func() (*httpclient.Client, error)
	ctx           context.Context
	client        *httpclient.Client
	human         protocol.Human
	projects      protocol.ProjectList
	selected      protocol.ProjectListEntry
	status        protocol.ProjectStatus
	members       protocol.ProjectMemberList
	events        []protocol.EventEnvelope
	asOfEvent     int64

	projectList list.Model
	selector    list.Model
	input       textinput.Model
	section     section
	screen      screen
	choosing    bool
	loading     bool
	help        bool
	err         error
	notice      string
	width       int
	height      int

	projectName        string
	memberID           uuid.UUID
	objective          protocol.ObjectiveRevisionInput
	objectiveMember    protocol.ProjectMember
	blueprintObjective protocol.ObjectiveSummary
	blueprintID        uuid.UUID
	blueprintCells     []protocol.CellCharter
	cell               protocol.CellCharter
	selectedCharter    charterItem
	charterView        protocol.BlueprintRevisionView
	charterOutcome     protocol.CharterOutcome
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
	return model{ctx: ctx, clientFactory: clientFactory, projectList: projects, selector: selector, input: input, loading: true, width: 80, height: 24}
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

func loadProject(ctx context.Context, client *httpclient.Client, projectID uuid.UUID) tea.Cmd {
	return func() tea.Msg {
		status, err := client.Status(ctx, projectID)
		if err != nil {
			return projectLoaded{projectID: projectID, err: err}
		}
		members, err := client.ProjectMembers(ctx, projectID)
		if err != nil {
			return projectLoaded{projectID: projectID, err: err}
		}
		events, err := client.Events(ctx, projectID, 0, 20)
		if err != nil {
			return projectLoaded{projectID: projectID, err: err}
		}
		return projectLoaded{projectID: projectID, status: status.Data, members: members.Data, events: events.Data, asOfEvent: status.AsOfEvent}
	}
}

func createProject(ctx context.Context, client *httpclient.Client, name string) tea.Cmd {
	return func() tea.Msg {
		result, err := client.CreateProject(ctx, protocol.ProjectCreate{ProjectID: uuid.NewV7(), Name: strings.TrimSpace(name)})
		if err != nil {
			return projectCreated{err: err}
		}
		return projectCreated{project: result.Data}
	}
}

func (m model) selectedProjectID() uuid.UUID { return m.selected.Project.ProjectID }

func (m model) startInput(next screen, prompt, placeholder string, value string) model {
	m.screen, m.choosing, m.help, m.err = next, false, false, nil
	m.input.Prompt, m.input.Placeholder = prompt, placeholder
	m.input.SetValue(value)
	m.input.Focus()
	return m
}

func (m model) startProjectCreate() model {
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

func (m model) startBlueprint() model {
	m.blueprintID, m.blueprintCells, m.cell = uuid.NewV7(), nil, protocol.CellCharter{}
	items := make([]list.Item, 0, len(m.status.Objectives))
	for _, objective := range m.status.Objectives {
		items = append(items, objectiveItem{objective})
	}
	m.selector.Title = "Objective selector for this blueprint"
	m.selector.SetItems(items)
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
					items = append(items, charterItem{blueprint.BlueprintID, revision, cell})
				}
			}
		}
	}
	m.selector.Title = "Pending charter selector"
	m.selector.SetItems(items)
	m.screen, m.err = charterPickScreen, nil
	return m
}

func (m model) chooseMember(next screen, title string) model {
	items := make([]list.Item, 0, len(m.members.Members))
	for _, member := range m.members.Members {
		items = append(items, memberItem{member})
	}
	m.selector.Title = title
	m.selector.SetItems(items)
	m.screen, m.err = next, nil
	return m
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.projectList.SetSize(max(30, msg.Width-4), max(8, msg.Height-7))
		m.selector.SetSize(max(30, msg.Width-4), max(8, msg.Height-7))
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
		m.loading, m.err = false, msg.err
		if msg.err != nil {
			return m, nil
		}
		m.createdProject, m.loading, m.notice = msg.project.ProjectID, true, "Project created."
		return m, loadProjects(m.ctx, m.clientFactory)
	case projectLoaded:
		m.loading, m.err = false, msg.err
		if msg.err == nil {
			m.status, m.members, m.events, m.asOfEvent = msg.status, msg.members, msg.events, msg.asOfEvent
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
		m.loading, m.err = false, msg.err
		if msg.err != nil {
			return m, nil
		}
		m.notice, m.screen, m.loading = msg.message, normalScreen, true
		return m, loadProject(m.ctx, m.client, m.selectedProjectID())
	case tea.KeyMsg:
		if msg.String() == "ctrl+c" {
			return m, tea.Quit
		}
		if m.loading {
			return m, nil
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
		m.section = teamSection
	case "4":
		m.section = updatesSection
	case "5":
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
			return m.startInput(objectiveTitleScreen, "Objective title: ", "e.g. Deliver checkout reliability", m.objective.Title), nil
		case objectiveMemberScreen:
			return m.startInput(objectiveOverviewScreen, "Markdown overview: ", "Describe the objective", m.objective.OverviewMarkdown), nil
		case objectiveConfirmScreen:
			return m.chooseMember(objectiveMemberScreen, "Accountable member selector"), nil
		case cellMemberScreen:
			m.screen = blueprintObjectiveScreen
			return m, nil
		case cellTitleScreen:
			return m.chooseMember(cellMemberScreen, "Accountable member selector for this cell"), nil
		case cellOverviewScreen:
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
			m.screen, m.loading, m.err = normalScreen, true, nil
			return m, createProject(m.ctx, m.client, m.projectName)
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
			return m.startInput(objectiveOverviewScreen, "Markdown overview: ", "Describe the objective", ""), nil
		}
	case objectiveOverviewScreen:
		if key == "enter" {
			if strings.TrimSpace(m.input.Value()) == "" {
				m.err = errors.New("Enter a Markdown overview before continuing.")
				return m, nil
			}
			m.objective.OverviewMarkdown = strings.TrimSpace(m.input.Value())
			return m.chooseMember(objectiveMemberScreen, "Accountable member selector"), nil
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
				m.blueprintObjective = item.objective
				return m.chooseMember(cellMemberScreen, "Accountable member selector for cell 1"), nil
			}
		}
		return m.updateSelector(msg)
	case cellMemberScreen:
		if key == "enter" {
			if item, ok := m.selector.SelectedItem().(memberItem); ok {
				m.cell.AccountableHumanID = item.member.Human.HumanID
				return m.startInput(cellTitleScreen, "Cell title: ", "e.g. Documentation", ""), nil
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
			return m.startInput(cellOverviewScreen, "Cell Markdown overview: ", "Describe this human's boundary", ""), nil
		}
	case cellOverviewScreen:
		if key == "enter" {
			if strings.TrimSpace(m.input.Value()) == "" {
				m.err = errors.New("Enter a cell Markdown overview before continuing.")
				return m, nil
			}
			m.cell.CellID, m.cell.OverviewMarkdown = uuid.NewV7(), strings.TrimSpace(m.input.Value())
			m.blueprintCells = append(m.blueprintCells, m.cell)
			m.cell, m.screen, m.err = protocol.CellCharter{}, cellMoreScreen, nil
			return m, nil
		}
	case cellMoreScreen:
		if key == "a" {
			return m.chooseMember(cellMemberScreen, fmt.Sprintf("Accountable member selector for cell %d", len(m.blueprintCells)+1)), nil
		}
		if key == "enter" {
			m.screen = blueprintConfirmScreen
			return m, nil
		}
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
	m.input, cmd = m.input.Update(msg)
	m.err = nil
	return m, cmd
}

func (m model) updateSelector(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	m.selector, cmd = m.selector.Update(msg)
	return m, cmd
}

func (m model) submitAddMember() (tea.Model, tea.Cmd) {
	m.screen, m.loading, m.err = normalScreen, true, nil
	return m, func() tea.Msg {
		_, err := m.client.AddMember(m.ctx, m.selectedProjectID(), m.memberID)
		return actionDone{"Member added.", err}
	}
}

func (m model) submitObjective() (tea.Model, tea.Cmd) {
	m.screen, m.loading, m.err = normalScreen, true, nil
	input := m.objective
	return m, func() tea.Msg {
		_, err := m.client.CreateObjective(m.ctx, m.selectedProjectID(), input)
		return actionDone{"Objective created.", err}
	}
}

func (m model) submitBlueprint() (tea.Model, tea.Cmd) {
	proposal := protocol.BlueprintProposal{BlueprintID: m.blueprintID, ObjectiveID: m.blueprintObjective.ObjectiveID, ObjectiveRevision: m.blueprintObjective.CurrentRevision, CellCharters: m.blueprintCells}
	m.screen, m.loading, m.err = normalScreen, true, nil
	return m, func() tea.Msg {
		_, err := m.client.ProposeBlueprint(m.ctx, m.selectedProjectID(), proposal)
		return actionDone{"Blueprint proposal created. Assigned humans must decide their own charters.", err}
	}
}

func (m model) submitCharterDecision() (tea.Model, tea.Cmd) {
	item, outcome, rationale := m.selectedCharter, m.charterOutcome, m.rejectionRationale
	m.screen, m.loading, m.err = normalScreen, true, nil
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

func (m model) View() string {
	if m.loading {
		return m.header() + "\n\n" + m.card("Loading", "Loading shared team state...") + "\n"
	}
	if m.screen != normalScreen {
		return m.formView()
	}
	if m.err != nil {
		return m.header() + "\n\n" + m.card("Could not complete that action", errorStyle.Render(clean(m.err.Error()))) + "\n\n" + footerStyle.Render("  Esc back · r refresh · q quit") + "\n"
	}
	if m.choosing {
		return m.header() + "\n\n" + m.projectList.View() + "\n" + footerStyle.Render("  Project picker: Up/down select · / filter · Enter open · c new project · Esc back · q quit") + "\n"
	}
	if len(m.projects.Projects) == 0 {
		return m.header() + "\n\n" + m.card("Start your first project", "No projects are available to this Conn account.\n\nPress c or Enter to create a project here.") + "\n\n" + footerStyle.Render("  c or Enter create project · r refresh · q quit") + "\n"
	}
	body := m.card(sections[m.section], m.sectionView())
	if m.notice != "" {
		body += "\n\n" + helpStyle.Render(clean(m.notice))
	}
	if m.help {
		body += "\n\n" + helpStyle.Render(m.helpText())
	}
	return m.header() + "\n\n" + body + "\n\n" + footerStyle.Render(m.normalFooter()) + "\n"
}

func (m model) formView() string {
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
		title, body, footer = "Create objective: Markdown overview", m.input.View(), "  Enter choose accountable member · Esc cancel"
	case objectiveMemberScreen:
		return m.header() + "\n\n" + m.selector.View() + "\n" + footerStyle.Render("  Form selector: Up/down select · / filter · Enter choose · Esc cancel") + "\n"
	case objectiveConfirmScreen:
		title, body, footer = "Create this objective?", fmt.Sprintf("Title: %s\n\nMarkdown overview:\n%s\n\nAccountable human: @%s · %s", clean(m.objective.Title), clean(m.objective.OverviewMarkdown), clean(m.objectiveMember.Human.GitHubLogin), m.objective.AccountableHumanID), "  Enter create objective · Esc edit member"
	case blueprintObjectiveScreen:
		return m.header() + "\n\n" + m.selector.View() + "\n" + footerStyle.Render("  Form selector: choose current objective · Enter choose · Esc cancel") + "\n"
	case cellMemberScreen:
		return m.header() + "\n\n" + m.selector.View() + "\n" + footerStyle.Render("  Form selector: choose accountable member · Enter choose · Esc cancel") + "\n"
	case cellTitleScreen:
		title, body, footer = "Add cell: title", m.input.View(), "  Enter next · Esc cancel blueprint"
	case cellOverviewScreen:
		title, body, footer = "Add cell: Markdown overview", m.input.View(), "  Enter add cell · Esc cancel blueprint"
	case cellMoreScreen:
		title, body, footer = "Blueprint cells", m.cellsReview(), "  a add another cell · Enter review proposal · Esc cancel"
	case blueprintConfirmScreen:
		title, body, footer = "Propose this blueprint?", m.blueprintReview(), "  Enter propose blueprint · Esc edit cells"
	case charterPickScreen:
		if len(m.selector.Items()) == 0 {
			title, body, footer = "No pending charter", "You have no assigned pending charters in a proposed blueprint.", "  Esc back"
		} else {
			return m.header() + "\n\n" + m.selector.View() + "\n" + footerStyle.Render("  Charter selector: Up/down select · / filter · Enter inspect · Esc back") + "\n"
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
	return m.header() + "\n\n" + m.card(title, body) + "\n\n" + footerStyle.Render(footer) + "\n"
}

func (m model) normalFooter() string {
	footer := "  Tab sections · Enter refresh · p project picker · c new project"
	if m.section == teamSection && m.status.Role == protocol.RoleOwner {
		footer += " · a add member"
	}
	if m.section == planSection {
		if m.status.Role == protocol.RoleOwner {
			footer += " · o objective · b blueprint"
		}
		footer += " · d decide charter"
	}
	return footer + " · ? help · q quit"
}

func (m model) helpText() string {
	return "p opens the project picker, even when only one project is available. Project picker and form selectors are labeled separately. Owners can add existing Conn humans, create objectives, and propose blueprints. Assigned humans decide only their own pending charters. Escape backs out of the current form or review without writing."
}

func (m model) sectionView() string {
	switch m.section {
	case overviewSection:
		return m.overview()
	case planSection:
		return m.plan()
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
	project := fmt.Sprintf("Project ID: %s\nRole: %s", m.selectedProjectID(), m.selected.Role)
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
			lines = append(lines, fmt.Sprintf("Blueprint %s · revision %d · %s\n  %d accepted · %d pending · %d rejected", blueprint.BlueprintID, revision.Revision, revision.Status, accepted, pending, rejected))
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
			lines = append(lines, fmt.Sprintf("  %s · revision %d", objective.ObjectiveID, objective.CurrentRevision))
		}
	}
	if len(m.status.Blueprints) == 0 {
		lines = append(lines, "\nNo blueprints have been proposed.")
	} else {
		lines = append(lines, "\nBlueprints:")
		for _, blueprint := range m.status.Blueprints {
			for _, revision := range blueprint.Revisions {
				lines = append(lines, fmt.Sprintf("  revision %d · %s · objective revision %d", revision.Revision, revision.Status, revision.ObjectiveRevision))
				for _, cell := range revision.Cells {
					lines = append(lines, fmt.Sprintf("    [%s] %s · %s", cell.Decision, clean(cell.Title), cell.AccountableHumanID))
				}
			}
		}
	}
	if m.status.Role == protocol.RoleOwner {
		lines = append(lines, "\nOwner actions: o create objective · b propose blueprint")
	}
	lines = append(lines, "Charter action: d review and decide your own pending charter")
	return strings.Join(lines, "\n")
}

func (m model) team() string {
	if len(m.members.Members) == 0 {
		return "Team\n\nNo members found."
	}
	lines := []string{"Team:"}
	for _, member := range m.members.Members {
		lines = append(lines, fmt.Sprintf("  @%s · %s · %s", clean(member.Human.GitHubLogin), member.Membership.Role, member.Human.HumanID))
	}
	if m.status.Role == protocol.RoleOwner {
		lines = append(lines, "\nOwner action: a add an already authenticated member")
	}
	return strings.Join(lines, "\n")
}

func (m model) updates() string {
	if len(m.events) == 0 {
		return "Updates\n\nNo events have been recorded yet."
	}
	lines := []string{"Recent updates:"}
	for _, event := range m.events {
		lines = append(lines, fmt.Sprintf("  %d · %s · %s", event.EventSequence, event.EventType(), event.OccurredAt))
	}
	return strings.Join(lines, "\n")
}
func (m model) account() string {
	return fmt.Sprintf("Account\n\nSigned in as @%s\nConn human ID: %s\nServer: %s\n\nRun conn doctor for detailed server and credential diagnostics.", clean(m.human.GitHubLogin), m.human.HumanID, m.client.URL())
}
func (m model) cellsReview() string {
	lines := []string{fmt.Sprintf("Objective: %s revision %d", m.blueprintObjective.ObjectiveID, m.blueprintObjective.CurrentRevision)}
	for i, cell := range m.blueprintCells {
		lines = append(lines, fmt.Sprintf("\nCell %d: %s\nAccountable human: %s\n%s", i+1, clean(cell.Title), cell.AccountableHumanID, clean(cell.OverviewMarkdown)))
	}
	return strings.Join(lines, "\n")
}
func (m model) blueprintReview() string {
	return m.cellsReview() + "\n\nThis creates a proposed, unapproved revision. Each assigned human must decide their own charter."
}
func (m model) charterReview() string {
	for _, charter := range m.charterView.BlueprintRevision.CellCharters {
		if charter.CellID == m.selectedCharter.cell.CellID {
			return fmt.Sprintf("Blueprint %s · revision %d\n\nCell: %s\n\nMarkdown overview:\n%s", m.selectedCharter.blueprintID, m.selectedCharter.revision.Revision, clean(charter.Title), clean(charter.OverviewMarkdown))
		}
	}
	return "The selected charter could not be rendered."
}

func (m model) card(title, body string) string {
	width := min(100, max(34, m.width-6))
	return cardStyle.Copy().Width(width).Render(cardTitleStyle.Render(title) + "\n\n" + body)
}
func (m model) header() string {
	project := "No project selected"
	if m.selectedProjectID() != (uuid.UUID{}) {
		project = clean(m.selected.Project.Name) + " · " + string(m.selected.Role)
	}
	tabs := make([]string, len(sections))
	for i, name := range sections {
		label := fmt.Sprintf("%d %s", i+1, name)
		if section(i) == m.section {
			tabs[i] = activeTabStyle.Render(label)
		} else {
			tabs[i] = tabStyle.Render(label)
		}
	}
	return brandStyle.Render("CONN") + "  " + projectStyle.Render(project) + "\n" + strings.Join(tabs, " ")
}
func clean(value string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
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
