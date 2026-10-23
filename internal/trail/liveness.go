package trail

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

type claudeProcess struct {
	PID       int    `json:"pid"`
	SessionID string `json:"sessionId"`
	Status    string `json:"status"`
}

func (t *Trail) refreshLiveness() {
	running := t.runningClaudeSessions()
	for key, session := range t.sessions {
		before := session.liveState()
		switch session.CLI {
		case Claude:
			t.claudeLiveness(session, running)
		case Copilot:
			session.Liveness, session.PID = t.copilotLiveness(session)
			session.Activity = turnActivity(session.TurnOpen && session.Liveness == LivenessRunning)
		case Codex:
			session.Activity = turnActivity(session.TurnOpen)
		}
		if before != session.liveState() {
			t.changed[key] = true
		}
	}
}

func (t *Trail) claudeLiveness(session *Session, running map[string]claudeProcess) {
	session.Liveness, session.Activity, session.PID = LivenessClosed, "", 0
	if session.Parent != "" {
		t.agentLiveness(session, running)
		return
	}
	if process, ok := running[session.ID]; ok {
		session.Liveness, session.Activity, session.PID = LivenessRunning, process.Status, process.PID
	}
}

func (t *Trail) agentLiveness(session *Session, running map[string]claudeProcess) {
	parent := t.sessions[session.Parent]
	if session.Finished || parent == nil {
		return
	}
	if _, parentRunning := running[parent.ID]; !parentRunning {
		return
	}
	session.Finished = readClaudeAgentMeta(session.metaPath).Finished
	if !session.Finished {
		session.Liveness, session.Activity = LivenessRunning, turnActivity(true)
	}
}

type liveState struct {
	liveness Liveness
	activity string
	pid      int
	finished bool
}

func (s *Session) liveState() liveState {
	return liveState{s.Liveness, s.Activity, s.PID, s.Finished}
}

func turnActivity(open bool) string {
	if open {
		return "working"
	}
	return ""
}

func (t *Trail) runningClaudeSessions() map[string]claudeProcess {
	running := map[string]claudeProcess{}
	files, _ := filepath.Glob(filepath.Join(t.home, ".claude", "sessions", "*.json"))
	for _, file := range files {
		raw, err := os.ReadFile(file)
		if err != nil {
			continue
		}
		var process claudeProcess
		if json.Unmarshal(raw, &process) == nil && process.SessionID != "" && processAlive(process.PID) {
			running[process.SessionID] = process
		}
	}
	return running
}

func (t *Trail) copilotLiveness(session *Session) (Liveness, int) {
	locks, _ := filepath.Glob(filepath.Join(t.home, ".copilot", "session-state", session.ID, "inuse.*.lock"))
	for _, lock := range locks {
		pid, err := strconv.Atoi(strings.TrimSuffix(strings.TrimPrefix(filepath.Base(lock), "inuse."), ".lock"))
		if err == nil && processAlive(pid) {
			return LivenessRunning, pid
		}
	}
	return LivenessClosed, 0
}

func processAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	err := syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}
