package tui

// The semantic coordination flows of conn app: repository connections,
// Intents, local analysis, evidence, collision context, resolution proposals
// and decisions, and Kiro specification imports. Every write goes through the
// authenticated HTTP client; this app acts as the human's CLI participant.

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"
	"uuid"

	"github.com/atotto/clipboard"
	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"

	localanalyzer "github.com/Juanqu261/conn/internal/analyzer"
	"github.com/Juanqu261/conn/internal/application"
	"github.com/Juanqu261/conn/internal/kirospec"
	"github.com/Juanqu261/conn/internal/protocol"
	"github.com/Juanqu261/conn/internal/repositorygit"
)

// defaultAnalyzerScript matches the conn intent analyze default, resolved
// from the directory conn app was started in.
const defaultAnalyzerScript = "packages/typescript-analyzer/dist/cli.js"

// semanticDraft holds the in-memory drafts and focused reads of the semantic
// flows. Like the other forms, nothing is written until a review confirms it.
type semanticDraft struct {
	repository protocol.RepositoryConnectInput

	intent          protocol.IntentInput
	intentExpected  int // the current revision a revise replaces, or 0 to publish
	assumptionsText string
	claimsText      string
	charters        []charterItem

	evidence     protocol.EvidenceAttachmentInput
	evidenceKeys []protocol.KeyPoint

	analysisIntentID uuid.UUID
	analysisRemote   string
	analysisWorktree string
	analysisScript   string
	analysisPaths    []string

	proposalsFor     uuid.UUID
	proposals        []protocol.ResolutionProposal
	proposalsLoading bool
	proposalsErr     error
	contextFor       uuid.UUID
	contextText      string

	proposal       protocol.ResolutionProposalInput
	referencesText string

	decision         protocol.ResolutionDecisionInput
	decisionProposal *protocol.ResolutionProposal

	importPath     string
	importInput    protocol.ObjectiveImportInput
	importExpected int
	importHeading  string // suggested from the specification's first heading
	importKept     []protocol.KeyPoint
	criteriaText   string
	importMember   protocol.ProjectMember
}

type proposalsLoaded struct {
	collisionID uuid.UUID
	proposals   []protocol.ResolutionProposal
	err         error
}

type contextLoaded struct {
	collisionID uuid.UUID
	pack        protocol.ConflictContextPack
	err         error
}

type evidenceKeysLoaded struct {
	keys []protocol.KeyPoint
	err  error
}

type importBaseLoaded struct {
	objective protocol.Objective
	err       error
}

// choiceItem is a form-selector option with a stable value.
type choiceItem struct {
	title, description, value string
}

func (c choiceItem) Title() string       { return c.title }
func (c choiceItem) Description() string { return c.description }
func (c choiceItem) FilterValue() string { return c.title + " " + c.value }

var (
	claimKinds = []string{"behavior", "invariant", "contract", "data_resource", "infrastructure_resource",
		"code_symbol", "dependency", "assumption", "question", "acceptance_criterion"}
	claimRelations = []string{"affects", "changes", "consumes", "produces", "depends_on", "removes", "verifies", "assumes", "questions"}
	// common#/$defs/semanticSubject and common#/$defs/key.
	subjectPattern = regexp.MustCompile(`^[a-z][a-z0-9_-]*(?::[A-Za-z0-9._~/-]+)+$`)
	keyPattern     = regexp.MustCompile(`^[a-z][a-z0-9]*(-[a-z0-9]+)*$`)

	resolutionKinds = []choiceItem{
		{"dependency", "One Intent waits for the other.", "dependency"},
		{"sequencing", "Agree the order in which the changes land.", "sequencing"},
		{"contract_agreement", "Agree a contract both Intents follow. Binding, owner-only decision.", "contract_agreement"},
		{"accepted_risk", "Accept the risk of both changes. Binding, owner-only decision.", "accepted_risk"},
		{"false_positive", "The rule matched, but the work does not interact.", "false_positive"},
		{"revise_objective", "Escalate to an Objective revision. Binding, owner-only decision.", "revise_objective"},
		{"revise_blueprint", "Escalate to a Blueprint revision. Binding, owner-only decision.", "revise_blueprint"},
		{"other", "Another resolution, explained in the rationale.", "other"},
	}
	evidenceKinds  = []string{"commit", "pull_request", "ci_run", "test_case", "trace", "artifact", "decision", "external_link"}
	evidenceStates = []string{"passing", "failing", "missing", "stale", "inaccessible", "unknown"}
)

// bindingKind reports a resolution kind that is always a binding decision.
func bindingKind(kind string) bool {
	return kind == "contract_agreement" || kind == "accepted_risk" || kind == "revise_objective" || kind == "revise_blueprint"
}

func semanticScreen(s screen) bool { return s >= radarCollisionScreen }

func (m model) owner() bool { return m.status.Role == protocol.RoleOwner }

func (m model) currentIntent() (protocol.IntentView, bool) {
	if len(m.intents.Intents) == 0 {
		return protocol.IntentView{}, false
	}
	return m.intents.Intents[max(0, min(m.intentCursor, len(m.intents.Intents)-1))], true
}

func (m model) currentCollision() (protocol.CollisionAssessment, bool) {
	if len(m.radar.OpenCollisions) == 0 {
		return protocol.CollisionAssessment{}, false
	}
	return m.radar.OpenCollisions[max(0, min(m.radarCollision, len(m.radar.OpenCollisions)-1))], true
}

// choose opens the form selector with items, selecting the one whose value is
// selected, if any.
func (m model) choose(next screen, title string, items []choiceItem, selected string) model {
	listed := make([]list.Item, len(items))
	for i, item := range items {
		listed[i] = item
	}
	m.selector.Title = title
	m.selector.ResetFilter()
	m.selector.SetItems(listed)
	m.selector.ResetSelected()
	for i, item := range items {
		if item.value == selected {
			m.selector.Select(i)
		}
	}
	m.screen, m.err = next, nil
	return m
}

func (m model) chosen() (choiceItem, bool) {
	item, ok := m.selector.SelectedItem().(choiceItem)
	return item, ok
}

// startSemanticAction handles the normal-screen keys of the semantic flows.
func (m model) startSemanticAction(key string) (tea.Model, tea.Cmd) {
	switch {
	case key == "g" && m.section == workSection && m.owner():
		return m.startRepositoryConnect(), nil
	case key == "n" && m.section == workSection:
		if len(m.radar.Objectives) == 0 {
			m.err = errors.New("Create or import an objective before publishing an Intent.")
			return m, nil
		}
		m.semantic.intent, m.semantic.intentExpected = protocol.IntentInput{IntentID: uuid.NewV7()}, 0
		m.semantic.assumptionsText, m.semantic.claimsText = "", ""
		return m.chooseIntentObjective(), nil
	case key == "i" && m.section == workSection && len(m.intents.Intents) > 0:
		m.screen, m.err = intentListScreen, nil
		return m, nil
	case key == "k" && m.section == planSection && m.owner():
		m.semantic.importInput = protocol.ObjectiveImportInput{ObjectiveID: uuid.NewV7(), SourceKind: "kiro_spec"}
		m.semantic.importExpected, m.semantic.importKept, m.semantic.criteriaText = 0, nil, ""
		return m.startInput(importSourceScreen, "Kiro specification: ", ".kiro/specs/<name>/requirements.md", m.semantic.importPath), nil
	}
	return m, nil
}

func (m model) startRepositoryConnect() model {
	m.semantic.repository = protocol.RepositoryConnectInput{RepositoryID: uuid.NewV7(), AnalysisEnabled: true}
	suggestion := ""
	if metadata, err := repositorygit.Discover(m.ctx, "."); err == nil {
		suggestion = metadata.CanonicalRemote
	}
	return m.startInput(repoRemoteScreen, "Repository remote: ", "github.com/owner/repository or a Git remote URL", suggestion)
}

// loadProposals reads the non-binding proposals of the collision on screen.
func (m model) loadProposals() (model, tea.Cmd) {
	collision, ok := m.currentCollision()
	if !ok || m.client == nil {
		return m, nil
	}
	m.semantic.proposalsFor, m.semantic.proposals, m.semantic.proposalsErr, m.semantic.proposalsLoading = collision.CollisionID, nil, nil, true
	client, ctx, projectID := m.client, m.ctx, m.selectedProjectID()
	return m, func() tea.Msg {
		result, err := client.ResolutionProposals(ctx, projectID, collision.CollisionID)
		return proposalsLoaded{collisionID: collision.CollisionID, proposals: result.Data.Proposals, err: err}
	}
}

