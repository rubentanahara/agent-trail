package tui

import (
	"fmt"
	"image/color"
	"path/filepath"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/rubentanahara/agent-trail/internal/trail"
)

const (
	borderAndTitle       = 2
	minLeftWidth         = 30
	maxLeftWidth         = 50
	leftPercent          = 32
	eventsPercent        = 55
	minPaneHeight        = 5
	minBoxHeight         = 3
	infoPaneRows         = 15
	hideLeftBelow        = 70
	whoWidth             = 16
	kindWidth            = 10
	timeLayout           = "01-02 15:04:05"
	shortIDLength        = 8
	footerHeight         = 1
	ellipsis             = "…"
	activeMarker         = "▶ "
	inactiveMarker       = "  "
	runningGlyph         = "● "
	closedGlyph          = "○ "
	unknownGlyph         = "◌ "
	finishedGlyph        = "✓ "
	middleBranch         = "├ "
	lastBranch           = "└ "
	minimumWidth         = 20
	minimumHeight        = 8
	tooSmallMessage      = "terminal too small"
	billingNotRecorded   = "not recorded"
	everyRepositoryLabel = "every repository"
)

var (
	cliColours = map[trail.CLI]string{trail.Claude: "208", trail.Codex: "40", trail.Copilot: "135"}

	promptStyle   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("51"))
	failedStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("196"))
	faintStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
	apiStyle      = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("203"))
	runningStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("42"))
	selectedStyle = lipgloss.NewStyle().Reverse(true)
	matchStyle    = lipgloss.NewStyle().Bold(true).Underline(true).Foreground(lipgloss.Color("226"))
	matchOnSelect = matchStyle.Reverse(true)
	titleStyle    = lipgloss.NewStyle().Bold(true)
	labelStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
	focusColour   = lipgloss.Color("75")
	idleColour    = lipgloss.Color("240")
)

type geometry struct {
	showLeft  bool
	leftW     int
	rightW    int
	sessionsH int
	infoH     int
	eventsH   int
	detailH   int
}

func (m model) layout() geometry {
	if m.zoomed {
		return m.zoomedLayout()
	}
	bodyH := m.height - footerHeight
	g := geometry{showLeft: m.width >= hideLeftBelow, rightW: m.width}
	if g.showLeft {
		g.leftW = max(minLeftWidth, min(m.width*leftPercent/100, maxLeftWidth))
		g.rightW = m.width - g.leftW
		g.sessionsH, g.infoH = split(bodyH, min(infoPaneRows, bodyH/2))
	}
	g.eventsH, g.detailH = split(bodyH, bodyH-max(minPaneHeight, bodyH*eventsPercent/100))
	return g
}

func (m model) zoomedLayout() geometry {
	bodyH := m.height - footerHeight
	switch m.focus {
	case sessionsPane:
		return geometry{showLeft: true, leftW: m.width, sessionsH: bodyH}
	case eventsPane:
		return geometry{rightW: m.width, eventsH: bodyH}
	}
	return geometry{rightW: m.width, detailH: bodyH}
}

func split(total, lower int) (int, int) {
	if lower < minBoxHeight {
		return total, 0
	}
	return total - lower, lower
}

func (m model) paneAt(x, y int) pane {
	g := m.layout()
	switch {
	case g.showLeft && x < g.leftW:
		return sessionsPane
	case y < g.eventsH:
		return eventsPane
	default:
		return detailPane
	}
}

func (m model) View() tea.View {
	view := tea.NewView(m.render())
	view.AltScreen = true
	view.MouseMode = tea.MouseModeCellMotion
	return view
}

