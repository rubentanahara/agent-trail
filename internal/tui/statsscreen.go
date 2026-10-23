package tui

import (
	tea "charm.land/bubbletea/v2"

	"github.com/rubentanahara/agent-trail/internal/savings"
	"github.com/rubentanahara/agent-trail/internal/stats"
	"github.com/rubentanahara/agent-trail/internal/trail"
)

type statsScreen struct {
	shown     bool
	requested bool
	loading   bool
	data      *stats.Stats
	savings   []savings.Saving
}

func (s *statsScreen) open(context Context) tea.Cmd {
	s.shown = true
	home, since := context.Home, context.StatsSince
	loadSavings := func() tea.Msg { return savingsMsg{savings.Load(home, since)} }
	if s.requested {
		return loadSavings
	}
	s.requested, s.data = true, stats.New(since)
	everywhere := context.Everywhere
	if everywhere == nil {
		return loadSavings
	}
	s.loading = true
	return tea.Batch(loadSavings, func() tea.Msg { return statsMsg{everywhere.Start(since)} })
}

func (s *statsScreen) absorb(batch trail.Batch) {
	s.loading = false
	s.data.Add(batch)
}

func (s statsScreen) poll(everywhere *trail.Trail) tea.Cmd {
	if !s.shown || s.loading || everywhere == nil {
		return nil
	}
	return func() tea.Msg { return statsMsg{everywhere.Poll()} }
}

func (m *model) statsKey(key string) (tea.Cmd, bool) {
	switch key {
	case "q", "ctrl+c":
		return nil, true
	case "t", "esc":
		m.statsScreen.shown = false
	case "a":
		m.statsScreen.data.ShowAutomated = !m.statsScreen.data.ShowAutomated
	}
	return nil, false
}
