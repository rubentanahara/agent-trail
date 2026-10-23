package trail

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"
)

const testRepo = "/work/app"

type fixture struct {
	t    *testing.T
	home string
}

func newFixture(t *testing.T) fixture {
	return fixture{t: t, home: t.TempDir()}
}

func (f fixture) write(path string, lines ...string) string {
	f.t.Helper()
	full := filepath.Join(f.home, path)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		f.t.Fatal(err)
	}
	handle, err := os.OpenFile(full, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		f.t.Fatal(err)
	}
	defer handle.Close()
	for _, line := range lines {
		if _, err := handle.WriteString(line + "\n"); err != nil {
			f.t.Fatal(err)
		}
	}
	return full
}

func (f fixture) appendRaw(path, text string) {
	f.t.Helper()
	handle, err := os.OpenFile(filepath.Join(f.home, path), os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		f.t.Fatal(err)
	}
	defer handle.Close()
	if _, err := handle.WriteString(text); err != nil {
		f.t.Fatal(err)
	}
}

func claudePrompt(text, cwd, stamp string) string {
	return `{"type":"user","sessionId":"s","cwd":"` + cwd + `","timestamp":"` + stamp + `","message":{"content":"` + text + `"}}`
}

func codexMeta(cwd string) string {
	return `{"timestamp":"2026-10-22T10:00:00Z","type":"session_meta","payload":{"cwd":"` + cwd + `","cli_version":"1"}}`
}

const (
	stampNow   = "2026-10-22T10:00:00Z"
	stampLater = "2026-10-22T10:00:05Z"
	claudeDir  = ".claude/projects/-work-app/"
	codexDir   = ".codex/sessions/2026/10/22/"
)

func summaries(events []Event) []string {
	var out []string
	for _, event := range events {
		out = append(out, event.Summary)
	}
	return out
}

func assertSummaries(t *testing.T, events []Event, want ...string) {
	t.Helper()
	if got := strings.Join(summaries(events), " | "); got != strings.Join(want, " | ") {
		t.Fatalf("summaries = %q, want %q", got, strings.Join(want, " | "))
	}
}

func TestSessionsInSubfoldersAreIncludedAndSiblingReposAreNot(t *testing.T) {
	f := newFixture(t)
	f.write(codexDir+"rollout-sub-00000000-0000-0000-0000-000000000001.jsonl", codexMeta(testRepo+"/src"))
	f.write(codexDir+"rollout-sib-00000000-0000-0000-0000-000000000002.jsonl", codexMeta(testRepo+"-other"))
	f.write(".claude/projects/-work-app-src/a.jsonl", claudePrompt("from src", testRepo+"/src", stampNow))
	f.write(".claude/projects/-work-app-other/b.jsonl", claudePrompt("from sibling", testRepo+"-other", stampNow))
	f.write(".copilot/session-state/sub/workspace.yaml", "cwd: "+testRepo+"/src", "git_root: "+testRepo)
	f.write(".copilot/session-state/sub/events.jsonl", `{"type":"user.message","timestamp":"`+stampNow+`","data":{"content":"copilot-sub"}}`)
	f.write(".copilot/session-state/sib/workspace.yaml", "cwd: "+testRepo+"-other", "git_root: "+testRepo+"-other")
	f.write(".copilot/session-state/sib/events.jsonl", `{"type":"user.message","timestamp":"`+stampNow+`","data":{"content":"copilot-sibling"}}`)
	got := summaries(New(f.home, testRepo).Start(time.Time{}).Events)
	sort.Strings(got)
	if strings.Join(got, ",") != "copilot-sub,from src,start 1" {
		t.Fatalf("got %q", got)
	}
}

func TestHistoryIsSortedAcrossYears(t *testing.T) {
	f := newFixture(t)
	f.write(claudeDir+"a.jsonl", claudePrompt("newer", testRepo, "2026-01-01T00:00:00Z"), claudePrompt("older", testRepo, "2025-12-31T23:00:00Z"))
	assertSummaries(t, New(f.home, testRepo).Start(time.Time{}).Events, "older", "newer")
}

