package tui

import (
	"fmt"
	"strconv"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/rubentanahara/agent-trail/internal/gh"
)

const (
	issueHint     = "↑↓ move  [ ] tab  enter detail/PR  P project  o open  R refresh  / filter  z zoom  ? keys  i back"
	noIssues      = "No issues"
	issueFieldPad = 11
)

var issueTabNames = [issueTabCount][2]string{
	{"Overview", "Ovw"},
	{"Comments", "Cmt"},
	{"Linked", "Lnk"},
}

func (m model) issueGeometry() (listW, detailW, bodyH int, single bool) {
	bodyH = m.height - footerHeight - headerHeight
	if m.width < prSinglePaneBelow || m.issues.zoomed {
		return m.width, m.width, bodyH, true
	}
	percent := prListPercentMid
	if m.width >= prWideFrom {
		percent = prListPercentWide
	}
	listW = max(prMinListWidth, m.width*percent/100)
	return listW, m.width - listW, bodyH, false
}

func (m model) renderIssues() string {
	listW, detailW, bodyH, single := m.issueGeometry()
	list := paneBox{m.issueListTitle(), m.issueListRows(listW-2, bodyH-borderAndTitle-1), listW, bodyH, filesBorder(m.issues.focus == prListFocus)}.render()
	detail := paneBox{m.issueTabStrip(), m.issueDetailRows(detailW-2, bodyH-borderAndTitle-1), detailW, bodyH, filesBorder(m.issues.focus == prDetailFocus)}.render()
	body := lipgloss.JoinHorizontal(lipgloss.Top, list, detail)
	if single {
		body = list
		if m.issues.focus == prDetailFocus {
			body = detail
		}
	}
	return lipgloss.JoinVertical(lipgloss.Left, fit(m.issueHeader(), m.width), body, m.footerLine(m.issueFooter()))
}

func (m model) issueListTitle() string {
	s := m.issues
	if !s.loaded {
		return "Issues"
	}
	title := s.viewTitle() + " (" + strconv.Itoa(len(s.viewIssues()))
	if total := s.viewTotal(); total > len(s.viewIssues()) {
		title += " of " + strconv.Itoa(total)
	}
	return title + ")"
}

func (m model) issueHeader() string {
	s := m.issues
	header := titleStyle.Render("Issues")
	if s.repo.FullName != "" {
		header += faintStyle.Render(" · " + s.repo.FullName)
	}
	if s.loaded && s.viewCount() > 1 {
		header += faintStyle.Render(fmt.Sprintf(" · view %d/%d", s.view+1, s.viewCount()))
	}
	switch {
	case s.list.busy && s.loaded:
		header += faintStyle.Render(" · refreshing…")
	case s.loaded && !m.now.IsZero():
		header += faintStyle.Render(" · updated " + ageAt(s.lastOK, m.now) + " ago")
	}
	return header
}

func (m model) issueTabStrip() string {
	narrow := m.width < prWideFrom
	labels := make([]string, issueTabCount)
	for tab := issueTab(0); tab < issueTabCount; tab++ {
		name := issueTabNames[tab][0]
		if narrow {
			name = issueTabNames[tab][1]
		}
		if tab == m.issues.tab {
			name = lipgloss.NewStyle().Reverse(true).Render(" " + name + " ")
		}
		labels[tab] = name
	}
	return strings.Join(labels, " ")
}

func (m model) issueFooter() string {
	switch {
	case m.issues.searching:
		return m.search.View()
	case m.notice != "":
		return promptStyle.Render(m.notice)
	case m.issues.filterText != "":
		return promptStyle.Render("filter: " + m.issues.filterText + " (esc clears)")
	}
	return faintStyle.Render(fit(issueHint, max(0, m.width-ansi.StringWidth("agent-trail "+m.context.Version)-1)))
}