func (m model) updateSemanticMessage(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case proposalsLoaded:
		if msg.collisionID == m.semantic.proposalsFor {
			m.semantic.proposals, m.semantic.proposalsErr, m.semantic.proposalsLoading = msg.proposals, msg.err, false
		}
	case contextLoaded:
		m.loading, m.err = false, msg.err
		if msg.err == nil {
			m.semantic.contextFor = msg.collisionID
			m.semantic.contextText = application.RenderConflictContextMarkdown(msg.pack)
			m.viewer.SetContent(cleanBlock(m.semantic.contextText))
			m.viewer.GotoTop()
			m.screen, m.formNote = contextScreen, ""
		}
	case evidenceKeysLoaded:
		m.loading, m.err = false, msg.err
		if msg.err == nil {
			m.semantic.evidenceKeys = msg.keys
			return m.chooseEvidenceKey(), nil
		}
	case importBaseLoaded:
		m.loading, m.err = false, msg.err
		if msg.err == nil {
			objective := msg.objective
			m.semantic.importInput.Key, m.semantic.importInput.OverviewMarkdown = objective.Key, objective.OverviewMarkdown
			m.semantic.importInput.AccountableHumanID = objective.AccountableHumanID
			m.semantic.importKept, m.semantic.criteriaText = nil, ""
			var criteria []string
			for _, point := range objective.KeyPoints {
				if point.Kind == protocol.KeyPointAcceptanceCriterion {
					criteria = append(criteria, point.Key+": "+point.Statement)
				} else {
					m.semantic.importKept = append(m.semantic.importKept, point)
				}
			}
			m.semantic.criteriaText = strings.Join(criteria, "\n")
			return m.startInput(importTitleScreen, "Objective title: ", "Reviewed title", objective.Title), nil
		}
	}
	return m, nil
}

// semanticBack is Escape: back one step, keeping the draft, or out of a flow.
func (m model) semanticBack() (tea.Model, tea.Cmd) {
	d := &m.semantic
	switch m.screen {
	case repoConfirmScreen:
		return m.startInput(repoRemoteScreen, "Repository remote: ", "github.com/owner/repository or a Git remote URL", d.repository.CanonicalRemote), nil
	case intentObjectiveScreen:
		if d.intentExpected > 0 {
			m.screen, m.err = intentListScreen, nil
			return m, nil
		}
	case intentCharterScreen:
		return m.chooseIntentObjective(), nil
	case intentRepositoryScreen:
		return m.chooseIntentCharterOrBack(), nil
	case intentSummaryScreen:
		d.intent.SummaryMarkdown = m.area.Value()
		if len(m.radar.Repositories) > 0 {
			return m.chooseIntentRepository(), nil
		}
		return m.chooseIntentCharterOrBack(), nil
	case intentAssumptionsScreen:
		d.assumptionsText = m.area.Value()
		return m.startInput(intentSummaryScreen, "", "Describe the intended change in Markdown", d.intent.SummaryMarkdown), nil
	case intentClaimsScreen:
		d.claimsText = m.area.Value()
		return m.startInput(intentAssumptionsScreen, "", "One assumption per line (optional)", d.assumptionsText), nil
	case intentConfirmScreen:
		return m.startInput(intentClaimsScreen, "", claimsPlaceholder, d.claimsText), nil
	case intentReleaseScreen, evidenceKeyScreen, analysisWorktreeScreen:
		m.screen, m.err = intentListScreen, nil
		return m, nil
	case evidenceKindScreen:
		return m.chooseEvidenceKey(), nil
	case evidenceStateScreen:
		return m.chooseEvidenceKind(), nil
	case evidenceLocatorScreen:
		return m.chooseEvidenceState(), nil
	case evidenceSummaryScreen:
		return m.startInput(evidenceLocatorScreen, "Locator: ", "commit SHA, CI run URL, or opaque test ID", d.evidence.Locator), nil
	case evidenceConfirmScreen:
		return m.startInput(evidenceSummaryScreen, "Summary: ", "optional one-line summary", d.evidence.Summary), nil
	case analysisScriptScreen:
		return m.startInput(analysisWorktreeScreen, "Worktree: ", "local repository worktree", d.analysisWorktree), nil
	case analysisPathsScreen:
		return m.startInput(analysisScriptScreen, "Analyzer script: ", defaultAnalyzerScript, d.analysisScript), nil
	case analysisConfirmScreen:
		return m.startInput(analysisPathsScreen, "Paths: ", "optional comma-separated repository-relative paths", strings.Join(d.analysisPaths, ", ")), nil
	case contextScreen, proposalKindScreen, decisionProposalScreen:
		m.screen, m.err, m.formNote = radarCollisionScreen, nil, ""
		return m, nil
	case proposalRationaleScreen:
		d.proposal.RationaleMarkdown = m.area.Value()
		return m.choose(proposalKindScreen, "Resolution kind selector for a non-binding proposal", resolutionKinds, d.proposal.Kind), nil
	case proposalReferencesScreen:
		d.referencesText = m.input.Value()
		return m.startInput(proposalRationaleScreen, "", "Explain the proposal", d.proposal.RationaleMarkdown), nil
	case proposalConfirmScreen:
		return m.startInput(proposalReferencesScreen, "References: ", "optional comma-separated note or link references", d.referencesText), nil
	case decisionKindScreen:
		return m.chooseDecisionProposal(), nil
	case decisionBindingScreen, decisionRationaleScreen:
		if m.screen == decisionRationaleScreen {
			d.decision.RationaleMarkdown = m.area.Value()
		}
		if m.screen == decisionRationaleScreen && !bindingKind(d.decision.Kind) {
			return m.chooseDecisionBinding(), nil
		}
		if d.decisionProposal != nil {
			return m.chooseDecisionProposal(), nil
		}
		return m.choose(decisionKindScreen, "Resolution kind selector for this decision", resolutionKinds, d.decision.Kind), nil
	case decisionReferencesScreen:
		d.referencesText = m.input.Value()
		return m.startInput(decisionRationaleScreen, "", "Explain the decision", d.decision.RationaleMarkdown), nil
	case decisionConfirmScreen:
		return m.startInput(decisionReferencesScreen, "References: ", "optional comma-separated meeting, note, or link references", d.referencesText), nil
	case importTargetScreen:
		return m.startInput(importSourceScreen, "Kiro specification: ", ".kiro/specs/<name>/requirements.md", d.importPath), nil
	case importTitleScreen:
		return m.chooseImportTarget(), nil
	case importOverviewScreen:
		d.importInput.OverviewMarkdown = m.area.Value()
		return m.startInput(importTitleScreen, "Objective title: ", "Reviewed title", d.importInput.Title), nil
	case importCriteriaScreen:
		d.criteriaText = m.area.Value()
		return m.startInput(importOverviewScreen, "", "Reviewed objective summary in Markdown", d.importInput.OverviewMarkdown), nil
	case importMemberScreen:
		return m.startInput(importCriteriaScreen, "", criteriaPlaceholder, d.criteriaText), nil
	case importConfirmScreen:
		return m.chooseMember(importMemberScreen, "Accountable member selector", d.importInput.AccountableHumanID), nil
	}
	m.screen, m.err, m.formNote = normalScreen, nil, ""
	return m, nil
}

const (
	claimsPlaceholder   = "One declared claim per line: kind relation subject [display name]"
	criteriaPlaceholder = "Optional acceptance criteria, one per line: key: statement"
)

