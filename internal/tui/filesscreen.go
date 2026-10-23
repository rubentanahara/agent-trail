package tui

import (
	"path/filepath"
	"slices"
	"strings"
	"time"

	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"

	"github.com/rubentanahara/agent-trail/internal/trail"
)

type changedFile struct {
	path      string
	label     string
	added     int
	removed   int
	edits     int
	uncounted int
	last      time.Time
	events    []trail.Event
}

type patchBlock struct {
	event trail.Event
	lines []string
}

type filePatchesMsg struct {
	path   string
	blocks []patchBlock
	err    error
}

type filesScreen struct {
	shown     bool
	files     []changedFile
	cursor    int
	top       int
	diffFocus bool
	loading   bool
	err       error
	diff      viewport.Model
}

func (m *model) openFiles() tea.Cmd {
	m.filesScreen = filesScreen{shown: true, files: m.changedFiles(), diff: viewport.New()}
	m.filesScreen.diff.SoftWrap = true
	m.resizeFiles()
	return m.pickFile(0)
}

func (m *model) resizeFiles() {
	if !m.filesScreen.shown {
		return
	}
	_, rightW, bodyH := m.filesGeometry()
	m.filesScreen.diff.SetWidth(max(1, rightW-2))
	m.filesScreen.diff.SetHeight(max(1, bodyH-borderAndTitle-1))
}

func (m model) changedFiles() []changedFile {
	byPath := map[string]*changedFile{}
	for _, event := range m.events {
		if len(event.Changes) == 0 || !m.inFilesScope(event) {
			continue
		}
		for _, change := range event.Changes {
			file, seen := byPath[change.Path]
			if !seen {
				file = &changedFile{path: change.Path}
				byPath[change.Path] = file
			}
			m.recordChange(file, event, change)
		}
	}
	files := make([]changedFile, 0, len(byPath))
	for _, file := range byPath {
		slices.SortStableFunc(file.events, func(a, b trail.Event) int { return a.Time.Compare(b.Time) })
		files = append(files, *file)
	}
	slices.SortFunc(files, func(a, b changedFile) int { return b.last.Compare(a.last) })
	return files
}

func (m model) recordChange(file *changedFile, event trail.Event, change trail.FileChange) {
	file.edits++
	file.added += change.Added
	file.removed += change.Removed
	if !change.Counted {
		file.uncounted++
	}
	file.events = append(file.events, event)
	if file.last.IsZero() || !event.Time.Before(file.last) {
		file.last = event.Time
		file.label = relativeTo(m.sessions[event.Session].Cwd, change.Path)
	}
}

func (m model) inFilesScope(event trail.Event) bool {
	session := m.sessions[event.Session]
	if m.activeSession != "" {
		return event.Session == m.activeSession || session.Parent == m.activeSession
	}
	if m.isChild(session) {
		return m.listed(session.Parent)
	}
	return m.listed(event.Session)
}

func relativeTo(cwd, path string) string {
	if cwd == "" {
		return path
	}
	relative, err := filepath.Rel(cwd, path)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return path
	}
	return relative
}

func (m *model) pickFile(row int) tea.Cmd {
	screen := &m.filesScreen
	if len(screen.files) == 0 {
		return nil
	}
	screen.cursor = max(0, min(row, len(screen.files)-1))
	screen.loading, screen.err = true, nil
	screen.diff.SetContent("")
	return loadFilePatches(screen.files[screen.cursor])
}

func loadFilePatches(file changedFile) tea.Cmd {
	return func() tea.Msg {
		blocks := make([]patchBlock, 0, len(file.events))
		for _, event := range file.events {
			patches, err := trail.LoadPatch(event)
			if err != nil {
				return filePatchesMsg{path: file.path, err: err}
			}
			blocks = append(blocks, patchBlock{event: event, lines: linesOfFile(patches, file.path)})
		}
		return filePatchesMsg{path: file.path, blocks: blocks}
	}
}

func linesOfFile(patches []trail.FilePatch, path string) []string {
	var lines []string
	for _, patch := range patches {
		if patch.Path == path {
			lines = append(lines, patch.Lines...)
		}
	}
	return lines
}

func (m *model) showFilePatches(msg filePatchesMsg) {
	screen := &m.filesScreen
	if !screen.shown || len(screen.files) == 0 || screen.files[screen.cursor].path != msg.path {
		return
	}
	screen.loading, screen.err = false, msg.err
	screen.diff.SetContent(renderBlocks(msg.blocks))
	screen.diff.GotoTop()
}

func (m *model) filesKey(key string) (tea.Cmd, bool) {
	screen := &m.filesScreen
	switch key {
	case "q", "ctrl+c":
		return nil, true
	case "d", "esc":
		screen.shown = false
	case "tab":
		screen.diffFocus = !screen.diffFocus
	case "enter":
		screen.diffFocus = true
	case "pgup":
		screen.diff.PageUp()
	case "pgdown":
		screen.diff.PageDown()
	case "up", "k":
		return m.moveInFiles(-1), false
	case "down", "j":
		return m.moveInFiles(1), false
	}
	return nil, false
}

func (m *model) moveInFiles(delta int) tea.Cmd {
	screen := &m.filesScreen
	if screen.diffFocus {
		screen.diff.ScrollDown(delta)
		return nil
	}
	return m.pickFile(screen.cursor + delta)
}
