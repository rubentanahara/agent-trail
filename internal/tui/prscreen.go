package tui

import (
	"context"
	"errors"
	"os"
	"slices"
	"strconv"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/rubentanahara/agent-trail/internal/gh"
	"github.com/rubentanahara/agent-trail/internal/trail"
)

const (
	prPollEvery    = 30 * time.Second
	prStaleAfter   = 90 * time.Second
	prRateLimitFor = 5 * time.Minute
	prMaxBackoff   = 2
	prLoadingAfter = 150 * time.Millisecond
)

type prTab int

const (
	overviewTab prTab = iota
	checksTab
	commentsTab
	diffTab
	sessionsTab
	prTabCount
)

type prFocus int

const (
	prListFocus prFocus = iota
	prDetailFocus
)

type prMessage interface{ isPRMessage() }

type prStartMsg struct{}

type prListMsg struct {
	seq, generation int
	at              time.Time
	open, requested []gh.PR
	viewer          string
	repo            gh.Repo
	retryAt         time.Time
	err             error
}

type prStamp struct {
	updatedAt time.Time
	head      string
}

type prDetailMsg struct {
	seq, generation int
	number          int
	stamp           prStamp
	pr              gh.PR
	err             error
}

type prThreadsMsg struct {
	seq, generation int
	number          int
	head            string
	threads         gh.Threads
	err             error
}

type prDiffMsg struct {
	seq, generation int
	number          int
	head            string
	files           []gh.FileDiff
	tooLarge        bool
	err             error
}

type prOpenedMsg struct{ err error }

func (prStartMsg) isPRMessage()   {}
func (prListMsg) isPRMessage()    {}
func (prDetailMsg) isPRMessage()  {}
func (prThreadsMsg) isPRMessage() {}
func (prDiffMsg) isPRMessage()    {}
func (prOpenedMsg) isPRMessage()  {}

type slot struct {
	seq  int
	busy bool
}

func (s *slot) begin() int {
	s.seq++
	s.busy = true
	return s.seq
}

func (s *slot) invalidate() {
	s.seq++
	s.busy = false
}

func (s *slot) accepts(seq int) bool { return seq == s.seq }

type prRow struct {
	header string
	number int
}

type prDiffData struct {
	head     string
	files    []gh.FileDiff
	tooLarge bool
}

type prThreadData struct {
	head    string
	threads gh.Threads
}

type prScreen struct {
	shown            bool
	openedAt         time.Time
	detailFilterText string
	detailFilter     query
	run              gh.Runner
	open             func(url string) error

	ctx    context.Context
	cancel context.CancelFunc

	viewer string
	repo   gh.Repo

	sections gh.Sections
	byNumber map[int]gh.PR
	requests map[int]bool
	rows     []prRow
	cursor   int
	top      int

	wantSession string
	confirm     *prConfirm
	log         *prLog
	mergeAfter  int
	quitArmed   bool
	inFlight    map[string]bool
	mergeRun    gh.Runner
	focus       prFocus
	tab         prTab
	zoomed      bool

	filterText string
	filter     query
	searching  bool

	generation int
	list       slot
	detailSlot slot
	threadSlot slot
	diffSlot   slot
	logSlot    slot

	bodyMarkdown *markdownCache

	details map[int]gh.PR
	stamps  map[int]prStamp
	threads map[int]prThreadData
	diffs   map[int]prDiffData

	tabCursor [prTabCount]int
	tabTop    [prTabCount]int

	loaded     bool
	problem    gh.Problem
	failure    error
	failures   int
	lastFetch  time.Time
	lastOK     time.Time
	pausedTill time.Time
}