func (m model) updateSemanticScreen(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := msg.String()
	if key == "esc" {
		return m.semanticBack()
	}
	d := &m.semantic
	switch m.screen {
	case radarCollisionScreen:
		switch key {
		case "up", "k":
			m.radarCollision = max(0, m.radarCollision-1)
			return m.loadProposals()
		case "down", "j":
			m.radarCollision = min(len(m.radar.OpenCollisions)-1, m.radarCollision+1)
			return m.loadProposals()
		case "c":
			return m.loadContext()
		case "p":
			if _, ok := m.currentCollision(); ok {
				d.proposal, d.referencesText = protocol.ResolutionProposalInput{}, ""
				return m.choose(proposalKindScreen, "Resolution kind selector for a non-binding proposal", resolutionKinds, ""), nil
			}
		case "d":
			if _, ok := m.currentCollision(); ok {
				d.decision, d.decisionProposal, d.referencesText = protocol.ResolutionDecisionInput{}, nil, ""
				return m.chooseDecisionProposal(), nil
			}
		}
		return m, nil

	case contextScreen:
		switch key {
		case "y":
			if err := clipboard.WriteAll(m.semantic.contextText); err != nil {
				m.err = fmt.Errorf("Could not copy to the clipboard: %v. Press w to save a file instead.", err)
			} else {
				m.err, m.formNote = nil, "Copied the context pack Markdown. Paste it into the agent session you choose."
			}
			return m, nil
		case "w":
			name := fmt.Sprintf("conn-context-%s.md", m.semantic.contextFor.String()[:8])
			if err := os.WriteFile(name, []byte(m.semantic.contextText), 0o600); err != nil {
				m.err = fmt.Errorf("Could not save the context pack: %v", err)
			} else {
				absolute, _ := filepath.Abs(name)
				m.err, m.formNote = nil, "Saved the context pack Markdown to "+absolute
			}
			return m, nil
		}
		var cmd tea.Cmd
		m.viewer, cmd = m.viewer.Update(msg)
		return m, cmd

	case repoRemoteScreen:
		if key == "enter" {
			metadata, err := repositorygit.NormalizeRemote(m.input.Value())
			if err != nil {
				m.err = errors.New("Enter a repository such as github.com/owner/repository or a Git remote URL without a port, query, or credentials.")
				return m, nil
			}
			d.repository.HostKind, d.repository.CanonicalRemote = metadata.HostKind, metadata.CanonicalRemote
			m.screen, m.err = repoConfirmScreen, nil
			return m, nil
		}
	case repoConfirmScreen:
		switch key {
		case "t":
			d.repository.AnalysisEnabled = !d.repository.AnalysisEnabled
		case "enter":
			input := d.repository
			m = m.confirm()
			return m, func() tea.Msg {
				_, err := m.client.ConnectRepository(m.ctx, m.selectedProjectID(), input)
				return actionDone{"Repository connected.", err}
			}
		}
		return m, nil

	case intentListScreen:
		return m.updateIntentList(key)
	case intentObjectiveScreen:
		if key == "enter" {
			if item, ok := m.chosen(); ok {
				// The value is "<objective ID> <revision>".
				idText, revisionText, _ := strings.Cut(item.value, " ")
				objectiveID, _ := uuid.Parse(idText)
				revision, _ := strconv.Atoi(revisionText)
				if objectiveID != d.intent.ObjectiveID || revision != d.intent.ObjectiveRevision {
					d.intent.BlueprintID, d.intent.BlueprintRevision, d.intent.CharterID = nil, nil, nil
				}
				d.intent.ObjectiveID, d.intent.ObjectiveRevision = objectiveID, revision
				return m.chooseIntentCharterOrNext(), nil
			}
		}
		return m.updateSelector(msg)
	case intentCharterScreen:
		if key == "enter" {
			if item, ok := m.chosen(); ok {
				d.intent.BlueprintID, d.intent.BlueprintRevision, d.intent.CharterID = nil, nil, nil
				for _, charter := range d.charters {
					if charter.cell.CellID.String() == item.value {
						blueprintID, revision, cellID := charter.blueprintID, charter.revision.Revision, charter.cell.CellID
						d.intent.BlueprintID, d.intent.BlueprintRevision, d.intent.CharterID = &blueprintID, &revision, &cellID
					}
				}
				return m.chooseIntentRepositoryOrNext(), nil
			}
		}
		return m.updateSelector(msg)
	case intentRepositoryScreen:
		if key == "enter" {
			if item, ok := m.chosen(); ok {
				d.intent.RepositoryID = nil
				if id, err := uuid.Parse(item.value); err == nil {
					d.intent.RepositoryID = &id
				}
				return m.startInput(intentSummaryScreen, "", "Describe the intended change in Markdown", d.intent.SummaryMarkdown), nil
			}
		}
		return m.updateSelector(msg)
	case intentSummaryScreen:
		if key == submitMarkdown {
			if strings.TrimSpace(m.area.Value()) == "" {
				m.err = errors.New("Describe the intended change before continuing.")
				return m, nil
			}
			d.intent.SummaryMarkdown = strings.TrimSpace(m.area.Value())
			return m.startInput(intentAssumptionsScreen, "", "One assumption per line (optional)", d.assumptionsText), nil
		}
	case intentAssumptionsScreen:
		if key == submitMarkdown {
			assumptions, err := parseAssumptions(m.area.Value())
			if err != nil {
				m.err = err
				return m, nil
			}
			d.assumptionsText, d.intent.Assumptions = m.area.Value(), assumptions
			return m.startInput(intentClaimsScreen, "", claimsPlaceholder, d.claimsText), nil
		}
	case intentClaimsScreen:
		if key == submitMarkdown {
			claims, err := parseClaims(m.area.Value(), d.intent.DeclaredClaims)
			if err != nil {
				m.err = err
				return m, nil
			}
			d.claimsText, d.intent.DeclaredClaims = m.area.Value(), claims
			m.screen, m.err = intentConfirmScreen, nil
			return m, nil
		}
	case intentConfirmScreen:
		if key == "enter" {
			input, expected := d.intent, d.intentExpected
			m = m.confirm()
			return m, func() tea.Msg {
				if expected > 0 {
					result, err := m.client.ReviseIntent(m.ctx, m.selectedProjectID(), input.IntentID, expected, input)
					return actionDone{fmt.Sprintf("Revised Intent %s to revision %d.", input.IntentID, result.Data.IntentRevision.Revision), err}
				}
				_, err := m.client.PublishIntent(m.ctx, m.selectedProjectID(), input)
				return actionDone{fmt.Sprintf("Published Intent %s as your CLI participant.", input.IntentID), err}
			}
		}
	case intentReleaseScreen:
		if key == "enter" {
			if intent, ok := m.currentIntent(); ok {
				intentID := intent.IntentRevision.IntentID
				m = m.confirm()
				return m, func() tea.Msg {
					_, err := m.client.ReleaseIntent(m.ctx, m.selectedProjectID(), intentID)
					return actionDone{fmt.Sprintf("Released Intent %s. Its revisions remain in history.", intentID), err}
				}
			}
		}
		return m, nil

	case evidenceKeyScreen:
		if key == "enter" {
			if item, ok := m.chosen(); ok {
				d.evidence.AcceptanceKey = item.value
				return m.chooseEvidenceKind(), nil
			}
		}
		return m.updateSelector(msg)
	case evidenceKindScreen:
		if key == "enter" {
			if item, ok := m.chosen(); ok {
				d.evidence.Kind = item.value
				return m.chooseEvidenceState(), nil
			}
		}
		return m.updateSelector(msg)
	case evidenceStateScreen:
		if key == "enter" {
			if item, ok := m.chosen(); ok {
				d.evidence.State = item.value
				return m.startInput(evidenceLocatorScreen, "Locator: ", "commit SHA, CI run URL, or opaque test ID", d.evidence.Locator), nil
			}
		}
		return m.updateSelector(msg)
	case evidenceLocatorScreen:
		if key == "enter" {
			if strings.TrimSpace(m.input.Value()) == "" {
				m.err = errors.New("Enter a locator: a commit SHA, an access-controlled link, or an opaque ID.")
				return m, nil
			}
			d.evidence.Locator = strings.TrimSpace(m.input.Value())
			return m.startInput(evidenceSummaryScreen, "Summary: ", "optional one-line summary", d.evidence.Summary), nil
		}
	case evidenceSummaryScreen:
		if key == "enter" {
			d.evidence.Summary = strings.TrimSpace(m.input.Value())
			m.screen, m.err = evidenceConfirmScreen, nil
			return m, nil
		}
	case evidenceConfirmScreen:
		if key == "enter" {
			input := d.evidence
			m = m.confirm()
			return m, func() tea.Msg {
				result, err := m.client.AttachEvidence(m.ctx, m.selectedProjectID(), input.IntentID, input)
				return actionDone{fmt.Sprintf("Attached %s evidence %s.", input.State, result.Data.EvidenceID), err}
			}
		}

	case analysisWorktreeScreen:
		if key == "enter" {
			if strings.TrimSpace(m.input.Value()) == "" {
				m.err = errors.New("Enter the local worktree to analyze.")
				return m, nil
			}
			d.analysisWorktree = strings.TrimSpace(m.input.Value())
			return m.startInput(analysisScriptScreen, "Analyzer script: ", defaultAnalyzerScript, d.analysisScript), nil
		}
	case analysisScriptScreen:
		if key == "enter" {
			if strings.TrimSpace(m.input.Value()) == "" {
				m.err = errors.New("Enter the built analyzer script, for example " + defaultAnalyzerScript + ".")
				return m, nil
			}
			d.analysisScript = strings.TrimSpace(m.input.Value())
			return m.startInput(analysisPathsScreen, "Paths: ", "optional comma-separated repository-relative paths", strings.Join(d.analysisPaths, ", ")), nil
		}
	case analysisPathsScreen:
		if key == "enter" {
			d.analysisPaths = splitList(m.input.Value())
			m.screen, m.err = analysisConfirmScreen, nil
			return m, nil
		}
	case analysisConfirmScreen:
		if key == "enter" {
			return m.runAnalysis()
		}

	case proposalKindScreen:
		if key == "enter" {
			if item, ok := m.chosen(); ok {
				d.proposal.Kind = item.value
				return m.startInput(proposalRationaleScreen, "", "Explain the proposal", d.proposal.RationaleMarkdown), nil
			}
		}
		return m.updateSelector(msg)
	case proposalRationaleScreen:
		if key == submitMarkdown {
			if strings.TrimSpace(m.area.Value()) == "" {
				m.err = errors.New("A proposal requires a rationale.")
				return m, nil
			}
			d.proposal.RationaleMarkdown = strings.TrimSpace(m.area.Value())
			return m.startInput(proposalReferencesScreen, "References: ", "optional comma-separated note or link references", d.referencesText), nil
		}
	case proposalReferencesScreen:
		if key == "enter" {
			references, err := parseReferences(m.input.Value())
			if err != nil {
				m.err = err
				return m, nil
			}
			d.referencesText, d.proposal.References = m.input.Value(), references
			m.screen, m.err = proposalConfirmScreen, nil
			return m, nil
		}
	case proposalConfirmScreen:
		if key == "enter" {
			collision, _ := m.currentCollision()
			input := d.proposal
			m = m.confirm()
			return m, func() tea.Msg {
				result, err := m.client.ProposeResolution(m.ctx, m.selectedProjectID(), collision.CollisionID, input)
				return actionDone{fmt.Sprintf("Recorded non-binding %s proposal %s. Only a human decision can adopt it.", input.Kind, result.Data.ProposalID), err}
			}
		}

	case decisionProposalScreen:
		if key == "enter" {
			if item, ok := m.chosen(); ok {
				d.decisionProposal, d.decision.SelectedProposalID = nil, nil
				for i, proposal := range d.proposals {
					if proposal.ProposalID.String() == item.value {
						selected := d.proposals[i]
						d.decisionProposal, d.decision.SelectedProposalID, d.decision.Kind = &selected, &selected.ProposalID, selected.Kind
					}
				}
				if d.decisionProposal == nil {
					return m.choose(decisionKindScreen, "Resolution kind selector for this decision", resolutionKinds, d.decision.Kind), nil
				}
				return m.afterDecisionKind()
			}
		}
		return m.updateSelector(msg)
	case decisionKindScreen:
		if key == "enter" {
			if item, ok := m.chosen(); ok {
				d.decision.Kind = item.value
				return m.afterDecisionKind()
			}
		}
		return m.updateSelector(msg)
	case decisionBindingScreen:
		if key == "enter" {
			if item, ok := m.chosen(); ok {
				if item.value == "binding" && !m.owner() {
					m.err = errors.New("Only the project owner can record a binding decision. Choose a non-binding record.")
					return m, nil
				}
				d.decision.Binding = item.value == "binding"
				return m.startInput(decisionRationaleScreen, "", "Explain the decision", d.decision.RationaleMarkdown), nil
			}
		}
		return m.updateSelector(msg)
	case decisionRationaleScreen:
		if key == submitMarkdown {
			if strings.TrimSpace(m.area.Value()) == "" {
				m.err = errors.New("A decision requires a rationale.")
				return m, nil
			}
			d.decision.RationaleMarkdown = strings.TrimSpace(m.area.Value())
			return m.startInput(decisionReferencesScreen, "References: ", "optional comma-separated meeting, note, or link references", d.referencesText), nil
		}
	case decisionReferencesScreen:
		if key == "enter" {
			references, err := parseReferences(m.input.Value())
			if err != nil {
				m.err = err
				return m, nil
			}
			d.referencesText, d.decision.References = m.input.Value(), references
			m.screen, m.err = decisionConfirmScreen, nil
			return m, nil
		}
	case decisionConfirmScreen:
		if key == "enter" {
			collision, _ := m.currentCollision()
			input := d.decision
			m = m.confirm()
			return m, func() tea.Msg {
				result, err := m.client.DecideResolution(m.ctx, m.selectedProjectID(), collision.CollisionID, input)
				label := "non-binding"
				if input.Binding {
					label = "binding"
				}
				return actionDone{fmt.Sprintf("Recorded %s %s decision %s. The collision stays advisory.", label, input.Kind, result.Data.DecisionID), err}
			}
		}

	case importSourceScreen:
		if key == "enter" {
			path := strings.TrimSpace(m.input.Value())
			ref, err := kirospec.SafeReference(path)
			if err != nil {
				m.err = fmt.Errorf("Enter a specification path relative to this directory: %v", err)
				return m, nil
			}
			hash, heading, err := kirospec.HashAndTitle(path)
			if err != nil {
				m.err = err
				return m, nil
			}
			d.importPath, d.importHeading = path, heading
			d.importInput.SourceRef, d.importInput.ContentHash = ref, hash
			return m.chooseImportTarget(), nil
		}
	case importTargetScreen:
		if key == "enter" {
			if item, ok := m.chosen(); ok {
				return m.chooseImportObjective(item.value)
			}
		}
		return m.updateSelector(msg)
	case importTitleScreen:
		if key == "enter" {
			if strings.TrimSpace(m.input.Value()) == "" {
				m.err = errors.New("Enter a reviewed objective title before continuing.")
				return m, nil
			}
			d.importInput.Title = strings.TrimSpace(m.input.Value())
			return m.startInput(importOverviewScreen, "", "Reviewed objective summary in Markdown; the source is never uploaded", d.importInput.OverviewMarkdown), nil
		}
	case importOverviewScreen:
		if key == submitMarkdown {
			if strings.TrimSpace(m.area.Value()) == "" {
				m.err = errors.New("Enter a reviewed Markdown summary before continuing.")
				return m, nil
			}
			d.importInput.OverviewMarkdown = strings.TrimSpace(m.area.Value())
			return m.startInput(importCriteriaScreen, "", criteriaPlaceholder, d.criteriaText), nil
		}
	case importCriteriaScreen:
		if key == submitMarkdown {
			criteria, err := parseCriteria(m.area.Value(), d.importKept)
			if err != nil {
				m.err = err
				return m, nil
			}
			d.criteriaText, d.importInput.KeyPoints = m.area.Value(), append(append([]protocol.KeyPoint(nil), d.importKept...), criteria...)
			return m.chooseMember(importMemberScreen, "Accountable member selector", d.importInput.AccountableHumanID), nil
		}
	case importMemberScreen:
		if key == "enter" {
			if item, ok := m.selector.SelectedItem().(memberItem); ok {
				d.importMember, d.importInput.AccountableHumanID, m.screen = item.member, item.member.Human.HumanID, importConfirmScreen
				return m, nil
			}
		}
		return m.updateSelector(msg)
	case importConfirmScreen:
		if key == "enter" {
			input, expected := d.importInput, d.importExpected
			m = m.confirm()
			return m, func() tea.Msg {
				result, err := m.client.ImportKiroSpecObjective(m.ctx, m.selectedProjectID(), expected, input)
				return actionDone{fmt.Sprintf("Imported objective %s revision %d from %s.", input.ObjectiveID, result.Data.Revision, input.SourceRef), err}
			}
		}
	}
	return m.updateField(msg)
}

