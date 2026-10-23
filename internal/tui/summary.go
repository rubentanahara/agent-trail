package tui

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/rubentanahara/agent-trail/internal/trail"
)

const (
	summaryTimeout      = 120 * time.Second
	summaryMaxChars     = 400_000
	summaryMaxBudgetUSD = "0.25"
	summaryAlias        = "haiku"
	summaryBinary       = "claude"
	summaryWaitDelay    = 5 * time.Second
	summaryHooksOff     = `{"disableAllHooks":true}`
	summarySelectNotice = "select a session to summarize"
	summaryPending      = "summarizing…"
	summarySystemPrompt = "You summarize a coding-agent conversation for the developer who ran it. Write exactly four sections with these headings: Goal, Done, Open, Next. Under each heading write short bullets that name concrete files, commands and decisions. No preamble and no closing remarks. If the conversation starts with the line \"" + trail.CutNotice + "\", make the first line of your answer: The start of the conversation was not included."
)

type summaryResult struct {
	Text    string
	CostUSD float64
}

type summaryRunner func(prompt string) (summaryResult, error)

type cachedSummary struct {
	result summaryResult
	events int
	at     time.Time
}

type summaryState struct {
	shown    bool
	key      string
	cache    map[string]cachedSummary
	failed   map[string]error
	pending  map[string]bool
	run      summaryRunner
	render   markdownRenderer
	rendered *markdownCache
}

type summaryMsg struct {
	key    string
	events int
	result summaryResult
	err    error
}

func (m *model) toggleSummary() tea.Cmd {
	s := &m.summary
	if s.shown && s.key == m.activeSession {
		s.shown = false
		m.refreshDetail()
		return nil
	}
	key := m.activeSession
	if key == "" {
		m.notice = summarySelectNotice
		return nil
	}
	s.shown, s.key = true, key
	defer m.refreshDetail()
	events := m.summaryEvents(key)
	if s.pending[key] || s.fresh(key, len(events)) {
		return nil
	}
	if s.pending == nil {
		s.pending, s.failed = map[string]bool{}, map[string]error{}
	}
	s.pending[key] = true
	delete(s.failed, key)
	return m.askSummary(key, events)
}

func (s summaryState) fresh(key string, events int) bool {
	cached, ok := s.cache[key]
	return ok && cached.events == events
}

func (m model) summaryEvents(key string) []trail.Event {
	var family []trail.Event
	for _, event := range m.events {
		if event.Session == key || m.sessions[event.Session].Parent == key {
			family = append(family, event)
		}
	}
	return family
}

func (m model) askSummary(key string, events []trail.Event) tea.Cmd {
	run := m.summary.run
	if run == nil {
		run = runClaude
	}
	return func() tea.Msg {
		result, err := run(trail.Conversation(events, summaryMaxChars).Text)
		return summaryMsg{key: key, events: len(events), result: result, err: err}
	}
}

func (m *model) finishSummary(msg summaryMsg) {
	s := &m.summary
	delete(s.pending, msg.key)
	if msg.err != nil {
		s.failed[msg.key] = msg.err
	} else {
		if s.cache == nil {
			s.cache = map[string]cachedSummary{}
		}
		s.cache[msg.key] = cachedSummary{result: msg.result, events: msg.events, at: time.Now()}
	}
	m.refreshDetail()
}

func (m model) summaryVisible() bool {
	return m.summary.shown && m.summary.key == m.activeSession
}

func (m model) summaryTitle() string {
	title := "Summary: " + sessionLabel(m.sessions[m.summary.key]) + " · " + summaryAlias
	if cached, ok := m.summary.cache[m.summary.key]; ok {
		title += " · " + trail.Dollars(cached.result.CostUSD) + " · " + age(cached.at)
	}
	return title
}

func (m model) summaryContent() string {
	s := m.summary
	if err := s.failed[s.key]; err != nil {
		return failedStyle.Render(err.Error())
	}
	if s.pending[s.key] {
		return faintStyle.Render(summaryPending)
	}
	return m.matchingLines(m.markdown(s.cache[s.key].result.Text))
}

func summaryArgs() []string {
	return []string{
		"-p", "--model", summaryAlias, "--no-session-persistence", "--tools", "",
		"--output-format", "json", "--max-budget-usd", summaryMaxBudgetUSD,
		"--system-prompt", summarySystemPrompt,
		"--settings", summaryHooksOff, "--setting-sources", "", "--disable-slash-commands",
	}
}

type summaryOutput struct {
	IsError bool    `json:"is_error"`
	Result  string  `json:"result"`
	CostUSD float64 `json:"total_cost_usd"`
}

func decodeSummary(stdout []byte) (summaryResult, error) {
	var out summaryOutput
	if err := json.Unmarshal(stdout, &out); err != nil {
		return summaryResult{}, fmt.Errorf("unreadable output from claude: %w", err)
	}
	if out.IsError {
		return summaryResult{}, errors.New(out.Result)
	}
	return summaryResult{Text: strings.TrimSpace(out.Result), CostUSD: out.CostUSD}, nil
}

func commandFailure(runErr error, stderr string) error {
	if text := strings.TrimSpace(stderr); text != "" {
		return errors.New(text)
	}
	return runErr
}

func runClaude(prompt string) (summaryResult, error) {
	return runSummaryCommand(summaryBinary, prompt)
}

func summaryCommand(ctx context.Context, name string) *exec.Cmd {
	command := exec.CommandContext(ctx, name, summaryArgs()...)
	command.Dir = os.TempDir()
	command.WaitDelay = summaryWaitDelay
	return command
}

func runSummaryCommand(name, prompt string) (summaryResult, error) {
	ctx, cancel := context.WithTimeout(context.Background(), summaryTimeout)
	defer cancel()
	command := summaryCommand(ctx, name)
	command.Stdin = strings.NewReader(prompt)
	var stdout, stderr bytes.Buffer
	command.Stdout, command.Stderr = &stdout, &stderr
	runErr := command.Run()
	if ctx.Err() != nil {
		return summaryResult{}, errors.New("claude did not answer within " + summaryTimeout.String())
	}
	if runErr != nil {
		return summaryResult{}, commandFailure(runErr, stderr.String())
	}
	return decodeSummary(stdout.Bytes())
}
