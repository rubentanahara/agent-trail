package tui

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/rubentanahara/agent-trail/internal/gh"
	"github.com/rubentanahara/agent-trail/internal/trail"
)

func TestPOpensTheScreenWithThreeSections(t *testing.T) {
	m := openedPRs(t, newFakeGH(t))

	if !m.pr.shown {
		t.Fatal("screen not shown")
	}
	text := screenText(m)
	for _, want := range []string{"Needs my review (1)", "My PRs (1)", "All open (2)", "rubentanahara/agent-trail"} {
		if !strings.Contains(text, want) {
			t.Fatalf("screen lacks %q:\n%s", want, text)
		}
	}
}

func TestEscClosesTheScreenAndGlobalModeSetsANotice(t *testing.T) {
	m := pressPR(openedPRs(t, newFakeGH(t)), "esc")
	if m.pr.shown {
		t.Fatal("esc should close")
	}

	global := sized(newModel(nil, sampleBatch(), Context{Repo: trail.EveryRepository}), 120, 30)
	global = pressPR(global, "p")
	if global.pr.shown || !strings.Contains(global.notice, "single repository") {
		t.Fatalf("shown = %v, notice = %q", global.pr.shown, global.notice)
	}
}

func TestFooterShowsTheNotice(t *testing.T) {
	m := openedPRs(t, newFakeGH(t))
	m.notice = "merged #7"

	if !strings.Contains(screenText(m), "merged #7") {
		t.Fatal("footer hides the notice")
	}
}

func TestOlderListResultsAndGenerationsAreDropped(t *testing.T) {
	m := openedPRs(t, newFakeGH(t))
	before := len(m.pr.rows)

	stale, _ := m.Update(prListMsg{seq: m.pr.list.seq - 1, generation: m.pr.generation, open: nil})
	if got := len(stale.(model).pr.rows); got != before {
		t.Fatalf("stale seq changed rows: %d -> %d", before, got)
	}
	m.pr.list.busy = true
	old, _ := m.Update(prListMsg{seq: m.pr.list.seq, generation: m.pr.generation - 1})
	if !old.(model).pr.list.busy {
		t.Fatal("older generation should be ignored")
	}
}

func TestPollWaitsThirtySecondsAndSkipsWhileInFlight(t *testing.T) {
	m := openedPRs(t, newFakeGH(t))
	m.now = clockAt(t, 0)
	m.pr.lastFetch = m.now

	early := m
	early.now = clockAt(t, 29*time.Second)
	if early.pollPRs(); early.pr.list.busy {
		t.Fatal("polled before 30s")
	}
	due := m
	due.now = clockAt(t, 31*time.Second)
	if cmd := due.pollPRs(); cmd == nil || !due.pr.list.busy {
		t.Fatal("no poll after 31s")
	}
	seq := due.pr.list.seq
	if due.pollPRs(); due.pr.list.seq != seq {
		t.Fatal("polled while in flight")
	}
}

func TestClosedScreenNeverPolls(t *testing.T) {
	m := prModel(t, newFakeGH(t))
	m.now = clockAt(t, time.Minute)

	if cmd := m.pollPRs(); cmd != nil {
		t.Fatal("a closed screen must not poll")
	}
}

func TestPollMessageStampsTheClockAndGatesTheFetch(t *testing.T) {
	m := openedPRs(t, newFakeGH(t))
	m.pr.lastFetch = clockAt(t, 0)

	updated, _ := m.Update(pollMsg{at: clockAt(t, 31*time.Second)})
	got := updated.(model)
	if !got.now.Equal(clockAt(t, 31*time.Second)) || !got.pr.list.busy {
		t.Fatalf("now = %v, busy = %v", got.now, got.pr.list.busy)
	}
}

func TestRefreshFetchesOnceEvenWhenPressedTwice(t *testing.T) {
	fake := newFakeGH(t)
	m := openedPRs(t, fake)
	listCall := "pr list --state=open --limit=100 --json"
	before := fake.count(listCall)

	updated, first := m.Update(tea.KeyPressMsg{Code: 'R', Text: "R"})
	_, second := updated.(model).Update(tea.KeyPressMsg{Code: 'R', Text: "R"})

	if countMessages(second) != 0 {
		t.Fatal("second R fetched while the first was in flight")
	}
	settlePR(updated.(model), first)
	if got := fake.count(listCall) - before; got != 1 {
		t.Fatalf("list fetched %d times, want 1", got)
	}
}

func TestPollRefetchesDetailOnlyWhenChangedOrPending(t *testing.T) {
	fake := newFakeGH(t)
	m := openedPRs(t, fake)
	views := fake.count("pr view")

	m.now = clockAt(t, time.Minute)
	m.pr.lastFetch = clockAt(t, 0)
	m = settlePR(m, m.fetchPRs())
	if got := fake.count("pr view"); got != views {
		t.Fatalf("unchanged PR refetched detail: %d -> %d", views, got)
	}

	pending := m.pr.details[m.pr.selectedNumber()]
	pending.Checks = append(pending.Checks, gh.Check{Name: "slow", State: gh.CheckPending})
	m.pr.details[m.pr.selectedNumber()] = pending
	m.pr.lastFetch = clockAt(t, 0)
	m = settlePR(m, m.fetchPRs())
	if got := fake.count("pr view"); got != views+1 {
		t.Fatalf("pending checks should refetch detail: %d -> %d", views, got)
	}
}

