package utils

import (
	"strings"
	"sync"
	"unicode"
	"unicode/utf8"

	"github.com/francescoalemanno/raijin-mono/libtui/pkg/ansi"
	"github.com/rivo/uniseg"
)

const widthCacheSize = 512

// widthCache is a simple FIFO cache for non-ASCII string widths.
// Protected by widthCacheMu for concurrent access.
var (
	widthCacheMu   sync.Mutex
	widthCache     = make(map[string]int)
	widthCacheKeys []string
)

// VisibleWidth calculates the visible width of a string in terminal columns.
func VisibleWidth(str string) int {
	if len(str) == 0 {
		return 0
	}

	// Fast path: ASCII-only content (including ANSI escapes and tabs/newlines),
	// avoiding allocations and grapheme segmentation.
	if width := visibleWidthASCIIOrNeg(str); width >= 0 {
		return width
	}

	// Check cache
	widthCacheMu.Lock()
	if cached, ok := widthCache[str]; ok {
		widthCacheMu.Unlock()
		return cached
	}
	widthCacheMu.Unlock()

	// Normalize: tabs to 3 spaces, strip ANSI escape codes
	clean := str
	if strings.Contains(str, "\t") {
		clean = strings.ReplaceAll(clean, "\t", "   ")
	}
	if strings.Contains(clean, "\x1b") {
		clean = StripAnsiCodes(clean)
	}

	// Calculate width using grapheme segmentation for proper emoji handling
	w := 0
	gr := uniseg.NewGraphemes(clean)
	for gr.Next() {
		w += gr.Width()
	}

	// Cache result
	cacheWidth(str, w)

	return w
}

func visibleWidthASCIIOrNeg(str string) int {
	width := 0
	for i := 0; i < len(str); {
		b := str[i]
		if b == '\x1b' {
			_, codeLen := ExtractAnsiCode(str, i)
			if codeLen <= 0 {
				return -1
			}
			i += codeLen
			continue
		}
		if b >= utf8.RuneSelf {
			return -1
		}

		switch b {
		case '\t':
			width += 3
		case '\n', '\r':
			// Zero visual width for line terminators.
		default:
			if b < 32 || b == 127 {
				return -1
			}
			width++
		}
		i++
	}
	return width
}

func isASCIIString(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] >= utf8.RuneSelf {
			return false
		}
	}
	return true
}

// cacheWidth adds a width to the cache, evicting oldest if full
func cacheWidth(key string, w int) {
	widthCacheMu.Lock()
	defer widthCacheMu.Unlock()

	if _, exists := widthCache[key]; exists {
		return
	}

	if len(widthCache) >= widthCacheSize {
		// Simple FIFO eviction
		if len(widthCacheKeys) > 0 {
			delete(widthCache, widthCacheKeys[0])
			widthCacheKeys = widthCacheKeys[1:]
		}
	}

	widthCache[key] = w
	widthCacheKeys = append(widthCacheKeys, key)
}

// stripAnsiCodes removes ANSI escape sequences from a string
func StripAnsiCodes(str string) string {
	var result strings.Builder
	i := 0
	for i < len(str) {
		if str[i] == '\x1b' && i+1 < len(str) {
			next := str[i+1]
			// CSI sequence: ESC [ ... m/G/K/H/J
			if next == '[' {
				j := i + 2
				for j < len(str) && !isCSITerminator(str[j]) {
					j++
				}
				if j < len(str) {
					i = j + 1
					continue
				}
			}
			// OSC sequence: ESC ] ... BEL or ESC ] ... ST
			if next == ']' {
				j := i + 2
				for j < len(str) && str[j] != '\x07' {
					// Check for ST (ESC \)
					if j+1 < len(str) && str[j] == '\x1b' && str[j+1] == '\\' {
						i = j + 2
						break
					}
					j++
				}
				if j < len(str) && str[j] == '\x07' {
					i = j + 1
					continue
				}
				if j < len(str) {
					continue
				}
			}
			// APC sequence: ESC _ ... BEL or ESC _ ... ST
			if next == '_' {
				j := i + 2
				for j < len(str) && str[j] != '\x07' {
					// Check for ST (ESC \)
					if j+1 < len(str) && str[j] == '\x1b' && str[j+1] == '\\' {
						i = j + 2
						break
					}
					j++
				}
				if j < len(str) && str[j] == '\x07' {
					i = j + 1
					continue
				}
				if j < len(str) {
					continue
				}
			}
		}
		result.WriteByte(str[i])
		i++
	}
	return result.String()
}

// isCSITerminator checks if a byte is a CSI sequence terminator
func isCSITerminator(c byte) bool {
	return c == 'm' || c == 'G' || c == 'K' || c == 'H' || c == 'J'
}

