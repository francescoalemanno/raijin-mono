package theme

import (
	"fmt"
	"strings"
)

// Ansi24 returns a function that wraps text in 24-bit foreground color.
func Ansi24(r, g, b int) func(string) string {
	prefix := fmt.Sprintf("\x1b[38;2;%d;%d;%dm", r, g, b)
	return func(s string) string {
		if s == "" {
			return ""
		}
		return prefix + s + "\x1b[0m"
	}
}

// AnsiBold wraps text in bold + 24-bit foreground color.
func AnsiBold(r, g, b int) func(string) string {
	prefix := fmt.Sprintf("\x1b[1;38;2;%d;%d;%dm", r, g, b)
	return func(s string) string {
		if s == "" {
			return ""
		}
		return prefix + s + "\x1b[0m"
	}
}

// AnsiUnderline wraps text in underline + 24-bit foreground color.
func AnsiUnderline(r, g, b int) func(string) string {
	prefix := fmt.Sprintf("\x1b[4;38;2;%d;%d;%dm", r, g, b)
	return func(s string) string {
		if s == "" {
			return ""
		}
		return prefix + s + "\x1b[0m"
	}
}

// AnsiItalic wraps text in italic + 24-bit foreground color.
func AnsiItalic(r, g, b int) func(string) string {
	prefix := fmt.Sprintf("\x1b[3;38;2;%d;%d;%dm", r, g, b)
	return func(s string) string {
		if s == "" {
			return ""
		}
		return prefix + s + "\x1b[0m"
	}
}

// AnsiBoldItalic wraps text in bold+italic + 24-bit foreground color.
func AnsiBoldItalic(r, g, b int) func(string) string {
	prefix := fmt.Sprintf("\x1b[1;3;38;2;%d;%d;%dm", r, g, b)
	return func(s string) string {
		if s == "" {
			return ""
		}
		return prefix + s + "\x1b[0m"
	}
}

// AnsiBgOnly returns a function that sets only the background color (no foreground change).
// Uses \x1b[49m (reset background only) instead of \x1b[0m (full reset) to avoid
// killing foreground styles.
func AnsiBgOnly(r, g, b int) func(string) string {
	prefix := fmt.Sprintf("\x1b[48;2;%d;%d;%dm", r, g, b)
	return func(s string) string {
		if s == "" {
			return ""
		}
		// Re-inject background after every full reset (\x1b[0m) in the content
		// so the background persists across styled text segments.
		patched := strings.ReplaceAll(s, "\x1b[0m", "\x1b[0m"+prefix)
		return prefix + patched + "\x1b[49m"
	}
}

// Border characters
const (
	BorderThin  = "│" // Thin vertical line
	BorderThick = "║" // Double (thick) vertical line
)

// Theme colors
var (
	ColorForeground = Ansi24(0xEB, 0xDB, 0xB2) // #EBDBB2
	ColorMuted      = Ansi24(0xA8, 0x99, 0x84) // #A89984
	ColorAccent     = Ansi24(0xFA, 0xBD, 0x2F) // #FABD2F
	ColorAccentAlt  = Ansi24(0xFE, 0x80, 0x19) // #FE8019
	ColorSuccess    = Ansi24(0xB8, 0xBB, 0x26) // #B8BB26
	ColorDanger     = Ansi24(0xFB, 0x49, 0x34) // #FB4934

	// Diff-specific foreground colors (lighter than success/danger for code previews)
	ColorDiffAdded   = Ansi24(0x8E, 0xC0, 0x7C) // #8EC07C
	ColorDiffRemoved = Ansi24(0xFB, 0x49, 0x34) // #FB4934

	// Typography variants in the base foreground color.
	ColorForegroundBold       = AnsiBold(0xEB, 0xDB, 0xB2)       // #EBDBB2
	ColorForegroundUnderline  = AnsiUnderline(0xEB, 0xDB, 0xB2)  // #EBDBB2
	ColorForegroundItalic     = AnsiItalic(0xEB, 0xDB, 0xB2)     // #EBDBB2
	ColorForegroundBoldItalic = AnsiBoldItalic(0xEB, 0xDB, 0xB2) // #EBDBB2

	ColorAccentAltBold = AnsiBold(0xFE, 0x80, 0x19) // #FE8019

	// Tool execution background colors (subtle dark tints)
	BgToolPending = AnsiBgOnly(0x3C, 0x38, 0x36) // neutral dark (gruvbox gray)
	BgToolSuccess = AnsiBgOnly(0x2F, 0x3B, 0x28) // dark olive tint
	BgToolError   = AnsiBgOnly(0x3F, 0x2A, 0x2A) // dark red tint

	// Tool title style (bold accent)
	ColorToolTitle = AnsiBold(0xFE, 0x80, 0x19) // #FE8019

	// Brand gradient colors for the title
	GradientColors = []func(string) string{
		Ansi24(0xEB, 0xDB, 0xB2), // #EBDBB2
		Ansi24(0xD5, 0xC4, 0xA1), // #D5C4A1
		Ansi24(0xFA, 0xBD, 0x2F), // #FABD2F
		Ansi24(0xFE, 0x80, 0x19), // #FE8019
		Ansi24(0xD6, 0x5D, 0x0E), // #D65D0E
		Ansi24(0xFB, 0x49, 0x34), // #FB4934
	}
	GradientBold = []func(string) string{
		AnsiBold(0xEB, 0xDB, 0xB2), // #EBDBB2
		AnsiBold(0xD5, 0xC4, 0xA1), // #D5C4A1
		AnsiBold(0xFA, 0xBD, 0x2F), // #FABD2F
		AnsiBold(0xFE, 0x80, 0x19), // #FE8019
		AnsiBold(0xD6, 0x5D, 0x0E), // #D65D0E
		AnsiBold(0xFB, 0x49, 0x34), // #FB4934
	}
)

// RenderGradientTitle renders the RAIJIN brand title with a gradient.
func RenderGradientTitle(text string) string {
	parts := strings.Split(text, "RAIJIN")
	if len(parts) != 2 {
		return text
	}
	var b strings.Builder
	if parts[0] != "" {
		b.WriteString(GradientColors[0](parts[0]))
	}
	runes := []rune("RAIJIN")
	n := len(GradientBold)
	for i, r := range runes {
		idx := i * (n - 1) / max(len(runes)-1, 1)
		b.WriteString(GradientBold[idx](string(r)))
	}
	if parts[1] != "" {
		b.WriteString(GradientColors[n-1](parts[1]))
	}
	return b.String()
}
