package terminal

import (
	"fmt"
	"io"
	"os"
	"os/signal"
	"regexp"
	"runtime"
	"sync"
	"syscall"
	"time"

	"golang.org/x/term"

	"github.com/francescoalemanno/raijin-mono/libtui/pkg/keys"
)

// Terminal interface defines the contract for terminal operations
type Terminal interface {
	// Start the terminal with input and resize handlers
	Start(onInput func(string), onResize func())

	// Stop the terminal and restore state
	Stop()

	// DrainInput drains stdin before exiting to prevent Kitty key release events from leaking
	// to the parent shell over slow SSH connections.
	// maxMs: Maximum time to drain (default: 1000ms)
	// idleMs: Exit early if no input arrives within this time (default: 50ms)
	DrainInput(maxMs, idleMs int) error

	// Write output to terminal
	Write(data string)

	// Get terminal dimensions
	Columns() int
	Rows() int

	// Whether Kitty keyboard protocol is active
	KittyProtocolActive() bool

	// Cursor positioning (relative to current position)
	MoveBy(lines int) // Move cursor up (negative) or down (positive) by N lines

	// Cursor visibility
	HideCursor() // Hide the cursor
	ShowCursor() // Show the cursor

	// Clear operations
	ClearLine()       // Clear current line
	ClearFromCursor() // Clear from cursor to end of screen
	ClearScreen()     // Clear entire screen and move cursor to (0,0)

	// Title operations
	SetTitle(title string) // Set terminal window title
}

// ProcessTerminal implements Terminal using OS stdin/stdout
type ProcessTerminal struct {
	wasRaw              bool
	inputHandler        func(string)
	resizeHandler       func()
	kittyProtocolActive bool
	stdinBuffer         *StdinBuffer
	mu                  sync.Mutex
	writeLogPath        string
	quitChan            chan struct{}
	dataChan            chan []byte
}

var (
	// ANSI escape sequences
	BRACKETED_PASTE_ENABLE  = "\x1b[?2004h"
	BRACKETED_PASTE_DISABLE = "\x1b[?2004l"
	CURSOR_HIDE             = "\x1b[?25l"
	CURSOR_SHOW             = "\x1b[?25h"
	CLEAR_LINE              = "\x1b[K"
	CLEAR_FROM_CURSOR       = "\x1b[J"
	CLEAR_SCREEN_HOME       = "\x1b[2J\x1b[H"
	CURSOR_UP               = "\x1b[%dA"
	CURSOR_DOWN             = "\x1b[%dB"
	KITTY_QUERY             = "\x1b[?u"
	KITTY_ENABLE            = "\x1b[>7u"
	KITTY_DISABLE           = "\x1b[<u"
	OSC_TITLE_PREFIX        = "\x1b]0;"
	OSC_TITLE_SUFFIX        = "\x07"
	KITTY_RESPONSE_PATTERN  *regexp.Regexp
)

func init() {
	// Pre-compile regex for Kitty protocol response
	KITTY_RESPONSE_PATTERN = regexp.MustCompile(`^\x1b\[\?(\d+)u$`)
}

// NewProcessTerminal creates a new ProcessTerminal
func NewProcessTerminal() *ProcessTerminal {
	writeLogPath := os.Getenv("TAU_TUI_WRITE_LOG")
	return &ProcessTerminal{
		writeLogPath: writeLogPath,
		quitChan:     make(chan struct{}),
		dataChan:     make(chan []byte, 100),
	}
}

// KittyProtocolActive returns whether the Kitty keyboard protocol is active
func (pt *ProcessTerminal) KittyProtocolActive() bool {
	pt.mu.Lock()
	defer pt.mu.Unlock()
	return pt.kittyProtocolActive
}

// Start initializes the terminal and starts listening for input
func (pt *ProcessTerminal) Start(onInput func(string), onResize func()) {
	pt.mu.Lock()
	pt.inputHandler = onInput
	pt.resizeHandler = onResize
	pt.mu.Unlock()

	// Save previous state and enable raw mode (Unix)
	wasRaw := setRawMode()
	pt.wasRaw = wasRaw

	// Enable bracketed paste mode
	pt.writeOutput(BRACKETED_PASTE_ENABLE)

	// Set up resize handler
	pt.setupResizeHandler(onResize)

	// Refresh terminal dimensions on Unix
	// Send SIGWINCH to ourselves to refresh dimensions (may be stale after suspend/resume)
	if runtime.GOOS != "windows" {
		syscall.Kill(os.Getpid(), syscall.SIGWINCH)
	}

	// Enable Windows VT input if needed
	pt.enableWindowsVTInput()

	// Query and enable Kitty keyboard protocol
	pt.queryAndEnableKittyProtocol()

	// Start reading stdin in a goroutine
	go pt.readStdin()
}