// updateField sends a key to the screen's text input or Markdown area.
func (m model) updateField(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	if multiline(m.screen) {
		m.area, cmd = m.area.Update(msg)
	} else {
		m.input, cmd = m.input.Update(msg)
	}
	m.err = nil
	return m, cmd
}

func (m model) updateIntentList(key string) (tea.Model, tea.Cmd) {
	intent, ok := m.currentIntent()
	if !ok {
		return m, nil
	}
	revision := intent.IntentRevision
	switch key {
	case "up", "k":
		m.intentCursor = max(0, m.intentCursor-1)
		return m, nil
	case "down", "j":
		m.intentCursor = min(len(m.intents.Intents)-1, m.intentCursor+1)
		return m, nil
	}
	if intent.Status != "active" {
		return m, nil
	}
	d := &m.semantic
	switch key {
	case "e":
		d.intent = protocol.IntentInput{
			IntentID: revision.IntentID, ObjectiveID: revision.ObjectiveID, ObjectiveRevision: revision.ObjectiveRevision,
			BlueprintID: revision.BlueprintID, BlueprintRevision: revision.BlueprintRevision, CharterID: revision.CharterID,
			RepositoryID: revision.RepositoryID, BaseRevisionRef: revision.BaseRevisionRef, HeadRevisionRef: revision.HeadRevisionRef,
			BranchRef: revision.BranchRef, SummaryMarkdown: revision.SummaryMarkdown, Assumptions: revision.Assumptions,
			DeclaredClaims: revision.DeclaredClaims,
		}
		d.intentExpected = revision.Revision
		d.assumptionsText, d.claimsText = strings.Join(revision.Assumptions, "\n"), formatClaims(revision.DeclaredClaims)
		return m.chooseIntentObjective(), nil
	case "x":
		m.screen, m.err = intentReleaseScreen, nil
		return m, nil
	case "v":
		d.evidence = protocol.EvidenceAttachmentInput{IntentID: revision.IntentID, ObjectiveID: revision.ObjectiveID, ObjectiveRevision: revision.ObjectiveRevision}
		m.loading, m.err = true, nil
		client, ctx, projectID := m.client, m.ctx, m.selectedProjectID()
		return m, func() tea.Msg {
			result, err := client.ObjectiveRevision(ctx, projectID, revision.ObjectiveID, revision.ObjectiveRevision)
			var keys []protocol.KeyPoint
			for _, point := range result.Data.KeyPoints {
				if point.Kind == protocol.KeyPointAcceptanceCriterion {
					keys = append(keys, point)
				}
			}
			return evidenceKeysLoaded{keys: keys, err: err}
		}
	case "z":
		if revision.RepositoryID == nil {
			m.err = errors.New("This Intent references no repository. Revise it with e and choose a repository before analysis.")
			return m, nil
		}
		repository, found := protocol.RepositoryConnection{}, false
		for _, item := range m.radar.Repositories {
			if item.RepositoryID == *revision.RepositoryID {
				repository, found = item, true
			}
		}
		if !found || !repository.AnalysisEnabled {
			m.err = errors.New("Local analysis is disabled or unavailable for this Intent's repository connection.")
			return m, nil
		}
		d.analysisIntentID, d.analysisRemote = revision.IntentID, repository.CanonicalRemote
		if d.analysisWorktree == "" {
			d.analysisWorktree = "."
		}
		if d.analysisScript == "" {
			d.analysisScript = defaultAnalyzerScript
		}
		return m.startInput(analysisWorktreeScreen, "Worktree: ", "local repository worktree", d.analysisWorktree), nil
	}
	return m, nil
}

