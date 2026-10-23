package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func helpModel(width, height int) model {
	return sized(newModel(nil, agentBatch(), Context{Repo: "/repo"}), width, height)
}

func TestQuestionMarkShowsTheKeyReference(t *testing.T) {
	m := press(helpModel(120, 60), "?")
	screen := ansi.Strip(m.render())
	for _, want := range []string{"space", "Global", "Sessions pane", "Events pane", "Detail pane", "Stats screen", "Files screen", "wheel", "click"} {
		if !strings.Contains(screen, want) {
			t.Fatalf("missing %q:\n%s", want, screen)
		}
	}
}

func TestEscAndQuestionMarkCloseTheKeyReference(t *testing.T) {
	for _, closer := range []string{"esc", "?"} {
		m := press(helpModel(120, 30), "?", closer)
		if m.help.shown {
			t.Fatalf("%s did not close the reference", closer)
		}
	}
}

func TestQuitsFromTheKeyReference(t *testing.T) {
	m := press(helpModel(120, 30), "?")
	_, quit := m.key(keyPress("q"))
	if !quit {
		t.Fatal("q did not quit")
	}
}

func TestQuestionMarkWhileSearchingTypesIntoTheFilter(t *testing.T) {
	m := press(helpModel(120, 30), "/", "?")
	if m.help.shown || m.search.Value() != "?" {
		t.Fatalf("shown = %v filter = %q", m.help.shown, m.search.Value())
	}
}

func TestKeyReferenceFitsEveryWidth(t *testing.T) {
	for _, width := range []int{60, 120, 200} {
		m := press(helpModel(width, 30), "?")
		lines := strings.Split(m.render(), "\n")
		if len(lines) != 30 {
			t.Fatalf("width %d: %d lines", width, len(lines))
		}
		for _, line := range lines {
			if got := ansi.StringWidth(line); got > width {
				t.Fatalf("width %d: line is %d wide: %q", width, got, line)
			}
		}
	}
}

func TestNarrowKeyReferenceScrolls(t *testing.T) {
	m := press(helpModel(60, 30), "?")
	if strings.Contains(ansi.Strip(m.render()), "Files screen") {
		t.Fatal("expected Files screen below the fold at 60 columns")
	}
	for range 6 {
		m = press(m, "pgdown")
		if strings.Contains(ansi.Strip(m.render()), "Files screen") {
			return
		}
	}
	t.Fatalf("scrolling did not reach Files screen:\n%s", ansi.Strip(m.render()))
}

func TestFooterStartsWithKeysHintInEveryPane(t *testing.T) {
	for _, focus := range []string{"1", "2", "3"} {
		m := press(helpModel(160, 30), focus)
		if !strings.HasPrefix(ansi.Strip(m.footer()), "? keys") {
			t.Fatalf("pane %s footer = %q", focus, ansi.Strip(m.footer()))
		}
	}
}
