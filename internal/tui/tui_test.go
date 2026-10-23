package tui

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/rubentanahara/agent-trail/internal/savings"
	"github.com/rubentanahara/agent-trail/internal/trail"
)

func TestQueryMatchesFuzzyExactAndExcludedTerms(t *testing.T) {
	cases := []struct {
		query, haystack string
		want            bool
	}{
		{"", "anything", true},
		{"cdx usr", "codex user fix login", true},
		{"'codex", "copilot user", false},
		{"'codex !failed", "codex result failed exit 1", false},
		{"Claude TOOL", "claude tool bash", true},
		{"xyz", "claude tool bash", false},
	}
	for _, c := range cases {
		if got := parseQuery(c.query).matches(c.haystack); got != c.want {
			t.Errorf("parseQuery(%q).matches(%q) = %v, want %v", c.query, c.haystack, got, c.want)
		}
	}
}

func sampleBatch() trail.Batch {
	at := time.Date(2026, 10, 22, 10, 0, 0, 0, time.UTC)
	event := func(seconds int, cli trail.CLI, session string, kind trail.Kind, summary string) trail.Event {
		return trail.Event{Time: at.Add(time.Duration(seconds) * time.Second), CLI: cli, Session: session, Kind: kind, Summary: summary}
	}
	return trail.Batch{
		Events: []trail.Event{
			event(0, trail.Claude, "claude:a", trail.KindUser, "fix the login"),
			event(1, trail.Codex, "codex:b", trail.KindUser, "write tests"),
			event(2, trail.Claude, "claude:a", trail.KindUsage, "claude-opus-5-5 in 10 out 5"),
			event(3, trail.Codex, "codex:b", trail.KindResult, "FAILED exit 1"),
		},
		Sessions: []trail.Session{
			{Key: "claude:a", ID: "a", CLI: trail.Claude, Name: "login", Events: 2, Last: at.Add(2 * time.Second), Billing: trail.BillingAPI, PricedCalls: 1, Calls: 1},
			{Key: "codex:b", ID: "b", CLI: trail.Codex, Name: "tests", Events: 2, Last: at.Add(3 * time.Second), Billing: trail.BillingSubscription, HasRateLimit: true},
		},
	}
}

func press(m model, keys ...string) model {
	for _, key := range keys {
		var msg tea.KeyPressMsg
		switch key {
		case "enter":
			msg = tea.KeyPressMsg{Code: tea.KeyEnter}
		case "esc":
			msg = tea.KeyPressMsg{Code: tea.KeyEscape}
		case "tab":
			msg = tea.KeyPressMsg{Code: tea.KeyTab}
		case "down":
			msg = tea.KeyPressMsg{Code: tea.KeyDown}
		case "up":
			msg = tea.KeyPressMsg{Code: tea.KeyUp}
		default:
			msg = tea.KeyPressMsg{Code: []rune(key)[0], Text: key}
		}
		updated, _ := m.Update(msg)
		m = updated.(model)
	}
	return m
}

func sized(m model, width, height int) model {
	updated, _ := m.Update(tea.WindowSizeMsg{Width: width, Height: height})
	return updated.(model)
}

func visibleSummaries(m model) []string {
	var out []string
	for _, index := range m.visible {
		out = append(out, m.events[index].Summary)
	}
	return out
}

func TestSessionsAreOrderedByLatestActivity(t *testing.T) {
	m := newModel(nil, sampleBatch(), Context{Repo: "/repo"})
	if strings.Join(m.order, ",") != "codex:b,claude:a" {
		t.Fatalf("order = %v", m.order)
	}
}

func TestMovingInSessionsPaneFiltersEventsToThatSession(t *testing.T) {
	m := sized(newModel(nil, sampleBatch(), Context{Repo: "/repo"}), 120, 40)
	m = press(m, "tab", "tab", "down")
	if got := strings.Join(visibleSummaries(m), ","); got != "write tests,FAILED exit 1" {
		t.Fatalf("visible = %q", got)
	}
	m = press(m, "a")
	if len(m.visible) != 4 {
		t.Fatalf("after a, visible = %d", len(m.visible))
	}
}

func TestSearchFiltersAndEscapeClears(t *testing.T) {
	m := sized(newModel(nil, sampleBatch(), Context{Repo: "/repo"}), 120, 40)
	m = press(m, "/", "'", "f", "a", "i", "l", "enter")
	if got := strings.Join(visibleSummaries(m), ","); got != "FAILED exit 1" {
		t.Fatalf("visible = %q", got)
	}
	m = press(m, "esc")
	if len(m.visible) != 4 {
		t.Fatalf("after esc, visible = %d", len(m.visible))
	}
}

