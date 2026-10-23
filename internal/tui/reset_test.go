package tui

import (
	"slices"
	"testing"
	"time"

	"github.com/rubentanahara/agent-trail/internal/trail"
)

func TestResetDropsOnlyThatSessionsOldEvents(t *testing.T) {
	m := sized(newModel(nil, sampleBatch(), Context{Repo: "/repo"}), 120, 30)
	at := time.Date(2026, 10, 22, 11, 0, 0, 0, time.UTC)

	m.absorb(trail.Batch{
		Reset:  []string{"claude:a"},
		Events: []trail.Event{{Time: at, CLI: trail.Claude, Session: "claude:a", Kind: trail.KindUser, Summary: "rewritten prompt"}},
		Sessions: []trail.Session{
			{Key: "claude:a", ID: "a", CLI: trail.Claude, Name: "login", Events: 1, Last: at},
		},
	})

	want := []string{"write tests", "FAILED exit 1", "rewritten prompt"}
	if got := visibleSummaries(m); !slices.Equal(got, want) {
		t.Fatalf("visible = %v, want %v", got, want)
	}
	if len(m.events) != len(m.haystack) {
		t.Fatalf("events = %d, haystack = %d", len(m.events), len(m.haystack))
	}
	if _, kept := m.sessions["claude:a"]; !kept {
		t.Fatal("session row was dropped")
	}
	if m.selectedEvent() == noEvent {
		t.Fatal("event cursor is invalid")
	}
}
