package theme

import (
	"fmt"
	"strings"

	"github.com/alecthomas/chroma/v2"
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

// Dark theme with Nord colors
// Nord is an arctic, north-bluish color palette by Arctic Ice Studio
// https://www.nordtheme.com/
var Dark = Theme{
	// Snow Storm - bright text colors
	Foreground: Color{0xE5, 0xE9, 0xF0}, // #E5E9F0 (nord5 - brighter for main text)
	Muted:      Color{0x81, 0xA1, 0xC1}, // #81A1C1 (nord9 - muted blue-gray)

	// Aurora accent colors (bright, vibrant)
	Accent:    Color{0xEB, 0xCB, 0x8B}, // #EBCB8B (nord13 - yellow, warm accent)
	AccentAlt: Color{0xD0, 0x87, 0x70}, // #D08770 (nord12 - orange, secondary accent)
	Success:   Color{0xA3, 0xBE, 0x8C}, // #A3BE8C (nord14 - green)
	Danger:    Color{0xBF, 0x61, 0x6A}, // #BF616A (nord11 - red)

	// Gradient from Frost blue to Aurora purple
	GradientLight: Color{0x88, 0xC0, 0xD0}, // #88C0D0 (nord8 - light cyan)
	GradientDark:  Color{0xB4, 0x8E, 0xAD}, // #B48EAD (nord15 - purple)

	// Diff colors - slightly brighter versions for readability
	DiffAdded:   Color{0xA3, 0xBE, 0x8C}, // #A3BE8C (nord14 - green)
	DiffRemoved: Color{0xBF, 0x61, 0x6A}, // #BF616A (nord11 - red)

	// Polar Night backgrounds (dark, subtle)
	BgToolPending: Color{0x3B, 0x42, 0x52}, // #3B4252 (nord1 - dark blue-gray)
	BgToolSuccess: Color{0x2E, 0x34, 0x40}, // #2E3440 with green tint (nord0 variant)
	BgToolError:   Color{0x3B, 0x2E, 0x32}, // Dark red-tinted background

	// Tool title uses orange accent
	ToolTitle: Color{0xD0, 0x87, 0x70}, // #D08770 (nord12 - orange)

	// Thinking block uses muted blue
	ThinkingMuted: Color{0x81, 0xA1, 0xC1}, // #81A1C1 (nord9 - muted blue)
}

// Default is the active theme. It starts as Dark but can be switched at startup.
var Default = Dark

// Light theme - designed for light terminal backgrounds
// Uses darker colors for contrast against light backgrounds
var Light = Theme{
	// Dark text colors for readability on light backgrounds
	Foreground: Color{0x2E, 0x34, 0x40}, // #2E3440 (nord0 - dark gray)
	Muted:      Color{0x4C, 0x56, 0x6A}, // #4C566A (nord3 - muted gray)

	// Aurora accent colors (same vibrant palette)
	Accent:    Color{0xD0, 0x87, 0x70}, // #D08770 (nord12 - orange, darker for contrast)
	AccentAlt: Color{0xB0, 0x6D, 0x5A}, // Darker orange variant
	Success:   Color{0x5E, 0x81, 0x50}, // #5E8150 (darker green for visibility)
	Danger:    Color{0xBF, 0x3E, 0x4A}, // #BF3E4A (darker red for visibility)

	// Gradient from Frost blue to Aurora purple (darker variants)
	GradientLight: Color{0x5E, 0x81, 0xAC}, // #5E81AC (nord10 - darker blue)
	GradientDark:  Color{0x81, 0x61, 0x9A}, // #81619A (darker purple)

	// Diff colors - darker for visibility
	DiffAdded:   Color{0x5E, 0x81, 0x50}, // Darker green
	DiffRemoved: Color{0xBF, 0x3E, 0x4A}, // Darker red

	// Light backgrounds for tool execution states
	BgToolPending: Color{0xE5, 0xE9, 0xF0}, // #E5E9F0 (nord5 - light gray)
	BgToolSuccess: Color{0xD8, 0xDE, 0xE9}, // Lighter success background
	BgToolError:   Color{0xEB, 0xE1, 0xE3}, // Light red-tinted background

	// Tool title uses accent color
	ToolTitle: Color{0xD0, 0x87, 0x70}, // #D08770 (nord12 - orange)

	// Thinking block uses muted gray
	ThinkingMuted: Color{0x4C, 0x56, 0x6A}, // #4C566A (nord3 - muted gray)
}

// SetTheme sets the default theme by name. Returns true if the theme was found and set.
func SetTheme(name string) bool {
	switch name {
	case "light":
		Default = Light
		return true
	case "dark":
		Default = Dark
		return true
	default:
		return false
	}
}

// AvailableThemes returns a list of available theme names.
func AvailableThemes() []string {
	return []string{"dark", "light"}
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

// toHex returns the color as a hex string (e.g., "#RRGGBB").
func (c Color) toHex() string {
	return fmt.Sprintf("#%02X%02X%02X", c.R, c.G, c.B)
}

// ChromaStyle returns a Chroma syntax highlighting style based on the theme colors.
func (t Theme) ChromaStyle() *chroma.Style {
	fg := t.Foreground.toHex()
	muted := t.Muted.toHex()
	accent := t.Accent.toHex()
	accentAlt := t.AccentAlt.toHex()
	success := t.Success.toHex()
	danger := t.Danger.toHex()

	b := chroma.NewStyleBuilder("raijin")

	// Base text and background
	b.Add(chroma.Background, fg)
	b.Add(chroma.Text, fg)
	b.Add(chroma.TextWhitespace, fg)

	// Comments - muted color, italic
	b.Add(chroma.Comment, "italic "+muted)
	b.Add(chroma.CommentHashbang, "italic "+muted)
	b.Add(chroma.CommentMultiline, "italic "+muted)
	b.Add(chroma.CommentSingle, "italic "+muted)
	b.Add(chroma.CommentSpecial, "italic "+muted)
	b.Add(chroma.CommentPreproc, muted)
	b.Add(chroma.CommentPreprocFile, muted)

	// Keywords - accent alt (orange), bold
	b.Add(chroma.Keyword, "bold "+accentAlt)
	b.Add(chroma.KeywordConstant, "bold "+accentAlt)
	b.Add(chroma.KeywordDeclaration, "bold "+accentAlt)
	b.Add(chroma.KeywordNamespace, "bold "+accentAlt)
	b.Add(chroma.KeywordPseudo, accentAlt)
	b.Add(chroma.KeywordReserved, "bold "+accentAlt)
	b.Add(chroma.KeywordType, accentAlt)

	// Names - foreground
	b.Add(chroma.Name, fg)
	b.Add(chroma.NameAttribute, accent)
	b.Add(chroma.NameBuiltin, accentAlt)
	b.Add(chroma.NameBuiltinPseudo, accentAlt)
	b.Add(chroma.NameClass, success)
	b.Add(chroma.NameConstant, accent)
	b.Add(chroma.NameDecorator, accentAlt)
	b.Add(chroma.NameEntity, accent)
	b.Add(chroma.NameException, danger)
	b.Add(chroma.NameFunction, success)
	b.Add(chroma.NameFunctionMagic, success)
	b.Add(chroma.NameLabel, accent)
	b.Add(chroma.NameNamespace, success)
	b.Add(chroma.NameOther, fg)
	b.Add(chroma.NameProperty, accent)
	b.Add(chroma.NameTag, accentAlt)
	b.Add(chroma.NameVariable, fg)
	b.Add(chroma.NameVariableClass, fg)
	b.Add(chroma.NameVariableGlobal, fg)
	b.Add(chroma.NameVariableInstance, fg)
	b.Add(chroma.NameVariableMagic, fg)

	// Literals
	b.Add(chroma.Literal, accent)
	b.Add(chroma.LiteralDate, accent)

	// Strings - accent (yellow/gold)
	b.Add(chroma.String, accent)
	b.Add(chroma.StringAffix, accent)
	b.Add(chroma.StringBacktick, accent)
	b.Add(chroma.StringChar, accent)
	b.Add(chroma.StringDelimiter, accent)
	b.Add(chroma.StringDoc, "italic "+muted)
	b.Add(chroma.StringDouble, accent)
	b.Add(chroma.StringEscape, success)
	b.Add(chroma.StringHeredoc, accent)
	b.Add(chroma.StringInterpol, fg)
	b.Add(chroma.StringOther, accent)
	b.Add(chroma.StringRegex, success)
	b.Add(chroma.StringSingle, accent)
	b.Add(chroma.StringSymbol, accent)

	// Numbers - accent alt (orange)
	b.Add(chroma.Number, accentAlt)
	b.Add(chroma.NumberBin, accentAlt)
	b.Add(chroma.NumberFloat, accentAlt)
	b.Add(chroma.NumberHex, accentAlt)
	b.Add(chroma.NumberInteger, accentAlt)
	b.Add(chroma.NumberIntegerLong, accentAlt)
	b.Add(chroma.NumberOct, accentAlt)

	// Operators and punctuation
	b.Add(chroma.Operator, fg)
	b.Add(chroma.OperatorWord, "bold "+accentAlt)
	b.Add(chroma.Punctuation, fg)

	// Generic tokens
	b.Add(chroma.Generic, fg)
	b.Add(chroma.GenericDeleted, danger)
	b.Add(chroma.GenericEmph, "italic")
	b.Add(chroma.GenericError, danger)
	b.Add(chroma.GenericHeading, "bold "+success)
	b.Add(chroma.GenericInserted, success)
	b.Add(chroma.GenericOutput, fg)
	b.Add(chroma.GenericPrompt, "bold "+muted)
	b.Add(chroma.GenericStrong, "bold")
	b.Add(chroma.GenericSubheading, "bold "+success)
	b.Add(chroma.GenericTraceback, danger)
	b.Add(chroma.GenericUnderline, "underline")

	// Errors
	b.Add(chroma.Error, danger)

	style, _ := b.Build()
	return style
}
