package tui

import (
	"context"
	"fmt"
	"os"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/rubentanahara/agent-trail/internal/gh"
)

type confirmKind int

const (
	confirmClose confirmKind = iota
	confirmMerge
	confirmReviewPick
	confirmReviewSend
	confirmRerun
	confirmDiscard
	confirmSubmitPick
)

type prConfirm struct {
	kind         confirmKind
	number       int
	title        string
	stamp        prStamp
	failing      bool
	pending      bool
	autoAllowed  bool
	methods      []gh.MergeMethod
	method       gh.MergeMethod
	deleteBranch bool
	auto         bool
	event        gh.ReviewEvent
	draft        string
	ref          gh.LogRef
	failedOnly   bool
	reviewID     string
}

type prWrite struct {
	key    string
	label  string
	number int
	args   []string
	draft  string
	merge  bool

	sequence func(ctx context.Context, run gh.Runner) error
}

type prWriteMsg struct {
	write prWrite
	err   error
}

func (prWriteMsg) isPRMessage() {}

var mergeKeys = map[string]gh.MergeMethod{"s": gh.MergeSquash, "m": gh.MergeCommit, "r": gh.MergeRebase}

var mergeMethodWords = map[gh.MergeMethod]string{gh.MergeSquash: "squash", gh.MergeCommit: "merge commit", gh.MergeRebase: "rebase"}

var reviewKeys = map[string]gh.ReviewEvent{"a": gh.ReviewApprove, "r": gh.ReviewRequestChanges, "c": gh.ReviewComment}

var reviewEventWords = map[gh.ReviewEvent]string{gh.ReviewApprove: "approve", gh.ReviewRequestChanges: "request changes", gh.ReviewComment: "comment"}

func (m *model) beginWrite(key string) bool {
	p := &m.pr
	if p.inFlight[key] {
		return false
	}
	if p.rateLimited(m.now) {
		m.notice = "rate limited · retry at " + p.pausedTill.Format(clockLayout) + " · writes are refused"
		return false
	}
	p.inFlight[key] = true
	return true
}

func (m *model) startWrite(write prWrite) tea.Cmd {
	if !m.beginWrite(write.key) {
		return nil
	}
	m.notice = write.label + "…"
	run := m.pr.run
	if write.merge {
		run = m.pr.mergeRun
	}
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), gh.WriteTimeout)
		defer cancel()
		return prWriteMsg{write: write, err: write.perform(ctx, run)}
	}
}

func (w prWrite) perform(ctx context.Context, run gh.Runner) error {
	if w.sequence != nil {
		return w.sequence(ctx, run)
	}
	_, err := run(ctx, w.args)
	return err
}

func (m *model) absorbWrite(msg prWriteMsg) tea.Cmd {
	write := msg.write
	delete(m.pr.inFlight, write.key)
	if msg.err != nil {
		m.notice = write.label + " failed: " + sanitize(msg.err.Error()) + draftNote(write.draft)
		return m.refetchAfterWrite(write.number)
	}
	discardDraft(write.draft)
	m.notice = write.label + " done"
	return m.refetchAfterWrite(write.number)
}

func draftNote(path string) string {
	if path == "" {
		return ""
	}
	return " · draft kept at " + path
}

func (m *model) refetchAfterWrite(number int) tea.Cmd {
	p := &m.pr
	_, hadThreads := p.threads[number]
	p.generation++
	p.invalidateReads()
	delete(p.details, number)
	delete(p.threads, number)
	delete(p.diffs, number)
	if !p.shown {
		return m.fetchPRs()
	}
	if hadThreads {
		return tea.Batch(m.fetchPRs(), m.fetchTabData(), m.fetchThreads())
	}
	return tea.Batch(m.fetchPRs(), m.fetchTabData())
}

func (p *prScreen) invalidateReads() {
	p.list.invalidate()
	p.detailSlot.invalidate()
	p.threadSlot.invalidate()
	p.diffSlot.invalidate()
}

func (m model) writeBlocksQuit(armed bool) bool {
	return len(m.pr.inFlight) > 0 && !armed
}

func (m *model) armQuit() {
	m.pr.quitArmed = true
	m.notice = "a write to GitHub is still running; press q again to quit anyway"
}