func (m model) render() string {
	if m.width < minimumWidth || m.height < minimumHeight {
		return tooSmallMessage
	}
	if m.statsScreen.shown {
		return m.renderStats()
	}
	g := m.layout()
	right := stack(
		paneBox{m.eventsTitle(), m.eventRows(g.rightW-2, g.eventsH-borderAndTitle-1), g.rightW, g.eventsH, m.borderFor(eventsPane)}.render(),
		paneBox{m.detailTitle(), strings.Split(m.detail.View(), "\n"), g.rightW, g.detailH, m.borderFor(detailPane)}.render(),
	)
	body := right
	if g.showLeft {
		left := stack(
			paneBox{m.sessionsTitle(), m.sessionRows(g.leftW-2, g.sessionsH-borderAndTitle-1), g.leftW, g.sessionsH, m.borderFor(sessionsPane)}.render(),
			paneBox{m.infoTitle(), m.infoRows(), g.leftW, g.infoH, idleColour}.render(),
		)
		body = lipgloss.JoinHorizontal(lipgloss.Top, left, right)
	}
	return lipgloss.JoinVertical(lipgloss.Left, body, m.footerLine(m.footer()))
}

func (m model) footerLine(text string) string {
	return spread(text, faintStyle.Render("agent-trail "+m.context.Version), m.width)
}

func stack(boxes ...string) string {
	var drawn []string
	for _, b := range boxes {
		if b != "" {
			drawn = append(drawn, b)
		}
	}
	return lipgloss.JoinVertical(lipgloss.Left, drawn...)
}

type paneBox struct {
	title  string
	rows   []string
	width  int
	height int
	border color.Color
}

func (m model) borderFor(target pane) color.Color {
	if m.focus == target {
		return focusColour
	}
	return idleColour
}

func (b paneBox) render() string {
	title, rows, width, height := b.title, b.rows, b.width, b.height
	innerW, innerH := width-2, height-2
	if innerW <= 0 || innerH <= 0 {
		return ""
	}
	lines := make([]string, 0, innerH)
	lines = append(lines, fit(titleStyle.Render(title), innerW))
	for _, row := range rows {
		if len(lines) == innerH {
			break
		}
		lines = append(lines, fit(row, innerW))
	}
	for len(lines) < innerH {
		lines = append(lines, strings.Repeat(" ", innerW))
	}
	return lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(b.border).Render(strings.Join(lines, "\n"))
}

func fit(line string, width int) string {
	line = ansi.Truncate(line, width, ellipsis)
	return line + strings.Repeat(" ", max(0, width-ansi.StringWidth(line)))
}

func cliStyle(cli trail.CLI) lipgloss.Style {
	return lipgloss.NewStyle().Foreground(lipgloss.Color(cliColours[cli]))
}

func (m model) sessionsTitle() string {
	title := fmt.Sprintf("Sessions (%d)", len(m.order)) + filterBadge(m.filterText[sessionsPane], "filter")
	if m.runningOnly {
		title += promptStyle.Render(" · running only")
	}
	if m.focus == sessionsPane {
		title += faintStyle.Render(" ↑↓ pick · enter events · c continue · x delete")
	}
	return title
}

func (m model) eventsTitle() string {
	scope, state := "all sessions", fmt.Sprintf("%d running", m.runningCount())
	if session, ok := m.sessions[m.activeSession]; ok {
		scope, state = sessionLabel(session), scopeState(session)
	}
	scroll := " · auto-scroll"
	if !m.follow {
		scroll = " · paused (f resumes)"
	}
	return fmt.Sprintf("Events: %s (%d) · %s%s", scope, len(m.visible), state, scroll) + filterBadge(m.filterText[eventsPane], "filter")
}

func (m model) runningCount() int {
	running := 0
	for _, key := range m.order {
		if session := m.sessions[key]; !m.isChild(session) && session.Liveness == trail.LivenessRunning {
			running++
		}
	}
	return running
}

func scopeState(session trail.Session) string {
	switch session.Liveness {
	case trail.LivenessRunning:
		return runningStyle.Render(runningGlyph + "running" + optional(" · ", session.Activity))
	case trail.LivenessClosed:
		return closedGlyph + "closed, no new events expected"
	}
	return unknownGlyph + "last event " + age(session.Last) + " ago" + optional(" · ", session.Activity)
}

