package trail

import (
	"regexp"
	"strings"
)

const ClaudePricesAsOf = "2026-10-06"

const (
	tokensPerMillion         = 1_000_000
	cacheWrite5mMultiplier   = 1.25
	cacheWrite1hMultiplier   = 2.0
	fastModeMultiplier       = 2.0
	haikuLongPromptThreshold = 100_000
	haikuLongPromptFactor    = 5.0
)

type claudePrice struct {
	input     float64
	output    float64
	cacheRead float64
}

var claudePrices = map[string]claudePrice{
	"claude-fable-5-1":  {input: 10, output: 50, cacheRead: 0.25},
	"claude-mythos-5-1": {input: 10, output: 50, cacheRead: 0.25},
	"claude-fable-5":    {input: 10, output: 50, cacheRead: 1.00},
	"claude-opus-5-5":   {input: 4, output: 20, cacheRead: 0.20},
	"claude-opus-5":     {input: 5, output: 25, cacheRead: 0.50},
	"claude-opus-4-8":   {input: 5, output: 25, cacheRead: 0.50},
	"claude-opus-4-7":   {input: 5, output: 25, cacheRead: 0.50},
	"claude-opus-4-6":   {input: 5, output: 25, cacheRead: 0.50},
	"claude-sonnet-5-5": {input: 2, output: 10, cacheRead: 0.20},
	"claude-sonnet-5":   {input: 2, output: 10, cacheRead: 0.20},
	"claude-sonnet-4-6": {input: 3, output: 15, cacheRead: 0.30},
	"claude-haiku-5-5":  {input: 0.10, output: 0.50, cacheRead: 0.01},
	"claude-haiku-4-5":  {input: 1, output: 5, cacheRead: 0.10},
}

var modelSuffix = regexp.MustCompile(`(\[[^\]]*\]|-\d{8})$`)

func normalizeClaudeModel(model string) string {
	return modelSuffix.ReplaceAllString(strings.TrimSpace(model), "")
}

type claudeTokens struct {
	model        string
	input        int64
	output       int64
	cacheRead    int64
	cacheWrite5m int64
	cacheWrite1h int64
	fast         bool
}

func claudeCost(t claudeTokens) (float64, bool) {
	model := normalizeClaudeModel(t.model)
	price, known := claudePrices[model]
	if !known {
		return 0, false
	}
	factor := 1.0
	if t.fast {
		factor *= fastModeMultiplier
	}
	if model == "claude-haiku-5-5" && t.input+t.cacheRead+t.cacheWrite5m+t.cacheWrite1h > haikuLongPromptThreshold {
		factor *= haikuLongPromptFactor
	}
	dollars := float64(t.input)*price.input +
		float64(t.output)*price.output +
		float64(t.cacheRead)*price.cacheRead +
		float64(t.cacheWrite5m)*price.input*cacheWrite5mMultiplier +
		float64(t.cacheWrite1h)*price.input*cacheWrite1hMultiplier
	return dollars * factor / tokensPerMillion, true
}
