package tui

import (
	"context"
	"errors"
	"slices"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/rubentanahara/agent-trail/internal/gh"
)

type issueTab int

const (
	issueOverviewTab issueTab = iota
	issueCommentsTab
	issueLinkedTab
	issueTabCount
)

const (
	openIssuesView = "Open issues"
	noStatusColumn = "No status"
)

type issueMessage interface{ isIssueMessage() }

type issueBoardMsg struct {
	seq, generation int
	at              time.Time
	repo            gh.Repo
	board           gh.Board
	err             error
}

type issueDetailMsg struct {
	seq, generation int
	number          int
	stamp           time.Time
	detail          gh.IssueDetail
	err             error
}

func (issueBoardMsg) isIssueMessage()  {}
func (issueDetailMsg) isIssueMessage() {}

type issueRow struct {
	header string
	number int
}

type issueDetailData struct {
	stamp  time.Time
	detail gh.IssueDetail
}

type issueScreen struct {
	shown    bool
	openedAt time.Time
	run      gh.Runner
	repo     gh.Repo

	board   gh.Board
	loaded  bool
	failure error
	problem gh.Problem
	lastOK  time.Time

	view     int
	rows     []issueRow
	cursor   int
	top      int
	byNumber map[int]gh.Issue

	focus  prFocus
	tab    issueTab
	zoomed bool

	filterText string
	filter     query
	searching  bool

	generation int
	list       slot
	detailSlot slot
	ctx        context.Context
	cancel     context.CancelFunc

	details  map[int]issueDetailData
	markdown *markdownCache

	tabCursor [issueTabCount]int
	tabTop    [issueTabCount]int
}

func newIssueScreen(repo string) issueScreen {
	return issueScreen{
		run:      gh.Exec(repo),
		problem:  -1,
		byNumber: map[int]gh.Issue{},
		details:  map[int]issueDetailData{},
		markdown: &markdownCache{},
	}
}

func (s *issueScreen) readContext() context.Context {
	if s.ctx == nil {
		s.ctx, s.cancel = context.WithCancel(context.Background())
	}
	return s.ctx
}

func (s *issueScreen) stopReads() {
	if s.cancel != nil {
		s.cancel()
	}
	s.ctx, s.cancel = nil, nil
	s.list.invalidate()
	s.detailSlot.invalidate()
}

func (s issueScreen) hasProblem() bool { return s.problem >= 0 }

func (s issueScreen) viewCount() int { return len(s.board.Projects) + 1 }

func (s issueScreen) inProject() bool { return s.view < len(s.board.Projects) }

func (s issueScreen) viewTitle() string {
	if s.inProject() {
		return s.board.Projects[s.view].Title
	}
	return openIssuesView
}

func (s issueScreen) viewIssues() []gh.Issue {
	if s.inProject() {
		return s.board.Projects[s.view].Issues
	}
	return s.board.Open
}

func (s issueScreen) viewTotal() int {
	if s.inProject() {
		return s.board.Projects[s.view].Total
	}
	return s.board.OpenTotal
}

func (s issueScreen) selectedNumber() int {
	if s.cursor < 0 || s.cursor >= len(s.rows) {
		return 0
	}
	return s.rows[s.cursor].number
}

func (s issueScreen) selected() (gh.Issue, bool) {
	issue, ok := s.byNumber[s.selectedNumber()]
	return issue, ok
}

func (m model) issuesAvailable() bool { return m.prAvailable() }

func (m *model) openIssues() tea.Cmd {
	if !m.issuesAvailable() {
		m.notice = "issues need a single repository; start agent-trail inside one"
		return nil
	}
	m.issues.shown, m.issues.openedAt = true, m.now
	if m.issues.loaded {
		return m.fetchIssueDetail()
	}
	return m.fetchBoard()
}

func (m *model) closeIssues() {
	m.issues.shown, m.issues.searching, m.issues.focus = false, false, prListFocus
	m.issues.stopReads()
}

func (m *model) fetchBoard() tea.Cmd {
	s := &m.issues
	if s.list.busy {
		return nil
	}
	seq := s.list.begin()
	return fetchIssueBoard(s.readContext(), s.run, seq, s.generation, m.now, s.repo)
}

func fetchIssueBoard(ctx context.Context, run gh.Runner, seq, generation int, at time.Time, repo gh.Repo) tea.Cmd {
	return func() tea.Msg {
		msg := issueBoardMsg{seq: seq, generation: generation, at: at, repo: repo}
		readCtx, cancel := context.WithTimeout(ctx, gh.ReadTimeout)
		defer cancel()
		if msg.repo.FullName == "" {
			if msg.repo, msg.err = gh.LoadRepo(readCtx, run); msg.err != nil {
				return msg
			}
		}
		msg.board, msg.err = gh.LoadBoard(readCtx, run, msg.repo.Owner, msg.repo.Name)
		return msg
	}
}

func (m *model) absorbIssue(msg issueMessage) tea.Cmd {
	switch msg := msg.(type) {
	case issueBoardMsg:
		return m.absorbIssueBoard(msg)
	case issueDetailMsg:
		return m.absorbIssueDetail(msg)
	}
	return nil
}

