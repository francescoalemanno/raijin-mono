package utils

import (
	"strings"
	"unicode/utf8"

	"github.com/francescoalemanno/raijin-mono/libtui/pkg/ansi"
	"github.com/rivo/uniseg"
)

// WrapTextWithAnsi wraps text with ANSI codes preserved.
// Only does word wrapping - NO padding, NO background colors.
// Returns lines where each line is <= width visible chars.
// Active ANSI codes are preserved across line breaks.
func WrapTextWithAnsi(text string, width int) []string {
	if text == "" {
		return []string{""}
	}

	// Handle newlines by processing each line separately
	inputLines := strings.Split(text, "\n")
	result := []string{}
	tracker := ansi.NewAnsiCodeTracker()

	for idx, inputLine := range inputLines {
		prefix := ""
		if idx > 0 {
			prefix = tracker.GetActiveCodes()
		}
		result = append(result, wrapSingleLine(prefix+inputLine, width)...)

		// Update tracker with codes from this line for next iteration
		updateTrackerFromText(inputLine, tracker)
	}

	if len(result) == 0 {
		return []string{""}
	}
	return result
}

// wrapSingleLine wraps a single line (no newlines) with ANSI preservation
func wrapSingleLine(line string, width int) []string {
	if line == "" {
		return []string{""}
	}

	visibleLength := VisibleWidth(line)
	if visibleLength <= width {
		return []string{line}
	}

	wrapped := []string{}
	tracker := ansi.NewAnsiCodeTracker()
	tokens := splitIntoTokensWithAnsi(line)

	currentLine := ""
	currentVisibleLength := 0

	for _, token := range tokens {
		tokenVisibleLength := VisibleWidth(token)
		isWhitespace := strings.TrimSpace(token) == ""

		// Token itself is too long - break it character by character
		if tokenVisibleLength > width && !isWhitespace {
			if currentLine != "" {
				lineEndReset := tracker.GetLineEndReset()
				if lineEndReset != "" {
					currentLine += lineEndReset
				}
				wrapped = append(wrapped, currentLine)
				currentLine = ""
				currentVisibleLength = 0
			}

			broken := breakLongWord(token, width, tracker)
			wrapped = append(wrapped, broken[:len(broken)-1]...)
			currentLine = broken[len(broken)-1]
			currentVisibleLength = VisibleWidth(currentLine)
			continue
		}

		// Check if adding this token would exceed width
		totalNeeded := currentVisibleLength + tokenVisibleLength

		if totalNeeded > width && currentVisibleLength > 0 {
			lineToWrap := strings.TrimRight(currentLine, " ")
			lineEndReset := tracker.GetLineEndReset()
			if lineEndReset != "" {
				lineToWrap += lineEndReset
			}
			wrapped = append(wrapped, lineToWrap)
			if isWhitespace {
				currentLine = tracker.GetActiveCodes()
				currentVisibleLength = 0
			} else {
				currentLine = tracker.GetActiveCodes() + token
				currentVisibleLength = tokenVisibleLength
			}
		} else {
			currentLine += token
			currentVisibleLength += tokenVisibleLength
		}

		updateTrackerFromText(token, tracker)
	}

	if currentLine != "" {
		wrapped = append(wrapped, currentLine)
	}

	if len(wrapped) == 0 {
		return []string{""}
	}

	// Trim trailing whitespace from all lines (JavaScript trimEnd())
	result := make([]string, len(wrapped))
	for i, line := range wrapped {
		result[i] = trimEnd(line)
	}
	return result
}

// trimEnd removes trailing whitespace characters
func trimEnd(s string) string {
	return strings.TrimRight(s, " \t\n\r")
}

// splitIntoTokensWithAnsi splits text into words while keeping ANSI codes attached
func splitIntoTokensWithAnsi(text string) []string {
	if isASCIIString(text) {
		return splitIntoTokensWithAnsiASCII(text)
	}

	tokens := []string{}
	current := ""
	pendingAnsi := "" // ANSI codes waiting to be attached to next visible content
	inWhitespace := false
	i := 0

	for i < len(text) {
		// Check for ANSI code at this position
		if code, codeLen := ExtractAnsiCode(text, i); codeLen > 0 {
			pendingAnsi += code
			i += codeLen
			continue
		}

		r, size := utf8.DecodeRuneInString(text[i:])
		char := text[i : i+size]
		charIsSpace := r == ' '

		if charIsSpace != inWhitespace && current != "" {
			tokens = append(tokens, current)
			current = ""
		}

		// Attach any pending ANSI codes to this visible character
		if pendingAnsi != "" {
			current += pendingAnsi
			pendingAnsi = ""
		}

		inWhitespace = charIsSpace
		current += char
		i += size
	}

	// Handle any remaining pending ANSI codes
	if pendingAnsi != "" {
		current += pendingAnsi
	}

	if current != "" {
		tokens = append(tokens, current)
	}

	return tokens
}