func (m *model) sendComposed(purpose composePurpose, body, path string) tea.Cmd {
	switch purpose.kind {
	case composeComment:
		return m.startWrite(prWrite{key: fmt.Sprintf("comment:%d", purpose.number), label: fmt.Sprintf("comment on #%d", purpose.number), number: purpose.number, args: gh.CommentArgs(purpose.number, path), draft: path})
	case composeReply:
		return m.startWrite(prWrite{key: "reply:" + purpose.threadID, label: "reply", number: purpose.number, args: gh.ReplyArgs(purpose.threadID, path), draft: path})
	case composeEditComment:
		return m.startWrite(m.editCommentWrite(purpose, path))
	case composeEditBody:
		return m.sendBodyEdit(purpose, body, path)
	case composeReview:
		m.pr.confirm = &prConfirm{kind: confirmReviewSend, number: purpose.number, event: purpose.event, draft: path}
		return nil
	}
	return m.sendInlineComposed(purpose, body, path)
}

func (m model) editCommentWrite(purpose composePurpose, path string) prWrite {
	args := gh.EditIssueCommentArgs(purpose.commentID, path)
	if purpose.inline {
		args = gh.EditReviewCommentArgs(purpose.commentID, path)
	}
	return prWrite{key: "editcomment:" + purpose.commentID, label: "edit comment", number: purpose.number, args: args, draft: path}
}

func (m *model) sendBodyEdit(purpose composePurpose, text, path string) tea.Cmd {
	title, body, _ := strings.Cut(text, "\n")
	title = strings.TrimSpace(title)
	if title == "" {
		discardDraft(path)
		m.notice = "the title cannot be empty, cancelled"
		return nil
	}
	if err := os.WriteFile(path, []byte(strings.TrimSpace(body)+"\n"), 0o600); err != nil {
		m.notice = "could not write the draft: " + err.Error()
		return nil
	}
	return m.startWrite(prWrite{key: fmt.Sprintf("edit:%d", purpose.number), label: fmt.Sprintf("edit #%d", purpose.number), number: purpose.number, args: gh.EditArgs(purpose.number, title, path), draft: path})
}

func (m *model) prActionKey(key string) (tea.Cmd, bool) {
	switch key {
	case "r":
		return m.replyOrComment(), true
	case "e":
		return m.editSelected(), true
	case "v":
		return m.toggleThread(), true
	case "X":
		m.askClose()
	case "M":
		return m.askMerge(), true
	case "A":
		m.askReview()
	case "E":
		m.askRerun()
	case "L":
		return m.openLog(), true
	case "S", "D", "c":
		return m.prReviewKey(key), true
	default:
		return nil, false
	}
	return nil, true
}

func (m *model) replyOrComment() tea.Cmd {
	pr, ok := m.pr.selected()
	if !ok || m.pr.tab != commentsTab {
		return nil
	}
	if thread, found := m.cursorThread(); found {
		return m.compose(composePurpose{kind: composeReply, number: pr.Number, threadID: thread.ID}, "")
	}
	return m.compose(composePurpose{kind: composeComment, number: pr.Number}, "")
}

func (m model) cursorThread() (gh.Thread, bool) {
	line, found := m.cursorLine()
	data, loaded := m.pr.threads[m.pr.selectedNumber()]
	if !found || !loaded || line.src != sourceThread || line.ref < 0 || line.ref >= len(data.threads.Threads) {
		return gh.Thread{}, false
	}
	return data.threads.Threads[line.ref], true
}

func (m *model) toggleThread() tea.Cmd {
	thread, found := m.cursorThread()
	if m.pr.tab != commentsTab || !found {
		m.notice = "select a review thread on the Comments tab"
		return nil
	}
	number := m.pr.selectedNumber()
	if thread.Resolved {
		return m.startWrite(prWrite{key: "resolve:" + thread.ID, label: "unresolve thread", number: number, args: gh.UnresolveArgs(thread.ID)})
	}
	return m.startWrite(prWrite{key: "resolve:" + thread.ID, label: "resolve thread", number: number, args: gh.ResolveArgs(thread.ID)})
}

func (m *model) editSelected() tea.Cmd {
	pr, ok := m.pr.selected()
	if !ok {
		return nil
	}
	if m.pr.tab == commentsTab {
		return m.editCursorComment(pr)
	}
	if pr.Author.Login != m.pr.viewer {
		m.notice = "you can only edit your own pull request; o opens it in the browser"
		return nil
	}
	return m.compose(composePurpose{kind: composeEditBody, number: pr.Number}, pr.Title+"\n\n"+pr.Body)
}

