package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/rubentanahara/agent-trail/internal/trail"
)

func keyPress(key string) tea.KeyPressMsg {
	if key == "space" {
		return tea.KeyPressMsg{Code: tea.KeySpace, Text: " "}
	}
	return tea.KeyPressMsg{Code: []rune(key)[0], Text: key}
}

func space(m model) model {
	updated, _ := m.Update(keyPress("space"))
	return updated.(model)
}

func foldModel() model {
	return sized(newModel(nil, agentBatch(), Context{Repo: "/repo"}), 160, 40)
}

func orderOf(m model) string {
	return strings.Join(m.order, ",")
}

func TestSpaceFoldsAndUnfoldsTheSelectedSessionsAgents(t *testing.T) {
	m := press(foldModel(), "1", "down")
	m = space(m)
	if got := orderOf(m); got != "claude:a,codex:b" || m.sessionKeyAt(m.sessionCursor) != "claude:a" {
		t.Fatalf("folded order = %s cursor = %d", got, m.sessionCursor)
	}
	m = space(m)
	if got := orderOf(m); got != "claude:a,claude:a/x1,claude:a/x2,codex:b" {
		t.Fatalf("unfolded order = %s", got)
	}
}

func TestFoldChoiceSurvivesLaterBatchesWhileStillRunning(t *testing.T) {
	m := press(foldModel(), "1", "down")
	m = space(m)
	batch := agentBatch()
	updated, _ := m.Update(batchMsg{batch: trail.Batch{Sessions: batch.Sessions}})
	m = updated.(model)
	if got := orderOf(m); got != "claude:a,codex:b" {
		t.Fatalf("order after batch = %s", got)
	}
}

func TestSpaceOnAnAgentFoldsItsParentAndSelectsIt(t *testing.T) {
	m := press(foldModel(), "1", "down", "down")
	if m.sessionKeyAt(m.sessionCursor) != "claude:a/x1" {
		t.Fatalf("setup: on %s", m.sessionKeyAt(m.sessionCursor))
	}
	m = space(m)
	if got := orderOf(m); got != "claude:a,codex:b" || m.activeSession != "claude:a" || m.sessionCursor != 1 {
		t.Fatalf("order = %s active = %s cursor = %d", got, m.activeSession, m.sessionCursor)
	}
}

func TestFoldMarksShowFoldedAndUnfoldedFamilies(t *testing.T) {
	m := press(foldModel(), "1", "down")
	screen := ansi.Strip(m.render())
	if !strings.Contains(screen, "▾ ◌ login") || !strings.Contains(screen, "    ◌ tests") {
		t.Fatalf("unfolded marks missing:\n%s", screen)
	}
	screen = ansi.Strip(space(m).render())
	if !strings.Contains(screen, "▶ ▸ ◌ login") {
		t.Fatalf("folded mark missing:\n%s", screen)
	}
}

func TestSpaceOnAllSessionsTogglesEveryFamily(t *testing.T) {
	m := press(foldModel(), "1", "a")
	m = space(m)
	if got := orderOf(m); got != "claude:a,codex:b" || m.sessionCursor != allSessionsRow {
		t.Fatalf("folded all: order = %s cursor = %d", got, m.sessionCursor)
	}
	m = space(m)
	if got := orderOf(m); got != "claude:a,claude:a/x1,claude:a/x2,codex:b" {
		t.Fatalf("unfolded all: order = %s", got)
	}
}

func TestFoldingKeepsTheSameSessionSelectedAndScrollValid(t *testing.T) {
	m := sized(newModel(nil, agentBatch(), Context{Repo: "/repo"}), 160, 12)
	m = press(m, "1", "down", "down", "down", "down")
	if m.sessionKeyAt(m.sessionCursor) != "codex:b" {
		t.Fatalf("setup: on %s", m.sessionKeyAt(m.sessionCursor))
	}
	m = press(m, "up", "up", "up")
	m = space(m)
	m = press(m, "down")
	if m.sessionKeyAt(m.sessionCursor) != "codex:b" || m.sessionTop < 0 || m.sessionTop > m.sessionCursor {
		t.Fatalf("cursor = %d top = %d order = %s", m.sessionCursor, m.sessionTop, orderOf(m))
	}
}
