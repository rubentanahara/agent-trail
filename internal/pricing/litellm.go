package pricing

import (
	"encoding/json"
	"regexp"
	"sort"
	"strconv"
)

const (
	tokensPerThousand = 1000
	sourceLiteLLM     = "litellm"
	sourceOverride    = "override"
)

var (
	firstPartyProviders = map[string]bool{"anthropic": true, "openai": true}
	tierField           = regexp.MustCompile(`^(input_cost_per_token|output_cost_per_token|cache_read_input_token_cost|cache_creation_input_token_cost|cache_creation_input_token_cost_above_1hr)_above_(\d+)k_tokens$`)
)

type entry map[string]json.RawMessage

func Parse(raw []byte) (Table, error) {
	prices, err := parseTable(raw, sourceOverride, nil)
	return Table{prices: prices}, err
}

func parseTable(raw []byte, source string, providers map[string]bool) (map[string]Price, error) {
	var entries map[string]entry
	if err := json.Unmarshal(raw, &entries); err != nil {
		return nil, err
	}
	prices := map[string]Price{}
	for model, fields := range entries {
		if providers != nil && !providers[fields.text("litellm_provider")] {
			continue
		}
		if price, ok := fields.price(source); ok {
			prices[model] = price
		}
	}
	return prices, nil
}

func (e entry) number(name string) *float64 {
	raw, present := e[name]
	if !present {
		return nil
	}
	var value float64
	if json.Unmarshal(raw, &value) != nil {
		return nil
	}
	return &value
}

func (e entry) text(name string) string {
	var value string
	_ = json.Unmarshal(e[name], &value)
	return value
}

func (e entry) price(source string) (Price, bool) {
	base := Rates{
		Input:        e.number("input_cost_per_token"),
		Output:       e.number("output_cost_per_token"),
		CacheRead:    e.number("cache_read_input_token_cost"),
		CacheWrite5m: e.number("cache_creation_input_token_cost"),
		CacheWrite1h: e.number("cache_creation_input_token_cost_above_1hr"),
	}
	if base.Input == nil && base.Output == nil {
		return Price{}, false
	}
	return Price{Base: base, Tiers: e.tiers(), FastMultiplier: e.fastMultiplier(), Source: source}, true
}

func (e entry) tiers() []Tier {
	byThreshold := map[int64]*Rates{}
	for name := range e {
		match := tierField.FindStringSubmatch(name)
		if match == nil {
			continue
		}
		thousands, err := strconv.ParseInt(match[2], 10, 64)
		if err != nil {
			continue
		}
		threshold := thousands * tokensPerThousand
		if byThreshold[threshold] == nil {
			byThreshold[threshold] = &Rates{}
		}
		rates, value := byThreshold[threshold], e.number(name)
		switch match[1] {
		case "input_cost_per_token":
			rates.Input = value
		case "output_cost_per_token":
			rates.Output = value
		case "cache_read_input_token_cost":
			rates.CacheRead = value
		case "cache_creation_input_token_cost":
			rates.CacheWrite5m = value
		case "cache_creation_input_token_cost_above_1hr":
			rates.CacheWrite1h = value
		}
	}
	tiers := make([]Tier, 0, len(byThreshold))
	for threshold, rates := range byThreshold {
		tiers = append(tiers, Tier{AboveTokens: threshold, Rates: *rates})
	}
	sort.Slice(tiers, func(i, j int) bool { return tiers[i].AboveTokens < tiers[j].AboveTokens })
	return tiers
}

func (e entry) fastMultiplier() *float64 {
	var specific map[string]json.RawMessage
	if json.Unmarshal(e["provider_specific_entry"], &specific) != nil {
		return nil
	}
	var multiplier float64
	if json.Unmarshal(specific["fast"], &multiplier) != nil {
		return nil
	}
	return &multiplier
}
