package tui

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/rubentanahara/agent-trail/internal/gh"
	"github.com/rubentanahara/agent-trail/internal/trail"
)

const (
	prSinglePaneBelow = 70
	prWideFrom        = 100
	prListPercentWide = 38
	prListPercentMid  = 30
	prMinListWidth    = 24
	prHint            = "↑↓ move  [ ] tab  enter detail  o open  R refresh  r reply  e edit  v resolve  M merge  X close  A review  L log  E re-run  c line  / filter  ? keys  p back"
	noPullRequests    = "No open pull requests"
	clockLayout       = "15:04"
	truncationMark    = "→"
)

func (m model) prGeometry() (listW, detailW, bodyH int, single bool) {
	bodyH = m.height - footerHeight - headerHeight
	if m.width < prSinglePaneBelow || m.pr.zoomed {
		return m.width, m.width, bodyH, true
	}
	percent := prListPercentMid
	if m.width >= prWideFrom {
		percent = prListPercentWide
	}
	listW = max(prMinListWidth, m.width*percent/100)
	return listW, m.width - listW, bodyH, false
}

func (m model) renderPRs() string {
	listW, detailW, bodyH, single := m.prGeometry()
	list := paneBox{m.prListTitle(), m.prListRows(listW-2, bodyH-borderAndTitle-1), listW, bodyH, filesBorder(m.pr.focus == prListFocus)}.render()
	detail := paneBox{m.prTabStrip(), m.prDetailRows(detailW-2, bodyH-borderAndTitle-1), detailW, bodyH, filesBorder(m.pr.focus == prDetailFocus)}.render()
	body := lipgloss.JoinHorizontal(lipgloss.Top, list, detail)
	if single {
		body = list
		if m.pr.focus == prDetailFocus {
			body = detail
		}
	}
	return lipgloss.JoinVertical(lipgloss.Left, fit(m.prHeader(), m.width), body, m.footerLine(m.prFooter()))
}

func (m model) renderPRScreen() string {
	if m.pr.log != nil {
		return m.renderLog()
	}
	return m.renderPRs()
}

func (m model) prListTitle() string {
	return "Pull requests (" + countText(len(m.pr.sections.All)) + ")"
}

func (m model) prHeader() string {
	p := m.pr
	if m.width < prSinglePaneBelow {
		return m.compactHeader()
	}
	header := titleStyle.Render("Pull requests")
	if p.repo.FullName != "" {
		header += faintStyle.Render(" · " + p.repo.FullName)
	}
	if count := p.pendingCount(); count > 0 {
		header += waitingStyle.Render(fmt.Sprintf(" · Pending (%d)", count))
	}
	return header + faintStyle.Render(m.prFreshness())
}

func (m model) compactHeader() string {
	number := m.pr.selectedNumber()
	return titleStyle.Render(fmt.Sprintf("PR #%d", number)) + faintStyle.Render(fmt.Sprintf(" · tab %d/%d %s", m.pr.tab+1, prTabCount, prTabNames[m.pr.tab][0]))
}

func (m model) prFreshness() string {
	p := m.pr
	switch {
	case p.rateLimited(m.now):
		return " · rate limited · retry at " + p.pausedTill.Format(clockLayout)
	case p.list.busy && p.loaded:
		return " · refreshing…"
	case !p.loaded || m.now.IsZero() || p.lastOK.IsZero():
		return ""
	case m.now.Sub(p.lastOK) > prStaleAfter:
		return " · stale " + ageAt(p.lastOK, m.now)
	}
	return " · updated " + ageAt(p.lastOK, m.now) + " ago"
}

func (m model) prTabStrip() string {
	narrow := m.width < prWideFrom
	labels := make([]string, prTabCount)
	for tab := prTab(0); tab < prTabCount; tab++ {
		name := prTabNames[tab][0]
		if narrow {
			name = prTabNames[tab][1]
		}
		if tab == m.pr.tab {
			name = lipgloss.NewStyle().Reverse(true).Render(" " + name + " ")
		}
		labels[tab] = name
	}
	return strings.Join(labels, " ")
}

func (m model) prListRows(width, rows int) []string {
	p := m.pr
	switch {
	case p.hasProblem():
		return []string{failedStyle.Render(p.problem.Remedy()), faintStyle.Render(sanitize(errorText(p.failure)))}
	case !p.loaded:
		if !m.now.IsZero() && m.now.Sub(p.openedAt) < prLoadingAfter {
			return nil
		}
		return []string{faintStyle.Render(loadingText)}
	case len(p.rows) == 0:
		return []string{faintStyle.Render(noPullRequests)}
	}
	top := scrollTop(p.top, p.cursor, len(p.rows), rows)
	out := make([]string, 0, max(0, rows))
	for row := top; row < len(p.rows) && len(out) < rows; row++ {
		out = append(out, m.prRowText(row, width))
	}
	return out
}