func (m model) chooseIntentObjective() model {
	d := m.semantic
	items := make([]choiceItem, 0, len(m.radar.Objectives)+1)
	current := false
	for _, objective := range m.radar.Objectives {
		value := fmt.Sprintf("%s %d", objective.ObjectiveID, objective.Revision)
		items = append(items, choiceItem{clean(objective.Title), fmt.Sprintf("current revision %d · %s", objective.Revision, objective.ObjectiveID), value})
		current = current || (objective.ObjectiveID == d.intent.ObjectiveID && objective.Revision == d.intent.ObjectiveRevision)
	}
	if d.intentExpected > 0 && !current {
		// A revision never silently moves to a newer Objective revision.
		items = append([]choiceItem{{"Keep the referenced Objective revision", fmt.Sprintf("revision %d · %s (not current)", d.intent.ObjectiveRevision, d.intent.ObjectiveID),
			fmt.Sprintf("%s %d", d.intent.ObjectiveID, d.intent.ObjectiveRevision)}}, items...)
	}
	return m.choose(intentObjectiveScreen, "Objective selector for this Intent", items, fmt.Sprintf("%s %d", d.intent.ObjectiveID, d.intent.ObjectiveRevision))
}

// eligibleCharters lists the human's accepted charters in a proposed or active
// revision that divides exactly this Objective revision.
func (m model) eligibleCharters(objectiveID uuid.UUID, objectiveRevision int) []charterItem {
	var charters []charterItem
	for _, blueprint := range m.status.Blueprints {
		for _, revision := range blueprint.Revisions {
			if revision.ObjectiveID != objectiveID || revision.ObjectiveRevision != objectiveRevision ||
				(revision.Status != protocol.BlueprintActive && revision.Status != protocol.BlueprintProposed) {
				continue
			}
			for _, cell := range revision.Cells {
				if cell.AccountableHumanID == m.human.HumanID && cell.Decision == protocol.CellAccepted {
					charters = append(charters, charterItem{blueprint.BlueprintID, revision, cell})
				}
			}
		}
	}
	return charters
}

func (m model) chooseIntentCharterOrNext() model {
	d := &m.semantic
	d.charters = m.eligibleCharters(d.intent.ObjectiveID, d.intent.ObjectiveRevision)
	if len(d.charters) == 0 {
		d.intent.BlueprintID, d.intent.BlueprintRevision, d.intent.CharterID = nil, nil, nil
		return m.chooseIntentRepositoryOrNext()
	}
	items := []choiceItem{{"No charter", "Unpartitioned coordination context", "none"}}
	selected := "none"
	for _, charter := range d.charters {
		items = append(items, choiceItem{clean(charter.cell.Title), fmt.Sprintf("Blueprint %s · revision %d · %s", charter.blueprintID, charter.revision.Revision, charter.revision.Status), charter.cell.CellID.String()})
		if d.intent.CharterID != nil && *d.intent.CharterID == charter.cell.CellID {
			selected = charter.cell.CellID.String()
		}
	}
	return m.choose(intentCharterScreen, "Your accepted charter selector for this Intent", items, selected)
}

// chooseIntentCharterOrBack returns to the charter step when it applies, or
// to the objective step otherwise.
func (m model) chooseIntentCharterOrBack() model {
	if len(m.eligibleCharters(m.semantic.intent.ObjectiveID, m.semantic.intent.ObjectiveRevision)) > 0 {
		return m.chooseIntentCharterOrNext()
	}
	return m.chooseIntentObjective()
}

func (m model) chooseIntentRepositoryOrNext() model {
	if len(m.radar.Repositories) == 0 {
		m.semantic.intent.RepositoryID = nil
		return m.startInput(intentSummaryScreen, "", "Describe the intended change in Markdown", m.semantic.intent.SummaryMarkdown)
	}
	return m.chooseIntentRepository()
}

func (m model) chooseIntentRepository() model {
	items := []choiceItem{{"No repository", "Declared claims only; local analysis needs a repository", "none"}}
	selected := "none"
	for _, repository := range m.radar.Repositories {
		items = append(items, choiceItem{clean(repository.CanonicalRemote), fmt.Sprintf("%s · local analysis %s", repository.HostKind, onOff(repository.AnalysisEnabled)), repository.RepositoryID.String()})
		if m.semantic.intent.RepositoryID != nil && *m.semantic.intent.RepositoryID == repository.RepositoryID {
			selected = repository.RepositoryID.String()
		}
	}
	return m.choose(intentRepositoryScreen, "Repository selector for this Intent", items, selected)
}

func (m model) chooseEvidenceKey() model {
	items := []choiceItem{{"No acceptance key", "Evidence for the Intent only; it does not cover a criterion", ""}}
	for _, point := range m.semantic.evidenceKeys {
		items = append(items, choiceItem{point.Key, clean(point.Statement), point.Key})
	}
	return m.choose(evidenceKeyScreen, "Acceptance criterion selector", items, m.semantic.evidence.AcceptanceKey)
}

func (m model) chooseEvidenceKind() model {
	items := make([]choiceItem, len(evidenceKinds))
	for i, kind := range evidenceKinds {
		items[i] = choiceItem{kind, "Evidence kind", kind}
	}
	return m.choose(evidenceKindScreen, "Evidence kind selector", items, m.semantic.evidence.Kind)
}

func (m model) chooseEvidenceState() model {
	items := make([]choiceItem, len(evidenceStates))
	for i, state := range evidenceStates {
		items[i] = choiceItem{state, "Evidence state", state}
	}
	return m.choose(evidenceStateScreen, "Evidence state selector", items, m.semantic.evidence.State)
}

func (m model) loadContext() (tea.Model, tea.Cmd) {
	collision, ok := m.currentCollision()
	if !ok {
		return m, nil
	}
	m.loading, m.err = true, nil
	client, ctx, projectID := m.client, m.ctx, m.selectedProjectID()
	return m, func() tea.Msg {
		result, err := client.ConflictContext(ctx, projectID, collision.CollisionID)
		return contextLoaded{collisionID: collision.CollisionID, pack: result.Data, err: err}
	}
}

func (m model) chooseDecisionProposal() model {
	items := []choiceItem{{"Decide without selecting a proposal", "Record a human decision directly", ""}}
	selected := ""
	if m.semantic.decisionProposal != nil {
		selected = m.semantic.decisionProposal.ProposalID.String()
	}
	for _, proposal := range m.semantic.proposals {
		items = append(items, choiceItem{fmt.Sprintf("%s proposal %s", proposal.Kind, proposal.ProposalID.String()[:8]), firstLine(proposal.RationaleMarkdown, 80), proposal.ProposalID.String()})
	}
	return m.choose(decisionProposalScreen, "Proposal selector for this decision", items, selected)
}

// afterDecisionKind applies the binding rules for the chosen kind: binding
// kinds are owner-only and always binding; other kinds may be either.
func (m model) afterDecisionKind() (tea.Model, tea.Cmd) {
	d := &m.semantic
	if bindingKind(d.decision.Kind) {
		if !m.owner() {
			m.err = fmt.Errorf("Only the project owner can record a %s decision, because it is always binding.", d.decision.Kind)
			return m, nil
		}
		d.decision.Binding = true
		return m.startInput(decisionRationaleScreen, "", "Explain the decision", d.decision.RationaleMarkdown), nil
	}
	return m.chooseDecisionBinding(), nil
}

func (m model) chooseDecisionBinding() model {
	selected := "record"
	if m.semantic.decision.Binding {
		selected = "binding"
	}
	return m.choose(decisionBindingScreen, "Decision authority selector", []choiceItem{
		{"Non-binding record", "Records the team's handling without binding anyone", "record"},
		{"Binding team decision", "Owner only. Binds the team until a later decision", "binding"},
	}, selected)
}

func (m model) chooseImportTarget() model {
	items := []choiceItem{{"Create a new Objective", "Revision 1 with this specification's provenance", "new"}}
	for _, objective := range m.radar.Objectives {
		items = append(items, choiceItem{"Refresh: " + clean(objective.Title), fmt.Sprintf("Creates revision %d of %s; revision %d is never changed", objective.Revision+1, objective.ObjectiveID, objective.Revision), objective.ObjectiveID.String()})
	}
	selected := "new"
	if m.semantic.importExpected > 0 {
		selected = m.semantic.importInput.ObjectiveID.String()
	}
	return m.choose(importTargetScreen, "Import target selector", items, selected)
}