func (m *model) editCursorComment(pr gh.PR) tea.Cmd {
	line, found := m.cursorLine()
	if !found {
		return nil
	}
	if thread, isThread := m.cursorThread(); isThread && line.sub >= 0 && line.sub < len(thread.Comments) {
		comment := thread.Comments[line.sub]
		return m.composeEditOfOwn(comment.CanUpdate, composePurpose{kind: composeEditComment, number: pr.Number, commentID: comment.ID, inline: true}, comment.Body)
	}
	if line.src == sourceComment && line.ref >= 0 && line.ref < len(pr.Comments) {
		comment := pr.Comments[line.ref]
		return m.composeEditOfOwn(comment.CanEdit, composePurpose{kind: composeEditComment, number: pr.Number, commentID: comment.ID}, comment.Body)
	}
	m.notice = "select one of your comments to edit it"
	return nil
}

func (m *model) composeEditOfOwn(own bool, purpose composePurpose, seed string) tea.Cmd {
	if !own {
		m.notice = "you can only edit your own comments; o opens it in the browser"
		return nil
	}
	return m.compose(purpose, seed)
}

func (m *model) askClose() {
	pr, ok := m.pr.selected()
	if !ok {
		return
	}
	if pr.State != "OPEN" {
		m.notice = fmt.Sprintf("#%d is already %s", pr.Number, strings.ToLower(pr.State))
		return
	}
	m.pr.confirm = &prConfirm{kind: confirmClose, number: pr.Number, title: pr.Title, stamp: stampOf(m.pr.byNumber[pr.Number])}
}

func (m *model) askReview() {
	pr, ok := m.pr.selected()
	if !ok {
		return
	}
	m.pr.confirm = &prConfirm{kind: confirmReviewPick, number: pr.Number, title: pr.Title}
}

func (m *model) askRerun() {
	pr, ok := m.pr.selected()
	check, found := m.cursorCheck(pr)
	if !ok || !found {
		m.notice = "select a check on the Checks tab to re-run it"
		return
	}
	ref, isActions := gh.ParseCheckURL(check.URL)
	if !isActions {
		m.notice = "only GitHub Actions checks can be re-run; o opens this one in the browser"
		return
	}
	m.pr.confirm = &prConfirm{kind: confirmRerun, number: pr.Number, ref: ref, failedOnly: true, stamp: stampOf(m.pr.byNumber[pr.Number])}
}

func (m model) cursorCheck(pr gh.PR) (gh.Check, bool) {
	line, found := m.cursorLine()
	if !found || m.pr.tab != checksTab || line.ref < 0 || line.ref >= len(pr.Checks) {
		return gh.Check{}, false
	}
	return pr.Checks[line.ref], true
}

func (m *model) askMerge() tea.Cmd {
	pr, ok := m.pr.selected()
	if !ok {
		return nil
	}
	if pr.State != "OPEN" {
		m.notice = fmt.Sprintf("#%d is already %s", pr.Number, strings.ToLower(pr.State))
		return nil
	}
	if len(m.pr.repo.Methods()) == 0 {
		m.notice = "no merge method is available to you in this repository"
		return nil
	}
	m.pr.mergeAfter = pr.Number
	m.pr.stamps[pr.Number] = prStamp{}
	if cmd := m.fetchDetail(false); cmd != nil {
		return cmd
	}
	m.openMergeDialog()
	return nil
}

func (m *model) openMergeDialog() {
	p := &m.pr
	pr, ok := p.selected()
	if !ok || p.mergeAfter != pr.Number {
		return
	}
	p.mergeAfter = 0
	methods := p.repo.Methods()
	p.confirm = &prConfirm{
		kind: confirmMerge, number: pr.Number, title: pr.Title, stamp: stampOf(p.byNumber[pr.Number]),
		failing: pr.Failing(), pending: pr.Pending(), autoAllowed: p.repo.AllowAutoMerge,
		methods: methods, method: methods[0],
	}
}

func (m *model) prConfirmKey(key string) tea.Cmd {
	c := m.pr.confirm
	switch c.kind {
	case confirmMerge:
		return m.mergeKey(c, key)
	case confirmReviewPick:
		return m.reviewPickKey(c, key)
	case confirmRerun:
		return m.rerunKey(c, key)
	case confirmSubmitPick:
		return m.submitPickKey(c, key)
	}
	m.pr.confirm = nil
	if key != "y" {
		m.cancelConfirm(c)
		return nil
	}
	return m.runConfirmed(c)
}

func (m *model) cancelConfirm(c *prConfirm) {
	discardDraft(c.draft)
	m.notice = "cancelled"
}

