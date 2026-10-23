package tui

import (
	"reflect"
	"strings"
	"testing"

	"github.com/rubentanahara/agent-trail/internal/trail"
)

func TestParseProcessTable(t *testing.T) {
	got := parseProcessTable("    1     0\n71172 26608\n  bad line\n26608     1\n")
	want := map[int]int{1: 0, 71172: 26608, 26608: 1}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("table = %v", got)
	}
}

func TestParsePanes(t *testing.T) {
	got := parsePanes("26608 %32\n27000 %33\nnonsense\n")
	want := map[int]string{26608: "%32", 27000: "%33"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("panes = %v", got)
	}
}

func TestFindPaneWalksAncestors(t *testing.T) {
	panes := map[int]string{100: "%1"}
	cases := []struct {
		name    string
		parents map[int]int
		pid     int
		pane    string
		found   bool
	}{
		{"direct pane", map[int]int{100: 1}, 100, "%1", true},
		{"grandchild", map[int]int{300: 200, 200: 100, 100: 1}, 300, "%1", true},
		{"no pane", map[int]int{300: 200, 200: 1}, 300, "", false},
		{"cycle", map[int]int{300: 200, 200: 300}, 300, "", false},
	}
	for _, tc := range cases {
		pane, found := findPane(tc.parents, panes, tc.pid)
		if pane != tc.pane || found != tc.found {
			t.Errorf("%s: pane = %q found = %v", tc.name, pane, found)
		}
	}
}

func TestContinueOnARunningSessionOutsideTmuxSaysWhereItRuns(t *testing.T) {
	t.Setenv("TMUX", "")
	batch := sampleBatch()
	batch.Sessions[1].Liveness, batch.Sessions[1].PID = trail.LivenessRunning, 42
	m := press(sized(newModel(nil, batch, Context{Repo: "/repo"}), 160, 40), "1")
	if cmd := m.resume(); cmd != nil || !strings.Contains(m.notice, "(pid 42); c can jump there only inside tmux") {
		t.Fatalf("notice = %q", m.notice)
	}
}

func TestFinishJumpReportsTheOutcome(t *testing.T) {
	m := model{}
	m.finishJump(jumpedMsg{label: "login"})
	if m.notice != "jumped to login" {
		t.Fatalf("notice = %q", m.notice)
	}
	m.finishJump(jumpedMsg{label: "login", pid: 42, err: errNoPane})
	if m.notice != "login is open in pid 42 outside tmux" {
		t.Fatalf("notice = %q", m.notice)
	}
}
