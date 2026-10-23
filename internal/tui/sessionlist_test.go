package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/rubentanahara/agent-trail/internal/trail"
)

const testVersion = "1.2.3"

func runningClaude(name, activity string, last time.Time) trail.Session {
	return trail.Session{Key: "claude:" + name, ID: name, CLI: trail.Claude, Name: name, Events: 1, Last: last, Liveness: trail.LivenessRunning, Activity: activity, Billing: trail.BillingSubscription, PricedCalls: 1}
}

func listModel(width int, sessions ...trail.Session) model {
	batch := sampleBatch()
	batch.Sessions = sessions
	return sized(newModel(nil, batch, Context{Repo: "/repo", Version: testVersion}), width, 30)
}

func rowContaining(screen, text string) string {
	for _, line := range strings.Split(screen, "\n") {
		if strings.Contains(line, text) {
			return line
		}
	}
	return ""
}

func sessionsColumn(screen string) string {
	var column []string
	for _, line := range strings.Split(screen, "\n") {
		if parts := strings.SplitN(line, "│", 3); len(parts) == 3 {
			column = append(column, parts[1])
		}
	}
	return strings.Join(column, "\n")
}

func TestWaitingSessionGetsItsOwnGlyph(t *testing.T) {
	now := time.Now()
	screen := ansi.Strip(listModel(120, runningClaude("waiter", "idle", now), runningClaude("worker", "busy", now.Add(-time.Second))).render())
	if row := rowContaining(screen, "waiter"); !strings.Contains(row, waitingGlyph) || strings.Contains(row, runningGlyph) {
		t.Fatalf("idle row = %q", row)
	}
	if row := rowContaining(screen, "worker"); !strings.Contains(row, runningGlyph) || strings.Contains(row, waitingGlyph) {
		t.Fatalf("busy row = %q", row)
	}
}

func TestWaitingReadsAsTextAndIsFilterable(t *testing.T) {
	now := time.Now()
	m := listModel(160, runningClaude("waiter", "idle", now), runningClaude("worker", "busy", now.Add(-time.Second)))
	waiter, worker := m.sessions["claude:waiter"], m.sessions["claude:worker"]
	if state := stateLine(waiter); !strings.Contains(state, "waiting for you") || strings.Contains(state, "idle") {
		t.Fatalf("state = %q", state)
	}
	if state := scopeState(waiter); !strings.Contains(state, "waiting for you") || strings.Contains(state, "idle") {
		t.Fatalf("scope state = %q", state)
	}
	if !parseQuery("waiting").matches(sessionHaystack(waiter)) || parseQuery("waiting").matches(sessionHaystack(worker)) {
		t.Fatal("waiting does not single out the idle session")
	}
}

func TestLegendListsTheWaitingGlyph(t *testing.T) {
	screen := ansi.Strip(listModel(160, runningClaude("waiter", "idle", time.Now())).render())
	if !strings.Contains(screen, waitingGlyph+"waiting for you") {
		t.Fatalf("legend missing:\n%s", screen)
	}
}

func TestCostColumnIsDollarsOrDash(t *testing.T) {
	now := time.Now()
	cheap := runningClaude("cheap", "", now)
	cheap.Tokens.CostUSD = 0.0009
	dear := runningClaude("dear", "", now.Add(-time.Second))
	dear.Tokens.CostUSD = 14.087
	unpriced := runningClaude("unpriced", "", now.Add(-2*time.Second))
	unpriced.PricedCalls = 0
	unpriced.Tokens.Output = 12300
	premium := trail.Session{Key: "copilot:premium", ID: "premium", CLI: trail.Copilot, Name: "premium", Events: 1, Last: now.Add(-3 * time.Second), PremiumRequests: 3}
	screen := ansi.Strip(listModel(120, cheap, dear, unpriced, premium).render())
	for name, cost := range map[string]string{"cheap": "<$0.01", "dear": "$14.09", "unpriced": "—", "premium": "—"} {
		if row := rowContaining(screen, name); !strings.Contains(row, cost) {
			t.Errorf("%s row = %q, want %q", name, row, cost)
		}
	}
	column, _, _ := strings.Cut(sessionsColumn(screen), "Totals")
	for _, banned := range []string{"req", "out", "~", " API"} {
		if strings.Contains(column, banned) {
			t.Errorf("sessions pane shows %q:\n%s", banned, column)
		}
	}
}

func TestAgesLineUpAcrossRows(t *testing.T) {
	now := time.Now()
	cheap := runningClaude("cheap", "", now.Add(-2*time.Second))
	cheap.Tokens.CostUSD = 0.0009
	dear := runningClaude("dear", "", now.Add(-3*time.Hour))
	dear.Tokens.CostUSD = 1234.5
	unpriced := runningClaude("unpriced", "", now.Add(-12*time.Minute))
	unpriced.PricedCalls = 0
	screen := ansi.Strip(listModel(120, cheap, dear, unpriced).render())
	ageEnds := map[string]int{}
	for name, age := range map[string]string{"cheap": "2s", "dear": "3h", "unpriced": "12m"} {
		inner := sessionsColumn(rowContaining(screen, name))
		ageEnds[name] = ansi.StringWidth(inner[:strings.Index(inner, " "+age+" ")+len(age)+1])
	}
	if ageEnds["cheap"] != ageEnds["dear"] || ageEnds["dear"] != ageEnds["unpriced"] {
		t.Fatalf("age columns end at %v", ageEnds)
	}
}

func footerOf(m model) string {
	lines := strings.Split(ansi.Strip(m.render()), "\n")
	return lines[len(lines)-1]
}

func TestFooterListsOnlyTheFocusedPanesKeys(t *testing.T) {
	m := listModel(260, runningClaude("waiter", "idle", time.Now()))
	globalHints := []string{"tab panes", "z zoom", "t stats", "q quit"}
	cases := []struct {
		focus          string
		present, other []string
	}{
		{"1", []string{"x delete", "c continue", "o running only", "m hide automated", "enter events"}, []string{"enter detail", "s event's session", "f follow", "r raw", "esc back"}},
		{"2", []string{"enter detail", "s event's session", "n show noise", "u usage", "f follow"}, []string{"x delete", "c continue", "o running only", "r raw", "esc back"}},
		{"3", []string{"↑↓ scroll", "r raw", "/ filter lines", "esc back"}, []string{"x delete", "enter detail", "f follow", "o running only"}},
	}
	for _, c := range cases {
		footer := footerOf(press(m, c.focus))
		for _, hint := range append(c.present, globalHints...) {
			if !strings.Contains(footer, hint) {
				t.Errorf("focus %s footer %q lacks %q", c.focus, footer, hint)
			}
		}
		for _, hint := range c.other {
			if strings.Contains(footer, hint) {
				t.Errorf("focus %s footer %q has another pane's %q", c.focus, footer, hint)
			}
		}
	}
}

func TestNarrowFooterDropsWholeHintsFromTheRight(t *testing.T) {
	full := listModel(260, runningClaude("waiter", "idle", time.Now()))
	hints := append(full.paneHints(), full.globalHints()...)
	for width := 40; width <= 120; width++ {
		footer := footerOf(listModel(width, runningClaude("waiter", "idle", time.Now())))
		left := strings.TrimRight(strings.TrimSuffix(footer, "agent-trail "+testVersion), " ")
		kept := 0
		for kept < len(hints) && left != strings.Join(hints[:kept], hintSeparator) {
			kept++
		}
		if left != strings.Join(hints[:kept], hintSeparator) {
			t.Fatalf("width %d footer %q is not a run of whole hints from the left", width, footer)
		}
	}
}
