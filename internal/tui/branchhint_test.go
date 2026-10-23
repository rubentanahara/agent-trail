package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/rubentanahara/agent-trail/internal/trail"
)

func onBranch(name, branch string, last time.Time) trail.Session {
	session := runningClaude(name, "busy", last)
	session.Branch = branch
	return session
}

func TestBranchHintAppearsOnlyWhenSessionsSpanSeveralBranches(t *testing.T) {
	now := time.Now()
	same := ansi.Strip(listModel(140, onBranch("alpha", "main", now), onBranch("beta", "main", now.Add(-time.Second))).render())
	if strings.Contains(same, "· main") {
		t.Fatalf("hint shown for a single branch:\n%s", same)
	}
	mixed := ansi.Strip(listModel(140, onBranch("alpha", "main", now), onBranch("beta", "feature-x", now.Add(-time.Second))).render())
	if row := rowContaining(mixed, "alpha"); !strings.Contains(row, "· main") {
		t.Fatalf("alpha row = %q", row)
	}
	if row := rowContaining(mixed, "beta"); !strings.Contains(row, "· feature-x") {
		t.Fatalf("beta row = %q", row)
	}
}
