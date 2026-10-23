package tui

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/rubentanahara/agent-trail/internal/trail"
)

var startedAt = time.Date(2026, 10, 22, 9, 0, 0, 0, time.UTC)

func claudeSessionAt(key, name, activity string) trail.Session {
	return trail.Session{Key: key, ID: key, CLI: trail.Claude, Name: name, Events: 1, Last: startedAt, Liveness: trail.LivenessRunning, Activity: activity}
}

func codexSessionAt(key, name string, turnOpen bool) trail.Session {
	return trail.Session{Key: key, ID: key, CLI: trail.Codex, Name: name, Events: 1, Last: startedAt, TurnOpen: turnOpen}
}

func notifyModel(notify bool, history ...trail.Session) model {
	m := newModel(nil, trail.Batch{Sessions: history}, Context{Notify: notify})
	return sized(m, 120, 30)
}

func poll(m *model, sessions ...trail.Session) string {
	cmd := m.absorbPolled(trail.Batch{Sessions: sessions})
	if cmd == nil {
		return ""
	}
	raw, ok := cmd().(tea.RawMsg)
	if !ok {
		return ""
	}
	return raw.Msg.(string)
}

func TestBusyToIdleInAPollAlerts(t *testing.T) {
	m := notifyModel(true, claudeSessionAt("claude:a", "login", "busy"))
	raw := poll(&m, claudeSessionAt("claude:a", "login", "idle"))
	if m.notice != "◐ login is waiting for you · w jumps" {
		t.Errorf("notice = %q", m.notice)
	}
	if !strings.HasPrefix(raw, "\a") || !strings.Contains(raw, "login is waiting for you") {
		t.Errorf("raw = %q", raw)
	}
}

func TestStartupHistoryNeverAlerts(t *testing.T) {
	m := notifyModel(true, claudeSessionAt("claude:a", "login", "idle"))
	if m.notice != "" {
		t.Errorf("notice = %q", m.notice)
	}
	if raw := poll(&m, claudeSessionAt("claude:a", "login", "idle")); raw != "" || m.notice != "" {
		t.Errorf("still waiting alerted: %q %q", raw, m.notice)
	}
}

func TestAgentSessionGoingIdleDoesNotAlert(t *testing.T) {
	agent := func(activity string) trail.Session {
		session := claudeSessionAt("claude:agent", "helper", activity)
		session.Parent = "claude:a"
		return session
	}
	m := notifyModel(true, claudeSessionAt("claude:a", "login", "busy"), agent("busy"))
	if raw := poll(&m, agent("idle")); raw != "" || m.notice != "" {
		t.Errorf("agent alerted: %q %q", raw, m.notice)
	}
}

func TestCodexTurnClosingAlerts(t *testing.T) {
	m := notifyModel(true, codexSessionAt("codex:b", "tests", true))
	if raw := poll(&m, codexSessionAt("codex:b", "tests", true)); raw != "" {
		t.Errorf("open turn alerted: %q", raw)
	}
	raw := poll(&m, codexSessionAt("codex:b", "tests", false))
	if m.notice != "◐ tests is waiting for you · w jumps" || raw == "" {
		t.Errorf("notice = %q, raw = %q", m.notice, raw)
	}
}

func TestNotifyOffKeepsTheNoticeButSendsNothing(t *testing.T) {
	m := notifyModel(false, claudeSessionAt("claude:a", "login", "busy"))
	raw := poll(&m, claudeSessionAt("claude:a", "login", "idle"))
	if raw != "" || m.notice == "" {
		t.Errorf("raw = %q, notice = %q", raw, m.notice)
	}
}

func TestSeveralTransitionsNameTheFirstAndCountTheRest(t *testing.T) {
	m := notifyModel(true, claudeSessionAt("claude:a", "login", "busy"), claudeSessionAt("claude:c", "docs", "busy"))
	poll(&m, claudeSessionAt("claude:a", "login", "idle"), claudeSessionAt("claude:c", "docs", "idle"))
	if m.notice != "◐ login is waiting for you (+1 more) · w jumps" {
		t.Errorf("notice = %q", m.notice)
	}
}

func TestHiddenAutomatedSessionDoesNotAlert(t *testing.T) {
	m := notifyModel(true, claudeSessionAt("claude:a", "login", "busy"))
	m.hideAutomated = true
	automated := claudeSessionAt("claude:a", "login", "idle")
	automated.Automated = true
	if raw := poll(&m, automated); raw != "" || m.notice != "" {
		t.Errorf("hidden automated alerted: %q %q", raw, m.notice)
	}
}

func TestWSelectsLongestWaitingAndCycles(t *testing.T) {
	older, newer := claudeSessionAt("claude:a", "login", "idle"), claudeSessionAt("claude:c", "docs", "busy")
	older.Last = startedAt
	m := notifyModel(false, older, newer, claudeSessionAt("claude:d", "busy one", "busy"))
	poll(&m, claudeSessionAt("claude:c", "docs", "idle"))
	m = press(m, "w")
	if m.activeSession != "claude:a" || m.focus != eventsPane {
		t.Fatalf("first w: active = %q focus = %v", m.activeSession, m.focus)
	}
	m = press(m, "w")
	if m.activeSession != "claude:c" {
		t.Fatalf("second w: active = %q", m.activeSession)
	}
	m = press(m, "w")
	if m.activeSession != "claude:a" {
		t.Fatalf("third w: active = %q", m.activeSession)
	}
}

func TestWWithNoWaitingSessionSaysSo(t *testing.T) {
	m := press(notifyModel(false, claudeSessionAt("claude:a", "login", "busy")), "w")
	if m.notice != "no session is waiting for you" {
		t.Errorf("notice = %q", m.notice)
	}
}

func TestBellAndDesktopNotificationBytes(t *testing.T) {
	t.Setenv("TMUX", "")
	if got, want := alertSequence("login"), "\a\x1b]9;login is waiting for you\a"; got != want {
		t.Errorf("outside tmux = %q, want %q", got, want)
	}
	t.Setenv("TMUX", "/tmp/tmux-501/default,1,0")
	want := "\a\x1bPtmux;\x1b\x1b]9;login is waiting for you\a\x1b\\"
	if got := alertSequence("login"); got != want {
		t.Errorf("inside tmux = %q, want %q", got, want)
	}
}

func TestClosedCopilotSessionStopsWaiting(t *testing.T) {
	copilot := trail.Session{Key: "copilot:c", ID: "c", CLI: trail.Copilot, Name: "cop", Events: 1, Last: startedAt, Liveness: trail.LivenessRunning, TurnOpen: true}
	m := notifyModel(false, copilot)
	copilot.TurnOpen = false
	m.absorbPolled(trail.Batch{Sessions: []trail.Session{copilot}})
	if keys := m.waitingKeys(); len(keys) != 1 {
		t.Fatalf("after its turn ended: waiting = %v", keys)
	}
	copilot.Liveness = trail.LivenessClosed
	m.absorbPolled(trail.Batch{Sessions: []trail.Session{copilot}})
	if keys := m.waitingKeys(); len(keys) != 0 {
		t.Fatalf("after it closed: waiting = %v", keys)
	}
}