func errorText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func (m model) prRowText(row, width int) string {
	entry := m.pr.rows[row]
	if entry.number == 0 {
		return titleStyle.Render(entry.header)
	}
	pr := m.pr.byNumber[entry.number]
	left := marker(row == m.pr.cursor) + fmt.Sprintf("#%d %s", pr.Number, sanitize(pr.Title))
	if pr.Author.Bot {
		left = faintStyle.Render(left + " [bot]")
	}
	line := spread(left, m.prRowBadges(pr), width)
	if row == m.pr.cursor && m.pr.focus == prListFocus {
		return selectedStyle.Render(fit(ansi.Strip(line), width))
	}
	return line
}

func (m model) prRowBadges(pr gh.PR) string {
	parts := []string{reviewGlyph(pr, m.pr.requests[pr.Number]), checksGlyph(pr)}
	if pr.Draft {
		parts = append(parts, "draft")
	}
	if pr.Mergeable == "CONFLICTING" {
		parts = append(parts, "⚠")
	}
	if count := len(m.prSessions(pr)); count > 0 {
		parts = append(parts, strconv.Itoa(count))
	}
	return strings.Join(parts, " ")
}

func reviewGlyph(pr gh.PR, requestedOfMe bool) string {
	switch {
	case pr.ReviewDecision == "APPROVED":
		return "✓"
	case pr.ReviewDecision == "CHANGES_REQUESTED":
		return "✗"
	case requestedOfMe:
		return "◐"
	}
	return "○"
}

func checksGlyph(pr gh.PR) string {
	switch {
	case len(pr.Checks) == 0:
		return "–"
	case pr.Failing():
		return "✗"
	case pr.Pending():
		return "●"
	}
	return "✓"
}

func (m model) prDetailRows(width, rows int) []string {
	p := m.pr
	if p.hasProblem() || !p.loaded {
		return nil
	}
	lines := m.prTabLines()
	cursor := min(p.tabCursor[p.tab], max(0, len(lines)-1))
	top := scrollTop(p.tabTop[p.tab], cursor, len(lines), rows)
	selectable := p.tab != overviewTab
	out := make([]string, 0, max(0, rows))
	for i := top; i < len(lines) && len(out) < rows; i++ {
		text := lines[i].text
		if lines[i].kind == diffBodyLine {
			text = ansi.Truncate(text, width, truncationMark)
		}
		if selectable && i == cursor && p.focus == prDetailFocus {
			text = selectedStyle.Render(fit(ansi.Strip(text), width))
		}
		out = append(out, text)
	}
	return out
}

func (m model) prFooter() string {
	switch {
	case m.pr.searching:
		return m.search.View()
	case m.pr.confirm != nil:
		return apiStyle.Render(m.confirmPrompt())
	case m.notice != "":
		return promptStyle.Render(m.notice)
	case m.pr.focusedFilter() != "":
		return promptStyle.Render("filter: " + m.pr.focusedFilter() + " (esc clears)")
	}
	hint := prHint
	if m.pr.pendingCount() > 0 {
		hint += "  S submit  D discard"
	}
	return faintStyle.Render(fit(hint, max(0, m.width-ansi.StringWidth("agent-trail "+m.context.Version)-1)))
}

func (m model) prOfSession(session trail.Session) (gh.PR, bool) {
	matches := m.prsOfSession(session)
	if len(matches) == 0 {
		return gh.PR{}, false
	}
	return matches[0], true
}

func (m model) prsOfSession(session trail.Session) []gh.PR {
	if session.Events == 0 {
		return nil
	}
	var matches []gh.PR
	for _, pr := range m.pr.byNumber {
		if pr.CrossRepo || pr.HeadRef == "" {
			continue
		}
		if slices.Contains(session.Branches, pr.HeadRef) || (len(session.Branches) == 0 && session.Branch == pr.HeadRef) {
			matches = append(matches, pr)
		}
	}
	slices.SortFunc(matches, func(a, b gh.PR) int { return b.UpdatedAt.Compare(a.UpdatedAt) })
	return matches
}

func (m model) prBadge(session trail.Session) string {
	matches := m.prsOfSession(session)
	if len(matches) == 0 {
		return ""
	}
	badge := fmt.Sprintf(" #%d", matches[0].Number)
	if len(matches) > 1 {
		badge += fmt.Sprintf("+%d", len(matches)-1)
	}
	if m.width >= prWideFrom {
		badge += " " + reviewGlyph(matches[0], false) + checksGlyph(matches[0])
	}
	return faintStyle.Render(badge)
}