// setupStdinBuffer sets up the stdin buffer for processing input
func (pt *ProcessTerminal) setupStdinBuffer() {
	pt.stdinBuffer = NewStdinBuffer(StdinBufferOptions{Timeout: 10})

	// Data handler: forward individual sequences or detect Kitty protocol response
	pt.stdinBuffer.SetOnData(func(sequence string) {
		pt.mu.Lock()
		kittyActive := pt.kittyProtocolActive
		pt.mu.Unlock()

		// Check for Kitty protocol response (only if not already enabled)
		if !kittyActive {
			if match := KITTY_RESPONSE_PATTERN.FindStringSubmatch(sequence); match != nil {
				pt.mu.Lock()
				pt.kittyProtocolActive = true
				pt.mu.Unlock()

				// Set the global Kitty protocol state in keys package
				keys.SetKittyProtocolActive(true)

				// Enable Kitty keyboard protocol (push flags)
				// Flag 1 (disambiguate escape codes) + 2 (report event types) + 4 (report alternate keys)
				pt.writeOutput(KITTY_ENABLE)

				// Don't forward protocol response to TUI
				return
			}
		}

		pt.mu.Lock()
		handler := pt.inputHandler
		pt.mu.Unlock()

		if handler != nil {
			handler(sequence)
		}
	})

	// Paste handler: re-wrap paste content with bracketed paste markers
	pt.stdinBuffer.SetOnPaste(func(content string) {
		pt.mu.Lock()
		handler := pt.inputHandler
		pt.mu.Unlock()

		if handler != nil {
			handler(BRACKETED_PASTE_START + content + BRACKETED_PASTE_END)
		}
	})
}

// queryAndEnableKittyProtocol queries the terminal for Kitty keyboard protocol support
func (pt *ProcessTerminal) queryAndEnableKittyProtocol() {
	pt.setupStdinBuffer()

	// Process data through stdin buffer
	go pt.processStdinBuffer()

	// Send query to terminal
	pt.writeOutput(KITTY_QUERY)
}

// DrainInput drains stdin to prevent Kitty key release events from leaking
func (pt *ProcessTerminal) DrainInput(maxMs, idleMs int) error {
	pt.mu.Lock()
	kittyActive := pt.kittyProtocolActive
	pt.mu.Unlock()

	if kittyActive {
		// Disable Kitty keyboard protocol first
		pt.writeOutput(KITTY_DISABLE)
		pt.mu.Lock()
		pt.kittyProtocolActive = false
		pt.mu.Unlock()
		keys.SetKittyProtocolActive(false)
	}

	// Save and clear input handler
	pt.mu.Lock()
	previousHandler := pt.inputHandler
	pt.inputHandler = nil
	pt.mu.Unlock()

	lastDataTime := time.Now()
	endTime := time.Now().Add(time.Duration(maxMs) * time.Millisecond)
	idleDuration := time.Duration(idleMs) * time.Millisecond

	// Wait for drain
drainLoop:
	for {
		now := time.Now()
		timeLeft := endTime.Sub(now)

		if timeLeft <= 0 {
			break
		}

		idleSinceLastData := now.Sub(lastDataTime)
		if idleSinceLastData >= idleDuration {
			break
		}

		// Wait for data or timeout
		select {
		case <-pt.dataChan:
			lastDataTime = time.Now()
		case <-time.After(min(timeLeft, idleDuration-idleSinceLastData)):
			break drainLoop
		case <-pt.quitChan:
			break drainLoop
		}
	}

	// Restore input handler
	pt.mu.Lock()
	pt.inputHandler = previousHandler
	pt.mu.Unlock()

	return nil
}

// Stop cleans up and restores terminal state
func (pt *ProcessTerminal) Stop() {
	// Disable bracketed paste mode
	pt.writeOutput(BRACKETED_PASTE_DISABLE)

	// Disable Kitty keyboard protocol if not already done
	pt.mu.Lock()
	kittyActive := pt.kittyProtocolActive
	pt.kittyProtocolActive = false
	pt.mu.Unlock()

	if kittyActive {
		pt.writeOutput(KITTY_DISABLE)
		keys.SetKittyProtocolActive(false)
	}

	// Clean up StdinBuffer
	if pt.stdinBuffer != nil {
		pt.stdinBuffer.Destroy()
		pt.stdinBuffer = nil
	}

	// Clear handlers
	pt.mu.Lock()
	pt.inputHandler = nil
	pt.resizeHandler = nil
	pt.mu.Unlock()

	// Stop signal goroutines
	close(pt.quitChan)

	// Restore terminal state (raw mode)
	restoreTerminalMode(pt.wasRaw)
}

// Write writes data to stdout
func (pt *ProcessTerminal) Write(data string) {
	pt.writeOutput(data)
}

