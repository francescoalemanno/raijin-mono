package oneshot

import (
	"bytes"
	"regexp"
	"strings"

	"github.com/alecthomas/chroma/v2/quick"
	"github.com/charmbracelet/lipgloss"
)

var (
	mdHeadingRE       = regexp.MustCompile(`^(#{1,6})\s+(.*)$`)
	mdQuoteRE         = regexp.MustCompile(`^\s*>\s?(.*)$`)
	mdListRE          = regexp.MustCompile(`^(\s*)([-*+]|\d+[.)])\s+(.*)$`)
	mdLinkRE          = regexp.MustCompile(`\[([^\]]+)\]\(([^)]+)\)`)
	mdCodeRE          = regexp.MustCompile("`([^`]+)`")
	mdBoldRE          = regexp.MustCompile(`\*\*([^*]+)\*\*|__([^_]+)__`)
	mdItalicRE        = regexp.MustCompile(`\*([^*]+)\*|_([^_]+)_`)
	mdFenceRE         = regexp.MustCompile("^\\s*```([a-zA-Z0-9_+-]*)\\s*$")
	mdTableSepCellRE  = regexp.MustCompile(`^:?-{3,}:?$`)
	mdTableMarkerChar = '|'
)

const fixedTableColumnWidth = 18

const (
	tableOutputSep     = "│"
	tableOutputRuleSeg = "─"
)

type lineMarkdownRenderer struct {
	inFence bool
	lang    string

	headingStyles []lipgloss.Style
	quoteStyle    lipgloss.Style
	listStyle     lipgloss.Style
	fenceStyle    lipgloss.Style
	inlineCode    lipgloss.Style
	boldStyle     lipgloss.Style
	italicStyle   lipgloss.Style
	linkStyle     lipgloss.Style
	codeFallback  lipgloss.Style
}

func newLineMarkdownRenderer() *lineMarkdownRenderer {
	return &lineMarkdownRenderer{
		headingStyles: []lipgloss.Style{
			oneshotAccentStyle.Bold(true),
			oneshotAccentStyle.Bold(true),
			oneshotWarningStyle.Bold(true),
			oneshotWarningStyle.Bold(true),
			oneshotNormalStyle.Bold(true),
			oneshotNormalStyle.Bold(true),
		},
		quoteStyle:   oneshotMutedStyle,
		listStyle:    oneshotAccentStyle.Bold(true),
		fenceStyle:   oneshotMutedStyle,
		inlineCode:   oneshotWarningStyle,
		boldStyle:    lipgloss.NewStyle().Bold(true),
		italicStyle:  lipgloss.NewStyle().Italic(true),
		linkStyle:    oneshotAccentStyle.Underline(true),
		codeFallback: oneshotWarningStyle,
	}
}

func (r *lineMarkdownRenderer) RenderLine(line string) string {
	line = strings.TrimRight(line, "\r")

	if matches := mdFenceRE.FindStringSubmatch(line); matches != nil {
		if r.inFence {
			r.inFence = false
			r.lang = ""
		} else {
			r.inFence = true
			r.lang = strings.TrimSpace(matches[1])
		}
		return r.fenceStyle.Render(strings.TrimSpace(line))
	}

	if r.inFence {
		return r.renderCodeLine(line)
	}
	return r.renderMarkdownLine(line)
}

func (r *lineMarkdownRenderer) renderCodeLine(line string) string {
	if strings.TrimSpace(line) == "" {
		return ""
	}
	if strings.TrimSpace(r.lang) == "" {
		return r.codeFallback.Render(line)
	}

	var out bytes.Buffer
	if err := quick.Highlight(&out, line, r.lang, "terminal16m", markdownCodeStyleName()); err != nil {
		return r.codeFallback.Render(line)
	}
	return strings.TrimRight(out.String(), "\n")
}

func markdownCodeStyleName() string {
	if lipgloss.HasDarkBackground() {
		return "monokai"
	}
	return "github"
}

func (r *lineMarkdownRenderer) renderMarkdownLine(line string) string {
	if strings.TrimSpace(line) == "" {
		return ""
	}
	if cells, ok := parseMarkdownTableCells(line); ok {
		return r.renderTableLine(cells)
	}

	if m := mdHeadingRE.FindStringSubmatch(line); m != nil {
		level := len(m[1])
		text := r.renderInline(strings.TrimSpace(m[2]))
		idx := min(max(level, 1), len(r.headingStyles)) - 1
		return r.headingStyles[idx].Render(text)
	}

	if m := mdQuoteRE.FindStringSubmatch(line); m != nil {
		body := r.renderInline(strings.TrimSpace(m[1]))
		return r.quoteStyle.Render("│ ") + body
	}

	if m := mdListRE.FindStringSubmatch(line); m != nil {
		indent := m[1]
		marker := m[2]
		body := r.renderInline(strings.TrimSpace(m[3]))
		return indent + r.listStyle.Render(marker) + " " + body
	}

	return r.renderInline(line)
}