func newPRScreen(repo string) prScreen {
	return prScreen{
		run:          gh.Exec(repo),
		open:         openInBrowser,
		details:      map[int]gh.PR{},
		stamps:       map[int]prStamp{},
		inFlight:     map[string]bool{},
		mergeRun:     gh.Exec(os.TempDir()),
		threads:      map[int]prThreadData{},
		diffs:        map[int]prDiffData{},
		byNumber:     map[int]gh.PR{},
		requests:     map[int]bool{},
		problem:      -1,
		bodyMarkdown: &markdownCache{},
	}
}

func (p *prScreen) readContext() context.Context {
	if p.ctx == nil {
		p.ctx, p.cancel = context.WithCancel(context.Background())
	}
	return p.ctx
}

func (p *prScreen) stopReads() {
	if p.cancel != nil {
		p.cancel()
	}
	p.ctx, p.cancel = nil, nil
	p.list.invalidate()
	p.detailSlot.invalidate()
	p.threadSlot.invalidate()
	p.diffSlot.invalidate()
}

func (p prScreen) hasProblem() bool { return p.problem >= 0 }

func (p prScreen) selectedNumber() int {
	if p.cursor < 0 || p.cursor >= len(p.rows) {
		return 0
	}
	return p.rows[p.cursor].number
}

func (p prScreen) selected() (gh.PR, bool) {
	number := p.selectedNumber()
	if number == 0 {
		return gh.PR{}, false
	}
	if detail, ok := p.details[number]; ok {
		return detail, true
	}
	pr, ok := p.byNumber[number]
	return pr, ok
}

func (p prScreen) pendingCount() int {
	if threads, ok := p.threads[p.selectedNumber()]; ok && threads.threads.Pending != nil {
		return threads.threads.Pending.Comments
	}
	return 0
}

func (p prScreen) pollDelay() time.Duration {
	if p.failures == 0 {
		return prPollEvery
	}
	return prPollEvery << min(p.failures-1, prMaxBackoff)
}

func (p prScreen) rateLimited(now time.Time) bool { return now.Before(p.pausedTill) }

func (m model) prAvailable() bool { return m.context.Repo != trail.EveryRepository }

func (m *model) openPRs() tea.Cmd {
	if !m.prAvailable() {
		m.notice = "pull requests need a single repository; start agent-trail inside one"
		return nil
	}
	p := &m.pr
	p.shown, p.openedAt = true, m.now
	m.wantSelectedSessionPR()
	return tea.Batch(m.fetchPRs(), m.fetchTabData())
}

func (m *model) wantSelectedSessionPR() {
	key := m.sessionKeyAt(m.sessionCursor)
	m.pr.wantSession = key
	m.selectWantedSessionPR()
}

func (m *model) selectWantedSessionPR() {
	p := &m.pr
	session, ok := m.sessions[p.wantSession]
	if !ok {
		return
	}
	if target, found := m.prOfSession(session); found {
		m.selectPR(target.Number)
		p.wantSession = ""
	}
}

func (m *model) closePRs() {
	m.pr.shown, m.pr.searching, m.pr.focus = false, false, prListFocus
	m.pr.confirm, m.pr.log = nil, nil
	m.pr.stopReads()
}

func (m model) startPRs() tea.Cmd {
	return func() tea.Msg { return prStartMsg{} }
}

func (m *model) pollPRs() tea.Cmd {
	p := &m.pr
	if !p.shown || p.list.busy || p.rateLimited(m.now) || m.now.Sub(p.lastFetch) < p.pollDelay() {
		return nil
	}
	return tea.Batch(m.fetchPRs(), m.fetchTabData())
}

func (m *model) fetchPRs() tea.Cmd {
	p := &m.pr
	if !m.prAvailable() || p.list.busy {
		return nil
	}
	seq := p.list.begin()
	p.lastFetch = m.now
	return fetchPRList(p.readContext(), p.run, seq, p.generation, m.now, p.viewer, p.repo)
}

