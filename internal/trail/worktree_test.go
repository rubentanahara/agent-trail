package trail

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/rubentanahara/agent-trail/internal/pricing"
)

type checkoutLayout struct {
	t    *testing.T
	main string
}

func newCheckoutLayout(t *testing.T) checkoutLayout {
	t.Helper()
	main := filepath.Join(t.TempDir(), "main")
	layout := checkoutLayout{t: t, main: main}
	layout.put(filepath.Join(main, ".git", "HEAD"), "ref: refs/heads/main\n")
	return layout
}

func (l checkoutLayout) put(path, content string) {
	l.t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		l.t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		l.t.Fatal(err)
	}
}

func (l checkoutLayout) addWorktree(name, branch string) string {
	l.t.Helper()
	root := filepath.Join(l.t.TempDir(), name)
	admin := filepath.Join(l.main, ".git", "worktrees", name)
	l.put(filepath.Join(root, ".git"), "gitdir: "+admin+"\n")
	l.put(filepath.Join(admin, "gitdir"), filepath.Join(root, ".git")+"\n")
	l.put(filepath.Join(admin, "HEAD"), "ref: refs/heads/"+branch+"\n")
	return root
}

func claudeLineOnBranch(text, cwd, branch string) string {
	return `{"type":"user","sessionId":"s","cwd":"` + cwd + `","gitBranch":"` + branch + `","timestamp":"` + stampNow + `","message":{"content":"` + text + `"}}`
}

func (f fixture) writeClaude(name, cwd string, lines ...string) {
	f.write(".claude/projects/"+claudeProjectPrefix(cwd)+"/"+name+".jsonl", lines...)
}

func sessionNamed(t *testing.T, batch Batch, id string) Session {
	t.Helper()
	for _, session := range batch.Sessions {
		if session.ID == id {
			return session
		}
	}
	t.Fatalf("no session %q in %+v", id, batch.Sessions)
	return Session{}
}

func TestSessionInOutsideWorktreeIsFollowedAndSiblingIsNot(t *testing.T) {
	layout := newCheckoutLayout(t)
	worktree := layout.addWorktree("feature", "feature")
	f := newFixture(t)
	f.writeClaude("in", worktree, claudePrompt("in worktree", worktree, stampNow))
	f.writeClaude("out", layout.main+"-sibling", claudePrompt("in sibling", layout.main+"-sibling", stampNow))
	assertSummaries(t, New(f.home, layout.main, pricing.Table{}).Start(time.Time{}).Events, "in worktree")
}

func TestBranchComesFromTheCheckoutHeadNotTheRecordedLines(t *testing.T) {
	layout := newCheckoutLayout(t)
	f := newFixture(t)
	f.writeClaude("s", layout.main, claudeLineOnBranch("a", layout.main, "worktree-agent-1"), claudeLineOnBranch("b", layout.main, "worktree-agent-2"))
	batch := New(f.home, layout.main, pricing.Table{}).Start(time.Time{})
	if got := sessionNamed(t, batch, "s").Branch; got != "main" {
		t.Fatalf("branch = %q, want main", got)
	}
}

func TestBranchFallsBackToTheRecordedOneWhenTheFolderIsGone(t *testing.T) {
	gone := filepath.Join(t.TempDir(), "gone")
	f := newFixture(t)
	f.writeClaude("s", gone, claudeLineOnBranch("a", gone, "recorded"))
	batch := New(f.home, EveryRepository, pricing.Table{}).Start(time.Time{})
	if got := sessionNamed(t, batch, "s").Branch; got != "recorded" {
		t.Fatalf("branch = %q, want recorded", got)
	}
}

func TestWorktreeAddedAfterStartIsPickedUpOnPoll(t *testing.T) {
	layout := newCheckoutLayout(t)
	f := newFixture(t)
	trail := New(f.home, layout.main, pricing.Table{})
	trail.Start(time.Time{})
	worktree := filepath.Join(t.TempDir(), "late")
	f.writeClaude("late", worktree, claudePrompt("before", worktree, stampNow))
	assertSummaries(t, trail.Poll().Events)
	admin := filepath.Join(layout.main, ".git", "worktrees", "late")
	layout.put(filepath.Join(worktree, ".git"), "gitdir: "+admin+"\n")
	layout.put(filepath.Join(admin, "gitdir"), filepath.Join(worktree, ".git")+"\n")
	layout.put(filepath.Join(admin, "HEAD"), "ref: refs/heads/late\n")
	assertSummaries(t, trail.Poll().Events, "before")
}
