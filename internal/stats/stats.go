package stats

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/rubentanahara/agent-trail/internal/trail"
)

const (
	UnknownProject  = "(no working directory recorded)"
	secondsPerMin   = 60
	gitMarker       = ".git"
	daysPerWeek     = 7
	averageMonthLen = 30 * 24 * time.Hour
)

type Totals struct {
	Sessions  int
	Prompts   int
	Calls     int
	Input     int64
	CacheRead int64
	Output    int64
	CostUSD   float64
}

func (t *Totals) add(other Totals) {
	t.Sessions += other.Sessions
	t.Prompts += other.Prompts
	t.Calls += other.Calls
	t.Input += other.Input
	t.CacheRead += other.CacheRead
	t.Output += other.Output
	t.CostUSD += other.CostUSD
}

func (t Totals) CacheReadShare() float64 {
	if t.Input == 0 {
		return 0
	}
	return float64(t.CacheRead) / float64(t.Input)
}

type Row struct {
	Label string
	Totals
}

type Count struct {
	Label string
	N     int
}

type PeriodSpend struct {
	Name    string
	Current float64
	Average float64
}

type Stats struct {
	since      time.Time
	sessions   map[string]trail.Session
	perSession map[string]*Totals
	extensions map[string]int
	spend      map[int64]float64
	roots      map[string]string
}

func New(since time.Time) *Stats {
	return &Stats{
		since:      since,
		sessions:   map[string]trail.Session{},
		perSession: map[string]*Totals{},
		extensions: map[string]int{},
		spend:      map[int64]float64{},
		roots:      map[string]string{},
	}
}

func (s *Stats) Add(batch trail.Batch) {
	for _, session := range batch.Sessions {
		s.sessions[session.Key] = session
	}
	for _, event := range batch.Events {
		s.addEvent(event)
	}
}

func (s *Stats) addEvent(event trail.Event) {
	totals := s.perSession[event.Session]
	if totals == nil {
		totals = &Totals{Sessions: 1}
		s.perSession[event.Session] = totals
	}
	if event.Kind == trail.KindUser {
		totals.Prompts++
	}
	for _, file := range event.Files {
		if extension := strings.ToLower(filepath.Ext(file)); extension != "" {
			s.extensions[extension]++
		}
	}
	usage := event.Usage
	if usage == nil {
		return
	}
	totals.Calls++
	totals.Input += usage.Input
	totals.CacheRead += usage.CacheRead
	totals.Output += usage.Output
	if usage.Priced {
		totals.CostUSD += usage.CostUSD
		s.spend[event.Time.Unix()/secondsPerMin] += usage.CostUSD
	}
}

func (s *Stats) Overall() Totals {
	var overall Totals
	for _, totals := range s.perSession {
		overall.add(*totals)
	}
	return overall
}

func (s *Stats) ByCLI() []Row {
	return s.group(func(session trail.Session) string { return string(session.CLI) })
}

func (s *Stats) ByProject() []Row {
	return s.group(func(session trail.Session) string { return s.project(session.Cwd) })
}

func (s *Stats) group(label func(trail.Session) string) []Row {
	byLabel := map[string]*Row{}
	for key, totals := range s.perSession {
		name := label(s.sessions[key])
		row := byLabel[name]
		if row == nil {
			row = &Row{Label: name}
			byLabel[name] = row
		}
		row.add(*totals)
	}
	rows := make([]Row, 0, len(byLabel))
	for _, row := range byLabel {
		rows = append(rows, *row)
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].Prompts != rows[j].Prompts {
			return rows[i].Prompts > rows[j].Prompts
		}
		return rows[i].CostUSD > rows[j].CostUSD
	})
	return rows
}

func (s *Stats) project(cwd string) string {
	if cwd == "" {
		return UnknownProject
	}
	if root, ok := s.roots[cwd]; ok {
		return root
	}
	root := gitRoot(cwd)
	s.roots[cwd] = root
	return root
}

func gitRoot(cwd string) string {
	for dir := cwd; ; dir = filepath.Dir(dir) {
		if _, err := os.Stat(filepath.Join(dir, gitMarker)); err == nil {
			return dir
		}
		if filepath.Dir(dir) == dir {
			return cwd
		}
	}
}

func (s *Stats) Extensions() []Count {
	counts := make([]Count, 0, len(s.extensions))
	for extension, n := range s.extensions {
		counts = append(counts, Count{Label: extension, N: n})
	}
	sort.Slice(counts, func(i, j int) bool {
		if counts[i].N != counts[j].N {
			return counts[i].N > counts[j].N
		}
		return counts[i].Label < counts[j].Label
	})
	return counts
}

func (s *Stats) Series(now time.Time, step time.Duration, count int) []float64 {
	values := make([]float64, count)
	for minute, cost := range s.spend {
		back := int(now.Sub(time.Unix(minute*secondsPerMin, 0)) / step)
		if back >= 0 && back < count {
			values[count-1-back] += cost
		}
	}
	return values
}

func (s *Stats) Spend(now time.Time) []PeriodSpend {
	window := now.Sub(s.windowStart())
	total := s.spentSince(time.Time{})
	periods := []struct {
		name   string
		start  time.Time
		length time.Duration
	}{
		{"minute", now.Truncate(time.Minute), time.Minute},
		{"hour", now.Truncate(time.Hour), time.Hour},
		{"day", midnight(now), 24 * time.Hour},
		{"week", midnight(now).AddDate(0, 0, -daysSinceMonday(now)), daysPerWeek * 24 * time.Hour},
		{"month", time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location()), averageMonthLen},
	}
	out := make([]PeriodSpend, 0, len(periods))
	for _, period := range periods {
		average := 0.0
		if window > 0 {
			average = total * float64(period.length) / float64(window)
		}
		out = append(out, PeriodSpend{Name: period.name, Current: s.spentSince(period.start), Average: average})
	}
	return out
}

func (s *Stats) windowStart() time.Time {
	if !s.since.IsZero() {
		return s.since
	}
	earliest := time.Time{}
	for minute := range s.spend {
		at := time.Unix(minute*secondsPerMin, 0)
		if earliest.IsZero() || at.Before(earliest) {
			earliest = at
		}
	}
	return earliest
}

func (s *Stats) spentSince(start time.Time) float64 {
	total := 0.0
	for minute, cost := range s.spend {
		if !time.Unix(minute*secondsPerMin, 0).Before(start.Truncate(time.Minute)) {
			total += cost
		}
	}
	return total
}

func midnight(at time.Time) time.Time {
	return time.Date(at.Year(), at.Month(), at.Day(), 0, 0, 0, 0, at.Location())
}

func daysSinceMonday(at time.Time) int {
	return (int(at.Weekday()) + daysPerWeek - 1) % daysPerWeek
}
