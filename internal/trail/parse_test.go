package trail

import (
	"math"
	"strings"
	"testing"
)

func parseOne(t *testing.T, p parser, line string) ([]Event, *Session) {
	t.Helper()
	session := &Session{}
	events, err := p.parse([]byte(line), session)
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}
	return events, session
}

func kinds(events []Event) []string {
	var out []string
	for _, event := range events {
		out = append(out, event.Who()+" "+string(event.Kind))
	}
	return out
}

func assertKinds(t *testing.T, events []Event, want ...string) {
	t.Helper()
	if got := strings.Join(kinds(events), ", "); got != strings.Join(want, ", ") {
		t.Fatalf("kinds = %q, want %q", got, strings.Join(want, ", "))
	}
}

const claudeStamp = `"timestamp":"2026-10-22T10:00:00.000Z","sessionId":"s"`

func claudeUserLine(content string, extra string) string {
	return `{"type":"user",` + claudeStamp + extra + `,"message":{"content":` + content + `}}`
}

func TestClaudeTypedPromptAndSlashCommandAreUser(t *testing.T) {
	for _, content := range []string{`"fix the bug"`, `"<command-name>/model</command-name>"`, `"<bash-input>make check</bash-input>"`} {
		events, _ := parseOne(t, newClaudeParser("a.jsonl"), claudeUserLine(content, ""))
		assertKinds(t, events, "claude user")
	}
}

func TestClaudeInjectedTextIsContext(t *testing.T) {
	for _, line := range []string{claudeUserLine(`"<bash-stdout>ok</bash-stdout>"`, ""), claudeUserLine(`"hook said no"`, `,"isMeta":true`)} {
		events, _ := parseOne(t, newClaudeParser("a.jsonl"), line)
		assertKinds(t, events, "claude context")
	}
}

func TestClaudePromptSentToSubagentIsContext(t *testing.T) {
	events, _ := parseOne(t, newClaudeParser("a.jsonl"), claudeUserLine(`"research X"`, `,"isSidechain":true`))
	assertKinds(t, events, "claude/sub context")
}

func TestClaudeMessageTypedMidTurnIsUser(t *testing.T) {
	line := `{"type":"attachment",` + claudeStamp + `,"attachment":{"type":"queued_command","prompt":"also this","origin":{"kind":"human"}}}`
	events, _ := parseOne(t, newClaudeParser("a.jsonl"), line)
	assertKinds(t, events, "claude user")
	if events[0].Text != "also this" {
		t.Fatalf("text = %q", events[0].Text)
	}
}

func TestClaudeToolErrorIsMarkedAndArrayContentJoined(t *testing.T) {
	line := claudeUserLine(`[{"type":"tool_result","is_error":true,"content":[{"type":"text","text":"boom"}]}]`, "")
	events, _ := parseOne(t, newClaudeParser("a.jsonl"), line)
	if len(events) != 1 || events[0].Kind != KindResult || events[0].Text != "FAILED boom" {
		t.Fatalf("events = %+v", events)
	}
}

func TestCopilotFailureShowsErrorMessage(t *testing.T) {
	line := `{"type":"tool.execution_complete","timestamp":"2026-10-22T10:00:00Z","data":{"success":false,"error":{"message":"Permission denied"}}}`
	events, _ := parseOne(t, newCopilotParser(""), line)
	if len(events) != 1 || events[0].Text != "FAILED Permission denied" {
		t.Fatalf("events = %+v", events)
	}
}

func TestCodexCommandWithoutExitCodeShowsStatus(t *testing.T) {
	line := `{"timestamp":"2026-10-22T10:00:00Z","type":"event_msg","payload":{"type":"item_completed","item":{"type":"CommandExecution","command":["zsh","-lc","ls"],"status":"declined","exit_code":null}}}`
	events, _ := parseOne(t, newCodexParser(""), line)
	if len(events) != 2 || events[0].Text != "exec ls" || events[1].Text != "FAILED declined " {
		t.Fatalf("events = %+v", events)
	}
}

func assistantLine(id string, usage string) string {
	return `{"type":"assistant",` + claudeStamp + `,"message":{"id":"` + id + `","model":"claude-opus-5-5","content":[{"type":"text","text":"hi"}],"usage":` + usage + `}}`
}

func TestClaudeUsageIsCountedOncePerMessage(t *testing.T) {
	p := newClaudeParser("a.jsonl")
	usage := `{"input_tokens":10,"output_tokens":5}`
	first, _ := parseOne(t, p, assistantLine("msg_1", usage))
	second, _ := parseOne(t, p, assistantLine("msg_1", usage))
	assertKinds(t, first, "claude assistant", "claude usage")
	assertKinds(t, second, "claude assistant")
}

func TestClaudeCostMatchesClaudeCodeReportedTotal(t *testing.T) {
	usage := `{"input_tokens":26,"output_tokens":8400,"cache_read_input_tokens":560692,"cache_creation_input_tokens":53595,"cache_creation":{"ephemeral_1h_input_tokens":53595,"ephemeral_5m_input_tokens":0}}`
	events, _ := parseOne(t, newClaudeParser("a.jsonl"), assistantLine("msg_1", usage))
	cost := events[len(events)-1].Usage.CostUSD
	if math.Abs(cost-0.7090024) > 1e-9 {
		t.Fatalf("cost = %v, want 0.7090024", cost)
	}
}

