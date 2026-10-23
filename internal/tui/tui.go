package tui

import (
	"slices"
	"sort"
	"strings"
	"time"

	"charm.land/bubbles/v2/textinput"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"

	"github.com/rubentanahara/agent-trail/internal/limits"
	"github.com/rubentanahara/agent-trail/internal/savings"
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

type pollMsg struct{ at time.Time }

type batchMsg struct{ batch trail.Batch }

type detailMsg struct {
	index  int
	source trail.Source
	detail trail.Detail
	err    error
}

type deletedMsg struct {
	key   string
	label string
	err   error
}

type statsMsg struct{ batch trail.Batch }

type savingsMsg struct{ found []savings.Saving }

type Context struct {
	Repo         string
	Home         string
	Version      string
	PricesAsOf   string
	PriceWarning string
	Everywhere   *trail.Trail
	StatsSince   time.Time
	Icons        bool
	Notify       bool
}

type model struct {
	follower *trail.Trail
	context  Context

	events   []trail.Event
	haystack []string
	revision int
	ledger   *ledgerCache
	sessions map[string]trail.Session
	order    []string
	children map[string][]string
	folded   map[string]bool

	sessionCursor int
	sessionTop    int
	activeSession string

	visible       []int
	eventCursor   int
	eventTop      int
	follow        bool
	showUsage     bool
	hideNoise     bool
	runningOnly   bool
	hideAutomated bool
	zoomed        bool

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

	summary summaryState

	confirmDelete string
	notice        string
	waitingSince  map[string]time.Time

	now time.Time
	pr  prScreen

	statsScreen statsScreen
	filesScreen filesScreen
	help        helpScreen

	claudeLimits    limits.Claude
	hasClaudeLimits bool

	width  int
	height int
}

var (
	noiseKinds = map[trail.Kind]bool{trail.KindAttachment: true, trail.KindSystem: true}
	focusKeys  = map[string]pane{"1": sessionsPane, "2": eventsPane, "3": detailPane}
)

func isNoise(event trail.Event) bool {
	if event.Kind == trail.KindThinking {
		return event.Summary == trail.HiddenThinking
	}
	return noiseKinds[event.Kind]
}

func Run(follower *trail.Trail, history trail.Batch, context Context) error {
	_, err := tea.NewProgram(newModel(follower, history, context)).Run()
	return err
}

func newModel(follower *trail.Trail, history trail.Batch, context Context) model {
	hideAutomated := context.Repo == trail.EveryRepository
	search := textinput.New()
	search.Prompt = "/ "
	search.Placeholder = "words must all match · 'exact · !exclude · esc clears"
	detail := viewport.New()
	detail.SoftWrap = true
	m := model{
		follower:        follower,
		context:         context,
		sessions:        map[string]trail.Session{},
		ledger:          &ledgerCache{},
		folded:          map[string]bool{},
		follow:          true,
		showUsage:       true,
		hideNoise:       true,
		hideAutomated:   hideAutomated,
		focus:           eventsPane,
		search:          search,
		detail:          detail,
		summary:         summaryState{rendered: &markdownCache{}},
		pr:              newPRScreen(context.Repo),
		detailRequested: noEvent,
		detailShown:     noEvent,
	}
	m.seedWaiting(history.Sessions)
	m.absorb(history)
	return m
}

func (m model) Init() tea.Cmd {
	return tea.Batch(tick(), m.requestDetail(), m.startPRs())
}

func tick() tea.Cmd {
	return tea.Tick(pollInterval, func(at time.Time) tea.Msg { return pollMsg{at: at} })
}

func (m model) poll() tea.Cmd {
	follower := m.follower
	return func() tea.Msg { return batchMsg{follower.Poll()} }
}

func loadDetail(index int, event trail.Event) tea.Cmd {
	return func() tea.Msg {
		detail, err := trail.LoadDetail(event)
		return detailMsg{index: index, source: event.Source, detail: detail, err: err}
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

func (m model) detailStillWanted(msg detailMsg) bool {
	return msg.index == m.detailRequested && msg.index < len(m.events) && m.events[msg.index].Source == msg.source
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.refreshDetail()
		m.resizeFiles()
	case pollMsg:
		m.now = msg.at
		return m, tea.Batch(m.poll(), m.statsScreen.poll(m.context.Everywhere), pollLimits(m.context.Home), m.pollPRs())
	case prMessage:
		cmds = append(cmds, m.absorbPR(msg))
	case batchMsg:
		cmds = append(cmds, m.absorbPolled(msg.batch), tick())
	case limitsMsg:
		m.claudeLimits, m.hasClaudeLimits = msg.claude, msg.found
	case statsMsg:
		m.statsScreen.absorb(msg.batch)
	case filePatchesMsg:
		m.showFilePatches(msg)
	case savingsMsg:
		m.statsScreen.savings = msg.found
	case deletedMsg:
		m.finishDelete(msg)
	case resumedMsg:
		m.finishResume(msg)
	case jumpedMsg:
		m.finishJump(msg)
	case summaryMsg:
		m.finishSummary(msg)
	case detailMsg:
		if m.detailStillWanted(msg) {
			m.detailData, m.detailErr, m.detailShown = msg.detail, msg.err, msg.index
			m.showDetailFromTop()
		}
	case tea.MouseWheelMsg:
		m.wheel(msg.Mouse())
	case tea.MouseClickMsg:
		m.click(msg.Mouse())
	case tea.KeyPressMsg:
		before := m.layout()
		armed := m.pr.quitArmed
		m.pr.quitArmed = false
		cmd, quit := m.key(msg)
		if quit && m.writeBlocksQuit(armed) {
			m.armQuit()
			quit = false
		}
		if quit {
			return m, tea.Quit
		}
		if m.layout() != before {
			m.refreshDetail()
		}
		cmds = append(cmds, cmd)
	}
	m.clampScroll()
	cmds = append(cmds, m.requestDetail())
	return m, tea.Batch(cmds...)
}

func (m *model) absorb(batch trail.Batch) {
	if changesTurns(m.sessions, batch) {
		m.revision++
	}
	if len(batch.Reset) > 0 {
		m.dropEvents(func(session string) bool { return slices.Contains(batch.Reset, session) })
	}
	for _, session := range batch.Sessions {
		m.sessions[session.Key] = session
	}
	if len(batch.Sessions) > 0 {
		m.reorderSessions()
	}
	for _, event := range batch.Events {
		m.events = append(m.events, event)
		m.haystack = append(m.haystack, strings.ToLower(event.Who()+" "+m.agentName(event)+" "+string(event.Kind)+" "+event.Summary))
		if m.includes(len(m.events) - 1) {
			m.visible = append(m.visible, len(m.events)-1)
		}
	}
	if m.follow {
		m.jumpToLatest()
	}
}

func changesTurns(known map[string]trail.Session, batch trail.Batch) bool {
	if len(batch.Events) > 0 || len(batch.Reset) > 0 {
		return true
	}
	for _, session := range batch.Sessions {
		previous, seen := known[session.Key]
		if !seen || previous.Parent != session.Parent {
			return true
		}
	}
	return false
}

func (m *model) jumpToLatest() {
	m.eventCursor = len(m.visible) - 1
}

func (m *model) showDetailFromTop() {
	m.refreshDetail()
	m.detail.GotoTop()
}

func (m *model) reorderSessions() {
	cursorKey := m.sessionKeyAt(m.sessionCursor)
	m.indexChildren()
	m.order = m.order[:0]
	for _, key := range m.listedParents() {
		m.order = append(m.order, key)
		if m.expanded(key) {
			m.order = append(m.order, m.children[key]...)
		}
	}
	m.sessionCursor = m.rowOf(cursorKey)
}

func (m *model) indexChildren() {
	m.children = map[string][]string{}
	for key, session := range m.sessions {
		if m.isChild(session) {
			m.children[session.Parent] = append(m.children[session.Parent], key)
		}
	}
	for _, keys := range m.children {
		sort.Slice(keys, func(i, j int) bool { return m.sessions[keys[i]].Start.Before(m.sessions[keys[j]].Start) })
	}
}

func (m model) isChild(session trail.Session) bool {
	parent, ok := m.sessions[session.Parent]
	return session.Events > 0 && ok && parent.Events > 0
}

func (m model) listedParents() []string {
	var keys []string
	for key, session := range m.sessions {
		if session.Events > 0 && !m.isChild(session) && m.listed(key) {
			keys = append(keys, key)
		}
	}
	sort.Slice(keys, func(i, j int) bool { return m.familyLast(keys[i]).After(m.familyLast(keys[j])) })
	return keys
}

func (m model) listed(key string) bool {
	session := m.sessions[key]
	switch {
	case m.hideAutomated && session.Automated:
		return false
	case m.runningOnly && !m.familyRunning(key):
		return false
	}
	return slices.ContainsFunc(m.family(key), func(member string) bool {
		return m.sessionQuery.matches(sessionHaystack(m.sessions[member]))
	})
}

func (m model) family(key string) []string {
	return append([]string{key}, m.children[key]...)
}

func (m model) familyRunning(key string) bool {
	return slices.ContainsFunc(m.family(key), func(member string) bool {
		return m.sessions[member].Liveness == trail.LivenessRunning
	})
}

func (m model) familyLast(key string) time.Time {
	last := m.sessions[key].Last
	for _, child := range m.children[key] {
		if m.sessions[child].Last.After(last) {
			last = m.sessions[child].Last
		}
	}
	return last
}

func (m model) expanded(key string) bool {
	if folded, chosen := m.folded[key]; chosen {
		return !folded
	}
	return m.autoExpanded(key)
}

func (m model) autoExpanded(key string) bool {
	return key == m.activeSession || key == m.sessions[m.activeSession].Parent || m.familyRunning(key)
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
	session := m.sessions[event.Session]
	if m.activeSession != "" && event.Session != m.activeSession && session.Parent != m.activeSession {
		return false
	}
	if m.hideAutomated && (session.Automated || m.sessions[session.Parent].Automated) {
		return false
	}
	if !m.showUsage && event.Kind == trail.KindUsage {
		return false
	}
	if m.hideNoise && isNoise(event) {
		return false
	}
	return m.filter.matches(m.haystack[index])
}

func (m *model) refilter() {
	m.refilterAround(m.selectedEvent())
}

func (m *model) refilterAround(selected int) {
	m.visible = m.visible[:0]
	for i := range m.events {
		if m.includes(i) {
			m.visible = append(m.visible, i)
		}
	}
	if m.follow || selected == noEvent {
		m.jumpToLatest()
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
		m.reorderSessions()
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
	m.notice = ""
	switch {
	case m.searching:
		return m.searchKey(msg), false
	case m.confirmDelete != "":
		return m.confirmKey(msg.String()), false
	case m.help.shown:
		return nil, m.helpKey(msg.String())
	case m.statsScreen.shown:
		return m.statsKey(msg.String())
	case m.filesScreen.shown:
		return m.filesKey(msg.String())
	case m.pr.shown:
		return m.prKey(msg)
	}
	if target, ok := focusKeys[msg.String()]; ok {
		m.focus = target
		return nil, false
	}
	switch msg.String() {
	case "q", "ctrl+c":
		return nil, true
	case "?":
		m.help.shown = true
	case "t":
		return m.statsScreen.open(m.context), false
	case "d":
		return m.openFiles(), false
	case "p":
		return m.openPRs(), false
	case "z":
		m.zoomed = !m.zoomed
	case "o":
		m.runningOnly = !m.runningOnly
		m.reorderSessions()
	case "m":
		m.hideAutomated = !m.hideAutomated
		m.reorderSessions()
		m.refilter()
	case "c":
		return m.resume(), false
	case "w":
		return m.goToWaiting(), false
	case "y":
		return m.toggleSummary(), false
	case "tab":
		m.focus = (m.focus + 1) % paneCount
	case "shift+tab":
		m.focus = (m.focus + paneCount - 1) % paneCount
	case "/":
		return m.startSearch(), false
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
	case "r":
		m.raw = !m.raw
		m.showDetailFromTop()
	case "f":
		m.toggleFollow()
	default:
		return m.paneKey(msg), false
	}
	return nil, false
}

func (m *model) startSearch() tea.Cmd {
	m.searching, m.searchTarget = true, m.focus
	m.search.SetValue(m.filterText[m.focus])
	m.search.CursorEnd()
	return m.search.Focus()
}

func (m *model) toggleFollow() {
	m.follow = !m.follow
	if m.follow {
		m.jumpToLatest()
	}
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
		m.showDetailFromTop()
	}
}

func sessionHaystack(session trail.Session) string {
	return strings.ToLower(strings.Join([]string{
		string(session.CLI), session.Name, session.ID, session.Model, session.Agent, strings.Join(session.Subagents, " "),
		session.Branch, string(session.Liveness), activityText(session), string(session.Billing), session.Plan,
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
	m.revealSession(m.events[index].Session)
	m.selectSession(m.rowOf(m.events[index].Session))
	m.focus = eventsPane
}

func (m *model) revealSession(key string) {
	session := m.sessions[key]
	if !m.isChild(session) || m.expanded(session.Parent) {
		return
	}
	m.folded[session.Parent] = false
	m.reorderSessions()
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
	case "x":
		m.askDelete()
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
	case "space":
		m.toggleFold()
	}
}

func (m *model) toggleFold() {
	key := m.sessionKeyAt(m.sessionCursor)
	if key == "" {
		m.toggleEveryFold()
		return
	}
	if m.isChild(m.sessions[key]) {
		key = m.sessions[key].Parent
	}
	if len(m.children[key]) == 0 {
		return
	}
	m.folded[key] = m.expanded(key)
	m.reorderSessions()
	m.selectSession(m.rowOf(key))
}

func (m *model) toggleEveryFold() {
	foldAll := slices.ContainsFunc(m.parentsWithAgents(), m.expanded)
	for _, key := range m.parentsWithAgents() {
		m.folded[key] = foldAll
	}
	m.reorderSessions()
}

func (m model) parentsWithAgents() []string {
	keys := make([]string, 0, len(m.children))
	for key := range m.children {
		keys = append(keys, key)
	}
	return keys
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

func (m *model) askDelete() {
	session, ok := m.sessions[m.sessionKeyAt(m.sessionCursor)]
	switch {
	case !ok:
		m.notice = "pick a session to delete"
	case m.isChild(session):
		m.notice = "agents are deleted with their parent session; select the parent"
	case session.Liveness == trail.LivenessRunning:
		m.notice = trail.ErrSessionRunning.Error()
	default:
		m.confirmDelete = session.Key
	}
}

func (m *model) confirmKey(key string) tea.Cmd {
	target := m.confirmDelete
	m.confirmDelete = ""
	if key != "y" {
		m.notice = "delete cancelled"
		return nil
	}
	follower, label := m.follower, sessionLabel(m.sessions[target])
	return func() tea.Msg {
		return deletedMsg{key: target, label: label, err: follower.Delete(target)}
	}
}

func (m *model) finishDelete(msg deletedMsg) {
	if msg.err != nil {
		m.notice = "not deleted: " + msg.err.Error()
		return
	}
	m.notice = "deleted " + msg.label
	m.forgetSession(msg.key)
}

func (m *model) forgetSession(key string) {
	m.revision++
	family := m.family(key)
	for _, member := range family {
		delete(m.sessions, member)
	}
	if slices.Contains(family, m.activeSession) {
		m.activeSession = ""
	}
	m.dropEvents(func(session string) bool { return slices.Contains(family, session) })
	m.reorderSessions()
}

func (m *model) dropEvents(drop func(session string) bool) {
	m.revision++
	previous, selected, kept := m.selectedEvent(), noEvent, 0
	for i, event := range m.events {
		if drop(event.Session) {
			continue
		}
		if i == previous {
			selected = kept
		}
		m.events[kept], m.haystack[kept] = event, m.haystack[i]
		kept++
	}
	m.events, m.haystack = m.events[:kept], m.haystack[:kept]
	m.detailRequested, m.detailShown, m.detailData = noEvent, noEvent, trail.Detail{}
	m.refilterAround(selected)
	m.refreshDetail()
}
