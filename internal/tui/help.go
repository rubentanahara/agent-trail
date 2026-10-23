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
