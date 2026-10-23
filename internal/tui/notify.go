package tui

import (
	"cmp"
	"os"
	"slices"
	"strconv"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/rubentanahara/agent-trail/internal/trail"
)

const (
	bellByte        = "\a"
	waitingSuffix   = " is waiting for you"
	noWaitingNotice = "no session is waiting for you"
	jumpHint        = " · w jumps"
)

func (m *model) absorbPolled(batch trail.Batch) tea.Cmd {
	handedBack := m.trackWaiting(batch.Sessions)
	m.absorb(batch)
	return m.alert(handedBack)
}

func (m *model) seedWaiting(sessions []trail.Session) {
	m.waitingSince = map[string]time.Time{}
	for _, session := range sessions {
		if session.Parent == "" && isWaiting(session) {
			m.waitingSince[session.Key] = session.Last
		}
	}
}

func (m *model) trackWaiting(sessions []trail.Session) []trail.Session {
	var handedBack []trail.Session
	for _, session := range sessions {
		if session.Parent != "" {
			continue
		}
		_, held := m.waitingSince[session.Key]
		switch {
		case becameWaiting(m.sessions[session.Key], session):
			m.waitingSince[session.Key] = time.Now()
			if m.notifiable(session) {
				handedBack = append(handedBack, session)
			}
		case held && !stillWaiting(session):
			delete(m.waitingSince, session.Key)
		}
	}
	return handedBack
}

func becameWaiting(previous, current trail.Session) bool {
	if current.CLI == trail.Claude {
		return isWaiting(current) && !isWaiting(previous)
	}
	return previous.TurnOpen && !current.TurnOpen
}

func stillWaiting(session trail.Session) bool {
	if session.CLI == trail.Claude {
		return isWaiting(session)
	}
	return !session.TurnOpen
}

func (m model) notifiable(session trail.Session) bool {
	return !m.hideAutomated || !session.Automated
}

func (m *model) alert(handedBack []trail.Session) tea.Cmd {
	if len(handedBack) == 0 {
		return nil
	}
	name := sessionLabel(handedBack[0])
	m.notice = "◐ " + name + waitingSuffix + moreText(len(handedBack)-1) + jumpHint
	if !m.context.Notify {
		return nil
	}
	return tea.Raw(alertSequence(name))
}

func moreText(others int) string {
	if others == 0 {
		return ""
	}
	return " (+" + strconv.Itoa(others) + " more)"
}

func alertSequence(name string) string {
	desktop := ansi.Notify(name + waitingSuffix)
	if os.Getenv("TMUX") != "" {
		desktop = ansi.TmuxPassthrough(desktop)
	}
	return bellByte + desktop
}

func (m model) waitingKeys() []string {
	var keys []string
	for key := range m.waitingSince {
		if session, ok := m.sessions[key]; ok && m.notifiable(session) {
			keys = append(keys, key)
		}
	}
	slices.SortFunc(keys, func(a, b string) int {
		return cmp.Or(m.waitingSince[a].Compare(m.waitingSince[b]), cmp.Compare(a, b))
	})
	return keys
}

func (m *model) goToWaiting() tea.Cmd {
	keys := m.waitingKeys()
	if len(keys) == 0 {
		m.notice = noWaitingNotice
		return nil
	}
	next := keys[0]
	if at := slices.Index(keys, m.activeSession); at >= 0 {
		next = keys[(at+1)%len(keys)]
	}
	m.selectSession(m.rowOf(next))
	m.focus = eventsPane
	session := m.sessions[next]
	if session.Liveness == trail.LivenessRunning && session.PID > 0 {
		return m.jumpToRunning(session)
	}
	return nil
}
