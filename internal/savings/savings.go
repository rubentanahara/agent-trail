package savings

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/rubentanahara/agent-trail/internal/trail"
)

const (
	Headroom     = "headroom"
	RTK          = "rtk"
	AnyCLI       = "any CLI"
	rtkDayLayout = "2006-01-02"
	rtkTimeout   = 10 * time.Second
	maxLineBytes = 1 << 20
)

var knownCLIs = []trail.CLI{trail.Claude, trail.Codex, trail.Copilot}

type Saving struct {
	Tool   string
	CLI    string
	Tokens int64
	USD    float64
	HasUSD bool
}

func Load(home string, since time.Time) []Saving {
	found := headroomSavings(filepath.Join(home, ".headroom", "savings_events.jsonl"), since)
	if saved, ok := rtkSavings(since); ok {
		found = append(found, saved)
	}
	sort.SliceStable(found, func(i, j int) bool { return found[i].Tokens > found[j].Tokens })
	return found
}

type headroomEvent struct {
	Timestamp string  `json:"ts"`
	Saved     int64   `json:"saved"`
	CostUSD   float64 `json:"cost_usd"`
	Client    string  `json:"client"`
}

func headroomSavings(path string, since time.Time) []Saving {
	file, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer file.Close()
	byCLI := map[string]*Saving{}
	var order []string
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, maxLineBytes), maxLineBytes)
	for scanner.Scan() {
		var event headroomEvent
		if json.Unmarshal(scanner.Bytes(), &event) != nil {
			continue
		}
		at, err := time.Parse(time.RFC3339Nano, event.Timestamp)
		if err != nil || at.Before(since) {
			continue
		}
		cli := cliOf(event.Client)
		if byCLI[cli] == nil {
			byCLI[cli] = &Saving{Tool: Headroom, CLI: cli, HasUSD: true}
			order = append(order, cli)
		}
		byCLI[cli].Tokens += event.Saved
		byCLI[cli].USD += event.CostUSD
	}
	out := make([]Saving, 0, len(order))
	for _, cli := range order {
		out = append(out, *byCLI[cli])
	}
	return out
}

func cliOf(client string) string {
	for _, cli := range knownCLIs {
		if strings.Contains(client, string(cli)) {
			return string(cli)
		}
	}
	return client
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
	firstDay := since.UTC().Format(rtkDayLayout)
	saving := Saving{Tool: RTK, CLI: AnyCLI}
	for _, day := range parsed.Daily {
		if since.IsZero() || day.Date >= firstDay {
			saving.Tokens += day.Saved
		}
	}
	return saving, true
}
