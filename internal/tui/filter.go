package tui

import "strings"

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

func (q query) matchesExactly(haystack string) bool {
	for _, t := range q {
		if strings.Contains(haystack, t.text) == t.negate {
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
