package tui

import (
	"cmp"
	"os"
	"os/exec"

	tea "charm.land/bubbletea/v2"

	"github.com/rubentanahara/agent-trail/internal/trail"
)

type resumedMsg struct {
	label string
	err   error
}

func (m *model) resume() tea.Cmd {
	session, ok := m.resumeTarget()
	switch {
	case !ok:
		m.notice = "select a session to open"
		return nil
	case session.Liveness == trail.LivenessRunning && session.PID > 0:
		return m.jumpToRunning(session)
	case session.Liveness == trail.LivenessRunning:
		m.notice = sessionLabel(session) + " is already open"
		return nil
	case session.TurnOpen:
		m.notice = sessionLabel(session) + " is still working; wait for its turn to end"
		return nil
	}
	args := trail.ResumeCommand(session)
	command := exec.Command(args[0], args[1:]...)
	command.Dir = m.workingDir(session)
	label := sessionLabel(session)
	return tea.ExecProcess(command, func(err error) tea.Msg { return resumedMsg{label: label, err: err} })
}

func (m model) resumeTarget() (trail.Session, bool) {
	key := m.activeSession
	if key == "" {
		if index := m.selectedEvent(); index != noEvent {
			key = m.events[index].Session
		}
	}
	session, ok := m.sessions[key]
	if parent, isAgent := m.sessions[session.Parent]; isAgent {
		return parent, true
	}
	return session, ok
}

func (m model) workingDir(session trail.Session) string {
	if info, err := os.Stat(session.Cwd); err == nil && info.IsDir() {
		return session.Cwd
	}
	return cmp.Or(m.context.Repo, ".")
}

func (m *model) finishResume(msg resumedMsg) {
	if msg.err != nil {
		m.notice = "could not open " + msg.label + ": " + msg.err.Error()
		return
	}
	m.notice = "back from " + msg.label
}
