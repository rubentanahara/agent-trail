package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/rubentanahara/agent-trail/internal/trail"
)

func turnEvents(session string, steps ...trail.Event) []trail.Event {
	at := time.Date(2026, 10, 22, 10, 0, 0, 0, time.UTC)
	for i := range steps {
		steps[i].Session = session
		steps[i].CLI = trail.Claude
		steps[i].Time = at.Add(time.Duration(i) * time.Second)
		steps[i].Source = trail.Source{Path: session, Index: i}
	}
	return steps
}

func prompt(text string) trail.Event {
	return trail.Event{Kind: trail.KindUser, Summary: text}
}

func call(agent string, usd float64) trail.Event {
	return trail.Event{Kind: trail.KindUsage, Agent: agent, Summary: "call", Usage: &trail.Usage{CostUSD: usd, Priced: true}}
}

func unpricedCall() trail.Event {
	return trail.Event{Kind: trail.KindUsage, Summary: "call", Usage: &trail.Usage{}}
}

func claudeSession(key string, events int) trail.Session {
	return trail.Session{Key: key, ID: key, CLI: trail.Claude, Events: events}
}

func turnsModel(events []trail.Event, sessions ...trail.Session) model {
	return sized(newModel(nil, trail.Batch{Events: events, Sessions: sessions}, Context{Repo: "/repo"}), 200, 50)
}

func screenOf(m model) string {
	return ansi.Strip(m.render())
}

func TestPromptRowsShowTheirTurnCostIncludingAgentUsage(t *testing.T) {
	events := turnEvents("claude:a",
		prompt("fix the login redirect"), call("", 0.5), call("explorer", 0.75),
		prompt("add a test"), call("", 0.004),
	)
	out := screenOf(turnsModel(events, claudeSession("claude:a", len(events))))
	for _, want := range []string{"$1.25 fix the login redirect", "<$0.01 add a test"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in\n%s", want, out)
		}
	}
}

func TestAgentSessionUsageCountsInTheParentsTurn(t *testing.T) {
	events := append(
		turnEvents("claude:a", prompt("delegate"), call("", 0.5)),
		turnEvents("claude:a/x", call("", 0.25))...,
	)
	child := claudeSession("claude:a/x", 1)
	child.Parent = "claude:a"
	out := screenOf(turnsModel(events, claudeSession("claude:a", 2), child))
	if !strings.Contains(out, "$0.75 delegate") {
		t.Errorf("agent usage missing in\n%s", out)
	}
}

func TestSelectedPromptRowShowsItsTurnCost(t *testing.T) {
	events := turnEvents("claude:a", prompt("fix it"), call("", 1))
	m := press(turnsModel(events, claudeSession("claude:a", 2)), "tab", "up", "up")
	if out := screenOf(m); !strings.Contains(out, "$1.00 fix it") {
		t.Errorf("missing cost in\n%s", out)
	}
}

func TestUnpricedCallMarksTurnPartial(t *testing.T) {
	events := turnEvents("claude:a", prompt("fix it"), call("", 1), unpricedCall())
	out := screenOf(turnsModel(events, claudeSession("claude:a", len(events))))
	if !strings.Contains(out, "$1.00+ fix it") {
		t.Errorf("missing partial cost in\n%s", out)
	}
}

func TestPromptWithoutPricedCallShowsNoCost(t *testing.T) {
	events := turnEvents("claude:a", prompt("just talk"), unpricedCall())
	out := screenOf(turnsModel(events, claudeSession("claude:a", len(events))))
	if strings.Contains(out, "+ just talk") || strings.Contains(out, "$0.00") {
		t.Errorf("unexpected cost in\n%s", out)
	}
}

func TestUsageDoesNotLeakIntoAnotherSessionsTurn(t *testing.T) {
	events := append(
		turnEvents("claude:a", prompt("in a")),
		turnEvents("claude:b", prompt("in b"), call("", 2))...,
	)
	out := screenOf(turnsModel(events, claudeSession("claude:a", 1), claudeSession("claude:b", 2)))
	if !strings.Contains(out, "$2.00 in b") || strings.Contains(out, "$2.00 in a") {
		t.Errorf("wrong attribution in\n%s", out)
	}
}

func TestLatestTurnGrowsAsEventsArrive(t *testing.T) {
	events := turnEvents("claude:a", prompt("keep going"), call("", 1), call("", 0.5))
	m := turnsModel(events[:2], claudeSession("claude:a", 2))
	updated, _ := m.Update(batchMsg{trail.Batch{Events: events[2:]}})
	if out := screenOf(updated.(model)); !strings.Contains(out, "$1.50 keep going") {
		t.Errorf("turn did not grow in\n%s", out)
	}
}

func TestInfoBoxNamesTheCostliestPrompt(t *testing.T) {
	events := turnEvents("claude:a",
		prompt("cheap one"), call("", 0.1),
		prompt("big"), call("", 3),
	)
	m := press(turnsModel(events, claudeSession("claude:a", len(events))), "tab", "tab", "down")
	if out := screenOf(m); !strings.Contains(out, "2 prompts · costliest $3.00 big") {
		t.Errorf("missing Turns row in\n%s", out)
	}
}

func BenchmarkTurnsOverFiftyThousandEvents(b *testing.B) {
	const events, promptEvery = 50_000, 10
	steps := make([]trail.Event, 0, events)
	for i := 0; i < events; i++ {
		if i%promptEvery == 0 {
			steps = append(steps, prompt("prompt"))
			continue
		}
		steps = append(steps, call("", 0.01))
	}
	m := turnsModel(turnEvents("claude:a", steps...), claudeSession("claude:a", events))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		m.turns().line("claude:a")
	}
}
