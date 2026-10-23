package tui

import (
	"bytes"
	"context"
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/rubentanahara/agent-trail/internal/gh"
)

const (
	logHint            = "↑↓ scroll  pgup/pgdn page  a failed/full  / search  n/N next match  esc back"
	logInProgressText  = "The run is still in progress, so its logs are not available yet. Press a or R later."
	logNoFailedText    = "No failed steps in this job. Press a for the full log."
	logTruncatedNotice = "last 5000 lines"
)

type prLog struct {
	ref        gh.LogRef
	name       string
	number     int
	failedOnly bool
	lines      []string
	truncated  bool
	loaded     bool
	message    string
	top        int
	searching  bool
	text       string
	query      query
	matches    []int
	current    int
}

type prLogMsg struct {
	seq, generation int
	lines           []string
	truncated       bool
	inProgress      bool
	err             error
}

func (prLogMsg) isPRMessage() {}

func (m *model) openLog() tea.Cmd {
	pr, ok := m.pr.selected()
	check, found := m.cursorCheck(pr)
	if !ok || !found {
		m.notice = "select a check on the Checks tab to read its log"
		return nil
	}
	ref, isActions := gh.ParseCheckURL(check.URL)
	if !isActions {
		m.notice = "logs exist only for GitHub Actions checks; o opens this one in the browser"
		return nil
	}
	m.pr.log = &prLog{ref: ref, name: check.Name, number: pr.Number, failedOnly: true}
	return m.fetchLog()
}

func (m *model) fetchLog() tea.Cmd {
	p := &m.pr
	log := p.log
	log.loaded, log.message, log.lines, log.top = false, "", nil, 0
	seq := p.logSlot.begin()
	return fetchPRLog(p.readContext(), p.run, seq, p.generation, log.ref, log.failedOnly)
}

func fetchPRLog(ctx context.Context, run gh.Runner, seq, generation int, ref gh.LogRef, failedOnly bool) tea.Cmd {
	return func() tea.Msg {
		readCtx, cancel := context.WithTimeout(ctx, gh.WriteTimeout)
		defer cancel()
		out, err := run(readCtx, gh.LogArgs(ref, failedOnly))
		msg := prLogMsg{seq: seq, generation: generation, err: err, inProgress: gh.IsRunInProgress(err)}
		if err == nil {
			msg.lines, msg.truncated, msg.err = gh.TailLog(bytes.NewReader(out), gh.LogTailLines)
		}
		return msg
	}
}

func (m *model) absorbLog(msg prLogMsg) {
	p := &m.pr
	if p.log == nil || !p.logSlot.accepts(msg.seq) || msg.generation != p.generation {
		return
	}
	p.logSlot.busy = false
	log := p.log
	log.loaded = true
	switch {
	case msg.inProgress:
		log.message = logInProgressText
	case msg.err != nil:
		log.message = "could not load the log: " + sanitize(msg.err.Error())
	case len(msg.lines) == 0:
		log.message = logNoFailedText
	default:
		log.lines, log.truncated = msg.lines, msg.truncated
		log.search(log.text)
	}
}

func (l *prLog) search(text string) {
	l.text, l.query, l.matches, l.current = text, parseQuery(text), nil, 0
	if text == "" {
		return
	}
	for i, line := range l.lines {
		if l.query.matches(strings.ToLower(line)) {
			l.matches = append(l.matches, i)
		}
	}
	l.showMatch()
}

func (l *prLog) showMatch() {
	if len(l.matches) > 0 {
		l.top = l.matches[l.current]
	}
}

func (l *prLog) stepMatch(delta int) {
	if len(l.matches) == 0 {
		return
	}
	l.current = (l.current + delta + len(l.matches)) % len(l.matches)
	l.showMatch()
}

func (m *model) logKey(msg tea.KeyPressMsg) (tea.Cmd, bool) {
	log := m.pr.log
	key := msg.String()
	if log.searching {
		return m.logSearchKey(msg), false
	}
	rows := m.logRows()
	switch key {
	case "q", "ctrl+c":
		return nil, true
	case "esc":
		m.pr.log = nil
		m.pr.logSlot.invalidate()
	case "a":
		log.failedOnly = !log.failedOnly
		return m.fetchLog(), false
	case "/":
		log.searching = true
		m.search.SetValue(log.text)
		m.search.CursorEnd()
		return m.search.Focus(), false
	case "n":
		log.stepMatch(1)
	case "N":
		log.stepMatch(-1)
	default:
		log.top = scrolledTop(log.top, key, len(log.lines), rows)
	}
	return nil, false
}

func scrolledTop(top int, key string, total, rows int) int {
	last := max(0, total-rows)
	switch key {
	case "up", "k":
		top--
	case "down", "j":
		top++
	case "pgup":
		top -= rows
	case "pgdown":
		top += rows
	case "home", "g":
		top = 0
	case "end", "G":
		top = last
	}
	return max(0, min(top, last))
}

func (m *model) logSearchKey(msg tea.KeyPressMsg) tea.Cmd {
	log := m.pr.log
	switch msg.String() {
	case "enter":
		log.searching = false
		m.search.Blur()
		return nil
	case "esc":
		log.searching = false
		m.search.Blur()
		m.search.SetValue("")
	}
	var cmd tea.Cmd
	m.search, cmd = m.search.Update(msg)
	log.search(m.search.Value())
	return cmd
}

func (m model) logRows() int {
	return max(1, m.height-footerHeight-borderAndTitle-1)
}

func (m model) renderLog() string {
	log := m.pr.log
	scope := "full log"
	if log.failedOnly {
		scope = "failed steps"
	}
	title := fmt.Sprintf("Log: %s · %s", sanitize(log.name), scope)
	if log.truncated {
		title += " · " + logTruncatedNotice
	}
	if log.text != "" {
		title += fmt.Sprintf(" · %d matches", len(log.matches))
	}
	body := paneBox{title, m.logBody(), m.width, m.height - footerHeight, focusColour}.render()
	return lipgloss.JoinVertical(lipgloss.Left, body, m.footerLine(m.logFooter()))
}

func (m model) logBody() []string {
	log := m.pr.log
	switch {
	case !log.loaded:
		return []string{faintStyle.Render(loadingText)}
	case log.message != "":
		return []string{faintStyle.Render(log.message)}
	}
	rows := m.logRows()
	end := min(len(log.lines), log.top+rows)
	out := make([]string, 0, rows)
	for _, line := range log.lines[log.top:end] {
		out = append(out, log.query.highlight(line, matchStyle))
	}
	return out
}

func (m model) logFooter() string {
	if m.pr.log.searching {
		return m.search.View()
	}
	if m.notice != "" {
		return promptStyle.Render(m.notice)
	}
	return faintStyle.Render(logHint)
}
