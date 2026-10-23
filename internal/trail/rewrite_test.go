package trail

import (
	"os"
	"slices"
	"testing"
	"time"

	"github.com/rubentanahara/agent-trail/internal/pricing"
)

func TestRewrittenTranscriptResetsSessionTotals(t *testing.T) {
	f := newFixture(t)
	usage := `{"input_tokens":10,"output_tokens":5}`
	path := f.write(claudeDir+"a.jsonl",
		claudePrompt("first old prompt", testRepo, stampNow),
		claudePrompt("second old prompt", testRepo, stampNow),
		assistantLine("msg_1", usage),
		claudePrompt("third old prompt", testRepo, stampNow),
	)
	trail := New(f.home, testRepo, pricing.Table{})
	trail.Start(time.Time{})

	rewritten := claudePrompt("fresh", testRepo, stampLater) + "\n" + assistantLine("msg_9", `{"input_tokens":1,"output_tokens":1}`) + "\n"
	if err := os.WriteFile(path, []byte(rewritten), 0o644); err != nil {
		t.Fatal(err)
	}
	batch := trail.Poll()

	if want := []string{"claude:a"}; !slices.Equal(batch.Reset, want) {
		t.Fatalf("Reset = %v, want %v", batch.Reset, want)
	}
	for _, event := range batch.Events {
		if event.Summary == "first old prompt" || event.Summary == "third old prompt" {
			t.Fatalf("batch carries event from the old file: %q", event.Summary)
		}
	}
	if len(batch.Sessions) != 1 {
		t.Fatalf("sessions = %d, want 1", len(batch.Sessions))
	}
	session := batch.Sessions[0]
	if session.Events != len(batch.Events) {
		t.Fatalf("Events = %d, want %d", session.Events, len(batch.Events))
	}
	if session.Calls != 1 || session.Tokens.Input != 1 || session.Tokens.Output != 1 {
		t.Fatalf("Calls=%d Tokens=%+v, want the new file's totals only", session.Calls, session.Tokens)
	}
}