func (m *model) runConfirmed(c *prConfirm) tea.Cmd {
	switch c.kind {
	case confirmClose:
		return m.startWrite(prWrite{key: fmt.Sprintf("close:%d", c.number), label: fmt.Sprintf("close #%d", c.number), number: c.number, args: gh.CloseArgs(c.number)})
	case confirmReviewSend:
		return m.startWrite(prWrite{key: fmt.Sprintf("review:%d", c.number), label: fmt.Sprintf("%s review on #%d", reviewEventWords[c.event], c.number), number: c.number, args: gh.ReviewArgs(c.number, c.event, c.draft), draft: c.draft})
	}
	return m.runReviewConfirmed(c)
}

func (m *model) mergeKey(c *prConfirm, key string) tea.Cmd {
	if method, isMethod := mergeKeys[key]; isMethod && slices.Contains(c.methods, method) {
		c.method = method
		return nil
	}
	switch {
	case key == "d":
		c.deleteBranch = !c.deleteBranch
	case key == "a" && c.pending && c.autoAllowed:
		c.auto = !c.auto
	case key == "y":
		m.pr.confirm = nil
		return m.startWrite(prWrite{
			key: fmt.Sprintf("merge:%d", c.number), label: fmt.Sprintf("merge #%d", c.number), number: c.number, merge: true,
			args: gh.MergeArgs(m.pr.repo.FullName, c.number, c.method, c.deleteBranch, c.auto),
		})
	default:
		m.pr.confirm = nil
		m.notice = "merge cancelled"
	}
	return nil
}

func (m *model) reviewPickKey(c *prConfirm, key string) tea.Cmd {
	m.pr.confirm = nil
	event, picked := reviewKeys[key]
	if !picked {
		m.notice = "review cancelled"
		return nil
	}
	return m.compose(composePurpose{kind: composeReview, number: c.number, event: event}, "")
}

func (m *model) rerunKey(c *prConfirm, key string) tea.Cmd {
	switch key {
	case "a":
		c.failedOnly = false
	case "y":
		m.pr.confirm = nil
		return m.startWrite(prWrite{key: "rerun:" + c.ref.RunID, label: "re-run of run " + c.ref.RunID, number: c.number, args: gh.RerunArgs(c.ref.RunID, c.failedOnly)})
	default:
		m.pr.confirm = nil
		m.notice = "re-run cancelled"
	}
	return nil
}

func (m model) confirmPrompt() string {
	c := m.pr.confirm
	prompt := m.confirmBody(c)
	if c.kind != confirmMerge && c.stamp != (prStamp{}) && stampOf(m.pr.byNumber[c.number]) != c.stamp {
		prompt += " · PR updated since you opened this"
	}
	return prompt
}

func (m model) confirmBody(c *prConfirm) string {
	switch c.kind {
	case confirmClose:
		return fmt.Sprintf("Close #%d %q? y closes · any other key cancels", c.number, sanitize(c.title))
	case confirmMerge:
		return m.mergePrompt(c)
	case confirmReviewPick:
		return fmt.Sprintf("Review #%d: a approve · r request changes · c comment · any other key cancels", c.number)
	case confirmReviewSend:
		return fmt.Sprintf("Submit a %s review on #%d? y sends · any other key discards the draft", reviewEventWords[c.event], c.number)
	case confirmRerun:
		return rerunPrompt(c)
	}
	return m.reviewConfirmPrompt(c)
}

func rerunPrompt(c *prConfirm) string {
	if c.failedOnly {
		return fmt.Sprintf("Re-run the FAILED jobs of run %s? y confirms · a re-runs all jobs instead · any other key cancels", c.ref.RunID)
	}
	return fmt.Sprintf("Re-run ALL jobs of run %s? y confirms · any other key cancels", c.ref.RunID)
}

func (m model) mergePrompt(c *prConfirm) string {
	var options []string
	for _, method := range c.methods {
		word := mergeMethodWords[method]
		if method == c.method {
			word = "[" + word + "]"
		}
		options = append(options, word)
	}
	parts := []string{fmt.Sprintf("Merge #%d %q", c.number, sanitize(c.title)), strings.Join(options, " "), "d: " + branchWords(c.deleteBranch)}
	if c.pending && c.autoAllowed {
		parts = append(parts, "a: auto-merge "+onOff(c.auto))
	}
	if c.failing {
		parts = append(parts, "⚠ checks are failing")
	}
	if current := m.pr.byNumber[c.number]; stampOf(current) != c.stamp {
		parts = append(parts, "PR updated since you opened this")
	}
	return strings.Join(parts, " · ") + " · y merges · any other key cancels"
}

func branchWords(deleted bool) string {
	if deleted {
		return "branch deleted on GitHub"
	}
	return "branch kept"
}

func onOff(on bool) string {
	if on {
		return "on"
	}
	return "off"
}
