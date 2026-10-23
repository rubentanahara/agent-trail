package gh

import (
	"os"
	"slices"
	"strconv"
	"strings"
	"testing"
)

func TestMergeArgsRunAgainstTheNamedRepoWithTheChosenMethod(t *testing.T) {
	got := MergeArgs("o/r", 7, MergeSquash, true, true)
	want := []string{"pr", "merge", "7", "--repo=o/r", "--squash", "--delete-branch", "--auto"}

	if !slices.Equal(got, want) {
		t.Fatalf("MergeArgs = %v, want %v", got, want)
	}
	plain := MergeArgs("o/r", 7, MergeRebase, false, false)
	if slices.Contains(plain, "--delete-branch") || slices.Contains(plain, "--auto") {
		t.Fatalf("plain merge carries toggles: %v", plain)
	}
}

func TestRepoMethodsListsOnlyAllowedOnes(t *testing.T) {
	repo := Repo{AllowSquash: true}

	if got := repo.Methods(); !slices.Equal(got, []MergeMethod{MergeSquash}) {
		t.Fatalf("Methods = %v", got)
	}
	if got := (Repo{AllowSquash: true, AllowMerge: true, AllowRebase: true}).Methods(); len(got) != 3 {
		t.Fatalf("Methods = %v", got)
	}
}

func TestTitlesAndBodiesNeverTouchArgvUnsafely(t *testing.T) {
	edit := EditArgs(7, "-rf --delete-branch", "/tmp/body")

	if !slices.Contains(edit, "--title=-rf --delete-branch") || !slices.Contains(edit, "--body-file=/tmp/body") {
		t.Fatalf("EditArgs = %v", edit)
	}
	for _, args := range [][]string{CommentArgs(7, "/tmp/b"), ReviewArgs(7, ReviewComment, "/tmp/b")} {
		if !slices.Contains(args, "--body-file=/tmp/b") {
			t.Fatalf("body not passed by file: %v", args)
		}
	}
}

func TestRerunUsesTheRunID(t *testing.T) {
	if got := RerunArgs("123", true); !slices.Equal(got, []string{"run", "rerun", "123", "--failed"}) {
		t.Fatalf("failed = %v", got)
	}
	if got := RerunArgs("123", false); !slices.Equal(got, []string{"run", "rerun", "123"}) {
		t.Fatalf("all = %v", got)
	}
}

func TestParseCheckURLFindsRunAndJobOfActionsChecks(t *testing.T) {
	ref, ok := ParseCheckURL("https://github.com/cli/cli/actions/runs/39640910716/job/119363866134")
	if !ok || ref.RunID != "39640910716" || ref.JobID != "119363866134" {
		t.Fatalf("ref = %+v, ok = %v", ref, ok)
	}
	if _, ok := ParseCheckURL("https://ci.example.com/build/1"); ok {
		t.Fatal("a non-Actions URL has no logs")
	}
}

func TestLogArgsSwitchBetweenFailedAndFull(t *testing.T) {
	ref := LogRef{RunID: "1", JobID: "2"}

	if got := LogArgs(ref, true); !slices.Contains(got, "--log-failed") || !slices.Contains(got, "--job=2") {
		t.Fatalf("failed = %v", got)
	}
	if got := LogArgs(ref, false); !slices.Contains(got, "--log") {
		t.Fatalf("full = %v", got)
	}
}

func TestTailLogStripsTimestampsAnsiAndStepColumns(t *testing.T) {
	lines, truncated, err := TailLog(strings.NewReader(string(fixture(t, "failed.log"))), LogTailLines)
	if err != nil {
		t.Fatal(err)
	}
	if truncated {
		t.Fatal("small log reported as truncated")
	}
	last := lines[len(lines)-1]
	if last != "Error: boom" {
		t.Fatalf("last = %q", last)
	}
	if strings.HasPrefix(lines[0], "close-suspected-spam") || strings.Contains(lines[0], "2026-10-23T") || strings.Contains(lines[0], "\ufeff") {
		t.Fatalf("first = %q", lines[0])
	}
}

