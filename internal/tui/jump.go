package tui

import (
	"errors"
	"os"
	"os/exec"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/rubentanahara/agent-trail/internal/trail"
)

const maxAncestorDepth = 64

var errNoPane = errors.New("no tmux pane found")

type jumpedMsg struct {
	label string
	pid   int
	err   error
}

func (m *model) jumpToRunning(session trail.Session) tea.Cmd {
	label := sessionLabel(session)
	if os.Getenv("TMUX") == "" {
		m.notice = label + " is open in another terminal (pid " + pidText(session.PID) + "); c can jump there only inside tmux"
		return nil
	}
	pid := session.PID
	return func() tea.Msg { return jumpedMsg{label: label, pid: pid, err: switchToPaneOf(pid)} }
}

func (m *model) finishJump(msg jumpedMsg) {
	if errors.Is(msg.err, errNoPane) {
		m.notice = msg.label + " is open in pid " + pidText(msg.pid) + " outside tmux"
		return
	}
	if msg.err != nil {
		m.notice = "could not jump to " + msg.label + ": " + msg.err.Error()
		return
	}
	m.notice = "jumped to " + msg.label
}

func switchToPaneOf(pid int) error {
	table, err := exec.Command("ps", "-A", "-o", "pid=,ppid=").Output()
	if err != nil {
		return err
	}
	listing, err := exec.Command("tmux", "list-panes", "-a", "-F", "#{pane_pid} #{pane_id}").Output()
	if err != nil {
		return err
	}
	pane, found := findPane(parseProcessTable(string(table)), parsePanes(string(listing)), pid)
	if !found {
		return errNoPane
	}
	return exec.Command("tmux", "switch-client", "-t", pane).Run()
}

func parseProcessTable(output string) map[int]int {
	parents := map[int]int{}
	for _, line := range strings.Split(output, "\n") {
		fields := strings.Fields(line)
		if len(fields) != 2 {
			continue
		}
		pid, pidErr := strconv.Atoi(fields[0])
		parent, parentErr := strconv.Atoi(fields[1])
		if pidErr == nil && parentErr == nil {
			parents[pid] = parent
		}
	}
	return parents
}

func parsePanes(output string) map[int]string {
	panes := map[int]string{}
	for _, line := range strings.Split(output, "\n") {
		fields := strings.Fields(line)
		if len(fields) != 2 {
			continue
		}
		if pid, err := strconv.Atoi(fields[0]); err == nil {
			panes[pid] = fields[1]
		}
	}
	return panes
}

func findPane(parents map[int]int, panes map[int]string, pid int) (string, bool) {
	seen := map[int]bool{}
	for depth := 0; depth < maxAncestorDepth && pid > 1 && !seen[pid]; depth++ {
		if pane, ok := panes[pid]; ok {
			return pane, true
		}
		seen[pid] = true
		pid = parents[pid]
	}
	return "", false
}
