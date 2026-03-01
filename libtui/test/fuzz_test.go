package test

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/francescoalemanno/raijin-mono/libtui/pkg/ansi"
	"github.com/francescoalemanno/raijin-mono/libtui/pkg/autocomplete"
	"github.com/francescoalemanno/raijin-mono/libtui/pkg/components"
	"github.com/francescoalemanno/raijin-mono/libtui/pkg/fuzzy"
	"github.com/francescoalemanno/raijin-mono/libtui/pkg/keys"
	"github.com/francescoalemanno/raijin-mono/libtui/pkg/utils"
)

// FuzzStripAnsiCodes covers the hand-rolled CSI/OSC/APC byte-level parser.
// Truncated escape sequences and arbitrary control bytes are the main risk.
func FuzzStripAnsiCodes(f *testing.F) {
	f.Add("")
	f.Add("hello world")
	f.Add("\x1b[31mred\x1b[0m")
	f.Add("\x1b[38;5;200mfg256\x1b[0m")
	f.Add("\x1b[38;2;10;20;30mrgb\x1b[0m")
	f.Add("\x1b]0;title\x07")
	f.Add("\x1b]0;title\x1b\\")
	f.Add("\x1b_apc\x07")
	f.Add("\x1b_apc\x1b\\")
	f.Add("\x1b")        // bare ESC at end
	f.Add("\x1b[")       // unterminated CSI
	f.Add("\x1b]")       // unterminated OSC
	f.Add("\x1b_")       // unterminated APC
	f.Add("\x1b[1;2;3m") // multi-param SGR
	// OSC terminated by ST mid-string, then more text
	f.Add("\x1b]0;title\x1b\\more text")
	// APC terminated by ST mid-string
	f.Add("\x1b_data\x1b\\after")
	// CSI with every recognised terminator
	f.Add("\x1b[1G\x1b[2K\x1b[3H\x1b[4J\x1b[5m")
	// ESC immediately followed by another ESC (double-escape)
	f.Add("\x1b\x1b[31m")
	// OSC where nested ESC is not followed by backslash
	f.Add("\x1b]\x1b[31m\x07")
	// Mix of valid and unterminated in one string
	f.Add("\x1b[32mok\x1b[unterminated")
	// CSI non-terminator bytes covering the inner loop
	f.Add("\x1b[1;2;3;4;5;6;7;8;9;10m")
	// Lone ESC not followed by [ ] _
	f.Add("\x1bX something")
	// Zero-width and control bytes interleaved
	f.Add("a\x00b\x1b[mc\x07d")
	f.Fuzz(func(t *testing.T, s string) {
		out := utils.StripAnsiCodes(s)
		// Stripping can only remove bytes, never add them
		if len(out) > len(s) {
			t.Fatalf("output longer than input: %d > %d", len(out), len(s))
		}
		// Any terminated CSI sequence in the input must not appear in the output.
		// (Unterminated sequences are passed through by design.)
		i := 0
		for i < len(s) {
			code, clen := utils.ExtractAnsiCode(s, i)
			if clen > 0 {
				if strings.Contains(out, code) {
					t.Fatalf("terminated sequence %q still present in output %q", code, out)
				}
				i += clen
			} else {
				i++
			}
		}
		// If input was valid UTF-8, output must also be valid UTF-8
		if utf8.ValidString(s) && !utf8.ValidString(out) {
			t.Fatalf("valid UTF-8 input produced invalid UTF-8 output")
		}
	})
}

