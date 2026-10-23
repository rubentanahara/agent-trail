package tui

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/rubentanahara/agent-trail/internal/gh"
)

const (
	firstThreadID = "PRRT_kwDODKw3uc6ut5yP"
	listCall      = "pr list --state=open --limit=100 --json"
)

func draftFile(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "draft.md")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func compose(m model, purpose composePurpose, path string) (model, tea.Cmd) {
	updated, cmd := m.Update(composedMsg{purpose: purpose, path: path})
	return updated.(model), cmd
}

func deliver(m model, cmd tea.Cmd) model {
	return settlePR(m, cmd)
}

func TestEditorCommandResolvesVisualThenEditorThenVi(t *testing.T) {
	t.Setenv("VISUAL", "code -w")
	t.Setenv("EDITOR", "nano")
	if got := editorCommand("/tmp/x").Args; !slices.Equal(got, []string{"code", "-w", "/tmp/x"}) {
		t.Fatalf("VISUAL: %v", got)
	}
	t.Setenv("VISUAL", "")
	if got := editorCommand("/tmp/x").Args; !slices.Equal(got, []string{"nano", "/tmp/x"}) {
		t.Fatalf("EDITOR: %v", got)
	}
	t.Setenv("EDITOR", "")
	if got := editorCommand("/tmp/x").Args; !slices.Equal(got, []string{"vi", "/tmp/x"}) {
		t.Fatalf("vi: %v", got)
	}
}

func TestEmptyDraftCancelsAndIsDeleted(t *testing.T) {
	fake := newFakeGH(t)
	m := openedPRs(t, fake)
	path := draftFile(t, "  \n\r\n ")

	m, cmd := compose(m, composePurpose{kind: composeComment, number: prForkPR}, path)

	if cmd != nil || !strings.Contains(m.notice, "cancelled") || exists(path) {
		t.Fatalf("cmd = %v, notice = %q, exists = %v", cmd != nil, m.notice, exists(path))
	}
}

func TestDraftIsNormalizedFromCRLF(t *testing.T) {
	path := draftFile(t, "one\r\ntwo\r\n")

	text, err := readDraft(path)

	if err != nil || text != "one\ntwo" {
		t.Fatalf("text = %q, err = %v", text, err)
	}
}

func TestCommentSendsTheBodyFileOnceAndRefetches(t *testing.T) {
	fake := newFakeGH(t)
	m := openedPRs(t, fake)
	path := draftFile(t, "looks good")
	args := gh.CommentArgs(prForkPR, path)
	fake.set(args, nil)
	generation := m.pr.generation
	lists := fake.count(listCall)

	m, cmd := compose(m, composePurpose{kind: composeComment, number: prForkPR}, path)
	_, second := compose(m, composePurpose{kind: composeComment, number: prForkPR}, path)
	if second != nil {
		t.Fatal("a second press while the write is pending must do nothing")
	}
	m = deliver(m, cmd)

	if fake.count("pr comment") != 1 {
		t.Fatalf("comment calls = %d", fake.count("pr comment"))
	}
	if m.pr.generation != generation+1 || fake.count(listCall) <= lists {
		t.Fatalf("generation %d -> %d, list calls %d -> %d", generation, m.pr.generation, lists, fake.count(listCall))
	}
	if exists(path) || !strings.Contains(m.notice, "done") {
		t.Fatalf("draft exists = %v, notice = %q", exists(path), m.notice)
	}
}

func TestFailedSendKeepsTheDraftAndShowsStderr(t *testing.T) {
	fake := newFakeGH(t)
	m := openedPRs(t, fake)
	path := draftFile(t, "hello")
	fake.fail(gh.CommentArgs(prForkPR, path), &gh.Error{Stderr: "HTTP 502: Bad Gateway", Err: errors.New("exit status 1")})

	m, cmd := compose(m, composePurpose{kind: composeComment, number: prForkPR}, path)
	m = deliver(m, cmd)

	if !exists(path) || !strings.Contains(m.notice, "HTTP 502") || !strings.Contains(m.notice, path) {
		t.Fatalf("draft exists = %v, notice = %q", exists(path), m.notice)
	}
	if len(m.pr.inFlight) != 0 {
		t.Fatal("a failed write must release its in-flight mark")
	}
}

func TestReplyGoesToTheThreadThroughGraphQL(t *testing.T) {
	fake := newFakeGH(t)
	m := openedPRs(t, fake)
	path := draftFile(t, "thanks")
	fake.set(gh.ReplyArgs(firstThreadID, path), nil)

	m, cmd := compose(m, composePurpose{kind: composeReply, number: prForkPR, threadID: firstThreadID}, path)
	deliver(m, cmd)

	if fake.count("api graphql -f query=mutation($threadId:ID!,$body:String!){addPullRequestReviewThreadReply") != 1 {
		t.Fatalf("calls = %v", fake.calls)
	}
}

