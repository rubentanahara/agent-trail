package tui

import (
	"regexp"
	"strings"

	"charm.land/glamour/v2"
	"charm.land/glamour/v2/ansi"
	"charm.land/glamour/v2/styles"
)

var trailingPadding = regexp.MustCompile(`( +)((?:\x1b\[[0-9;]*m)*)$`)

func summaryStyle() ansi.StyleConfig {
	style := styles.DarkStyleConfig
	noMargin := uint(0)
	style.Document.Margin = &noMargin
	for _, heading := range []*ansi.StyleBlock{&style.H1, &style.H2, &style.H3, &style.H4, &style.H5, &style.H6} {
		heading.Prefix = ""
	}
	return style
}

type markdownRenderer func(text string, width int) (string, error)

type markdownCache struct {
	text  string
	width int
	out   string
	valid bool
}

func glamourRender(text string, width int) (string, error) {
	r, err := glamour.NewTermRenderer(glamour.WithStyles(summaryStyle()), glamour.WithWordWrap(width))
	if err != nil {
		return "", err
	}
	return r.Render(text)
}

func trimMarkdown(rendered string) string {
	lines := strings.Split(rendered, "\n")
	for i, line := range lines {
		lines[i] = trimTrailingBlanks(line)
	}
	return strings.Trim(strings.Join(lines, "\n"), "\n")
}

func trimTrailingBlanks(line string) string {
	for {
		trimmed := trailingPadding.ReplaceAllString(line, "$2")
		if trimmed == line {
			return line
		}
		line = trimmed
	}
}

func (m model) markdown(text string) string {
	return m.markdownIn(m.summary.rendered, text, m.detail.Width())
}

func (m model) markdownIn(cache *markdownCache, text string, width int) string {
	if cache.valid && cache.text == text && cache.width == width {
		return cache.out
	}
	render := m.summary.render
	if render == nil {
		render = glamourRender
	}
	out, err := render(text, width)
	if err != nil {
		return text
	}
	*cache = markdownCache{text: text, width: width, out: trimMarkdown(out), valid: true}
	return cache.out
}