// FuzzExtractAnsiCode covers the shared escape-sequence extractor used by
// WrapTextWithAnsi, SliceWithWidth, TruncateToWidth, and others.
func FuzzExtractAnsiCode(f *testing.F) {
	f.Add("", 0)
	f.Add("\x1b[31m", 0)
	f.Add("\x1b]0;title\x07", 0)
	f.Add("\x1b_data\x1b\\", 0)
	f.Add("\x1b[", 0)     // unterminated
	f.Add("\x1b", 0)      // lone ESC
	f.Add("abc\x1b[m", 3) // non-zero start pos
	// pos points exactly at ESC inside a longer string
	f.Add("hello\x1b[32mworld", 5)
	// pos points past end
	f.Add("\x1b[m", 10)
	// OSC with ST terminator, pos at start
	f.Add("\x1b]0;title\x1b\\", 0)
	// APC with BEL
	f.Add("\x1b_pay\x07load", 0)
	// CSI with each terminator type (G K H J m)
	f.Add("\x1b[10G", 0)
	f.Add("\x1b[2K", 0)
	f.Add("\x1b[1H", 0)
	f.Add("\x1b[0J", 0)
	// CSI where inner ESC is not a valid ST
	f.Add("\x1b]\x1b[31m\x07", 0)
	// pos == 1 (not at ESC)
	f.Add("\x1b[31m", 1)
	f.Fuzz(func(t *testing.T, s string, pos int) {
		if pos < 0 {
			pos = -pos
		}
		if pos > len(s) {
			pos = len(s)
		}
		code, length := utils.ExtractAnsiCode(s, pos)
		if length == 0 {
			// No match: code must be empty too
			if code != "" {
				t.Fatalf("length==0 but code=%q", code)
			}
			return
		}
		// Returned slice must match the reported length
		if len(code) != length {
			t.Fatalf("len(code)=%d != length=%d", len(code), length)
		}
		// Extracted bytes must equal the corresponding substring
		if pos+length > len(s) {
			t.Fatalf("pos+length=%d exceeds len(s)=%d", pos+length, len(s))
		}
		if s[pos:pos+length] != code {
			t.Fatalf("code %q != s[pos:pos+length] %q", code, s[pos:pos+length])
		}
		// Every recognised sequence starts with ESC
		if code[0] != '\x1b' {
			t.Fatalf("extracted code does not start with ESC: %q", code)
		}
	})
}

// FuzzAnsiTrackerProcess covers the SGR parameter parser in AnsiCodeTracker,
// including multi-param 256-color and RGB sequences.
func FuzzAnsiTrackerProcess(f *testing.F) {
	f.Add("\x1b[0m")
	f.Add("\x1b[1m")
	f.Add("\x1b[31m")
	f.Add("\x1b[38;5;200m")
	f.Add("\x1b[38;2;10;20;30m")
	f.Add("\x1b[48;5;100m")
	f.Add("\x1b[1;3;4;31;42m")
	f.Add("\x1b[m")
	f.Add("\x1b[999m")
	f.Add("")
	// All individual attribute on-codes (1-9)
	f.Add("\x1b[2m") // dim
	f.Add("\x1b[3m") // italic
	f.Add("\x1b[4m") // underline
	f.Add("\x1b[5m") // blink
	f.Add("\x1b[7m") // inverse
	f.Add("\x1b[8m") // hidden
	f.Add("\x1b[9m") // strikethrough
	// All individual attribute off-codes (21-29)
	f.Add("\x1b[21m")
	f.Add("\x1b[22m")
	f.Add("\x1b[23m")
	f.Add("\x1b[24m")
	f.Add("\x1b[25m")
	f.Add("\x1b[27m")
	f.Add("\x1b[28m")
	f.Add("\x1b[29m")
	// Default fg/bg (39, 49)
	f.Add("\x1b[39m")
	f.Add("\x1b[49m")
	// Bright foreground (90-97) and background (100-107)
	f.Add("\x1b[90m")
	f.Add("\x1b[97m")
	f.Add("\x1b[100m")
	f.Add("\x1b[107m")
	// 256-color fg with missing/incomplete params
	f.Add("\x1b[38;5m")
	f.Add("\x1b[38m")
	// RGB fg with missing params
	f.Add("\x1b[38;2;10;20m")
	f.Add("\x1b[38;2m")
	// 256-color bg
	f.Add("\x1b[48;5;0m")
	// RGB bg
	f.Add("\x1b[48;2;255;255;255m")
	// Non-SGR sequence (no trailing m) — should be ignored
	f.Add("\x1b[31G")
	f.Add("\x1b]0;title\x07")
	// Param that is not a number
	f.Add("\x1b[xm")
	f.Add("\x1b[1;x;4m")
	f.Fuzz(func(t *testing.T, code string) {
		tr := ansi.NewAnsiCodeTracker()
		tr.Process(code)
		active := tr.GetActiveCodes()
		lineReset := tr.GetLineEndReset()
		hasActive := tr.HasActiveCodes()
		// GetActiveCodes must be valid UTF-8
		if !utf8.ValidString(active) {
			t.Fatalf("GetActiveCodes returned invalid UTF-8: %q", active)
		}
		// If active codes are non-empty they must form a valid SGR sequence
		if active != "" {
			if !strings.HasPrefix(active, "\x1b[") || !strings.HasSuffix(active, "m") {
				t.Fatalf("GetActiveCodes has unexpected format: %q", active)
			}
		}
		// HasActiveCodes must agree with whether GetActiveCodes is non-empty
		if hasActive != (active != "") {
			t.Fatalf("HasActiveCodes=%v but GetActiveCodes=%q", hasActive, active)
		}
		// GetLineEndReset is either empty or the underline-off sequence
		if lineReset != "" && lineReset != "\x1b[24m" {
			t.Fatalf("unexpected GetLineEndReset value: %q", lineReset)
		}
	})
}

