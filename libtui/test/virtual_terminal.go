package test

import (
	"strconv"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/francescoalemanno/raijin-mono/libtui/pkg/terminal"
)

// VirtualTerminal is an in-memory terminal emulator for TUI tests.
// It captures writes and applies a small ANSI subset used by the renderer.
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

	screen    [][]rune
	cursorRow int
	cursorCol int
}

// NewVirtualTerminal creates a new virtual terminal.
func NewVirtualTerminal(columns, rows int) *VirtualTerminal {
	v := &VirtualTerminal{
		columns: columns,
		rows:    rows,
		written: []string{},
	}
	v.resetScreenLocked()
	return v
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
	v.applyANSIToScreenLocked(data)
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
		v.Write("\x1b[" + strconv.Itoa(lines) + "B")
	} else if lines < 0 {
		v.Write("\x1b[" + strconv.Itoa(-lines) + "A")
	}
}

// HideCursor hides the cursor.
func (v *VirtualTerminal) HideCursor() {
	v.Write("\x1b[?25l")
}

// ShowCursor shows the cursor.
func (v *VirtualTerminal) ShowCursor() {
	v.Write("\x1b[?25h")
}

// ClearLine clears the current line.
func (v *VirtualTerminal) ClearLine() {
	v.Write("\x1b[2K")
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

// Resize resizes the terminal and clears screen state.
func (v *VirtualTerminal) Resize(columns, rows int) {
	v.mu.Lock()
	v.columns = max(1, columns)
	v.rows = max(1, rows)
	v.resetScreenLocked()
	cb := v.onResize
	v.mu.Unlock()

	if cb != nil {
		cb()
	}
}

// Flush waits for output to settle.
func (v *VirtualTerminal) Flush() {
	// No-op for virtual terminal - output is immediate
}

// GetViewport returns the current visible rows (right-trimmed).
func (v *VirtualTerminal) GetViewport() []string {
	v.mu.Lock()
	defer v.mu.Unlock()

	out := make([]string, len(v.screen))
	for i, row := range v.screen {
		out[i] = strings.TrimRight(string(row), " ")
	}
	return out
}

// GetLine returns one visible row (right-trimmed).
func (v *VirtualTerminal) GetLine(row int) string {
	v.mu.Lock()
	defer v.mu.Unlock()
	if row < 0 || row >= len(v.screen) {
		return ""
	}
	return strings.TrimRight(string(v.screen[row]), " ")
}

// GetCursor returns the current cursor position (0-indexed row, col).
func (v *VirtualTerminal) GetCursor() (int, int) {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.cursorRow, v.cursorCol
}

// GetWritten returns all written chunks.
func (v *VirtualTerminal) GetWritten() []string {
	v.mu.Lock()
	defer v.mu.Unlock()
	result := make([]string, len(v.written))
	copy(result, v.written)
	return result
}

// GetBuffer returns all raw output written to the terminal.
func (v *VirtualTerminal) GetBuffer() string {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.buffer.String()
}

// SimulateInput simulates keyboard input.
func (v *VirtualTerminal) SimulateInput(data string) {
	v.mu.Lock()
	cb := v.onInput
	v.mu.Unlock()
	if cb != nil {
		cb(data)
	}
}

// IsHidden returns whether the cursor is hidden.
func (v *VirtualTerminal) IsHidden() bool {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.hidden
}

func (v *VirtualTerminal) resetScreenLocked() {
	v.screen = make([][]rune, max(1, v.rows))
	for i := range v.screen {
		v.screen[i] = make([]rune, max(1, v.columns))
		for j := range v.screen[i] {
			v.screen[i][j] = ' '
		}
	}
	v.cursorRow = 0
	v.cursorCol = 0
}

func (v *VirtualTerminal) applyANSIToScreenLocked(data string) {
	for i := 0; i < len(data); {
		if data[i] == '\x1b' {
			next := byte(0)
			if i+1 < len(data) {
				next = data[i+1]
			}

			switch next {
			case '[':
				j := i + 2
				for j < len(data) && (data[j] < 0x40 || data[j] > 0x7e) {
					j++
				}
				if j >= len(data) {
					return
				}
				params := data[i+2 : j]
				final := data[j]
				v.handleCSILocked(params, final)
				i = j + 1
				continue
			case ']', '_':
				j := i + 2
				for j < len(data) {
					if data[j] == '\a' {
						j++
						break
					}
					if j+1 < len(data) && data[j] == '\x1b' && data[j+1] == '\\' {
						j += 2
						break
					}
					j++
				}
				i = min(j, len(data))
				continue
			default:
				i += 2
				continue
			}
		}

		r, size := utf8.DecodeRuneInString(data[i:])
		if size == 0 {
			break
		}
		i += size

		switch r {
		case '\r':
			v.cursorCol = 0
		case '\n':
			v.newLineLocked()
		default:
			if r < 32 {
				continue
			}
			v.writeRuneLocked(r)
		}
	}
}

func (v *VirtualTerminal) handleCSILocked(params string, final byte) {
	param := strings.TrimSpace(params)

	switch final {
	case 'A':
		n := parseIntWithDefault(stripPrivatePrefix(param), 1)
		v.cursorRow = max(0, v.cursorRow-n)
	case 'B':
		n := parseIntWithDefault(stripPrivatePrefix(param), 1)
		v.cursorRow = min(v.rows-1, v.cursorRow+n)
	case 'G':
		n := parseIntWithDefault(stripPrivatePrefix(param), 1)
		v.cursorCol = min(v.columns-1, max(0, n-1))
	case 'H':
		parts := strings.Split(stripPrivatePrefix(param), ";")
		row := 1
		col := 1
		if len(parts) >= 1 && parts[0] != "" {
			row = parseIntWithDefault(parts[0], 1)
		}
		if len(parts) >= 2 && parts[1] != "" {
			col = parseIntWithDefault(parts[1], 1)
		}
		v.cursorRow = min(v.rows-1, max(0, row-1))
		v.cursorCol = min(v.columns-1, max(0, col-1))
	case 'J':
		if param == "2" || param == "3" {
			v.resetScreenLocked()
		}
	case 'K':
		mode := stripPrivatePrefix(param)
		if mode == "2" || mode == "" {
			for c := 0; c < v.columns; c++ {
				v.screen[v.cursorRow][c] = ' '
			}
		}
	case 'm':
		// Styling ignored for viewport text.
	case 'h':
		if strings.HasPrefix(param, "?25") {
			v.hidden = false
		}
	case 'l':
		if strings.HasPrefix(param, "?25") {
			v.hidden = true
		}
	}
}

func (v *VirtualTerminal) writeRuneLocked(r rune) {
	if v.cursorCol >= v.columns {
		v.newLineLocked()
	}
	if v.cursorRow < 0 || v.cursorRow >= v.rows || v.cursorCol < 0 || v.cursorCol >= v.columns {
		return
	}
	v.screen[v.cursorRow][v.cursorCol] = r
	v.cursorCol++
}

func (v *VirtualTerminal) newLineLocked() {
	v.cursorCol = 0
	if v.cursorRow < v.rows-1 {
		v.cursorRow++
		return
	}
	for r := 0; r < v.rows-1; r++ {
		copy(v.screen[r], v.screen[r+1])
	}
	for c := 0; c < v.columns; c++ {
		v.screen[v.rows-1][c] = ' '
	}
}

func stripPrivatePrefix(s string) string {
	return strings.TrimPrefix(s, "?")
}

func parseIntWithDefault(s string, fallback int) int {
	if s == "" {
		return fallback
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return fallback
	}
	return n
}

var _ terminal.Terminal = (*VirtualTerminal)(nil)
