package pricing

import (
	"regexp"
	"strings"
)

type Rates struct {
	Input        *float64
	Output       *float64
	CacheRead    *float64
	CacheWrite5m *float64
	CacheWrite1h *float64
}

type Tier struct {
	AboveTokens int64
	Rates       Rates
}

type Price struct {
	Base           Rates
	Tiers          []Tier
	FastMultiplier *float64
	MaxInputTokens int64
	Source         string
}

type Tokens struct {
	Model        string
	Input        int64
	CacheRead    int64
	CacheWrite   int64
	CacheWrite1h int64
	Output       int64
	Fast         bool
}

type Table struct {
	prices    map[string]Price
	FetchedAt string
}

var (
	bracketSuffix = regexp.MustCompile(`\[[^\]]*\]$`)
	dateSuffix    = regexp.MustCompile(`-\d{4}-?\d{2}-?\d{2}$`)
)

func (t Table) Lookup(model string) (Price, bool) {
	for _, candidate := range candidates(model) {
		if price, ok := t.prices[candidate]; ok {
			return price, true
		}
	}
	return Price{}, false
}

func (t Table) ContextLimit(model string) (int64, bool) {
	price, ok := t.Lookup(model)
	return price.MaxInputTokens, ok && price.MaxInputTokens > 0
}

func candidates(model string) []string {
	base := strings.ToLower(strings.TrimSpace(model))
	base = bracketSuffix.ReplaceAllString(base, "")
	undated := dateSuffix.ReplaceAllString(base, "")
	dashed := strings.ReplaceAll(undated, ".", "-")
	return []string{base, undated, dashed}
}

func (t Table) Cost(tokens Tokens) (float64, bool) {
	price, ok := t.Lookup(tokens.Model)
	if !ok {
		return 0, false
	}
	return price.Cost(tokens)
}

func (p Price) Cost(tokens Tokens) (float64, bool) {
	cacheWrite5m := tokens.CacheWrite - tokens.CacheWrite1h
	uncached := tokens.Input - tokens.CacheRead - tokens.CacheWrite
	if uncached < 0 || cacheWrite5m < 0 {
		return 0, false
	}
	rates := p.ratesFor(tokens.Input)
	total := 0.0
	for _, part := range []struct {
		count int64
		rate  *float64
	}{
		{uncached, rates.Input},
		{tokens.Output, rates.Output},
		{tokens.CacheRead, rates.CacheRead},
		{cacheWrite5m, rates.CacheWrite5m},
		{tokens.CacheWrite1h, rates.CacheWrite1h},
	} {
		if part.count == 0 {
			continue
		}
		if part.rate == nil {
			return 0, false
		}
		total += float64(part.count) * *part.rate
	}
	if tokens.Fast {
		if p.FastMultiplier == nil {
			return 0, false
		}
		total *= *p.FastMultiplier
	}
	return total, true
}

func (p Price) ratesFor(promptTokens int64) Rates {
	rates := p.Base
	for _, tier := range p.Tiers {
		if promptTokens > tier.AboveTokens {
			rates = rates.overriddenBy(tier.Rates)
		}
	}
	return rates
}

func (r Rates) overriddenBy(other Rates) Rates {
	return Rates{
		Input:        firstSet(other.Input, r.Input),
		Output:       firstSet(other.Output, r.Output),
		CacheRead:    firstSet(other.CacheRead, r.CacheRead),
		CacheWrite5m: firstSet(other.CacheWrite5m, r.CacheWrite5m),
		CacheWrite1h: firstSet(other.CacheWrite1h, r.CacheWrite1h),
	}
}

func firstSet(values ...*float64) *float64 {
	for _, value := range values {
		if value != nil {
			return value
		}
	}
	return nil
}