func splitIntoTokensWithAnsiASCII(text string) []string {
	tokens := []string{}
	current := ""
	pendingAnsi := ""
	inWhitespace := false
	i := 0

	for i < len(text) {
		if code, codeLen := ExtractAnsiCode(text, i); codeLen > 0 {
			pendingAnsi += code
			i += codeLen
			continue
		}

		char := text[i : i+1]
		charIsSpace := char == " "

		if charIsSpace != inWhitespace && current != "" {
			tokens = append(tokens, current)
			current = ""
		}

		if pendingAnsi != "" {
			current += pendingAnsi
			pendingAnsi = ""
		}

		inWhitespace = charIsSpace
		current += char
		i++
	}

	if pendingAnsi != "" {
		current += pendingAnsi
	}

	if current != "" {
		tokens = append(tokens, current)
	}

	return tokens
}

// updateTrackerFromText processes ANSI codes in text to update the tracker
func updateTrackerFromText(text string, tracker *ansi.AnsiCodeTracker) {
	i := 0
	for i < len(text) {
		if code, codeLen := ExtractAnsiCode(text, i); codeLen > 0 {
			tracker.Process(code)
			i += codeLen
		} else {
			i++
		}
	}
}

// breakLongWord breaks a long word into multiple lines
func breakLongWord(word string, width int, tracker *ansi.AnsiCodeTracker) []string {
	if isASCIIString(word) {
		return breakLongWordASCII(word, width, tracker)
	}

	lines := []string{}
	currentLine := tracker.GetActiveCodes()
	currentWidth := 0

	// Process word, handling ANSI codes specially
	i := 0
	for i < len(word) {
		// Check for ANSI code at this position
		if code, codeLen := ExtractAnsiCode(word, i); codeLen > 0 {
			currentLine += code
			tracker.Process(code)
			i += codeLen
			continue
		}

		// Find the next ANSI code or end of string
		textEnd := i
		for textEnd < len(word) {
			if _, codeLen := ExtractAnsiCode(word, textEnd); codeLen > 0 {
				break
			}
			textEnd++
		}

		// Segment this non-ANSI portion into graphemes
		textPortion := word[i:textEnd]
		gr := uniseg.NewGraphemes(textPortion)
		for gr.Next() {
			segment := gr.Str()
			gw := gr.Width()

			if currentWidth+gw > width {
				lineEndReset := tracker.GetLineEndReset()
				if lineEndReset != "" {
					currentLine += lineEndReset
				}
				lines = append(lines, currentLine)
				currentLine = tracker.GetActiveCodes()
				currentWidth = 0
			}

			currentLine += segment
			currentWidth += gw
		}

		i = textEnd
	}

	if currentLine != "" {
		lines = append(lines, currentLine)
	}

	if len(lines) == 0 {
		return []string{""}
	}
	return lines
}

func breakLongWordASCII(word string, width int, tracker *ansi.AnsiCodeTracker) []string {
	lines := []string{}
	currentLine := tracker.GetActiveCodes()
	currentWidth := 0

	for i := 0; i < len(word); {
		if code, codeLen := ExtractAnsiCode(word, i); codeLen > 0 {
			currentLine += code
			tracker.Process(code)
			i += codeLen
			continue
		}

		if currentWidth+1 > width {
			lineEndReset := tracker.GetLineEndReset()
			if lineEndReset != "" {
				currentLine += lineEndReset
			}
			lines = append(lines, currentLine)
			currentLine = tracker.GetActiveCodes()
			currentWidth = 0
		}

		currentLine += word[i : i+1]
		currentWidth++
		i++
	}

	if currentLine != "" {
		lines = append(lines, currentLine)
	}
	if len(lines) == 0 {
		return []string{""}
	}
	return lines
}

// ApplyBackgroundToLine applies background color to a line, padding to full width
func ApplyBackgroundToLine(line string, width int, bgFn func(string) string) string {
	visibleLen := VisibleWidth(line)
	paddingNeeded := width - visibleLen
	if paddingNeeded < 0 {
		paddingNeeded = 0
	}
	padding := strings.Repeat(" ", paddingNeeded)

	withPadding := line + padding
	return bgFn(withPadding)
}