func TestJumpToSelectedEventsSession(t *testing.T) {
	m := sized(newModel(nil, sampleBatch(), Context{Repo: "/repo"}), 120, 40)
	m = press(m, "up", "up", "up", "s")
	if m.activeSession != "claude:a" || len(m.visible) != 2 {
		t.Fatalf("active = %q visible = %d", m.activeSession, len(m.visible))
	}
}

func TestUsageRowsCanBeHidden(t *testing.T) {
	m := press(sized(newModel(nil, sampleBatch(), Context{Repo: "/repo"}), 120, 40), "u")
	for _, summary := range visibleSummaries(m) {
		if strings.HasPrefix(summary, "claude-opus") {
			t.Fatal("usage row still visible")
		}
	}
}

func TestNewEventsKeepFollowingTheNewest(t *testing.T) {
	m := sized(newModel(nil, sampleBatch(), Context{Repo: "/repo"}), 120, 40)
	more := trail.Batch{Events: []trail.Event{{Time: time.Now(), CLI: trail.Copilot, Session: "copilot:c", Kind: trail.KindUser, Summary: "newest"}}}
	updated, _ := m.Update(batchMsg{more})
	m = updated.(model)
	if m.events[m.selectedEvent()].Summary != "newest" {
		t.Fatalf("selected = %q", m.events[m.selectedEvent()].Summary)
	}
}

func TestRenderFitsTheTerminalAtAnySize(t *testing.T) {
	for _, size := range [][2]int{{0, 0}, {10, 5}, {20, 8}, {60, 20}, {69, 12}, {70, 12}, {200, 60}} {
		m := sized(newModel(nil, sampleBatch(), Context{Repo: "/repo"}), size[0], size[1])
		lines := strings.Split(m.render(), "\n")
		if size[0] >= minimumWidth && size[1] >= minimumHeight && len(lines) != size[1] {
			t.Errorf("%v: %d lines, want %d", size, len(lines), size[1])
		}
		for _, line := range lines {
			if width := ansi.StringWidth(line); size[0] >= minimumWidth && width > size[0] {
				t.Errorf("%v: line width %d exceeds terminal", size, width)
			}
		}
	}
}

func TestSlashFiltersTheFocusedPane(t *testing.T) {
	m := sized(newModel(nil, sampleBatch(), Context{Repo: "/repo"}), 120, 40)
	m = press(m, "1", "/", "t", "e", "s", "t", "s", "enter")
	if strings.Join(m.order, ",") != "codex:b" || len(m.visible) != 4 {
		t.Fatalf("sessions = %v visible events = %d", m.order, len(m.visible))
	}
	m = press(m, "esc")
	if len(m.order) != 2 {
		t.Fatalf("after esc, sessions = %v", m.order)
	}
}

func TestDetailFilterKeepsOnlyMatchingLines(t *testing.T) {
	m := sized(newModel(nil, sampleBatch(), Context{Repo: "/repo"}), 120, 40)
	m.detailShown, m.detailData = 0, trail.Detail{Text: "alpha line\nbeta line\nalpha again"}
	m = press(m, "3", "/", "a", "l", "p", "h", "a", "enter")
	content := ansi.Strip(m.detailContent())
	if strings.Contains(content, "beta") || strings.Count(content, "alpha") != 2 {
		t.Fatalf("content = %q", content)
	}
}

func TestColorJSONKeepsTheTextIntact(t *testing.T) {
	pretty := "{\n  \"key\": \"va\\\"lue\",\n  \"n\": -1.5e3,\n  \"ok\": true,\n  \"none\": null\n}"
	if got := ansi.Strip(colorJSON(pretty)); got != pretty {
		t.Fatalf("got %q", got)
	}
}

func TestEventsTitleShowsTheRealStateOfTheSelectedSession(t *testing.T) {
	batch := sampleBatch()
	batch.Sessions[0].Liveness = trail.LivenessClosed
	batch.Sessions[1].Liveness = trail.LivenessRunning
	m := sized(newModel(nil, batch, Context{Repo: "/repo"}), 160, 40)
	if title := ansi.Strip(m.eventsTitle()); !strings.Contains(title, "1 running") {
		t.Fatalf("all-sessions title = %q", title)
	}
	m = press(m, "1", "down", "down")
	if title := ansi.Strip(m.eventsTitle()); !strings.Contains(title, "closed") || strings.Contains(title, "running") {
		t.Fatalf("closed-session title = %q", title)
	}
}

