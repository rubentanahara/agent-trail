package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/rubentanahara/agent-trail/internal/trail"
)

func familyBatch() trail.Batch {
	at := time.Date(2026, 10, 22, 10, 0, 0, 0, time.UTC)
	event := func(seconds int, cli trail.CLI, session, agent, summary string) trail.Event {
		return trail.Event{Time: at.Add(time.Duration(seconds) * time.Second), CLI: cli, Session: session, Agent: agent, Kind: trail.KindTool, Summary: summary}
	}
	return trail.Batch{
		Events: []trail.Event{
			event(0, trail.Claude, "claude:p", "", "parent works"),
			event(1, trail.Claude, "claude:p/x", "general-purpose", "agent x works"),
			event(2, trail.Claude, "claude:p/y", "general-purpose", "agent y works"),
			event(3, trail.Codex, "codex:c", "", "codex works"),
			event(4, trail.Copilot, "copilot:k", "", "copilot works"),
		},
		Sessions: []trail.Session{
			{Key: "claude:p", ID: "p", CLI: trail.Claude, Name: "parent task", Events: 1, Start: at, Last: at.Add(5 * time.Second), Branch: "main"},
			{Key: "claude:p/x", ID: "x", CLI: trail.Claude, Name: "Fix rewritten transcript", Parent: "claude:p", Agent: "general-purpose", Events: 1, Start: at.Add(time.Second), Last: at.Add(time.Second)},
			{Key: "claude:p/y", ID: "y", CLI: trail.Claude, Name: "Review pricing table", Parent: "claude:p", Agent: "general-purpose", Events: 1, Start: at.Add(2 * time.Second), Last: at.Add(2 * time.Second)},
			{Key: "codex:c", ID: "c", CLI: trail.Codex, Name: "codex task", Events: 1, Last: at.Add(3 * time.Second)},
			{Key: "copilot:k", ID: "k", CLI: trail.Copilot, Name: "copilot task", Events: 1, Last: at.Add(4 * time.Second)},
		},
	}
}

func familyScreen(icons bool, width, height int) string {
	m := sized(newModel(nil, familyBatch(), Context{Repo: "/repo", Icons: icons}), width, height)
	return ansi.Strip(m.render())
}

func TestIconsAppearOnSessionRowsEventRowsAndBranchLine(t *testing.T) {
	m := sized(newModel(nil, familyBatch(), Context{Repo: "/repo", Icons: true}), 140, 40)
	m = press(m, "1", "down")
	screen := ansi.Strip(m.render())
	for _, icon := range []string{claudeIcon, codexIcon, copilotIcon, agentIcon, gitIcon} {
		if !strings.Contains(screen, icon) {
			t.Errorf("icon %q missing:\n%s", icon, screen)
		}
	}
	for _, line := range strings.Split(screen, "\n") {
		if strings.Contains(line, "Branch") && !strings.Contains(line, gitIcon) {
			t.Errorf("branch line has no git icon: %q", line)
		}
		if strings.Contains(line, "agent x works") && !strings.Contains(line, agentIcon) {
			t.Errorf("agent event row has no agent icon: %q", line)
		}
		if strings.Contains(line, "codex works") && !strings.Contains(line, codexIcon) {
			t.Errorf("codex event row has no codex icon: %q", line)
		}
	}
}

func TestIconsOffShowNoIconCodePoints(t *testing.T) {
	screen := familyScreen(false, 140, 40)
	for _, icon := range []string{claudeIcon, codexIcon, copilotIcon, agentIcon, gitIcon, folderIcon} {
		if strings.Contains(screen, strings.TrimSpace(icon)) {
			t.Errorf("icon %q shown with icons off", icon)
		}
	}
}

func TestAgentEventsShowTheAgentNameInTheWhoColumn(t *testing.T) {
	screen := familyScreen(false, 140, 40)
	for _, line := range strings.Split(screen, "\n") {
		if strings.Contains(line, "agent x works") && !strings.Contains(line, "Fix rewritten") {
			t.Errorf("agent x row lacks its name: %q", line)
		}
		if strings.Contains(line, "agent y works") && !strings.Contains(line, "Review pricing") {
			t.Errorf("agent y row lacks its name: %q", line)
		}
		if strings.Contains(line, "parent works") && !strings.Contains(line, "claude ") {
			t.Errorf("parent row changed: %q", line)
		}
	}
}

func TestEventsFilterFindsAnAgentByAWordOfItsName(t *testing.T) {
	m := sized(newModel(nil, familyBatch(), Context{Repo: "/repo"}), 140, 40)
	m = press(m, "2", "/", "p", "r", "i", "c", "i", "n", "g", "enter")
	if got := strings.Join(visibleSummaries(m), ","); got != "agent y works" {
		t.Fatalf("visible = %q", got)
	}
}

func TestIconsRenderFitsTheTerminal(t *testing.T) {
	for _, icons := range []bool{true, false} {
		for _, width := range []int{60, 100, 200} {
			for _, line := range strings.Split(familyScreen(icons, width, 30), "\n") {
				if ansi.StringWidth(line) > width {
					t.Errorf("icons=%v width %d: line width %d: %q", icons, width, ansi.StringWidth(line), line)
				}
			}
		}
	}
}
