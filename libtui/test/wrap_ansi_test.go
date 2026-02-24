package test

import (
	"strings"
	"testing"

	"github.com/francescoalemanno/raijin-mono/libtui/pkg/utils"
	"github.com/stretchr/testify/assert"
)

func TestWrapTextWithAnsi_UnderlineStyling(t *testing.T) {
	t.Run("should not apply underline style before the styled text", func(t *testing.T) {
		underlineOn := "\x1b[4m"
		underlineOff := "\x1b[24m"
		url := "https://example.com/very/long/path/that/will/wrap"
		text := "read this thread " + underlineOn + url + underlineOff

		wrapped := utils.WrapTextWithAnsi(text, 40)

		// First line should NOT contain underline code
		assert.Equal(t, "read this thread", wrapped[0])

		// Second line should start with underline, have URL content
		assert.True(t, strings.HasPrefix(wrapped[1], underlineOn))
		assert.Contains(t, wrapped[1], "https://")
	})

	t.Run("should not have whitespace before underline reset code", func(t *testing.T) {
		underlineOn := "\x1b[4m"
		underlineOff := "\x1b[24m"
		textWithUnderlinedTrailingSpace := underlineOn + "underlined text here " + underlineOff + "more"

		wrapped := utils.WrapTextWithAnsi(textWithUnderlinedTrailingSpace, 18)

		// Only check line 0 (the line that gets wrapped)
		assert.False(t, strings.Contains(wrapped[0], " "+underlineOff))
	})

	t.Run("should not bleed underline to padding", func(t *testing.T) {
		underlineOn := "\x1b[4m"
		underlineOff := "\x1b[24m"
		url := "https://example.com/very/long/path/that/will/definitely/wrap"
		text := "prefix " + underlineOn + url + underlineOff + " suffix"

		wrapped := utils.WrapTextWithAnsi(text, 30)

		// Middle lines (with underlined content) should end with underline-off, not full reset
		for i := 1; i < len(wrapped)-1; i++ {
			line := wrapped[i]
			if strings.Contains(line, underlineOn) {
				assert.True(t, strings.HasSuffix(line, underlineOff), "Line %d should end with underline-off", i)
				assert.False(t, strings.HasSuffix(line, "\x1b[0m"), "Line %d should not end with full reset", i)
			}
		}
	})
}

func TestWrapTextWithAnsi_BackgroundColorPreservation(t *testing.T) {
	t.Run("should preserve background color across wrapped lines without full reset", func(t *testing.T) {
		bgBlue := "\x1b[44m"
		reset := "\x1b[0m"
		text := bgBlue + "hello world this is blue background text" + reset

		wrapped := utils.WrapTextWithAnsi(text, 15)

		// Each line should have background color
		for _, line := range wrapped {
			assert.Contains(t, line, bgBlue, "Line should have background color")
		}

		// Middle lines should NOT end with full reset
		for i := 0; i < len(wrapped)-1; i++ {
			assert.False(t, strings.HasSuffix(wrapped[i], "\x1b[0m"), "Middle line %d should not end with full reset", i)
		}
	})

	t.Run("should reset underline but preserve background when wrapping underlined text inside background", func(t *testing.T) {
		underlineOn := "\x1b[4m"
		underlineOff := "\x1b[24m"
		reset := "\x1b[0m"

		text := "\x1b[41mprefix " + underlineOn + "UNDERLINED_CONTENT_THAT_WRAPS" + underlineOff + " suffix" + reset

		wrapped := utils.WrapTextWithAnsi(text, 20)

		// All lines should have background color 41
		for _, line := range wrapped {
			hasBgColor := strings.Contains(line, "[41m") || strings.Contains(line, ";41m") || strings.Contains(line, "[41;")
			assert.True(t, hasBgColor, "Line should have background color 41: %s", line)
		}

		// Lines with underlined content should use underline-off at end, not full reset
		for i := 0; i < len(wrapped)-1; i++ {
			line := wrapped[i]
			if (strings.Contains(line, "[4m") || strings.Contains(line, "[4;") || strings.Contains(line, ";4m")) &&
				!strings.Contains(line, underlineOff) {
				assert.True(t, strings.HasSuffix(line, underlineOff), "Line %d with underline should end with underline-off", i)
				assert.False(t, strings.HasSuffix(line, "\x1b[0m"), "Line %d should not end with full reset", i)
			}
		}
	})
}

func TestWrapTextWithAnsi_BasicWrapping(t *testing.T) {
	t.Run("should wrap plain text correctly", func(t *testing.T) {
		text := "hello world this is a test"
		wrapped := utils.WrapTextWithAnsi(text, 10)

		assert.Greater(t, len(wrapped), 1, "Should wrap to multiple lines")
		for _, line := range wrapped {
			assert.LessOrEqual(t, utils.VisibleWidth(line), 10, "Line should not exceed width")
		}
	})

	t.Run("should truncate trailing whitespace that exceeds width", func(t *testing.T) {
		twoSpacesWrappedToWidth1 := utils.WrapTextWithAnsi("  ", 1)
		assert.LessOrEqual(t, utils.VisibleWidth(twoSpacesWrappedToWidth1[0]), 1)
	})

	t.Run("should preserve color codes across wraps", func(t *testing.T) {
		red := "\x1b[31m"
		reset := "\x1b[0m"
		text := red + "hello world this is red" + reset

		wrapped := utils.WrapTextWithAnsi(text, 10)

		// Each continuation line should start with red code
		for i := 1; i < len(wrapped); i++ {
			assert.True(t, strings.HasPrefix(wrapped[i], red), "Line %d should start with red code", i)
		}

		// Middle lines should not end with full reset
		for i := 0; i < len(wrapped)-1; i++ {
			assert.False(t, strings.HasSuffix(wrapped[i], "\x1b[0m"), "Middle line %d should not end with full reset", i)
		}
	})
}