func fetchPRList(ctx context.Context, run gh.Runner, seq, generation int, at time.Time, viewer string, repo gh.Repo) tea.Cmd {
	return func() tea.Msg {
		msg := prListMsg{seq: seq, generation: generation, at: at, viewer: viewer, repo: repo}
		readCtx, cancel := context.WithTimeout(ctx, gh.ReadTimeout)
		defer cancel()
		msg.err = loadPRList(readCtx, run, &msg)
		if gh.ProblemOf(msg.err) == gh.RateLimited {
			msg.retryAt, _ = gh.ResetTime(readCtx, run)
		}
		return msg
	}
}

func loadPRList(ctx context.Context, run gh.Runner, msg *prListMsg) error {
	var err error
	if msg.repo.FullName == "" {
		if msg.repo, err = gh.LoadRepo(ctx, run); err != nil {
			return err
		}
	}
	if msg.viewer == "" {
		if msg.viewer, err = gh.Viewer(ctx, run); err != nil {
			return err
		}
	}
	if msg.open, err = gh.List(ctx, run); err != nil {
		return err
	}
	msg.requested, err = gh.ListReviewRequested(ctx, run)
	return err
}

func (m *model) absorbPR(msg prMessage) tea.Cmd {
	switch msg := msg.(type) {
	case prStartMsg:
		return m.fetchPRs()
	case prListMsg:
		return m.absorbPRList(msg)
	case prDetailMsg:
		return m.absorbPRDetail(msg)
	case prThreadsMsg:
		return m.absorbPRThreads(msg)
	case prDiffMsg:
		return m.absorbPRDiff(msg)
	case prWriteMsg:
		return m.absorbWrite(msg)
	case composedMsg:
		return m.absorbComposed(msg)
	case prLogMsg:
		m.absorbLog(msg)
	case prOpenedMsg:
		if msg.err != nil {
			m.notice = "could not open the browser: " + msg.err.Error()
		}
	}
	return nil
}

func (m *model) absorbPRList(msg prListMsg) tea.Cmd {
	p := &m.pr
	if !p.list.accepts(msg.seq) || msg.generation != p.generation {
		return nil
	}
	p.list.busy = false
	if msg.err != nil {
		m.failPRList(msg)
		return nil
	}
	p.failures, p.problem, p.failure, p.loaded = 0, -1, nil, true
	p.lastOK = msg.at
	p.viewer, p.repo = msg.viewer, msg.repo
	p.sections = gh.Partition(msg.open, msg.requested, msg.viewer)
	p.index(msg.open, msg.requested)
	p.rebuildRows()
	m.selectWantedSessionPR()
	return tea.Batch(m.fetchDetail(true), m.fetchTabData())
}

func (m *model) failPRList(msg prListMsg) {
	p := &m.pr
	p.failures++
	p.failure = msg.err
	p.problem = gh.ProblemOf(msg.err)
	if p.problem == gh.RateLimited {
		p.pausedTill = msg.at.Add(prRateLimitFor)
		if msg.retryAt.After(msg.at) {
			p.pausedTill = msg.retryAt
		}
	}
	if errors.Is(msg.err, context.Canceled) {
		p.problem, p.failure = -1, nil
	}
}

func (p *prScreen) index(open, requested []gh.PR) {
	p.byNumber = map[int]gh.PR{}
	for _, pr := range open {
		p.byNumber[pr.Number] = pr
	}
	p.requests = map[int]bool{}
	for _, pr := range requested {
		p.requests[pr.Number] = true
	}
}

func (p *prScreen) rebuildRows() {
	selected := p.selectedNumber()
	p.rows = p.rows[:0]
	p.addSection("Needs my review", p.sections.NeedsReview)
	p.addSection("My PRs", p.sections.Mine)
	p.addSection("All open", p.sections.All)
	p.cursor = p.rowOf(selected)
	if p.cursor < 0 {
		p.cursor = p.firstPR()
	}
}

