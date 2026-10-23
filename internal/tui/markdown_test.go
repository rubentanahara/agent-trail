package tui

import (
	"errors"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

const longBullet = "- a very long bullet that keeps going and going so that it must wrap at every width we try here, with plenty of extra words to make sure of it"

func markdownModel(text string, width int) model {
	runner := &fakeRunner{result: summaryResult{Text: text}}
	m := sized(newModel(nil, sampleBatch(), Context{Repo: "/repo"}), width, 40)
	m.summary.run = runner.run
	return pressY(press(m, "1", "down"))
}

func TestSummaryRendersMarkdown(t *testing.T) {
	m := markdownModel("## Goal\n- **bold** item", 140)
	got := detailText(m)
	if strings.Contains(got, "##") || strings.Contains(got, "**") {
		t.Fatalf("raw markdown left: %q", got)
	}
	if !strings.Contains(got, "Goal") || !strings.Contains(got, "• bold item") {
		t.Fatalf("detail = %q", got)
	}
}

func TestSummaryRewrapsOnWidthChange(t *testing.T) {
	m := markdownModel("## Goal\n"+longBullet, 120)
	wide := strings.Count(detailText(m), "\n")
	m = sized(m, 60, 40)
	if narrow := strings.Count(detailText(m), "\n"); narrow <= wide {
		t.Fatalf("lines narrow=%d wide=%d", narrow, wide)
	}
}

func TestSummaryNeverExceedsTheTerminal(t *testing.T) {
	for _, width := range []int{60, 120, 200} {
		m := markdownModel("## Goal\n"+longBullet, width)
		for _, line := range strings.Split(ansi.Strip(m.View().Content), "\n") {
			if ansi.StringWidth(line) > width {
				t.Fatalf("width %d: line %q is %d wide", width, line, ansi.StringWidth(line))
			}
		}
		for _, line := range strings.Split(detailText(m), "\n") {
			if line != strings.TrimRight(line, " ") {
				t.Fatalf("width %d: trailing spaces in %q", width, line)
			}
		}
	}
}

func TestSummaryFilterMatchesRenderedText(t *testing.T) {
	m := markdownModel("## Goal\n- **bold** item\n- other thing", 140)
	m.detailQuery = parseQuery("bold")
	got := detailText(m)
	if !strings.Contains(got, "bold") || strings.Contains(got, "other") {
		t.Fatalf("detail = %q", got)
	}
}

func TestSummaryFallsBackToRawTextWhenRenderingFails(t *testing.T) {
	runner := &fakeRunner{result: summaryResult{Text: "## Goal\n- raw"}}
	m := sized(newModel(nil, sampleBatch(), Context{Repo: "/repo"}), 140, 40)
	m.summary.run = runner.run
	m.summary.render = func(string, int) (string, error) { return "", errors.New("boom") }
	m = pressY(press(m, "1", "down"))
	if got := detailText(m); got != "## Goal\n- raw" {
		t.Fatalf("detail = %q", got)
	}
}

func TestSummaryRenderIsCachedPerWidth(t *testing.T) {
	calls := 0
	m := markdownModel("## Goal\n- x", 140)
	m.summary.render = func(text string, width int) (string, error) { calls++; return glamourRender(text, width) }
	m.refreshDetail()
	m.refreshDetail()
	m.refreshDetail()
	if calls > 1 {
		t.Fatalf("renders = %d", calls)
	}
}
