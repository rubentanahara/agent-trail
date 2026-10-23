package tui

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/rubentanahara/agent-trail/internal/gh"
	"github.com/rubentanahara/agent-trail/internal/trail"
)

func TestTabsCycleBothWays(t *testing.T) {
	m := openedPRs(t, newFakeGH(t))

	if m = pressPR(m, "]", "]", "]", "]"); m.pr.tab != sessionsTab {
		t.Fatalf("tab = %d after four ]", m.pr.tab)
	}
	if m = pressPR(openedPRs(t, newFakeGH(t)), "["); m.pr.tab != sessionsTab {
		t.Fatalf("tab = %d after [ on Overview", m.pr.tab)
	}
}

func TestOverviewStripsControlSequencesFromTheBody(t *testing.T) {
	m := openedPRs(t, newFakeGH(t))
	number := m.pr.selectedNumber()
	detail := m.pr.details[number]
	detail.Body = "hello \x1b]0;pwned\x07 \x1b[31mred\x1b[0m world"
	detail.Title = "t\x1b[2Jitle"
	m.pr.details[number] = detail

	raw := m.render()

	for _, bad := range []string{"\x1b]0;pwned", "\x07", "\x1b[2J"} {
		if strings.Contains(raw, bad) {
			t.Fatalf("render leaks %q", bad)
		}
	}
	if !strings.Contains(ansi.Strip(raw), "hello") {
		t.Fatal("body text vanished")
	}
}

func TestChecksTabListsEveryCheckWithStateWords(t *testing.T) {
	m := pressPR(openedPRs(t, newFakeGH(t)), "]")

	text := screenText(m)
	if !strings.Contains(text, "label-external") || !strings.Contains(text, "skipped") {
		t.Fatalf("checks tab:\n%s", text)
	}
}

func TestChecksTabOpensTheSelectedCheckInTheBrowser(t *testing.T) {
	var opened string
	m := openedPRs(t, newFakeGH(t))
	m.pr.open = func(url string) error { opened = url; return nil }

	m = pressPR(m, "]", "2", "o")

	if !strings.HasPrefix(opened, "https://github.com/cli/cli/actions/runs/") {
		t.Fatalf("opened %q", opened)
	}
}

func TestCommentsTabLabelsThreadsInWords(t *testing.T) {
	m := pressPR(openedPRs(t, newFakeGH(t)), "]", "]")

	text := screenText(m)
	for _, want := range []string{"internal/ghcmd/global_flags.go:25", "unresolved"} {
		if !strings.Contains(text, want) {
			t.Fatalf("comments tab lacks %q:\n%s", want, text)
		}
	}
}

func TestThreadsAreFetchedOnlyWhenTheTabOpens(t *testing.T) {
	fake := newFakeGH(t)
	m := openedPRs(t, fake)
	if fake.count("api graphql") != 0 {
		t.Fatal("threads fetched before the Comments tab opened")
	}
	pressPR(m, "]", "]")
	if fake.count("api graphql") != 1 {
		t.Fatalf("graphql calls = %d", fake.count("api graphql"))
	}
}

func TestDiffTabShowsHeaderFreeCountsRenameAndBinary(t *testing.T) {
	m := pressPR(openedPRs(t, newFakeGH(t)), "]", "]", "]")

	text := screenText(m)
	for _, want := range []string{"docs/guide.md +2 −1", "old/name.go → new/name.go", "Binary files"} {
		if !strings.Contains(text, want) {
			t.Fatalf("diff tab lacks %q:\n%s", want, text)
		}
	}
}

func TestDiffTooLargeShowsTheDegradedState(t *testing.T) {
	fake := newFakeGH(t)
	for _, number := range []int{prForkPR, prBranchPR} {
		fake.fail(gh.DiffArgs(number), &gh.Error{Args: gh.DiffArgs(number), Stderr: "HTTP 406: the diff exceeded the maximum number of lines", Err: errors.New("exit status 1")})
	}
	m := pressPR(openedPRs(t, fake), "]", "]", "]")

	if !strings.Contains(screenText(m), "diff too large, open on GitHub") {
		t.Fatalf("diff tab:\n%s", screenText(m))
	}
}

func TestDiffTabStepsBetweenFiles(t *testing.T) {
	m := pressPR(openedPRs(t, newFakeGH(t)), "]", "]", "]", "2")
	start := m.pr.tabCursor[diffTab]

	m = pressPR(m, "n")

	if m.pr.tabCursor[diffTab] <= start {
		t.Fatalf("cursor %d -> %d", start, m.pr.tabCursor[diffTab])
	}
}

func linkedModel(t *testing.T, sessions ...trail.Session) model {
	t.Helper()
	m := settleBatch(prModel(t, newFakeGH(t)), sessions...)
	return settlePR(m, m.startPRs())
}

