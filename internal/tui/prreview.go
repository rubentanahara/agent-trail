package tui

import (
	"context"
	"fmt"

	tea "charm.land/bubbletea/v2"

	"github.com/rubentanahara/agent-trail/internal/gh"
)

const (
	pendingMark      = "✎"
	rightSideOnly    = "comments go on added or unchanged lines; move to one in the Diff tab"
	noPendingNotice  = "no pending review; c on a diff line starts one"
	headMovedNotice  = "the pull request has new commits; the diff is reloading, then press c again"
	diffOnlyNotice   = "c comments on a line of the Diff tab"
	submitKeysPrompt = "a approve · r request changes · c comment · any other key cancels"
)

var submitKeys = map[string]gh.SubmitEvent{"a": gh.SubmitApprove, "r": gh.SubmitRequestChanges, "c": gh.SubmitComment}

var submitWords = map[gh.SubmitEvent]string{gh.SubmitApprove: "approve", gh.SubmitRequestChanges: "request changes", gh.SubmitComment: "comment"}

func (m *model) prReviewKey(key string) tea.Cmd {
	switch key {
	case "c":
		return m.askLineComment()
	case "S":
		m.askSubmit()
	case "D":
		m.askDiscard()
	}
	return nil
}

func (m model) pendingReview() (*gh.PendingReview, bool) {
	data, loaded := m.pr.threads[m.pr.selectedNumber()]
	if !loaded || data.threads.Pending == nil {
		return nil, false
	}
	return data.threads.Pending, true
}

func (m *model) askLineComment() tea.Cmd {
	p := &m.pr
	pr, ok := p.selected()
	if !ok {
		return nil
	}
	if p.tab != diffTab {
		m.notice = diffOnlyNotice
		return nil
	}
	line, found := m.cursorLine()
	if !found || line.kind != diffBodyLine || line.newLine <= 0 {
		m.notice = rightSideOnly
		return nil
	}
	data, loaded := p.diffs[pr.Number]
	if !loaded || data.head != p.byNumber[pr.Number].HeadSHA {
		m.notice = headMovedNotice
		delete(p.diffs, pr.Number)
		return m.fetchTabData()
	}
	path := data.files[line.file].Path
	return m.compose(composePurpose{kind: composeLine, number: pr.Number, path: path, line: line.newLine, headSHA: data.head}, "")
}

func (m *model) sendInlineComposed(purpose composePurpose, _ string, path string) tea.Cmd {
	switch purpose.kind {
	case composeLine:
		return m.startWrite(m.lineCommentWrite(purpose, path))
	case composeSubmit:
		return m.startWrite(m.submitWrite(purpose, path))
	}
	return nil
}

func (m model) lineCommentWrite(purpose composePurpose, bodyFile string) prWrite {
	repo, pullRequestID := m.pr.repo, m.pr.details[purpose.number].ID
	return prWrite{
		key: fmt.Sprintf("line:%d:%s:%d", purpose.number, purpose.path, purpose.line), number: purpose.number, draft: bodyFile,
		label: fmt.Sprintf("comment on %s:%d", purpose.path, purpose.line),
		sequence: func(ctx context.Context, run gh.Runner) error {
			return gh.AddLineComment(ctx, run, repo, purpose.number, pullRequestID, purpose.headSHA, purpose.path, purpose.line, bodyFile)
		},
	}
}

func (m model) submitWrite(purpose composePurpose, bodyFile string) prWrite {
	review, _ := m.pendingReview()
	reviewID := ""
	if review != nil {
		reviewID = review.ID
	}
	return prWrite{
		key: "submit:" + reviewID, number: purpose.number, draft: bodyFile,
		label: fmt.Sprintf("%s review on #%d", submitWords[purpose.submit], purpose.number),
		args:  gh.SubmitReviewArgs(reviewID, purpose.submit, bodyFile),
	}
}

func (m *model) askSubmit() {
	pr, ok := m.pr.selected()
	review, pending := m.pendingReview()
	if !ok || !pending {
		m.notice = noPendingNotice
		return
	}
	m.pr.confirm = &prConfirm{kind: confirmSubmitPick, number: pr.Number, reviewID: review.ID}
}

func (m *model) submitPickKey(c *prConfirm, key string) tea.Cmd {
	m.pr.confirm = nil
	event, picked := submitKeys[key]
	if !picked {
		m.notice = "submit cancelled"
		return nil
	}
	return m.compose(composePurpose{kind: composeSubmit, number: c.number, submit: event}, "")
}

func (m *model) askDiscard() {
	pr, ok := m.pr.selected()
	review, pending := m.pendingReview()
	if !ok || !pending {
		m.notice = noPendingNotice
		return
	}
	m.pr.confirm = &prConfirm{kind: confirmDiscard, number: pr.Number, reviewID: review.ID, stamp: stampOf(m.pr.byNumber[pr.Number])}
}

func (m *model) runReviewConfirmed(c *prConfirm) tea.Cmd {
	if c.kind != confirmDiscard {
		return nil
	}
	return m.startWrite(prWrite{key: "discard:" + c.reviewID, label: fmt.Sprintf("discard pending review on #%d", c.number), number: c.number, args: gh.DiscardReviewArgs(c.reviewID)})
}

func (m model) reviewConfirmPrompt(c *prConfirm) string {
	switch c.kind {
	case confirmSubmitPick:
		return fmt.Sprintf("Submit your pending review (%d comments) on #%d: %s", m.pr.pendingCount(), c.number, submitKeysPrompt)
	case confirmDiscard:
		return fmt.Sprintf("Discard your pending review (%d comments) on #%d? y discards · any other key cancels", m.pr.pendingCount(), c.number)
	}
	return ""
}

func (m model) pendingLines(pr gh.PR) map[string]bool {
	marked := map[string]bool{}
	data, loaded := m.pr.threads[pr.Number]
	if !loaded {
		return marked
	}
	for _, thread := range data.threads.Threads {
		if len(thread.Comments) > 0 && thread.Comments[0].Pending {
			marked[fmt.Sprintf("%s:%d", thread.Path, thread.Line)] = true
		}
	}
	return marked
}

func (m *model) remindPending() {
	if count := m.pr.pendingCount(); count > 0 {
		m.notice = fmt.Sprintf("Pending (%d) kept. S submits.", count)
	}
}
