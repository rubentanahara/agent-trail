package tui

import (
	"strings"

	"charm.land/lipgloss/v2"
)

var (
	jsonKeyStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("75"))
	jsonStringStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("114"))
	jsonNumberStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("215"))
	jsonLiteralStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("176"))
)

func colorJSON(pretty string) string {
	var out strings.Builder
	runes := []rune(pretty)
	for i := 0; i < len(runes); {
		switch r := runes[i]; {
		case r == '"':
			end := stringEnd(runes, i)
			style := jsonStringStyle
			if isKey(runes, end) {
				style = jsonKeyStyle
			}
			out.WriteString(style.Render(string(runes[i:end])))
			i = end
		case r == '-' || (r >= '0' && r <= '9'):
			end := wordEnd(runes, i)
			out.WriteString(jsonNumberStyle.Render(string(runes[i:end])))
			i = end
		case r == 't' || r == 'f' || r == 'n':
			end := wordEnd(runes, i)
			out.WriteString(jsonLiteralStyle.Render(string(runes[i:end])))
			i = end
		default:
			out.WriteRune(r)
			i++
		}
	}
	return out.String()
}

func stringEnd(runes []rune, start int) int {
	for i := start + 1; i < len(runes); i++ {
		switch runes[i] {
		case '\\':
			i++
		case '"':
			return i + 1
		}
	}
	return len(runes)
}

func wordEnd(runes []rune, start int) int {
	i := start
	for i < len(runes) && !strings.ContainsRune(",]}: \n\t", runes[i]) {
		i++
	}
	return i
}

func isKey(runes []rune, after int) bool {
	for i := after; i < len(runes); i++ {
		if runes[i] != ' ' {
			return runes[i] == ':'
		}
	}
	return false
}
