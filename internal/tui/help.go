package tui

import (
	"charm.land/lipgloss/v2"
)

const (
	helpKeyWidth = 26
	helpHint     = "↑↓ scroll  pgup/pgdn page  ?/esc back  q quit"
	helpTitle    = "Keys"
)

type keyHelp struct {
	keys   string
	action string
}

type helpSection struct {
	title   string
	entries []keyHelp
}

var helpSections = []helpSection{
	{"Global", []keyHelp{
		{"1 2 3, tab, shift+tab", "focus the sessions, events or detail pane"},
		{"/", "filter the focused pane"},
		{"esc", "clear the filter, leave the detail pane, then back to all sessions"},
		{"z", "zoom the focused pane to the whole screen, or restore"},
		{"w", "jump to the session that has waited for you the longest"},
		{"y", "summarize the selected session with Claude Haiku, or go back"},
		{"c", "continue the selected session in its own CLI"},
		{"m", "show or hide sessions started by a program"},
		{"d", "open or close the files screen"},
		{"t", "open or close the stats screen"},
		{"?", "open or close this reference"},
		{"q", "quit"},
		{"wheel", "scroll the pane under the pointer"},
		{"click", "select a row, focus a pane"},
	}},
	{"Sessions pane", []keyHelp{
		{"up down j k, g G", "move; moving selects the session whose events are shown"},
		{"enter", "go to its events"},
		{"space", "fold or unfold the agents of the session; on All sessions, every session"},
		{"o", "show only running sessions, or all again"},
		{"x", "delete the selected session and its transcript files, after y"},
		{"a", "show all sessions"},
	}},
	{"Events pane", []keyHelp{
		{"up down j k, pgup pgdown", "move"},
		{"g G", "first or last event"},
		{"enter", "go to the detail"},
		{"s", "show only the session of the selected event"},
		{"a", "show all sessions"},
		{"n", "show or hide noise rows"},
		{"u", "show or hide token-usage rows"},
		{"f", "turn auto-scroll to the newest event on or off"},
	}},
	{"Detail pane", []keyHelp{
		{"up down, pgup pgdown", "scroll"},
		{"r", "readable text or the raw JSON record"},
		{"enter", "back to the events"},
	}},
	{"Stats screen", []keyHelp{
		{"a", "show or hide sessions started by a program"},
		{"t, esc", "close"},
	}},
	{"Files screen", []keyHelp{
		{"up down j k", "pick a file, or scroll its diff"},
		{"tab, enter", "move to the diff, or back to the files"},
		{"pgup pgdown", "scroll the diff"},
		{"d, esc", "close"},
	}},
	{"Issues screen", []keyHelp{
		{"i", "open or close the repository's issues, grouped by project column"},
		{"up down j k, pgup pgdown", "move in the focused pane"},
		{"tab, 1 2, enter", "switch between the list and the detail"},
		{"[ ]", "previous or next tab: Overview, Comments, Linked"},
		{"P", "next project, then the repository's open issues"},
		{"R", "refresh now"},
		{"o", "open the issue, or the selected comment, pull request or sub-issue, in the browser"},
		{"/", "filter the list"},
		{"z", "zoom the focused pane"},
		{"esc", "clear the filter, back to the list, then close"},
	}},
	{"Pull requests screen", []keyHelp{
		{"p", "open or close; on a session with a pull request, open that one"},
		{"up down j k, pgup pgdown", "move in the focused pane"},
		{"tab, 1 2, enter", "switch between the list and the detail"},
		{"[ ]", "previous or next tab: Overview, Checks, Comments, Diff, Sessions"},
		{"R", "refresh now"},
		{"o", "open the pull request, or the selected check, in the browser"},
		{"n N", "next or previous file in the Diff tab"},
		{"enter", "on the Sessions tab, jump to that session"},
		{"/", "filter the list"},
		{"z", "zoom the focused pane"},
		{"esc", "clear the filter, back to the list, then close"},
		{"r", "Comments tab: reply to the thread, or add a pull request comment, in your editor"},
		{"e", "edit your comment on the Comments tab, or your pull request title and body"},
		{"v", "resolve or unresolve the selected thread"},
		{"M", "merge: s m r pick a method, d toggles branch deletion, a auto-merge, y confirms"},
		{"X", "close the pull request, after y"},
		{"C", "check out the pull request branch in this repository, after y"},
		{"A", "review: a approve, r request changes, c comment, then your editor, then y"},
		{"L", "Checks tab: read the log of a GitHub Actions check; a failed or full, / search, n N next"},
		{"E", "Checks tab: re-run a GitHub Actions check, after y; a switches to all jobs"},
		{"c", "Diff tab: comment on the selected added or unchanged line"},
		{"S", "submit your pending review: a approve, r request changes, c comment"},
		{"D", "discard your pending review, after y"},
	}},
}

type helpScreen struct {
	shown bool
	top   int
}

func helpLines() []string {
	var lines []string
	for _, section := range helpSections {
		lines = append(lines, titleStyle.Render(section.title))
		for _, entry := range section.entries {
			lines = append(lines, "  "+promptStyle.Render(fit(entry.keys, helpKeyWidth))+" "+entry.action)
		}
		lines = append(lines, "")
	}
	return lines
}

func (m model) helpRows() int {
	return max(1, m.height-footerHeight-borderAndTitle-1)
}

func (m *model) helpKey(key string) bool {
	switch key {
	case "q", "ctrl+c":
		return true
	case "?", "esc":
		m.help = helpScreen{}
	case "up", "k":
		m.scrollHelp(-1)
	case "down", "j":
		m.scrollHelp(1)
	case "pgup":
		m.scrollHelp(-m.helpRows())
	case "pgdown":
		m.scrollHelp(m.helpRows())
	}
	return false
}

func (m *model) scrollHelp(delta int) {
	m.help.top = max(0, min(m.help.top+delta, len(helpLines())-m.helpRows()))
}

func (m model) renderHelp() string {
	rows := helpLines()[min(m.help.top, len(helpLines())):]
	body := paneBox{helpTitle, rows, m.width, m.height - footerHeight, focusColour}.render()
	return lipgloss.JoinVertical(lipgloss.Left, body, m.footerLine(faintStyle.Render(helpHint)))
}
