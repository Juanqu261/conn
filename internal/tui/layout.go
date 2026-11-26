package tui

// Layout fits every screen to the terminal. Text wider than the terminal
// wraps, footers wrap between their key hints, and a card body taller than
// the space left scrolls, so the header and footer always stay on screen and
// nothing is cut off.

import (
	"fmt"
	"strings"
	"uuid"

	"github.com/charmbracelet/lipgloss"
)

// listKind names the list a page shows instead of a card.
type listKind int

const (
	noList listKind = iota
	selectorList
	projectsList
)

// page is one screen's content before it is fitted to the terminal.
type page struct {
	title, body string
	footer      string   // key hints separated by " · "
	list        listKind // a list page shows a list instead of a card
}

// cursorMarker starts the selected row of the in-card lists, so scrolling
// can keep that row on screen.
const cursorMarker = "▸ "

func (m model) View() string { return m.render(m.page()) }

// render lays out a page: the header, a card or a list, and the footer, in at
// most the terminal's rows.
func (m model) render(p page) string {
	header, footer := m.header(), m.footer(p.footer)
	if p.list != noList {
		view := m.selector.View()
		if p.list == projectsList {
			view = m.projectList.View()
		}
		// The list's own help line can pass its width.
		parts := []string{header, "", lipgloss.NewStyle().MaxWidth(max(1, m.width)).Render(view)}
		if err := m.listError(); err != "" {
			parts = append(parts, err)
		}
		return strings.Join(append(parts, "", footer), "\n")
	}
	return strings.Join([]string{header, "", m.card(p.title, p.body, m.cardRows(p)), "", footer}, "\n")
}

func (m model) cardWidth() int { return min(100, max(20, m.width-2)) }

// textWidth is the width of the text inside a card's border and padding.
func (m model) textWidth() int { return m.cardWidth() - 4 }

// cardRows is how many rows the card of p may take.
func (m model) cardRows(p page) int {
	return m.height - lipgloss.Height(m.header()) - lipgloss.Height(m.footer(p.footer)) - 2
}

// listRows is how many rows a list page leaves its list.
func (m model) listRows(p page) int {
	rows := m.height - lipgloss.Height(m.header()) - lipgloss.Height(m.footer(p.footer)) - 2
	if err := m.listError(); err != "" {
		rows -= lipgloss.Height(err)
	}
	return max(4, rows)
}

// bodyRows is how many of a card's rows are left for its body: roomy with
// the card's vertical padding and a blank row under the title, tight
// without them.
func (m model) bodyRows(title string, cardRows int) (roomy, tight int) {
	titleRows := lipgloss.Height(cardTitleStyle.Width(m.textWidth()).Render(title))
	// The border takes two rows, the padding two, and the blank row one.
	return cardRows - 5 - titleRows, cardRows - 2 - titleRows
}

// bodyLines wraps a card body to the card and returns its lines and how many
// rows are left for them. A body taller than the card first takes the rows
// of the card's vertical padding and the blank row under the title.
func (m model) bodyLines(title, body string, cardRows int) (lines []string, rows int, compact bool) {
	lines = wrapBody(body, m.textWidth())
	roomy, tight := m.bodyRows(title, cardRows)
	if len(lines) > roomy {
		return lines, tight, true
	}
	return lines, roomy, false
}

// wrapBody wraps each line of body to width. A wrapped list item continues
// under its text rather than at the margin, so lists stay readable.
func wrapBody(body string, width int) []string {
	var lines []string
	for _, line := range strings.Split(body, "\n") {
		if lipgloss.Width(line) <= width {
			lines = append(lines, line)
			continue
		}
		prefix := listPrefix(line)
		indent := lipgloss.Width(prefix)
		if indent > width/2 {
			prefix, indent = "", 0
		}
		wrapped := strings.Split(lipgloss.NewStyle().Width(width-indent).Render(strings.TrimPrefix(line, prefix)), "\n")
		lines = append(lines, prefix+wrapped[0])
		for _, rest := range wrapped[1:] {
			lines = append(lines, strings.Repeat(" ", indent)+rest)
		}
	}
	return lines
}

// listPrefix is a line's leading spaces and list marker, if any.
func listPrefix(line string) string {
	rest := strings.TrimLeft(line, " ")
	for _, marker := range []string{"- ", cursorMarker} {
		if strings.HasPrefix(rest, marker) {
			rest = strings.TrimPrefix(rest, marker)
			break
		}
	}
	return line[:len(line)-len(rest)]
}

