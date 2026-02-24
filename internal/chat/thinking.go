package chat

import (
	"fmt"
	"strings"

	"github.com/francescoalemanno/raijin-mono/libtui/pkg/components"
	"github.com/francescoalemanno/raijin-mono/libtui/pkg/tui"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/text"

	"github.com/francescoalemanno/raijin-mono/internal/theme"
)

const thinkingPreviewLines = 4

var (
	thinkingMutedBold       = theme.AnsiBold(0xA8, 0x99, 0x84)
	thinkingMutedItalic     = theme.AnsiItalic(0xA8, 0x99, 0x84)
	thinkingMutedBoldItalic = theme.AnsiBoldItalic(0xA8, 0x99, 0x84)
)

// inlineFormat applies basic markdown formatting (**bold** and *italic*) to text.
// It processes each line individually so formatting never spans across lines.
func inlineFormat(text string) string {
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		lines[i] = inlineFormatLine(line)
	}
	return strings.Join(lines, "\n")
}

// inlineFormatLine applies markdown inline formatting to a single line.
// It uses Goldmark parsing and only styles emphasis/strong text.
func inlineFormatLine(line string) string {
	if line == "" {
		return ""
	}

	source := []byte(line)
	doc := goldmark.New().Parser().Parse(text.NewReader(source))

	var out strings.Builder
	renderInlineAST(&out, doc, source, 0)
	return out.String()
}

type inlineStyle uint8

const (
	styleBold inlineStyle = 1 << iota
	styleItalic
)

func renderInlineAST(out *strings.Builder, node ast.Node, source []byte, style inlineStyle) {
	for child := node.FirstChild(); child != nil; child = child.NextSibling() {
		switch n := child.(type) {
		case *ast.Text:
			txt := string(n.Value(source))
			if n.SoftLineBreak() || n.HardLineBreak() {
				txt += "\n"
			}
			out.WriteString(applyInlineStyle(txt, style))
		case *ast.String:
			out.WriteString(applyInlineStyle(string(n.Value), style))
		case *ast.Emphasis:
			nextStyle := style
			if n.Level >= 2 {
				nextStyle |= styleBold
			} else {
				nextStyle |= styleItalic
			}
			renderInlineAST(out, n, source, nextStyle)
		default:
			renderInlineAST(out, n, source, style)
		}
	}
}

func applyInlineStyle(txt string, style inlineStyle) string {
	if txt == "" {
		return ""
	}

	switch style {
	case styleBold | styleItalic:
		return thinkingMutedBoldItalic(txt)
	case styleBold:
		return thinkingMutedBold(txt)
	case styleItalic:
		return thinkingMutedItalic(txt)
	default:
		return theme.ColorMuted(txt)
	}
}

// ThinkingComponent renders a thinking trace in an expandable block.
type ThinkingComponent struct {
	text   string
	done   bool
	status *StatusBlock
}

func NewThinking(ui components.UILike) *ThinkingComponent {
	t := &ThinkingComponent{
		status: NewStatusBlock(ui, theme.ColorMuted, theme.ColorAccent, ""),
	}
	t.updateContent()
	return t
}

func (t *ThinkingComponent) SetText(text string) {
	t.text = text
	t.updateContent()
}

func (t *ThinkingComponent) Finish() {
	t.done = true
	t.status.Transition(StatusSuccess, t.buildContent())
}

func (t *ThinkingComponent) SetExpanded(expanded bool) {
	t.status.SetExpanded(expanded)
	t.updateContent()
}

func (t *ThinkingComponent) IsExpanded() bool {
	return t.status.IsExpanded()
}

func (t *ThinkingComponent) updateContent() {
	t.status.SetText(t.buildContent())
}

func (t *ThinkingComponent) buildContent() string {
	header := "⟳ " + theme.ColorToolTitle("Thinking…")
	if t.done {
		header = "✓ " + theme.ColorToolTitle("Thinking")
	}

	body := strings.TrimSpace(t.text)
	if body == "" {
		return header
	}

	body = inlineFormat(body)
	body = t.truncate(body)
	return header + "\n" + body
}

func (t *ThinkingComponent) truncate(content string) string {
	lines := strings.Split(content, "\n")
	if t.status.IsExpanded() || len(lines) <= thinkingPreviewLines {
		return content
	}
	preview := strings.Join(lines[:thinkingPreviewLines], "\n")
	remaining := len(lines) - thinkingPreviewLines
	hint := theme.ColorMuted(fmt.Sprintf("… (%d more lines, press 'ctrl+o' to expand)", remaining))
	return preview + "\n" + hint
}

func (t *ThinkingComponent) Render(width int) []string { return t.status.Render(width) }
func (t *ThinkingComponent) HandleInput(data string)   {}
func (t *ThinkingComponent) Invalidate()               { t.status.Invalidate() }

var _ tui.Component = (*ThinkingComponent)(nil)