func TestHighlightMarksTheMatchedCharacters(t *testing.T) {
	brackets := lipgloss.NewStyle().Transform(func(s string) string { return "[" + s + "]" })
	cases := []struct{ query, line, want string }{
		{"cdx", "codex user", "[c]o[d]e[x] user"},
		{"co", "codex", "[co]dex"},
		{"'us", "user uses", "[us]er [us]es"},
		{"!user", "user", "user"},
		{"zz", "fizz buzz", "fi[zz] buzz"},
		{"zq", "fizz buzz", "fizz buzz"},
		{"'abc", "⚠️ abc", "⚠️ [abc]"},
		{"'abc", "👨‍👩‍👧 abc", "👨‍👩‍👧 [abc]"},
		{"'ab", "日本 ab", "日本 [ab]"},
	}
	for _, c := range cases {
		if got := ansi.Strip(parseQuery(c.query).highlight(c.line, brackets)); got != c.want {
			t.Errorf("highlight(%q, %q) = %q, want %q", c.query, c.line, got, c.want)
		}
	}
}

func TestDetailFilterMatchesFuzzily(t *testing.T) {
	m := sized(newModel(nil, sampleBatch(), Context{Repo: "/repo"}), 120, 40)
	m.detailShown, m.detailData = 0, trail.Detail{Text: "make check passed\nbeta line"}
	m = press(m, "3", "/", "m", "c", "k", "enter")
	if content := ansi.Strip(m.detailContent()); !strings.Contains(content, "make check") || strings.Contains(content, "beta") {
		t.Fatalf("content = %q", content)
	}
}

func TestZoomShowsOnlyTheFocusedPane(t *testing.T) {
	m := press(sized(newModel(nil, sampleBatch(), Context{Repo: "/repo"}), 120, 40), "z")
	screen := ansi.Strip(m.render())
	if !strings.Contains(screen, "Events:") || strings.Contains(screen, "Sessions (") || strings.Contains(screen, "Detail") {
		t.Fatalf("events zoom shows other panes:\n%s", screen)
	}
	m = press(m, "1")
	if screen = ansi.Strip(m.render()); !strings.Contains(screen, "Sessions (") || strings.Contains(screen, "Events:") {
		t.Fatalf("zoom did not follow focus:\n%s", screen)
	}
	m = press(m, "z")
	if screen = ansi.Strip(m.render()); !strings.Contains(screen, "Sessions (") || !strings.Contains(screen, "Events:") {
		t.Fatalf("z did not restore the layout:\n%s", screen)
	}
}

func TestRunningOnlyHidesClosedSessions(t *testing.T) {
	batch := sampleBatch()
	batch.Sessions[1].Liveness = trail.LivenessRunning
	m := press(sized(newModel(nil, batch, Context{Repo: "/repo"}), 120, 40), "o")
	if strings.Join(m.order, ",") != "codex:b" {
		t.Fatalf("order = %v", m.order)
	}
	if m = press(m, "o"); len(m.order) != 2 {
		t.Fatalf("after second o, order = %v", m.order)
	}
}

func TestDeleteAsksFirstAndCancelsOnAnyOtherKey(t *testing.T) {
	m := sized(newModel(nil, sampleBatch(), Context{Repo: "/repo"}), 120, 40)
	m = press(m, "1", "down", "x")
	if m.confirmDelete != "codex:b" || !strings.Contains(m.footer(), "permanently") {
		t.Fatalf("confirm = %q footer = %q", m.confirmDelete, m.footer())
	}
	m = press(m, "n")
	if m.confirmDelete != "" || len(m.order) != 2 {
		t.Fatalf("after n: confirm = %q order = %v", m.confirmDelete, m.order)
	}
}

func TestDeletedSessionDisappearsWithItsEvents(t *testing.T) {
	m := sized(newModel(nil, sampleBatch(), Context{Repo: "/repo"}), 120, 40)
	m = press(m, "1", "down")
	updated, _ := m.Update(deletedMsg{key: "codex:b", label: "tests"})
	m = updated.(model)
	if strings.Join(m.order, ",") != "claude:a" || m.activeSession != "" || len(m.events) != 2 || len(m.visible) != 2 {
		t.Fatalf("order = %v active = %q events = %d visible = %d", m.order, m.activeSession, len(m.events), len(m.visible))
	}
}

