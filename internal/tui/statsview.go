package tui

import (
	"fmt"
	"math"
	"os"
	"strings"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/rubentanahara/agent-trail/internal/stats"
	"github.com/rubentanahara/agent-trail/internal/trail"
)

const (
	headerHeight     = 1
	barBlock         = "█"
	sparkLevels      = "▁▂▃▄▅▆▇█"
	labelShare       = 2
	fifths           = 5
	minutesShown     = 60
	hoursShown       = 24
	daysShown        = 30
	sparkLabelW      = 10
	percentPerUnit   = 100
	notEnoughHistory = "—"
)

var barStyle = lipgloss.NewStyle().Foreground(focusColour)

type barRow struct {
	label string
	style lipgloss.Style
	value float64
	note  string
}

func (m model) renderStats() string {
	bodyH := m.height - footerHeight - headerHeight
	if m.statsScreen.loading {
		footer := m.footerLine(faintStyle.Render("t/esc back to the trail  q quit"))
		return lipgloss.JoinVertical(lipgloss.Left, fit(m.statsHeader(), m.width), paneBox{"Stats", statsPlaceholder(), m.width, bodyH, focusColour}.render(), footer)
	}
	footer := m.footerLine(faintStyle.Render("t/esc back to the trail  a " + shownOrHidden(!m.statsScreen.data.ShowAutomated) + " automated sessions  q quit"))
	leftW, topH := m.width/2, bodyH/2
	rightW, bottomH := m.width-leftW, bodyH-topH
	cliH := topH / 2
	now := time.Now()
	left := stack(
		paneBox{"Which CLI you use most (prompts)", m.cliRows(leftW - 2), leftW, cliH, idleColour}.render(),
		paneBox{"Tokens your tools saved, as they record it", m.savingsRows(leftW - 2), leftW, topH - cliH, idleColour}.render(),
	)
	top := lipgloss.JoinHorizontal(lipgloss.Top,
		left,
		paneBox{"Which project you work on most (prompts)", m.projectRows(rightW - 2), rightW, topH, idleColour}.render(),
	)
	bottom := lipgloss.JoinHorizontal(lipgloss.Top,
		paneBox{"Which file types the agents touch (tool calls)", m.extensionRows(leftW - 2), leftW, bottomH, idleColour}.render(),
		paneBox{"Spend at API list prices", m.spendRows(now, rightW-2), rightW, bottomH, idleColour}.render(),
	)
	return lipgloss.JoinVertical(lipgloss.Left, fit(m.statsHeader(), m.width), top, bottom, footer)
}

func (m model) statsHeader() string {
	since := "the whole history"
	if !m.context.StatsSince.IsZero() {
		since = "since " + m.context.StatsSince.Local().Format(time.DateOnly)
	}
	return titleStyle.Render("Stats") + faintStyle.Render(" · every repository · "+since+" · subscription costs are API-equivalent"+m.automatedNote())
}

func (m model) automatedNote() string {
	if m.statsScreen.data.ShowAutomated {
		return ""
	}
	return fmt.Sprintf(" · %d automated sessions hidden", m.statsScreen.data.AutomatedSessions())
}

func (m model) savingsRows(width int) []string {
	if len(m.statsScreen.savings) == 0 {
		return []string{faintStyle.Render("no RTK or Headroom savings recorded")}
	}
	var rows []barRow
	for _, saving := range m.statsScreen.savings {
		note := trail.Tokens(saving.Tokens) + " tokens"
		if !saving.Since.IsZero() {
			note += " since " + saving.Since.Local().Format(time.DateOnly)
		}
		if saving.HasUSD {
			note += " · " + trail.Dollars(saving.USD) + " by " + saving.Tool + "'s count"
		}
		rows = append(rows, barRow{label: saving.Tool + " · " + saving.CLI, style: lipgloss.NewStyle(), value: float64(saving.Tokens), note: note})
	}
	return bars(rows, width)
}

func statsPlaceholder() []string {
	return []string{"Reading the transcripts of every repository…"}
}

func (m model) cliRows(width int) []string {
	var rows []barRow
	for _, row := range m.statsScreen.data.ByCLI() {
		note := fmt.Sprintf("%d prompts · %d sessions · %s · cache %s", row.Prompts, row.Sessions, trail.Dollars(row.CostUSD), percent(row.CacheReadShare()))
		rows = append(rows, barRow{label: row.Label, style: cliStyle(trail.CLI(row.Label)), value: float64(row.Prompts), note: note})
	}
	return bars(rows, width)
}

