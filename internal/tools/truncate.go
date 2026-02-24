package tools

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

const (
	DefaultMaxLines      = 2000
	DefaultMaxBytes      = 50 * 1024 // 50KB
	GrepMaxLineLength    = 500
	toolOutputTempPrefix = "tool-output-*.txt"
)

// TruncationResult reports how content was truncated.
type TruncationResult struct {
	Content               string
	Truncated             bool
	TruncatedBy           string // "lines", "bytes", or ""
	TotalLines            int
	TotalBytes            int
	OutputLines           int
	OutputBytes           int
	LastLinePartial       bool
	FirstLineExceedsLimit bool
	MaxLines              int
	MaxBytes              int
}

// TruncationOptions configures line/byte limits.
type TruncationOptions struct {
	MaxLines int
	MaxBytes int
}

// FormatSize renders byte counts in a compact human-readable format.
func FormatSize(bytes int) string {
	if bytes < 1024 {
		return fmt.Sprintf("%dB", bytes)
	}
	if bytes < 1024*1024 {
		return fmt.Sprintf("%.1fKB", float64(bytes)/1024)
	}
	return fmt.Sprintf("%.1fMB", float64(bytes)/(1024*1024))
}

// TruncateHead keeps the first lines/bytes of content.
// It never returns partial lines.
func TruncateHead(content string, options TruncationOptions) TruncationResult {
	maxLines, maxBytes := normalizeTruncationOptions(options)

	totalBytes := len(content)
	lines := splitLines(content)
	totalLines := len(lines)

	if totalLines <= maxLines && totalBytes <= maxBytes {
		return newNoTruncationResult(content, totalLines, totalBytes, maxLines, maxBytes)
	}

	firstLineBytes := len(lines[0])
	if firstLineBytes > maxBytes {
		return TruncationResult{
			Content:               "",
			Truncated:             true,
			TruncatedBy:           "bytes",
			TotalLines:            totalLines,
			TotalBytes:            totalBytes,
			OutputLines:           0,
			OutputBytes:           0,
			LastLinePartial:       false,
			FirstLineExceedsLimit: true,
			MaxLines:              maxLines,
			MaxBytes:              maxBytes,
		}
	}

	outLines := make([]string, 0, min(len(lines), maxLines))
	outputBytes := 0
	truncatedBy := "lines"

	for i := 0; i < len(lines) && i < maxLines; i++ {
		line := lines[i]
		lineBytes := len(line)
		if i > 0 {
			lineBytes++ // newline
		}

		if outputBytes+lineBytes > maxBytes {
			truncatedBy = "bytes"
			break
		}

		outLines = append(outLines, line)
		outputBytes += lineBytes
	}

	if len(outLines) >= maxLines && outputBytes <= maxBytes {
		truncatedBy = "lines"
	}

	outContent := joinLines(outLines)
	return TruncationResult{
		Content:               outContent,
		Truncated:             true,
		TruncatedBy:           truncatedBy,
		TotalLines:            totalLines,
		TotalBytes:            totalBytes,
		OutputLines:           len(outLines),
		OutputBytes:           len(outContent),
		LastLinePartial:       false,
		FirstLineExceedsLimit: false,
		MaxLines:              maxLines,
		MaxBytes:              maxBytes,
	}
}

// TruncateTail keeps the last lines/bytes of content.
// It may return a partial first line in the output when the final original line exceeds maxBytes.
func TruncateTail(content string, options TruncationOptions) TruncationResult {
	maxLines, maxBytes := normalizeTruncationOptions(options)

	totalBytes := len(content)
	lines := splitLines(content)
	totalLines := len(lines)

	if totalLines <= maxLines && totalBytes <= maxBytes {
		return newNoTruncationResult(content, totalLines, totalBytes, maxLines, maxBytes)
	}

	outLines := make([]string, 0, min(len(lines), maxLines))
	outputBytes := 0
	truncatedBy := "lines"
	lastLinePartial := false

	for i := len(lines) - 1; i >= 0 && len(outLines) < maxLines; i-- {
		line := lines[i]
		lineBytes := len(line)
		if len(outLines) > 0 {
			lineBytes++ // newline
		}

		if outputBytes+lineBytes > maxBytes {
			truncatedBy = "bytes"
			if len(outLines) == 0 {
				truncatedLine := truncateStringToBytesFromEnd(line, maxBytes)
				outLines = append([]string{truncatedLine}, outLines...)
				outputBytes = len(truncatedLine)
				lastLinePartial = true
			}
			break
		}

		outLines = append([]string{line}, outLines...)
		outputBytes += lineBytes
	}

	if len(outLines) >= maxLines && outputBytes <= maxBytes {
		truncatedBy = "lines"
	}

	outContent := joinLines(outLines)
	return TruncationResult{
		Content:               outContent,
		Truncated:             true,
		TruncatedBy:           truncatedBy,
		TotalLines:            totalLines,
		TotalBytes:            totalBytes,
		OutputLines:           len(outLines),
		OutputBytes:           len(outContent),
		LastLinePartial:       lastLinePartial,
		FirstLineExceedsLimit: false,
		MaxLines:              maxLines,
		MaxBytes:              maxBytes,
	}
}

// TruncateLine truncates a single line by rune count and adds a suffix.
func TruncateLine(line string, maxChars int) (text string, wasTruncated bool) {
	if maxChars <= 0 {
		maxChars = GrepMaxLineLength
	}
	runes := []rune(line)
	if len(runes) <= maxChars {
		return line, false
	}
	return string(runes[:maxChars]) + "... [truncated]", true
}

func newNoTruncationResult(content string, totalLines, totalBytes, maxLines, maxBytes int) TruncationResult {
	return TruncationResult{
		Content:               content,
		Truncated:             false,
		TruncatedBy:           "",
		TotalLines:            totalLines,
		TotalBytes:            totalBytes,
		OutputLines:           totalLines,
		OutputBytes:           totalBytes,
		LastLinePartial:       false,
		FirstLineExceedsLimit: false,
		MaxLines:              maxLines,
		MaxBytes:              maxBytes,
	}
}

func normalizeTruncationOptions(options TruncationOptions) (maxLines int, maxBytes int) {
	maxLines = options.MaxLines
	if maxLines <= 0 {
		maxLines = DefaultMaxLines
	}
	maxBytes = options.MaxBytes
	if maxBytes <= 0 {
		maxBytes = DefaultMaxBytes
	}
	return maxLines, maxBytes
}

func splitLines(content string) []string {
	return strings.Split(content, "\n")
}

func joinLines(lines []string) string {
	return strings.Join(lines, "\n")
}

func truncateStringToBytesFromEnd(s string, maxBytes int) string {
	if len(s) <= maxBytes {
		return s
	}
	if maxBytes <= 0 {
		return ""
	}

	b := []byte(s)
	start := len(b) - maxBytes
	for start < len(b) && (b[start]&0xc0) == 0x80 {
		start++
	}
	if start >= len(b) {
		return ""
	}

	truncated := b[start:]
	if utf8.Valid(truncated) {
		return string(truncated)
	}

	for start < len(b) {
		_, size := utf8.DecodeRune(b[start:])
		if size > 0 {
			return string(b[start:])
		}
		start++
	}
	return ""
}
