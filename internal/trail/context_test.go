package trail

import (
	"testing"
	"time"

	"github.com/rubentanahara/agent-trail/internal/pricing"
)

const (
	limitedModelPrices   = `{"claude-opus-5-5":{"input_cost_per_token":0.000005,"output_cost_per_token":0.000025,"max_input_tokens":1000000}}`
	unlimitedModelPrices = `{"claude-opus-5-5":{"input_cost_per_token":0.000005,"output_cost_per_token":0.000025}}`
)

func parsedPrices(t *testing.T, raw string) pricing.Table {
	t.Helper()
	table, err := pricing.Parse([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	return table
}

func claudeSessionWith(t *testing.T, prices string, inputs ...string) Session {
	t.Helper()
	f := newFixture(t)
	lines := []string{claudePrompt("hi", testRepo, stampNow)}
	for i, input := range inputs {
		id := "msg_" + string(rune('a'+i))
		lines = append(lines, assistantLine(id, `{"input_tokens":`+input+`,"output_tokens":1}`))
	}
	f.write(claudeDir+"a.jsonl", lines...)
	return New(f.home, testRepo, parsedPrices(t, prices)).Start(time.Time{}).Sessions[0]
}

func TestClaudeContextIsTheLastPromptOverTheListedLimit(t *testing.T) {
	session := claudeSessionWith(t, limitedModelPrices, "100000", "250000")
	if !session.HasContext || session.ContextPercent != 25 {
		t.Fatalf("context = %v (has %v), want 25", session.ContextPercent, session.HasContext)
	}
}

func TestClaudeContextIsHiddenWhenThePriceListHasNoLimit(t *testing.T) {
	session := claudeSessionWith(t, unlimitedModelPrices, "250000")
	if session.HasContext {
		t.Fatalf("context = %v, want none", session.ContextPercent)
	}
}

func TestCodexKeepsItsOwnContextFigure(t *testing.T) {
	f := newFixture(t)
	f.write(codexDir+"rollout-c-00000000-0000-0000-0000-000000000007.jsonl",
		codexMeta(testRepo),
		`{"timestamp":"`+stampNow+`","type":"turn_context","payload":{"model":"claude-opus-5-5"}}`,
		`{"timestamp":"`+stampNow+`","type":"event_msg","payload":{"type":"token_count","info":{"last_token_usage":{"input_tokens":10},"model_context_window":100}}}`,
		`{"timestamp":"`+stampLater+`","type":"token_usage_record","payload":{"usage":{"input_tokens":500000,"output_tokens":1}}}`,
	)
	session := New(f.home, testRepo, parsedPrices(t, limitedModelPrices)).Start(time.Time{}).Sessions[0]
	if !session.HasContext || session.ContextPercent != 10 {
		t.Fatalf("context = %v (has %v), want 10", session.ContextPercent, session.HasContext)
	}
}
