package trail

import (
	"slices"
	"strings"
	"time"
)

type CLI string

const (
	Claude  CLI = "claude"
	Codex   CLI = "codex"
	Copilot CLI = "copilot"
)

type Kind string

const (
	KindUser       Kind = "user"
	KindContext    Kind = "context"
	KindAssistant  Kind = "assistant"
	KindThinking   Kind = "thinking"
	KindTool       Kind = "tool"
	KindResult     Kind = "result"
	KindSystem     Kind = "system"
	KindAttachment Kind = "attachment"
	KindUsage      Kind = "usage"
	KindSession    Kind = "session"
	KindPermission Kind = "permission"
	KindHook       Kind = "hook"
	KindEdit       Kind = "edit"
	KindTurn       Kind = "turn"
	KindError      Kind = "error"
	KindAgent      Kind = "agent"
	KindUnparsed   Kind = "unparsed"
)

type Event struct {
	Time    time.Time
	CLI     CLI
	Agent   string
	Kind    Kind
	Text    string
	Summary string
	Session string
	Usage   *Usage
	Source  Source
}

type Source struct {
	Path   string
	Offset int64
	Index  int
}

func (e Event) Who() string {
	if e.Agent == "" {
		return string(e.CLI)
	}
	return string(e.CLI) + "/" + e.Agent
}

type Usage struct {
	Model        string
	Input        int64
	CacheRead    int64
	CacheWrite   int64
	CacheWrite1h int64
	Output       int64
	Reasoning    int64
	Fast         bool
	CostUSD      float64
	Priced       bool
	Duration     time.Duration
}

type Liveness string

const (
	LivenessUnknown Liveness = ""
	LivenessRunning Liveness = "running"
	LivenessClosed  Liveness = "closed"
)

type Billing string

const (
	BillingUnknown      Billing = ""
	BillingSubscription Billing = "subscription"
	BillingAPI          Billing = "api"
)

type Session struct {
	Key          string
	ID           string
	CLI          CLI
	Name         string
	Agent        string
	Subagents    []string
	Model        string
	Branch       string
	Cwd          string
	Permission   string
	Billing      Billing
	Plan         string
	Start        time.Time
	Last         time.Time
	Events       int
	Calls        int
	Tokens       Usage
	PricedCalls  int
	LinesAdded   int
	LinesRemoved int

	PremiumRequests  float64
	ReportedCostUSD  float64
	HasReportedCost  bool
	Liveness         Liveness
	Activity         string
	PID              int
	TurnOpen         bool
	RateLimitPercent float64
	HasRateLimit     bool
	ContextPercent   float64
	HasContext       bool

	nameRank int
}

func (s *Session) setName(name string, rank int) {
	if name == "" || rank < s.nameRank {
		return
	}
	s.Name, s.nameRank = name, rank
}

const fallbackNameLimit = 60

func (s *Session) setFallbackName(prompt string) {
	if s.Name != "" || prompt == "" || strings.HasPrefix(prompt, "<") {
		return
	}
	name := []rune(Clip(prompt))
	if len(name) > fallbackNameLimit {
		name = append(name[:fallbackNameLimit-1], '…')
	}
	s.Name = string(name)
}

func (s *Session) addSubagent(name string) {
	if name != "" && !slices.Contains(s.Subagents, name) {
		s.Subagents = append(s.Subagents, name)
	}
}

func (s *Session) addUsage(u Usage) {
	s.Calls++
	s.Tokens.Input += u.Input
	s.Tokens.CacheRead += u.CacheRead
	s.Tokens.CacheWrite += u.CacheWrite
	s.Tokens.Output += u.Output
	s.Tokens.Reasoning += u.Reasoning
	s.Tokens.Duration += u.Duration
	if u.Priced {
		s.PricedCalls++
		s.Tokens.CostUSD += u.CostUSD
	}
}

func (s *Session) touch(t time.Time) {
	if s.Start.IsZero() || t.Before(s.Start) {
		s.Start = t
	}
	if t.After(s.Last) {
		s.Last = t
	}
}

func (s Session) clone() Session {
	s.Subagents = slices.Clone(s.Subagents)
	return s
}
