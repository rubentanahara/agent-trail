package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/rubentanahara/agent-trail/internal/trail"
)

func TestInfoBoxShowsCostTurnsAndContextBeforeItRunsOutOfRows(t *testing.T) {
	at := time.Date(2026, 10, 22, 10, 0, 0, 0, time.UTC)
	usage := &trail.Usage{Model: "claude-opus-5-5", CostUSD: 2, Priced: true}
	batch := trail.Batch{
		Events: []trail.Event{
			{Time: at, CLI: trail.Claude, Session: "claude:p", Kind: trail.KindUser, Summary: "ship it", Source: trail.Source{Path: "p", Offset: 1}},
			{Time: at.Add(time.Second), CLI: trail.Claude, Session: "claude:p", Kind: trail.KindUsage, Usage: usage, Source: trail.Source{Path: "p", Offset: 2}},
			{Time: at.Add(2 * time.Second), CLI: trail.Claude, Session: "claude:p/a", Agent: "Explore", Kind: trail.KindTool, Summary: "look", Source: trail.Source{Path: "a", Offset: 1}},
		},
		Sessions: []trail.Session{
			{Key: "claude:p", ID: "p", CLI: trail.Claude, Name: "parent", Events: 2, Last: at, Start: at, Agent: "grand-architect", Cwd: "/work/app", Branch: "main", Model: "claude-opus-5-5", Permission: "default", Billing: trail.BillingSubscription, Calls: 1, PricedCalls: 1, Tokens: trail.Usage{Input: 10, Output: 5, CostUSD: 2}, HasContext: true, ContextPercent: 42, Liveness: trail.LivenessRunning, Activity: "busy", PID: 7},
			{Key: "claude:p/a", ID: "a", CLI: trail.Claude, Parent: "claude:p", Name: "explorer", Agent: "Explore", Events: 1, Last: at.Add(2 * time.Second), Start: at},
		},
	}
	m := press(sized(newModel(nil, batch, Context{Repo: "/work/app", Icons: true}), 160, 30), "1", "down")
	screen := ansi.Strip(m.render())
	for _, want := range []string{"Cost ", "Turns ", "1 prompt · costliest $2.00 ship it", "context 42%"} {
		if !strings.Contains(screen, want) {
			t.Fatalf("info box lacks %q:\n%s", want, screen)
		}
	}
}
