package test

import (
	"strings"
	"testing"

	"github.com/francescoalemanno/raijin-mono/libtui/pkg/components"
	"github.com/stretchr/testify/assert"
)

// defaultMarkdownTheme creates a simple theme for testing
func defaultMarkdownTheme() components.MarkdownTheme {
	return components.MarkdownTheme{
		Heading:         func(s string) string { return "\x1b[1;36m" + s + "\x1b[0m" },
		Link:            func(s string) string { return "\x1b[34m" + s + "\x1b[0m" },
		LinkURL:         func(s string) string { return "\x1b[2m" + s + "\x1b[0m" },
		Code:            func(s string) string { return "\x1b[33m" + s + "\x1b[0m" },
		CodeBlock:       func(s string) string { return "\x1b[32m" + s + "\x1b[0m" },
		CodeBlockBorder: func(s string) string { return "\x1b[2m" + s + "\x1b[0m" },
		Quote:           func(s string) string { return "\x1b[3m" + s + "\x1b[0m" },
		QuoteBorder:     func(s string) string { return "\x1b[2m" + s + "\x1b[0m" },
		HR:              func(s string) string { return "\x1b[2m" + s + "\x1b[0m" },
		ListBullet:      func(s string) string { return "\x1b[36m" + s + "\x1b[0m" },
		Bold:            func(s string) string { return "\x1b[1m" + s + "\x1b[0m" },
		Italic:          func(s string) string { return "\x1b[3m" + s + "\x1b[0m" },
		Strikethrough:   func(s string) string { return "\x1b[9m" + s + "\x1b[0m" },
		Underline:       func(s string) string { return "\x1b[4m" + s + "\x1b[0m" },
	}
}

// stripAnsi removes ANSI codes from text
func stripAnsi(text string) string {
	return strings.ReplaceAll(strings.ReplaceAll(text, "\x1b[0m", ""), "\x1b[", "")
}

// TestMarkdown_SimpleHeading tests basic heading rendering
func TestMarkdown_SimpleHeading(t *testing.T) {
	markdown := components.NewMarkdown(
		"# Hello World",
		0, 0,
		defaultMarkdownTheme(),
		nil,
	)

	lines := markdown.Render(80)
	assert.Greater(t, len(lines), 0)

	// Strip ANSI codes for checking
	plainLines := make([]string, len(lines))
	for i, line := range lines {
		plainLines[i] = stripAnsi(line)
	}

	// Check that we have a heading
	found := false
	for _, line := range plainLines {
		if strings.Contains(line, "Hello World") {
			found = true
			break
		}
	}
	assert.True(t, found, "Should contain 'Hello World'")
}

// TestMarkdown_NestedList tests nested list rendering
func TestMarkdown_NestedList(t *testing.T) {
	markdown := components.NewMarkdown(
		`- Item 1
  - Nested 1.1
  - Nested 1.2
- Item 2`,
		0, 0,
		defaultMarkdownTheme(),
		nil,
	)

	lines := markdown.Render(80)
	assert.Greater(t, len(lines), 0)

	// Strip ANSI codes and trim whitespace
	plainLines := make([]string, len(lines))
	for i, line := range lines {
		plainLines[i] = strings.TrimSpace(stripAnsi(line))
	}

	// Check structure
	assert.True(t, containsLine(plainLines, "- Item 1"), "Should contain '- Item 1'")
	assert.True(t, containsLine(plainLines, "- Nested 1.1"), "Should contain '- Nested 1.1'")
	assert.True(t, containsLine(plainLines, "- Nested 1.2"), "Should contain '- Nested 1.2'")
	assert.True(t, containsLine(plainLines, "- Item 2"), "Should contain '- Item 2'")
}

// TestMarkdown_OrderedList tests ordered list rendering
func TestMarkdown_OrderedList(t *testing.T) {
	markdown := components.NewMarkdown(
		`1. First
2. Second
3. Third`,
		0, 0,
		defaultMarkdownTheme(),
		nil,
	)

	lines := markdown.Render(80)
	assert.Greater(t, len(lines), 0)

	// Strip ANSI codes
	plainLines := make([]string, len(lines))
	for i, line := range lines {
		plainLines[i] = stripAnsi(line)
	}

	// Check structure
	assert.True(t, containsLine(plainLines, "1. First"), "Should contain '1. First'")
	assert.True(t, containsLine(plainLines, "2. Second"), "Should contain '2. Second'")
	assert.True(t, containsLine(plainLines, "3. Third"), "Should contain '3. Third'")
}

