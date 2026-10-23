package tui

import (
	"testing"
	"time"

	"github.com/rubentanahara/agent-trail/internal/trail"
)

func absorbBatch(m model, batch trail.Batch) model {
	updated, _ := m.Update(batchMsg{batch})
	return updated.(model)
}

func TestOpenFilesScreenListsNewEditsAndKeepsTheSelectedFile(t *testing.T) {
	m := press(filesModel(120), "a", "d")
	m = press(m, "j")
	selected := m.filesScreen.files[m.filesScreen.cursor].path
	at := time.Date(2026, 10, 22, 11, 0, 0, 0, time.UTC)
	m = absorbBatch(m, trail.Batch{Events: []trail.Event{
		editEvent(at, "claude:a", "", "/repo/fresh.go", 4, 0),
		editEvent(at.Add(time.Second), "claude:a", "", "/repo/main.go", 1, 1),
	}})
	screen := &m.filesScreen
	if screen.files[0].path != "/repo/main.go" || screen.files[0].edits != 3 {
		t.Fatalf("main.go not first with 3 edits: %+v", screen.files[0])
	}
	var found bool
	for _, file := range screen.files {
		found = found || file.path == "/repo/fresh.go"
	}
	if !found {
		t.Fatal("new file missing from the list")
	}
	if got := screen.files[screen.cursor].path; got != selected {
		t.Fatalf("selection moved from %s to %s", selected, got)
	}
}

func TestOpenFilesScreenReloadsTheShownDiffWithoutLosingScroll(t *testing.T) {
	m := press(filesModel(120), "d")
	path := m.filesScreen.files[0].path
	lines := make([]string, 200)
	for i := range lines {
		lines[i] = "+line"
	}
	block := patchBlock{event: editEvent(time.Now(), "claude:a", "", path, 1, 0), lines: lines}
	updated, _ := m.Update(filePatchesMsg{path: path, blocks: []patchBlock{block}})
	m = updated.(model)
	m.filesScreen.diff.SetYOffset(7)
	at := time.Date(2026, 10, 22, 11, 0, 0, 0, time.UTC)
	updated, cmd := m.Update(batchMsg{trail.Batch{Events: []trail.Event{editEvent(at, "claude:a", "", path, 1, 0)}}})
	m = updated.(model)
	if cmd == nil {
		t.Fatal("no reload command for the shown file")
	}
	updated, _ = m.Update(filePatchesMsg{path: path, blocks: []patchBlock{block}})
	m = updated.(model)
	if got := m.filesScreen.diff.YOffset(); got != 7 {
		t.Fatalf("diff scroll = %d, want 7", got)
	}
}

func TestClosedFilesScreenIgnoresEditBatches(t *testing.T) {
	m := filesModel(120)
	at := time.Date(2026, 10, 22, 11, 0, 0, 0, time.UTC)
	m = absorbBatch(m, trail.Batch{Events: []trail.Event{editEvent(at, "claude:a", "", "/repo/fresh.go", 4, 0)}})
	if m.filesScreen.shown || m.filesScreen.files != nil {
		t.Fatalf("closed screen was touched: %+v", m.filesScreen)
	}
}

func ledgerModel() model {
	events := turnEvents("claude:a", prompt("one"), call("", 1), prompt("two"), call("", 2))
	return turnsModel(events, claudeSession("claude:a", len(events)))
}

func TestLedgerIsCachedUntilEventsOrParentLinksChange(t *testing.T) {
	m := ledgerModel()
	m.turns()
	m.turns()
	builds := m.ledger.builds
	m.turns()
	if m.ledger.builds != builds {
		t.Fatalf("unchanged model rebuilt the ledger: %d -> %d", builds, m.ledger.builds)
	}
	m = absorbBatch(m, trail.Batch{Events: turnEvents("claude:a", call("", 1))})
	m.turns()
	if m.ledger.builds != builds+1 {
		t.Fatalf("absorb did not invalidate: %d builds", m.ledger.builds)
	}
	m.dropEvents(func(session string) bool { return session == "claude:a" })
	m.turns()
	if m.ledger.builds != builds+2 {
		t.Fatalf("dropEvents did not invalidate: %d builds", m.ledger.builds)
	}
	m.forgetSession("claude:a")
	m.turns()
	if m.ledger.builds != builds+3 {
		t.Fatalf("forgetSession did not invalidate: %d builds", m.ledger.builds)
	}
}

func TestEmptyPollsKeepTheLedger(t *testing.T) {
	m := ledgerModel()
	m.turns()
	builds := m.ledger.builds
	m = absorbBatch(m, trail.Batch{Sessions: []trail.Session{claudeSession("claude:a", 4)}})
	m.turns()
	if m.ledger.builds != builds {
		t.Fatalf("a session refresh rebuilt the ledger: %d -> %d", builds, m.ledger.builds)
	}
}

func TestChangedParentLinkRebuildsTheLedger(t *testing.T) {
	m := ledgerModel()
	m.turns()
	builds := m.ledger.builds
	child := claudeSession("claude:a/x", 1)
	child.Parent = "claude:a"
	m = absorbBatch(m, trail.Batch{Sessions: []trail.Session{child}})
	m.turns()
	if m.ledger.builds != builds+1 {
		t.Fatalf("new parent link did not rebuild: %d builds", m.ledger.builds)
	}
}

func TestLateAgentUsageIsChargedToThePromptItFollowed(t *testing.T) {
	events := turnEvents("claude:a", prompt("first"), call("", 1), prompt("second"), call("", 2))
	agentUsage := turnEvents("claude:a/x", call("", 0.5))[0]
	agentUsage.Time = events[1].Time.Add(100 * time.Millisecond)
	child := claudeSession("claude:a/x", 1)
	child.Parent = "claude:a"
	m := turnsModel(append(events, agentUsage), claudeSession("claude:a", len(events)), child)
	ordered := m.turns().ordered
	if ordered[0].cost != 1.5 || ordered[1].cost != 2 {
		t.Fatalf("costs = %v, %v; want 1.5, 2", ordered[0].cost, ordered[1].cost)
	}
}

func fiftyThousandEventModel() model {
	const events, promptEvery = 50_000, 10
	steps := make([]trail.Event, 0, events)
	for i := 0; i < events; i++ {
		if i%promptEvery == 0 {
			steps = append(steps, prompt("prompt"))
			continue
		}
		steps = append(steps, call("", 0.01))
	}
	return turnsModel(turnEvents("claude:a", steps...), claudeSession("claude:a", events))
}

func BenchmarkCachedTurnsOverFiftyThousandEvents(b *testing.B) {
	m := fiftyThousandEventModel()
	m.turns()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		m.turns().line("claude:a")
	}
}
