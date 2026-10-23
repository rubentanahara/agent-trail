package tui

import (
	"os/exec"
	"runtime"
	"strings"
	"unicode"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/rubentanahara/agent-trail/internal/gh"
)

const browserOpenerDarwin = "open"

func openInBrowser(url string) error {
	opener := "xdg-open"
	if runtime.GOOS == "darwin" {
		opener = browserOpenerDarwin
	}
	return exec.Command(opener, url).Start()
}

func (m *model) openURL(url string) tea.Cmd {
	if url == "" {
		m.notice = "nothing to open"
		return nil
	}
	open := m.pr.open
	return func() tea.Msg { return prOpenedMsg{err: open(url)} }
}

func (m *model) prKey(msg tea.KeyPressMsg) (tea.Cmd, bool) {
	key := msg.String()
	p := &m.pr
	if p.searching {
		return m.prSearchKey(msg), false
	}
	if p.log != nil {
		return m.logKey(msg)
	}
	if p.confirm != nil {
		return m.prConfirmKey(key), false
	}
	if cmd, handled := m.prActionKey(key); handled {
		return cmd, false
	}
	switch key {
	case "q", "ctrl+c":
		return nil, true
	case "?":
		m.help.shown = true
	case "p":
		m.closePRs()
		m.remindPending()
	case "esc":
		m.prEscape()
	case "tab", "shift+tab":
		p.focus = 1 - p.focus
	case "1":
		p.focus = prListFocus
	case "2":
		p.focus = prDetailFocus
	case "[":
		return m.shiftTab(-1), false
	case "]":
		return m.shiftTab(1), false
	case "R":
		return m.refreshPRs(), false
	case "z":
		p.zoomed = !p.zoomed
	case "/":
		return m.startPRSearch(), false
	default:
		return m.prNavigate(key), false
	}
	return nil, false
}

func (m *model) prEscape() {
	p := &m.pr
	switch {
	case p.focusedFilter() != "":
		m.applyPRFilter("")
	case p.focus == prDetailFocus:
		p.focus = prListFocus
	default:
		m.closePRs()
		m.remindPending()
	}
}

func (m *model) shiftTab(delta int) tea.Cmd {
	p := &m.pr
	p.tab = (p.tab + prTab(delta) + prTabCount) % prTabCount
	return m.fetchTabData()
}

func (m *model) prNavigate(key string) tea.Cmd {
	p := &m.pr
	if key == "o" {
		return m.openSelected()
	}
	if p.focus == prListFocus {
		return m.prListKey(key)
	}
	return m.prDetailKey(key)
}

func (m *model) prListKey(key string) tea.Cmd {
	switch key {
	case "up", "k":
		return m.movePR(-1)
	case "down", "j":
		return m.movePR(1)
	case "pgup":
		return m.movePR(-pageRows)
	case "pgdown":
		return m.movePR(pageRows)
	case "home", "g":
		return m.movePR(-len(m.pr.rows))
	case "end", "G":
		return m.movePR(len(m.pr.rows))
	case "enter", "right", "l":
		m.pr.focus = prDetailFocus
	}
	return nil
}

func (m *model) prDetailKey(key string) tea.Cmd {
	p := &m.pr
	lines := m.prTabLines()
	cursor := &p.tabCursor[p.tab]
	switch key {
	case "up", "k":
		*cursor = max(0, *cursor-1)
	case "down", "j":
		*cursor = min(len(lines)-1, *cursor+1)
	case "pgup":
		*cursor = max(0, *cursor-pageRows)
	case "pgdown":
		*cursor = max(0, min(len(lines)-1, *cursor+pageRows))
	case "home", "g":
		*cursor = 0
	case "end", "G":
		*cursor = max(0, len(lines)-1)
	case "left", "h":
		p.focus = prListFocus
	case "n":
		m.stepFile(1)
	case "N":
		m.stepFile(-1)
	case "enter":
		return m.activateLine()
	}
	return nil
}

func (m *model) openSelected() tea.Cmd {
	p := &m.pr
	pr, ok := p.selected()
	if !ok {
		return nil
	}
	if p.focus == prDetailFocus {
		if url := m.cursorURL(pr); url != "" {
			return m.openURL(url)
		}
	}
	return m.openURL(pr.URL)
}

func (m model) cursorURL(pr gh.PR) string {
	line, found := m.cursorLine()
	if !found || line.ref < 0 {
		return ""
	}
	switch {
	case m.pr.tab == checksTab && line.ref < len(pr.Checks):
		return pr.Checks[line.ref].URL
	case m.pr.tab == commentsTab && line.src == sourceComment && line.ref < len(pr.Comments):
		return pr.Comments[line.ref].URL
	case m.pr.tab == commentsTab && line.src == sourceThread:
		return m.threadCommentURL(line)
	}
	return ""
}

func (m model) threadCommentURL(line prLine) string {
	thread, found := m.cursorThread()
	if !found || len(thread.Comments) == 0 {
		return ""
	}
	return thread.Comments[max(0, line.sub)].URL
}

func (m *model) startPRSearch() tea.Cmd {
	m.pr.searching = true
	m.search.SetValue(m.pr.focusedFilter())
	m.search.CursorEnd()
	return m.search.Focus()
}

func (m *model) prSearchKey(msg tea.KeyPressMsg) tea.Cmd {
	switch msg.String() {
	case "enter":
		m.pr.searching = false
		m.search.Blur()
		return nil
	case "esc":
		m.pr.searching = false
		m.search.Blur()
		m.search.SetValue("")
	}
	var cmd tea.Cmd
	m.search, cmd = m.search.Update(msg)
	m.applyPRFilter(m.search.Value())
	return cmd
}

func (m *model) applyPRFilter(text string) {
	p := &m.pr
	if p.focusedFilter() == text {
		return
	}
	if p.focus == prDetailFocus {
		p.detailFilterText, p.detailFilter = text, parseQuery(text)
		p.tabCursor[p.tab], p.tabTop[p.tab] = 0, 0
		return
	}
	p.filterText, p.filter = text, parseQuery(text)
	p.rebuildRows()
}

func sanitize(text string) string {
	return withoutControls(ansi.Strip(text))
}

func sanitizeBody(text string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r == '\n':
			return r
		case r == '\t':
			return ' '
		case unicode.IsControl(r):
			return -1
		}
		return r
	}, ansi.Strip(text))
}
