package tui

import (
	"strings"
	"testing"

	"github.com/rubentanahara/agent-trail/internal/gh"
)

func onDiff(t *testing.T, fake *fakeGH) model {
	t.Helper()
	return pressPR(openedPRs(t, fake), "]", "]", "]", "2")
}

func diffRow(t *testing.T, m model, match func(prLine) bool) int {
	t.Helper()
	for i, line := range m.prTabLines() {
		if match(line) {
			return i
		}
	}
	t.Fatal("no matching diff row")
	return 0
}

func addedRow(t *testing.T, m model) int {
	return diffRow(t, m, func(l prLine) bool {
		return l.kind == diffBodyLine && l.newLine == 2 && strings.Contains(ansiPlain(l.text), "+new line")
	})
}

func ansiPlain(text string) string { return sanitize(text) }

func TestCOnAnAddedLineStartsComposingForThatLine(t *testing.T) {
	m := onDiff(t, newFakeGH(t))
	m.pr.tabCursor[diffTab] = addedRow(t, m)

	m = pressPR(m, "c")

	if m.notice != editorWaitNotice {
		t.Fatalf("notice = %q", m.notice)
	}
}

func TestCRefusesRemovedLinesHunksAndOtherTabs(t *testing.T) {
	m := onDiff(t, newFakeGH(t))
	removed := diffRow(t, m, func(l prLine) bool { return l.kind == diffBodyLine && strings.Contains(ansiPlain(l.text), "-old line") })
	hunk := diffRow(t, m, func(l prLine) bool { return l.kind == diffBodyLine && strings.Contains(ansiPlain(l.text), "@@") })
	for name, row := range map[string]int{"removed": removed, "hunk": hunk} {
		m.pr.tabCursor[diffTab] = row
		if got := pressPR(m, "c"); !strings.Contains(got.notice, "added or unchanged") {
			t.Errorf("%s: notice = %q", name, got.notice)
		}
	}
	overview := pressPR(openedPRs(t, newFakeGH(t)), "c")
	if !strings.Contains(overview.notice, "Diff tab") {
		t.Fatalf("overview notice = %q", overview.notice)
	}
}

func TestCIsBlockedWhenTheHeadMovedSinceTheDiffLoaded(t *testing.T) {
	m := onDiff(t, newFakeGH(t))
	m.pr.tabCursor[diffTab] = addedRow(t, m)
	moved := m.pr.byNumber[prForkPR]
	moved.HeadSHA = "newer-head"
	m.pr.byNumber[prForkPR] = moved

	m = pressPR(m, "c")

	if !strings.Contains(m.notice, "new commits") {
		t.Fatalf("notice = %q", m.notice)
	}
}

func lineCommentSetup(t *testing.T) (*fakeGH, model, string) {
	t.Helper()
	fake := newFakeGH(t)
	m := onDiff(t, fake)
	m.pr.details[prForkPR] = gh.PR{ID: "PR_node", Number: prForkPR}
	path := draftFile(t, "nit: rename this")
	return fake, m, path
}

func TestLineCommentContinuesTheExistingPendingReview(t *testing.T) {
	fake, m, path := lineCommentSetup(t)
	fake.set(gh.LineThreadArgs("PRR_pending", "docs/guide.md", 2, path), []byte(`{}`))
	purpose := composePurpose{kind: composeLine, number: prForkPR, path: "docs/guide.md", line: 2, headSHA: "abc123"}

	m, cmd := compose(m, purpose, path)
	deliver(m, cmd)

	if fake.count("api graphql -f query=mutation($reviewId:ID!,$path:String!") != 1 || fake.count("api graphql -f query=mutation($prId") != 0 {
		t.Fatalf("calls = %v", fake.calls)
	}
}

