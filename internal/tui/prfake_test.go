package tui

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/rubentanahara/agent-trail/internal/gh"
	"github.com/rubentanahara/agent-trail/internal/trail"
)

const (
	prViewer    = "dependabot"
	prForkPR    = 14783
	prBranchPR  = 14716
	prBranch    = "dependabot/github_actions/actions/create-github-app-token-3.3.0"
	prTestEpoch = "2026-10-23T10:00:00Z"
)

type fakeGH struct {
	t     *testing.T
	mu    sync.Mutex
	calls []string
	reply map[string]fakeReply
}

type fakeReply struct {
	out []byte
	err error
}

func testdata(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile("../gh/testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func viewJSON(t *testing.T, number int) []byte {
	t.Helper()
	var view map[string]any
	if err := json.Unmarshal(testdata(t, "view.json"), &view); err != nil {
		t.Fatal(err)
	}
	var listed []map[string]any
	if err := json.Unmarshal(testdata(t, "list.json"), &listed); err != nil {
		t.Fatal(err)
	}
	for _, entry := range listed {
		if int(entry["number"].(float64)) == number {
			for _, field := range []string{"number", "headRefName", "isCrossRepository", "updatedAt", "headRefOid", "title", "author"} {
				view[field] = entry[field]
			}
		}
	}
	data, err := json.Marshal(view)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func newFakeGH(t *testing.T) *fakeGH {
	f := &fakeGH{t: t, reply: map[string]fakeReply{}}
	f.set(gh.RepoArgs(), testdata(t, "repo.json"))
	f.set(gh.ViewerArgs(), []byte(prViewer+"\n"))
	f.set(gh.ListArgs(), testdata(t, "list.json"))
	f.set(gh.ReviewRequestedArgs(), testdata(t, "requested.json"))
	for _, number := range []int{prForkPR, prBranchPR} {
		f.set(gh.ViewArgs(number), viewJSON(t, number))
		f.set(gh.DiffArgs(number), testdata(t, "diff.patch"))
		f.set(gh.ThreadsArgs("rubentanahara", "agent-trail", number), testdata(t, "threads.json"))
	}
	return f
}

func (f *fakeGH) set(args []string, out []byte) {
	f.reply[strings.Join(args, " ")] = fakeReply{out: out}
}

func (f *fakeGH) fail(args []string, err error) {
	f.reply[strings.Join(args, " ")] = fakeReply{err: err}
}

func (f *fakeGH) run(_ context.Context, args []string) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	key := strings.Join(args, " ")
	f.calls = append(f.calls, key)
	reply, known := f.reply[key]
	if !known {
		f.t.Errorf("unexpected gh call: %s", key)
		return nil, &gh.Error{Args: args, Err: context.Canceled}
	}
	return reply.out, reply.err
}

func (f *fakeGH) count(prefix string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	total := 0
	for _, call := range f.calls {
		if strings.HasPrefix(call, prefix) {
			total++
		}
	}
	return total
}

func prModel(t *testing.T, fake *fakeGH) model {
	t.Helper()
	m := sized(newModel(nil, sampleBatch(), Context{Repo: "/repo", Version: "v0"}), 140, 40)
	m.pr.run = fake.run
	m.pr.open = func(string) error { return nil }
	return m
}

func settlePR(m model, cmd tea.Cmd) model {
	if cmd == nil {
		return m
	}
	switch msg := cmd().(type) {
	case tea.BatchMsg:
		for _, inner := range msg {
			m = settlePR(m, inner)
		}
	case prMessage:
		updated, next := m.Update(msg)
		m = settlePR(updated.(model), next)
	}
	return m
}

func pressPR(m model, keys ...string) model {
	for _, key := range keys {
		var msg tea.KeyPressMsg
		switch key {
		case "enter":
			msg = tea.KeyPressMsg{Code: tea.KeyEnter}
		case "esc":
			msg = tea.KeyPressMsg{Code: tea.KeyEscape}
		case "tab":
			msg = tea.KeyPressMsg{Code: tea.KeyTab}
		case "down":
			msg = tea.KeyPressMsg{Code: tea.KeyDown}
		case "up":
			msg = tea.KeyPressMsg{Code: tea.KeyUp}
		default:
			msg = tea.KeyPressMsg{Code: []rune(key)[0], Text: key}
		}
		updated, cmd := m.Update(msg)
		m = settlePR(updated.(model), cmd)
	}
	return m
}

func openedPRs(t *testing.T, fake *fakeGH) model {
	t.Helper()
	return pressPR(prModel(t, fake), "p")
}

func screenText(m model) string {
	return ansi.Strip(m.render())
}

func clockAt(t *testing.T, offset time.Duration) time.Time {
	t.Helper()
	base, err := time.Parse(time.RFC3339, prTestEpoch)
	if err != nil {
		t.Fatal(err)
	}
	return base.Add(offset)
}

func sessionOnBranches(key string, last time.Time, branches ...string) trail.Session {
	return trail.Session{Key: key, ID: key, CLI: trail.Claude, Name: key, Events: 2, Last: last, Branches: branches, Branch: branches[len(branches)-1]}
}

func settleBatch(m model, sessions ...trail.Session) model {
	m.absorb(trail.Batch{Sessions: sessions})
	return m
}

var errNotInstalled = &execNotFound{}

type execNotFound struct{}

func (*execNotFound) Error() string { return "executable file not found in $PATH" }
func (*execNotFound) Is(target error) bool {
	return target == exec.ErrNotFound
}

func countMessages(cmd tea.Cmd) int {
	if cmd == nil {
		return 0
	}
	switch msg := cmd().(type) {
	case tea.BatchMsg:
		total := 0
		for _, inner := range msg {
			total += countMessages(inner)
		}
		return total
	case prMessage:
		_ = msg
		return 1
	}
	return 0
}
