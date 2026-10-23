package trail

import (
	"slices"
	"testing"
	"time"

	"github.com/rubentanahara/agent-trail/internal/pricing"
)

const (
	claudeEditResult = `{"type":"user","cwd":"/work/app","timestamp":"` + stampNow + `","message":{"content":[{"type":"tool_result","content":"ok"}]},"toolUseResult":{"filePath":"/work/app/main.go","structuredPatch":[{"oldStart":3,"oldLines":3,"newStart":3,"newLines":4,"lines":[" keep","-old","+new","+extra"]}]}}`
	claudeRead       = `{"type":"user","cwd":"/work/app","timestamp":"` + stampNow + `","message":{"content":[{"type":"tool_result","content":"text"}]},"toolUseResult":{"type":"text","file":{"filePath":"/work/app/main.go","content":"a"}}}`
	claudeCreate     = `{"type":"user","cwd":"/work/app","timestamp":"` + stampNow + `","message":{"content":[{"type":"tool_result","content":"ok"}]},"toolUseResult":{"type":"create","filePath":"/work/app/new.go","content":"one\ntwo\n","structuredPatch":[]}}`
	claudeBashEdit   = `{"type":"user","cwd":"/work/app","timestamp":"` + stampNow + `","message":{"content":[{"type":"tool_result","content":"ok"}]},"toolUseResult":{"stdout":"","bashEditDiff":{"files":[{"filePath":"/work/app/sed.txt","hunks":[{"oldStart":1,"oldLines":1,"newStart":1,"newLines":1,"lines":["-a","+b"]}]}]}}}`
	codexAdd         = `{"timestamp":"` + stampNow + `","type":"event_msg","payload":{"type":"item_completed","item":{"type":"FileChange","changes":{"/work/app/b.txt":{"type":"add","content":"yo\nhi\n"},"/work/app/a.txt":{"type":"update","unified_diff":"@@ -1 +1 @@\n-x\n+y\n"},"/work/app/c.txt":{"type":"update"}}}}}`
	copilotEdit      = `{"type":"tool.execution_complete","timestamp":"` + stampNow + `","data":{"success":true,"fileEdits":[{"path":"/work/app/p.md","kind":"edit"}],"result":{"content":"updated","detailedContent":"\ndiff --git a/work/app/p.md b/work/app/p.md\nindex 0000000..0000000 100644\n--- a/work/app/p.md\n+++ b/work/app/p.md\n@@ -5,2 +5,2 @@\n ctx\n-gone\n+here\n"}}}`
)

func eventsOf(t *testing.T, path string, lines ...string) []Event {
	t.Helper()
	f := newFixture(t)
	f.write(path, lines...)
	return New(f.home, testRepo, pricing.Table{}).Start(time.Time{}).Events
}

func TestClaudeEditResultRecordsCountsAndLoadPatchReturnsTheHunk(t *testing.T) {
	events := eventsOf(t, claudeDir+"a.jsonl", claudeEditResult)
	want := []FileChange{{Path: "/work/app/main.go", Added: 2, Removed: 1, Counted: true}}
	if len(events) != 1 || !slices.Equal(events[0].Changes, want) {
		t.Fatalf("events = %+v", events)
	}
	patches, err := LoadPatch(events[0])
	wantLines := []string{"@@ -3,3 +3,4 @@", " keep", "-old", "+new", "+extra"}
	if err != nil || len(patches) != 1 || !slices.Equal(patches[0].Lines, wantLines) {
		t.Fatalf("patches = %+v, err = %v", patches, err)
	}
}

func TestClaudeCreateCountsTheWholeContentAndBashEditsCount(t *testing.T) {
	created := eventsOf(t, claudeDir+"a.jsonl", claudeCreate)
	if got := created[0].Changes; len(got) != 1 || got[0].Added != 2 || got[0].Removed != 0 {
		t.Fatalf("create = %+v", got)
	}
	bash := eventsOf(t, claudeDir+"a.jsonl", claudeBashEdit)
	if got := bash[0].Changes; len(got) != 1 || got[0].Path != "/work/app/sed.txt" || got[0].Added != 1 || got[0].Removed != 1 {
		t.Fatalf("bash = %+v", got)
	}
}

func TestReadResultHasNoChanges(t *testing.T) {
	events := eventsOf(t, claudeDir+"a.jsonl", claudeRead)
	if len(events) != 1 || events[0].Changes != nil {
		t.Fatalf("events = %+v", events)
	}
}

func TestCodexFileChangeRecordsCountsAndMarksMissingDiffsUncounted(t *testing.T) {
	events := eventsOf(t, codexDir+"rollout-2026-10-22T10-00-00-00000000-0000-0000-0000-000000000001.jsonl", codexMeta(testRepo), codexAdd)
	var edit Event
	for _, event := range events {
		if event.Kind == KindEdit {
			edit = event
		}
	}
	want := []FileChange{
		{Path: "/work/app/a.txt", Added: 1, Removed: 1, Counted: true},
		{Path: "/work/app/b.txt", Added: 2, Counted: true},
		{Path: "/work/app/c.txt"},
	}
	if !slices.Equal(edit.Changes, want) {
		t.Fatalf("changes = %+v", edit.Changes)
	}
	patches, err := LoadPatch(edit)
	if err != nil || len(patches) != 3 || patches[0].Lines[1] != "-x" {
		t.Fatalf("patches = %+v, err = %v", patches, err)
	}
}

func TestCopilotEditResultRecordsCountsFromItsDiff(t *testing.T) {
	f := newFixture(t)
	f.write(".copilot/session-state/s1/workspace.yaml", "cwd: "+testRepo)
	f.write(".copilot/session-state/s1/events.jsonl", copilotEdit)
	events := New(f.home, testRepo, pricing.Table{}).Start(time.Time{}).Events
	want := []FileChange{{Path: "/work/app/p.md", Added: 1, Removed: 1, Counted: true}}
	if len(events) != 1 || !slices.Equal(events[0].Changes, want) {
		t.Fatalf("events = %+v", events)
	}
}