func TestTailLogKeepsExactlyTheLastFiveThousandLines(t *testing.T) {
	var log strings.Builder
	for i := 1; i <= 8000; i++ {
		log.WriteString("line ")
		log.WriteString(strconv.Itoa(i))
		log.WriteString("\n")
	}

	lines, truncated, err := TailLog(strings.NewReader(log.String()), LogTailLines)

	if err != nil || !truncated || len(lines) != 5000 {
		t.Fatalf("len = %d, truncated = %v, err = %v", len(lines), truncated, err)
	}
	if lines[0] != "line 3001" || lines[4999] != "line 8000" {
		t.Fatalf("window = %q .. %q", lines[0], lines[4999])
	}
}

func TestTailLogTruncatesLinesOver64KiB(t *testing.T) {
	long := strings.Repeat("x", 200<<10)

	lines, _, err := TailLog(strings.NewReader("before\n"+long+"\nafter\n"), LogTailLines)

	if err != nil || len(lines) != 3 {
		t.Fatalf("lines = %d, err = %v", len(lines), err)
	}
	if len(lines[1]) > 65<<10 || !strings.HasSuffix(lines[1], "[line truncated]") || lines[2] != "after" {
		t.Fatalf("line 1 len = %d, line 2 = %q", len(lines[1]), lines[2])
	}
}

func TestIsRunInProgress(t *testing.T) {
	err := &Error{Stderr: "run 1 is still in progress; logs will be available when it is complete"}
	if !IsRunInProgress(err) {
		t.Fatal("expected in progress")
	}
}

func TestDecodeRunsKeepsRepoWideRunsAndDropsPullRequestOnes(t *testing.T) {
	data, err := os.ReadFile("testdata/runs.json")
	if err != nil {
		t.Fatal(err)
	}

	runs, err := DecodeRuns(data)

	if err != nil || len(runs) != 2 {
		t.Fatalf("runs = %+v, err = %v", runs, err)
	}
	if runs[0].State != CheckPassing || runs[0].Workflow != "ci · push · main" || runs[0].CompletedAt.IsZero() {
		t.Fatalf("first = %+v", runs[0])
	}
	if runs[1].State != CheckPending || !runs[1].CompletedAt.IsZero() {
		t.Fatalf("second = %+v", runs[1])
	}
}

func TestDecodeBoardReadsProjectFieldsAndSkipsNonIssueItems(t *testing.T) {
	board, err := DecodeBoard(fixtureFile(t, "board.json"))
	if err != nil || len(board.Projects) != 1 {
		t.Fatalf("board = %+v, err = %v", board, err)
	}
	project := board.Projects[0]
	if project.Title != "lazyclipboard" || project.Total != 200 || len(project.Issues) != 8 {
		t.Fatalf("project = %s total %d issues %d", project.Title, project.Total, len(project.Issues))
	}
	first := project.Issues[0]
	if first.Number != 1 || first.Field("Status") != "Done" || first.Field("Priority") != "P0" || first.Field(titleFieldName) != "" {
		t.Fatalf("first = %+v", first)
	}
	if first.SubTotal != 3 || len(first.Labels) != 2 || len(first.Assignees) != 1 || first.Author != "rubentanahara" {
		t.Fatalf("first = %+v", first)
	}
	if len(project.Columns) == 0 || len(board.Open) != 4 || board.OpenTotal < 4 {
		t.Fatalf("open = %d of %d", len(board.Open), board.OpenTotal)
	}
}

func TestDecodeIssueDetailReadsCommentsLinkedPullRequestsAndSubIssues(t *testing.T) {
	detail, err := DecodeIssueDetail(fixtureFile(t, "issue_detail.json"))
	if err != nil {
		t.Fatal(err)
	}
	if detail.Body == "" || detail.CommentTotal != 1 || detail.Comments[0].Author != "octo" {
		t.Fatalf("detail = %+v", detail)
	}
	if len(detail.ClosedBy) != 1 || detail.ClosedBy[0].State != "MERGED" || len(detail.SubIssues) == 0 {
		t.Fatalf("detail = %+v", detail)
	}
}

func TestBoardArgsKeepValuesOutOfTheQuery(t *testing.T) {
	args := strings.Join(BoardArgs("o", "n; drop"), " ")

	if strings.Contains(args[:strings.Index(args, "-f owner")], "drop") {
		t.Fatalf("name leaked into the query: %s", args)
	}
}

func fixtureFile(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return data
}
