package chat

import (
	"regexp"
	"strings"
	"testing"
)

var ansiRe = regexp.MustCompile(`\x1b\[[0-9;]*m`)

func stripANSI(s string) string {
	return ansiRe.ReplaceAllString(s, "")
}

func hasBoldStyle(s string) bool {
	return strings.Contains(s, "\x1b[1;38;2;") || strings.Contains(s, "\x1b[1;3;38;2;")
}

func hasItalicStyle(s string) bool {
	return strings.Contains(s, "\x1b[3;38;2;") || strings.Contains(s, "\x1b[1;3;38;2;")
}

func TestInlineFormatLine(t *testing.T) {
	tests := []struct {
		name       string
		input      string
		expected   string
		wantBold   bool
		wantItalic bool
	}{
		{
			name:       "plain text",
			input:      "plain text",
			expected:   "plain text",
			wantBold:   false,
			wantItalic: false,
		},
		{
			name:       "bold asterisks",
			input:      "**bold**",
			expected:   "bold",
			wantBold:   true,
			wantItalic: false,
		},
		{
			name:       "bold underscores",
			input:      "__bold__",
			expected:   "bold",
			wantBold:   true,
			wantItalic: false,
		},
		{
			name:       "italic asterisks",
			input:      "*italic*",
			expected:   "italic",
			wantBold:   false,
			wantItalic: true,
		},
		{
			name:       "italic underscores",
			input:      "_italic_",
			expected:   "italic",
			wantBold:   false,
			wantItalic: true,
		},
		{
			name:       "bold italic triple marker",
			input:      "***both***",
			expected:   "both",
			wantBold:   true,
			wantItalic: true,
		},
		{
			name:       "mixed segments",
			input:      "before **bold** and *italic* after",
			expected:   "before bold and italic after",
			wantBold:   true,
			wantItalic: true,
		},
		{
			name:       "intraword underscore is not italic",
			input:      "foo_bar_baz",
			expected:   "foo_bar_baz",
			wantBold:   false,
			wantItalic: false,
		},
		{
			name:       "unclosed markers stay plain",
			input:      "this is *not closed",
			expected:   "this is *not closed",
			wantBold:   false,
			wantItalic: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := inlineFormatLine(tt.input)
			gotPlain := stripANSI(got)
			if gotPlain != tt.expected {
				t.Fatalf("plain text mismatch: got %q want %q", gotPlain, tt.expected)
			}

			hasBold := hasBoldStyle(got)
			hasItalic := hasItalicStyle(got)
			if hasBold != tt.wantBold {
				t.Fatalf("bold style mismatch: got %v want %v output=%q", hasBold, tt.wantBold, got)
			}
			if hasItalic != tt.wantItalic {
				t.Fatalf("italic style mismatch: got %v want %v output=%q", hasItalic, tt.wantItalic, got)
			}
		})
	}
}

func TestInlineFormatNoCrossLineFormatting(t *testing.T) {
	input := "**start bold**\nend bold**\n*start italic*\nend italic*"
	lines := strings.Split(inlineFormat(input), "\n")
	if len(lines) != 4 {
		t.Fatalf("expected 4 lines, got %d", len(lines))
	}

	if !hasBoldStyle(lines[0]) {
		t.Fatalf("line 0 should be bold: %q", lines[0])
	}
	if hasBoldStyle(lines[1]) {
		t.Fatalf("line 1 should not be bold: %q", lines[1])
	}
	if !hasItalicStyle(lines[2]) {
		t.Fatalf("line 2 should be italic: %q", lines[2])
	}
	if hasItalicStyle(lines[3]) {
		t.Fatalf("line 3 should not be italic: %q", lines[3])
	}
}

func TestInlineFormatPreservesLineStructure(t *testing.T) {
	input := "**line1**\n*line2*\nline3"
	result := inlineFormat(input)
	lines := strings.Split(result, "\n")
	if len(lines) != 3 {
		t.Fatalf("expected 3 lines, got %d", len(lines))
	}

	if stripANSI(lines[0]) != "line1" {
		t.Fatalf("line 0 mismatch: %q", stripANSI(lines[0]))
	}
	if stripANSI(lines[1]) != "line2" {
		t.Fatalf("line 1 mismatch: %q", stripANSI(lines[1]))
	}
	if stripANSI(lines[2]) != "line3" {
		t.Fatalf("line 2 mismatch: %q", stripANSI(lines[2]))
	}
}

func TestInlineFormatPreservesMutedColorAcrossInlineStyles(t *testing.T) {
	formatted := inlineFormat("before **bold** and *italic* after")

	// Check that regular text has 24-bit color ANSI codes
	// Pattern: \x1b[38;2;R;G;Bm (where R,G,B are 0-255)
	if !regexp.MustCompile(`\x1b\[38;2;\d{1,3};\d{1,3};\d{1,3}m`).MatchString(formatted) {
		t.Fatalf("expected 24-bit color sequence in output: %q", formatted)
	}

	// Check that bold formatting exists with color (\x1b[1;38;2;...)
	if !regexp.MustCompile(`\x1b\[1;38;2;\d{1,3};\d{1,3};\d{1,3}m`).MatchString(formatted) {
		t.Fatalf("expected bold+color sequence in output: %q", formatted)
	}

	// Check that italic formatting exists with color (\x1b[3;38;2;...)
	if !regexp.MustCompile(`\x1b\[3;38;2;\d{1,3};\d{1,3};\d{1,3}m`).MatchString(formatted) {
		t.Fatalf("expected italic+color sequence in output: %q", formatted)
	}
}
