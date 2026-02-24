package tui

// Component is the interface that all TUI components must implement.
type Component interface {
	// Render returns an array of strings, one per line. Each line must not exceed width.
	Render(width int) []string

	// HandleInput is called when the component has focus and receives keyboard input.
	HandleInput(data string)

	// Invalidate clears any cached render state. Components should re-render from scratch.
	Invalidate()
}

// Focusable is implemented by components that display a text cursor and need IME support.
type Focusable interface {
	// Focused is set by TUI when focus changes.
	SetFocused(bool)
	IsFocused() bool
}

// IsFocusable checks if a component implements the Focusable interface.
func IsFocusable(c Component) bool {
	_, ok := c.(Focusable)
	return ok
}

// SizeValue can be absolute (number) or percentage (string like "50%")
type SizeValue struct {
	IsPercent bool
	Value     float64
}

// CURSOR_MARKER is the cursor position marker - an APC escape sequence.
// Components emit this at the cursor position when focused.
const CURSOR_MARKER = "\x1b_raijin:c\x07"