// FuzzWrapTextWithAnsi covers the full word-wrap pipeline: tokenisation,
// long-word breaking, and tracker-based ANSI state preservation.
func FuzzWrapTextWithAnsi(f *testing.F) {
	f.Add("hello world", 10)
	f.Add("\x1b[31mred text here\x1b[0m", 5)
	f.Add("a", 1)
	f.Add("  ", 1)
	f.Add("", 80)
	f.Add("\x1b[4munderlined very long word thatwillwrap\x1b[24m", 10)
	f.Add("word\nnewline\nthird", 6)
	f.Add("日本語テスト", 4)
	f.Add("emoji 🎉🎊 test", 6)
	// ANSI code that spans exactly the wrap boundary
	f.Add("\x1b[32mshort\x1b[0m word", 5)
	// Multiple consecutive spaces (whitespace token path)
	f.Add("a     b", 3)
	// Single word longer than width (breakLongWord path)
	f.Add("superlongwordwithnospacesatall", 5)
	// ANSI inside a long-word break
	f.Add("\x1b[1mthiswordisverylongindeed\x1b[0m", 6)
	// Trailing ANSI code with no visible content after
	f.Add("word \x1b[31m", 10)
	// Multiple newlines producing empty lines
	f.Add("a\n\n\nb", 80)
	// Zero-width grapheme sequences (combining chars)
	f.Add("e\u0301 cafe\u0301", 4) // é using combining accent
	// Wide CJK chars at exact wrap boundary
	f.Add("ab日cd", 3)
	// Width == 1 (hardest for word-breaker)
	f.Add("hello world", 1)
	// Background color that must be preserved across lines
	f.Add("\x1b[44mblue background text wraps here\x1b[0m", 10)
	// Underline that should reset at line end (GetLineEndReset path)
	f.Add("\x1b[4munderline across multiple lines of text\x1b[24m", 12)
	f.Fuzz(func(t *testing.T, s string, width int) {
		if width < 1 {
			width = 1
		}
		if width > 500 {
			width = 500
		}
		lines := utils.WrapTextWithAnsi(s, width)
		if lines == nil {
			t.Fatal("WrapTextWithAnsi returned nil")
		}
		for i, line := range lines {
			// Every line must be valid UTF-8
			if !utf8.ValidString(line) {
				t.Fatalf("line %d is invalid UTF-8: %q", i, line)
			}
			// Visible width of each line must not exceed the requested width
			if vw := utils.VisibleWidth(line); vw > width {
				t.Fatalf("line %d visible width %d exceeds requested width %d: %q", i, vw, width, line)
			}
		}
	})
}