func parseMarkdownTableCells(line string) ([]string, bool) {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" {
		return nil, false
	}
	pipeCount := strings.Count(trimmed, string(mdTableMarkerChar))
	if pipeCount < 1 {
		return nil, false
	}

	hasLeading := strings.HasPrefix(trimmed, string(mdTableMarkerChar))
	hasTrailing := strings.HasSuffix(trimmed, string(mdTableMarkerChar))

	parts := strings.Split(trimmed, string(mdTableMarkerChar))
	start := 0
	end := len(parts)
	if hasLeading {
		start++
	}
	if hasTrailing {
		end--
	}
	minCells := 2
	if hasLeading && hasTrailing {
		minCells = 1
	}
	if end-start < minCells {
		return nil, false
	}

	cells := make([]string, 0, end-start)
	for _, part := range parts[start:end] {
		cells = append(cells, strings.TrimSpace(part))
	}
	return cells, true
}

func (r *lineMarkdownRenderer) renderTableLine(cells []string) string {
	if len(cells) == 0 {
		return ""
	}
	if isTableSeparatorRow(cells) {
		var out strings.Builder
		out.WriteString(tableOutputSep)
		for _, cell := range cells {
			out.WriteString(" ")
			out.WriteString(renderTableSeparatorCell(cell, fixedTableColumnWidth))
			out.WriteString(" ")
			out.WriteString(tableOutputSep)
		}
		return out.String()
	}

	wrapped := make([][]string, len(cells))
	maxLines := 1
	for i, cell := range cells {
		wrapped[i] = r.wrapToWidth(cell, fixedTableColumnWidth)
		if len(wrapped[i]) > maxLines {
			maxLines = len(wrapped[i])
		}
	}

	var out strings.Builder
	for row := 0; row < maxLines; row++ {
		out.WriteString(tableOutputSep)
		for col := range cells {
			segment := ""
			if row < len(wrapped[col]) {
				segment = wrapped[col][row]
			}
			out.WriteString(" ")
			if segment != "" {
				rendered := r.renderInline(segment)
				out.WriteString(rendered)
				if pad := fixedTableColumnWidth - r.inlineDisplayWidth(segment); pad > 0 {
					out.WriteString(strings.Repeat(" ", pad))
				}
			} else {
				out.WriteString(strings.Repeat(" ", fixedTableColumnWidth))
			}
			out.WriteString(" ")
			out.WriteString(tableOutputSep)
		}
		if row < maxLines-1 {
			out.WriteString("\n")
		}
	}
	return out.String()
}

func isTableSeparatorRow(cells []string) bool {
	if len(cells) == 0 {
		return false
	}
	for _, cell := range cells {
		if !mdTableSepCellRE.MatchString(cell) {
			return false
		}
	}
	return true
}

func renderTableSeparatorCell(cell string, width int) string {
	if width <= 0 {
		return ""
	}
	left := strings.HasPrefix(cell, ":")
	right := strings.HasSuffix(cell, ":")
	switch {
	case left && right:
		if width == 1 {
			return ":"
		}
		if width == 2 {
			return "::"
		}
		return ":" + strings.Repeat(tableOutputRuleSeg, width-2) + ":"
	case left:
		if width == 1 {
			return ":"
		}
		return ":" + strings.Repeat(tableOutputRuleSeg, width-1)
	case right:
		if width == 1 {
			return ":"
		}
		return strings.Repeat(tableOutputRuleSeg, width-1) + ":"
	default:
		return strings.Repeat(tableOutputRuleSeg, width)
	}
}