func (m model) projectRows(width int) []string {
	home, _ := os.UserHomeDir()
	var rows []barRow
	for _, row := range m.statsScreen.data.ByProject() {
		label := row.Label
		if home != "" && strings.HasPrefix(label, home) {
			label = "~" + strings.TrimPrefix(label, home)
		}
		note := fmt.Sprintf("%d prompts · %s", row.Prompts, trail.Dollars(row.CostUSD))
		rows = append(rows, barRow{label: label, style: lipgloss.NewStyle(), value: float64(row.Prompts), note: note})
	}
	return bars(rows, width)
}

func (m model) extensionRows(width int) []string {
	var rows []barRow
	for _, count := range m.statsScreen.data.Extensions() {
		rows = append(rows, barRow{label: count.Label, style: lipgloss.NewStyle(), value: float64(count.N), note: fmt.Sprintf("%d", count.N)})
	}
	return bars(rows, width)
}

func (m model) spendRows(now time.Time, width int) []string {
	rows := []string{labelStyle.Render(fmt.Sprintf("%-*s %12s %12s", sparkLabelW-1, "per", "current", "average"))}
	for _, period := range m.statsScreen.data.Spend(now) {
		rows = append(rows, fmt.Sprintf("%-*s %12s %12s", sparkLabelW-1, period.Name, trail.Dollars(period.Current), averageText(period)))
	}
	sparkW := width - sparkLabelW
	rows = append(rows, "",
		m.sparkRow(now, "last 60m", time.Minute, min(minutesShown, sparkW)),
		m.sparkRow(now, "last 24h", time.Hour, min(hoursShown, sparkW)),
		m.sparkRow(now, "last 30d", 24*time.Hour, min(daysShown, sparkW)),
		"",
	)
	return append(rows, tokenAdvice(m.statsScreen.data.Overall())...)
}

func averageText(period stats.PeriodSpend) string {
	if !period.HasAverage {
		return notEnoughHistory
	}
	return trail.Dollars(period.Average)
}

func (m model) sparkRow(now time.Time, label string, step time.Duration, count int) string {
	values := m.statsScreen.data.Series(now, step, max(1, count))
	peak := 0.0
	for _, value := range values {
		peak = math.Max(peak, value)
	}
	return labelStyle.Render(fmt.Sprintf("%-*s", sparkLabelW, label)) + barStyle.Render(sparkline(values, peak)) + faintStyle.Render(" peak "+trail.Dollars(peak))
}

func tokenAdvice(overall stats.Totals) []string {
	if overall.Input == 0 {
		return nil
	}
	return []string{
		fmt.Sprintf("cache reads are %s of input tokens", percent(overall.CacheReadShare())),
		fmt.Sprintf("in %s · out %s · %d model calls", trail.Tokens(overall.Input), trail.Tokens(overall.Output), overall.Calls),
	}
}

func percent(share float64) string {
	return fmt.Sprintf("%.0f%%", share*percentPerUnit)
}

func bars(rows []barRow, width int) []string {
	labelW, noteW, peak := 0, 0, 0.0
	for _, row := range rows {
		labelW = max(labelW, ansi.StringWidth(row.label))
		noteW = max(noteW, ansi.StringWidth(row.note))
		peak = math.Max(peak, row.value)
	}
	labelW = min(labelW, width*labelShare/fifths)
	barW := max(1, min(width-labelW-noteW-2, width/2))
	out := make([]string, 0, len(rows))
	for _, row := range rows {
		out = append(out, row.style.Render(fit(row.label, labelW))+" "+barStyle.Render(fit(bar(row.value, peak, barW), barW))+" "+row.note)
	}
	return out
}

func bar(value, peak float64, width int) string {
	if value <= 0 || peak <= 0 {
		return ""
	}
	return strings.Repeat(barBlock, max(1, int(math.Round(value/peak*float64(width)))))
}

func sparkline(values []float64, peak float64) string {
	levels := []rune(sparkLevels)
	var line strings.Builder
	for _, value := range values {
		level := 0
		if peak > 0 {
			level = int(math.Round(value / peak * float64(len(levels)-1)))
		}
		line.WriteRune(levels[level])
	}
	return line.String()
}