// FuzzVisibleWidth covers the grapheme-aware width calculator including its
// ASCII fast-path, ANSI stripping, and uniseg integration.
func FuzzVisibleWidth(f *testing.F) {
	f.Add("")
	f.Add("hello")
	f.Add("\x1b[31mred\x1b[0m")
	f.Add("日本語")
	f.Add("🎉")
	f.Add("\t tab")
	f.Fuzz(func(t *testing.T, s string) {
		w := utils.VisibleWidth(s)
		if w < 0 {
			t.Fatalf("VisibleWidth returned negative value %d for %q", w, s)
		}
		// Pure ASCII printable strings: width must equal byte length (fast-path)
		pureASCII := true
		for _, r := range s {
			if r < 32 || r > 126 {
				pureASCII = false
				break
			}
		}
		if pureASCII && w != len(s) {
			t.Fatalf("pure ASCII %q: expected width %d, got %d", s, len(s), w)
		}
	})
}

// FuzzTruncateToWidth covers the two-pass grapheme+ANSI truncation logic.
func FuzzTruncateToWidth(f *testing.F) {
	f.Add("hello world", 5, "...")
	f.Add("\x1b[31mred text\x1b[0m", 4, "…")
	f.Add("日本語テスト", 3, "")
	f.Add("", 10, "...")
	f.Add("a", 0, "")
	f.Fuzz(func(t *testing.T, s string, maxWidth int, ellipsis string) {
		if maxWidth < 0 {
			maxWidth = -maxWidth
		}
		if maxWidth > 500 {
			maxWidth = 500
		}
		out := utils.TruncateToWidth(s, maxWidth, ellipsis)
		// Output must be valid UTF-8
		if !utf8.ValidString(out) {
			t.Fatalf("TruncateToWidth returned invalid UTF-8: %q", out)
		}
		// Visible width of output must not exceed maxWidth
		if vw := utils.VisibleWidth(out); vw > maxWidth {
			t.Fatalf("visible width %d exceeds maxWidth %d for output %q", vw, maxWidth, out)
		}
		// If input already fits, output visible content must match input visible content
		if utils.VisibleWidth(s) <= maxWidth {
			if utils.StripAnsiCodes(out) != utils.StripAnsiCodes(s) {
				t.Fatalf("input fit but content changed: in=%q out=%q", s, out)
			}
		}
	})
}

// FuzzSliceWithWidth covers the ANSI-aware column slicer used for rendering.
func FuzzSliceWithWidth(f *testing.F) {
	f.Add("hello world", 2, 5)
	f.Add("\x1b[31mred\x1b[0m", 0, 3)
	f.Add("日本語テスト", 1, 4)
	f.Add("", 0, 10)
	f.Add("abc", 5, 3) // startCol beyond string length
	f.Fuzz(func(t *testing.T, s string, startCol, length int) {
		if startCol < 0 {
			startCol = -startCol
		}
		if length < 0 {
			length = -length
		}
		if startCol > 1000 {
			startCol = 1000
		}
		if length > 1000 {
			length = 1000
		}
		out := utils.SliceByColumn(s, startCol, length)
		// Output must be valid UTF-8
		if !utf8.ValidString(out) {
			t.Fatalf("SliceByColumn returned invalid UTF-8: %q", out)
		}
		// Visible width of result must not exceed the requested length
		if vw := utils.VisibleWidth(out); vw > length {
			t.Fatalf("visible width %d exceeds requested length %d: %q", vw, length, out)
		}
	})
}

