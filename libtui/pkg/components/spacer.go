package components

import (
	"github.com/francescoalemanno/raijin-mono/libtui/pkg/tui"
)

// NewSpacer creates a new Spacer component with given number of lines.
func NewSpacer(lines int) *Spacer {
	return &Spacer{lines: lines}
}

// Spacer component that renders empty lines.
type Spacer struct {
	lines int
}

// SetLines changes the number of empty lines to render.
func (s *Spacer) SetLines(lines int) {
	s.lines = lines
}

// Invalidate clears cached state (no-op for Spacer as it has no cache).
func (s *Spacer) Invalidate() {
	// No cached state to invalidate
}

// HandleInput processes keyboard input (no-op for Spacer).
func (s *Spacer) HandleInput(data string) {
	// Spacer doesn't handle input
}

// Render returns empty lines.
func (s *Spacer) Render(width int) []string {
	result := make([]string, s.lines)
	for i := range result {
		result[i] = ""
	}
	return result
}

// Ensure Spacer implements Component interface.
var _ tui.Component = (*Spacer)(nil)
