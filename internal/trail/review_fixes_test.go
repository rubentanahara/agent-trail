package trail

import (
	"os"
	"slices"
	"testing"
	"time"

	"github.com/rubentanahara/agent-trail/internal/pricing"
)

func TestClaudeContextIsHiddenWhenAPromptExceedsTheListedLimit(t *testing.T) {
	session := claudeSessionWith(t, limitedModelPrices, "100000", "1500000")
	if session.HasContext {
		t.Fatalf("context = %v, want hidden: the listed limit is not this session's window", session.ContextPercent)
	}
}

func TestRewritingOneTranscriptRereadsEveryTranscriptOfItsSession(t *testing.T) {
	f := newFixture(t)
	rewrittenPath := f.write(".claude/projects/-work-app/s.jsonl", claudePrompt("first old", testRepo, stampNow), claudePrompt("second old", testRepo, stampNow))
	f.write(".claude/projects/-work-app-src/s.jsonl", claudePrompt("kept sibling", testRepo+"/src", stampNow))
	follower := New(f.home, testRepo, pricing.Table{})
	follower.Start(time.Time{})
	if err := os.WriteFile(rewrittenPath, []byte(claudePrompt("fresh", testRepo, stampLater)+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	batch := follower.Poll()
	if !slices.Equal(batch.Reset, []string{"claude:s"}) {
		t.Fatalf("Reset = %v", batch.Reset)
	}
	assertSummaries(t, batch.Events, "kept sibling", "fresh")
	if session := batch.Sessions[0]; session.Events != 2 {
		t.Fatalf("Events = %d, want 2", session.Events)
	}
}