func (m model) issueListRows(width, rows int) []string {
	s := m.issues
	switch {
	case s.hasProblem():
		return []string{failedStyle.Render(s.problem.Remedy()), faintStyle.Render(sanitize(errorText(s.failure)))}
	case !s.loaded:
		if !m.now.IsZero() && m.now.Sub(s.openedAt) < prLoadingAfter {
			return nil
		}
		return []string{faintStyle.Render(loadingText)}
	case len(s.rows) == 0:
		return []string{faintStyle.Render(noIssues)}
	}
	top := scrollTop(s.top, s.cursor, len(s.rows), rows)
	out := make([]string, 0, max(0, rows))
	for row := top; row < len(s.rows) && len(out) < rows; row++ {
		out = append(out, m.issueRowText(row, width))
	}
	return out
}

func (m model) issueRowText(row, width int) string {
	entry := m.issues.rows[row]
	if entry.number == 0 {
		return titleStyle.Render(entry.header)
	}
	issue := m.issues.byNumber[entry.number]
	glyph, _, style := issueLook(issue.State)
	left := marker(row == m.issues.cursor) + style.Render(glyph) + fmt.Sprintf(" #%d %s", issue.Number, sanitize(issue.Title))
	line := spread(left, issueBadges(issue), width)
	if row == m.issues.cursor && m.issues.focus == prListFocus {
		return selectedStyle.Render(fit(ansi.Strip(line), width))
	}
	return line
}

func issueBadges(issue gh.Issue) string {
	var parts []string
	if priority := issue.Field("Priority"); priority != "" {
		parts = append(parts, sanitize(priority))
	}
	if issue.SubTotal > 0 {
		parts = append(parts, fmt.Sprintf("%d/%d", issue.SubDone, issue.SubTotal))
	}
	if len(issue.Assignees) > 0 {
		parts = append(parts, "@"+sanitize(issue.Assignees[0]))
	}
	return strings.Join(parts, " ")
}

func issueLook(state string) (string, string, lipgloss.Style) {
	switch state {
	case "OPEN":
		return "○", "open", runningStyle
	case "MERGED":
		return "✓", "merged", runningStyle
	}
	return "✓", "closed", faintStyle
}

func (m model) issueDetailRows(width, rows int) []string {
	s := m.issues
	if s.hasProblem() || !s.loaded {
		return nil
	}
	lines := m.issueTabLines()
	cursor := min(s.tabCursor[s.tab], max(0, len(lines)-1))
	top := scrollTop(s.tabTop[s.tab], cursor, len(lines), rows)
	selectable := s.tab != issueOverviewTab
	out := make([]string, 0, max(0, rows))
	for i := top; i < len(lines) && len(out) < rows; i++ {
		text := lines[i].text
		if selectable && i == cursor && s.focus == prDetailFocus {
			text = selectedStyle.Render(fit(ansi.Strip(text), width))
		}
		out = append(out, text)
	}
	return out
}

func (m model) issueDetailWidth() int {
	_, detailW, _, _ := m.issueGeometry()
	return max(1, detailW-2)
}

func (m model) issueTabLines() []prLine {
	issue, ok := m.issues.selected()
	if !ok {
		return plainLines(faintStyle.Render("No issue selected"))
	}
	data, loaded := m.issues.details[issue.Number]
	switch m.issues.tab {
	case issueCommentsTab:
		return m.issueCommentLines(data.detail, loaded)
	case issueLinkedTab:
		return issueLinkedLines(issue, data.detail, loaded)
	}
	return m.issueOverviewLines(issue, data.detail, loaded)
}

