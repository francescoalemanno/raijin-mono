package test

import (
	"strings"
	"sync"

	"github.com/francescoalemanno/raijin-mono/libtui/pkg/terminal"
)

// VirtualTerminal is a mock terminal for testing.
type VirtualTerminal struct {
	mu       sync.Mutex
	columns  int
	rows     int
	buffer   strings.Builder
	written  []string
	started  bool
	onInput  func(data string)
	onResize func()
	hidden   bool
}

// NewVirtualTerminal creates a new virtual terminal.
func NewVirtualTerminal(columns, rows int) *VirtualTerminal {
	return &VirtualTerminal{
		columns: columns,
		rows:    rows,
		written: []string{},
	}
}

// Start starts the terminal.
func (v *VirtualTerminal) Start(onInput func(data string), onResize func()) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.started = true
	v.onInput = onInput
	v.onResize = onResize
}

// DrainInput is a no-op for virtual terminal.
func (v *VirtualTerminal) DrainInput(maxMs, idleMs int) error {
	return nil
}

// Stop stops the terminal.
func (v *VirtualTerminal) Stop() {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.started = false
}

// Write writes data to the terminal buffer.
func (v *VirtualTerminal) Write(data string) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.buffer.WriteString(data)
	v.written = append(v.written, data)
}

// Columns returns the terminal width.
func (v *VirtualTerminal) Columns() int {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.columns
}

// Rows returns the terminal height.
func (v *VirtualTerminal) Rows() int {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.rows
}

// KittyProtocolActive returns true for testing.
func (v *VirtualTerminal) KittyProtocolActive() bool {
	return true
}

// MoveBy moves the cursor by lines.
func (v *VirtualTerminal) MoveBy(lines int) {
	if lines > 0 {
		v.Write(strings.Repeat("\x1b[1B", lines))
	} else if lines < 0 {
		v.Write(strings.Repeat("\x1b[1A", -lines))
	}
}

// HideCursor hides the cursor.
func (v *VirtualTerminal) HideCursor() {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.hidden = true
	v.Write("\x1b[?25l")
}

// ShowCursor shows the cursor.
func (v *VirtualTerminal) ShowCursor() {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.hidden = false
	v.Write("\x1b[?25h")
}

// ClearLine clears the current line.
func (v *VirtualTerminal) ClearLine() {
	v.Write("\x1b[K")
}

// ClearFromCursor clears from cursor to end of screen.
func (v *VirtualTerminal) ClearFromCursor() {
	v.Write("\x1b[J")
}

// ClearScreen clears the entire screen.
func (v *VirtualTerminal) ClearScreen() {
	v.Write("\x1b[2J\x1b[H")
}

// SetTitle sets the terminal title.
func (v *VirtualTerminal) SetTitle(title string) {
	v.Write("\x1b]0;" + title + "\x07")
}

// Resize resizes the terminal.
func (v *VirtualTerminal) Resize(columns, rows int) {
	v.mu.Lock()
	v.columns = columns
	v.rows = rows
	v.mu.Unlock()
	if v.onResize != nil {
		v.onResize()
	}
}

// Flush waits for output to settle.
func (v *VirtualTerminal) Flush() {
	// No-op for virtual terminal - output is immediate
}

// GetViewport returns the current viewport contents.
func (v *VirtualTerminal) GetViewport() []string {
	v.mu.Lock()
	defer v.mu.Unlock()
	// Parse the buffer to extract visible lines
	content := v.buffer.String()
	lines := strings.Split(content, "\n")
	// Apply terminal size limits
	if len(lines) > v.rows {
		lines = lines[len(lines)-v.rows:]
	}
	return lines
}

// GetWritten returns all written data.
func (v *VirtualTerminal) GetWritten() []string {
	v.mu.Lock()
	defer v.mu.Unlock()
	result := make([]string, len(v.written))
	copy(result, v.written)
	return result
}

// GetBuffer returns the current buffer contents.
func (v *VirtualTerminal) GetBuffer() string {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.buffer.String()
}

// SimulateInput simulates keyboard input.
func (v *VirtualTerminal) SimulateInput(data string) {
	if v.onInput != nil {
		v.onInput(data)
	}
}

// IsHidden returns whether the cursor is hidden.
func (v *VirtualTerminal) IsHidden() bool {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.hidden
}

var _ terminal.Terminal = (*VirtualTerminal)(nil)