func TestLineCommentStartsAReviewAtTheLoadedHeadWhenNonePending(t *testing.T) {
	fake, m, path := lineCommentSetup(t)
	none := `{"data":{"repository":{"pullRequest":{"reviewThreads":{"totalCount":0,"nodes":[]},"reviews":{"nodes":[]}}}}}`
	fake.set(gh.ThreadsArgs("rubentanahara", "agent-trail", prForkPR), []byte(none))
	fake.set(gh.StartReviewArgs("PR_node", "head1"), []byte(`{"data":{"addPullRequestReview":{"pullRequestReview":{"id":"PRR_new"}}}}`))
	fake.set(gh.LineThreadArgs("PRR_new", "docs/guide.md", 2, path), []byte(`{}`))
	purpose := composePurpose{kind: composeLine, number: prForkPR, path: "docs/guide.md", line: 2, headSHA: "head1"}

	m, cmd := compose(m, purpose, path)
	deliver(m, cmd)

	if fake.count("api graphql -f query=mutation($prId:ID!,$commit:GitObjectID!)") != 1 {
		t.Fatalf("review not started: %v", fake.calls)
	}
}

func TestPendingCountShowsInTheHeaderOnEveryTabAndTheDiffGetsAGutterMark(t *testing.T) {
	m := onDiff(t, newFakeGH(t))

	if !strings.Contains(screenText(m), "Pending (3)") {
		t.Fatalf("diff tab lacks the pending header:\n%s", screenText(m))
	}
	if !strings.Contains(screenText(m), pendingMark) {
		t.Fatal("no gutter mark on the pending line")
	}
	if overview := pressPR(m, "["); !strings.Contains(screenText(overview), "Pending (3)") {
		t.Fatal("pending header is missing off the diff tab")
	}
}

func TestLeavingWithAPendingReviewRemindsAndKeepsIt(t *testing.T) {
	m := pressPR(onDiff(t, newFakeGH(t)), "esc", "esc")

	if m.pr.shown || !strings.Contains(m.notice, "Pending (3) kept. S submits.") {
		t.Fatalf("shown = %v, notice = %q", m.pr.shown, m.notice)
	}
}

func TestSubmitSendsEachReviewEvent(t *testing.T) {
	events := map[gh.SubmitEvent]string{gh.SubmitApprove: "a", gh.SubmitRequestChanges: "r", gh.SubmitComment: "c"}
	for event, key := range events {
		fake := newFakeGH(t)
		m := onDiff(t, fake)
		path := draftFile(t, "overall")
		fake.set(gh.SubmitReviewArgs("PRR_pending", event, path), []byte(`{}`))

		m = pressPR(m, "S")
		if !strings.Contains(m.confirmPrompt(), "Submit your pending review (3 comments)") {
			t.Fatalf("prompt = %q", m.confirmPrompt())
		}
		m = pressPR(m, key)
		if m.notice != editorWaitNotice {
			t.Fatalf("%s: notice = %q", event, m.notice)
		}
		m, cmd := compose(m, composePurpose{kind: composeSubmit, number: prForkPR, submit: event}, path)
		deliver(m, cmd)

		if fake.count("api graphql -f query=mutation($reviewId:ID!,$event:PullRequestReviewEvent!") != 1 {
			t.Errorf("%s: calls = %v", event, fake.calls)
		}
	}
}

func TestSubmitAndDiscardNeedAPendingReview(t *testing.T) {
	fake := newFakeGH(t)
	m := onDiff(t, fake)
	data := m.pr.threads[prForkPR]
	data.threads.Pending = nil
	m.pr.threads[prForkPR] = data

	for _, key := range []string{"S", "D"} {
		if got := pressPR(m, key); got.pr.confirm != nil || !strings.Contains(got.notice, "no pending review") {
			t.Errorf("%s: confirm = %v, notice = %q", key, got.pr.confirm, got.notice)
		}
	}
}

func TestDiscardAsksForYBeforeDeleting(t *testing.T) {
	fake := newFakeGH(t)
	fake.set(gh.DiscardReviewArgs("PRR_pending"), []byte(`{}`))
	m := onDiff(t, fake)

	cancelled := pressPR(m, "D", "n")
	if fake.count("api graphql -f query=mutation($reviewId:ID!){deletePullRequestReview") != 0 || cancelled.pr.confirm != nil {
		t.Fatal("discard ran without y")
	}
	pressPR(m, "D", "y")
	if fake.count("api graphql -f query=mutation($reviewId:ID!){deletePullRequestReview") != 1 {
		t.Fatalf("calls = %v", fake.calls)
	}
}
