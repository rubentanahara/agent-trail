package tui

import (
	tea "charm.land/bubbletea/v2"

	"github.com/rubentanahara/agent-trail/internal/gh"
)

func (m *model) issuesKey(msg tea.KeyPressMsg) (tea.Cmd, bool) {
	key := msg.String()
	s := &m.issues
	if s.searching {
		return m.issueSearchKey(msg), false
	}
	switch key {
	case "q", "ctrl+c":
		return nil, true
	case "?":
		m.help.shown = true
	case "i":
		m.closeIssues()
	case "esc":
		m.issueEscape()
	case "tab", "shift+tab":
		s.focus = 1 - s.focus
	case "1":
		s.focus = prListFocus
	case "2":
		s.focus = prDetailFocus
	case "[":
		s.tab = (s.tab + issueTabCount - 1) % issueTabCount
	case "]":
		s.tab = (s.tab + 1) % issueTabCount
	case "P":
		return m.cycleIssueView(), false
	case "R":
		return m.refreshIssues(), false
	case "z":
		s.zoomed = !s.zoomed
	case "/":
		return m.startIssueSearch(), false
	case "o":
		return m.openIssueURL(), false
	default:
		return m.issueNavigate(key), false
	}
	return nil, false
}

func (m *model) issueEscape() {
	s := &m.issues
	switch {
	case s.filterText != "":
		m.applyIssueFilter("")
	case s.focus == prDetailFocus:
		s.focus = prListFocus
	default:
		m.closeIssues()
	}
}

func (m *model) issueNavigate(key string) tea.Cmd {
	if m.issues.focus == prListFocus {
		return m.issueListKey(key)
	}
	m.issueDetailKey(key)
	return nil
}

func (m *model) issueListKey(key string) tea.Cmd {
	switch key {
	case "up", "k":
		return m.moveIssue(-1)
	case "down", "j":
		return m.moveIssue(1)
	case "pgup":
		return m.moveIssue(-pageRows)
	case "pgdown":
		return m.moveIssue(pageRows)
	case "home", "g":
		return m.moveIssue(-len(m.issues.rows))
	case "end", "G":
		return m.moveIssue(len(m.issues.rows))
	case "enter", "right", "l":
		m.issues.focus = prDetailFocus
	}
	return nil
}

func (m *model) moveIssue(delta int) tea.Cmd {
	s := &m.issues
	step := sign(delta)
	next := s.cursor
	for candidate := s.cursor + step; candidate >= 0 && candidate < len(s.rows); candidate += step {
		if s.rows[candidate].number == 0 {
			continue
		}
		next = candidate
		if abs(candidate-s.cursor) >= abs(delta) {
			break
		}
	}
	if next == s.cursor {
		return nil
	}
	s.cursor = next
	return m.fetchIssueDetail()
}

func (m *model) issueDetailKey(key string) {
	s := &m.issues
	last := max(0, len(m.issueTabLines())-1)
	cursor := &s.tabCursor[s.tab]
	switch key {
	case "up", "k":
		*cursor = max(0, *cursor-1)
	case "down", "j":
		*cursor = min(last, *cursor+1)
	case "pgup":
		*cursor = max(0, *cursor-pageRows)
	case "pgdown":
		*cursor = min(last, *cursor+pageRows)
	case "home", "g":
		*cursor = 0
	case "end", "G":
		*cursor = last
	case "left", "h":
		s.focus = prListFocus
	}
}

func (m *model) openIssueURL() tea.Cmd {
	s := &m.issues
	issue, ok := s.selected()
	if !ok {
		return nil
	}
	if s.focus == prDetailFocus {
		if url := m.issueCursorURL(issue); url != "" {
			return m.openURL(url)
		}
	}
	return m.openURL(issue.URL)
}

func (m model) issueCursorURL(issue gh.Issue) string {
	lines := m.issueTabLines()
	cursor := m.issues.tabCursor[m.issues.tab]
	if cursor < 0 || cursor >= len(lines) || lines[cursor].ref < 0 {
		return ""
	}
	detail := m.issues.details[issue.Number].detail
	ref := lines[cursor].ref
	switch m.issues.tab {
	case issueCommentsTab:
		if ref < len(detail.Comments) {
			return detail.Comments[ref].URL
		}
	case issueLinkedTab:
		if refs := issueLinkedRefs(detail); ref < len(refs) {
			return refs[ref].URL
		}
	}
	return ""
}

func (m *model) startIssueSearch() tea.Cmd {
	m.issues.searching = true
	m.search.SetValue(m.issues.filterText)
	m.search.CursorEnd()
	return m.search.Focus()
}

func (m *model) issueSearchKey(msg tea.KeyPressMsg) tea.Cmd {
	switch msg.String() {
	case "enter":
		m.issues.searching = false
		m.search.Blur()
		return nil
	case "esc":
		m.issues.searching = false
		m.search.Blur()
		m.search.SetValue("")
	}
	var cmd tea.Cmd
	m.search, cmd = m.search.Update(msg)
	m.applyIssueFilter(m.search.Value())
	return cmd
}

func (m *model) applyIssueFilter(text string) {
	s := &m.issues
	if s.filterText == text {
		return
	}
	s.filterText, s.filter = text, parseQuery(text)
	s.rebuildRows()
}