func age(at time.Time) string {
	elapsed := time.Since(at)
	switch {
	case elapsed < time.Minute:
		return fmt.Sprintf("%ds", max(0, int(elapsed.Seconds())))
	case elapsed < time.Hour:
		return fmt.Sprintf("%dm", int(elapsed.Minutes()))
	case elapsed < 24*time.Hour:
		return fmt.Sprintf("%dh", int(elapsed.Hours()))
	}
	return fmt.Sprintf("%dd", int(elapsed.Hours()/24))
}

func (m model) eventRows(width, rows int) []string {
	out := make([]string, 0, max(0, rows))
	for row := m.eventTop; row < len(m.visible) && len(out) < rows; row++ {
		event := m.events[m.visible[row]]
		stamp := event.Time.Local().Format(timeLayout) + " "
		if row == m.eventCursor {
			body := selectedStyle.Render(fit(plainEventBody(event), width-len(stamp)))
			out = append(out, selectedStyle.Render(stamp)+m.filter.highlight(body, matchOnSelect))
			continue
		}
		out = append(out, faintStyle.Render(stamp)+m.filter.highlight(styledEventBody(event), matchStyle))
	}
	return out
}

func plainEventBody(event trail.Event) string {
	return fmt.Sprintf("%s %s %s", fit(event.Who(), whoWidth), fit(string(event.Kind), kindWidth), event.Summary)
}

func styledEventBody(event trail.Event) string {
	who := cliStyle(event.CLI).Render(fit(event.Who(), whoWidth))
	kind := fit(string(event.Kind), kindWidth)
	summary := event.Summary
	switch {
	case event.Kind == trail.KindUser:
		kind, summary = promptStyle.Render(kind), promptStyle.Render(summary)
	case event.Kind == trail.KindUsage:
		kind, summary = faintStyle.Render(kind), faintStyle.Render(summary)
	case strings.HasPrefix(summary, trail.FailedPrefix):
		summary = failedStyle.Render(summary)
	default:
		kind = cliStyle(event.CLI).Render(kind)
	}
	return who + " " + kind + " " + summary
}

func (m model) sessionRows(width, rows int) []string {
	all := fmt.Sprintf("%sAll sessions", marker(m.activeSession == ""))
	lines := []string{spread(all, fmt.Sprintf("%d ev", len(m.events)), width)}
	for row, key := range m.order {
		lines = append(lines, spread(m.sessionLabelRow(row, key), faintStyle.Render(fmt.Sprintf("%4s ", age(m.familyLast(key))))+sessionMetric(m.familySession(key)), width))
	}
	out := make([]string, 0, max(0, rows))
	for row := m.sessionTop; row < len(lines) && len(out) < rows; row++ {
		line := lines[row]
		if row == m.sessionCursor && m.focus == sessionsPane {
			line = m.sessionQuery.highlight(selectedStyle.Render(fit(ansi.Strip(line), width)), matchOnSelect)
		}
		out = append(out, line)
	}
	return out
}

func (m model) sessionLabelRow(row int, key string) string {
	session := m.sessions[key]
	name := m.sessionQuery.highlight(sessionLabel(session), matchStyle)
	if !m.isChild(session) {
		return marker(key == m.activeSession) + cliStyle(session.CLI).Render(stateGlyph(session)) + name + m.projectHint(session)
	}
	return marker(key == m.activeSession) + faintStyle.Render(m.branchGlyph(row)) + cliStyle(session.CLI).Render(stateGlyph(session)) + name + faintStyle.Render(" · "+session.Agent)
}

func (m model) branchGlyph(row int) string {
	next := row + 1
	if next < len(m.order) && m.sessions[m.order[next]].Parent == m.sessions[m.order[row]].Parent {
		return middleBranch
	}
	return lastBranch
}

func (m model) projectHint(session trail.Session) string {
	if m.context.Repo != trail.EveryRepository || session.Cwd == "" {
		return ""
	}
	return faintStyle.Render(" · " + filepath.Base(session.Cwd))
}

func (m model) familySession(key string) trail.Session {
	merged := m.sessions[key]
	for _, child := range m.children[key] {
		merged = merged.WithUsageOf(m.sessions[child])
	}
	return merged
}

