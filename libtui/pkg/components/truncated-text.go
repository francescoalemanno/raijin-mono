package components

import (
	"strings"

	"github.com/francescoalemanno/raijin-mono/libtui/pkg/tui"
	"github.com/francescoalemanno/raijin-mono/libtui/pkg/utils"
)

// NewTruncatedText creates a new TruncatedText component.
func NewTruncatedText(text string, paddingX, paddingY int) *TruncatedText {
	return &TruncatedText{
		text:     text,
		paddingX: paddingX,
		paddingY: paddingY,
	}
}

// TruncatedText component that truncates text to fit viewport width.
type TruncatedText struct {
	text      string
	paddingX  int                 // Horizontal padding (left and right)
	paddingY  int                 // Vertical padding (top and bottom)
	fgColorFn func(string) string // Optional foreground color for padding
}

// SetFgColorFn sets the foreground color function for padding.
func (t *TruncatedText) SetFgColorFn(fn func(string) string) {
	t.fgColorFn = fn
}

// Invalidate clears cached state (no-op for TruncatedText).
func (t *TruncatedText) Invalidate() {
	// No cached state to invalidate
}

// HandleInput processes keyboard input (no-op for TruncatedText).
func (t *TruncatedText) HandleInput(data string) {
	// TruncatedText doesn't handle input
}

// Render renders the text truncated to fit the width.
func (t *TruncatedText) Render(width int) []string {
	if width < 1 {
		width = 1
	}

	result := []string{}

	// Empty line padded to width
	emptyLine := strings.Repeat(" ", width)
	if t.fgColorFn != nil {
		emptyLine = t.fgColorFn(emptyLine)
	}

	// Add vertical padding above
	for i := 0; i < t.paddingY; i++ {
		result = append(result, emptyLine)
	}

	// Calculate available width after horizontal padding
	maxPadding := max(0, (width-1)/2)
	effectivePaddingX := min(t.paddingX, maxPadding)
	availableWidth := width - effectivePaddingX*2
	if availableWidth < 1 {
		availableWidth = 1
	}

	// Take only the first line (stop at newline)
	singleLineText := t.text
	newlineIndex := strings.Index(t.text, "\n")
	if newlineIndex != -1 {
		singleLineText = t.text[:newlineIndex]
	}

	// Truncate text if needed (accounting for ANSI codes)
	displayText := utils.TruncateToWidth(singleLineText, availableWidth)

	// Add horizontal padding
	leftPadding := strings.Repeat(" ", effectivePaddingX)
	rightPadding := strings.Repeat(" ", effectivePaddingX)
	// Apply foreground color to padding if specified
	if t.fgColorFn != nil {
		leftPadding = t.fgColorFn(leftPadding)
		rightPadding = t.fgColorFn(rightPadding)
	}
	lineWithPadding := leftPadding + displayText + rightPadding

	// Pad line to exactly width characters
	finalLine := lineWithPadding
	lineVisibleWidth := utils.VisibleWidth(finalLine)
	paddingNeeded := width - lineVisibleWidth
	if paddingNeeded < 0 {
		paddingNeeded = 0
	}
	padding := strings.Repeat(" ", paddingNeeded)
	if t.fgColorFn != nil {
		padding = t.fgColorFn(padding)
	}
	finalLine = finalLine + padding

	result = append(result, finalLine)

	// Add vertical padding below
	for i := 0; i < t.paddingY; i++ {
		result = append(result, emptyLine)
	}

	return result
}

// Ensure TruncatedText implements Component interface.
var _ tui.Component = (*TruncatedText)(nil)