func (m model) issueOverviewLines(issue gh.Issue, detail gh.IssueDetail, loaded bool) []prLine {
	_, word, _ := issueLook(issue.State)
	lines := plainLines(
		titleStyle.Render(fmt.Sprintf("#%d %s", issue.Number, sanitize(issue.Title))),
		faintStyle.Render(word+" · by "+sanitize(issue.Author)+" · opened"+m.agoText(issue.CreatedAt)+" · updated"+m.agoText(issue.UpdatedAt)),
	)
	if m.issues.inProject() {
		for _, field := range issue.Fields {
			lines = append(lines, issueFieldLine(field.Name, sanitize(field.Value)))
		}
	}
	lines = append(lines, issueFieldLine("Assignees", listOrNone(issue.Assignees)), issueFieldLine("Labels", listOrNone(issue.Labels)))
	if issue.Milestone != "" {
		lines = append(lines, issueFieldLine("Milestone", sanitize(issue.Milestone)))
	}
	if issue.ParentNumber != 0 {
		lines = append(lines, issueFieldLine("Parent", fmt.Sprintf("#%d %s", issue.ParentNumber, sanitize(issue.ParentTitle))))
	}
	if issue.SubTotal > 0 {
		lines = append(lines, issueFieldLine("Sub-issues", fmt.Sprintf("%d of %d done", issue.SubDone, issue.SubTotal)))
	}
	lines = append(lines, issueFieldLine("Comments", strconv.Itoa(issue.Comments)))
	lines = append(lines, plainLines("")...)
	return append(lines, plainLines(m.issueBody(detail, loaded)...)...)
}

func issueFieldLine(name, value string) prLine {
	return plainLines(labelStyle.Render(fmt.Sprintf("%-*s", issueFieldPad, name)) + value)[0]
}

func listOrNone(items []string) string {
	if len(items) == 0 {
		return faintStyle.Render("none")
	}
	return sanitize(strings.Join(items, ", "))
}

func (m model) issueBody(detail gh.IssueDetail, loaded bool) []string {
	switch {
	case !loaded:
		return []string{faintStyle.Render(loadingText)}
	case strings.TrimSpace(detail.Body) == "":
		return []string{faintStyle.Render("No description provided")}
	}
	return strings.Split(m.markdownIn(m.issues.markdown, sanitizeBody(detail.Body), m.issueDetailWidth()), "\n")
}

func (m model) issueCommentLines(detail gh.IssueDetail, loaded bool) []prLine {
	if !loaded {
		return plainLines(faintStyle.Render(loadingText))
	}
	if len(detail.Comments) == 0 {
		return plainLines(faintStyle.Render("No comments yet"))
	}
	var lines []prLine
	if detail.CommentTotal > len(detail.Comments) {
		lines = plainLines(faintStyle.Render(fmt.Sprintf("showing %d of %d comments", len(detail.Comments), detail.CommentTotal)))
	}
	for i, comment := range detail.Comments {
		heading := sanitize(comment.Author) + " commented" + m.agoText(comment.CreatedAt)
		lines = append(lines, bodyLines(heading, comment.Body, m.issueDetailWidth(), i, -1, sourceComment)...)
	}
	return lines
}

func issueLinkedRefs(detail gh.IssueDetail) []gh.IssueRef {
	return append(append([]gh.IssueRef{}, detail.ClosedBy...), detail.SubIssues...)
}

func issueLinkedLines(issue gh.Issue, detail gh.IssueDetail, loaded bool) []prLine {
	if !loaded {
		return plainLines(faintStyle.Render(loadingText))
	}
	refs := issueLinkedRefs(detail)
	if len(refs) == 0 && issue.ParentNumber == 0 {
		return plainLines(faintStyle.Render("No linked pull requests or sub-issues"))
	}
	var lines []prLine
	if issue.ParentNumber != 0 {
		lines = append(lines, plainLines(titleStyle.Render("Parent"), fmt.Sprintf("  #%d %s", issue.ParentNumber, sanitize(issue.ParentTitle)), "")...)
	}
	lines = append(lines, issueRefSection("Closed by pull requests", detail.ClosedBy, 0)...)
	return append(lines, issueRefSection("Sub-issues", detail.SubIssues, len(detail.ClosedBy))...)
}

func issueRefSection(title string, refs []gh.IssueRef, firstRef int) []prLine {
	if len(refs) == 0 {
		return nil
	}
	lines := plainLines(titleStyle.Render(title))
	for i, ref := range refs {
		glyph, word, style := issueLook(ref.State)
		text := style.Render(glyph+" "+fmt.Sprintf("%-6s", word)) + fmt.Sprintf(" #%d %s", ref.Number, sanitize(ref.Title))
		lines = append(lines, prLine{text: text, ref: firstRef + i, sub: -1, file: -1})
	}
	return append(lines, plainLines("")...)
}
