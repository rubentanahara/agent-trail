package savings

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"time"
)

const (
	Headroom   = "headroom"
	RTK        = "rtk"
	AnyCLI     = "any CLI"
	rtkTimeout = 10 * time.Second
)

type Saving struct {
	Tool   string
	CLI    string
	Tokens int64
	USD    float64
	HasUSD bool
	Since  time.Time
}

func Load(home string, since time.Time) []Saving {
	var found []Saving
	if saved, ok := headroomTotal(filepath.Join(home, ".headroom", "proxy_savings.json")); ok {
		found = append(found, saved)
	}
	if saved, ok := rtkSavings(since); ok {
		found = append(found, saved)
	}
	sort.SliceStable(found, func(i, j int) bool { return found[i].Tokens > found[j].Tokens })
	return found
}

type headroomSummary struct {
	Lifetime *struct {
		TokensSaved *int64  `json:"tokens_saved"`
		SavingsUSD  float64 `json:"compression_savings_usd"`
	} `json:"lifetime"`
	Metrics struct {
		StartedAt time.Time `json:"started_at"`
	} `json:"lifetime_metrics"`
}

func headroomTotal(path string) (Saving, bool) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return Saving{}, false
	}
	var summary headroomSummary
	if json.Unmarshal(raw, &summary) != nil || summary.Lifetime == nil || summary.Lifetime.TokensSaved == nil {
		return Saving{}, false
	}
	return Saving{
		Tool: Headroom, CLI: AnyCLI, Since: summary.Metrics.StartedAt,
		Tokens: *summary.Lifetime.TokensSaved, USD: summary.Lifetime.SavingsUSD, HasUSD: true,
	}, true
}

type rtkReport struct {
	Daily []struct {
		Date  string `json:"date"`
		Saved int64  `json:"saved_tokens"`
	} `json:"daily"`
}

func rtkSavings(since time.Time) (Saving, bool) {
	ctx, cancel := context.WithTimeout(context.Background(), rtkTimeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, RTK, "gain", "--daily", "--format", "json").Output()
	if err != nil {
		return Saving{}, false
	}
	return rtkTotal(out, since)
}

func rtkTotal(report []byte, since time.Time) (Saving, bool) {
	var parsed rtkReport
	if json.Unmarshal(report, &parsed) != nil {
		return Saving{}, false
	}
	firstDay := since.UTC().Format(time.DateOnly)
	saving := Saving{Tool: RTK, CLI: AnyCLI}
	for _, day := range parsed.Daily {
		if since.IsZero() || day.Date >= firstDay {
			saving.Tokens += day.Saved
		}
	}
	return saving, true
}