// TestMarkdown_CodeBlock tests code block rendering
func TestMarkdown_CodeBlock(t *testing.T) {
	markdown := components.NewMarkdown(
		"```go\nfmt.Println(\"Hello\")\n```",
		0, 0,
		defaultMarkdownTheme(),
		nil,
	)

	lines := markdown.Render(80)
	assert.Greater(t, len(lines), 0)

	// Strip ANSI codes
	plainLines := make([]string, len(lines))
	for i, line := range lines {
		plainLines[i] = stripAnsi(line)
	}

	// Check for code block markers
	assert.True(t, containsLine(plainLines, "```go"), "Should contain '```go'")
	assert.True(t, containsLine(plainLines, "fmt.Println"), "Should contain 'fmt.Println'")
	assert.True(t, containsLine(plainLines, "```"), "Should contain closing ```")
}

// TestMarkdown_Blockquote tests blockquote rendering
func TestMarkdown_Blockquote(t *testing.T) {
	markdown := components.NewMarkdown(
		"> This is a quote",
		0, 0,
		defaultMarkdownTheme(),
		nil,
	)

	lines := markdown.Render(80)
	assert.Greater(t, len(lines), 0)

	// Strip ANSI codes
	plainLines := make([]string, len(lines))
	for i, line := range lines {
		plainLines[i] = stripAnsi(line)
	}

	// Check for quote border
	found := false
	for _, line := range plainLines {
		if strings.Contains(line, "│") && strings.Contains(line, "This is a quote") {
			found = true
			break
		}
	}
	assert.True(t, found, "Should contain quote with border")
}

// TestMarkdown_InlineCode tests inline code rendering
func TestMarkdown_InlineCode(t *testing.T) {
	markdown := components.NewMarkdown(
		"Use `fmt.Println` for output",
		0, 0,
		defaultMarkdownTheme(),
		nil,
	)

	lines := markdown.Render(80)
	assert.Greater(t, len(lines), 0)

	// Strip ANSI codes
	plainLines := make([]string, len(lines))
	for i, line := range lines {
		plainLines[i] = stripAnsi(line)
	}

	// Check for inline code
	found := false
	for _, line := range plainLines {
		if strings.Contains(line, "fmt.Println") {
			found = true
			break
		}
	}
	assert.True(t, found, "Should contain 'fmt.Println'")
}

// TestMarkdown_BoldItalic tests bold and italic rendering
func TestMarkdown_BoldItalic(t *testing.T) {
	markdown := components.NewMarkdown(
		"**bold** and *italic*",
		0, 0,
		defaultMarkdownTheme(),
		nil,
	)

	lines := markdown.Render(80)
	assert.Greater(t, len(lines), 0)

	// Strip ANSI codes
	plainLines := make([]string, len(lines))
	for i, line := range lines {
		plainLines[i] = stripAnsi(line)
	}

	// Check content
	found := false
	for _, line := range plainLines {
		if strings.Contains(line, "bold") && strings.Contains(line, "italic") {
			found = true
			break
		}
	}
	assert.True(t, found, "Should contain 'bold' and 'italic'")
}

// TestMarkdown_Link tests link rendering
func TestMarkdown_Link(t *testing.T) {
	markdown := components.NewMarkdown(
		"[Link](https://example.com)",
		0, 0,
		defaultMarkdownTheme(),
		nil,
	)

	lines := markdown.Render(80)
	assert.Greater(t, len(lines), 0)

	// Strip ANSI codes
	plainLines := make([]string, len(lines))
	for i, line := range lines {
		plainLines[i] = stripAnsi(line)
	}

	// Check for link text and URL
	found := false
	for _, line := range plainLines {
		if strings.Contains(line, "Link") && strings.Contains(line, "https://example.com") {
			found = true
			break
		}
	}
	assert.True(t, found, "Should contain link text and URL")
}

// TestMarkdown_Table tests table rendering
func TestMarkdown_Table(t *testing.T) {
	markdown := components.NewMarkdown(
		`| Name | Age |
| --- | --- |
| Alice | 30 |
| Bob | 25 |`,
		0, 0,
		defaultMarkdownTheme(),
		nil,
	)

	lines := markdown.Render(80)
	assert.Greater(t, len(lines), 0)

	// Strip ANSI codes
	plainLines := make([]string, len(lines))
	for i, line := range lines {
		plainLines[i] = stripAnsi(line)
	}

	// Check for table structure
	assert.True(t, containsLine(plainLines, "Name"), "Should contain 'Name'")
	assert.True(t, containsLine(plainLines, "Age"), "Should contain 'Age'")
	assert.True(t, containsLine(plainLines, "Alice"), "Should contain 'Alice'")
	assert.True(t, containsLine(plainLines, "Bob"), "Should contain 'Bob'")
	// Check for table borders
	foundBorder := false
	for _, line := range plainLines {
		if strings.Contains(line, "│") || strings.Contains(line, "─") {
			foundBorder = true
			break
		}
	}
	assert.True(t, foundBorder, "Should contain table borders")
}

