package tui

import (
	"slices"
	"sort"
	"strings"
	"time"

	"charm.land/bubbles/v2/textinput"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"

	"github.com/rubentanahara/agent-trail/internal/trail"
)

const (
	pollInterval   = time.Second
	pageRows       = 10
	wheelRows      = 3
	noEvent        = -1
	allSessionsRow = 0
)

type pane int

const (
	sessionsPane pane = iota
	eventsPane
	detailPane
	paneCount
)

type pollMsg struct{}

type batchMsg struct{ batch trail.Batch }

type detailMsg struct {
	index  int
	detail trail.Detail
	err    error
}

type Context struct {
	Repo         string
	PricesAsOf   string
	PriceWarning string
}

type model struct {
	follower *trail.Trail
	context  Context

	events   []trail.Event
	haystack []string
	sessions map[string]trail.Session
	order    []string

	sessionCursor int
	sessionTop    int
	activeSession string

	visible     []int
	eventCursor int
	eventTop    int
	follow      bool
	showUsage   bool
	hideNoise   bool

	focus        pane
	search       textinput.Model
	searching    bool
	searchTarget pane
	filterText   [paneCount]string
	filter       query
	sessionQuery query
	detailQuery  query

	detail          viewport.Model
	detailRequested int
	detailShown     int
	detailData      trail.Detail
	detailErr       error
	raw             bool

	width  int
	height int
}

var noiseKinds = map[trail.Kind]bool{trail.KindAttachment: true, trail.KindSystem: true}

func Run(follower *trail.Trail, history trail.Batch, context Context) error {
	_, err := tea.NewProgram(newModel(follower, history, context)).Run()
	return err
}

func newModel(follower *trail.Trail, history trail.Batch, context Context) model {
	search := textinput.New()
	search.Prompt = "/ "
	search.Placeholder = "words must all match · 'exact · !exclude · esc clears"
	detail := viewport.New()
	detail.SoftWrap = true
	m := model{
		follower:        follower,
		context:         context,
		sessions:        map[string]trail.Session{},
		follow:          true,
		showUsage:       true,
		hideNoise:       true,
		focus:           eventsPane,
		search:          search,
		detail:          detail,
		detailRequested: noEvent,
		detailShown:     noEvent,
	}
	m.absorb(history)
	return m
}

func (m model) Init() tea.Cmd {
	return tea.Batch(tick(), m.requestDetail())
}

func tick() tea.Cmd {
	return tea.Tick(pollInterval, func(time.Time) tea.Msg { return pollMsg{} })
}

func (m model) poll() tea.Cmd {
	follower := m.follower
	return func() tea.Msg { return batchMsg{follower.Poll()} }
}

func loadDetail(index int, event trail.Event) tea.Cmd {
	return func() tea.Msg {
		detail, err := trail.LoadDetail(event)
		return detailMsg{index: index, detail: detail, err: err}
	}
}

func (m *model) requestDetail() tea.Cmd {
	index := m.selectedEvent()
	if index == m.detailRequested {
		return nil
	}
	m.detailRequested = index
	if index == noEvent {
		m.detailData, m.detailShown = trail.Detail{}, noEvent
		m.refreshDetail()
		return nil
	}
	return loadDetail(index, m.events[index])
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.refreshDetail()
	case pollMsg:
		return m, m.poll()
	case batchMsg:
		m.absorb(msg.batch)
		cmds = append(cmds, tick())
	case detailMsg:
		if msg.index == m.detailRequested {
			m.detailData, m.detailErr, m.detailShown = msg.detail, msg.err, msg.index
			m.refreshDetail()
			m.detail.GotoTop()
		}
	case tea.MouseWheelMsg:
		m.wheel(msg.Mouse())
	case tea.MouseClickMsg:
		m.click(msg.Mouse())
	case tea.KeyPressMsg:
		cmd, quit := m.key(msg)
		if quit {
			return m, tea.Quit
		}
		cmds = append(cmds, cmd)
	}
	m.clampScroll()
	cmds = append(cmds, m.requestDetail())
	return m, tea.Batch(cmds...)
}

func (m *model) absorb(batch trail.Batch) {
	for _, session := range batch.Sessions {
		m.sessions[session.Key] = session
	}
	if len(batch.Sessions) > 0 {
		m.reorderSessions()
	}
	for _, event := range batch.Events {
		m.events = append(m.events, event)
		m.haystack = append(m.haystack, strings.ToLower(event.Who()+" "+string(event.Kind)+" "+event.Summary))
		if m.includes(len(m.events) - 1) {
			m.visible = append(m.visible, len(m.events)-1)
		}
	}
	if m.follow {
		m.eventCursor = len(m.visible) - 1
	}
}

