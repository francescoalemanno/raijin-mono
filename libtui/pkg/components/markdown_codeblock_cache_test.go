package components

import (
	"strings"
	"testing"
)

func identity(s string) string { return s }

func testMarkdownTheme(highlight func(code, lang string) []string) MarkdownTheme {
	return MarkdownTheme{
		Heading:         identity,
		Link:            identity,
		LinkURL:         identity,
		Code:            identity,
		CodeBlock:       identity,
		CodeBlockBorder: identity,
		Quote:           identity,
		QuoteBorder:     identity,
		HR:              identity,
		ListBullet:      identity,
		Bold:            identity,
		Italic:          identity,
		Strikethrough:   identity,
		Underline:       identity,
		HighlightCode:   highlight,
	}
}

func TestMarkdownCodeBlockCache_WidthChangeDoesNotRehighlight(t *testing.T) {
	t.Parallel()

	calls := 0
	theme := testMarkdownTheme(func(code, lang string) []string {
		calls++
		return strings.Split(code, "\n")
	})
	md := NewMarkdown("```go\nfmt.Println(1)\n```\n", 0, 0, theme, nil)

	_ = md.Render(80)
	_ = md.Render(60)

	if calls != 1 {
		t.Fatalf("highlight calls = %d, want 1", calls)
	}
}

func TestMarkdownCodeBlockCache_UnchangedCompletedBlockNotRehighlighted(t *testing.T) {
	t.Parallel()

	calls := 0
	theme := testMarkdownTheme(func(code, lang string) []string {
		calls++
		return strings.Split(code, "\n")
	})
	md := NewMarkdown("```go\na\n```\n", 0, 0, theme, nil)

	_ = md.Render(80)
	md.SetText("```go\na\n```\n\nsome trailing text")
	_ = md.Render(80)

	if calls != 1 {
		t.Fatalf("highlight calls = %d, want 1", calls)
	}
}

func TestMarkdownCodeBlockCache_OnlyChangedBlockRehighlighted(t *testing.T) {
	t.Parallel()

	calls := 0
	theme := testMarkdownTheme(func(code, lang string) []string {
		calls++
		return strings.Split(code, "\n")
	})
	md := NewMarkdown(
		"```go\na\n```\n\n```go\nb\n```\n",
		0,
		0,
		theme,
		nil,
	)

	_ = md.Render(80)
	md.SetText("```go\na\n```\n\n```go\nb2\n```\n")
	_ = md.Render(80)

	if calls != 3 {
		t.Fatalf("highlight calls = %d, want 3 (2 initial + 1 changed)", calls)
	}
}