func (r *lineMarkdownRenderer) wrapToWidth(s string, width int) []string {
	if width <= 0 {
		return []string{""}
	}
	trimmed := strings.TrimSpace(s)
	if trimmed == "" {
		return []string{""}
	}
	words := strings.Fields(trimmed)
	if len(words) == 0 {
		return []string{""}
	}

	lines := make([]string, 0, 2)
	current := ""
	for _, word := range words {
		if current == "" {
			if r.inlineDisplayWidth(word) <= width {
				current = word
				continue
			}
			parts := r.splitTokenByWidth(word, width)
			if len(parts) > 1 {
				lines = append(lines, parts[:len(parts)-1]...)
			}
			current = parts[len(parts)-1]
			continue
		}

		candidate := current + " " + word
		if r.inlineDisplayWidth(candidate) <= width {
			current = candidate
			continue
		}
		lines = append(lines, current)
		if r.inlineDisplayWidth(word) <= width {
			current = word
			continue
		}
		parts := r.splitTokenByWidth(word, width)
		if len(parts) > 1 {
			lines = append(lines, parts[:len(parts)-1]...)
		}
		current = parts[len(parts)-1]
	}

	if current != "" {
		lines = append(lines, current)
	}
	if len(lines) == 0 {
		return []string{""}
	}
	return lines
}

func (r *lineMarkdownRenderer) splitTokenByWidth(token string, width int) []string {
	if width <= 0 || token == "" {
		return []string{""}
	}

	parts := make([]string, 0, 2)
	current := ""

	for _, ch := range token {
		next := current + string(ch)
		if r.inlineDisplayWidth(next) <= width {
			current = next
			continue
		}

		if current != "" {
			parts = append(parts, current)
			current = string(ch)
			if r.inlineDisplayWidth(current) > width {
				parts = append(parts, current)
				current = ""
			}
			continue
		}
		parts = append(parts, string(ch))
	}
	if current != "" {
		parts = append(parts, current)
	}

	if len(parts) == 0 {
		return []string{""}
	}
	return parts
}

func (r *lineMarkdownRenderer) inlineDisplayWidth(s string) int {
	return lipgloss.Width(r.renderInlinePlain(s))
}

func (r *lineMarkdownRenderer) renderInlinePlain(line string) string {
	out := mdLinkRE.ReplaceAllStringFunc(line, func(m string) string {
		sub := mdLinkRE.FindStringSubmatch(m)
		if len(sub) != 3 {
			return m
		}
		return sub[1] + " (" + sub[2] + ")"
	})

	out = mdCodeRE.ReplaceAllStringFunc(out, func(m string) string {
		sub := mdCodeRE.FindStringSubmatch(m)
		if len(sub) != 2 {
			return m
		}
		return sub[1]
	})

	out = mdBoldRE.ReplaceAllStringFunc(out, func(m string) string {
		sub := mdBoldRE.FindStringSubmatch(m)
		switch {
		case len(sub) > 1 && sub[1] != "":
			return sub[1]
		case len(sub) > 2 && sub[2] != "":
			return sub[2]
		default:
			return m
		}
	})

	out = mdItalicRE.ReplaceAllStringFunc(out, func(m string) string {
		sub := mdItalicRE.FindStringSubmatch(m)
		switch {
		case len(sub) > 1 && sub[1] != "":
			return sub[1]
		case len(sub) > 2 && sub[2] != "":
			return sub[2]
		default:
			return m
		}
	})

	return out
}

func (r *lineMarkdownRenderer) renderInline(line string) string {
	out := mdLinkRE.ReplaceAllStringFunc(line, func(m string) string {
		sub := mdLinkRE.FindStringSubmatch(m)
		if len(sub) != 3 {
			return m
		}
		return r.linkStyle.Render(sub[1]) + " (" + sub[2] + ")"
	})

	out = mdCodeRE.ReplaceAllStringFunc(out, func(m string) string {
		sub := mdCodeRE.FindStringSubmatch(m)
		if len(sub) != 2 {
			return m
		}
		return r.inlineCode.Render(sub[1])
	})

	out = mdBoldRE.ReplaceAllStringFunc(out, func(m string) string {
		sub := mdBoldRE.FindStringSubmatch(m)
		text := ""
		switch {
		case len(sub) > 1 && sub[1] != "":
			text = sub[1]
		case len(sub) > 2 && sub[2] != "":
			text = sub[2]
		default:
			return m
		}
		return r.boldStyle.Render(text)
	})

	out = mdItalicRE.ReplaceAllStringFunc(out, func(m string) string {
		sub := mdItalicRE.FindStringSubmatch(m)
		text := ""
		switch {
		case len(sub) > 1 && sub[1] != "":
			text = sub[1]
		case len(sub) > 2 && sub[2] != "":
			text = sub[2]
		default:
			return m
		}
		return r.italicStyle.Render(text)
	})

	return out
}