func TestStatsScreenFitsTheTerminal(t *testing.T) {
	m := sized(newModel(nil, sampleBatch(), Context{Repo: "/repo"}), 120, 30)
	m = press(m, "t")
	updated, _ := m.Update(statsMsg{sampleBatch()})
	m = updated.(model)
	lines := strings.Split(m.render(), "\n")
	if len(lines) != 30 || !strings.Contains(ansi.Strip(m.render()), "Which CLI") {
		t.Fatalf("%d lines:\n%s", len(lines), ansi.Strip(m.render()))
	}
	for _, line := range lines {
		if ansi.StringWidth(line) > 120 {
			t.Fatalf("line too wide: %q", ansi.Strip(line))
		}
	}
	if m = press(m, "esc"); m.statsScreen.shown {
		t.Fatal("esc did not leave the stats screen")
	}
}

func TestVersionSitsInTheBottomRightCorner(t *testing.T) {
	for _, screen := range []string{"", "t"} {
		m := sized(newModel(nil, sampleBatch(), Context{Repo: "/repo", Version: "1.2.3"}), 120, 30)
		if screen != "" {
			m = press(m, screen)
		}
		lines := strings.Split(ansi.Strip(m.render()), "\n")
		if last := lines[len(lines)-1]; !strings.HasSuffix(last, " agent-trail 1.2.3") || ansi.StringWidth(last) != 120 {
			t.Fatalf("screen %q footer = %q", screen, last)
		}
	}
}

func TestStatsShowsSavingsAndTogglesAutomatedSessions(t *testing.T) {
	batch := sampleBatch()
	batch.Sessions[1].Automated = true
	m := sized(newModel(nil, sampleBatch(), Context{Repo: "/repo"}), 160, 40)
	m = press(m, "t")
	for _, msg := range []tea.Msg{statsMsg{batch}, savingsMsg{[]savings.Saving{{Tool: "headroom", CLI: savings.AnyCLI, Tokens: 2_000_000, USD: 3, HasUSD: true, Since: time.Date(2026, 8, 1, 12, 0, 0, 0, time.Local)}}}} {
		updated, _ := m.Update(msg)
		m = updated.(model)
	}
	screen := ansi.Strip(m.render())
	if !strings.Contains(screen, "headroom · any CLI") || !strings.Contains(screen, "2.0M tokens since 2026-08-01 · $3.00 by headroom's count") {
		t.Fatalf("savings missing:\n%s", screen)
	}
	if !strings.Contains(screen, "1 automated sessions hidden") || strings.Contains(screen, "codex ") {
		t.Fatalf("automated session not hidden:\n%s", screen)
	}
	if screen = ansi.Strip(press(m, "a").render()); strings.Contains(screen, "automated sessions hidden") || !strings.Contains(screen, "codex ") {
		t.Fatalf("a did not show automated sessions:\n%s", screen)
	}
}

func agentBatch() trail.Batch {
	batch := sampleBatch()
	at := time.Date(2026, 10, 22, 10, 0, 0, 0, time.UTC)
	batch.Sessions = append(batch.Sessions,
		trail.Session{Key: "claude:a/x1", ID: "x1", Parent: "claude:a", CLI: trail.Claude, Name: "Review the code", Agent: "reviewer", Events: 1, Start: at.Add(5 * time.Second), Last: at.Add(9 * time.Second), Liveness: trail.LivenessRunning, Tokens: trail.Usage{CostUSD: 2}, PricedCalls: 1, Calls: 1},
		trail.Session{Key: "claude:a/x2", ID: "x2", Parent: "claude:a", CLI: trail.Claude, Name: "Find the docs", Agent: "web-fetch", Events: 1, Start: at.Add(6 * time.Second), Last: at.Add(7 * time.Second), Finished: true},
	)
	batch.Sessions[0].Tokens.CostUSD = 1
	batch.Events = append(batch.Events,
		trail.Event{Time: at.Add(6 * time.Second), CLI: trail.Claude, Agent: "reviewer", Session: "claude:a/x1", Kind: trail.KindAssistant, Summary: "reviewing"},
		trail.Event{Time: at.Add(7 * time.Second), CLI: trail.Claude, Agent: "web-fetch", Session: "claude:a/x2", Kind: trail.KindAssistant, Summary: "fetching"},
	)
	return batch
}

func TestAgentsAreListedUnderTheirParentSession(t *testing.T) {
	m := sized(newModel(nil, agentBatch(), Context{Repo: "/repo"}), 160, 40)
	if got := strings.Join(m.order, ","); got != "claude:a,claude:a/x1,claude:a/x2,codex:b" {
		t.Fatalf("order = %s", got)
	}
	screen := ansi.Strip(m.render())
	for _, want := range []string{"├ ● Review the code · reviewer", "└ ✓ Find the docs · web-fetch", "$3.00"} {
		if !strings.Contains(screen, want) {
			t.Fatalf("missing %q:\n%s", want, screen)
		}
	}
}

