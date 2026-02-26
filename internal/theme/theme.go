package theme

import (
	"fmt"
	"strings"
)

// Color represents an RGB color.
type Color struct {
	R, G, B byte
}

// Ansi24 returns the text wrapped in 24-bit foreground color ANSI codes.
func (c Color) Ansi24(s string) string {
	if s == "" {
		return ""
	}
	prefix := fmt.Sprintf("\x1b[38;2;%d;%d;%dm", c.R, c.G, c.B)
	return prefix + s + "\x1b[0m"
}

// AnsiBold returns the text wrapped in bold + 24-bit foreground color.
func (c Color) AnsiBold(s string) string {
	if s == "" {
		return ""
	}
	prefix := fmt.Sprintf("\x1b[1;38;2;%d;%d;%dm", c.R, c.G, c.B)
	return prefix + s + "\x1b[0m"
}

// AnsiUnderline returns the text wrapped in underline + 24-bit foreground color.
func (c Color) AnsiUnderline(s string) string {
	if s == "" {
		return ""
	}
	prefix := fmt.Sprintf("\x1b[4;38;2;%d;%d;%dm", c.R, c.G, c.B)
	return prefix + s + "\x1b[0m"
}

// AnsiItalic returns the text wrapped in italic + 24-bit foreground color.
func (c Color) AnsiItalic(s string) string {
	if s == "" {
		return ""
	}
	prefix := fmt.Sprintf("\x1b[3;38;2;%d;%d;%dm", c.R, c.G, c.B)
	return prefix + s + "\x1b[0m"
}

// AnsiBoldItalic returns the text wrapped in bold+italic + 24-bit foreground color.
func (c Color) AnsiBoldItalic(s string) string {
	if s == "" {
		return ""
	}
	prefix := fmt.Sprintf("\x1b[1;3;38;2;%d;%d;%dm", c.R, c.G, c.B)
	return prefix + s + "\x1b[0m"
}

// AnsiBgOnly returns the text wrapped in background color only (no foreground change).
// Uses \x1b[49m (reset background only) instead of \x1b[0m (full reset) to avoid
// killing foreground styles.
func (c Color) AnsiBgOnly(s string) string {
	if s == "" {
		return ""
	}
	prefix := fmt.Sprintf("\x1b[48;2;%d;%d;%dm", c.R, c.G, c.B)
	// Re-inject background after every full reset (\x1b[0m) in the content
	// so the background persists across styled text segments.
	patched := strings.ReplaceAll(s, "\x1b[0m", "\x1b[0m"+prefix)
	return prefix + patched + "\x1b[49m"
}

// Border characters
const (
	BorderThin  = "│" // Thin vertical line
	BorderThick = "║" // Double (thick) vertical line
)

// Theme holds all theme colors for the application.
type Theme struct {
	// Foreground colors
	Foreground Color
	Muted      Color
	Accent     Color
	AccentAlt  Color
	Success    Color
	Danger     Color

	// Gradient colors for title rendering (start and end points)
	GradientLight Color
	GradientDark  Color

	// Diff-specific foreground colors (lighter for code previews)
	DiffAdded   Color
	DiffRemoved Color

	// Background colors for tool execution states
	BgToolPending Color
	BgToolSuccess Color
	BgToolError   Color

	// Tool title style color
	ToolTitle Color

	// Thinking block text color
	ThinkingMuted Color
}

// Default theme with Gruvbox-inspired colors
var Default = Theme{
	Foreground: Color{0xEB, 0xDB, 0xB2}, // #EBDBB2
	Muted:      Color{0xA8, 0x99, 0x84}, // #A89984
	Accent:     Color{0xFA, 0xBD, 0x2F}, // #FABD2F
	AccentAlt:  Color{0xFE, 0x80, 0x19}, // #FE8019
	Success:    Color{0xB8, 0xBB, 0x26}, // #B8BB26
	Danger:     Color{0xFB, 0x49, 0x34}, // #FB4934

	GradientLight: Color{0xEB, 0xDB, 0xB2}, // #EBDBB2 (same as Foreground)
	GradientDark:  Color{0xFB, 0x49, 0x34}, // #FB4934 (same as Danger)

	DiffAdded:   Color{0x8E, 0xC0, 0x7C}, // #8EC07C
	DiffRemoved: Color{0xFB, 0x49, 0x34}, // #FB4934

	BgToolPending: Color{0x3C, 0x38, 0x36}, // neutral dark (gruvbox gray)
	BgToolSuccess: Color{0x2F, 0x3B, 0x28}, // dark olive tint
	BgToolError:   Color{0x3F, 0x2A, 0x2A}, // dark red tint

	ToolTitle: Color{0xFE, 0x80, 0x19}, // #FE8019

	ThinkingMuted: Color{0xA8, 0x99, 0x84}, // #A89984 (same as Muted)
}



// RenderGradient applies a gradient color to text, interpolating between
// GradientLight and GradientDark across the length of the string.
// Alphabetic characters (A-Z, a-z) are rendered in bold.
func (t Theme) RenderGradient(text string) string {
	if text == "" {
		return ""
	}
	runes := []rune(text)
	n := len(runes)
	if n == 0 {
		return ""
	}
	var b strings.Builder
	for i, r := range runes {
		frac := float64(i) / float64(max(n-1, 1))
		c := t.GradientLight.Interpolate(t.GradientDark, frac)
		if (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') {
			b.WriteString(c.AnsiBold(string(r)))
		} else {
			b.WriteString(c.Ansi24(string(r)))
		}
	}
	return b.String()
}

// Interpolate returns a color between c and target based on fraction (0.0 to 1.0).
func (c Color) Interpolate(target Color, frac float64) Color {
	if frac <= 0 {
		return c
	}
	if frac >= 1 {
		return target
	}
	return Color{
		R: byte(float64(c.R) + (float64(target.R)-float64(c.R))*frac),
		G: byte(float64(c.G) + (float64(target.G)-float64(c.G))*frac),
		B: byte(float64(c.B) + (float64(target.B)-float64(c.B))*frac),
	}
}