func (m *model) absorbIssueBoard(msg issueBoardMsg) tea.Cmd {
	s := &m.issues
	if !s.list.accepts(msg.seq) || msg.generation != s.generation {
		return nil
	}
	s.list.busy = false
	if msg.err != nil {
		if !errors.Is(msg.err, context.Canceled) {
			s.failure, s.problem = msg.err, gh.ProblemOf(msg.err)
		}
		return nil
	}
	s.failure, s.problem, s.loaded, s.lastOK = nil, -1, true, msg.at
	s.repo, s.board = msg.repo, msg.board
	if s.view >= s.viewCount() {
		s.view = 0
	}
	s.rebuildRows()
	return m.fetchIssueDetail()
}

func (s *issueScreen) rebuildRows() {
	selected := s.selectedNumber()
	s.rows = s.rows[:0]
	s.byNumber = map[int]gh.Issue{}
	var matching []gh.Issue
	for _, issue := range s.viewIssues() {
		if s.filter.matches(issueHaystack(issue)) {
			matching = append(matching, issue)
			s.byNumber[issue.Number] = issue
		}
	}
	if s.inProject() {
		s.addColumns(matching)
	} else if len(matching) > 0 {
		s.addGroup(openIssuesView, matching)
	}
	s.cursor = slices.IndexFunc(s.rows, func(row issueRow) bool { return selected != 0 && row.number == selected })
	if s.cursor < 0 {
		s.cursor = slices.IndexFunc(s.rows, func(row issueRow) bool { return row.number != 0 })
	}
}

func (s *issueScreen) addColumns(issues []gh.Issue) {
	columns := slices.Clone(s.board.Projects[s.view].Columns)
	for _, issue := range issues {
		if status := columnOf(issue); status != noStatusColumn && !slices.Contains(columns, status) {
			columns = append(columns, status)
		}
	}
	columns = append(columns, noStatusColumn)
	for _, column := range columns {
		var inColumn []gh.Issue
		for _, issue := range issues {
			if columnOf(issue) == column {
				inColumn = append(inColumn, issue)
			}
		}
		s.addGroup(column, inColumn)
	}
}

func (s *issueScreen) addGroup(title string, issues []gh.Issue) {
	if len(issues) == 0 {
		return
	}
	s.rows = append(s.rows, issueRow{header: title + " (" + strconv.Itoa(len(issues)) + ")"})
	for _, issue := range issues {
		s.rows = append(s.rows, issueRow{number: issue.Number})
	}
}

func columnOf(issue gh.Issue) string {
	if status := issue.Field(gh.StatusField); status != "" {
		return status
	}
	return noStatusColumn
}

func issueHaystack(issue gh.Issue) string {
	parts := []string{"#" + strconv.Itoa(issue.Number), issue.Title, issue.Author, issue.State, issue.Milestone}
	parts = append(parts, issue.Labels...)
	parts = append(parts, issue.Assignees...)
	for _, field := range issue.Fields {
		parts = append(parts, field.Value)
	}
	return strings.ToLower(strings.Join(parts, " "))
}

func (m *model) fetchIssueDetail() tea.Cmd {
	s := &m.issues
	issue, ok := s.selected()
	if !ok || s.detailSlot.busy || s.repo.Name == "" {
		return nil
	}
	if cached, have := s.details[issue.Number]; have && cached.stamp.Equal(issue.UpdatedAt) {
		return nil
	}
	seq := s.detailSlot.begin()
	return fetchIssueDetail(s.readContext(), s.run, seq, s.generation, s.repo, issue)
}

func fetchIssueDetail(ctx context.Context, run gh.Runner, seq, generation int, repo gh.Repo, issue gh.Issue) tea.Cmd {
	return func() tea.Msg {
		readCtx, cancel := context.WithTimeout(ctx, gh.ReadTimeout)
		defer cancel()
		detail, err := gh.LoadIssueDetail(readCtx, run, repo.Owner, repo.Name, issue.Number)
		return issueDetailMsg{seq: seq, generation: generation, number: issue.Number, stamp: issue.UpdatedAt, detail: detail, err: err}
	}
}

func (m *model) absorbIssueDetail(msg issueDetailMsg) tea.Cmd {
	s := &m.issues
	if !s.detailSlot.accepts(msg.seq) || msg.generation != s.generation {
		return nil
	}
	s.detailSlot.busy = false
	if msg.err != nil {
		m.noticeReadFailure("issue #"+strconv.Itoa(msg.number), msg.err)
		return nil
	}
	s.details[msg.number] = issueDetailData{stamp: msg.stamp, detail: msg.detail}
	return m.fetchIssueDetail()
}

func (m *model) refreshIssues() tea.Cmd {
	s := &m.issues
	s.generation++
	s.stopReads()
	s.details = map[int]issueDetailData{}
	return m.fetchBoard()
}

func (m *model) cycleIssueView() tea.Cmd {
	s := &m.issues
	s.view = (s.view + 1) % s.viewCount()
	s.cursor, s.top = 0, 0
	s.tabCursor, s.tabTop = [issueTabCount]int{}, [issueTabCount]int{}
	s.rebuildRows()
	return m.fetchIssueDetail()
}
