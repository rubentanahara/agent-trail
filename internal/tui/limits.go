package tui

import (
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/rubentanahara/agent-trail/internal/limits"
	"github.com/rubentanahara/agent-trail/internal/trail"
)

const alarmingUsagePercent = 80

type limitsMsg struct {
	claude limits.Claude
	found  bool
}

func pollLimits(home string) tea.Cmd {
	return func() tea.Msg {
		claude, found := limits.Load(home)
		return limitsMsg{claude: claude, found: found}
	}
}

func (m model) claudeLimitsText() string {
	if !m.hasClaudeLimits {
		return ""
	}
	now := time.Now()
	parts := []string{usageWindow("5h", m.claudeLimits.FiveHour, now)}
	if resets := m.claudeLimits.FiveHour; resets.Known && resets.ResetsAt.After(now) {
		parts = append(parts, "resets "+resets.ResetsAt.Local().Format("15:04"))
	}
	parts = append(parts, usageWindow("week", m.claudeLimits.SevenDay, now), "as of "+age(m.claudeLimits.AsOf))
	return strings.Join(nonBlank(parts), " · ")
}

func usageWindow(label string, window limits.Window, now time.Time) string {
	switch {
	case !window.Known:
		return ""
	case !window.ResetsAt.After(now):
		return label + " reset"
	case window.Percent >= alarmingUsagePercent:
		return failedStyle.Render(fmt.Sprintf("%s %.0f%%", label, window.Percent))
	}
	return fmt.Sprintf("%s %.0f%%", label, window.Percent)
}

func nonBlank(parts []string) []string {
	kept := parts[:0]
	for _, part := range parts {
		if part != "" {
			kept = append(kept, part)
		}
	}
	return kept
}

func (m model) sessionLimits(session trail.Session) string {
	own := limitsLine(session)
	if session.CLI != trail.Claude {
		return own
	}
	return strings.Join(nonBlank([]string{m.claudeLimitsText(), own}), " · ")
}
