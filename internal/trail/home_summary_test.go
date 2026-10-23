package trail

import (
	"testing"

	"github.com/rubentanahara/agent-trail/internal/pricing"
)

func TestSummaryDropsCdIntoTheSessionFolderWrittenFromHome(t *testing.T) {
	follower := New("/Users/me", EveryRepository, pricing.Table{})
	cases := []struct {
		name, text, cwd, want string
	}{
		{"tilde", "Bash command: cd ~/src/app && make", "/Users/me/src/app", "Bash command: make"},
		{"quoted tilde", `Bash command: cd "~/src/app" && make`, "/Users/me/src/app", "Bash command: make"},
		{"absolute still dropped", "Bash command: cd /Users/me/src/app && make", "/Users/me/src/app", "Bash command: make"},
		{"other folder under home kept", "Bash command: cd ~/src/other && make", "/Users/me/src/app", "Bash command: cd ~/src/other && make"},
		{"folder outside home", "Bash command: cd ~/work/app && make", "/work/app", "Bash command: cd ~/work/app && make"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := follower.commandSummary(c.text, c.cwd); got != c.want {
				t.Fatalf("got %q, want %q", got, c.want)
			}
		})
	}
}