func TestEditSendsTitleFlagAndBodyFileEvenWhenTitleStartsWithADash(t *testing.T) {
	fake := newFakeGH(t)
	m := openedPRs(t, fake)
	path := draftFile(t, "-rf new title\n\nnew body\n")
	fake.set(gh.EditArgs(prForkPR, "-rf new title", path), nil)

	m, cmd := compose(m, composePurpose{kind: composeEditBody, number: prForkPR}, path)
	deliver(m, cmd)

	if fake.count("pr edit 14783 --title=-rf new title --body-file=") != 1 {
		t.Fatalf("calls = %v", fake.calls)
	}
}

func TestEditIsUnavailableOnSomeoneElsesPullRequest(t *testing.T) {
	m := openedPRs(t, newFakeGH(t))

	m = pressPR(m, "e")

	if !strings.Contains(m.notice, "only edit your own") {
		t.Fatalf("notice = %q", m.notice)
	}
}

func threadHeaderRow(t *testing.T, m model) int {
	t.Helper()
	for i, line := range m.prTabLines() {
		if line.src == sourceThread && line.sub < 0 && line.ref == 0 {
			return i
		}
	}
	t.Fatal("no thread header on the Comments tab")
	return 0
}

func onThread(t *testing.T, m model) model {
	t.Helper()
	m = pressPR(m, "]", "]", "2")
	m.pr.tabCursor[commentsTab] = threadHeaderRow(t, m)
	return m
}

func TestVResolvesThenUnresolves(t *testing.T) {
	fake := newFakeGH(t)
	fake.set(gh.ResolveArgs(firstThreadID), nil)
	fake.set(gh.UnresolveArgs(firstThreadID), nil)
	m := onThread(t, openedPRs(t, fake))

	m = pressPR(m, "v")
	if fake.count("api graphql -f query=mutation($threadId:ID!){resolveReviewThread") != 1 {
		t.Fatalf("calls = %v", fake.calls)
	}
	data := m.pr.threads[prForkPR]
	data.threads.Threads[0].Resolved = true
	m.pr.threads[prForkPR] = data
	m.pr.tabCursor[commentsTab] = threadHeaderRow(t, m)
	pressPR(m, "v")
	if fake.count("api graphql -f query=mutation($threadId:ID!){unresolveReviewThread") != 1 {
		t.Fatalf("calls = %v", fake.calls)
	}
}

func TestCloseNeedsYAndAnyOtherKeyCancels(t *testing.T) {
	fake := newFakeGH(t)
	fake.set(gh.CloseArgs(prForkPR), nil)
	m := openedPRs(t, fake)

	cancelled := pressPR(m, "X", "n")
	if fake.count("pr close") != 0 || !strings.Contains(cancelled.notice, "cancel") {
		t.Fatalf("close called after a cancel: %v", fake.calls)
	}
	pressPR(m, "X", "y")
	if fake.count("pr close") != 1 {
		t.Fatalf("close calls = %d", fake.count("pr close"))
	}
}

func TestCheckoutNeedsYAndAnyOtherKeyCancels(t *testing.T) {
	fake := newFakeGH(t)
	fake.set(gh.CheckoutArgs(prForkPR), nil)
	m := openedPRs(t, fake)

	cancelled := pressPR(m, "C", "n")
	if fake.count("pr checkout") != 0 || !strings.Contains(cancelled.notice, "cancel") {
		t.Fatalf("checkout called after a cancel: %v", fake.calls)
	}
	pressPR(m, "C", "y")
	if fake.count("pr checkout") != 1 {
		t.Fatalf("checkout calls = %d", fake.count("pr checkout"))
	}
}

func TestConfirmationCapturesEveryKeyAndNamesThePullRequest(t *testing.T) {
	m := pressPR(openedPRs(t, newFakeGH(t)), "X")

	if m.pr.confirm == nil || !strings.Contains(screenText(m), "Close #14783") {
		t.Fatalf("footer:\n%s", screenText(m))
	}
	m = pressPR(m, "]")
	if m.pr.tab != overviewTab || m.pr.confirm != nil {
		t.Fatal("a key during confirmation must cancel, not navigate")
	}
}

