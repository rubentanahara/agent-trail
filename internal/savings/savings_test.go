package savings

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

var since = time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)

func TestHeadroomShowsItsLifetimeSummary(t *testing.T) {
	path := filepath.Join(t.TempDir(), "proxy_savings.json")
	summary := `{"lifetime":{"tokens_saved":101565249,"compression_savings_usd":3824.5},"lifetime_metrics":{"started_at":"2026-08-01T16:44:15Z"},"history":[]}`
	if err := os.WriteFile(path, []byte(summary), 0o600); err != nil {
		t.Fatal(err)
	}
	got, ok := headroomTotal(path)
	want := Saving{Tool: Headroom, CLI: AnyCLI, Tokens: 101565249, USD: 3824.5, HasUSD: true, Since: time.Date(2026, 8, 1, 16, 44, 15, 0, time.UTC)}
	if !ok || got != want {
		t.Fatalf("got %+v ok=%v", got, ok)
	}
	if _, ok := headroomTotal(filepath.Join(t.TempDir(), "missing.json")); ok {
		t.Fatal("missing file reported savings")
	}
	renamed := filepath.Join(t.TempDir(), "proxy_savings.json")
	if err := os.WriteFile(renamed, []byte(`{"totals":{"saved":5}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if got, ok := headroomTotal(renamed); ok {
		t.Fatalf("unknown schema reported %+v", got)
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