func TestHistoryBeforeCutoffIsSkippedButCountsTowardSessionTotals(t *testing.T) {
	f := newFixture(t)
	f.write(claudeDir+"a.jsonl", claudePrompt("old", testRepo, "2020-01-01T00:00:00Z"), claudePrompt("recent", testRepo, stampNow))
	batch := New(f.home, testRepo).Start(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	assertSummaries(t, batch.Events, "recent")
	if len(batch.Sessions) != 1 || batch.Sessions[0].Start.Year() != 2020 || batch.Sessions[0].Events != 1 {
		t.Fatalf("sessions = %+v", batch.Sessions)
	}
}

func TestPollReturnsOnlyLinesAppendedSinceLastRead(t *testing.T) {
	f := newFixture(t)
	f.write(claudeDir+"a.jsonl", claudePrompt("first", testRepo, stampNow))
	trail := New(f.home, testRepo)
	trail.Start(time.Time{})
	f.write(claudeDir+"a.jsonl", claudePrompt("second", testRepo, stampLater))
	assertSummaries(t, trail.Poll().Events, "second")
}

func TestPartialLineWaitsUntilItIsComplete(t *testing.T) {
	f := newFixture(t)
	f.write(claudeDir+"a.jsonl", claudePrompt("first", testRepo, stampNow))
	trail := New(f.home, testRepo)
	trail.Start(time.Time{})
	line := claudePrompt("second", testRepo, stampLater)
	f.appendRaw(claudeDir+"a.jsonl", line[:20])
	assertSummaries(t, trail.Poll().Events)
	f.appendRaw(claudeDir+"a.jsonl", line[20:]+"\n")
	assertSummaries(t, trail.Poll().Events, "second")
}

func TestSessionStartedAfterLaunchIsFollowedFromItsFirstLine(t *testing.T) {
	f := newFixture(t)
	trail := New(f.home, testRepo)
	trail.Start(time.Time{})
	f.write(codexDir+"rollout-new-00000000-0000-0000-0000-000000000003.jsonl", codexMeta(testRepo))
	assertSummaries(t, trail.Poll().Events, "start 1")
}

func TestOldSessionResumedAfterLaunchShowsOnlyNewLines(t *testing.T) {
	f := newFixture(t)
	path := f.write(claudeDir+"a.jsonl", `{"type":"custom-title","customTitle":"old work"}`, claudePrompt("old", testRepo, "2020-01-01T00:00:00Z"))
	old := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	if err := os.Chtimes(path, old, old); err != nil {
		t.Fatal(err)
	}
	trail := New(f.home, testRepo)
	trail.Start(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	f.write(claudeDir+"a.jsonl", claudePrompt("resumed", testRepo, stampNow))
	batch := trail.Poll()
	assertSummaries(t, batch.Events, "resumed")
	if len(batch.Sessions) != 1 || batch.Sessions[0].Name != "old work" {
		t.Fatalf("sessions = %+v", batch.Sessions)
	}
}

func TestSubagentEventsJoinParentSessionUnderAgentName(t *testing.T) {
	f := newFixture(t)
	f.write(claudeDir+"parent.jsonl", claudePrompt("main", testRepo, stampNow))
	f.write(claudeDir+"parent/subagents/agent-1.meta.json", `{"agentType":"web-fetch"}`)
	f.write(claudeDir+"parent/subagents/agent-1.jsonl", `{"type":"assistant","isSidechain":true,"sessionId":"parent","cwd":"`+testRepo+`","timestamp":"`+stampLater+`","message":{"content":[{"type":"text","text":"fetched"}]}}`)
	batch := New(f.home, testRepo).Start(time.Time{})
	if len(batch.Sessions) != 1 || strings.Join(batch.Sessions[0].Subagents, ",") != "web-fetch" {
		t.Fatalf("sessions = %+v", batch.Sessions)
	}
	if who := batch.Events[1].Who(); who != "claude/web-fetch" {
		t.Fatalf("who = %q", who)
	}
}

func TestSessionNamesComeFromCodexIndexAndCopilotWorkspace(t *testing.T) {
	f := newFixture(t)
	f.write(codexDir+"rollout-x-00000000-0000-0000-0000-000000000004.jsonl", codexMeta(testRepo))
	f.write(".codex/session_index.jsonl", `{"id":"00000000-0000-0000-0000-000000000004","thread_name":"Fix login"}`)
	f.write(".copilot/session-state/c1/workspace.yaml", "cwd: "+testRepo, "name: Create Broken.cs Class", "branch: main")
	f.write(".copilot/session-state/c1/events.jsonl", `{"type":"user.message","timestamp":"`+stampNow+`","data":{"content":"hi"}}`)
	names := map[string]string{}
	for _, session := range New(f.home, testRepo).Start(time.Time{}).Sessions {
		names[string(session.CLI)] = session.Name
	}
	if names["codex"] != "Fix login" || names["copilot"] != "Create Broken.cs Class" {
		t.Fatalf("names = %v", names)
	}
}

func TestDetailReturnsFullTextOfTheSelectedBlock(t *testing.T) {
	f := newFixture(t)
	long := strings.Repeat("x", SummaryLimit*2)
	f.write(claudeDir+"a.jsonl", `{"type":"assistant","sessionId":"s","cwd":"`+testRepo+`","timestamp":"`+stampNow+`","message":{"content":[{"type":"text","text":"short"},{"type":"text","text":"`+long+`"}]}}`)
	events := New(f.home, testRepo).Start(time.Time{}).Events
	detail, err := LoadDetail(events[1])
	if err != nil || detail.Text != long || !strings.Contains(detail.Raw, "\n  ") {
		t.Fatalf("detail = %q, err = %v", detail.Text[:10], err)
	}
}

func TestUnknownFieldTypesSurfaceAsUnparsed(t *testing.T) {
	f := newFixture(t)
	f.write(claudeDir+"a.jsonl", claudePrompt("ok", testRepo, stampNow), `{"type":"user","sessionId":"s","timestamp":"`+stampLater+`","isMeta":"yes"}`)
	events := New(f.home, testRepo).Start(time.Time{}).Events
	if len(events) != 2 || events[1].Kind != KindUnparsed {
		t.Fatalf("events = %+v", events)
	}
}
