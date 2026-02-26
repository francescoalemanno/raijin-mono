package chat

import (
	"strings"

	"github.com/francescoalemanno/raijin-mono/libtui/pkg/components"
	"github.com/francescoalemanno/raijin-mono/libtui/pkg/tui"
	"github.com/francescoalemanno/raijin-mono/libtui/pkg/utils"

	"github.com/francescoalemanno/raijin-mono/internal/theme"
)

// MessageComponent renders text with a left border.
// For assistant messages it renders markdown; for others plain text.
type MessageComponent struct {
	content     string
	borderChar  string
	borderColor func(string) string
	bodyColor   func(string) string
	markdown    bool
	mdTheme     components.MarkdownTheme

	cachedContent string
	cachedWidth   int
	cachedLines   []string
}

func NewMessage(content string, borderChar string, borderColor, bodyColor func(string) string, markdown bool) *MessageComponent {
	content = strings.TrimSpace(content)

	m := &MessageComponent{
		content:     content,
		borderChar:  borderChar,
		borderColor: borderColor,
		bodyColor:   bodyColor,
		markdown:    markdown,
	}
	if markdown {
		m.mdTheme = defaultMarkdownTheme()
	}
	return m
}

func (m *MessageComponent) SetContent(content string) {
	content = strings.TrimSpace(content)
	if m.content != content {
		m.content = content
		m.cachedContent = ""
		m.cachedLines = nil
	}
}

func (m *MessageComponent) Invalidate() {
	m.cachedContent = ""
	m.cachedWidth = 0
	m.cachedLines = nil
}

func (m *MessageComponent) HandleInput(data string) {}

func (m *MessageComponent) Render(width int) []string {
	if width < 1 {
		width = 1
	}
	if m.cachedLines != nil && m.cachedContent == m.content && m.cachedWidth == width {
		return m.cachedLines
	}
	if m.content == "" {
		return []string{}
	}
	borderPrefix := m.borderColor(m.borderChar) + " "
	borderWidth := 2 // "│ " = 2 visible chars
	contentWidth := width - borderWidth
	if contentWidth < 4 {
		contentWidth = 4
	}

	var contentLines []string
	if m.markdown {
		defaultStyle := &components.DefaultTextStyle{
			Color: theme.Default.Foreground.Ansi24,
		}
		md := components.NewMarkdown(m.content, 0, 0, m.mdTheme, defaultStyle)
		contentLines = md.Render(contentWidth)
	} else {
		contentLines = utils.WrapTextWithAnsi(m.bodyColor(m.content), contentWidth)
	}

	if len(contentLines) == 0 {
		contentLines = []string{""}
	}

	result := make([]string, len(contentLines))
	for i, line := range contentLines {
		pad := contentWidth - utils.VisibleWidth(line)
		if pad < 0 {
			pad = 0
		}
		padding := theme.Default.Foreground.Ansi24(strings.Repeat(" ", pad))
		full := borderPrefix + line + padding
		result[i] = utils.TruncateToWidth(full, width, "")
	}

	m.cachedContent = m.content
	m.cachedWidth = width
	m.cachedLines = result
	return result
}

var _ tui.Component = (*MessageComponent)(nil)

func defaultMarkdownTheme() components.MarkdownTheme {
	chromaStyle := theme.Default.ChromaStyle()
	return components.MarkdownTheme{
		Heading:         theme.Default.Foreground.AnsiBold,
		Link:            theme.Default.Accent.Ansi24,
		LinkURL:         theme.Default.Muted.Ansi24,
		Code:            theme.Default.AccentAlt.Ansi24,
		CodeBlock:       theme.Default.Foreground.Ansi24,
		CodeBlockBorder: theme.Default.Muted.Ansi24,
		Quote:           theme.Default.Muted.Ansi24,
		QuoteBorder:     theme.Default.Muted.Ansi24,
		HR:              theme.Default.Muted.Ansi24,
		ListBullet:      theme.Default.Accent.Ansi24,
		Bold:            theme.Default.Foreground.AnsiBold,
		Italic:          theme.Default.Foreground.Ansi24,
		Strikethrough:   theme.Default.Muted.Ansi24,
		Underline:       theme.Default.Foreground.AnsiUnderline,
		HighlightCode: func(code string, lang string) []string {
			return utils.HighlightCodeLines(code, lang, "", chromaStyle)
		},
	}
}