func TestAlreadyClosedErrorResolvesThroughARefetch(t *testing.T) {
	fake := newFakeGH(t)
	fake.fail(gh.CloseArgs(prForkPR), &gh.Error{Stderr: "pull request is already closed", Err: errors.New("exit status 1")})
	m := openedPRs(t, fake)
	lists := fake.count(listCall)

	m = pressPR(m, "X", "y")

	if fake.count(listCall) <= lists || !strings.Contains(m.notice, "already closed") {
		t.Fatalf("lists %d -> %d, notice %q", lists, fake.count(listCall), m.notice)
	}
}

func viewWithRollup(t *testing.T, number int, rollup []map[string]any) []byte {
	t.Helper()
	var view map[string]any
	if err := json.Unmarshal(viewJSON(t, number), &view); err != nil {
		t.Fatal(err)
	}
	view["statusCheckRollup"] = rollup
	data, err := json.Marshal(view)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func mergeReady(t *testing.T, fake *fakeGH, mutate func(*model)) model {
	t.Helper()
	m := openedPRs(t, fake)
	m.pr.mergeRun = fake.run
	if mutate != nil {
		mutate(&m)
	}
	return m
}

func TestMergeDialogListsOnlyAllowedMethodsAndSendsRepoFromTheTempRunner(t *testing.T) {
	fake := newFakeGH(t)
	fake.set(gh.MergeArgs("rubentanahara/agent-trail", prForkPR, gh.MergeSquash, false, false), nil)
	m := mergeReady(t, fake, func(m *model) { m.pr.repo.AllowMerge, m.pr.repo.AllowRebase = false, false })

	m = pressPR(m, "M")
	prompt := m.confirmPrompt()
	if !strings.Contains(prompt, "squash") || strings.Contains(prompt, "rebase") || strings.Contains(prompt, "merge commit") {
		t.Fatalf("prompt = %q", prompt)
	}
	pressPR(m, "y")

	if fake.count("pr merge 14783 --repo=rubentanahara/agent-trail --squash") != 1 {
		t.Fatalf("calls = %v", fake.calls)
	}
}

func TestMergeOffersAutoMergeWhenChecksArePendingAndAllowed(t *testing.T) {
	fake := newFakeGH(t)
	fake.set(gh.MergeArgs("rubentanahara/agent-trail", prForkPR, gh.MergeSquash, true, true), nil)
	fake.set(gh.ViewArgs(prForkPR), viewWithRollup(t, prForkPR, []map[string]any{{"__typename": "CheckRun", "name": "slow", "status": "IN_PROGRESS", "conclusion": ""}}))
	m := mergeReady(t, fake, func(m *model) { m.pr.repo.AllowAutoMerge = true })

	m = pressPR(m, "M")
	if !strings.Contains(m.confirmPrompt(), "auto-merge") {
		t.Fatalf("prompt = %q", m.confirmPrompt())
	}
	pressPR(m, "d", "a", "y")

	if fake.count("pr merge 14783 --repo=rubentanahara/agent-trail --squash --delete-branch --auto") != 1 {
		t.Fatalf("calls = %v", fake.calls)
	}
}

func TestMergeWarnsOnFailingChecksButStillMerges(t *testing.T) {
	fake := newFakeGH(t)
	fake.set(gh.MergeArgs("rubentanahara/agent-trail", prForkPR, gh.MergeSquash, false, false), nil)
	fake.set(gh.ViewArgs(prForkPR), viewWithRollup(t, prForkPR, []map[string]any{{"__typename": "CheckRun", "name": "broken", "status": "COMPLETED", "conclusion": "FAILURE"}}))
	m := mergeReady(t, fake, nil)

	m = pressPR(m, "M")
	if !strings.Contains(m.confirmPrompt(), "checks are failing") {
		t.Fatalf("prompt = %q", m.confirmPrompt())
	}
	pressPR(m, "y")
	if fake.count("pr merge") != 1 {
		t.Fatal("a failing check must warn, not block")
	}
}

func TestMergeResultReachesTheNoticeAfterTheScreenClosed(t *testing.T) {
	fake := newFakeGH(t)
	fake.set(gh.MergeArgs("rubentanahara/agent-trail", prForkPR, gh.MergeSquash, false, false), nil)
	m := mergeReady(t, fake, nil)
	m = pressPR(m, "M")
	updated, cmd := m.Update(tea.KeyPressMsg{Code: 'y', Text: "y"})
	m = updated.(model)
	m.closePRs()

	m = deliver(m, cmd)

	if !strings.Contains(m.notice, "merge #14783") || !strings.Contains(m.notice, "done") {
		t.Fatalf("notice = %q", m.notice)
	}
}

func TestMergeDialogWarnsWhenThePullRequestChangedUnderIt(t *testing.T) {
	m := mergeReady(t, newFakeGH(t), nil)
	m = pressPR(m, "M")
	changed := m.pr.byNumber[prForkPR]
	changed.HeadSHA = "moved"
	m.pr.byNumber[prForkPR] = changed

	if !strings.Contains(m.confirmPrompt(), "updated since you opened this") {
		t.Fatalf("prompt = %q", m.confirmPrompt())
	}
}

func TestOneShotReviewSendsTheBodyFileAfterConfirmation(t *testing.T) {
	fake := newFakeGH(t)
	m := openedPRs(t, fake)
	path := draftFile(t, "ship it")
	fake.set(gh.ReviewArgs(prForkPR, gh.ReviewApprove, path), nil)

	m = pressPR(m, "A")
	if !strings.Contains(m.confirmPrompt(), "approve") {
		t.Fatalf("picker = %q", m.confirmPrompt())
	}
	m, cmd := compose(m, composePurpose{kind: composeReview, number: prForkPR, event: gh.ReviewApprove}, path)
	if cmd != nil || m.pr.confirm == nil {
		t.Fatal("the review must wait for y")
	}
	pressPR(m, "y")

	if fake.count("pr review 14783 --approve --body-file=") != 1 {
		t.Fatalf("calls = %v", fake.calls)
	}
}

func TestRequestChangesNeedsABodyButApproveDoesNot(t *testing.T) {
	m := openedPRs(t, newFakeGH(t))
	empty := draftFile(t, "\n")

	m, _ = compose(m, composePurpose{kind: composeReview, number: prForkPR, event: gh.ReviewRequestChanges}, empty)
	if m.pr.confirm != nil {
		t.Fatal("request changes with an empty body must cancel")
	}
	m, _ = compose(m, composePurpose{kind: composeReview, number: prForkPR, event: gh.ReviewApprove}, draftFile(t, "\n"))
	if m.pr.confirm == nil {
		t.Fatal("an empty approve is allowed")
	}
}

func actionsCheckRow(t *testing.T, m model) int {
	t.Helper()
	pr, _ := m.pr.selected()
	for i, check := range pr.Checks {
		if _, ok := gh.ParseCheckURL(check.URL); ok {
			return i
		}
	}
	t.Fatal("fixture has no Actions check")
	return 0
}

func onActionsCheck(t *testing.T, fake *fakeGH) model {
	t.Helper()
	m := pressPR(openedPRs(t, fake), "]", "2")
	m.pr.tabCursor[checksTab] = actionsCheckRow(t, m)
	return m
}

func logRef(t *testing.T, m model) gh.LogRef {
	t.Helper()
	pr, _ := m.pr.selected()
	ref, _ := gh.ParseCheckURL(pr.Checks[actionsCheckRow(t, m)].URL)
	return ref
}

func TestLogViewerShowsCleanFailedLogAndTogglesToFull(t *testing.T) {
	fake := newFakeGH(t)
	m := onActionsCheck(t, fake)
	ref := logRef(t, m)
	fake.set(gh.LogArgs(ref, true), testdata(t, "failed.log"))
	fake.set(gh.LogArgs(ref, false), []byte("full one\nfull two\n"))

	m = pressPR(m, "L", "G")
	text := screenText(m)
	if !strings.Contains(text, "Error: boom") || strings.Contains(text, "2026-10-23T") || !strings.Contains(text, "failed steps") {
		t.Fatalf("failed log:\n%s", text)
	}
	m = pressPR(m, "a")
	if !strings.Contains(screenText(m), "full two") {
		t.Fatalf("full log:\n%s", screenText(m))
	}
	m = pressPR(m, "esc")
	if m.pr.log != nil {
		t.Fatal("esc should close the log")
	}
}

func TestLogViewerKeepsTheLastFiveThousandLinesAndSaysSo(t *testing.T) {
	fake := newFakeGH(t)
	m := onActionsCheck(t, fake)
	var log strings.Builder
	for i := 1; i <= 8000; i++ {
		log.WriteString("row " + strings.Repeat("x", i%7) + "\n")
	}
	fake.set(gh.LogArgs(logRef(t, m), true), []byte(log.String()))

	m = pressPR(m, "L")

	if len(m.pr.log.lines) != 5000 || !strings.Contains(screenText(m), "last 5000 lines") {
		t.Fatalf("lines = %d", len(m.pr.log.lines))
	}
}

func TestLogSearchFindsMatches(t *testing.T) {
	fake := newFakeGH(t)
	m := onActionsCheck(t, fake)
	fake.set(gh.LogArgs(logRef(t, m), true), []byte("alpha\nbeta needle\ngamma\nneedle again\n"))

	m = pressPR(m, "L", "/", "n", "e", "e", "d", "l", "e", "enter")

	if len(m.pr.log.matches) != 2 {
		t.Fatalf("matches = %v", m.pr.log.matches)
	}
	m = pressPR(m, "n")
	if m.pr.log.current != 1 {
		t.Fatalf("current = %d", m.pr.log.current)
	}
}

func TestRunInProgressShowsItsOwnMessage(t *testing.T) {
	fake := newFakeGH(t)
	m := onActionsCheck(t, fake)
	fake.fail(gh.LogArgs(logRef(t, m), true), &gh.Error{Stderr: "run 1 is still in progress; logs will be available when it is complete", Err: errors.New("exit status 1")})

	m = pressPR(m, "L")

	if !strings.Contains(screenText(m), "still in progress") {
		t.Fatalf("screen:\n%s", screenText(m))
	}
}

func TestNonActionsChecksHaveNoLogsAndNoRerun(t *testing.T) {
	m := pressPR(openedPRs(t, newFakeGH(t)), "]", "2")
	pr, _ := m.pr.selected()
	pr.Checks = []gh.Check{{Name: "ci/legacy", State: gh.CheckFailing, URL: "https://ci.example.com/1", StatusOnly: true}}
	m.pr.details[prForkPR] = pr
	m.pr.stamps[prForkPR] = stampOf(m.pr.byNumber[prForkPR])

	if m = pressPR(m, "L"); m.pr.log != nil || !strings.Contains(m.notice, "only for GitHub Actions") {
		t.Fatalf("log = %v, notice = %q", m.pr.log, m.notice)
	}
	if m = pressPR(m, "E"); m.pr.confirm != nil || !strings.Contains(m.notice, "only GitHub Actions") {
		t.Fatalf("confirm = %v, notice = %q", m.pr.confirm, m.notice)
	}
}

func TestRerunAsksForYAndUsesTheRunID(t *testing.T) {
	fake := newFakeGH(t)
	m := onActionsCheck(t, fake)
	ref := logRef(t, m)
	fake.set(gh.RerunArgs(ref.RunID, true), nil)
	fake.set(gh.RerunArgs(ref.RunID, false), nil)

	if cancelled := pressPR(m, "E", "n"); fake.count("run rerun") != 0 || cancelled.pr.confirm != nil {
		t.Fatal("any other key must cancel")
	}
	pressPR(m, "E", "y")
	pressPR(m, "E", "a", "y")

	if fake.count("run rerun "+ref.RunID+" --failed") != 1 || fake.count("run rerun "+ref.RunID) != 2 {
		t.Fatalf("calls = %v", fake.calls)
	}
}

func TestQuitDuringAWriteWarnsAndNeedsASecondQ(t *testing.T) {
	m := openedPRs(t, newFakeGH(t))
	m.pr.inFlight["merge:1"] = true

	updated, cmd := m.Update(tea.KeyPressMsg{Code: 'q', Text: "q"})
	m = updated.(model)
	if cmd != nil && countQuit(cmd) {
		t.Fatal("first q must not quit")
	}
	if !strings.Contains(m.notice, "press q again") {
		t.Fatalf("notice = %q", m.notice)
	}
	_, cmd = m.Update(tea.KeyPressMsg{Code: 'q', Text: "q"})
	if !countQuit(cmd) {
		t.Fatal("second q must quit")
	}
}

func countQuit(cmd tea.Cmd) bool {
	if cmd == nil {
		return false
	}
	_, quits := cmd().(tea.QuitMsg)
	return quits
}

func TestWritesAreRefusedWhileRateLimited(t *testing.T) {
	m := openedPRs(t, newFakeGH(t))
	m.now = clockAt(t, 0)
	m.pr.pausedTill = clockAt(t, 5*60*1e9)

	m = pressPR(m, "X", "y")

	if !strings.Contains(m.notice, "writes are refused") || len(m.pr.inFlight) != 0 {
		t.Fatalf("notice = %q", m.notice)
	}
}

func TestEveryConfirmationWarnsWhenThePullRequestChanged(t *testing.T) {
	m := pressPR(openedPRs(t, newFakeGH(t)), "X")
	changed := m.pr.byNumber[prForkPR]
	changed.HeadSHA = "moved"
	m.pr.byNumber[prForkPR] = changed

	if !strings.Contains(m.confirmPrompt(), "PR updated since you opened this") {
		t.Fatalf("prompt = %q", m.confirmPrompt())
	}
}