// maxScroll is the largest useful scroll offset for a body. When the body
// does not fit, its last row shows a scroll hint instead of text.
func maxScroll(lines, rows int) int {
	if lines <= rows {
		return 0
	}
	return lines - max(1, rows-1)
}

// card renders a titled card in at most cardRows rows. A body that does not
// fit shows the rows from the scroll offset and a hint to scroll for more.
func (m model) card(title, body string, cardRows int) string {
	width := m.textWidth()
	lines, rows, compact := m.bodyLines(title, body, cardRows)
	if len(lines) > rows {
		visible := max(1, rows-1)
		offset := max(0, min(m.scroll, len(lines)-visible))
		lines = append(lines[offset:offset+visible:offset+visible], scrollHint(offset, visible, len(lines), width))
	}
	style, gap := cardStyle, "\n\n"
	if compact {
		style, gap = cardStyle.Padding(0, 2), "\n"
	}
	return style.Width(m.cardWidth()).Render(cardTitleStyle.Width(width).Render(title) + gap + strings.Join(lines, "\n"))
}

func scrollHint(offset, visible, total, width int) string {
	arrow := "↕"
	switch {
	case offset == 0:
		arrow = "↓"
	case offset+visible >= total:
		arrow = "↑"
	}
	hint := fmt.Sprintf("%s lines %d-%d of %d · PgUp/PgDn scroll", arrow, offset+1, offset+visible, total)
	if lipgloss.Width(hint) > width {
		hint = fmt.Sprintf("%s %d-%d/%d PgUp/PgDn", arrow, offset+1, offset+visible, total)
	}
	return footerStyle.Render(truncate(hint, width))
}

// header shows the product, the project, and the section tabs, wrapping the
// tabs when the terminal is too narrow for one row.
func (m model) header() string {
	width := max(1, m.width)
	role := ""
	project := "No project selected"
	if m.selectedProjectID() != (uuid.UUID{}) {
		project, role = clean(m.selected.Project.Name), " · "+string(m.selected.Role)
	}
	project = truncate(project, width-lipgloss.Width("CONN  "+role)) + role
	tabs := make([]string, len(sections))
	for i, name := range sections {
		label := fmt.Sprintf("%d %s", i+1, name)
		if section(i) == m.section {
			tabs[i] = activeTabStyle.Render(label)
		} else {
			tabs[i] = tabStyle.Render(label)
		}
	}
	return brandStyle.Render("CONN") + "  " + projectStyle.Render(project) + "\n" + wrapItems(tabs, " ", "", width)
}

// footer wraps key hints between items so none is cut off.
func (m model) footer(hints string) string {
	width := max(1, m.width)
	items := strings.Split(strings.TrimSpace(hints), " · ")
	return footerStyle.Width(width).Render(wrapItems(items, " · ", "  ", width))
}

// listError shows the last error under a list, such as a failed load after
// a choice.
func (m model) listError() string {
	if m.err == nil {
		return ""
	}
	return errorStyle.PaddingLeft(2).Width(max(3, m.width)).Render(clean(m.err.Error()))
}

