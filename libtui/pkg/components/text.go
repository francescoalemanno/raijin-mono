package components

import (
	"strings"

	"github.com/francescoalemanno/raijin-mono/libtui/pkg/tui"
	"github.com/francescoalemanno/raijin-mono/libtui/pkg/utils"
)

// NewText creates a new Text component.
func NewText(text string, paddingX, paddingY int, customBgFn func(string) string) *Text {
	return &Text{
		text:       text,
		paddingX:   paddingX,
		paddingY:   paddingY,
		customBgFn: customBgFn,
	}
}

// Text component that displays multi-line text with word wrapping.
type Text struct {
	text       string
	paddingX   int                 // Left/right padding
	paddingY   int                 // Top/bottom padding
	customBgFn func(string) string // Optional background function

	// Cache for rendered output
	cachedText  string
	cachedWidth int
	cachedLines []string
}

// SetText changes the text content and invalidates cache.
func (t *Text) SetText(text string) {
	t.text = text
	t.invalidateCache()
}

// SetCustomBgFn changes the background function and invalidates cache.
func (t *Text) SetCustomBgFn(customBgFn func(string) string) {
	t.customBgFn = customBgFn
	t.invalidateCache()
}

// Invalidate clears cached render state.
func (t *Text) Invalidate() {
	t.invalidateCache()
}

// HandleInput processes keyboard input (no-op for Text).
func (t *Text) HandleInput(data string) {
	// Text doesn't handle input
}

// Render renders the text with word wrapping.
func (t *Text) Render(width int) []string {
	if width < 1 {
		width = 1
	}

	// Check cache
	if t.cachedLines != nil && t.cachedText == t.text && t.cachedWidth == width {
		return t.cachedLines
	}

	// Don't render anything if there's no actual text
	if t.text == "" || strings.TrimSpace(t.text) == "" {
		result := []string{}
		t.cachedText = t.text
		t.cachedWidth = width
		t.cachedLines = result
		return result
	}

	// Replace tabs with 3 spaces
	normalizedText := strings.ReplaceAll(t.text, "\t", "   ")

	// Calculate content width (subtract left/right margins)
	maxPadding := max(0, (width-1)/2)
	effectivePaddingX := min(t.paddingX, maxPadding)
	contentWidth := width - effectivePaddingX*2
	if contentWidth < 1 {
		contentWidth = 1
	}

	// Wrap text (this preserves ANSI codes but does NOT pad)
	wrappedLines := utils.WrapTextWithAnsi(normalizedText, contentWidth)

	// Add margins and background to each line
	leftMargin := strings.Repeat(" ", effectivePaddingX)
	rightMargin := strings.Repeat(" ", effectivePaddingX)
	contentLines := []string{}

	for _, line := range wrappedLines {
		// Add margins
		lineWithMargins := leftMargin + line + rightMargin

		// Apply background if specified (this also pads to full width)
		if t.customBgFn != nil {
			contentLines = append(contentLines, utils.TruncateToWidth(utils.ApplyBackgroundToLine(lineWithMargins, width, t.customBgFn), width, ""))
		} else {
			// No background - just pad to width with spaces
			visibleLen := utils.VisibleWidth(lineWithMargins)
			paddingNeeded := width - visibleLen
			if paddingNeeded < 0 {
				paddingNeeded = 0
			}
			contentLines = append(contentLines, utils.TruncateToWidth(lineWithMargins+strings.Repeat(" ", paddingNeeded), width, ""))
		}
	}

	// Add top/bottom padding (empty lines)
	emptyLine := strings.Repeat(" ", width)
	emptyLines := []string{}
	for i := 0; i < t.paddingY; i++ {
		var line string
		if t.customBgFn != nil {
			line = utils.ApplyBackgroundToLine(emptyLine, width, t.customBgFn)
		} else {
			line = emptyLine
		}
		emptyLines = append(emptyLines, line)
	}

	// Combine: top padding + content + bottom padding
	result := append(emptyLines, contentLines...)
	result = append(result, emptyLines...)

	// Update cache
	t.cachedText = t.text
	t.cachedWidth = width
	t.cachedLines = result

	// Return at least one empty string if result is empty
	if len(result) == 0 {
		return []string{""}
	}
	return result
}

func (t *Text) invalidateCache() {
	t.cachedText = ""
	t.cachedWidth = 0
	t.cachedLines = nil
}

// Ensure Text implements Component interface.
var _ tui.Component = (*Text)(nil)