func (m model) chooseImportObjective(value string) (tea.Model, tea.Cmd) {
	d := &m.semantic
	if value == "new" {
		if d.importExpected > 0 {
			d.importInput.ObjectiveID, d.importInput.OverviewMarkdown, d.importInput.Key = uuid.NewV7(), "", ""
			d.importKept, d.criteriaText = nil, ""
		}
		d.importExpected = 0
		return m.startInput(importTitleScreen, "Objective title: ", "Reviewed title", firstNonEmpty(d.importInput.Title, d.importHeading)), nil
	}
	for _, objective := range m.radar.Objectives {
		if objective.ObjectiveID.String() != value {
			continue
		}
		d.importInput.ObjectiveID, d.importExpected = objective.ObjectiveID, objective.Revision
		m.loading, m.err = true, nil
		client, ctx, projectID := m.client, m.ctx, m.selectedProjectID()
		return m, func() tea.Msg {
			result, err := client.ObjectiveRevision(ctx, projectID, objective.ObjectiveID, objective.Revision)
			return importBaseLoaded{objective: result.Data, err: err}
		}
	}
	return m, nil
}

func (m model) runAnalysis() (tea.Model, tea.Cmd) {
	d := m.semantic
	script, err := filepath.Abs(d.analysisScript)
	if err != nil {
		m.err = err
		return m, nil
	}
	m = m.confirm()
	client, ctx, projectID := m.client, m.ctx, m.selectedProjectID()
	return m, func() tea.Msg {
		report, err := localanalyzer.Run(ctx, localanalyzer.Config{NodePath: "node", AnalyzerPath: script, Timeout: localanalyzer.DefaultTimeout},
			localanalyzer.Request{Worktree: d.analysisWorktree, CanonicalRemote: d.analysisRemote, Paths: d.analysisPaths})
		if err != nil {
			return actionDone{err: err}
		}
		if _, err := client.AttachStaticAnalysis(ctx, projectID, d.analysisIntentID, report); err != nil {
			return actionDone{err: err}
		}
		message := fmt.Sprintf("Attached %d static-analysis claims to Intent %s.", len(report.Claims), d.analysisIntentID)
		if len(report.Diagnostics) > 0 {
			message += fmt.Sprintf(" %d bounded diagnostics were kept as incomplete-analysis context.", len(report.Diagnostics))
		}
		return actionDone{message: message}
	}
}

// semanticFormView renders the semantic screens.
func (m model) semanticFormView() string {
	d := m.semantic
	title, body, footer := "", "", ""
	switch m.screen {
	case radarCollisionScreen:
		title, body = "Collision drill-down", m.radarCollisionView()+"\n\n"+m.collisionDetails()
		footer = "  Up/down collision · c context pack · p propose · d decide · Esc back to Radar"
	case contextScreen:
		title, body = "Conflict context pack", m.viewer.View()
		footer = "  Up/down/PgUp/PgDn scroll · y copy Markdown · w save Markdown file · Esc back"
	case repoRemoteScreen:
		title, body, footer = "Connect repository", "Conn stores only a normalized, credential-free identity such as github.com/owner/repository.\n\n"+m.input.View(), "  Enter review · Esc cancel"
	case repoConfirmScreen:
		body = fmt.Sprintf("Remote: %s\nHost kind: %s\nLocal analysis: %s\n\nThis records a repository scope. It grants Conn no repository write access.", clean(d.repository.CanonicalRemote), d.repository.HostKind, onOff(d.repository.AnalysisEnabled))
		title, footer = "Connect this repository?", "  t toggle local analysis · Enter connect · Esc edit"
	case intentListScreen:
		title, body = "Intents", m.intentListView()
		footer = "  Up/down select · Esc back"
		if intent, ok := m.currentIntent(); ok && intent.Status == "active" {
			footer = "  Up/down select · e revise · x release · v attach evidence · z analyze locally · Esc back"
		}
	case intentSummaryScreen:
		title, body, footer = m.intentStep("Markdown summary"), m.area.View(), "  Ctrl+S next · Enter new line · Esc back"
	case intentAssumptionsScreen:
		title, body, footer = m.intentStep("assumptions"), "One assumption per line. Leave empty for none.\n\n"+m.area.View(), "  Ctrl+S next · Enter new line · Esc back"
	case intentClaimsScreen:
		title = m.intentStep("declared claims")
		body = "One claim per line: kind relation subject [display name]\nExample: invariant changes invariant:checkout:one-idempotency-key-one-successful-charge One key, one charge\n" +
			"Kinds: " + strings.Join(claimKinds, ", ") + "\nRelations: " + strings.Join(claimRelations, ", ") + "\n\n" + m.area.View()
		footer = "  Ctrl+S review · Enter new line · Esc back"
	case intentConfirmScreen:
		title, body, footer = m.intentStep("review"), m.intentReview(), "  Enter "+map[bool]string{true: "revise", false: "publish"}[d.intentExpected > 0]+" Intent · Esc edit claims"
	case intentReleaseScreen:
		intent, _ := m.currentIntent()
		title = "Release this Intent?"
		body = fmt.Sprintf("Intent %s revision %d\n%s\n\nReleasing ends active coordination for this Intent. Every revision stays in history, and its open collisions become stale.\nOnly the participant that published it can release it.", intent.IntentRevision.IntentID, intent.IntentRevision.Revision, firstLine(intent.IntentRevision.SummaryMarkdown, 200))
		footer = "  Enter release · Esc back"
	case evidenceLocatorScreen:
		title, body, footer = "Attach evidence: locator", "Store a reference, never logs, screenshots, or source.\n\n"+m.input.View(), "  Enter next · Esc back"
	case evidenceSummaryScreen:
		title, body, footer = "Attach evidence: summary", m.input.View(), "  Enter review · Esc back"
	case evidenceConfirmScreen:
		key := d.evidence.AcceptanceKey
		if key == "" {
			key = "none (does not cover a criterion)"
		}
		body = fmt.Sprintf("Intent: %s\nObjective: %s revision %d\nAcceptance key: %s\nKind: %s\nState: %s\nLocator: %s\nSummary: %s", d.evidence.IntentID, d.evidence.ObjectiveID, d.evidence.ObjectiveRevision, key, d.evidence.Kind, d.evidence.State, clean(d.evidence.Locator), clean(firstNonEmpty(d.evidence.Summary, "none")))
		title, footer = "Attach this evidence?", "  Enter attach · Esc edit"
	case analysisWorktreeScreen:
		title, body, footer = "Analyze locally: worktree", "The TypeScript analyzer runs on this machine. Conn receives only normalized claims, paths, hashes, and code-only diagnostics.\n\n"+m.input.View(), "  Enter next · Esc back"
	case analysisScriptScreen:
		title, body, footer = "Analyze locally: analyzer script", "Build it with pnpm build. Relative paths start from the directory conn app was started in.\n\n"+m.input.View(), "  Enter next · Esc back"
	case analysisPathsScreen:
		title, body, footer = "Analyze locally: paths", "Limit analysis to the files this Intent changes. Leave empty to analyze every configured TypeScript project; every export then counts as affected.\n\n"+m.input.View(), "  Enter review · Esc back"
	case analysisConfirmScreen:
		paths := "all configured TypeScript projects"
		if len(d.analysisPaths) > 0 {
			paths = strings.Join(d.analysisPaths, ", ")
		}
		body = fmt.Sprintf("Intent: %s\nRepository: %s\nWorktree: %s\nAnalyzer: %s\nPaths: %s\n\nThe analyzer receives no Conn credentials. Source text never leaves this machine.", d.analysisIntentID, clean(d.analysisRemote), clean(d.analysisWorktree), clean(d.analysisScript), clean(paths))
		title, footer = "Run local analysis?", "  Enter analyze and attach claims · Esc edit"
	case proposalRationaleScreen:
		title, body, footer = "Propose resolution: rationale", m.area.View(), "  Ctrl+S next · Enter new line · Esc back"
	case proposalReferencesScreen:
		title, body, footer = "Propose resolution: references", m.input.View(), "  Enter review · Esc back"
	case proposalConfirmScreen:
		collision, _ := m.currentCollision()
		body = fmt.Sprintf("Collision: %s (%s)\nKind: %s\nRationale:\n%s\nReferences: %s\n\nA proposal is non-binding. It does not change the collision, objectives, blueprints, or charters.", collision.CollisionID, clean(collision.RuleID), d.proposal.Kind, cleanBlock(d.proposal.RationaleMarkdown), clean(firstNonEmpty(strings.Join(d.proposal.References, ", "), "none")))
		title, footer = "Record this proposal?", "  Enter record proposal · Esc edit"
	case decisionRationaleScreen:
		title, body, footer = "Record decision: rationale", m.area.View(), "  Ctrl+S next · Enter new line · Esc back"
	case decisionReferencesScreen:
		title, body, footer = "Record decision: references", "Reference the meeting, agent session note, or link where the team decided. Conn stores no raw conversation.\n\n"+m.input.View(), "  Enter review · Esc back"
	case decisionConfirmScreen:
		collision, _ := m.currentCollision()
		proposal := "none"
		if d.decisionProposal != nil {
			proposal = fmt.Sprintf("%s (%s)", d.decisionProposal.ProposalID, d.decisionProposal.Kind)
		}
		authority := "Non-binding record"
		if d.decision.Binding {
			authority = "Binding team decision (owner)"
		}
		body = fmt.Sprintf("Collision: %s (%s)\nSelected proposal: %s\nKind: %s\nAuthority: %s\nRationale:\n%s\nReferences: %s\n\nThe decision is immutable and attributed to you. The collision stays advisory; this does not merge, block, or assign work.", collision.CollisionID, clean(collision.RuleID), proposal, d.decision.Kind, authority, cleanBlock(d.decision.RationaleMarkdown), clean(firstNonEmpty(strings.Join(d.decision.References, ", "), "none")))
		title, footer = "Record this decision?", "  Enter record decision · Esc edit"
	case importSourceScreen:
		title, body, footer = "Import Kiro specification", "Conn hashes the file locally and stores only its relative reference and SHA-256 digest. The source is never uploaded.\n\n"+m.input.View(), "  Enter next · Esc cancel"
	case importTitleScreen:
		title, body, footer = m.importStep("title"), m.input.View(), "  Enter next · Esc back"
	case importOverviewScreen:
		title, body, footer = m.importStep("reviewed Markdown summary"), m.area.View(), "  Ctrl+S next · Enter new line · Esc back"
	case importCriteriaScreen:
		title = m.importStep("acceptance criteria")
		body = "One keyed acceptance criterion per line, for example:\nretry-safe: A retried charge never charges twice.\nEvidence coverage is measured against these keys. Leave empty for none.\n\n" + m.area.View()
		footer = "  Ctrl+S choose accountable member · Enter new line · Esc back"
	case importConfirmScreen:
		title, body, footer = m.importStep("review"), m.importReview(), "  Enter import · Esc edit member"
	default:
		// Selector screens.
		footers := map[screen]string{
			intentObjectiveScreen:  "  Form selector: choose the Objective revision · Enter choose · Esc back",
			intentCharterScreen:    "  Form selector: choose your accepted charter or none · Enter choose · Esc back",
			intentRepositoryScreen: "  Form selector: choose a repository or none · Enter choose · Esc back",
			evidenceKeyScreen:      "  Form selector: choose an acceptance criterion · Enter choose · Esc back",
			evidenceKindScreen:     "  Form selector: choose the evidence kind · Enter choose · Esc back",
			evidenceStateScreen:    "  Form selector: choose the evidence state · Enter choose · Esc back",
			proposalKindScreen:     "  Form selector: choose the proposal kind · Enter choose · Esc back",
			decisionProposalScreen: "  Form selector: choose a proposal to adopt or none · Enter choose · Esc back",
			decisionKindScreen:     "  Form selector: choose the decision kind · Enter choose · Esc back",
			decisionBindingScreen:  "  Form selector: choose the decision authority · Enter choose · Esc back",
			importTargetScreen:     "  Form selector: new Objective or explicit refresh · Enter choose · Esc back",
			importMemberScreen:     "  Form selector: choose the accountable member · Enter choose · Esc back",
		}
		return m.selectorView(footers[m.screen])
	}
	if m.formNote != "" {
		body += "\n\n" + helpStyle.Render(clean(m.formNote))
	}
	if m.err != nil {
		body += "\n\n" + errorStyle.Render(clean(m.err.Error()))
	}
	return m.header() + "\n\n" + m.card(title, body) + "\n\n" + footerStyle.Render(footer) + "\n"
}