// wrapItems joins items with sep, starting a new line that begins with
// indent whenever the next item would pass width.
func wrapItems(items []string, sep, indent string, width int) string {
	var lines []string
	line := ""
	for _, item := range items {
		switch {
		case line == "":
			line = indent + item
		case lipgloss.Width(line+sep+item) > width:
			lines, line = append(lines, line), indent+item
		default:
			line += sep + item
		}
	}
	if line != "" {
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n")
}

// truncate cuts plain text to at most width terminal cells, ending with an
// ellipsis when it cuts.
func truncate(text string, width int) string {
	if lipgloss.Width(text) <= width {
		return text
	}
	if width <= 0 {
		return ""
	}
	var b strings.Builder
	used := 0
	for _, r := range text {
		w := lipgloss.Width(string(r))
		if used+w > width-1 {
			break
		}
		b.WriteRune(r)
		used += w
	}
	return b.String() + "…"
}

// position is what a scroll offset belongs to. When it changes, the body
// scrolls back to the top, or to the selected row.
type position struct {
	screen                                   screen
	section                                  section
	choosing, help                           bool
	intentCursor, cellCursor, radarCollision int
}

func (m model) position() position {
	return position{m.screen, m.section, m.choosing, m.help, m.intentCursor, m.cellCursor, m.radarCollision}
}

// scrollStep is how far a key scrolls the card body on this screen, if it
// scrolls at all. PgUp and PgDn scroll every card; the arrow keys scroll
// only where they do nothing else.
func (m model) scrollStep(key string) (int, bool) {
	p := m.page()
	if p.list != noList || m.screen == contextScreen { // lists and the context viewer scroll themselves
		return 0, false
	}
	_, rows, _ := m.bodyLines(p.title, p.body, m.cardRows(p))
	switch key {
	case "pgdown":
		return max(1, rows-2), true
	case "pgup":
		return -max(1, rows-2), true
	case "down":
		return 1, m.arrowsScroll()
	case "up":
		return -1, m.arrowsScroll()
	}
	return 0, false
}

func (m model) arrowsScroll() bool {
	switch m.screen {
	case normalScreen:
		return !m.choosing
	case projectConfirmScreen, memberConfirmScreen, objectiveConfirmScreen, blueprintConfirmScreen, charterActionScreen,
		charterConfirmScreen, repoConfirmScreen, intentConfirmScreen, intentReleaseScreen, evidenceConfirmScreen,
		analysisConfirmScreen, proposalConfirmScreen, decisionConfirmScreen, importConfirmScreen:
		return true
	}
	return false
}

// fit sizes the lists, the text fields, and the context viewer to the space
// the current screen leaves them, and keeps the scroll offset in range. When
// moved, the body starts at the top, or at the selected row of an in-card
// list.
func (m model) fit(moved bool) model {
	width := m.textWidth()
	if m.area.Width() != width-2 {
		m.area.SetWidth(width - 2)
	}
	m.input.Width = max(8, width-lipgloss.Width(m.input.Prompt)-1)
	if m.viewer.Width != width-2 {
		m.viewer.Width = width - 2
		m.wrapContext()
	}
	p := m.page()
	if p.list != noList {
		rows := m.listRows(p)
		listWidth := max(20, m.width-2)
		m.projectList.SetSize(listWidth, rows)
		m.selector.SetSize(listWidth, rows)
		// The help line's padding is not counted in its width, and it can add
		// one more key than fits, so leave it room.
		m.projectList.Help.Width, m.selector.Help.Width = listWidth-4, listWidth-4
		return m
	}
	cardRows := m.cardRows(p)
	lines, rows, _ := m.bodyLines(p.title, p.body, cardRows)
	// A text field or the context viewer takes the rows the rest of the body
	// leaves, keeping two for an error so the field does not jump when one
	// appears.
	if field, ok := m.fieldHeight(); ok {
		reserve := 2
		if m.err != nil || m.formNote != "" {
			reserve = 0
		}
		roomy, tight := m.bodyRows(p.title, cardRows)
		rest := len(lines) - field
		height := roomy - rest - reserve
		if height < 3 {
			height = max(3, tight-rest-reserve)
		}
		if height != field {
			m.setFieldHeight(height)
			lines, rows, _ = m.bodyLines(p.title, m.page().body, cardRows)
		}
	}
	if moved {
		m.scroll = 0
		if inputScreen(m.screen) {
			// A field follows its instructions; keep it on a short terminal.
			m.scroll = maxScroll(len(lines), rows)
		}
		for i, line := range lines {
			if strings.HasPrefix(line, cursorMarker) {
				// Bring the selected row to the top when it is out of view.
				if i >= max(1, rows-1) {
					m.scroll = i
				}
				break
			}
		}
	}
	m.scroll = max(0, min(m.scroll, maxScroll(len(lines), rows)))
	return m
}

// inputScreen reports a form step whose card ends in a text field.
func inputScreen(s screen) bool {
	switch s {
	case projectCreateScreen, memberEntryScreen, objectiveTitleScreen, cellTitleScreen, charterRationaleScreen, repoRemoteScreen,
		evidenceLocatorScreen, evidenceSummaryScreen, analysisWorktreeScreen, analysisScriptScreen, analysisPathsScreen,
		proposalReferencesScreen, decisionReferencesScreen, importSourceScreen, importTitleScreen:
		return true
	}
	return multiline(s)
}

func (m model) fieldHeight() (int, bool) {
	switch {
	case m.screen == contextScreen:
		return m.viewer.Height, true
	case multiline(m.screen):
		return m.area.Height(), true
	}
	return 0, false
}

func (m *model) setFieldHeight(height int) {
	if m.screen == contextScreen {
		m.viewer.Height = height
		return
	}
	m.area.SetHeight(height)
}

// wrapContext fits the context pack to the viewer's width. The viewer only
// truncates long lines, which would hide the end of the pack.
func (m *model) wrapContext() {
	if m.semantic.contextText == "" {
		return
	}
	m.viewer.SetContent(lipgloss.NewStyle().Width(max(1, m.viewer.Width)).Render(cleanBlock(m.semantic.contextText)))
}
