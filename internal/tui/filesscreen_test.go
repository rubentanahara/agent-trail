package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/rubentanahara/agent-trail/internal/trail"
)

func editEvent(at time.Time, session, agent, path string, added, removed int) trail.Event {
	return trail.Event{
		Time: at, CLI: trail.Claude, Agent: agent, Session: session, Kind: trail.KindResult, Summary: "ok",
		Changes: []trail.FileChange{{Path: path, Added: added, Removed: removed, Counted: true}},
	}
}

func filesModel(width int) model {
	at := time.Date(2026, 10, 22, 10, 0, 0, 0, time.UTC)
	batch := trail.Batch{
		Events: []trail.Event{
			editEvent(at, "claude:a", "", "/repo/main.go", 3, 1),
			editEvent(at.Add(time.Second), "claude:a/agent", "reviewer", "/repo/pkg/util.go", 5, 0),
			editEvent(at.Add(2*time.Second), "claude:a", "", "/repo/main.go", 2, 2),
			editEvent(at.Add(3*time.Second), "codex:b", "", "/elsewhere/other.txt", 9, 9),
		},
		Sessions: []trail.Session{
			{Key: "claude:a", ID: "a", CLI: trail.Claude, Name: "login", Events: 2, Cwd: "/repo", Last: at.Add(2 * time.Second)},
			{Key: "claude:a/agent", ID: "ag", Parent: "claude:a", CLI: trail.Claude, Name: "review", Agent: "reviewer", Events: 1, Cwd: "/repo", Last: at.Add(time.Second)},
			{Key: "codex:b", ID: "b", CLI: trail.Codex, Name: "tests", Events: 1, Cwd: "/elsewhere", Last: at.Add(3 * time.Second)},
		},
	}
	return sized(newModel(nil, batch, Context{Repo: "/repo"}), width, 30)
}

func TestFilesScreenListsTheSelectedSessionsFilesWithItsAgents(t *testing.T) {
	m := press(filesModel(120), "1", "j", "j", "d")
	screen := ansi.Strip(m.render())
	if !m.filesScreen.shown {
		t.Fatal("d did not open the files screen")
	}
	for _, want := range []string{"main.go", "+5 −3", "pkg/util.go", "+5 −0"} {
		if !strings.Contains(screen, want) {
			t.Fatalf("missing %q:\n%s", want, screen)
		}
	}
	if strings.Contains(screen, "other.txt") {
		t.Fatalf("another session's file is listed:\n%s", screen)
	}
}

func TestFilesScreenOfAllSessionsListsEveryFileMostRecentFirst(t *testing.T) {
	screen := ansi.Strip(press(filesModel(120), "a", "d").render())
	other, util, mainGo := strings.Index(screen, "other.txt"), strings.Index(screen, "pkg/util.go"), strings.Index(screen, "main.go")
	if other < 0 || other > mainGo || mainGo > util {
		t.Fatalf("order wrong (%d %d %d):\n%s", other, util, mainGo, screen)
	}
}

func TestFilesScreenClosesOnEscapeAndOnD(t *testing.T) {
	for _, key := range []string{"esc", "d"} {
		if m := press(filesModel(120), "d", key); m.filesScreen.shown {
			t.Fatalf("%s did not close the files screen", key)
		}
	}
}

func TestFilesScreenShowsTheLoadedDiff(t *testing.T) {
	m := press(filesModel(120), "d")
	at := time.Date(2026, 10, 22, 10, 0, 0, 0, time.UTC)
	blocks := []patchBlock{{event: editEvent(at, "claude:a", "", "/repo/main.go", 1, 1), lines: []string{"@@ -1,2 +1,2 @@", " keep", "-old", "+new"}}}
	updated, _ := m.Update(filePatchesMsg{path: m.filesScreen.files[0].path, blocks: blocks})
	screen := ansi.Strip(updated.(model).render())
	for _, want := range []string{"claude", "keep", "-old", "+new"} {
		if !strings.Contains(screen, want) {
			t.Fatalf("missing %q:\n%s", want, screen)
		}
	}
}

func TestFilesScreenFitsTheTerminal(t *testing.T) {
	for _, width := range []int{60, 120, 200} {
		m := press(filesModel(width), "a", "d")
		lines := strings.Split(m.render(), "\n")
		if len(lines) != 30 {
			t.Fatalf("width %d: %d lines", width, len(lines))
		}
		for _, line := range lines {
			if ansi.StringWidth(line) > width {
				t.Fatalf("width %d: line too wide: %q", width, ansi.Strip(line))
			}
		}
	}
}

func TestUpScrollsTheFocusedDiffBackFromTheBottom(t *testing.T) {
	m := press(filesModel(120), "d", "enter")
	lines := make([]string, 200)
	for i := range lines {
		lines[i] = "+line"
	}
	at := time.Date(2026, 10, 22, 10, 0, 0, 0, time.UTC)
	blocks := []patchBlock{{event: editEvent(at, "claude:a", "", "/repo/main.go", 200, 0), lines: lines}}
	updated, _ := m.Update(filePatchesMsg{path: m.filesScreen.files[0].path, blocks: blocks})
	m = updated.(model)
	m.filesScreen.diff.GotoBottom()
	bottom := m.filesScreen.diff.YOffset()
	m = press(m, "up")
	if got := m.filesScreen.diff.YOffset(); got >= bottom {
		t.Fatalf("offset after up = %d, bottom = %d", got, bottom)
	}
}