func (m model) intentStep(field string) string {
	if m.semantic.intentExpected > 0 {
		return fmt.Sprintf("Revise Intent (revision %d): %s", m.semantic.intentExpected+1, field)
	}
	return "Publish Intent: " + field
}

func (m model) importStep(field string) string {
	if m.semantic.importExpected > 0 {
		return fmt.Sprintf("Refresh objective (revision %d): %s", m.semantic.importExpected+1, field)
	}
	return "Import objective: " + field
}

func (m model) objectiveTitle(id uuid.UUID) string {
	for _, objective := range m.radar.Objectives {
		if objective.ObjectiveID == id {
			return clean(objective.Title)
		}
	}
	return id.String()
}

func (m model) repositoryName(id *uuid.UUID) string {
	if id == nil {
		return "none"
	}
	for _, repository := range m.radar.Repositories {
		if repository.RepositoryID == *id {
			return clean(repository.CanonicalRemote)
		}
	}
	return id.String()
}

func (m model) intentReview() string {
	d := m.semantic
	intent := d.intent
	charter := "none (unpartitioned coordination context)"
	if intent.CharterID != nil {
		charter = fmt.Sprintf("%s in Blueprint %s revision %d", *intent.CharterID, *intent.BlueprintID, *intent.BlueprintRevision)
	}
	lines := []string{
		fmt.Sprintf("Intent: %s", intent.IntentID),
		fmt.Sprintf("Objective: %s (%s) revision %d", m.objectiveTitle(intent.ObjectiveID), intent.ObjectiveID, intent.ObjectiveRevision),
		"Charter: " + charter,
		"Repository: " + m.repositoryName(intent.RepositoryID),
		"\nSummary:\n" + cleanBlock(intent.SummaryMarkdown),
	}
	lines = append(lines, "\nAssumptions:")
	if len(intent.Assumptions) == 0 {
		lines = append(lines, "  none declared")
	}
	for _, assumption := range intent.Assumptions {
		lines = append(lines, "  - "+clean(assumption))
	}
	lines = append(lines, "\nDeclared claims:")
	if len(intent.DeclaredClaims) == 0 {
		lines = append(lines, "  none declared; Conn cannot assess collisions without claims")
	}
	for _, claim := range intent.DeclaredClaims {
		lines = append(lines, fmt.Sprintf("  - %s %s %s (%s)", claim.SubjectKind, claim.Relation, clean(claim.NormalizedSubject), clean(claim.DisplayName)))
	}
	if d.intentExpected > 0 {
		lines = append(lines, fmt.Sprintf("\nThis appends revision %d after revision %d. If someone revised it first, Conn returns a conflict.", d.intentExpected+1, d.intentExpected))
	}
	lines = append(lines, "This app publishes as your CLI participant; Kiro sessions are separate participants.")
	return strings.Join(lines, "\n")
}

func (m model) importReview() string {
	d := m.semantic
	input := d.importInput
	lines := []string{
		fmt.Sprintf("Source: %s (kiro_spec)\nSHA-256: %s", clean(input.SourceRef), input.ContentHash),
		fmt.Sprintf("Objective: %s", input.ObjectiveID),
	}
	if d.importExpected > 0 {
		lines = append(lines, fmt.Sprintf("Refresh: creates revision %d. Revision %d and earlier stay unchanged.", d.importExpected+1, d.importExpected))
	} else {
		lines = append(lines, "Creates revision 1.")
	}
	lines = append(lines, "Title: "+clean(input.Title), "\nSummary:\n"+cleanBlock(input.OverviewMarkdown), "\nKey points:")
	if len(input.KeyPoints) == 0 {
		lines = append(lines, "  none")
	}
	for _, point := range input.KeyPoints {
		lines = append(lines, fmt.Sprintf("  %s (%s): %s", point.Key, point.Kind, clean(point.Statement)))
	}
	lines = append(lines, fmt.Sprintf("\nAccountable human: @%s · %s", clean(d.importMember.Human.GitHubLogin), input.AccountableHumanID),
		"\nLater changes to the specification never change this Objective; refresh it explicitly.")
	return strings.Join(lines, "\n")
}

// work renders the Work section: repository scopes and Intents.
func (m model) work() string {
	lines := []string{"Repositories:"}
	if len(m.radar.Repositories) == 0 {
		lines = append(lines, "  none connected; repository scope is unknown")
	}
	for _, repository := range m.radar.Repositories {
		lines = append(lines, fmt.Sprintf("  %s · %s · local analysis %s", clean(repository.CanonicalRemote), repository.HostKind, onOff(repository.AnalysisEnabled)))
	}
	lines = append(lines, "\nIntents:")
	if len(m.intents.Intents) == 0 {
		lines = append(lines, "  none published. This is absence of reported work, not completion.")
	}
	for _, intent := range m.intents.Intents {
		revision := intent.IntentRevision
		lines = append(lines, fmt.Sprintf("  [%s] %s · revision %d · %s", intent.Status, revision.IntentID, revision.Revision, firstLine(revision.SummaryMarkdown, 60)))
	}
	actions := "\nActions: n publish Intent"
	if len(m.intents.Intents) > 0 {
		actions += " · i inspect, revise, release, analyze, or attach evidence"
	}
	if m.owner() {
		actions += " · g connect repository (owner)"
	}
	return strings.Join(append(lines, actions), "\n")
}

