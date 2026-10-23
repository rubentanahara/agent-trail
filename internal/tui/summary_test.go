package tui

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/rubentanahara/agent-trail/internal/trail"
)

type fakeRunner struct {
	prompts []string
	result  summaryResult
	err     error
}

func (f *fakeRunner) run(prompt string) (summaryResult, error) {
	f.prompts = append(f.prompts, prompt)
	return f.result, f.err
}

func summaryModel(runner *fakeRunner) model {
	m := sized(newModel(nil, sampleBatch(), Context{Repo: "/repo"}), 140, 40)
	m.summary.run = runner.run
	return press(m, "1", "down")
}

func settle(m model, cmd tea.Cmd) model {
	if cmd == nil {
		return m
	}
	switch msg := cmd().(type) {
	case tea.BatchMsg:
		for _, inner := range msg {
			m = settle(m, inner)
		}
	case summaryMsg:
		updated, _ := m.Update(msg)
		m = updated.(model)
	}
	return m
}

func pressY(m model) model {
	updated, cmd := m.Update(tea.KeyPressMsg{Code: 'y', Text: "y"})
	return settle(updated.(model), cmd)
}

func detailText(m model) string {
	return ansi.Strip(m.detailContent())
}

func TestSummaryKeyWithoutASessionSaysToSelectOne(t *testing.T) {
	runner := &fakeRunner{}
	m := sized(newModel(nil, sampleBatch(), Context{Repo: "/repo"}), 140, 40)
	m.summary.run = runner.run
	m = pressY(m)
	if m.notice != "select a session to summarize" || len(runner.prompts) != 0 {
		t.Fatalf("notice = %q, calls = %d", m.notice, len(runner.prompts))
	}
}

func TestSummaryKeyShowsTheSummaryAndTogglesBack(t *testing.T) {
	runner := &fakeRunner{result: summaryResult{Text: "Goal\n- ship it", CostUSD: 0.0012}}
	m := pressY(summaryModel(runner))
	if !strings.Contains(detailText(m), "ship it") {
		t.Fatalf("detail = %q", detailText(m))
	}
	title := ansi.Strip(m.detailTitle())
	if !strings.HasPrefix(title, "Summary: tests") || !strings.Contains(title, "haiku") || !strings.Contains(title, "$0.0012") {
		t.Fatalf("title = %q", title)
	}
	if !strings.Contains(runner.prompts[0], "write tests") || strings.Contains(runner.prompts[0], "fix the login") {
		t.Fatalf("prompt = %q", runner.prompts[0])
	}
	m = pressY(m)
	if strings.HasPrefix(ansi.Strip(m.detailTitle()), "Summary") {
		t.Fatalf("title after second y = %q", m.detailTitle())
	}
}

func TestCachedSummaryIsReusedUntilNewEventsArrive(t *testing.T) {
	runner := &fakeRunner{result: summaryResult{Text: "first"}}
	m := pressY(pressY(pressY(summaryModel(runner))))
	if len(runner.prompts) != 1 {
		t.Fatalf("calls = %d, want 1", len(runner.prompts))
	}
	extra := trail.Event{Time: time.Now(), CLI: trail.Codex, Session: "codex:b", Kind: trail.KindUser, Summary: "one more thing"}
	m.absorb(trail.Batch{Events: []trail.Event{extra}})
	m = pressY(pressY(m))
	if len(runner.prompts) != 2 || !strings.Contains(runner.prompts[1], "one more thing") {
		t.Fatalf("prompts = %q", runner.prompts)
	}
}

func TestSummaryFailureShowsTheErrorText(t *testing.T) {
	runner := &fakeRunner{err: errors.New("credit balance is too low")}
	m := pressY(summaryModel(runner))
	if !strings.Contains(detailText(m), "credit balance is too low") {
		t.Fatalf("detail = %q", detailText(m))
	}
	runner.err = nil
	runner.result = summaryResult{Text: "recovered"}
	m = pressY(pressY(m))
	if !strings.Contains(detailText(m), "recovered") {
		t.Fatalf("detail after retry = %q", detailText(m))
	}
}

func TestSummaryCommandIsolatesHooksAndPlugins(t *testing.T) {
	args := summaryArgs()
	for _, want := range []string{"-p", "--no-session-persistence", "--disable-slash-commands", "--setting-sources", "--settings"} {
		if !slices.Contains(args, want) {
			t.Errorf("args lack %s: %q", want, args)
		}
	}
	if i := slices.Index(args, "--tools"); i < 0 || args[i+1] != "" {
		t.Errorf("tools are not disabled: %q", args)
	}
	if i := slices.Index(args, "--model"); i < 0 || args[i+1] != "haiku" {
		t.Errorf("model is not haiku: %q", args)
	}
}

func TestSummaryOutputDecodesTextCostAndErrors(t *testing.T) {
	ok, err := decodeSummary([]byte(`{"is_error":false,"result":"Goal","total_cost_usd":0.002}`))
	if err != nil || ok.Text != "Goal" || ok.CostUSD != 0.002 {
		t.Fatalf("ok = %+v, err = %v", ok, err)
	}
	if _, err := decodeSummary([]byte(`{"is_error":true,"result":"Not logged in"}`)); err == nil || err.Error() != "Not logged in" {
		t.Fatalf("err = %v", err)
	}
	if _, err := decodeSummary([]byte(`garbage`)); err == nil {
		t.Fatal("garbage decoded")
	}
}

func TestSummaryKeyOnAnotherSessionSummarizesIt(t *testing.T) {
	runner := &fakeRunner{result: summaryResult{Text: "done"}}
	m := pressY(summaryModel(runner))
	m = pressY(press(m, "down"))
	if len(runner.prompts) != 2 || !strings.HasPrefix(ansi.Strip(m.detailTitle()), "Summary: login") {
		t.Fatalf("calls = %d, title = %q", len(runner.prompts), m.detailTitle())
	}
}

func TestMissingClaudeBinaryReportsTheRunError(t *testing.T) {
	_, err := runSummaryCommand("agent-trail-no-such-binary", "prompt")
	if err == nil || !strings.Contains(err.Error(), "executable file not found") {
		t.Fatalf("err = %v", err)
	}
}

func TestSummaryCommandCannotBeHeldOpenByGrandchildren(t *testing.T) {
	command := summaryCommand(context.Background(), "claude")
	if command.WaitDelay != summaryWaitDelay {
		t.Fatalf("WaitDelay = %v", command.WaitDelay)
	}
}