func TestSessionBadgeShowsForBranchHistoryMatches(t *testing.T) {
	m := linkedModel(t, sessionOnBranches("s1", clockAt(t, 0), prBranch, "other"))

	if got := ansi.Strip(m.sessionLabelRow(0, "s1")); !strings.Contains(got, "#14716") {
		t.Fatalf("row = %q", got)
	}
}

func TestCrossRepoPullRequestsAndEmptyBranchesDoNotLink(t *testing.T) {
	m := linkedModel(t, sessionOnBranches("fork", clockAt(t, 0), "release-download-verify"))

	if got := ansi.Strip(m.sessionLabelRow(0, "fork")); strings.Contains(got, "#") {
		t.Fatalf("fork PR linked: %q", got)
	}
	if got := m.prSessions(gh.PR{HeadRef: ""}); len(got) != 0 {
		t.Fatalf("empty branch linked: %v", got)
	}
}

func TestSeveralPullRequestsOnOneBranchShowPlusN(t *testing.T) {
	m := linkedModel(t, sessionOnBranches("s1", clockAt(t, 0), prBranch))
	twin := m.pr.byNumber[prBranchPR]
	twin.Number = 99
	m.pr.byNumber[99] = twin

	if got := ansi.Strip(m.sessionLabelRow(0, "s1")); !strings.Contains(got, "+1") {
		t.Fatalf("row = %q", got)
	}
}

func TestSessionsTabListsSessionsAndEnterJumpsToOne(t *testing.T) {
	m := linkedModel(t, sessionOnBranches("s1", clockAt(t, 0), prBranch))
	m = pressPR(m, "p", "down")
	if m.pr.selectedNumber() != prBranchPR {
		t.Fatalf("selected %d", m.pr.selectedNumber())
	}

	m = pressPR(m, "]", "]", "]", "]")
	if !strings.Contains(screenText(m), "s1") {
		t.Fatalf("sessions tab:\n%s", screenText(m))
	}
	m = pressPR(m, "2", "enter")

	if m.pr.shown || m.activeSession != "s1" || m.focus != eventsPane {
		t.Fatalf("shown = %v, active = %q, focus = %v", m.pr.shown, m.activeSession, m.focus)
	}
}

func TestJumpToAHiddenSessionShowsANoticeInstead(t *testing.T) {
	hidden := sessionOnBranches("s1", clockAt(t, 0), prBranch)
	hidden.Automated = true
	m := linkedModel(t, hidden)
	m.hideAutomated = true
	m.reorderSessions()

	m = pressPR(m, "p", "down", "]", "]", "]", "]", "2", "enter")

	if !m.pr.shown || !strings.Contains(m.notice, "hidden") {
		t.Fatalf("shown = %v, notice = %q", m.pr.shown, m.notice)
	}
}

func TestPOnASessionWithAPullRequestOpensThatPullRequest(t *testing.T) {
	m := linkedModel(t, sessionOnBranches("s1", clockAt(t, 0), prBranch))
	m.selectSession(m.rowOf("s1"))

	m = pressPR(m, "p")

	if !m.pr.shown || m.pr.selectedNumber() != prBranchPR {
		t.Fatalf("shown = %v, selected = %d", m.pr.shown, m.pr.selectedNumber())
	}
}

func TestSummedSessionCostAppearsOnTheOverview(t *testing.T) {
	a := sessionOnBranches("a", clockAt(t, 0), prBranch)
	a.PricedCalls, a.Billing, a.Tokens.CostUSD = 1, trail.BillingAPI, 1.5
	b := sessionOnBranches("b", clockAt(t, time.Second), prBranch)
	b.PricedCalls, b.Billing, b.Tokens.CostUSD = 1, trail.BillingAPI, 2.5
	m := linkedModel(t, a, b)

	m = pressPR(m, "p", "down")

	if text := screenText(m); !strings.Contains(text, "2 · $4") {
		t.Fatalf("overview:\n%s", text)
	}
}

func TestNarrowTerminalsFallBackToOnePaneWithACompactHeader(t *testing.T) {
	m := sized(openedPRs(t, newFakeGH(t)), 60, 20)

	text := screenText(m)
	if !strings.Contains(text, "PR #") || !strings.Contains(text, "tab 1/5 Overview") {
		t.Fatalf("compact header missing:\n%s", text)
	}
	for _, line := range strings.Split(m.render(), "\n") {
		if ansi.StringWidth(line) > 60 {
			t.Fatalf("line wider than the terminal: %q", ansi.Strip(line))
		}
	}
}

func TestHelpListsThePullRequestKeys(t *testing.T) {
	text := ansi.Strip(strings.Join(helpLines(), "\n"))

	if !strings.Contains(text, "Pull requests screen") || !strings.Contains(text, "jump to that session") {
		t.Fatal("help lacks the pull request section")
	}
}
