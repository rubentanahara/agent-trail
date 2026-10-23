package tui

import (
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/rubentanahara/agent-trail/internal/trail"
)

const (
	borderAndTitle   = 2
	minLeftWidth     = 30
	maxLeftWidth     = 50
	leftPercent      = 32
	eventsPercent    = 55
	minPaneHeight    = 5
	minBoxHeight     = 3
	infoRows         = 15
	hideLeftBelow    = 70
	whoWidth         = 16
	kindWidth        = 10
	timeLayout       = "01-02 15:04:05"
	shortIDLength    = 8
	footerHeight     = 1
	ellipsis         = "…"
	activeMarker     = "▶ "
	inactiveMarker   = "  "
	sessionDot       = "● "
	minimumWidth     = 20
	minimumHeight    = 8
	tooSmallMessage  = "terminal too small"
	unknownCostLabel = "?"
)

var (
	cliColours = map[trail.CLI]string{trail.Claude: "208", trail.Codex: "40", trail.Copilot: "135"}

	promptStyle   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("51"))
	failedStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("196"))
	faintStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
	apiStyle      = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("203"))
	selectedStyle = lipgloss.NewStyle().Reverse(true)
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
	bodyH := m.height - footerHeight
	g := geometry{showLeft: m.width >= hideLeftBelow, rightW: m.width}
	if g.showLeft {
		g.leftW = max(minLeftWidth, min(m.width*leftPercent/100, maxLeftWidth))
		g.rightW = m.width - g.leftW
		g.sessionsH, g.infoH = split(bodyH, min(infoRows, bodyH/2))
	}
	g.eventsH, g.detailH = split(bodyH, bodyH-max(minPaneHeight, bodyH*eventsPercent/100))
	return g
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
	g := m.layout()
	right := stack(
		box(m.eventsTitle(), m.eventRows(g.rightW-2, g.eventsH-borderAndTitle-1), g.rightW, g.eventsH, m.focus == eventsPane),
		box(m.detailTitle(), strings.Split(m.detail.View(), "\n"), g.rightW, g.detailH, m.focus == detailPane),
	)
	body := right
	if g.showLeft {
		left := stack(
			box(fmt.Sprintf("Sessions (%d)", len(m.order)), m.sessionRows(g.leftW-2, g.sessionsH-borderAndTitle-1), g.leftW, g.sessionsH, m.focus == sessionsPane),
			box(m.infoTitle(), m.infoRows(g.leftW-2), g.leftW, g.infoH, false),
		)
		body = lipgloss.JoinHorizontal(lipgloss.Top, left, right)
	}
	return lipgloss.JoinVertical(lipgloss.Left, body, fit(m.footer(), m.width))
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

func box(title string, rows []string, width, height int, focused bool) string {
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
	border := idleColour
	if focused {
		border = focusColour
	}
	return lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(border).Render(strings.Join(lines, "\n"))
}

func fit(line string, width int) string {
	line = ansi.Truncate(line, width, ellipsis)
	return line + strings.Repeat(" ", max(0, width-ansi.StringWidth(line)))
}

func cliStyle(cli trail.CLI) lipgloss.Style {
	return lipgloss.NewStyle().Foreground(lipgloss.Color(cliColours[cli]))
}

func (m model) eventsTitle() string {
	scope := "all sessions"
	if session, ok := m.sessions[m.activeSession]; ok {
		scope = sessionLabel(session)
	}
	follow := ""
	if m.follow {
		follow = " · following"
	}
	return fmt.Sprintf("Events: %s (%d)%s", scope, len(m.visible), follow)
}

func (m model) eventRows(width, rows int) []string {
	out := make([]string, 0, rows)
	for row := m.eventTop; row < len(m.visible) && len(out) < rows; row++ {
		event := m.events[m.visible[row]]
		if row == m.eventCursor {
			out = append(out, selectedStyle.Render(fit(plainEventRow(event), width)))
			continue
		}
		out = append(out, styledEventRow(event))
	}
	return out
}

func plainEventRow(event trail.Event) string {
	return fmt.Sprintf("%s %s %s %s", event.Time.Local().Format(timeLayout), fit(event.Who(), whoWidth), fit(string(event.Kind), kindWidth), event.Summary)
}

