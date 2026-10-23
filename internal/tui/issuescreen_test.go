package tui

import (
	"strings"
	"testing"

	"github.com/rubentanahara/agent-trail/internal/gh"
)

func newIssueFake(t *testing.T) *fakeGH {
	t.Helper()
	fake := newFakeGH(t)
	fake.set(gh.BoardArgs("rubentanahara", "agent-trail"), testdata(t, "board.json"))
	board, err := gh.DecodeBoard(testdata(t, "board.json"))
	if err != nil {
		t.Fatal(err)
	}
	issues := append([]gh.Issue{}, board.Open...)
	for _, project := range board.Projects {
		issues = append(issues, project.Issues...)
	}
	for _, issue := range issues {
		fake.set(gh.IssueDetailArgs("rubentanahara", "agent-trail", issue.Number), testdata(t, "issue_detail.json"))
	}
	return fake
}

func openedIssues(t *testing.T, fake *fakeGH) model {
	t.Helper()
	m := prModel(t, fake)
	m.issues.run = fake.run
	m.pr.open = func(string) error { return nil }
	return pressPR(m, "i")
}

func TestIssuesGroupTheProjectByStatusColumn(t *testing.T) {
	m := openedIssues(t, newIssueFake(t))

	text := screenText(m)
	for _, want := range []string{"lazyclipboard", "Done (", "In Progress (", "E0 · Foundation"} {
		if !strings.Contains(text, want) {
			t.Fatalf("issues screen lacks %q:\n%s", want, text)
		}
	}
}

func TestIssueOverviewShowsProjectFieldsAndBody(t *testing.T) {
	m := openedIssues(t, newIssueFake(t))

	text := screenText(m)
	for _, want := range []string{"Status", "Priority", "P0", "Assignees", "rubentanahara", "Sub-issues", "Everything later issues depend on"} {
		if !strings.Contains(text, want) {
			t.Fatalf("overview lacks %q:\n%s", want, text)
		}
	}
}

func TestPCyclesProjectsThenOpenIssues(t *testing.T) {
	m := pressPR(openedIssues(t, newIssueFake(t)), "P")

	if m.issues.viewTitle() != openIssuesView || !strings.Contains(screenText(m), "Open issues") {
		t.Fatalf("view = %q", m.issues.viewTitle())
	}
	if m = pressPR(m, "P"); m.issues.inProject() == false {
		t.Fatal("P did not wrap back to the project")
	}
}

func TestCommentsAndLinkedTabsListTheDetailAndOpenTheCursorRow(t *testing.T) {
	var opened string
	m := openedIssues(t, newIssueFake(t))
	m.pr.open = func(url string) error { opened = url; return nil }

	m = pressPR(m, "]")
	if text := screenText(m); !strings.Contains(text, "octo commented") || !strings.Contains(text, "looks") {
		t.Fatalf("comments tab:\n%s", text)
	}
	m = pressPR(m, "]")
	if text := screenText(m); !strings.Contains(text, "Closed by pull requests") || !strings.Contains(text, "fix it") || !strings.Contains(text, "Sub-issues") {
		t.Fatalf("linked tab:\n%s", text)
	}
	pressPR(m, "2", "down", "o")
	if opened != "https://github.com/o/r/pull/9" {
		t.Fatalf("opened %q", opened)
	}
}

func TestOpeningFromTheListOpensTheIssue(t *testing.T) {
	var opened string
	m := openedIssues(t, newIssueFake(t))
	m.pr.open = func(url string) error { opened = url; return nil }

	pressPR(m, "o")

	if !strings.HasPrefix(opened, "https://github.com/rubentanahara/lazyclipboard/issues/") {
		t.Fatalf("opened %q", opened)
	}
}

func TestIssueFilterNarrowsTheListAndEscClearsIt(t *testing.T) {
	m := openedIssues(t, newIssueFake(t))
	all := len(m.issues.rows)

	m = pressPR(m, "/", "'", "F", "o", "u", "n", "d", "a", "t", "i", "o", "n", "enter")
	if len(m.issues.rows) >= all {
		t.Fatalf("rows %d -> %d", all, len(m.issues.rows))
	}
	if m = pressPR(m, "esc"); len(m.issues.rows) != all {
		t.Fatalf("esc did not clear the filter: %d rows", len(m.issues.rows))
	}
}

func TestIssueKeyIsRefusedOutsideASingleRepository(t *testing.T) {
	m := prModel(t, newIssueFake(t))
	m.context.Repo = ""
	m.issues.shown = false

	if m = pressPR(m, "i"); m.issues.shown {
		t.Fatal("issues opened without a single repository")
	}
}

func TestIssueRefreshRefetchesTheBoard(t *testing.T) {
	fake := newIssueFake(t)
	m := openedIssues(t, fake)
	boards := fake.count("api graphql -f query=query($owner:String!,$name:String!){repository(owner:$owner,name:$name){projectsV2")

	pressPR(m, "R")

	if fake.count("api graphql -f query=query($owner:String!,$name:String!){repository(owner:$owner,name:$name){projectsV2") != boards+1 {
		t.Fatalf("board fetches did not grow by one from %d", boards)
	}
}

func TestIssueScreenFitsEveryWidth(t *testing.T) {
	for _, width := range []int{60, 100, 160} {
		m := openedIssues(t, newIssueFake(t))
		m.width, m.height = width, 30
		for _, line := range strings.Split(m.render(), "\n") {
			if got := len([]rune(line)); got == 0 {
				t.Fatalf("width %d: empty rendered line", width)
			}
		}
	}
}