func (m *model) reorderSessions() {
	cursorKey := m.sessionKeyAt(m.sessionCursor)
	m.order = m.order[:0]
	for key, session := range m.sessions {
		if session.Events > 0 {
			m.order = append(m.order, key)
		}
	}
	m.order = slices.DeleteFunc(m.order, func(key string) bool {
		return !m.sessionQuery.matches(sessionHaystack(m.sessions[key]))
	})
	sort.Slice(m.order, func(i, j int) bool {
		return m.sessions[m.order[i]].Last.After(m.sessions[m.order[j]].Last)
	})
	m.sessionCursor = m.rowOf(cursorKey)
}

func (m model) sessionKeyAt(row int) string {
	if row <= allSessionsRow || row > len(m.order) {
		return ""
	}
	return m.order[row-1]
}

func (m model) rowOf(key string) int {
	for i, candidate := range m.order {
		if candidate == key {
			return i + 1
		}
	}
	return allSessionsRow
}

func (m model) includes(index int) bool {
	event := m.events[index]
	if m.activeSession != "" && event.Session != m.activeSession {
		return false
	}
	if !m.showUsage && event.Kind == trail.KindUsage {
		return false
	}
	if m.hideNoise && noiseKinds[event.Kind] {
		return false
	}
	return m.filter.matches(m.haystack[index])
}

func (m *model) refilter() {
	selected := m.selectedEvent()
	m.visible = m.visible[:0]
	for i := range m.events {
		if m.includes(i) {
			m.visible = append(m.visible, i)
		}
	}
	if m.follow || selected == noEvent {
		m.eventCursor = len(m.visible) - 1
		return
	}
	m.eventCursor = sort.SearchInts(m.visible, selected)
	if m.eventCursor >= len(m.visible) {
		m.eventCursor = len(m.visible) - 1
	}
}

func (m model) selectedEvent() int {
	if m.eventCursor < 0 || m.eventCursor >= len(m.visible) {
		return noEvent
	}
	return m.visible[m.eventCursor]
}

func (m *model) selectSession(row int) {
	m.sessionCursor = max(allSessionsRow, min(row, len(m.order)))
	key := m.sessionKeyAt(m.sessionCursor)
	if key != m.activeSession {
		m.activeSession = key
		m.refilter()
	}
}

func (m *model) moveEvents(delta int) {
	if len(m.visible) == 0 {
		return
	}
	m.eventCursor = max(0, min(m.eventCursor+delta, len(m.visible)-1))
	m.follow = m.eventCursor == len(m.visible)-1
}

func (m *model) key(msg tea.KeyPressMsg) (tea.Cmd, bool) {
	if m.searching {
		return m.searchKey(msg), false
	}
	switch msg.String() {
	case "q", "ctrl+c":
		return nil, true
	case "tab":
		m.focus = (m.focus + 1) % paneCount
	case "shift+tab":
		m.focus = (m.focus + paneCount - 1) % paneCount
	case "/":
		m.searching, m.searchTarget = true, m.focus
		m.search.SetValue(m.filterText[m.focus])
		m.search.CursorEnd()
		return m.search.Focus(), false
	case "esc":
		m.escape()
	case "a":
		m.selectSession(allSessionsRow)
	case "s":
		m.jumpToEventSession()
	case "u":
		m.showUsage = !m.showUsage
		m.refilter()
	case "n":
		m.hideNoise = !m.hideNoise
		m.refilter()
	case "1":
		m.focus = sessionsPane
	case "2":
		m.focus = eventsPane
	case "3":
		m.focus = detailPane
	case "r":
		m.raw = !m.raw
		m.refreshDetail()
		m.detail.GotoTop()
	case "f":
		m.follow = !m.follow
		if m.follow {
			m.eventCursor = len(m.visible) - 1
		}
	default:
		return m.paneKey(msg), false
	}
	return nil, false
}

func (m *model) escape() {
	switch {
	case m.filterText[m.focus] != "":
		m.applyFilter(m.focus, "")
	case m.focus == detailPane:
		m.focus = eventsPane
	case m.activeSession != "":
		m.selectSession(allSessionsRow)
	}
}

func (m *model) applyFilter(target pane, text string) {
	if m.filterText[target] == text {
		return
	}
	m.filterText[target] = text
	switch target {
	case sessionsPane:
		m.sessionQuery = parseQuery(text)
		m.reorderSessions()
	case eventsPane:
		m.filter = parseQuery(text)
		m.refilter()
	case detailPane:
		m.detailQuery = parseQuery(text)
		m.refreshDetail()
		m.detail.GotoTop()
	}
}