func styledEventRow(event trail.Event) string {
	stamp := faintStyle.Render(event.Time.Local().Format(timeLayout))
	who := cliStyle(event.CLI).Render(fit(event.Who(), whoWidth))
	kind := fit(string(event.Kind), kindWidth)
	summary := event.Summary
	switch {
	case event.Kind == trail.KindUser:
		kind, summary = promptStyle.Render(kind), promptStyle.Render(summary)
	case event.Kind == trail.KindUsage:
		kind, summary = faintStyle.Render(kind), faintStyle.Render(summary)
	case strings.HasPrefix(summary, "FAILED"):
		summary = failedStyle.Render(summary)
	default:
		kind = cliStyle(event.CLI).Render(kind)
	}
	return stamp + " " + who + " " + kind + " " + summary
}

func (m model) sessionRows(width, rows int) []string {
	all := fmt.Sprintf("%sAll sessions", marker(m.activeSession == ""))
	lines := []string{spread(all, fmt.Sprintf("%d ev", len(m.events)), width)}
	for _, key := range m.order {
		session := m.sessions[key]
		label := marker(key == m.activeSession) + cliStyle(session.CLI).Render(sessionDot) + sessionLabel(session)
		lines = append(lines, spread(label, sessionMetric(session), width))
	}
	out := make([]string, 0, rows)
	for row := m.sessionTop; row < len(lines) && len(out) < rows; row++ {
		line := lines[row]
		if row == m.sessionCursor && m.focus == sessionsPane {
			line = selectedStyle.Render(fit(ansi.Strip(line), width))
		}
		out = append(out, line)
	}
	return out
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

func sessionMetric(session trail.Session) string {
	switch session.CLI {
	case trail.Claude:
		if session.PricedCalls == 0 {
			return trail.Tokens(session.Tokens.Output) + " out"
		}
		if session.Billing == trail.BillingAPI {
			return apiStyle.Render(trail.Dollars(session.Tokens.CostUSD) + " API")
		}
		return "~" + trail.Dollars(session.Tokens.CostUSD)
	case trail.Codex:
		if session.Billing == trail.BillingAPI {
			return apiStyle.Render("API " + trail.Tokens(session.Tokens.Output))
		}
		if session.HasRateLimit {
			return fmt.Sprintf("%.0f%% lim", session.RateLimitPercent)
		}
	case trail.Copilot:
		if session.Billing == trail.BillingAPI {
			return apiStyle.Render("BYOK")
		}
		if session.PremiumRequests > 0 {
			return fmt.Sprintf("%g req", session.PremiumRequests)
		}
	}
	return trail.Tokens(session.Tokens.Output) + " out"
}

func (m model) infoTitle() string {
	if session, ok := m.sessions[m.sessionKeyAt(m.sessionCursor)]; ok {
		return "Session " + shortID(session.ID)
	}
	return "Totals"
}

func (m model) infoRows(width int) []string {
	session, ok := m.sessions[m.sessionKeyAt(m.sessionCursor)]
	if !ok {
		return m.totalRows()
	}
	rows := []string{
		field("Name", sessionLabel(session)),
		field("CLI", string(session.CLI)+optional(" · agent ", session.Agent)),
		field("Subagents", strings.Join(session.Subagents, ", ")),
		field("Model", session.Model),
		field("Branch", session.Branch),
		field("Mode", session.Permission),
		field("Billing", billingLabel(session)),
		field("Active", activeSpan(session)),
		field("Tokens", tokenLine(session.Tokens)),
		field("Cost", costLine(session)),
		field("Limits", limitsLine(session)),
		field("Lines", linesLine(session)),
		field("Events", fmt.Sprintf("%d · %d model calls", session.Events, session.Calls)),
	}
	return nonEmpty(rows)
}

func (m model) totalRows() []string {
	var tokens trail.Usage
	var apiCost, equivalentCost, premium float64
	perCLI := map[trail.CLI]int{}
	for _, key := range m.order {
		session := m.sessions[key]
		perCLI[session.CLI]++
		tokens.Input += session.Tokens.Input
		tokens.CacheRead += session.Tokens.CacheRead
		tokens.CacheWrite += session.Tokens.CacheWrite
		tokens.Output += session.Tokens.Output
		premium += session.PremiumRequests
		if session.Billing == trail.BillingAPI {
			apiCost += session.Tokens.CostUSD
		} else {
			equivalentCost += session.Tokens.CostUSD
		}
	}
	return nonEmpty([]string{
		field("Repo", m.repo),
		field("Sessions", fmt.Sprintf("claude %d · codex %d · copilot %d", perCLI[trail.Claude], perCLI[trail.Codex], perCLI[trail.Copilot])),
		field("Tokens", tokenLine(tokens)),
		field("API billed", nonZeroDollars(apiCost)),
		field("Subscription", nonZeroDollars(equivalentCost)+optional("", ifPositive(equivalentCost, " API-equivalent"))),
		field("Copilot", ifPositive(premium, fmt.Sprintf("%g premium requests", premium))),
		field("Events", fmt.Sprintf("%d", len(m.events))),
		field("Prices", faintStyle.Render("Claude API list "+trail.ClaudePricesAsOf)),
	})
}

func field(label, value string) string {
	if strings.TrimSpace(value) == "" {
		return ""
	}
	return labelStyle.Render(fmt.Sprintf("%-9s", label)) + " " + value
}

func nonEmpty(rows []string) []string {
	out := rows[:0]
	for _, row := range rows {
		if row != "" {
			out = append(out, row)
		}
	}
	return out
}

func optional(prefix, value string) string {
	if value == "" {
		return ""
	}
	return prefix + value
}

func ifPositive(amount float64, text string) string {
	if amount <= 0 {
		return ""
	}
	return text
}

func nonZeroDollars(amount float64) string {
	return ifPositive(amount, trail.Dollars(amount))
}

func billingLabel(session trail.Session) string {
	switch session.Billing {
	case trail.BillingAPI:
		return apiStyle.Render("API key" + optional(" · ", session.Plan))
	case trail.BillingSubscription:
		return "subscription" + optional(" · ", session.Plan)
	}
	return unknownCostLabel
}

func activeSpan(session trail.Session) string {
	if session.Start.IsZero() {
		return ""
	}
	return fmt.Sprintf("%s → %s (%s)", session.Start.Local().Format("01-02 15:04"), session.Last.Local().Format("15:04"), session.Last.Sub(session.Start).Round(time.Minute))
}

func tokenLine(u trail.Usage) string {
	if u.Input == 0 && u.Output == 0 {
		return ""
	}
	return fmt.Sprintf("in %s (cache %s/%s) · out %s", trail.Tokens(u.Input), trail.Tokens(u.CacheRead), trail.Tokens(u.CacheWrite), trail.Tokens(u.Output)) +
		optional(" · think ", tokensIfPositive(u.Reasoning))
}

func tokensIfPositive(n int64) string {
	if n <= 0 {
		return ""
	}
	return trail.Tokens(n)
}

func costLine(session trail.Session) string {
	if session.PricedCalls == 0 {
		return ""
	}
	cost := trail.Dollars(session.Tokens.CostUSD)
	if session.PricedCalls < session.Calls {
		cost += fmt.Sprintf(" (%d calls unpriced)", session.Calls-session.PricedCalls)
	}
	if session.Billing == trail.BillingAPI {
		return apiStyle.Render(cost + " billed to API key")
	}
	return "~" + cost + " API-equivalent"
}

func limitsLine(session trail.Session) string {
	var parts []string
	if session.HasRateLimit {
		parts = append(parts, fmt.Sprintf("rate limit %.0f%% used", session.RateLimitPercent))
	}
	if session.HasContext {
		parts = append(parts, fmt.Sprintf("context %.0f%%", session.ContextPercent))
	}
	if session.PremiumRequests > 0 {
		parts = append(parts, fmt.Sprintf("%g premium requests", session.PremiumRequests))
	}
	return strings.Join(parts, " · ")
}

func linesLine(session trail.Session) string {
	if session.LinesAdded == 0 && session.LinesRemoved == 0 {
		return ""
	}
	return fmt.Sprintf("+%d −%d", session.LinesAdded, session.LinesRemoved)
}

func (m model) detailTitle() string {
	if m.raw {
		return "Detail · raw JSON (r: text)"
	}
	return "Detail (r: raw JSON)"
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
		return m.detailData.Raw
	}
	event := m.events[m.detailShown]
	header := fmt.Sprintf("%s · %s · %s", event.Time.Local().Format(time.DateTime), cliStyle(event.CLI).Render(event.Who()), event.Kind)
	if session, ok := m.sessions[event.Session]; ok {
		header += "\n" + faintStyle.Render("session "+sessionLabel(session)+" ("+session.ID+")")
	}
	return header + "\n\n" + m.detailData.Text
}

func (m model) footer() string {
	if m.searching {
		return m.search.View()
	}
	status := []string{"tab pane", "/ filter", "enter open", "s event's session", "a all", "u usage", "r raw", "f follow", "q quit"}
	if query := m.search.Value(); query != "" {
		status = append([]string{promptStyle.Render("filter: " + query + " (esc clears)")}, status...)
	}
	return faintStyle.Render(strings.Join(status, "  "))
}