func TestSelectingAParentShowsItsAgentsEventsAndSelectingAnAgentShowsOnlyIts(t *testing.T) {
	m := sized(newModel(nil, agentBatch(), Context{Repo: "/repo"}), 160, 40)
	m = press(m, "1", "down")
	if got := strings.Join(visibleSummaries(m), ","); got != "fix the login,claude-opus-5-5 in 10 out 5,reviewing,fetching" {
		t.Fatalf("parent events = %q", got)
	}
	m = press(m, "down", "down")
	if got := strings.Join(visibleSummaries(m), ","); got != "fetching" || m.activeSession != "claude:a/x2" {
		t.Fatalf("agent events = %q active = %s", got, m.activeSession)
	}
}

func TestFinishedAgentsCollapseUnderAClosedParentUntilItIsSelected(t *testing.T) {
	batch := agentBatch()
	batch.Sessions[2].Liveness = trail.LivenessClosed
	m := sized(newModel(nil, batch, Context{Repo: "/repo"}), 160, 40)
	if got := strings.Join(m.order, ","); got != "claude:a,codex:b" {
		t.Fatalf("collapsed order = %s", got)
	}
	m = press(m, "1", "down")
	if got := strings.Join(m.order, ","); got != "claude:a,claude:a/x1,claude:a/x2,codex:b" || m.sessionCursor != 1 {
		t.Fatalf("expanded order = %s cursor = %d", got, m.sessionCursor)
	}
	m = press(m, "down", "down", "down")
	if m.activeSession != "codex:b" || m.sessionCursor != 2 {
		t.Fatalf("after leaving the family: active = %s cursor = %d order = %v", m.activeSession, m.sessionCursor, m.order)
	}
}

func TestAgentsAreDeletedOnlyWithTheirParent(t *testing.T) {
	m := sized(newModel(nil, agentBatch(), Context{Repo: "/repo"}), 160, 40)
	m = press(m, "1", "down", "down", "x")
	if m.confirmDelete != "" || !strings.Contains(m.notice, "parent") {
		t.Fatalf("confirm = %q notice = %q", m.confirmDelete, m.notice)
	}
	updated, _ := m.Update(deletedMsg{key: "claude:a", label: "login"})
	m = updated.(model)
	if len(m.sessions) != 1 || len(m.events) != 2 {
		t.Fatalf("sessions = %v events = %d", m.sessions, len(m.events))
	}
}

func TestContinueOpensTheParentSessionAndRefusesARunningOne(t *testing.T) {
	m := sized(newModel(nil, agentBatch(), Context{Repo: "/repo"}), 160, 40)
	m = press(m, "1", "down", "down")
	target, ok := m.resumeTarget()
	if !ok || target.Key != "claude:a" {
		t.Fatalf("target = %+v", target)
	}
	if got := strings.Join(trail.ResumeCommand(target), " "); got != "claude --resume a" {
		t.Fatalf("command = %q", got)
	}
	if cmd := m.resume(); cmd == nil {
		t.Fatalf("closed session not opened: %q", m.notice)
	}
	batch := agentBatch()
	batch.Sessions[0].Liveness, batch.Sessions[0].PID = trail.LivenessRunning, 42
	m = press(sized(newModel(nil, batch, Context{Repo: "/repo"}), 160, 40), "1", "down")
	if cmd := m.resume(); cmd != nil || !strings.Contains(m.notice, "already open in pid 42") {
		t.Fatalf("running session: notice = %q", m.notice)
	}
}

func TestEveryRepositoryHidesAutomatedSessionsUntilShown(t *testing.T) {
	batch := sampleBatch()
	batch.Sessions[1].Automated, batch.Sessions[1].Cwd = true, "/work/tests"
	batch.Sessions[0].Cwd = "/work/login"
	m := sized(newModel(nil, batch, Context{Repo: trail.EveryRepository}), 160, 40)
	if got := strings.Join(m.order, ","); got != "claude:a" || len(m.visible) != 2 {
		t.Fatalf("order = %s visible = %d", got, len(m.visible))
	}
	if screen := ansi.Strip(m.render()); !strings.Contains(screen, "login · login") || !strings.Contains(screen, "every repository") {
		t.Fatalf("project hint or repo label missing:\n%s", screen)
	}
	if m = press(m, "m"); len(m.order) != 2 || len(m.visible) != 4 {
		t.Fatalf("after m: order = %v visible = %d", m.order, len(m.visible))
	}
}