func TestClaudeUnknownModelIsNotPriced(t *testing.T) {
	line := strings.Replace(assistantLine("m", `{"input_tokens":1,"output_tokens":1}`), "claude-opus-5-5", "claude-future-9", 1)
	events, _ := parseOne(t, newClaudeParser("a.jsonl"), line)
	if events[len(events)-1].Usage.Priced {
		t.Fatal("unknown model must not be priced")
	}
}

func TestClaudeBillingComesFromOAuthBeta(t *testing.T) {
	cases := map[string]Billing{
		`{"type":"api-request","params":{"betas":["claude-code-20250219","oauth-2025-04-20"]}}`: BillingSubscription,
		`{"type":"api-request","params":{"betas":["claude-code-20250219"]}}`:                    BillingAPI,
	}
	for line, want := range cases {
		_, session := parseOne(t, newClaudeParser("a.jsonl"), line)
		if session.Billing != want {
			t.Fatalf("billing = %q, want %q for %s", session.Billing, want, line)
		}
	}
}

func TestClaudeCustomTitleOutranksAITitle(t *testing.T) {
	p, session := newClaudeParser("a.jsonl"), &Session{}
	for _, line := range []string{`{"type":"custom-title","customTitle":"mine"}`, `{"type":"ai-title","aiTitle":"generated"}`} {
		if _, err := p.parse([]byte(line), session); err != nil {
			t.Fatal(err)
		}
	}
	if session.Name != "mine" {
		t.Fatalf("name = %q", session.Name)
	}
}

func TestCodexBillingFollowsPlanType(t *testing.T) {
	subscription := `{"timestamp":"2026-10-22T10:00:00Z","type":"event_msg","payload":{"type":"token_count","rate_limits":{"plan_type":"plus","primary":{"used_percent":7}}}}`
	_, session := parseOne(t, newCodexParser(""), subscription)
	if session.Billing != BillingSubscription || session.Plan != "plus" || session.RateLimitPercent != 7 {
		t.Fatalf("session = %+v", session)
	}
	apiKey := `{"timestamp":"2026-10-22T10:00:00Z","type":"event_msg","payload":{"type":"token_count","rate_limits":{"plan_type":null}}}`
	_, session = parseOne(t, newCodexParser(""), apiKey)
	if session.Billing != BillingAPI {
		t.Fatalf("billing = %q", session.Billing)
	}
}

func TestCodexUsageUsesModelFromTurnContext(t *testing.T) {
	p := newCodexParser("")
	parseOne(t, p, `{"timestamp":"2026-10-22T10:00:00Z","type":"turn_context","payload":{"model":"gpt-6-luna","sandbox_policy":{"type":"read-only"}}}`)
	events, _ := parseOne(t, p, `{"timestamp":"2026-10-22T10:00:01Z","type":"token_usage_record","payload":{"response_id":"r1","usage":{"input_tokens":100,"cached_input_tokens":40,"output_tokens":7}}}`)
	if len(events) != 1 || events[0].Usage.Model != "gpt-6-luna" || events[0].Usage.CacheRead != 40 {
		t.Fatalf("events = %+v", events)
	}
}

func TestCopilotBYOKIsAPIBilling(t *testing.T) {
	line := `{"type":"session.usage_record","timestamp":"2026-10-22T10:00:00Z","data":{"usage":{"model":"m","inputTokens":5,"outputTokens":1,"isByok":true}}}`
	events, session := parseOne(t, newCopilotParser(""), line)
	if session.Billing != BillingAPI || len(events) != 1 || events[0].Kind != KindUsage {
		t.Fatalf("session = %+v events = %+v", session, events)
	}
}

func TestClipCollapsesWhitespaceAndLimitsRunes(t *testing.T) {
	text := "a  b\n\tc" + strings.Repeat("é", SummaryLimit*3)
	clipped := Clip(text)
	if !strings.HasPrefix(clipped, "a b c") || len([]rune(clipped)) != SummaryLimit {
		t.Fatalf("clip = %q (%d runes)", clipped[:10], len([]rune(clipped)))
	}
}

func TestToolArgumentsShowOneFieldPerLineWithRealNewlines(t *testing.T) {
	line := `{"type":"assistant",` + claudeStamp + `,"message":{"content":[{"type":"tool_use","name":"Bash","input":{"command":"make\ncheck","timeout":5}}]}}`
	events, _ := parseOne(t, newClaudeParser("a.jsonl"), line)
	if events[0].Text != "Bash command: make\ncheck\ntimeout: 5" {
		t.Fatalf("text = %q", events[0].Text)
	}
}

func TestFirstTypedPromptNamesAnUnnamedSession(t *testing.T) {
	session := &Session{}
	session.setFallbackName("<command-name>/clear</command-name>")
	session.setFallbackName("fix the login bug")
	session.setFallbackName("second prompt")
	session.setName("Custom", nameRankCustomTitle)
	if session.Name != "Custom" {
		t.Fatalf("name = %q", session.Name)
	}
	unnamed := &Session{}
	unnamed.setFallbackName("fix the login bug")
	unnamed.setFallbackName("second prompt")
	if unnamed.Name != "fix the login bug" {
		t.Fatalf("name = %q", unnamed.Name)
	}
}