// FuzzParseKey covers both legacy terminal sequences and Kitty CSI-u parsing.
func FuzzParseKey(f *testing.F) {
	f.Add("\x1b[A")           // up arrow
	f.Add("\x1b[31;5u")       // kitty ctrl+1
	f.Add("\x1b[99;5u")       // kitty ctrl+c
	f.Add("\x1b[1089::99;5u") // kitty cyrillic ctrl+c
	f.Add("\x1b[24~")         // f12
	f.Add("\x1b")             // bare ESC
	f.Add("\x1b[")            // unterminated CSI
	f.Add("\x03")             // ctrl+c legacy
	f.Add("")
	f.Add("\x1b[200~paste content\x1b[201~") // bracketed paste
	f.Add("\x1b[1:2:3;4:5u")                 // full kitty format
	f.Fuzz(func(t *testing.T, s string) {
		result := keys.ParseKey(s)
		release := keys.IsKeyRelease(s)
		// Output must be valid UTF-8
		if !utf8.ValidString(result) {
			t.Fatalf("ParseKey returned invalid UTF-8: %q", result)
		}
		// A key-release sequence must parse to a non-empty key name
		if release && result == "" {
			t.Fatalf("IsKeyRelease=true but ParseKey returned empty string for %q", s)
		}
		// Result must contain no raw control bytes (only printable + '+')
		for _, r := range result {
			if r < 32 && r != '+' {
				t.Fatalf("ParseKey result contains control byte 0x%02x: %q", r, result)
			}
		}
	})
}

// FuzzFuzzyMatch covers the multi-heuristic scoring engine including
// subsequence search, swapped alphanumeric token detection, and score arithmetic.
func FuzzFuzzyMatch(f *testing.F) {
	f.Add("", "")
	f.Add("foo", "foobar")
	f.Add("abc", "cba")
	f.Add("codex52", "gpt-5.2-codex")
	f.Add("日本", "日本語テスト")
	f.Add("a b c", "alpha beta charlie")
	f.Fuzz(func(t *testing.T, query, text string) {
		result := fuzzy.Match(query, text)
		// Empty query always matches with score 0
		if query == "" {
			if !result.Matches {
				t.Fatal("empty query must always match")
			}
			if result.Score != 0 {
				t.Fatalf("empty query must have score 0, got %f", result.Score)
			}
		}
		// Non-empty query against empty text never matches
		if query != "" && text == "" && result.Matches {
			t.Fatal("non-empty query against empty text must not match")
		}
		// If it matches, score must be finite (not NaN or ±Inf)
		if result.Matches {
			score := result.Score
			if score != score { // NaN check
				t.Fatal("match score is NaN")
			}
			if score > 1e15 || score < -1e15 {
				t.Fatalf("score out of reasonable range: %f", score)
			}
		}
	})
}

// FuzzTextRender covers the Text component: word-wrap + padding + background.
func FuzzTextRender(f *testing.F) {
	f.Add("hello world", 80, 0, 0)
	f.Add("", 80, 0, 0)
	f.Add("   ", 40, 0, 0)
	f.Add("line one\nline two\nline three", 20, 2, 1)
	f.Add("\x1b[31mred text\x1b[0m", 10, 0, 0)
	f.Add("日本語テスト", 6, 1, 0)
	f.Add("word", 1, 0, 0)
	f.Add("a very long line that definitely exceeds the narrow width given", 8, 0, 0)
	f.Fuzz(func(t *testing.T, text string, width, paddingX, paddingY int) {
		if width < 1 {
			width = 1
		}
		if width > 500 {
			width = 500
		}
		if paddingX < 0 {
			paddingX = -paddingX
		}
		if paddingY < 0 {
			paddingY = -paddingY
		}
		paddingX %= 20
		paddingY %= 10
		c := components.NewText(text, paddingX, paddingY, nil)
		lines := c.Render(width)
		for i, line := range lines {
			if !utf8.ValidString(line) {
				t.Fatalf("line %d is invalid UTF-8: %q", i, line)
			}
			if vw := utils.VisibleWidth(line); vw > width {
				t.Fatalf("line %d visible width %d exceeds width %d", i, vw, width)
			}
		}
	})
}

