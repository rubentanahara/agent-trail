package tui

import (
	"slices"
	"testing"
	"time"

	"github.com/rubentanahara/agent-trail/internal/trail"
)

func thinkingBatch() trail.Batch {
	at := time.Date(2026, 10, 22, 10, 0, 0, 0, time.UTC)
	event := func(seconds int, summary string) trail.Event {
		return trail.Event{Time: at.Add(time.Duration(seconds) * time.Second), CLI: trail.Claude, Session: "claude:a", Kind: trail.KindThinking, Summary: summary}
	}
	return trail.Batch{
		Events:   []trail.Event{event(0, trail.HiddenThinking), event(1, "weighing options")},
		Sessions: []trail.Session{{Key: "claude:a", ID: "a", CLI: trail.Claude, Name: "login", Events: 2, Last: at.Add(time.Second)}},
	}
}

func TestEmptyThinkingIsHiddenAtStartAndShownWithNoise(t *testing.T) {
	m := sized(newModel(nil, thinkingBatch(), Context{Repo: "/repo"}), 120, 40)
	if got := visibleSummaries(m); !slices.Equal(got, []string{"weighing options"}) {
		t.Fatalf("at start = %q", got)
	}
	m = press(m, "n")
	if got := visibleSummaries(m); !slices.Equal(got, []string{trail.HiddenThinking, "weighing options"}) {
		t.Fatalf("after n = %q", got)
	}
}