func (p *prScreen) addSection(title string, prs []gh.PR) {
	var matching []gh.PR
	for _, pr := range prs {
		if p.filter.matches(prHaystack(pr)) {
			matching = append(matching, pr)
		}
	}
	if len(matching) == 0 {
		return
	}
	p.rows = append(p.rows, prRow{header: title + " (" + countText(len(matching)) + ")"})
	for _, pr := range matching {
		p.rows = append(p.rows, prRow{number: pr.Number})
	}
}

func countText(count int) string {
	if count >= gh.ListLimit {
		return strconv.Itoa(gh.ListLimit) + "+"
	}
	return strconv.Itoa(count)
}

func prHaystack(pr gh.PR) string {
	return "#" + strconv.Itoa(pr.Number) + " " + pr.Title + " " + pr.Author.Login + " " + pr.HeadRef
}

func (p prScreen) rowOf(number int) int {
	if number == 0 {
		return -1
	}
	return slices.IndexFunc(p.rows, func(row prRow) bool { return row.number == number })
}

func (p prScreen) firstPR() int {
	return slices.IndexFunc(p.rows, func(row prRow) bool { return row.number != 0 })
}

func (m *model) selectPR(number int) {
	if row := m.pr.rowOf(number); row >= 0 {
		m.pr.cursor = row
	}
}

func (m *model) movePR(delta int) tea.Cmd {
	p := &m.pr
	next := p.cursor
	for step := 0; step < len(p.rows); step++ {
		candidate := next + sign(delta)
		if candidate < 0 || candidate >= len(p.rows) {
			break
		}
		next = candidate
		if p.rows[next].number != 0 && abs(delta) <= 1 {
			break
		}
		if abs(delta) > 1 && step+1 >= abs(delta) {
			break
		}
	}
	if p.rows[next].number == 0 {
		return nil
	}
	p.cursor = next
	return tea.Batch(m.fetchDetail(false), m.fetchTabData())
}