func sessionHaystack(session trail.Session) string {
	return strings.ToLower(strings.Join([]string{
		string(session.CLI), session.Name, session.ID, session.Model, session.Agent, strings.Join(session.Subagents, " "),
		session.Branch, string(session.Liveness), session.Activity, string(session.Billing), session.Plan,
	}, " "))
}

func (m *model) searchKey(msg tea.KeyPressMsg) tea.Cmd {
	switch msg.String() {
	case "enter":
		m.searching = false
		m.search.Blur()
		return nil
	case "esc":
		m.searching = false
		m.search.Blur()
		m.search.SetValue("")
	}
	var cmd tea.Cmd
	m.search, cmd = m.search.Update(msg)
	m.applyFilter(m.searchTarget, m.search.Value())
	return cmd
}

func (m *model) jumpToEventSession() {
	index := m.selectedEvent()
	if index == noEvent {
		return
	}
	m.selectSession(m.rowOf(m.events[index].Session))
	m.focus = eventsPane
}

func (m *model) paneKey(msg tea.KeyPressMsg) tea.Cmd {
	key := msg.String()
	switch m.focus {
	case sessionsPane:
		m.sessionsKey(key)
	case eventsPane:
		m.eventsKey(key)
	case detailPane:
		if key == "enter" {
			m.focus = eventsPane
			return nil
		}
		var cmd tea.Cmd
		m.detail, cmd = m.detail.Update(msg)
		return cmd
	}
	return nil
}

func (m *model) sessionsKey(key string) {
	switch key {
	case "up", "k":
		m.selectSession(m.sessionCursor - 1)
	case "down", "j":
		m.selectSession(m.sessionCursor + 1)
	case "home", "g":
		m.selectSession(allSessionsRow)
	case "end", "G":
		m.selectSession(len(m.order))
	case "enter", "right", "l":
		m.focus = eventsPane
	}
}

func (m *model) eventsKey(key string) {
	switch key {
	case "up", "k":
		m.moveEvents(-1)
	case "down", "j":
		m.moveEvents(1)
	case "pgup", "ctrl+u":
		m.moveEvents(-pageRows)
	case "pgdown", "ctrl+d":
		m.moveEvents(pageRows)
	case "home", "g":
		m.moveEvents(-len(m.visible))
	case "end", "G":
		m.moveEvents(len(m.visible))
	case "enter", "right", "l":
		m.focus = detailPane
	case "left", "h":
		m.focus = sessionsPane
	}
}

func (m *model) wheel(mouse tea.Mouse) {
	delta := wheelRows
	if mouse.Button == tea.MouseWheelUp {
		delta = -wheelRows
	}
	switch m.paneAt(mouse.X, mouse.Y) {
	case sessionsPane:
		m.selectSession(m.sessionCursor + delta/wheelRows)
	case eventsPane:
		m.moveEvents(delta)
	case detailPane:
		if delta < 0 {
			m.detail.ScrollUp(-delta)
		} else {
			m.detail.ScrollDown(delta)
		}
	}
}

func (m *model) click(mouse tea.Mouse) {
	if mouse.Button != tea.MouseLeft {
		return
	}
	target := m.paneAt(mouse.X, mouse.Y)
	m.focus = target
	geometry := m.layout()
	switch target {
	case sessionsPane:
		row := mouse.Y - borderAndTitle + m.sessionTop
		if row >= 0 && row <= len(m.order) && mouse.Y < geometry.sessionsH-1 {
			m.selectSession(row)
		}
	case eventsPane:
		row := mouse.Y - borderAndTitle + m.eventTop
		if row >= 0 && row < len(m.visible) && mouse.Y < geometry.eventsH-1 {
			m.eventCursor = row
			m.follow = row == len(m.visible)-1
		}
	}
}

func (m *model) clampScroll() {
	geometry := m.layout()
	m.sessionTop = scrollTop(m.sessionTop, m.sessionCursor, len(m.order)+1, geometry.sessionsH-borderAndTitle-1)
	m.eventTop = scrollTop(m.eventTop, m.eventCursor, len(m.visible), geometry.eventsH-borderAndTitle-1)
}

func scrollTop(top, cursor, total, rows int) int {
	if rows <= 0 {
		return 0
	}
	if cursor < top {
		top = cursor
	}
	if cursor >= top+rows {
		top = cursor - rows + 1
	}
	return max(0, min(top, total-rows))
}