// Columns returns the terminal width
func (pt *ProcessTerminal) Columns() int {
	w, _, err := getTerminalSize()
	if err != nil || w == 0 {
		return 80
	}
	return w
}

// Rows returns the terminal height
func (pt *ProcessTerminal) Rows() int {
	_, h, err := getTerminalSize()
	if err != nil || h == 0 {
		return 24
	}
	return h
}

// MoveBy moves the cursor by the specified number of lines
func (pt *ProcessTerminal) MoveBy(lines int) {
	if lines > 0 {
		pt.writeOutput(fmt.Sprintf(CURSOR_DOWN, lines))
	} else if lines < 0 {
		pt.writeOutput(fmt.Sprintf(CURSOR_UP, -lines))
	}
	// lines == 0: no movement
}

// HideCursor hides the cursor
func (pt *ProcessTerminal) HideCursor() {
	pt.writeOutput(CURSOR_HIDE)
}

// ShowCursor shows the cursor
func (pt *ProcessTerminal) ShowCursor() {
	pt.writeOutput(CURSOR_SHOW)
}

// ClearLine clears the current line
func (pt *ProcessTerminal) ClearLine() {
	pt.writeOutput(CLEAR_LINE)
}

// ClearFromCursor clears from cursor to end of screen
func (pt *ProcessTerminal) ClearFromCursor() {
	pt.writeOutput(CLEAR_FROM_CURSOR)
}

// ClearScreen clears the entire screen and moves cursor to (1,1)
func (pt *ProcessTerminal) ClearScreen() {
	pt.writeOutput(CLEAR_SCREEN_HOME)
}

// SetTitle sets the terminal window title via OSC 0
func (pt *ProcessTerminal) SetTitle(title string) {
	pt.writeOutput(OSC_TITLE_PREFIX + title + OSC_TITLE_SUFFIX)
}

// readStdin reads from stdin in a separate goroutine
func (pt *ProcessTerminal) readStdin() {
	buf := make([]byte, 1024)
	for {
		select {
		case <-pt.quitChan:
			return
		default:
			n, err := os.Stdin.Read(buf)
			if err != nil {
				if err != io.EOF {
					// Log error if needed
				}
				continue
			}
			if n > 0 {
				data := make([]byte, n)
				copy(data, buf[:n])
				pt.dataChan <- data
			}
		}
	}
}

// processStdinBuffer processes data from dataChan through stdinBuffer
func (pt *ProcessTerminal) processStdinBuffer() {
	for {
		select {
		case <-pt.quitChan:
			return
		case data := <-pt.dataChan:
			pt.stdinBuffer.Process(data)
		}
	}
}

// setupResizeHandler sets up handler for terminal resize events
func (pt *ProcessTerminal) setupResizeHandler(onResize func()) {
	if runtime.GOOS == "windows" {
		// Windows doesn't have SIGWINCH; skip
		return
	}

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGWINCH)

	go func() {
		for {
			select {
			case <-sigChan:
				pt.mu.Lock()
				handler := pt.resizeHandler
				pt.mu.Unlock()
				if handler != nil {
					handler()
				}
			case <-pt.quitChan:
				return
			}
		}
	}()
}

// enableWindowsVTInput enables VT input mode on Windows (TODO)
func (pt *ProcessTerminal) enableWindowsVTInput() {
	if runtime.GOOS != "windows" {
		return
	}
	// TODO: Use syscall or golang.org/x/sys/windows to enable ENABLE_VIRTUAL_TERMINAL_INPUT
	// For now, Shift+Tab won't be distinguishable from Tab on Windows
}

// writeOutput writes data to stdout with optional file logging
func (pt *ProcessTerminal) writeOutput(data string) {
	os.Stdout.WriteString(data)

	// Optional: write to log file
	if pt.writeLogPath != "" {
		f, err := os.OpenFile(pt.writeLogPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
		if err == nil {
			f.WriteString(data)
			f.Close()
		}
	}
}

// savedTermState holds the original terminal state for restoration.
var savedTermState *term.State

// setRawMode enables raw mode using golang.org/x/term.
func setRawMode() bool {
	state, err := term.MakeRaw(int(os.Stdin.Fd()))
	if err != nil {
		return false
	}
	savedTermState = state
	return true
}

// restoreTerminalMode restores the terminal to its original state.
func restoreTerminalMode(wasRaw bool) {
	if wasRaw && savedTermState != nil {
		term.Restore(int(os.Stdin.Fd()), savedTermState)
	}
}

// getTerminalSize returns the terminal dimensions.
func getTerminalSize() (cols int, rows int, err error) {
	return term.GetSize(int(os.Stdout.Fd()))
}

// Write writes data directly to stdout (utility function)
func Write(data string) {
	os.Stdout.WriteString(data)
}
