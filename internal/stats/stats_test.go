package stats

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/rubentanahara/agent-trail/internal/trail"
)

var now = time.Date(2026, 10, 22, 12, 30, 0, 0, time.Local)

func sample(repo string) trail.Batch {
	usage := func(cost, input, cacheRead int64) *trail.Usage {
		return &trail.Usage{Input: input, CacheRead: cacheRead, CostUSD: float64(cost), Priced: true}
	}
	return trail.Batch{
		Sessions: []trail.Session{
			{Key: "claude:a", CLI: trail.Claude, Cwd: filepath.Join(repo, "src")},
			{Key: "codex:b", CLI: trail.Codex, Cwd: "/elsewhere"},
		},
		Events: []trail.Event{
			{Session: "claude:a", Kind: trail.KindUser, Time: now.Add(-time.Minute)},
			{Session: "claude:a", Kind: trail.KindUser, Time: now.Add(-time.Minute)},
			{Session: "claude:a", Kind: trail.KindTool, Files: []string{"x.GO", "y.go", "README"}},
			{Session: "claude:a", Kind: trail.KindUsage, Time: now.Add(-time.Minute), Usage: usage(3, 100, 80)},
			{Session: "codex:b", Kind: trail.KindUser, Time: now.Add(-48 * time.Hour)},
			{Session: "codex:b", Kind: trail.KindUsage, Time: now.Add(-48 * time.Hour), Usage: usage(5, 100, 0)},
		},
	}
}

func TestRowsAreGroupedRankedAndResolvedToTheGitRoot(t *testing.T) {
	repo := t.TempDir()
	if err := os.Mkdir(filepath.Join(repo, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	s := New(now.AddDate(0, 0, -30))
	s.Add(sample(repo))
	clis := s.ByCLI()
	if len(clis) != 2 || clis[0].Label != "claude" || clis[0].Prompts != 2 || clis[0].CacheReadShare() != 0.8 {
		t.Fatalf("by CLI = %+v", clis)
	}
	if projects := s.ByProject(); projects[0].Label != repo || projects[1].Label != "/elsewhere" {
		t.Fatalf("by project = %+v", projects)
	}
	if extensions := s.Extensions(); len(extensions) != 1 || extensions[0] != (Count{Label: ".go", N: 2}) {
		t.Fatalf("extensions = %+v", extensions)
	}
}

func TestSpendIsBucketedByPeriod(t *testing.T) {
	s := New(now.AddDate(0, 0, -30))
	s.Add(sample(t.TempDir()))
	if hourly := s.Series(now, time.Hour, 3); hourly[2] != 3 || hourly[0]+hourly[1] != 0 {
		t.Fatalf("hourly = %v", hourly)
	}
	spend := s.Spend(now)
	if spend[2].Name != "day" || spend[2].Current != 3 {
		t.Fatalf("day = %+v", spend[2])
	}
	if spend[4].Name != "month" || spend[4].Current != 8 || spend[4].Average != 8 {
		t.Fatalf("month = %+v", spend[4])
	}
}

func TestAutomatedSessionsAreHiddenUntilShown(t *testing.T) {
	batch := sample(t.TempDir())
	batch.Sessions[1].Automated = true
	s := New(now.AddDate(0, 0, -30))
	s.Add(batch)
	if rows := s.ByCLI(); len(rows) != 1 || rows[0].Label != "claude" || s.Overall().CostUSD != 3 || s.AutomatedSessions() != 1 {
		t.Fatalf("hidden: rows = %+v overall = %+v", rows, s.Overall())
	}
	if spend := s.Spend(now); spend[4].Current != 3 {
		t.Fatalf("hidden month spend = %v", spend[4].Current)
	}
	s.ShowAutomated = true
	if rows := s.ByCLI(); len(rows) != 2 || s.Overall().CostUSD != 8 {
		t.Fatalf("shown: rows = %+v", rows)
	}
}

func TestAveragesNeedAWindowAtLeastAsLongAsThePeriod(t *testing.T) {
	s := New(now.Add(-2 * time.Hour))
	s.Add(sample(t.TempDir()))
	spend := s.Spend(now)
	if !spend[1].HasAverage || spend[2].HasAverage || spend[4].HasAverage {
		t.Fatalf("spend = %+v", spend)
	}
}

func TestTheCurrentHourStartsOnTheLocalHour(t *testing.T) {
	india := time.FixedZone("IST", 5*3600+1800)
	at := time.Date(2026, 10, 22, 10, 45, 0, 0, india)
	s := New(at.AddDate(0, 0, -30))
	s.Add(trail.Batch{
		Sessions: []trail.Session{{Key: "claude:a", CLI: trail.Claude}},
		Events: []trail.Event{
			{Session: "claude:a", Kind: trail.KindUsage, Time: at.Add(-50 * time.Minute), Usage: &trail.Usage{CostUSD: 1, Priced: true}},
			{Session: "claude:a", Kind: trail.KindUsage, Time: at.Add(-30 * time.Minute), Usage: &trail.Usage{CostUSD: 2, Priced: true}},
		},
	})
	if hour := s.Spend(at)[1]; hour.Current != 2 {
		t.Fatalf("hour = %+v", hour)
	}
}

func TestResetForgetsTheRewrittenSessionsOldTotals(t *testing.T) {
	s := New(now.AddDate(0, 0, -30))
	s.Add(sample(t.TempDir()))
	s.Add(trail.Batch{
		Reset:  []string{"claude:a"},
		Events: []trail.Event{{Session: "claude:a", Kind: trail.KindUser, Time: now.Add(-time.Minute)}},
	})
	clis := s.ByCLI()
	if len(clis) != 2 || clis[0].Label != "codex" || clis[1].Label != "claude" || clis[1].Prompts != 1 || clis[1].CostUSD != 0 {
		t.Fatalf("by CLI = %+v", clis)
	}
	if spend := s.Spend(now); spend[2].Current != 0 {
		t.Fatalf("day spend = %v", spend[2].Current)
	}
}
