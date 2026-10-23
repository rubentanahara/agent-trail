package gh

import (
	"context"
	"errors"
	"slices"
	"strconv"
	"strings"
	"testing"
)

func TestDecodeThreadsReadsThreadsCommentsAndPendingReview(t *testing.T) {
	threads, err := DecodeThreads(fixture(t, "threads.json"))
	if err != nil {
		t.Fatal(err)
	}
	if threads.Total != 2 || len(threads.Threads) != 2 {
		t.Fatalf("threads = %+v", threads)
	}
	first := threads.Threads[0]
	if first.Path != "internal/ghcmd/global_flags.go" || first.Line != 25 || first.Resolved || len(first.Comments) == 0 {
		t.Fatalf("first = %+v", first)
	}
	if threads.Pending == nil || threads.Pending.ID != "PRR_pending" || threads.Pending.CommitOID != "abc123" || threads.Pending.Comments != 3 {
		t.Fatalf("pending = %+v", threads.Pending)
	}
}

func TestThreadsArgsKeepValuesOutOfTheQuery(t *testing.T) {
	args := ThreadsArgs("o", "n", 7)

	for _, want := range []string{"owner=o", "name=n", "number=7"} {
		if !slices.Contains(args, want) {
			t.Fatalf("args %v lack %s", args, want)
		}
	}
	if query := args[slices.Index(args, "-f")+1]; query != "query="+threadsQuery {
		t.Fatalf("query was altered: %q", query)
	}
}

func TestDecodeRepoReadsMergeSettings(t *testing.T) {
	repo, err := DecodeRepo(fixture(t, "repo.json"))
	if err != nil {
		t.Fatal(err)
	}
	if repo.FullName != "rubentanahara/agent-trail" || repo.Owner != "rubentanahara" || repo.Name != "agent-trail" {
		t.Fatalf("repo = %+v", repo)
	}
}

func TestMutationArgsPassValuesAsVariablesAndBodiesAsFiles(t *testing.T) {
	cases := map[string][]string{
		"reply":    ReplyArgs("PRRT_1", "/tmp/b"),
		"edit":     EditIssueCommentArgs("IC_1", "/tmp/b"),
		"editline": EditReviewCommentArgs("PRRC_1", "/tmp/b"),
		"thread":   LineThreadArgs("PRR_1", "a/b.go", 12, "/tmp/b"),
		"submit":   SubmitReviewArgs("PRR_1", SubmitApprove, "/tmp/b"),
	}
	for name, args := range cases {
		if !slices.Contains(args, "body=@/tmp/b") {
			t.Errorf("%s: body not read from a file: %v", name, args)
		}
		for _, arg := range args {
			if strings.Contains(arg, "/tmp/b") && !strings.HasPrefix(arg, "body=@") {
				t.Errorf("%s: path leaked elsewhere: %q", name, arg)
			}
		}
	}
	thread := cases["thread"]
	if !slices.Contains(thread, "line=12") || !slices.Contains(thread, "path=a/b.go") {
		t.Fatalf("thread args = %v", thread)
	}
	if i := slices.Index(thread, "line=12"); thread[i-1] != "-F" {
		t.Fatalf("line must be sent with -F (int): %v", thread)
	}
	if !strings.Contains(lineThreadMutation, "side:RIGHT") || !strings.Contains(lineThreadMutation, "addPullRequestReviewThread") {
		t.Fatal("line comments must use the thread mutation on the right side")
	}
}

func TestResolveAndDiscardTargetOnlyTheirID(t *testing.T) {
	if got := ResolveArgs("PRRT_1"); !slices.Contains(got, "threadId=PRRT_1") {
		t.Fatalf("resolve = %v", got)
	}
	if got := UnresolveArgs("PRRT_1"); !slices.Contains(got, "threadId=PRRT_1") {
		t.Fatalf("unresolve = %v", got)
	}
	if got := DiscardReviewArgs("PRR_1"); !slices.Contains(got, "reviewId=PRR_1") {
		t.Fatalf("discard = %v", got)
	}
}

func TestDecodeStartedReviewReadsTheNewID(t *testing.T) {
	id, err := DecodeStartedReview([]byte(`{"data":{"addPullRequestReview":{"pullRequestReview":{"id":"PRR_new"}}}}`))
	if err != nil || id != "PRR_new" {
		t.Fatalf("id = %q, err = %v", id, err)
	}
}

type scriptedRunner struct {
	t       *testing.T
	replies map[string][]byte
	calls   [][]string
}

func (s *scriptedRunner) run(_ context.Context, args []string) ([]byte, error) {
	s.calls = append(s.calls, args)
	for needle, reply := range s.replies {
		if strings.Contains(strings.Join(args, " "), needle) {
			return reply, nil
		}
	}
	s.t.Fatalf("unexpected call %v", args)
	return nil, nil
}