// extractAnsiCode extracts an ANSI escape sequence starting at position pos.
// Returns the code and its length, or empty if not found.
func ExtractAnsiCode(str string, pos int) (string, int) {
	if pos >= len(str) || str[pos] != '\x1b' {
		return "", 0
	}

	if pos+1 >= len(str) {
		return "", 0
	}

	next := str[pos+1]

	// CSI sequence: ESC [ ... m/G/K/H/J
	if next == '[' {
		j := pos + 2
		for j < len(str) && !isCSITerminator(str[j]) {
			j++
		}
		if j < len(str) {
			return str[pos : j+1], j + 1 - pos
		}
		return "", 0
	}

	// OSC sequence: ESC ] ... BEL or ESC ] ... ST
	if next == ']' {
		j := pos + 2
		for j < len(str) {
			if str[j] == '\x07' {
				return str[pos : j+1], j + 1 - pos
			}
			if j+1 < len(str) && str[j] == '\x1b' && str[j+1] == '\\' {
				return str[pos : j+2], j + 2 - pos
			}
			j++
		}
		return "", 0
	}

	// APC sequence: ESC _ ... BEL or ESC _ ... ST
	if next == '_' {
		j := pos + 2
		for j < len(str) {
			if str[j] == '\x07' {
				return str[pos : j+1], j + 1 - pos
			}
			if j+1 < len(str) && str[j] == '\x1b' && str[j+1] == '\\' {
				return str[pos : j+2], j + 2 - pos
			}
			j++
		}
		return "", 0
	}

	return "", 0
}

// TruncateToWidthPadded truncates text to fit within maxWidth, adding ellipsis if needed,
// and pads the result with spaces to exactly maxWidth.
func TruncateToWidthPadded(text string, maxWidth int, ellipsis string) string {
	textVisibleWidth := VisibleWidth(text)
	if textVisibleWidth <= maxWidth {
		return text + strings.Repeat(" ", maxWidth-textVisibleWidth)
	}
	truncated := TruncateToWidth(text, maxWidth, ellipsis)
	truncatedWidth := VisibleWidth(truncated)
	if truncatedWidth < maxWidth {
		return truncated + strings.Repeat(" ", maxWidth-truncatedWidth)
	}
	return truncated
}

// TruncateToWidth truncates text to fit within maxWidth, adding ellipsis if needed.
func TruncateToWidth(text string, maxWidth int, ellipsis ...string) string {
	if maxWidth <= 0 {
		return ""
	}

	ellipsisStr := "..."
	if len(ellipsis) > 0 {
		ellipsisStr = ellipsis[0]
	}

	textVisibleWidth := VisibleWidth(text)
	if textVisibleWidth <= maxWidth {
		return text
	}

	ellipsisWidth := VisibleWidth(ellipsisStr)
	targetWidth := maxWidth - ellipsisWidth

	if targetWidth <= 0 {
		return SliceByColumn(ellipsisStr, 0, maxWidth)
	}

	// Single pass: keep ANSI sequences and consume graphemes with their widths,
	// avoiding per-grapheme VisibleWidth calls.
	var result strings.Builder
	result.Grow(min(len(text)+len(ellipsisStr)+4, len(text)+16))
	currentWidth := 0
	i := 0
	state := 0
	for i < len(text) {
		if text[i] == '\x1b' {
			code, codeLen := ExtractAnsiCode(text, i)
			if codeLen > 0 {
				result.WriteString(code)
				i += codeLen
				continue
			}
		}
		if text[i] == '\t' {
			if currentWidth+3 > targetWidth {
				break
			}
			result.WriteByte('\t')
			currentWidth += 3
			i++
			state = 0
			continue
		}

		cluster, rest, w, newState := uniseg.FirstGraphemeClusterInString(text[i:], state)
		if cluster == "" {
			break
		}
		if currentWidth+w > targetWidth {
			break
		}
		result.WriteString(cluster)
		currentWidth += w
		state = newState
		consumed := len(text[i:]) - len(rest)
		if consumed <= 0 {
			_, consumed = utf8.DecodeRuneInString(text[i:])
			if consumed <= 0 {
				break
			}
		}
		i += consumed
	}

	// Add reset code before ellipsis to prevent styling leaking into it.
	// Keep this behavior stable even for empty ellipsis.
	result.WriteString("\x1b[0m")
	result.WriteString(ellipsisStr)

	return result.String()
}

// IsWhitespaceChar checks if a string (or its first rune) is whitespace.
// Accepts either a single rune or a string argument via the generic constraint.
func IsWhitespaceChar[T ~rune | ~string](v T) bool {
	switch val := any(v).(type) {
	case rune:
		return unicode.IsSpace(val)
	case string:
		for _, r := range val {
			return unicode.IsSpace(r)
		}
		return false
	}
	return false
}

// PunctuationChars is the set of punctuation characters
const PunctuationChars = "(){}[]<>.,;:'\"!?+-=*/\\|&%^$#@~`"

