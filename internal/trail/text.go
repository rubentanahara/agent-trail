package trail

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

const (
	FailedPrefix  = "FAILED "
	SummaryLimit  = 240
	clipScanBytes = SummaryLimit * 2 * utf8.UTFMax
)

func Clip(text string) string {
	runes := []rune(leadingBytes(text, clipScanBytes))
	if len(runes) > SummaryLimit*2 {
		runes = runes[:SummaryLimit*2]
	}
	collapsed := strings.Join(strings.FieldsFunc(string(runes), unicode.IsSpace), " ")
	runes = []rune(collapsed)
	if len(runes) > SummaryLimit {
		runes = runes[:SummaryLimit]
	}
	return string(runes)
}

func leadingBytes(text string, limit int) string {
	if len(text) <= limit {
		return text
	}
	for limit > 0 && !utf8.RuneStart(text[limit]) {
		limit--
	}
	return text[:limit]
}

func failedPrefix(failed bool) string {
	if failed {
		return FailedPrefix
	}
	return ""
}

func rawText(raw json.RawMessage) string {
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}
	var text string
	if json.Unmarshal(raw, &text) == nil {
		return text
	}
	return compactJSON(raw)
}

func compactJSON(raw json.RawMessage) string {
	var out bytes.Buffer
	if json.Compact(&out, raw) != nil {
		return string(raw)
	}
	return out.String()
}

var pathArgumentKeys = []string{"file_path", "notebook_path", "path"}

func pathArguments(raw json.RawMessage) []string {
	var arguments map[string]json.RawMessage
	if json.Unmarshal(raw, &arguments) != nil {
		return nil
	}
	var paths []string
	for _, key := range pathArgumentKeys {
		var path string
		if json.Unmarshal(arguments[key], &path) == nil && path != "" {
			paths = append(paths, path)
		}
	}
	return paths
}

func argumentsText(raw json.RawMessage) string {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	if token, err := decoder.Token(); err != nil || token != json.Delim('{') {
		return rawText(raw)
	}
	var lines []string
	for decoder.More() {
		key, err := decoder.Token()
		if err != nil {
			return rawText(raw)
		}
		var value json.RawMessage
		if decoder.Decode(&value) != nil {
			return rawText(raw)
		}
		lines = append(lines, fmt.Sprintf("%v: %s", key, rawText(value)))
	}
	return strings.Join(lines, "\n")
}

type textBlock struct {
	Text string `json:"text"`
}

func joinedText(raw json.RawMessage) string {
	var blocks []textBlock
	if json.Unmarshal(raw, &blocks) != nil {
		return ""
	}
	parts := make([]string, 0, len(blocks))
	for _, block := range blocks {
		if block.Text != "" {
			parts = append(parts, block.Text)
		}
	}
	return strings.Join(parts, " ")
}

func parseTime(value string) (time.Time, bool) {
	if value == "" {
		return time.Time{}, false
	}
	parsed, err := time.Parse(time.RFC3339Nano, value)
	return parsed, err == nil
}

func Tokens(n int64) string {
	switch {
	case n >= 1_000_000:
		return fmt.Sprintf("%.1fM", float64(n)/1_000_000)
	case n >= 1_000:
		return fmt.Sprintf("%.1fk", float64(n)/1_000)
	default:
		return fmt.Sprintf("%d", n)
	}
}

func Dollars(usd float64) string {
	if usd < 0.01 && usd > 0 {
		return fmt.Sprintf("$%.4f", usd)
	}
	return fmt.Sprintf("$%.2f", usd)
}

const (
	subCent      = 0.01
	subCentLabel = "<$0.01"
)

func ShortDollars(usd float64) string {
	if usd > 0 && usd < subCent {
		return subCentLabel
	}
	return fmt.Sprintf("$%.2f", usd)
}

func (u Usage) Line() string {
	var line strings.Builder
	line.WriteString(u.Model)
	fmt.Fprintf(&line, " in %s", Tokens(u.Input))
	if u.CacheRead > 0 || u.CacheWrite > 0 {
		fmt.Fprintf(&line, " (cache read %s, write %s)", Tokens(u.CacheRead), Tokens(u.CacheWrite))
	}
	fmt.Fprintf(&line, " out %s", Tokens(u.Output))
	if u.Reasoning > 0 {
		fmt.Fprintf(&line, " (reasoning %s)", Tokens(u.Reasoning))
	}
	if u.Priced {
		fmt.Fprintf(&line, " %s", Dollars(u.CostUSD))
	}
	if u.Duration > 0 {
		fmt.Fprintf(&line, " %s", u.Duration.Round(time.Millisecond))
	}
	return strings.TrimSpace(line.String())
}
