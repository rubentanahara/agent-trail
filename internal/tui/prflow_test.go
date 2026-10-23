package tui

import (
	"encoding/json"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/rubentanahara/agent-trail/internal/gh"
)

const responsivenessBudget = 100 * time.Millisecond

func manyPullRequests(t *testing.T, count int) []byte {
	t.Helper()
	var template []map[string]any
	if err := json.Unmarshal(testdata(t, "list.json"), &template); err != nil {
		t.Fatal(err)
	}
	many := make([]map[string]any, count)
	for i := range many {
		entry := map[string]any{}
		for key, value := range template[1] {
			entry[key] = value
		}
		entry["number"] = 1000 + i
		entry["title"] = "pull request " + strconv.Itoa(i)
		many[i] = entry
	}
	data, err := json.Marshal(many)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestTwoHundredPullRequestsRedrawWithinTheBudget(t *testing.T) {
	fake := newFakeGH(t)
	fake.set(gh.ListArgs(), manyPullRequests(t, 200))
	fake.set(gh.ReviewRequestedArgs(), []byte("[]"))
	for _, number := range []int{1000, 1001} {
		fake.set(gh.ViewArgs(number), viewJSON(t, prBranchPR))
	}
	m := openedPRs(t, fake)

	started := time.Now()
	m = pressPR(m, "down")
	_ = m.render()

	if elapsed := time.Since(started); elapsed > responsivenessBudget {
		t.Fatalf("keypress and redraw took %v, budget %v", elapsed, responsivenessBudget)
	}
	if !strings.Contains(screenText(m), "100+") {
		t.Fatalf("the title should cap the count at 100+:\n%s", screenText(m))
	}
}

func TestEightyByTwentyFourRendersWithoutOverlap(t *testing.T) {
	m := sized(openedPRs(t, newFakeGH(t)), 80, 24)

	lines := strings.Split(m.render(), "\n")
	if len(lines) != 24 {
		t.Fatalf("height = %d", len(lines))
	}
	for _, line := range lines {
		if width := ansi.StringWidth(line); width > 80 {
			t.Fatalf("line is %d wide: %q", width, ansi.Strip(line))
		}
	}
}

func TestSessionToMergedPullRequestEndToEnd(t *testing.T) {
	fake := newFakeGH(t)
	m := settleBatch(prModel(t, fake), sessionOnBranches("s1", clockAt(t, 0), prBranch))
	m.pr.mergeRun = fake.run
	m = settlePR(m, m.startPRs())
	ref := gh.LogRef{}
	fake.set(gh.MergeArgs("rubentanahara/agent-trail", prBranchPR, gh.MergeSquash, true, false), nil)

	m.selectSession(m.rowOf("s1"))
	m = pressPR(m, "p")
	if m.pr.selectedNumber() != prBranchPR {
		t.Fatalf("selected %d, want the session's pull request", m.pr.selectedNumber())
	}
	pr, _ := m.pr.selected()
	for i, check := range pr.Checks {
		if parsed, ok := gh.ParseCheckURL(check.URL); ok {
			ref = parsed
			m = pressPR(m, "]", "2")
			m.pr.tabCursor[checksTab] = i
			break
		}
	}
	fake.set(gh.LogArgs(ref, true), testdata(t, "failed.log"))
	fake.set(gh.RerunArgs(ref.RunID, true), nil)

	m = pressPR(m, "L", "esc", "E", "y")
	m = pressPR(m, "1", "[", "M", "d", "y")

	if fake.count("run rerun") != 1 || fake.count("pr merge 14716 --repo=rubentanahara/agent-trail --squash --delete-branch") != 1 {
		t.Fatalf("calls = %v", fake.calls)
	}
}