// TestMarkdown_EmptyText tests empty text handling
func TestMarkdown_EmptyText(t *testing.T) {
	markdown := components.NewMarkdown(
		"",
		0, 0,
		defaultMarkdownTheme(),
		nil,
	)

	lines := markdown.Render(80)
	assert.Equal(t, 0, len(lines), "Empty markdown should return empty slice")
}

// TestMarkdown_WhitespaceOnly tests whitespace-only text
func TestMarkdown_WhitespaceOnly(t *testing.T) {
	markdown := components.NewMarkdown(
		"   \n\t  ",
		0, 0,
		defaultMarkdownTheme(),
		nil,
	)

	lines := markdown.Render(80)
	assert.Equal(t, 0, len(lines), "Whitespace-only markdown should return empty slice")
}

func TestMarkdown_NestedCodeBlockUsesHighlightCallback(t *testing.T) {
	theme := defaultMarkdownTheme()
	theme.HighlightCode = func(code string, lang string) []string {
		return []string{"<hl>" + lang + ":" + strings.TrimSpace(code) + "</hl>"}
	}

	markdown := components.NewMarkdown(
		"- Item\n\n  ```go\n  fmt.Println(\"Hello\")\n  ```",
		0, 0,
		theme,
		nil,
	)

	lines := markdown.Render(80)
	assert.Greater(t, len(lines), 0)

	found := false
	for _, line := range lines {
		if strings.Contains(line, "<hl>go:") {
			found = true
			break
		}
	}
	assert.True(t, found, "nested fenced code should use HighlightCode callback")
}

func TestMarkdown_CodeBlockWithTabsDoesNotPanic(t *testing.T) {
	markdown := components.NewMarkdown(
		"\tintro\n\n```go\n\tfmt.Println(\"Hello\")\n```",
		0, 0,
		defaultMarkdownTheme(),
		nil,
	)

	assert.NotPanics(t, func() {
		lines := markdown.Render(80)
		assert.Greater(t, len(lines), 0)
	})
}

// TestMarkdown_HardLineBreak tests that hard line breaks (two spaces at end of line)
// are preserved and rendered as new lines.
func TestMarkdown_HardLineBreak(t *testing.T) {
	// In markdown, two spaces at end of line creates a hard line break
	markdown := components.NewMarkdown(
		"Line 1  \nLine 2",
		0, 0,
		defaultMarkdownTheme(),
		nil,
	)

	lines := markdown.Render(80)
	assert.Greater(t, len(lines), 0)

	// Strip ANSI codes
	plainLines := make([]string, len(lines))
	for i, line := range lines {
		plainLines[i] = stripAnsi(line)
	}

	// Should have two separate lines (hard line break should create a new line)
	foundLine1 := false
	foundLine2 := false
	for _, line := range plainLines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "Line 1" {
			foundLine1 = true
		}
		if trimmed == "Line 2" {
			foundLine2 = true
		}
	}
	assert.True(t, foundLine1, "Should contain 'Line 1'")
	assert.True(t, foundLine2, "Should contain 'Line 2'")
}

// TestMarkdown_HardLineBreakWithUnicode tests hard line breaks with unicode content
// (similar to how Codex outputs unicode bullet points with explicit newlines).
func TestMarkdown_HardLineBreakWithUnicode(t *testing.T) {
	// Unicode bullet with hard line break
	markdown := components.NewMarkdown(
		"• Item 1  \n• Item 2  \n• Item 3",
		0, 0,
		defaultMarkdownTheme(),
		nil,
	)

	lines := markdown.Render(80)
	assert.Greater(t, len(lines), 0)

	// Strip ANSI codes
	plainLines := make([]string, len(lines))
	for i, line := range lines {
		plainLines[i] = stripAnsi(line)
	}

	// All three items should be present
	found1 := false
	found2 := false
	found3 := false
	for _, line := range plainLines {
		if strings.Contains(line, "• Item 1") {
			found1 = true
		}
		if strings.Contains(line, "• Item 2") {
			found2 = true
		}
		if strings.Contains(line, "• Item 3") {
			found3 = true
		}
	}
	assert.True(t, found1, "Should contain '• Item 1'")
	assert.True(t, found2, "Should contain '• Item 2'")
	assert.True(t, found3, "Should contain '• Item 3'")
}

// Helper function
func containsLine(lines []string, substr string) bool {
	for _, line := range lines {
		if strings.Contains(line, substr) {
			return true
		}
	}
	return false
}
