package trail

import (
	"strings"
	"testing"
	"time"

	"github.com/rubentanahara/agent-trail/internal/pricing"
)

func TestSummaryDropsCdIntoTheSessionFolder(t *testing.T) {
	cases := []struct {
		name, text, cwd, want string
	}{
		{"claude plain", "Bash command: cd /work/app && make test description: run", "/work/app", "Bash command: make test description: run"},
		{"claude double quoted", `Bash command: cd "/work/app" && make`, "/work/app", "Bash command: make"},
		{"claude single quoted", "Bash command: cd '/work/app' && make", "/work/app", "Bash command: make"},
		{"codex", "exec cd /work/app && ls", "/work/app", "exec ls"},
		{"other folder kept", "Bash command: cd /work/other && make", "/work/app", "Bash command: cd /work/other && make"},
		{"empty cwd", "Bash command: cd /work/app && make", "", "Bash command: cd /work/app && make"},
		{"cd without and", "Bash command: cd /work/app", "/work/app", "Bash command: cd /work/app"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := summaryText(c.text, c.cwd); got != c.want {
				t.Fatalf("got %q, want %q", got, c.want)
			}
		})
	}
}

func TestStoreSummaryLosesCdWhileDetailKeepsTheCommand(t *testing.T) {
	f := newFixture(t)
	f.write(claudeDir+"a.jsonl", `{"type":"assistant","sessionId":"s","cwd":"`+testRepo+`","timestamp":"`+stampNow+`","message":{"content":[{"type":"tool_use","name":"Bash","input":{"command":"cd `+testRepo+` && make test"}}]}}`)
	events := New(f.home, testRepo, pricing.Table{}).Start(time.Time{}).Events
	if len(events) != 1 || strings.Contains(events[0].Summary, "cd ") {
		t.Fatalf("events = %+v", events)
	}
	detail, err := LoadDetail(events[0])
	if err != nil || !strings.Contains(detail.Text, "cd "+testRepo+" && make test") {
		t.Fatalf("detail = %q, err = %v", detail.Text, err)
	}
}
