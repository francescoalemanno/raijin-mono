package test

import (
	"testing"

	"github.com/francescoalemanno/raijin-mono/libtui/pkg/components"
	"github.com/francescoalemanno/raijin-mono/libtui/pkg/utils"
	"github.com/stretchr/testify/assert"
)

func TestTruncatedText_PadsOutputToExactWidth(t *testing.T) {
	text := components.NewTruncatedText("Hello world", 1, 0)
	lines := text.Render(50)

	// Should have exactly one content line (no vertical padding)
	assert.Equal(t, 1, len(lines))

	// Line should be exactly 50 visible characters
	visibleLen := utils.VisibleWidth(lines[0])
	assert.Equal(t, 50, visibleLen)
}

func TestTruncatedText_PadsWithVerticalPadding(t *testing.T) {
	text := components.NewTruncatedText("Hello", 0, 2)
	lines := text.Render(40)

	// Should have 2 padding lines + 1 content line + 2 padding lines = 5 total
	assert.Equal(t, 5, len(lines))

	// All lines should be exactly 40 characters
	for _, line := range lines {
		assert.Equal(t, 40, utils.VisibleWidth(line))
	}
}

func TestTruncatedText_TruncatesLongText(t *testing.T) {
	longText := "This is a very long piece of text that will definitely exceed the available width"
	text := components.NewTruncatedText(longText, 1, 0)
	lines := text.Render(30)

	assert.Equal(t, 1, len(lines))

	// Should be exactly 30 characters
	assert.Equal(t, 30, utils.VisibleWidth(lines[0]))

	// Should contain ellipsis
	stripped := utils.StripAnsiCodes(lines[0])
	assert.Contains(t, stripped, "...")
}

func TestTruncatedText_PreservesAnsiCodes(t *testing.T) {
	// Simulate ANSI styled text (red "Hello" blue "world")
	styledText := "\x1b[31mHello\x1b[0m \x1b[34mworld\x1b[0m"
	text := components.NewTruncatedText(styledText, 1, 0)
	lines := text.Render(40)

	assert.Equal(t, 1, len(lines))

	// Should be exactly 40 visible characters (ANSI codes don't count)
	assert.Equal(t, 40, utils.VisibleWidth(lines[0]))

	// Should preserve the color codes
	assert.Contains(t, lines[0], "\x1b[")
}

func TestTruncatedText_TruncatesStyledTextWithReset(t *testing.T) {
	// Long styled red text
	longStyledText := "\x1b[31mThis is a very long red text that will be truncated\x1b[0m"
	text := components.NewTruncatedText(longStyledText, 1, 0)
	lines := text.Render(20)

	assert.Equal(t, 1, len(lines))

	// Should be exactly 20 visible characters
	assert.Equal(t, 20, utils.VisibleWidth(lines[0]))

	// Should contain reset code before ellipsis
	// Note: Our truncateToWidth adds reset before ellipsis when truncating styled text
	assert.Contains(t, lines[0], "\x1b[0m...")
}

func TestTruncatedText_HandlesTextThatFitsExactly(t *testing.T) {
	// With paddingX=1, available width is 30-2=28
	// "Hello world" is 11 chars, fits comfortably
	text := components.NewTruncatedText("Hello world", 1, 0)
	lines := text.Render(30)

	assert.Equal(t, 1, len(lines))
	assert.Equal(t, 30, utils.VisibleWidth(lines[0]))

	// Should NOT contain ellipsis
	stripped := utils.StripAnsiCodes(lines[0])
	assert.NotContains(t, stripped, "...")
}

func TestTruncatedText_HandlesEmptyText(t *testing.T) {
	text := components.NewTruncatedText("", 1, 0)
	lines := text.Render(30)

	assert.Equal(t, 1, len(lines))
	assert.Equal(t, 30, utils.VisibleWidth(lines[0]))
}

func TestTruncatedText_StopsAtNewline(t *testing.T) {
	multilineText := "First line\nSecond line\nThird line"
	text := components.NewTruncatedText(multilineText, 1, 0)
	lines := text.Render(40)

	assert.Equal(t, 1, len(lines))
	assert.Equal(t, 40, utils.VisibleWidth(lines[0]))

	// Should only contain "First line"
	stripped := utils.StripAnsiCodes(lines[0])
	assert.Contains(t, stripped, "First line")
	assert.NotContains(t, stripped, "Second line")
	assert.NotContains(t, stripped, "Third line")
}

func TestTruncatedText_TruncatesFirstLineEvenWithNewlines(t *testing.T) {
	longMultilineText := "This is a very long first line that needs truncation\nSecond line"
	text := components.NewTruncatedText(longMultilineText, 1, 0)
	lines := text.Render(25)

	assert.Equal(t, 1, len(lines))
	assert.Equal(t, 25, utils.VisibleWidth(lines[0]))

	// Should contain ellipsis and not second line
	stripped := utils.StripAnsiCodes(lines[0])
	assert.Contains(t, stripped, "...")
	assert.NotContains(t, stripped, "Second line")
}
