package pricing

import (
	"math"
	"testing"
)

const fixture = `{
  "claude-opus-5-5": {"litellm_provider": "anthropic", "input_cost_per_token": 4e-06, "output_cost_per_token": 2e-05,
    "cache_read_input_token_cost": 2e-07, "cache_creation_input_token_cost": 5e-06, "cache_creation_input_token_cost_above_1hr": 8e-06,
    "provider_specific_entry": {"fast": 2}},
  "small-model": {"litellm_provider": "anthropic", "input_cost_per_token": 1e-07, "output_cost_per_token": 5e-07,
    "input_cost_per_token_above_100k_tokens": 5e-07, "output_cost_per_token_above_100k_tokens": 2.5e-06},
  "no-cache-model": {"litellm_provider": "openai", "input_cost_per_token": 1e-06, "output_cost_per_token": 2e-06},
  "azure/claude-opus-5-5": {"litellm_provider": "azure", "input_cost_per_token": 1}
}`

func table(t *testing.T) Table {
	t.Helper()
	prices, err := parseTable([]byte(fixture), sourceLiteLLM, firstPartyProviders)
	if err != nil {
		t.Fatal(err)
	}
	return Table{prices: prices}
}

func assertCost(t *testing.T, got float64, priced bool, want float64) {
	t.Helper()
	if !priced || math.Abs(got-want) > 1e-9 {
		t.Fatalf("cost = %v (priced %v), want %v", got, priced, want)
	}
}

func TestCostMatchesClaudeCodeReportedTotal(t *testing.T) {
	cost, priced := table(t).Cost(Tokens{Model: "claude-opus-5-5[1m]", Input: 26 + 560692 + 53595, CacheRead: 560692, CacheWrite: 53595, CacheWrite1h: 53595, Output: 8400})
	assertCost(t, cost, priced, 0.7090024)
}

func TestFastModeAppliesTheListedMultiplier(t *testing.T) {
	cost, priced := table(t).Cost(Tokens{Model: "claude-opus-5-5", Input: 1_000_000, Fast: true})
	assertCost(t, cost, priced, 8)
}

func TestLongPromptUsesTheTierAboveItsThreshold(t *testing.T) {
	cost, priced := table(t).Cost(Tokens{Model: "small-model", Input: 200_000, Output: 1_000_000})
	assertCost(t, cost, priced, 200_000*5e-07+1_000_000*2.5e-06)
}

func TestMissingRateForUsedTokensLeavesCostUnpriced(t *testing.T) {
	if _, priced := table(t).Cost(Tokens{Model: "no-cache-model", Input: 100, CacheRead: 50}); priced {
		t.Fatal("cache reads without a cache-read rate must not be priced")
	}
	if _, priced := table(t).Cost(Tokens{Model: "small-model", Input: 10, Fast: true}); priced {
		t.Fatal("fast mode without a listed multiplier must not be priced")
	}
}

func TestLookupNormalisesSuffixesAndDots(t *testing.T) {
	for _, model := range []string{"claude-opus-5-5", "claude-opus-5-5[1m]", "claude-opus-5-5-20261001", "claude-opus-5.5"} {
		if _, ok := table(t).Lookup(model); !ok {
			t.Errorf("lookup %q failed", model)
		}
	}
	if _, ok := table(t).Lookup("unknown"); ok {
		t.Error("unknown model must not resolve")
	}
}

func TestThirdPartyProviderEntriesAreIgnored(t *testing.T) {
	if _, ok := table(t).prices["azure/claude-opus-5-5"]; ok {
		t.Fatal("azure entry must be ignored")
	}
}
