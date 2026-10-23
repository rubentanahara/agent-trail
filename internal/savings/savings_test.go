package savings

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

var since = time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)

func TestHeadroomSavingsAreSummedPerCLIWithinTheWindow(t *testing.T) {
	path := filepath.Join(t.TempDir(), "savings_events.jsonl")
	lines := []string{
		`{"ts":"2026-09-30T23:59:59+00:00","saved":1000,"cost_usd":1,"client":"claude-code"}`,
		`{"ts":"2026-10-02T10:00:00.5+00:00","saved":30,"cost_usd":0.25,"client":"claude-code"}`,
		`{"ts":"2026-10-02T11:00:00+00:00","saved":20,"cost_usd":0.25,"client":"claude"}`,
		`{"ts":"2026-10-03T11:00:00+00:00","saved":7,"cost_usd":0.1,"client":"cursor"}`,
		`not json`,
	}
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")), 0o600); err != nil {
		t.Fatal(err)
	}
	got := headroomSavings(path, since)
	want := []Saving{
		{Tool: Headroom, CLI: "claude", Tokens: 50, USD: 0.5, HasUSD: true},
		{Tool: Headroom, CLI: "cursor", Tokens: 7, USD: 0.1, HasUSD: true},
	}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("got %+v", got)
	}
}

func TestRTKSavingsCountOnlyDaysInTheWindow(t *testing.T) {
	report := []byte(`{"summary":{},"daily":[{"date":"2026-09-30","saved_tokens":500},{"date":"2026-10-01","saved_tokens":40},{"date":"2026-10-22","saved_tokens":2}]}`)
	got, ok := rtkTotal(report, since)
	if !ok || got != (Saving{Tool: RTK, CLI: AnyCLI, Tokens: 42}) {
		t.Fatalf("got %+v ok=%v", got, ok)
	}
	if all, _ := rtkTotal(report, time.Time{}); all.Tokens != 542 {
		t.Fatalf("whole history = %d", all.Tokens)
	}
}