func (m model) intentListView() string {
	var lines []string
	for i, intent := range m.intents.Intents {
		marker := "  "
		if i == m.intentCursor {
			marker = "> "
		}
		lines = append(lines, fmt.Sprintf("%s[%s] %s · revision %d · %s", marker, intent.Status, intent.IntentRevision.IntentID, intent.IntentRevision.Revision, firstLine(intent.IntentRevision.SummaryMarkdown, 50)))
	}
	intent, ok := m.currentIntent()
	if !ok {
		return "No Intents are published."
	}
	revision := intent.IntentRevision
	charter := "none (unpartitioned coordination context)"
	if revision.CharterID != nil {
		charter = revision.CharterID.String()
	}
	lines = append(lines,
		fmt.Sprintf("\nIntent %s", revision.IntentID),
		fmt.Sprintf("Status: %s · revision %d · participant %s", intent.Status, revision.Revision, revision.ParticipantID),
		fmt.Sprintf("Objective: %s revision %d", m.objectiveTitle(revision.ObjectiveID), revision.ObjectiveRevision),
		"Charter: "+charter,
		"Repository: "+m.repositoryName(revision.RepositoryID),
		"\nSummary:\n"+cleanBlock(revision.SummaryMarkdown))
	if len(revision.Assumptions) > 0 {
		lines = append(lines, "\nAssumptions:")
		for _, assumption := range revision.Assumptions {
			lines = append(lines, "  - "+clean(assumption))
		}
	}
	lines = append(lines, "\nClaims on this revision:")
	claims := 0
	for _, claim := range m.radar.SemanticClaims {
		if claim.IntentID == revision.IntentID && claim.IntentRevision == revision.Revision {
			claims++
			lines = append(lines, fmt.Sprintf("  - %s %s %s · %s", claim.SubjectKind, claim.Relation, clean(claim.NormalizedSubject), claim.ProvenanceKind))
		}
	}
	if claims == 0 {
		if intent.Status == "active" {
			lines = append(lines, "  none")
		} else {
			lines = append(lines, "  not shown for a released Intent")
		}
	}
	collisions := 0
	for _, collision := range m.radar.OpenCollisions {
		if collision.LeftIntentID == revision.IntentID || collision.RightIntentID == revision.IntentID {
			collisions++
		}
	}
	lines = append(lines, fmt.Sprintf("\nOpen advisory collisions: %d", collisions))
	for _, observation := range intent.AdvisoryObservations {
		lines = append(lines, "Advisory: "+clean(observation))
	}
	if intent.Status == "active" {
		lines = append(lines, "\nOnly the participant that published this Intent can change it. This app acts as your CLI participant.")
	}
	return strings.Join(lines, "\n")
}

// collisionDetails lists the matching claims and proposals of the collision on screen.
func (m model) collisionDetails() string {
	collision, ok := m.currentCollision()
	if !ok {
		return ""
	}
	claims := map[uuid.UUID]protocol.SemanticClaim{}
	for _, claim := range m.radar.SemanticClaims {
		claims[claim.ClaimID] = claim
	}
	lines := []string{"Matching claims:"}
	for _, id := range collision.MatchingClaimRefs {
		claim, found := claims[id]
		if !found {
			lines = append(lines, fmt.Sprintf("  - %s (details unavailable)", id))
			continue
		}
		confidence := ""
		if claim.Confidence != nil {
			confidence = fmt.Sprintf(", confidence %.2f", *claim.Confidence)
		}
		lines = append(lines, fmt.Sprintf("  - %s %s %s · %s%s · Intent %s revision %d", claim.SubjectKind, claim.Relation, clean(claim.NormalizedSubject), claim.ProvenanceKind, confidence, claim.IntentID, claim.IntentRevision))
	}
	lines = append(lines, "\nProposals (non-binding):")
	switch {
	case m.semantic.proposalsFor != collision.CollisionID || m.semantic.proposalsLoading:
		lines = append(lines, "  loading...")
	case m.semantic.proposalsErr != nil:
		lines = append(lines, "  could not load: "+clean(m.semantic.proposalsErr.Error()))
	case len(m.semantic.proposals) == 0:
		lines = append(lines, "  none recorded")
	}
	if m.semantic.proposalsFor == collision.CollisionID {
		for _, proposal := range m.semantic.proposals {
			author := "human"
			if proposal.ProposedByParticipantID != nil {
				author = "participant " + proposal.ProposedByParticipantID.String()
			}
			lines = append(lines, fmt.Sprintf("  - %s %s by %s: %s", proposal.ProposalID, proposal.Kind, author, firstLine(proposal.RationaleMarkdown, 120)))
		}
	}
	return strings.Join(lines, "\n")
}

// parseAssumptions reads one assumption per non-empty line.
func parseAssumptions(text string) ([]string, error) {
	var assumptions []string
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if utf8.RuneCountInString(line) > 1000 {
			return nil, errors.New("Each assumption must be at most 1000 characters.")
		}
		assumptions = append(assumptions, line)
	}
	if len(assumptions) > 50 {
		return nil, errors.New("An Intent can declare at most 50 assumptions.")
	}
	return assumptions, nil
}

// parseClaims reads one declared claim per non-empty line:
// kind relation subject [display name]. A claim matching an existing one
// keeps its evidence references and confidence.
func parseClaims(text string, existing []protocol.SemanticClaimCandidate) ([]protocol.SemanticClaimCandidate, error) {
	var claims []protocol.SemanticClaimCandidate
	for i, line := range strings.Split(text, "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		if len(fields) < 3 {
			return nil, fmt.Errorf("Claim line %d needs: kind relation subject [display name].", i+1)
		}
		kind, relation, subject := fields[0], fields[1], fields[2]
		if !slices.Contains(claimKinds, kind) {
			return nil, fmt.Errorf("Claim line %d: %q is not a claim kind.", i+1, kind)
		}
		if !slices.Contains(claimRelations, relation) {
			return nil, fmt.Errorf("Claim line %d: %q is not a relation.", i+1, relation)
		}
		if len(subject) > 512 || !subjectPattern.MatchString(subject) {
			return nil, fmt.Errorf("Claim line %d: subject must be namespaced, such as invariant:checkout:one-charge.", i+1)
		}
		name := strings.Join(fields[3:], " ")
		if name == "" {
			name = subject
		}
		if utf8.RuneCountInString(name) > 200 {
			name = string([]rune(name)[:200])
		}
		claim := protocol.SemanticClaimCandidate{SubjectKind: kind, Relation: relation, NormalizedSubject: subject, DisplayName: name, ProvenanceKind: "declared"}
		for _, previous := range existing {
			if previous.SubjectKind == kind && previous.Relation == relation && previous.NormalizedSubject == subject {
				claim.Confidence, claim.EvidenceRefs = previous.Confidence, previous.EvidenceRefs
			}
		}
		claims = append(claims, claim)
	}
	if len(claims) > 100 {
		return nil, errors.New("An Intent can declare at most 100 claims.")
	}
	return claims, nil
}

func formatClaims(claims []protocol.SemanticClaimCandidate) string {
	lines := make([]string, len(claims))
	for i, claim := range claims {
		lines[i] = strings.Join([]string{claim.SubjectKind, claim.Relation, claim.NormalizedSubject}, " ")
		if claim.DisplayName != claim.NormalizedSubject {
			lines[i] += " " + claim.DisplayName
		}
	}
	return strings.Join(lines, "\n")
}

// parseCriteria reads "key: statement" lines into acceptance criteria.
func parseCriteria(text string, kept []protocol.KeyPoint) ([]protocol.KeyPoint, error) {
	seen := map[string]bool{}
	for _, point := range kept {
		seen[point.Key] = true
	}
	var criteria []protocol.KeyPoint
	for i, line := range strings.Split(text, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		key, statement, found := strings.Cut(line, ":")
		key, statement = strings.TrimSpace(key), strings.TrimSpace(statement)
		if !found || statement == "" {
			return nil, fmt.Errorf("Criterion line %d needs: key: statement.", i+1)
		}
		if len(key) > 64 || !keyPattern.MatchString(key) {
			return nil, fmt.Errorf("Criterion line %d: key %q must be a lowercase slug such as retry-safe.", i+1, key)
		}
		if seen[key] {
			return nil, fmt.Errorf("Criterion line %d: key %q is used twice.", i+1, key)
		}
		if utf8.RuneCountInString(statement) > 1000 {
			return nil, fmt.Errorf("Criterion line %d is longer than 1000 characters.", i+1)
		}
		seen[key] = true
		criteria = append(criteria, protocol.KeyPoint{Key: key, Kind: protocol.KeyPointAcceptanceCriterion, Statement: statement})
	}
	if len(kept)+len(criteria) > 50 {
		return nil, errors.New("An Objective can have at most 50 key points.")
	}
	return criteria, nil
}

// parseReferences reads up to 20 comma-separated opaque references.
func parseReferences(text string) ([]string, error) {
	references := splitList(text)
	if len(references) > 20 {
		return nil, errors.New("Record at most 20 references.")
	}
	for _, reference := range references {
		if len(reference) > 256 {
			return nil, errors.New("Each reference must be at most 256 characters.")
		}
	}
	return references, nil
}

func splitList(text string) []string {
	var items []string
	for _, item := range strings.Split(text, ",") {
		if item = strings.TrimSpace(item); item != "" {
			items = append(items, item)
		}
	}
	return items
}

func firstLine(text string, limit int) string {
	line, _, _ := strings.Cut(strings.TrimSpace(text), "\n")
	line = clean(line)
	if utf8.RuneCountInString(line) > limit {
		return string([]rune(line)[:limit-1]) + "…"
	}
	return line
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func onOff(enabled bool) string {
	if enabled {
		return "on"
	}
	return "off"
}