// FuzzTruncatedTextRender covers the TruncatedText component: single-line truncation + padding.
func FuzzTruncatedTextRender(f *testing.F) {
	f.Add("hello world", 80, 0, 0)
	f.Add("", 40, 0, 0)
	f.Add("\x1b[32mcoloured\x1b[0m", 5, 1, 0)
	f.Add("日本語テスト", 4, 0, 1)
	f.Add("line one\nline two", 30, 0, 0) // only first line shown
	f.Add("x", 1, 0, 0)
	f.Fuzz(func(t *testing.T, text string, width, paddingX, paddingY int) {
		if width < 1 {
			width = 1
		}
		if width > 500 {
			width = 500
		}
		if paddingX < 0 {
			paddingX = -paddingX
		}
		if paddingY < 0 {
			paddingY = -paddingY
		}
		paddingX %= 20
		paddingY %= 10
		c := components.NewTruncatedText(text, paddingX, paddingY)
		lines := c.Render(width)
		for i, line := range lines {
			if !utf8.ValidString(line) {
				t.Fatalf("line %d is invalid UTF-8: %q", i, line)
			}
			if vw := utils.VisibleWidth(line); vw > width {
				t.Fatalf("line %d visible width %d exceeds width %d", i, vw, width)
			}
		}
	})
}

// FuzzBoxRender covers the Box component: padding, background, child composition.
func FuzzBoxRender(f *testing.F) {
	f.Add("hello", 80, 0, 0)
	f.Add("line\nsecond", 20, 2, 1)
	f.Add("", 40, 0, 0)
	f.Add("\x1b[31mred\x1b[0m", 10, 1, 0)
	f.Add("wide 日本語", 5, 0, 0)
	f.Fuzz(func(t *testing.T, text string, width, paddingX, paddingY int) {
		if width < 1 {
			width = 1
		}
		if width > 500 {
			width = 500
		}
		if paddingX < 0 {
			paddingX = -paddingX
		}
		if paddingY < 0 {
			paddingY = -paddingY
		}
		paddingX %= 20
		paddingY %= 10
		box := components.NewBox(paddingX, paddingY, nil)
		box.AddChild(components.NewText(text, 0, 0, nil))
		lines := box.Render(width)
		for i, line := range lines {
			if !utf8.ValidString(line) {
				t.Fatalf("line %d is invalid UTF-8: %q", i, line)
			}
			if vw := utils.VisibleWidth(line); vw > width {
				t.Fatalf("line %d visible width %d exceeds width %d", i, vw, width)
			}
		}
	})
}

// FuzzSelectListRender covers the SelectList rendering and SetFilter path.
func FuzzSelectListRender(f *testing.F) {
	f.Add("foo", "f", 0, 80)
	f.Add("", "", 0, 40)
	f.Add("hello world", "he", 1, 30)
	f.Add("日本語", "日", 0, 20)
	f.Add("item with description", "item", 0, 100)
	f.Fuzz(func(t *testing.T, label, filter string, selectedIndex, width int) {
		if width < 1 {
			width = 1
		}
		if width > 500 {
			width = 500
		}
		if selectedIndex < 0 {
			selectedIndex = -selectedIndex
		}
		theme := components.SelectListTheme{
			Prefix:         func(s string) string { return s },
			SelectedPrefix: func(s string) string { return s },
			SelectedText:   func(s string) string { return s },
			Description:    func(s string) string { return s },
			ScrollInfo:     func(s string) string { return s },
			NoMatch:        func(s string) string { return s },
		}
		items := []components.SelectItem{
			{Value: label, Label: label, Description: "desc " + label},
			{Value: "other", Label: "other", Description: ""},
		}
		sl := components.NewSelectList(items, 5, theme)
		sl.SetFilter(filter)
		sl.SetSelectedIndex(selectedIndex)
		lines := sl.Render(width)
		for i, line := range lines {
			if !utf8.ValidString(line) {
				t.Fatalf("line %d is invalid UTF-8: %q", i, line)
			}
		}
	})
}

