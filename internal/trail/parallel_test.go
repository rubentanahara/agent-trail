package trail

import (
	"fmt"
	"reflect"
	"sort"
	"testing"
	"time"

	"github.com/rubentanahara/agent-trail/internal/pricing"
)

const (
	parallelTranscripts = 40
	sharedStampCount    = 3
)

func sharedStamp(index int) string {
	return fmt.Sprintf("2026-10-22T10:00:%02dZ", index%sharedStampCount)
}

func writeMixedTranscripts(f fixture) {
	for index := range parallelTranscripts {
		text := fmt.Sprintf("prompt %d", index)
		switch index % 3 {
		case 0:
			f.write(fmt.Sprintf("%sa%d.jsonl", claudeDir, index), claudePrompt(text, testRepo, sharedStamp(index)), claudePrompt(text+" again", testRepo, sharedStamp(index+1)))
		case 1:
			f.write(fmt.Sprintf("%srollout-c%d-00000000-0000-0000-0000-%012d.jsonl", codexDir, index, index), codexMeta(testRepo))
		default:
			dir := fmt.Sprintf(".copilot/session-state/p%d/", index)
			f.write(dir+"workspace.yaml", "cwd: "+testRepo, "git_root: "+testRepo)
			f.write(dir+"events.jsonl", `{"type":"user.message","timestamp":"`+sharedStamp(index)+`","data":{"content":"`+text+`"}}`)
		}
	}
}

func serialStart(t *Trail, cutoff time.Time) Batch {
	t.cutoff = cutoff
	var events []Event
	for _, path := range t.discover() {
		events = append(events, t.read(path, t.transcripts[path])...)
	}
	return t.finish(events)
}

func sortedByKey(sessions []Session) []Session {
	sort.Slice(sessions, func(i, j int) bool { return sessions[i].Key < sessions[j].Key })
	return sessions
}

func TestStartMatchesAFileByFileSerialReadOfTheSameTranscripts(t *testing.T) {
	f := newFixture(t)
	writeMixedTranscripts(f)

	want := serialStart(New(f.home, testRepo, pricing.Table{}), time.Time{})
	got := New(f.home, testRepo, pricing.Table{}).Start(time.Time{})

	if len(got.Events) <= parallelTranscripts {
		t.Fatalf("only %d events from %d transcripts", len(got.Events), parallelTranscripts)
	}
	if !reflect.DeepEqual(got.Events, want.Events) {
		t.Fatalf("events differ:\n got %q\nwant %q", summaries(got.Events), summaries(want.Events))
	}
	if !reflect.DeepEqual(sortedByKey(got.Sessions), sortedByKey(want.Sessions)) {
		t.Fatalf("sessions differ:\n got %+v\nwant %+v", got.Sessions, want.Sessions)
	}
}
