package tui

import (
	"cmp"

	"github.com/rubentanahara/agent-trail/internal/trail"
)

const (
	claudeIcon  = ""
	codexIcon   = ""
	copilotIcon = ""
	agentIcon   = ""
	gitIcon     = ""
	folderIcon  = ""
)

var cliIcons = map[trail.CLI]string{trail.Claude: claudeIcon, trail.Codex: codexIcon, trail.Copilot: copilotIcon}

func (m model) icon(glyph string) string {
	if !m.context.Icons {
		return ""
	}
	return glyph + " "
}

func (m model) iconBefore(glyph, value string) string {
	if value == "" {
		return ""
	}
	return m.icon(glyph) + value
}

func (m model) cliIcon(cli trail.CLI) string {
	return m.icon(cliIcons[cli])
}

func (m model) agentName(event trail.Event) string {
	session, ok := m.sessions[event.Session]
	if !ok || session.Parent == "" {
		return ""
	}
	return cmp.Or(session.Name, event.Agent)
}

func (m model) eventWho(event trail.Event) string {
	if name := m.agentName(event); name != "" {
		return name
	}
	return event.Who()
}

func (m model) eventWhoIcon(event trail.Event) string {
	if m.agentName(event) != "" {
		return m.icon(agentIcon)
	}
	return m.cliIcon(event.CLI)
}