// FuzzWordWrapLine covers the editor's grapheme-aware word-wrap with byte index tracking.
func FuzzWordWrapLine(f *testing.F) {
	f.Add("hello world", 5)
	f.Add("", 10)
	f.Add("superlongwordwithnobreaks", 4)
	f.Add("日本語テスト", 3)
	f.Add("a b c d e", 3)
	f.Add("\t tabbed", 8)
	f.Add("emoji 🎉 here", 6)
	f.Add("e\u0301 caf\u0301", 3) // combining accent
	f.Fuzz(func(t *testing.T, line string, maxWidth int) {
		if maxWidth < 1 {
			maxWidth = 1
		}
		if maxWidth > 500 {
			maxWidth = 500
		}
		chunks := components.WordWrapLine(line, maxWidth)
		if len(chunks) == 0 {
			t.Fatal("WordWrapLine returned no chunks")
		}
		// Chunks must tile the original string without gaps or overlaps
		pos := 0
		for i, ch := range chunks {
			if ch.StartIndex != pos {
				t.Fatalf("chunk %d StartIndex=%d, expected %d", i, ch.StartIndex, pos)
			}
			if ch.EndIndex < ch.StartIndex {
				t.Fatalf("chunk %d EndIndex %d < StartIndex %d", i, ch.EndIndex, ch.StartIndex)
			}
			if ch.EndIndex > len(line) {
				t.Fatalf("chunk %d EndIndex %d exceeds len(line) %d", i, ch.EndIndex, len(line))
			}
			// Text must equal the slice of the original line
			if ch.Text != line[ch.StartIndex:ch.EndIndex] {
				t.Fatalf("chunk %d text mismatch: %q != %q", i, ch.Text, line[ch.StartIndex:ch.EndIndex])
			}
			// Visible width must not exceed maxWidth
			if vw := utils.VisibleWidth(ch.Text); vw > maxWidth {
				t.Fatalf("chunk %d visible width %d exceeds maxWidth %d: %q", i, vw, maxWidth, ch.Text)
			}
			pos = ch.EndIndex
		}
		// Last chunk must reach end of string
		if pos != len(line) {
			t.Fatalf("chunks end at %d but len(line)=%d", pos, len(line))
		}
	})
}

// FuzzInputRender covers the Input component's scrolling render and HandleInput.
func FuzzInputRender(f *testing.F) {
	f.Add("hello", 20, 3)
	f.Add("", 10, 0)
	f.Add("日本語テスト", 8, 2)
	f.Add("a very long input value that exceeds any reasonable display width", 15, 40)
	f.Add("\x1b[200~pasted content\x1b[201~", 30, 0) // bracketed paste
	f.Add("abc", 2, 1)                               // width smaller than prompt
	f.Fuzz(func(t *testing.T, inputData string, width, cursor int) {
		if width < 1 {
			width = 1
		}
		if width > 500 {
			width = 500
		}
		inp := components.NewInput()
		inp.HandleInput(inputData)
		// Clamp cursor into the valid range after HandleInput
		val := inp.GetValue()
		if cursor < 0 {
			cursor = -cursor
		}
		if cursor > len(val) {
			cursor = len(val)
		}
		lines := inp.Render(width)
		if len(lines) != 1 {
			t.Fatalf("Input.Render must always return exactly 1 line, got %d", len(lines))
		}
		if !utf8.ValidString(lines[0]) {
			t.Fatalf("Input.Render returned invalid UTF-8: %q", lines[0])
		}
	})
}

// FuzzRuneIndexToByteOffset covers the autocomplete rune-to-byte converter
// used to map cursor column (rune index) to byte offsets in multi-byte strings.
func FuzzRuneIndexToByteOffset(f *testing.F) {
	f.Add("hello", 3)
	f.Add("", 0)
	f.Add("日本語テスト", 4)
	f.Add("e\u0301caf\u0301", 2) // combining accent
	f.Add("🎉🎊", 1)
	f.Add("abc", -1)  // negative index → clamped to 0
	f.Add("abc", 100) // beyond end → clamped to len
	f.Fuzz(func(t *testing.T, s string, runeIdx int) {
		offset := autocomplete.RuneIndexToByteOffset(s, runeIdx)
		// Result must be a valid byte index within [0, len(s)]
		if offset < 0 || offset > len(s) {
			t.Fatalf("offset %d out of range [0, %d] for %q", offset, len(s), s)
		}
		// offset must land on a UTF-8 rune boundary
		if offset > 0 && offset < len(s) {
			_, size := utf8.DecodeRuneInString(s[offset:])
			if size == 0 {
				t.Fatalf("offset %d is not a rune boundary in %q", offset, s)
			}
		}
	})
}