func TestStartupFetchFillsTheBadgeWithoutOpeningTheScreen(t *testing.T) {
	fake := newFakeGH(t)
	m := settleBatch(prModel(t, fake), sessionOnBranches("s1", clockAt(t, 0), prBranch))

	m = settlePR(m, m.startPRs())

	if m.pr.shown {
		t.Fatal("startup fetch must not open the screen")
	}
	if got := ansi.Strip(m.sessionLabelRow(0, "s1")); !strings.Contains(got, "#14716") {
		t.Fatalf("row lacks the PR badge: %q", got)
	}
	if fake.count("pr list --state=open --limit=100 --json") != 1 {
		t.Fatal("startup fetch should list once")
	}
}

func TestClosingTheScreenCancelsTheReadContext(t *testing.T) {
	m := openedPRs(t, newFakeGH(t))
	ctx := m.pr.readContext()

	m = pressPR(m, "esc")

	if !errors.Is(ctx.Err(), context.Canceled) {
		t.Fatalf("context not cancelled: %v", ctx.Err())
	}
}

func TestEachProblemRendersItsRemedy(t *testing.T) {
	cases := map[gh.Problem]string{
		gh.Unauthenticated: "Run `gh auth login`",
		gh.NoRemote:        "no GitHub remote",
		gh.AmbiguousRemote: "gh repo set-default",
		gh.MissingScope:    "gh auth refresh",
		gh.OutdatedGh:      "too old",
		gh.RateLimited:     "rate limit",
	}
	stderr := map[gh.Problem]string{
		gh.Unauthenticated: "To get started with GitHub CLI, please run:  gh auth login",
		gh.NoRemote:        "no git remotes found",
		gh.AmbiguousRemote: "No default remote repository has been set",
		gh.MissingScope:    "your token has not been granted the required scopes",
		gh.OutdatedGh:      "unknown flag: --json",
		gh.RateLimited:     "API rate limit exceeded (HTTP 403)",
	}
	for problem, want := range cases {
		fake := newFakeGH(t)
		fake.fail(gh.RateResetArgs(), errors.New("unavailable"))
		fake.fail(gh.RepoArgs(), &gh.Error{Args: gh.RepoArgs(), Stderr: stderr[problem], Err: errors.New("exit status 1")})
		m := openedPRs(t, fake)

		if !strings.Contains(screenText(m), want) {
			t.Errorf("%v: screen lacks %q:\n%s", problem, want, screenText(m))
		}
	}
}

func TestMissingGhShowsInstallRemedyAndLeavesTheMainScreenAlone(t *testing.T) {
	fake := newFakeGH(t)
	fake.fail(gh.RepoArgs(), &gh.Error{Args: gh.RepoArgs(), Err: errNotInstalled})
	m := openedPRs(t, fake)

	if !strings.Contains(screenText(m), "not installed") {
		t.Fatalf("no install remedy:\n%s", screenText(m))
	}
	m = pressPR(m, "esc")
	if !strings.Contains(screenText(m), "fix the login") {
		t.Fatal("main screen changed")
	}
}

func TestRateLimitedPausesPollingAndShowsRetry(t *testing.T) {
	fake := newFakeGH(t)
	fake.fail(gh.RepoArgs(), &gh.Error{Args: gh.RepoArgs(), Stderr: "API rate limit exceeded (HTTP 403)", Err: errors.New("exit status 1")})
	fake.set(gh.RateResetArgs(), []byte(strconv.FormatInt(clockAt(t, 4*time.Minute).Unix(), 10)))
	m := prModel(t, fake)
	m.now = clockAt(t, 0)
	m = pressPR(m, "p")

	if !m.pr.rateLimited(m.now) {
		t.Fatal("not rate limited")
	}
	if !strings.Contains(screenText(m), "retry in 4m") {
		t.Fatalf("header lacks retry:\n%s", screenText(m))
	}
	m.now = clockAt(t, time.Minute)
	if cmd := m.pollPRs(); cmd != nil {
		t.Fatal("must not poll while rate limited")
	}
}

func TestErrorsBackOffThirtySixtyThenOneTwenty(t *testing.T) {
	var p prScreen
	var delays []time.Duration
	for range 4 {
		p.failures++
		delays = append(delays, p.pollDelay())
	}
	want := []time.Duration{30 * time.Second, 60 * time.Second, 120 * time.Second, 120 * time.Second}
	for i := range want {
		if delays[i] != want[i] {
			t.Fatalf("delays = %v, want %v", delays, want)
		}
	}
}

func TestLoadingTextWaitsAboutOneHundredFiftyMilliseconds(t *testing.T) {
	m := prModel(t, newFakeGH(t))
	m.now = clockAt(t, 0)
	m.pr.shown, m.pr.openedAt = true, m.now

	if strings.Contains(screenText(m), "loading…") {
		t.Fatal("loading shown immediately")
	}
	m.now = clockAt(t, 200*time.Millisecond)
	if !strings.Contains(screenText(m), "loading…") {
		t.Fatal("loading not shown after 150ms")
	}
}

func TestSlashFiltersTheFocusedDetailPane(t *testing.T) {
	m := pressPR(openedPRs(t, newFakeGH(t)), "]", "2", "/", "s", "k", "i", "p", "enter")

	text := screenText(m)
	if !strings.Contains(text, "skipped") || strings.Contains(text, "passed") {
		t.Fatalf("detail not filtered:\n%s", text)
	}
	if m = pressPR(m, "esc"); m.pr.detailFilterText != "" {
		t.Fatal("esc should clear the detail filter first")
	}
}
