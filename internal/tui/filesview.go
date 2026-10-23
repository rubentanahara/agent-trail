package tui

import (
	"fmt"
	"image/color"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

const (
	filesLeftPercent = 40
	filesMinLeft     = 24
	unknownCount     = "?"
	filesHint        = "↑↓ pick file  tab/enter diff  pgup/pgdn scroll  d/esc back  q quit"
	diffHint         = "↑↓ scroll  pgup/pgdn page  tab files  d/esc back  q quit"
	noFilesText      = "no file changes recorded"
	loadingText      = "loading…"
	unrecordedText   = "this CLI did not record the diff"
	addedPrefix      = "+"
	removedPrefix    = "-"
	hunkPrefix       = "@@"
)

var (
	addedStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("42"))
	hunkStyle  = lipgloss.NewStyle().Foreground(focusColour)
)

func (m model) filesGeometry() (leftW, rightW, bodyH int) {
	leftW = min(m.width/2, max(filesMinLeft, m.width*filesLeftPercent/100))
	return leftW, m.width - leftW, m.height - footerHeight - headerHeight
}

func (m model) renderFiles() string {
	leftW, rightW, bodyH := m.filesGeometry()
	screen := m.filesScreen
	hint := filesHint
	if screen.diffFocus {
		hint = diffHint
	}
	body := lipgloss.JoinHorizontal(lipgloss.Top,
		paneBox{fmt.Sprintf("Files (%d)", len(screen.files)), m.fileRows(leftW-2, bodyH-borderAndTitle-1), leftW, bodyH, filesBorder(!screen.diffFocus)}.render(),
		paneBox{m.diffTitle(), m.diffRows(), rightW, bodyH, filesBorder(screen.diffFocus)}.render(),
	)
	return lipgloss.JoinVertical(lipgloss.Left, fit(m.filesHeader(), m.width), body, m.footerLine(faintStyle.Render(hint)))
}

func filesBorder(focused bool) color.Color {
	if focused {
		return focusColour
	}
	return idleColour
}

func (m model) filesHeader() string {
	added, removed := 0, 0
	for _, file := range m.filesScreen.files {
		added, removed = added+file.added, removed+file.removed
	}
	return titleStyle.Render("Files changed") + faintStyle.Render(" · "+m.filesScope()+" · ") + countsText(added, removed, 0)
}

func (m model) filesScope() string {
	if session, ok := m.sessions[m.activeSession]; ok {
		return sessionLabel(session)
	}
	return "all sessions"
}

func (m model) fileRows(width, rows int) []string {
	screen := m.filesScreen
	if len(screen.files) == 0 {
		return []string{faintStyle.Render(noFilesText)}
	}
	top := scrollTop(screen.top, screen.cursor, len(screen.files), rows)
	out := make([]string, 0, max(0, rows))
	for row := top; row < len(screen.files) && len(out) < rows; row++ {
		file := screen.files[row]
		right := countsText(file.added, file.removed, file.uncounted) + faintStyle.Render(fmt.Sprintf(" ×%d", file.edits))
		line := spread(marker(row == screen.cursor)+file.label, right, width)
		if row == screen.cursor {
			line = selectedStyle.Render(fit(ansi.Strip(line), width))
		}
		out = append(out, line)
	}
	return out
}

func countsText(added, removed, uncounted int) string {
	if uncounted > 0 && added+removed == 0 {
		return faintStyle.Render("+" + unknownCount + " −" + unknownCount)
	}
	return addedStyle.Render(fmt.Sprintf("+%d", added)) + " " + failedStyle.Render(fmt.Sprintf("−%d", removed))
}

func (m model) diffTitle() string {
	screen := m.filesScreen
	if len(screen.files) == 0 {
		return "Diff"
	}
	return "Diff: " + screen.files[screen.cursor].label
}

func (m model) diffRows() []string {
	screen := m.filesScreen
	switch {
	case len(screen.files) == 0:
		return nil
	case screen.err != nil:
		return []string{failedStyle.Render(screen.err.Error())}
	case screen.loading:
		return []string{faintStyle.Render(loadingText)}
	}
	return strings.Split(screen.diff.View(), "\n")
}

func renderBlocks(blocks []patchBlock) string {
	rendered := make([]string, 0, len(blocks))
	for _, block := range blocks {
		rendered = append(rendered, renderBlock(block))
	}
	return strings.Join(rendered, "\n\n")
}

func renderBlock(block patchBlock) string {
	event := block.event
	lines := []string{cliStyle(event.CLI).Render(event.Time.Local().Format(timeLayout) + "  " + event.Who())}
	if len(block.lines) == 0 {
		return strings.Join(append(lines, faintStyle.Render(unrecordedText)), "\n")
	}
	for _, line := range block.lines {
		lines = append(lines, diffLineStyle(line).Render(line))
	}
	return strings.Join(lines, "\n")
}

func diffLineStyle(line string) lipgloss.Style {
	switch {
	case strings.HasPrefix(line, hunkPrefix):
		return hunkStyle
	case strings.HasPrefix(line, addedPrefix):
		return addedStyle
	case strings.HasPrefix(line, removedPrefix):
		return failedStyle
	}
	return faintStyle
}