// FuzzMarkdownRender covers the markdown parser + ANSI wrapping pipeline.
func FuzzMarkdownRender(f *testing.F) {
	f.Add("# Heading", 80)
	f.Add("**bold** and *italic*", 40)
	f.Add("```go\nfmt.Println()\n```", 60)
	f.Add("> blockquote", 30)
	f.Add("- item\n  - nested", 20)
	f.Add("| a | b |\n|---|---|\n| 1 | 2 |", 40)
	f.Add("", 80)
	f.Add("[link](https://example.com)", 80)
	f.Add("  \n  \n", 10) // whitespace-only
	// H2 and H3 headings (different rendering paths)
	f.Add("## Section", 80)
	f.Add("### Subsection", 80)
	// Inline code
	f.Add("use `fmt.Println` here", 40)
	// Strikethrough (goldmark extension)
	f.Add("~~deleted text~~", 40)
	// Horizontal rule
	f.Add("---", 40)
	f.Add("before\n\n---\n\nafter", 40)
	// Hard line break (two trailing spaces)
	f.Add("line one  \nline two  \nline three", 40)
	// Ordered list
	f.Add("1. first\n2. second\n3. third", 40)
	// Deeply nested list (3 levels)
	f.Add("- a\n  - b\n    - c\n      - d", 40)
	// Mixed inline: bold, italic, code, link in one paragraph
	f.Add("**bold** *italic* `code` [url](https://x.com) ~~strike~~", 80)
	// Blockquote containing a list
	f.Add("> - item one\n> - item two", 40)
	// Fenced code block with no language tag
	f.Add("```\nplain code\n```", 40)
	// Multiple paragraphs separated by blank lines
	f.Add("first paragraph\n\nsecond paragraph\n\nthird paragraph", 30)
	// Table with more columns and alignment
	f.Add("| L | C | R |\n|:--|:-:|--:|\n| a | b | c |", 40)
	// Narrow width forcing wrap inside every element
	f.Add("# Long heading that will definitely wrap", 10)
	f.Add("**bold text that is longer than the narrow width**", 8)
	f.Add("- list item that is much longer than the narrow width given", 12)
	// Unicode content in various elements
	f.Add("# 日本語見出し", 20)
	f.Add("- 🎉 emoji item\n- 🚀 another", 20)
	f.Add("| 名前 | 年齢 |\n|---|---|\n| 太郎 | 30 |", 30)
	// Paragraph long enough to require wrapping
	f.Add("this is a very long paragraph that will need to be wrapped across multiple lines when rendered at a narrow width", 20)
	// Nested blockquotes
	f.Add("> outer\n>\n> > inner", 40)
	// Code block inside a list item (indented)
	f.Add("- Item\n\n  ```go\n  fmt.Println()\n  ```", 40)
	// HTML block (rendered as-is)
	f.Add("<div>raw html</div>", 40)
	// Heading immediately followed by list (no blank line)
	f.Add("## Title\n- item one\n- item two", 40)
	f.Fuzz(func(t *testing.T, md string, width int) {
		if width < 1 {
			width = 1
		}
		if width > 500 {
			width = 500
		}
		theme := defaultMarkdownTheme()
		m := components.NewMarkdown(md, 0, 0, theme, nil)
		lines := m.Render(width)
		// Whitespace-only input must produce no lines
		if strings.TrimSpace(md) == "" && len(lines) != 0 {
			t.Fatalf("whitespace-only markdown produced %d lines", len(lines))
		}
		for i, line := range lines {
			// Every line must be valid UTF-8
			if !utf8.ValidString(line) {
				t.Fatalf("line %d is invalid UTF-8: %q", i, line)
			}
			// Visible width must not exceed the requested width
			if vw := utils.VisibleWidth(line); vw > width {
				t.Fatalf("line %d visible width %d exceeds requested width %d", i, vw, width)
			}
		}
	})
}
