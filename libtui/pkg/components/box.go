package components

import (
	"strings"

	"github.com/francescoalemanno/raijin-mono/libtui/pkg/tui"
	"github.com/francescoalemanno/raijin-mono/libtui/pkg/utils"
)

// NewBox creates a new Box component.
func NewBox(paddingX, paddingY int, bgFn func(string) string) *Box {
	return &Box{
		paddingX: paddingX,
		paddingY: paddingY,
		bgFn:     bgFn,
	}
}

// renderCache holds cached render data.
type renderCache struct {
	childLines []string
	width      int
	bgSample   string
	lines      []string
}

// Box component - a container that applies padding and background to all children.
type Box struct {
	children []tui.Component
	paddingX int
	paddingY int
	bgFn     func(string) string

	// Cache for rendered output
	cache *renderCache
}

// AddChild adds a component to the box.
func (b *Box) AddChild(component tui.Component) {
	b.children = append(b.children, component)
	b.invalidateCache()
}

// RemoveChild removes a component from the box.
func (b *Box) RemoveChild(component tui.Component) {
	for i, child := range b.children {
		if child == component {
			b.children = append(b.children[:i], b.children[i+1:]...)
			b.invalidateCache()
			return
		}
	}
}

// Clear removes all children from the box.
func (b *Box) Clear() {
	b.children = b.children[:0]
	b.invalidateCache()
}

// SetBgFn sets the background function.
func (b *Box) SetBgFn(bgFn func(string) string) {
	b.bgFn = bgFn
	// Don't invalidate here - we'll detect bgFn changes by sampling output
}

// Invalidate clears cached render state for the box and all children.
func (b *Box) Invalidate() {
	b.invalidateCache()
	for _, child := range b.children {
		child.Invalidate()
	}
}

// HandleInput processes keyboard input (no-op for Box).
func (b *Box) HandleInput(data string) {
	// Box doesn't handle input
}

// Render renders all children with padding and background.
func (b *Box) Render(width int) []string {
	if width < 1 {
		width = 1
	}
	if len(b.children) == 0 {
		return []string{}
	}

	maxPadding := max(0, (width-1)/2)
	effectivePaddingX := min(b.paddingX, maxPadding)
	contentWidth := width - effectivePaddingX*2
	if contentWidth < 1 {
		contentWidth = 1
	}
	leftPad := strings.Repeat(" ", effectivePaddingX)

	// Render all children
	childLines := []string{}
	for _, child := range b.children {
		lines := child.Render(contentWidth)
		for _, line := range lines {
			childLines = append(childLines, leftPad+line)
		}
	}

	if len(childLines) == 0 {
		return []string{}
	}

	// Check if bgFn output changed by sampling
	bgSample := ""
	if b.bgFn != nil {
		bgSample = b.bgFn("test")
	}

	// Check cache validity
	if b.matchCache(width, childLines, bgSample) {
		return b.cache.lines
	}

	// Apply background and padding
	result := []string{}

	// Top padding
	for i := 0; i < b.paddingY; i++ {
		result = append(result, b.applyBg("", width))
	}

	// Content
	for _, line := range childLines {
		result = append(result, b.applyBg(line, width))
	}

	// Bottom padding
	for i := 0; i < b.paddingY; i++ {
		result = append(result, b.applyBg("", width))
	}

	// Update cache
	b.cache = &renderCache{
		childLines: childLines,
		width:      width,
		bgSample:   bgSample,
		lines:      result,
	}

	return result
}

func (b *Box) invalidateCache() {
	b.cache = nil
}

func (b *Box) matchCache(width int, childLines []string, bgSample string) bool {
	if b.cache == nil {
		return false
	}
	if b.cache.width != width {
		return false
	}
	if b.cache.bgSample != bgSample {
		return false
	}
	if len(b.cache.childLines) != len(childLines) {
		return false
	}
	for i, line := range childLines {
		if b.cache.childLines[i] != line {
			return false
		}
	}
	return true
}

func (b *Box) applyBg(line string, width int) string {
	visLen := utils.VisibleWidth(line)
	if visLen > width {
		line = utils.TruncateToWidth(line, width, "")
		visLen = width
	}
	padNeeded := width - visLen
	if padNeeded < 0 {
		padNeeded = 0
	}
	padded := line + strings.Repeat(" ", padNeeded)

	if b.bgFn != nil {
		return utils.ApplyBackgroundToLine(padded, width, b.bgFn)
	}
	return padded
}

// Ensure Box implements Component interface.
var _ tui.Component = (*Box)(nil)