// IsPunctuationChar checks if the first rune of the value is punctuation.
func IsPunctuationChar[T ~rune | ~string](v T) bool {
	switch val := any(v).(type) {
	case rune:
		return strings.ContainsRune(PunctuationChars, val)
	case string:
		for _, r := range val {
			return strings.ContainsRune(PunctuationChars, r)
		}
		return false
	}
	return false
}

// GetGraphemes returns a slice of grapheme clusters from a string.
func GetGraphemes(s string) []string {
	gr := uniseg.NewGraphemes(s)
	result := []string{}
	for gr.Next() {
		result = append(result, gr.Str())
	}
	return result
}

// SliceResult contains the sliced text and its actual visible width
type SliceResult struct {
	Text  string
	Width int
}

// SliceWithWidth slices a line by visible columns and returns the text with actual width.
func SliceWithWidth(line string, startCol, length int, strict ...bool) SliceResult {
	strictMode := false
	if len(strict) > 0 {
		strictMode = strict[0]
	}
	if length <= 0 {
		return SliceResult{Text: "", Width: 0}
	}
	endCol := startCol + length
	var result strings.Builder
	resultWidth := 0
	currentCol := 0
	i := 0
	var pendingAnsi strings.Builder

	for i < len(line) {
		code, codeLen := ExtractAnsiCode(line, i)
		if codeLen > 0 {
			if currentCol >= startCol && currentCol < endCol {
				result.WriteString(code)
			} else if currentCol < startCol {
				pendingAnsi.WriteString(code)
			}
			i += codeLen
			continue
		}

		// Process graphemes
		gr := uniseg.NewGraphemes(line[i:])
		for gr.Next() {
			segment := gr.Str()
			w := gr.Width()

			inRange := currentCol >= startCol && currentCol < endCol
			fits := !strictMode || currentCol+w <= endCol
			if inRange && fits {
				if pendingAnsi.Len() > 0 {
					result.WriteString(pendingAnsi.String())
					pendingAnsi.Reset()
				}
				result.WriteString(segment)
				resultWidth += w
			}
			currentCol += w
			if currentCol >= endCol {
				break
			}
		}
		break
	}

	return SliceResult{Text: result.String(), Width: resultWidth}
}

// SliceByColumn slices a line by visible columns.
func SliceByColumn(line string, startCol, length int, strict ...bool) string {
	return SliceWithWidth(line, startCol, length, strict...).Text
}

// SegmentResult contains the extracted segments
type SegmentResult struct {
	Before      string
	BeforeWidth int
	After       string
	AfterWidth  int
}

// ExtractSegments extracts "before" and "after" segments from a line in a single pass.
func ExtractSegments(line string, beforeEnd, afterStart, afterLen int, strictAfter ...bool) SegmentResult {
	strict := false
	if len(strictAfter) > 0 {
		strict = strictAfter[0]
	}

	var before, after strings.Builder
	beforeWidth, afterWidth := 0, 0
	currentCol := 0
	i := 0
	var pendingAnsiBefore strings.Builder
	afterStarted := false
	afterEnd := afterStart + afterLen

	tracker := ansi.NewAnsiCodeTracker()

	for i < len(line) {
		code, codeLen := ExtractAnsiCode(line, i)
		if codeLen > 0 {
			// Track all SGR codes
			tracker.Process(code)
			// Include ANSI codes in their respective segments
			if currentCol < beforeEnd {
				pendingAnsiBefore.WriteString(code)
			} else if currentCol >= afterStart && currentCol < afterEnd && afterStarted {
				after.WriteString(code)
			}
			i += codeLen
			continue
		}

		// Process graphemes
		gr := uniseg.NewGraphemes(line[i:])
		for gr.Next() {
			segment := gr.Str()
			w := gr.Width()

			if currentCol < beforeEnd {
				if pendingAnsiBefore.Len() > 0 {
					before.WriteString(pendingAnsiBefore.String())
					pendingAnsiBefore.Reset()
				}
				before.WriteString(segment)
				beforeWidth += w
			} else if currentCol >= afterStart && currentCol < afterEnd {
				fits := !strict || currentCol+w <= afterEnd
				if fits {
					if !afterStarted {
						after.WriteString(tracker.GetActiveCodes())
						afterStarted = true
					}
					after.WriteString(segment)
					afterWidth += w
				}
			}

			currentCol += w
			exitCondition := false
			if afterLen <= 0 {
				exitCondition = currentCol >= beforeEnd
			} else {
				exitCondition = currentCol >= afterEnd
			}
			if exitCondition {
				break
			}
		}
		break
	}

	return SegmentResult{
		Before:      before.String(),
		BeforeWidth: beforeWidth,
		After:       after.String(),
		AfterWidth:  afterWidth,
	}
}