func sign(n int) int {
	if n < 0 {
		return -1
	}
	return 1
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

func (p prScreen) detailNeeded(number int, onPoll bool) bool {
	listed, ok := p.byNumber[number]
	if !ok {
		return false
	}
	cached, have := p.details[number]
	switch {
	case !have:
		return true
	case p.stamps[number] != stampOf(listed):
		return true
	}
	return onPoll && cached.Pending()
}

func (m *model) fetchDetail(onPoll bool) tea.Cmd {
	p := &m.pr
	number := p.selectedNumber()
	if p.detailSlot.busy || !p.detailNeeded(number, onPoll) {
		return nil
	}
	seq := p.detailSlot.begin()
	return fetchPRDetail(p.readContext(), p.run, seq, p.generation, number, stampOf(p.byNumber[number]))
}

func stampOf(pr gh.PR) prStamp { return prStamp{updatedAt: pr.UpdatedAt, head: pr.HeadSHA} }

func fetchPRDetail(ctx context.Context, run gh.Runner, seq, generation, number int, stamp prStamp) tea.Cmd {
	return func() tea.Msg {
		readCtx, cancel := context.WithTimeout(ctx, gh.ReadTimeout)
		defer cancel()
		pr, err := gh.View(readCtx, run, number)
		return prDetailMsg{seq: seq, generation: generation, number: number, stamp: stamp, pr: pr, err: err}
	}
}

func (m *model) absorbPRDetail(msg prDetailMsg) tea.Cmd {
	p := &m.pr
	if !p.detailSlot.accepts(msg.seq) || msg.generation != p.generation {
		return nil
	}
	p.detailSlot.busy = false
	if msg.err != nil {
		m.noticeReadFailure("pull request #"+strconv.Itoa(msg.number), msg.err)
		return nil
	}
	p.details[msg.number], p.stamps[msg.number] = msg.pr, msg.stamp
	m.openMergeDialog()
	return tea.Batch(m.fetchDetail(false), m.fetchTabData())
}

func (m *model) fetchTabData() tea.Cmd {
	switch m.pr.tab {
	case commentsTab:
		return m.fetchThreads()
	case diffTab:
		return tea.Batch(m.fetchDiff(), m.fetchThreads())
	}
	return nil
}

func (m *model) fetchThreads() tea.Cmd {
	p := &m.pr
	number := p.selectedNumber()
	pr, ok := p.selected()
	if !ok || p.threadSlot.busy || p.repo.Name == "" {
		return nil
	}
	if cached, have := p.threads[number]; have && cached.head == pr.HeadSHA {
		return nil
	}
	seq := p.threadSlot.begin()
	return fetchPRThreads(p.readContext(), p.run, seq, p.generation, p.repo, number, pr.HeadSHA)
}

func fetchPRThreads(ctx context.Context, run gh.Runner, seq, generation int, repo gh.Repo, number int, head string) tea.Cmd {
	return func() tea.Msg {
		readCtx, cancel := context.WithTimeout(ctx, gh.ReadTimeout)
		defer cancel()
		threads, err := gh.LoadThreads(readCtx, run, repo.Owner, repo.Name, number)
		return prThreadsMsg{seq: seq, generation: generation, number: number, head: head, threads: threads, err: err}
	}
}

func (m *model) absorbPRThreads(msg prThreadsMsg) tea.Cmd {
	p := &m.pr
	if !p.threadSlot.accepts(msg.seq) || msg.generation != p.generation {
		return nil
	}
	p.threadSlot.busy = false
	if msg.err != nil {
		m.noticeReadFailure("review threads", msg.err)
		return nil
	}
	p.threads[msg.number] = prThreadData{head: msg.head, threads: msg.threads}
	return m.fetchTabData()
}

func (m *model) fetchDiff() tea.Cmd {
	p := &m.pr
	number := p.selectedNumber()
	pr, ok := p.selected()
	if !ok || p.diffSlot.busy {
		return nil
	}
	if cached, have := p.diffs[number]; have && cached.head == pr.HeadSHA {
		return nil
	}
	seq := p.diffSlot.begin()
	return fetchPRDiff(p.readContext(), p.run, seq, p.generation, number, pr.HeadSHA)
}

func fetchPRDiff(ctx context.Context, run gh.Runner, seq, generation, number int, head string) tea.Cmd {
	return func() tea.Msg {
		readCtx, cancel := context.WithTimeout(ctx, gh.ReadTimeout)
		defer cancel()
		out, err := run(readCtx, gh.DiffArgs(number))
		msg := prDiffMsg{seq: seq, generation: generation, number: number, head: head}
		switch {
		case gh.IsDiffTooLarge(err):
			msg.tooLarge = true
		case err != nil:
			msg.err = err
		default:
			msg.files = gh.SplitDiff(string(out))
		}
		return msg
	}
}

func (m *model) absorbPRDiff(msg prDiffMsg) tea.Cmd {
	p := &m.pr
	if !p.diffSlot.accepts(msg.seq) || msg.generation != p.generation {
		return nil
	}
	p.diffSlot.busy = false
	if msg.err != nil {
		m.noticeReadFailure("the diff", msg.err)
		return nil
	}
	p.diffs[msg.number] = prDiffData{head: msg.head, files: msg.files, tooLarge: msg.tooLarge}
	return m.fetchTabData()
}

func (m *model) refreshPRs() tea.Cmd {
	p := &m.pr
	number := p.selectedNumber()
	delete(p.threads, number)
	delete(p.diffs, number)
	delete(p.details, number)
	return tea.Batch(m.fetchPRs(), m.fetchTabData())
}

func ghFailure(what string, err error) string {
	return "could not load " + what + ": " + sanitize(err.Error())
}

func (m *model) noticeReadFailure(what string, err error) {
	if !errors.Is(err, context.Canceled) {
		m.notice = ghFailure(what, err)
	}
}

func (p prScreen) focusedFilter() string {
	if p.focus == prDetailFocus {
		return p.detailFilterText
	}
	return p.filterText
}
