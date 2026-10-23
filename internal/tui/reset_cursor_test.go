package tui

import (
	"testing"
	"time"

	"github.com/rubentanahara/agent-trail/internal/trail"
)

func TestResetOfAnotherSessionKeepsThePausedCursorOnItsEvent(t *testing.T) {
	m := press(sized(newModel(nil, sampleBatch(), Context{Repo: "/repo"}), 120, 30), "2", "up", "up")
	if got := m.events[m.selectedEvent()].Summary; got != "write tests" || m.follow {
		t.Fatalf("setup: selected %q, follow %v", got, m.follow)
	}
	at := time.Date(2026, 10, 22, 11, 0, 0, 0, time.UTC)
	m.absorb(trail.Batch{
		Reset:  []string{"claude:a"},
		Events: []trail.Event{{Time: at, CLI: trail.Claude, Session: "claude:a", Kind: trail.KindUser, Summary: "rewritten prompt"}},
	})
	if got := m.events[m.selectedEvent()].Summary; got != "write tests" {
		t.Fatalf("selected %q after reset, want the paused cursor kept on %q", got, "write tests")
	}
}