func marker(active bool) string {
	if active {
		return activeMarker
	}
	return inactiveMarker
}

func spread(left, right string, width int) string {
	room := width - ansi.StringWidth(right) - 1
	if room <= 0 {
		return fit(left, width)
	}
	return fit(left, room) + " " + right
}

func sessionLabel(session trail.Session) string {
	if session.Name != "" {
		return session.Name
	}
	return string(session.CLI) + " " + shortID(session.ID)
}

func shortID(id string) string {
	if len(id) > shortIDLength {
		return id[:shortIDLength]
	}
	return id
}

func stateGlyph(session trail.Session) string {
	if session.Finished && session.Liveness != trail.LivenessRunning {
		return finishedGlyph
	}
	switch session.Liveness {
	case trail.LivenessRunning:
		return runningGlyph
	case trail.LivenessClosed:
		return closedGlyph
	}
	return unknownGlyph
}

func (m model) detailTitle() string {
	title := "Detail (r: raw JSON)"
	if m.raw {
		title = "Detail · raw JSON (r: text)"
	}
	return title + filterBadge(m.filterText[detailPane], "matching lines")
}

func filterBadge(text, what string) string {
	if text == "" {
		return ""
	}
	return promptStyle.Render(" · " + what + ": " + text)
}

func (m *model) refreshDetail() {
	g := m.layout()
	m.detail.SetWidth(max(1, g.rightW-2))
	m.detail.SetHeight(max(1, g.detailH-borderAndTitle-1))
	m.detail.SetContent(m.detailContent())
}

func (m model) detailContent() string {
	if m.detailErr != nil {
		return failedStyle.Render(m.detailErr.Error())
	}
	if m.detailShown == noEvent || m.detailShown >= len(m.events) {
		return faintStyle.Render("no event selected")
	}
	if m.raw {
		return m.matchingLines(colorJSON(m.detailData.Raw))
	}
	event := m.events[m.detailShown]
	header := fmt.Sprintf("%s · %s · %s", event.Time.Local().Format(time.DateTime), cliStyle(event.CLI).Render(event.Who()), event.Kind)
	if session, ok := m.sessions[event.Session]; ok {
		header += "\n" + faintStyle.Render("session "+sessionLabel(session)+" ("+session.ID+")")
	}
	return header + "\n\n" + m.matchingLines(m.detailData.Text)
}

func (m model) matchingLines(text string) string {
	if len(m.detailQuery) == 0 {
		return text
	}
	var kept []string
	for _, line := range strings.Split(text, "\n") {
		if m.detailQuery.matches(strings.ToLower(ansi.Strip(line))) {
			kept = append(kept, m.detailQuery.highlight(line, matchStyle))
		}
	}
	if len(kept) == 0 {
		return faintStyle.Render("no line matches")
	}
	return strings.Join(kept, "\n")
}

func zoomAction(zoomed bool) string {
	if zoomed {
		return "restore"
	}
	return "zoom"
}

func shownOrHidden(shown bool) string {
	if shown {
		return "hide"
	}
	return "show"
}

func (m model) footer() string {
	switch {
	case m.searching:
		return m.search.View()
	case m.confirmDelete != "":
		return apiStyle.Render(fmt.Sprintf("Delete %q and its transcript files permanently? y deletes · any other key cancels", sessionLabel(m.sessions[m.confirmDelete])))
	case m.notice != "":
		return promptStyle.Render(m.notice)
	}
	status := faintStyle.Render(strings.Join([]string{"1 2 3/tab panes", "/ filter pane", "c continue session", "z " + zoomAction(m.zoomed), "o running only", "m " + shownOrHidden(m.hideAutomated) + " automated", "t stats", "enter open", "s event's session", "a all", "n " + shownOrHidden(!m.hideNoise) + " noise", "u usage", "r raw", "f follow", "q quit"}, "  "))
	if text := m.filterText[m.focus]; text != "" {
		status = promptStyle.Render("filter: "+text+" (esc clears)") + "  " + status
	}
	return status
}
