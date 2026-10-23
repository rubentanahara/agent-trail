package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/rubentanahara/agent-trail/internal/limits"
	"github.com/rubentanahara/agent-trail/internal/trail"
)

func withClaudeLimits(m model, five float64) model {
	now := time.Now()
	updated, _ := m.Update(limitsMsg{claude: limits.Claude{
		FiveHour: limits.Window{Percent: five, ResetsAt: now.Add(time.Hour), Known: true},
		SevenDay: limits.Window{Percent: 21, ResetsAt: now.Add(72 * time.Hour), Known: true},
		AsOf:     now.Add(-2 * time.Minute),
	}, found: true})
	return updated.(model)
}

func TestTotalsShowClaudeUsageWindows(t *testing.T) {
	m := withClaudeLimits(sized(newModel(nil, sampleBatch(), Context{}), 160, 30), 50)
	screen := ansi.Strip(m.render())
	if !strings.Contains(screen, "Claude    5h 50% · resets ") {
		t.Fatalf("totals lack the Claude row:\n%s", screen)
	}
	text := ansi.Strip(m.claudeLimitsText())
	for _, want := range []string{"5h 50% · resets ", " · week 21% · as of 2m"} {
		if !strings.Contains(text, want) {
			t.Fatalf("row lacks %q: %q", want, text)
		}
	}
}

func TestClaudeUsageIsRedFromEightyPercent(t *testing.T) {
	m := newModel(nil, sampleBatch(), Context{})
	if got := withClaudeLimits(m, 80).claudeLimitsText(); !strings.Contains(got, failedStyle.Render("5h 80%")) {
		t.Fatalf("80%% not red: %q", got)
	}
	if got := withClaudeLimits(m, 79).claudeLimitsText(); strings.Contains(got, failedStyle.Render("5h 79%")) {
		t.Fatalf("79%% red: %q", got)
	}
}

func TestNothingShownWithoutClaudeUsage(t *testing.T) {
	screen := ansi.Strip(sized(newModel(nil, sampleBatch(), Context{}), 160, 30).render())
	if strings.Contains(screen, "5h ") {
		t.Fatalf("unexpected usage:\n%s", screen)
	}
}

func TestClaudeSessionLimitsLineStartsWithUsage(t *testing.T) {
	m := withClaudeLimits(sized(newModel(nil, sampleBatch(), Context{}), 160, 30), 50)
	m = press(m, "1", "down")
	if m.sessions[m.sessionKeyAt(m.sessionCursor)].CLI != trail.Claude {
		t.Skip("cursor not on the claude session")
	}
	if screen := ansi.Strip(m.render()); !strings.Contains(screen, "Limits   5h 50%") {
		t.Fatalf("limits line lacks usage:\n%s", screen)
	}
}
