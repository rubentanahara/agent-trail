package trail

import (
	"strings"
	"testing"
	"time"

	"github.com/rubentanahara/agent-trail/internal/pricing"
)

func claudeLine(kind, stamp, content string) string {
	return `{"type":"` + kind + `","sessionId":"s","cwd":"` + testRepo + `","timestamp":"` + stamp + `","message":{"content":` + content + `}}`
}

func conversationEvents(t *testing.T, lines ...string) []Event {
	f := newFixture(t)
	f.write(claudeDir+"a.jsonl", lines...)
	return New(f.home, testRepo, pricing.Table{}).Start(time.Time{}).Events
}

func TestConversationKeepsPromptsAndRepliesInFullAndToolCallsAsOneLine(t *testing.T) {
	longReply := strings.Repeat("reply ", SummaryLimit)
	events := conversationEvents(t,
		claudeLine("user", stampNow, `"fix the login"`),
		claudeLine("assistant", stampNow, `[{"type":"thinking","thinking":"secret musing"},{"type":"text","text":"`+longReply+`"},{"type":"tool_use","name":"Bash","input":{"command":"make\ncheck"}}]`),
		claudeLine("user", stampLater, `[{"type":"tool_result","content":[{"type":"text","text":"HUGE OUTPUT"}]}]`),
	)
	got := Conversation(events, 1<<20)
	if got.Cut {
		t.Fatal("conversation was cut")
	}
	for _, want := range []string{"fix the login", longReply, "Bash"} {
		if !strings.Contains(got.Text, want) {
			t.Errorf("conversation lacks %q:\n%s", want, got.Text)
		}
	}
	for _, banned := range []string{"secret musing", "HUGE OUTPUT"} {
		if strings.Contains(got.Text, banned) {
			t.Errorf("conversation has %q", banned)
		}
	}
	if strings.Contains(got.Text, "make\ncheck") {
		t.Error("tool call spans several lines")
	}
}

func TestConversationCapKeepsTheMostRecentPartAndReportsTheCut(t *testing.T) {
	var lines []string
	for _, word := range []string{"first", "second", "third"} {
		lines = append(lines, claudeLine("user", stampNow, `"`+word+strings.Repeat("x", 100)+`"`))
	}
	got := Conversation(conversationEvents(t, lines...), 250)
	if !got.Cut || !strings.Contains(got.Text, "third") || strings.Contains(got.Text, "first") {
		t.Fatalf("cut = %v, text:\n%s", got.Cut, got.Text)
	}
	if !strings.HasPrefix(got.Text, CutNotice) {
		t.Fatalf("text does not start with the notice:\n%s", got.Text)
	}
}

func TestConversationOfThousandsOfEventsIsBuiltQuickly(t *testing.T) {
	var lines []string
	for i := 0; i < 2500; i++ {
		lines = append(lines, claudeLine("user", stampNow, `"question `+strings.Repeat("q", 200)+`"`), claudeLine("assistant", stampLater, `[{"type":"text","text":"answer `+strings.Repeat("a", 400)+`"}]`))
	}
	events := conversationEvents(t, lines...)
	started := time.Now()
	Conversation(events, 400_000)
	if elapsed := time.Since(started); elapsed > 300*time.Millisecond {
		t.Fatalf("took %s for %d events", elapsed, len(events))
	}
}
