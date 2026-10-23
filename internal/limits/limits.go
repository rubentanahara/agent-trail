package limits

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const (
	sourceCachedUsage = "claude.json"
	sourceStatusline  = "statusline"
)

type Window struct {
	Percent  float64
	ResetsAt time.Time
	Known    bool
}

type Claude struct {
	FiveHour, SevenDay Window
	AsOf               time.Time
	Source             string
}

type cachedFile struct {
	modified time.Time
	value    Claude
	found    bool
}

var (
	cacheLock sync.Mutex
	cache     = map[string]cachedFile{}
)

func Load(home string) (Claude, bool) {
	cached, cachedFound := cachedRead(filepath.Join(home, ".claude.json"), parseCachedUsage)
	statusline, statuslineFound := cachedRead(filepath.Join(home, ".claude", "abtop-rate-limits.json"), parseStatusline)
	switch {
	case cachedFound && statuslineFound && cached.AsOf.After(statusline.AsOf):
		return expired(cached, time.Now()), true
	case statuslineFound:
		return expired(statusline, time.Now()), true
	case cachedFound:
		return expired(cached, time.Now()), true
	}
	return Claude{}, false
}

func expired(c Claude, now time.Time) Claude {
	c.FiveHour = afterReset(c.FiveHour, now)
	c.SevenDay = afterReset(c.SevenDay, now)
	return c
}

func afterReset(w Window, now time.Time) Window {
	if w.Known && !w.ResetsAt.After(now) {
		w.Percent = 0
	}
	return w
}

func cachedRead(path string, parse func([]byte) (Claude, bool)) (Claude, bool) {
	info, err := os.Stat(path)
	if err != nil {
		return Claude{}, false
	}
	cacheLock.Lock()
	defer cacheLock.Unlock()
	if hit, ok := cache[path]; ok && hit.modified.Equal(info.ModTime()) {
		return hit.value, hit.found
	}
	value, found := readFile(path, parse)
	cache[path] = cachedFile{modified: info.ModTime(), value: value, found: found}
	return value, found
}

func readFile(path string, parse func([]byte) (Claude, bool)) (Claude, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Claude{}, false
	}
	return parse(data)
}

type cachedWindow struct {
	Utilization *float64 `json:"utilization"`
	ResetsAt    string   `json:"resets_at"`
}

type cachedUsageFile struct {
	Usage *struct {
		FetchedAtMs int64 `json:"fetchedAtMs"`
		Utilization struct {
			FiveHour cachedWindow `json:"five_hour"`
			SevenDay cachedWindow `json:"seven_day"`
		} `json:"utilization"`
	} `json:"cachedUsageUtilization"`
}

func parseCachedUsage(data []byte) (Claude, bool) {
	var file cachedUsageFile
	if json.Unmarshal(data, &file) != nil || file.Usage == nil {
		return Claude{}, false
	}
	usage := file.Usage
	claude := Claude{
		FiveHour: usage.Utilization.FiveHour.window(),
		SevenDay: usage.Utilization.SevenDay.window(),
		AsOf:     time.UnixMilli(usage.FetchedAtMs),
		Source:   sourceCachedUsage,
	}
	return claude, claude.FiveHour.Known || claude.SevenDay.Known
}

func (w cachedWindow) window() Window {
	resets, err := time.Parse(time.RFC3339Nano, w.ResetsAt)
	if w.Utilization == nil || err != nil {
		return Window{}
	}
	return Window{Percent: *w.Utilization, ResetsAt: resets, Known: true}
}

type statuslineWindow struct {
	UsedPercentage *float64 `json:"used_percentage"`
	ResetsAt       int64    `json:"resets_at"`
}

type statuslineFile struct {
	UpdatedAt int64            `json:"updated_at"`
	FiveHour  statuslineWindow `json:"five_hour"`
	SevenDay  statuslineWindow `json:"seven_day"`
}

func parseStatusline(data []byte) (Claude, bool) {
	var file statuslineFile
	if json.Unmarshal(data, &file) != nil {
		return Claude{}, false
	}
	claude := Claude{
		FiveHour: file.FiveHour.window(),
		SevenDay: file.SevenDay.window(),
		AsOf:     time.Unix(file.UpdatedAt, 0),
		Source:   sourceStatusline,
	}
	return claude, claude.FiveHour.Known || claude.SevenDay.Known
}

func (w statuslineWindow) window() Window {
	if w.UsedPercentage == nil || w.ResetsAt == 0 {
		return Window{}
	}
	return Window{Percent: *w.UsedPercentage, ResetsAt: time.Unix(w.ResetsAt, 0), Known: true}
}
