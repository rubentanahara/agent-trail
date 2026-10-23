package tui

import (
	"slices"
	"strings"
	"unicode"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

type term struct {
	text   string
	exact  bool
	negate bool
}

type query []term

func parseQuery(input string) query {
	var parsed query
	for _, field := range strings.Fields(strings.ToLower(input)) {
		switch {
		case strings.HasPrefix(field, "!") && len(field) > 1:
			parsed = append(parsed, term{text: field[1:], exact: true, negate: true})
		case strings.HasPrefix(field, "'") && len(field) > 1:
			parsed = append(parsed, term{text: field[1:], exact: true})
		default:
			parsed = append(parsed, term{text: field})
		}
	}
	return parsed
}

func (q query) matches(haystack string) bool {
	for _, t := range q {
		found := strings.Contains(haystack, t.text)
		if !t.exact {
			found = isSubsequence(haystack, t.text)
		}
		if found == t.negate {
			return false
		}
	}
	return true
}

func isSubsequence(haystack, needle string) bool {
	remaining := []rune(needle)
	for _, r := range haystack {
		if len(remaining) == 0 {
			return true
		}
		if r == remaining[0] {
			remaining = remaining[1:]
		}
	}
	return len(remaining) == 0
}

func (q query) highlight(line string, style lipgloss.Style) string {
	if len(q) == 0 {
		return line
	}
	plain := []rune(ansi.Strip(line))
	lower := make([]rune, len(plain))
	for i, r := range plain {
		lower[i] = unicode.ToLower(r)
	}
	marked := make([]bool, len(plain))
	for _, t := range q {
		if !t.negate {
			t.mark(lower, marked)
		}
	}
	return lipgloss.StyleRanges(line, markedRanges(plain, marked, style)...)
}

func (t term) mark(text []rune, marked []bool) {
	needle := []rune(t.text)
	if t.exact {
		markOccurrences(text, needle, marked)
		return
	}
	markSubsequence(text, needle, marked)
}

func markSubsequence(text, needle []rune, marked []bool) {
	found := make([]int, 0, len(needle))
	for i := 0; i < len(text) && len(found) < len(needle); i++ {
		if text[i] == needle[len(found)] {
			found = append(found, i)
		}
	}
	if len(found) < len(needle) {
		return
	}
	for _, i := range found {
		marked[i] = true
	}
}

func markOccurrences(text, needle []rune, marked []bool) {
	for i := 0; i+len(needle) <= len(text); i++ {
		if !slices.Equal(text[i:i+len(needle)], needle) {
			continue
		}
		for j := range needle {
			marked[i+j] = true
		}
		i += len(needle) - 1
	}
}

func markedRanges(text []rune, marked []bool, style lipgloss.Style) []lipgloss.Range {
	var ranges []lipgloss.Range
	column := 0
	for i, r := range text {
		width := ansi.StringWidth(string(r))
		if marked[i] {
			last := len(ranges) - 1
			if last >= 0 && ranges[last].End == column {
				ranges[last].End += width
			} else {
				ranges = append(ranges, lipgloss.NewRange(column, column+width, style))
			}
		}
		column += width
	}
	return ranges
}
