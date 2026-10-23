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
		before := [3]any{session.Liveness, session.Activity, session.PID}
		switch session.CLI {
		case Claude:
			session.Liveness, session.Activity, session.PID = LivenessClosed, "", 0
			if process, ok := running[session.ID]; ok {
				session.Liveness, session.Activity, session.PID = LivenessRunning, process.Status, process.PID
			}
		case Copilot:
			session.Liveness, session.PID = t.copilotLiveness(session)
			session.Activity = turnActivity(session.TurnOpen && session.Liveness == LivenessRunning)
		case Codex:
			session.Activity = turnActivity(session.TurnOpen)
		}
		if before != [3]any{session.Liveness, session.Activity, session.PID} {
			t.changed[key] = true
		}
	}
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
