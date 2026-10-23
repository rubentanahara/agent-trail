package trail

import (
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/rubentanahara/agent-trail/internal/pricing"
)

func codexMetaOnBranch(cwd, branch string) string {
	return `{"timestamp":"` + stampNow + `","type":"session_meta","payload":{"cwd":"` + cwd + `","git":{"branch":"` + branch + `"}}}`
}

func sessionOf(t *testing.T, batch Batch, cli CLI) Session {
	t.Helper()
	for _, session := range batch.Sessions {
		if session.CLI == cli {
			return session
		}
	}
	t.Fatalf("no %s session in %+v", cli, batch.Sessions)
	return Session{}
}

func (l checkoutLayout) checkout(branch string) {
	l.t.Helper()
	l.put(filepath.Join(l.main, ".git", "HEAD"), "ref: refs/heads/"+branch+"\n")
}

func TestClaudeSessionThatSwitchedBranchKeepsBoth(t *testing.T) {
	gone := filepath.Join(t.TempDir(), "gone")
	f := newFixture(t)
	f.writeClaude("s", gone, claudeLineOnBranch("a", gone, "feat/one"), claudeLineOnBranch("b", gone, "feat/two"))
	batch := New(f.home, EveryRepository, pricing.Table{}).Start(time.Time{})
	if got := sessionOf(t, batch, Claude).Branches; !slices.Equal(got, []string{"feat/one", "feat/two"}) {
		t.Fatalf("Branches = %v", got)
	}
}

func TestCodexSessionRecordsItsStartBranch(t *testing.T) {
	gone := filepath.Join(t.TempDir(), "gone")
	f := newFixture(t)
	f.write(codexDir+"rollout-c-00000000-0000-0000-0000-000000000009.jsonl", codexMetaOnBranch(gone, "feat/codex"))
	batch := New(f.home, EveryRepository, pricing.Table{}).Start(time.Time{})
	if got := sessionOf(t, batch, Codex).Branches; !slices.Equal(got, []string{"feat/codex"}) {
		t.Fatalf("Branches = %v", got)
	}
}

func TestSampledCheckoutSwitchIsAddedAndKept(t *testing.T) {
	layout := newCheckoutLayout(t)
	f := newFixture(t)
	f.write(codexDir+"rollout-c-00000000-0000-0000-0000-000000000009.jsonl", codexMetaOnBranch(layout.main, "main"))
	tr := New(f.home, layout.main, pricing.Table{})
	tr.Start(time.Time{})
	layout.checkout("feat/late")
	session := sessionOf(t, tr.Poll(), Codex)
	if !slices.Equal(session.Branches, []string{"main", "feat/late"}) || session.Branch != "feat/late" {
		t.Fatalf("Branch = %q, Branches = %v", session.Branch, session.Branches)
	}
	layout.checkout("main")
	if got := sessionOf(t, tr.Poll(), Codex).Branches; !slices.Equal(got, []string{"main", "feat/late"}) {
		t.Fatalf("Branches after switching back = %v", got)
	}
}

func TestClosedSessionStopsSamplingTheCheckout(t *testing.T) {
	layout := newCheckoutLayout(t)
	f := newFixture(t)
	f.writeClaude("s", layout.main, claudeLineOnBranch("a", layout.main, "feat/recorded"))
	tr := New(f.home, layout.main, pricing.Table{})
	tr.Start(time.Time{})
	layout.checkout("feat/elsewhere")
	f.appendRaw(".claude/projects/"+claudeProjectPrefix(layout.main)+"/s.jsonl", claudeLineOnBranch("b", layout.main, "feat/recorded")+"\n")
	if got := sessionOf(t, tr.Poll(), Claude).Branches; slices.Contains(got, "feat/elsewhere") || slices.Contains(got, "main") {
		t.Fatalf("closed session sampled the checkout: %v", got)
	}
}