func lineCommentRepo() Repo { return Repo{Owner: "o", Name: "n"} }

func TestAddLineCommentContinuesAnExistingPendingReviewOnTheSameCommit(t *testing.T) {
	runner := &scriptedRunner{t: t, replies: map[string][]byte{"reviewThreads": fixture(t, "threads.json"), "addPullRequestReviewThread": []byte(`{}`)}}

	err := AddLineComment(context.Background(), runner.run, lineCommentRepo(), 7, "PR_1", "abc123", "a.go", 4, "/tmp/b")

	if err != nil || len(runner.calls) != 2 {
		t.Fatalf("err = %v, calls = %d", err, len(runner.calls))
	}
	if !slices.Contains(runner.calls[1], "reviewId=PRR_pending") {
		t.Fatalf("thread not added to the pending review: %v", runner.calls[1])
	}
}

func TestAddLineCommentStartsAReviewAtTheLoadedHead(t *testing.T) {
	none := []byte(`{"data":{"repository":{"pullRequest":{"reviewThreads":{"totalCount":0,"nodes":[]},"reviews":{"nodes":[]}}}}}`)
	runner := &scriptedRunner{t: t, replies: map[string][]byte{
		"reviewThreads":              none,
		"addPullRequestReview(":      []byte(`{"data":{"addPullRequestReview":{"pullRequestReview":{"id":"PRR_new"}}}}`),
		"addPullRequestReviewThread": []byte(`{}`),
	}}

	err := AddLineComment(context.Background(), runner.run, lineCommentRepo(), 7, "PR_1", "head1", "a.go", 4, "/tmp/b")

	if err != nil || len(runner.calls) != 3 {
		t.Fatalf("err = %v, calls = %d", err, len(runner.calls))
	}
	if !slices.Contains(runner.calls[1], "commit=head1") || !slices.Contains(runner.calls[1], "prId=PR_1") {
		t.Fatalf("review not started at the head: %v", runner.calls[1])
	}
	if !slices.Contains(runner.calls[2], "reviewId=PRR_new") {
		t.Fatalf("thread not added to the new review: %v", runner.calls[2])
	}
}

func TestAddLineCommentRefusesAPendingReviewFromAnOlderCommit(t *testing.T) {
	runner := &scriptedRunner{t: t, replies: map[string][]byte{"reviewThreads": fixture(t, "threads.json")}}

	err := AddLineComment(context.Background(), runner.run, lineCommentRepo(), 7, "PR_1", "newer", "a.go", 4, "/tmp/b")

	if !errors.Is(err, ErrPendingOnOlderCommit) || len(runner.calls) != 1 {
		t.Fatalf("err = %v, calls = %d", err, len(runner.calls))
	}
}

func TestDecodeThreadsMarksPendingComments(t *testing.T) {
	threads, err := DecodeThreads(fixture(t, "threads.json"))
	if err != nil {
		t.Fatal(err)
	}
	if threads.Threads[0].Comments[0].Pending || !threads.Threads[1].Comments[0].Pending {
		t.Fatalf("pending flags = %+v", threads.Threads)
	}
}

func TestLoadThreadsFollowsCursorsAndKeepsThePendingReviewOfTheFirstPage(t *testing.T) {
	page := func(cursor string, more bool, pending string) []byte {
		reviews := `[]`
		if pending != "" {
			reviews = `[{"id":"` + pending + `","commit":{"oid":"c"},"comments":{"totalCount":2}}]`
		}
		return []byte(`{"data":{"repository":{"pullRequest":{"reviewThreads":{"totalCount":2,"pageInfo":{"hasNextPage":` + strconv.FormatBool(more) + `,"endCursor":"` + cursor + `"},"nodes":[{"id":"T` + cursor + `","path":"a.go","line":1,"comments":{"nodes":[]}}]},"reviews":{"nodes":` + reviews + `}}}}}`)
	}
	var calls [][]string
	run := func(_ context.Context, args []string) ([]byte, error) {
		calls = append(calls, args)
		if slices.Contains(args, "after=c1") {
			return page("c2", false, ""), nil
		}
		return page("c1", true, "PRR_1"), nil
	}

	threads, err := LoadThreads(context.Background(), run, "o", "n", 7)

	if err != nil || len(calls) != 2 || len(threads.Threads) != 2 || threads.Pending == nil || threads.Pending.ID != "PRR_1" {
		t.Fatalf("err = %v, calls = %d, threads = %+v", err, len(calls), threads)
	}
	if slices.Contains(calls[0], "after=") {
		t.Fatalf("the first page must omit the cursor: %v", calls[0])
	}
}
