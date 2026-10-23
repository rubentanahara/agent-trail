package gh

import (
	"context"
	"os"
	"slices"
	"testing"
)

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestDecodeListHandlesBothRollupKindsAndANullAuthor(t *testing.T) {
	prs, err := DecodeList(fixture(t, "list.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(prs) != 2 {
		t.Fatalf("len = %d", len(prs))
	}
	first := prs[0]
	if first.Author.Login != "ghost" {
		t.Fatalf("null author = %+v", first.Author)
	}
	states := map[string]CheckState{}
	var statusOnly []string
	for _, check := range first.Checks {
		states[check.Name] = check.State
		if check.StatusOnly {
			statusOnly = append(statusOnly, check.Name)
		}
	}
	if states["label-external"] != CheckSkipped || states["ci/legacy"] != CheckPending || states["build"] != CheckPending {
		t.Fatalf("states = %v", states)
	}
	if !slices.Equal(statusOnly, []string{"ci/legacy"}) {
		t.Fatalf("status contexts = %v", statusOnly)
	}
	if !first.Pending() || first.Failing() {
		t.Fatalf("Pending = %v, Failing = %v", first.Pending(), first.Failing())
	}
	if first.Checks[len(first.Checks)-1].CompletedAt.IsZero() != true {
		t.Fatal("zero completedAt should decode as unset")
	}
}

func TestDecodeListMarksBotsAndDrops(t *testing.T) {
	prs, err := DecodeList(fixture(t, "list.json"))
	if err != nil {
		t.Fatal(err)
	}
	bot := prs[1]
	if !bot.Author.Bot || bot.Author.Login != "dependabot" {
		t.Fatalf("author = %+v", bot.Author)
	}
	if bot.ReviewDecision != "APPROVED" || bot.CrossRepo {
		t.Fatalf("pr = %+v", bot)
	}
}

func TestDecodeViewReadsBodyReviewsCommentsAndFiles(t *testing.T) {
	pr, err := DecodeView(fixture(t, "view.json"))
	if err != nil {
		t.Fatal(err)
	}
	if pr.Number != 14781 || pr.Body == "" || pr.Author.Login != "williammartin" {
		t.Fatalf("pr = %+v", pr)
	}
	if len(pr.Reviews) == 0 || pr.Reviews[0].Author.Login != "copilot-pull-request-reviewer" {
		t.Fatalf("reviews = %+v", pr.Reviews)
	}
	if len(pr.Files) == 0 || pr.Files[0].Path != "docs/api-and-hosts.md" || pr.Files[0].Additions != 89 {
		t.Fatalf("files = %+v", pr.Files)
	}
}

func TestDecodeRejectsGarbage(t *testing.T) {
	if _, err := DecodeList([]byte("nope")); err == nil {
		t.Fatal("expected an error")
	}
}

func TestPartitionSplitsMineAndNeedsReview(t *testing.T) {
	open := []PR{{Number: 1, Author: Author{Login: "me"}}, {Number: 2, Author: Author{Login: "them"}}, {Number: 3, Author: Author{Login: "them"}}}
	sections := Partition(open, []PR{{Number: 3}}, "me")

	if len(sections.All) != 3 || len(sections.Mine) != 1 || sections.Mine[0].Number != 1 {
		t.Fatalf("sections = %+v", sections)
	}
	if len(sections.NeedsReview) != 1 || sections.NeedsReview[0].Number != 3 {
		t.Fatalf("needs review = %+v", sections.NeedsReview)
	}
}

func TestArgumentBuildersUseFlagEqualsValueForm(t *testing.T) {
	if got := ViewArgs(7); got[0] != "pr" || got[1] != "view" || got[2] != "7" {
		t.Fatalf("ViewArgs = %v", got)
	}
	for _, args := range [][]string{ListArgs(), ReviewRequestedArgs(), ViewArgs(7), ViewerArgs()} {
		for _, arg := range args[2:] {
			if len(arg) > 1 && arg[0] == '-' && !containsEquals(arg) {
				t.Fatalf("flag %q is not in --flag=value form (%v)", arg, args)
			}
		}
	}
	if !slices.Contains(ReviewRequestedArgs(), "--search=review-requested:@me") {
		t.Fatalf("ReviewRequestedArgs = %v", ReviewRequestedArgs())
	}
	if !slices.Contains(ViewerArgs(), "--cache=1h") {
		t.Fatalf("ViewerArgs = %v", ViewerArgs())
	}
}

func containsEquals(arg string) bool {
	for _, r := range arg {
		if r == '=' {
			return true
		}
	}
	return false
}

func TestListViewAndViewerGoThroughTheRunner(t *testing.T) {
	var calls [][]string
	run := func(_ context.Context, args []string) ([]byte, error) {
		calls = append(calls, args)
		switch args[1] {
		case "list":
			return fixture(t, "list.json"), nil
		case "view":
			return fixture(t, "view.json"), nil
		}
		return []byte("me\n"), nil
	}
	if _, err := List(context.Background(), run); err != nil {
		t.Fatal(err)
	}
	if _, err := ListReviewRequested(context.Background(), run); err != nil {
		t.Fatal(err)
	}
	if pr, err := View(context.Background(), run, 14781); err != nil || pr.Number != 14781 {
		t.Fatalf("View = %+v, %v", pr, err)
	}
	if login, err := Viewer(context.Background(), run); err != nil || login != "me" {
		t.Fatalf("Viewer = %q, %v", login, err)
	}
	if len(calls) != 4 {
		t.Fatalf("calls = %v", calls)
	}
}

func TestResetTimeReadsTheGraphQLResetEpoch(t *testing.T) {
	run := func(_ context.Context, args []string) ([]byte, error) {
		if !slices.Equal(args, RateResetArgs()) {
			t.Fatalf("args = %v", args)
		}
		return []byte("1792800000\n"), nil
	}

	at, ok := ResetTime(context.Background(), run)

	if !ok || at.Unix() != 1792800000 {
		t.Fatalf("at = %v, ok = %v", at, ok)
	}
}
