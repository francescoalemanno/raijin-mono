// Package tui provides a terminal user interface framework.
//
// This package is derived from the pi-mono TUI library.
// Original work copyright (c) 2025 Mario Zechner, licensed under MIT License.
// Source: https://github.com/badlogic/pi-mono
package tui

import (
	"context"
	"fmt"
	"os"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/francescoalemanno/raijin-mono/internal/paths"
	"github.com/francescoalemanno/raijin-mono/libtui/pkg/keys"
	"github.com/francescoalemanno/raijin-mono/libtui/pkg/terminal"
	"github.com/francescoalemanno/raijin-mono/libtui/pkg/utils"
)

type uiTask struct {
	fn    func()
	force bool
}

// InputListenerResult is the return type for input listeners
type InputListenerResult struct {
	Consume bool
	Data    string
}

// InputListener is a function that processes input
type InputListener func(data string) *InputListenerResult

// Container is a component that contains other components.
// All methods must be called from the UI event-loop goroutine.
type Container struct {
	children []Component
}

// AddChild adds a child component
func (c *Container) AddChild(component Component) {
	if componentIsNil(component) {
		return
	}
	c.children = append(c.children, component)
}

// RemoveChild removes a child component
func (c *Container) RemoveChild(component Component) {
	for i, child := range c.children {
		if child == component {
			c.children = append(c.children[:i], c.children[i+1:]...)
			return
		}
	}
}

// Clear removes all children
func (c *Container) Clear() {
	c.children = nil
}

// Invalidate invalidates all children
func (c *Container) Invalidate() {
	for _, child := range c.children {
		if componentIsNil(child) {
			continue
		}
		if inv, ok := child.(interface{ Invalidate() }); ok {
			inv.Invalidate()
		}
	}
}

// HandleInput is a no-op for Container.
func (c *Container) HandleInput(data string) {}

// Render renders all children
func (c *Container) Render(width int) []string {
	var lines []string
	for _, child := range c.children {
		if componentIsNil(child) {
			continue
		}
		lines = append(lines, child.Render(width)...)
	}
	return lines
}

func componentIsNil(c Component) bool {
	if c == nil {
		return true
	}

	v := reflect.ValueOf(c)
	switch v.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return v.IsNil()
	default:
		return false
	}
}

// TUI is the main class for managing terminal UI with differential rendering
type TUI struct {
	*Container
	Terminal terminal.Terminal

	previousLines        []string
	previousWidth        int
	previousHeight       int
	focusedComponent     Component
	inputListeners       []InputListener
	onDebug              func()
	cursorRow            int
	hardwareCursorRow    int
	showHardwareCursor   bool
	clearOnShrink        bool
	maxLinesRendered     int
	previousViewportTop  int
	fullRedrawCount      atomic.Int64
	stopped              atomic.Bool
	dynamicRenderDelayNS atomic.Int64

	tasks  chan uiTask
	stopCh chan struct{}
	doneCh chan struct{}

	enqueueMu     sync.Mutex
	renderPending bool
	renderForce   bool

	inputMu      sync.Mutex
	inputQueue   []string
	inputPending bool

	lastRenderTime     time.Time
	delayedRenderArmed atomic.Bool
}

// NewTUI creates a new TUI instance
func NewTUI(t terminal.Terminal, showHardwareCursor ...bool) *TUI {
	tui := &TUI{
		Container:          &Container{},
		Terminal:           t,
		inputListeners:     []InputListener{},
		showHardwareCursor: os.Getenv("TAU_HARDWARE_CURSOR") == "1",
		clearOnShrink:      os.Getenv("TAU_CLEAR_ON_SHRINK") == "1",
		tasks:              make(chan uiTask, 256),
		stopCh:             make(chan struct{}, 1),
		doneCh:             make(chan struct{}),
	}
	if len(showHardwareCursor) > 0 {
		tui.showHardwareCursor = showHardwareCursor[0]
	}
	go tui.loop()
	return tui
}

// FullRedraws returns the number of full redraws performed
func (t *TUI) FullRedraws() int {
	return int(t.fullRedrawCount.Load())
}

// GetShowHardwareCursor returns whether hardware cursor is shown
func (t *TUI) GetShowHardwareCursor() bool {
	return t.showHardwareCursor
}

// SetShowHardwareCursor sets whether to show hardware cursor
func (t *TUI) SetShowHardwareCursor(enabled bool) {
	if t.showHardwareCursor == enabled {
		return
	}
	t.showHardwareCursor = enabled
	if !enabled {
		t.Terminal.HideCursor()
	}
	t.RequestRender()
}

// GetClearOnShrink returns whether to clear on shrink
func (t *TUI) GetClearOnShrink() bool {
	return t.clearOnShrink
}

// SetClearOnShrink sets whether to trigger full re-render when content shrinks
func (t *TUI) SetClearOnShrink(enabled bool) {
	t.clearOnShrink = enabled
}

// SetFocus sets the focused component
func (t *TUI) SetFocus(component Component) {
	// Clear focused flag on old component
	if t.focusedComponent != nil {
		if f, ok := t.focusedComponent.(interface{ SetFocused(bool) }); ok {
			f.SetFocused(false)
		}
	}

	t.focusedComponent = component

	// Set focused flag on new component
	if component != nil {
		if f, ok := component.(interface{ SetFocused(bool) }); ok {
			f.SetFocused(true)
		}
	}
}

// Invalidate invalidates all components
func (t *TUI) Invalidate() {
	t.Container.Invalidate()
}

// Start starts the TUI
func (t *TUI) Start() {
	t.stopped.Store(false)
	t.Terminal.Start(t.handleInput, func() { t.RequestRender(true) })
	t.Terminal.HideCursor()
	t.RequestRender()
}

// AddInputListener adds an input listener and returns a function to remove it.
// Must be called from the UI event-loop goroutine.
func (t *TUI) AddInputListener(listener InputListener) func() {
	t.inputListeners = append(t.inputListeners, listener)
	return func() {
		t.RemoveInputListener(listener)
	}
}

// RemoveInputListener removes an input listener.
// Must be called from the UI event-loop goroutine.
func (t *TUI) RemoveInputListener(listener InputListener) {
	for i, l := range t.inputListeners {
		if reflect.ValueOf(l).Pointer() == reflect.ValueOf(listener).Pointer() {
			t.inputListeners = append(t.inputListeners[:i], t.inputListeners[i+1:]...)
			break
		}
	}
}

// Stop stops the TUI
func (t *TUI) Stop() {
	t.stopped.Store(true)
	select {
	case t.stopCh <- struct{}{}:
	default:
	}
	<-t.doneCh

	if len(t.previousLines) > 0 {
		targetRow := len(t.previousLines)
		lineDiff := targetRow - t.hardwareCursorRow
		if lineDiff > 0 {
			t.Terminal.Write(fmt.Sprintf("\x1b[%dB", lineDiff))
		} else if lineDiff < 0 {
			t.Terminal.Write(fmt.Sprintf("\x1b[%dA", -lineDiff))
		}
		t.Terminal.Write("\r\n")
	}
	t.Terminal.ShowCursor()
	t.Terminal.Stop()
}

// RequestRender requests a render.
// Render requests are coalesced: at most one pending render marker is queued.
func (t *TUI) RequestRender(force ...bool) {
	f := len(force) > 0 && force[0]
	t.enqueueMu.Lock()
	if t.renderPending {
		// Coalesce-to-latest: overwrite pending render intent.
		t.renderForce = f
		t.enqueueMu.Unlock()
		return
	}
	t.renderPending = true
	t.renderForce = f
	t.enqueueMu.Unlock()

	task := uiTask{fn: nil, force: f}
	select {
	case t.tasks <- task:
		return
	case <-t.doneCh:
		t.enqueueMu.Lock()
		t.renderPending = false
		t.renderForce = false
		t.enqueueMu.Unlock()
		return
	default:
	}

	go func() {
		if !t.enqueueTask(task) {
			t.enqueueMu.Lock()
			t.renderPending = false
			t.renderForce = false
			t.enqueueMu.Unlock()
		}
	}()
}

// DispatchOrdered sends fn to be executed in the event loop, blocking until
// accepted or the TUI shuts down. Unlike Dispatch, it never spawns a goroutine,
// so callers that must preserve strict FIFO ordering across consecutive calls
// (e.g. streaming event pipelines) can rely on this guarantee.
func (t *TUI) DispatchOrdered(fn func()) {
	if fn == nil {
		return
	}
	t.enqueueTask(uiTask{fn: fn})
}

// Dispatch sends fn to be executed in the event loop before the next render.
func (t *TUI) Dispatch(fn func()) {
	if fn == nil {
		return
	}
	task := uiTask{fn: fn}
	select {
	case t.tasks <- task:
		return
	case <-t.doneCh:
		return
	default:
	}
	go func() {
		_ = t.enqueueTask(task)
	}()
}

// UIToken is an unforgeable proof that the holder is executing on the UI
// event-loop goroutine. It is only constructible inside DispatchSync.
type UIToken struct{ _ struct{} }

// DispatchSync sends fn to the UI event loop and blocks until it has run.
// It returns false if the operation was abandoned because either ctx was
// cancelled or the TUI shut down before the function could execute.
func (t *TUI) DispatchSync(ctx context.Context, fn func(UIToken)) bool {
	done := make(chan struct{})
	task := uiTask{fn: func() {
		fn(UIToken{})
		close(done)
	}}
	if !t.enqueueTaskWithContext(ctx, task) {
		return false
	}
	select {
	case <-done:
		return true
	case <-ctx.Done():
		return false
	case <-t.doneCh:
		return false
	}
}

func (t *TUI) enqueueTask(task uiTask) bool {
	select {
	case t.tasks <- task:
		return true
	case <-t.doneCh:
		return false
	}
}

func (t *TUI) enqueueTaskWithContext(ctx context.Context, task uiTask) bool {
	select {
	case t.tasks <- task:
		return true
	case <-ctx.Done():
		return false
	case <-t.doneCh:
		return false
	}
}

func (t *TUI) onRenderTaskDequeued(taskForce bool) bool {
	t.enqueueMu.Lock()
	_ = taskForce
	force := t.renderForce
	t.renderPending = false
	t.renderForce = false
	t.enqueueMu.Unlock()
	return force
}

const maxTasksPerRenderCycle = 256

func (t *TUI) loop() {
	defer close(t.doneCh)
	for {
		select {
		case <-t.stopCh:
			return
		case task := <-t.tasks:
			forceRedraw := false
			processed := 0
			handleTask := func(next uiTask) {
				processed++
				if next.fn != nil {
					next.fn()
					return
				}
				if t.onRenderTaskDequeued(next.force) {
					forceRedraw = true
				}
			}

			handleTask(task)

			draining := true
			for draining && processed < maxTasksPerRenderCycle {
				select {
				case <-t.stopCh:
					return
				case next := <-t.tasks:
					handleTask(next)
				default:
					draining = false
				}
			}
			if forceRedraw {
				t.previousLines = nil
				t.previousWidth = -1
				t.previousHeight = -1
				t.cursorRow = 0
				t.hardwareCursorRow = 0
				t.maxLinesRendered = 0
				t.previousViewportTop = 0
			}
			if shouldRender, delay := t.shouldRenderNow(forceRedraw); !shouldRender {
				t.scheduleDelayedRender(delay)
				continue
			}
			start := time.Now()
			t.doRender()
			t.lastRenderTime = time.Now()
			t.dynamicRenderDelayNS.Store(int64(dynamicGateDelay(time.Since(start))))
		}
	}
}

func (t *TUI) shouldRenderNow(force bool) (bool, time.Duration) {
	if force {
		return true, 0
	}
	interval := time.Duration(t.dynamicRenderDelayNS.Load())
	if interval <= 0 {
		return true, 0
	}
	if t.lastRenderTime.IsZero() {
		return true, 0
	}
	elapsed := time.Since(t.lastRenderTime)
	if elapsed >= interval {
		return true, 0
	}
	return false, interval - elapsed
}

func dynamicGateDelay(renderDuration time.Duration) time.Duration {
	if renderDuration <= 0 {
		return 0
	}
	return renderDuration + renderDuration/2
}

func (t *TUI) scheduleDelayedRender(delay time.Duration) {
	if delay <= 0 {
		t.RequestRender()
		return
	}
	if !t.delayedRenderArmed.CompareAndSwap(false, true) {
		return
	}
	time.AfterFunc(delay, func() {
		t.delayedRenderArmed.Store(false)
		t.RequestRender()
	})
}

const maxInputEventsPerBatch = 128

func (t *TUI) handleInput(data string) {
	t.inputMu.Lock()
	t.inputQueue = append(t.inputQueue, data)
	if t.inputPending {
		t.inputMu.Unlock()
		return
	}
	t.inputPending = true
	t.inputMu.Unlock()

	t.Dispatch(t.drainInputQueue)
}

func (t *TUI) drainInputQueue() {
	processed := 0
	for processed < maxInputEventsPerBatch {
		t.inputMu.Lock()
		if len(t.inputQueue) == 0 {
			t.inputPending = false
			t.inputMu.Unlock()
			return
		}
		next := t.inputQueue[0]
		t.inputQueue = t.inputQueue[1:]
		t.inputMu.Unlock()

		t.handleInputDirect(next)
		processed++
	}

	t.Dispatch(t.drainInputQueue)
}

func (t *TUI) handleInputDirect(data string) {
	if len(t.inputListeners) > 0 {
		listeners := t.inputListeners
		current := data
		for _, listener := range listeners {
			result := listener(current)
			if result != nil {
				if result.Consume {
					return
				}
				if result.Data != "" {
					current = result.Data
				}
			}
		}
		if len(current) == 0 {
			return
		}
		data = current
	}

	// Global debug key handler (Shift+Ctrl+D)
	if t.onDebug != nil && keys.MatchesKey(data, "shift+ctrl+d") {
		t.onDebug()
		return
	}

	// Pass input to focused component
	if t.focusedComponent != nil {
		if handler, ok := t.focusedComponent.(interface{ HandleInput(string) }); ok {
			// Filter out key release events unless component opts in
			if keys.IsKeyRelease(data) {
				if wc, ok := t.focusedComponent.(interface{ GetWantsKeyRelease() bool }); ok {
					if !wc.GetWantsKeyRelease() {
						return
					}
				} else {
					return
				}
			}
			handler.HandleInput(data)
			t.RequestRender()
		}
	}
}

const segmentReset = "\x1b[0m\x1b]8;;\x07"

func writeLineWithReset(buffer *strings.Builder, line string) {
	buffer.WriteString(line)
	buffer.WriteString(segmentReset)
}

// extractCursorPosition finds and extracts cursor position from rendered lines
func (t *TUI) extractCursorPosition(lines []string, height int) (row, col int, found bool) {
	viewportTop := max(0, len(lines)-height)
	for row := len(lines) - 1; row >= viewportTop; row-- {
		line := lines[row]
		before, after, ok := strings.Cut(line, CURSOR_MARKER)
		if ok {
			beforeMarker := before
			col = utils.VisibleWidth(beforeMarker)
			lines[row] = before + after
			return row, col, true
		}
	}
	return 0, 0, false
}

// doRender performs the actual rendering
func (t *TUI) doRender() {
	if t.stopped.Load() {
		return
	}

	width := t.Terminal.Columns()
	height := t.Terminal.Rows()
	viewportTop := max(0, t.maxLinesRendered-height)
	prevViewportTop := t.previousViewportTop
	hardwareCursorRow := t.hardwareCursorRow

	computeLineDiff := func(targetRow int) int {
		currentScreenRow := hardwareCursorRow - prevViewportTop
		targetScreenRow := targetRow - viewportTop
		return targetScreenRow - currentScreenRow
	}

	// Render all components
	newLines := t.Container.Render(width)

	// Extract cursor position
	cursorRow, cursorCol, hasCursor := t.extractCursorPosition(newLines, height)
	cursorPos := struct{ Row, Col int }{cursorRow, cursorCol}

	debugRedraw := os.Getenv("RAIJIN_DEBUG_REDRAW") == "1"

	// Width-overflow crash guard: only active under RAIJIN_DEBUG_REDRAW=1.
	// Checked before applyLineResets so lines contain no trailing ANSI resets
	// and VisibleWidth stays on the fast pure-ASCII path for normal content.
	if debugRedraw {
		for i, line := range newLines {
			if utils.VisibleWidth(line) > width {
				crashLogPath := paths.RaijinPath("crash.log")
				var crashData strings.Builder
				crashData.WriteString("Crash at " + time.Now().Format(time.RFC3339) + "\n")
				crashData.WriteString(fmt.Sprintf("Terminal width: %d\n", width))
				crashData.WriteString(fmt.Sprintf("Line %d visible width: %d\n", i, utils.VisibleWidth(line)))
				crashData.WriteString("\n=== All rendered lines ===\n")
				for idx, l := range newLines {
					crashData.WriteString(fmt.Sprintf("[%d] (w=%d) %s\n", idx, utils.VisibleWidth(l), l))
				}
				crashData.WriteString("\n")
				os.MkdirAll(paths.RaijinPath(), 0o755)
				os.WriteFile(crashLogPath, []byte(crashData.String()), 0o644)
				t.Stop()
				panic(fmt.Sprintf("Rendered line %d exceeds terminal width (%d > %d).\n\n"+
					"This is likely caused by a custom TUI component not truncating its output.\n"+
					"Use VisibleWidth() to measure and TruncateToWidth() to truncate lines.\n\n"+
					"Debug log written to: %s", i, utils.VisibleWidth(line), width, crashLogPath))
			}
		}
	}

	// Width/height changed - need full re-render
	widthChanged := t.previousWidth != 0 && t.previousWidth != width
	heightChanged := t.previousHeight != 0 && t.previousHeight != height

	// Helper for full render
	fullRender := func(clear bool) {
		t.fullRedrawCount.Add(1)
		var buffer strings.Builder
		buffer.WriteString("\x1b[?2026h") // Begin synchronized output
		if clear {
			buffer.WriteString("\x1b[3J\x1b[2J\x1b[H") // Clear scrollback, screen, and home
		}
		for i, line := range newLines {
			if i > 0 {
				buffer.WriteString("\r\n")
			}
			writeLineWithReset(&buffer, line)
		}
		buffer.WriteString("\x1b[?2026l") // End synchronized output
		t.Terminal.Write(buffer.String())

		t.cursorRow = max(0, len(newLines)-1)
		t.hardwareCursorRow = t.cursorRow
		if clear {
			t.maxLinesRendered = len(newLines)
		} else {
			t.maxLinesRendered = max(t.maxLinesRendered, len(newLines))
		}
		t.previousViewportTop = max(0, t.maxLinesRendered-height)
		t.positionHardwareCursor(&cursorPos, len(newLines))
		t.previousLines = newLines
		t.previousWidth = width
		t.previousHeight = height
	}

	logRedraw := func(reason string) {
		if !debugRedraw {
			return
		}
		logPath := paths.RaijinPath("tui-debug.log")
		if logPath == "" {
			return
		}
		msg := `[` + time.Now().Format(time.RFC3339) + `] fullRender: ` + reason +
			` (prev=` + strconv.Itoa(len(t.previousLines)) + `, new=` + strconv.Itoa(len(newLines)) +
			`, height=` + strconv.Itoa(height) + `)\n`
		os.MkdirAll(paths.RaijinPath(), 0o755)
		f, _ := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
		if f != nil {
			f.WriteString(msg)
			f.Close()
		}
	}

	// First render
	if len(t.previousLines) == 0 && !widthChanged && !heightChanged {
		logRedraw("first render")
		fullRender(false)
		return
	}

	// Width changed
	if widthChanged {
		logRedraw(fmt.Sprintf("width changed (%d -> %d)", t.previousWidth, width))
		fullRender(true)
		return
	}

	// Height changed
	if heightChanged {
		logRedraw(fmt.Sprintf("height changed (%d -> %d)", t.previousHeight, height))
		fullRender(true)
		return
	}

	// Content shrunk
	if t.clearOnShrink && len(newLines) < t.maxLinesRendered {
		logRedraw(fmt.Sprintf("clearOnShrink (maxLinesRendered=%d)", t.maxLinesRendered))
		fullRender(true)
		return
	}

	// Find changed lines
	firstChanged, lastChanged := -1, -1
	maxLines := max(len(newLines), len(t.previousLines))
	for i := range maxLines {
		oldLine := ""
		if i < len(t.previousLines) {
			oldLine = t.previousLines[i]
		}
		newLine := ""
		if i < len(newLines) {
			newLine = newLines[i]
		}
		if oldLine != newLine {
			if firstChanged == -1 {
				firstChanged = i
			}
			lastChanged = i
		}
	}

	appendedLines := len(newLines) > len(t.previousLines)
	if appendedLines {
		if firstChanged == -1 {
			firstChanged = len(t.previousLines)
		}
		lastChanged = len(newLines) - 1
	}
	appendStart := appendedLines && firstChanged == len(t.previousLines) && firstChanged > 0

	// No changes - but still need to update hardware cursor position if it moved
	if firstChanged == -1 {
		var cp *struct{ Row, Col int }
		if hasCursor {
			cp = &cursorPos
		}
		t.positionHardwareCursor(cp, len(newLines))
		t.previousViewportTop = max(0, t.maxLinesRendered-height)
		return
	}

	// All changes in deleted lines
	if firstChanged >= len(newLines) {
		if len(t.previousLines) > len(newLines) {
			var buffer strings.Builder
			buffer.WriteString("\x1b[?2026h")
			targetRow := max(0, len(newLines)-1)
			lineDiff := computeLineDiff(targetRow)
			if lineDiff > 0 {
				buffer.WriteString(fmt.Sprintf("\x1b[%dB", lineDiff))
			} else if lineDiff < 0 {
				buffer.WriteString(fmt.Sprintf("\x1b[%dA", -lineDiff))
			}
			buffer.WriteString("\r")

			extraLines := len(t.previousLines) - len(newLines)
			if extraLines > height {
				logRedraw(fmt.Sprintf("extraLines > height (%d > %d)", extraLines, height))
				fullRender(true)
				return
			}
			if extraLines > 0 {
				buffer.WriteString("\x1b[1B")
			}
			for i := range extraLines {
				buffer.WriteString("\r\x1b[2K")
				if i < extraLines-1 {
					buffer.WriteString("\x1b[1B")
				}
			}
			if extraLines > 0 {
				buffer.WriteString(fmt.Sprintf("\x1b[%dA", extraLines))
			}
			buffer.WriteString("\x1b[?2026l")
			t.Terminal.Write(buffer.String())
			t.cursorRow = targetRow
			t.hardwareCursorRow = targetRow
		}
		if hasCursor {
			t.positionHardwareCursor(&cursorPos, len(newLines))
		}
		t.previousLines = newLines
		t.previousWidth = width
		t.previousHeight = height
		t.previousViewportTop = max(0, t.maxLinesRendered-height)
		return
	}

	// First change is above what was previously visible
	previousContentViewportTop := max(0, len(t.previousLines)-height)
	if firstChanged < previousContentViewportTop {
		logRedraw(fmt.Sprintf("firstChanged < viewportTop (%d < %d)", firstChanged, previousContentViewportTop))
		fullRender(true)
		return
	}

	// Render from first changed line to end
	var buffer strings.Builder
	buffer.WriteString("\x1b[?2026h")
	prevViewportBottom := prevViewportTop + height - 1
	moveTargetRow := firstChanged
	if appendStart {
		moveTargetRow = firstChanged - 1
	}

	if moveTargetRow > prevViewportBottom {
		currentScreenRow := max(0, min(height-1, hardwareCursorRow-prevViewportTop))
		moveToBottom := height - 1 - currentScreenRow
		if moveToBottom > 0 {
			buffer.WriteString(fmt.Sprintf("\x1b[%dB", moveToBottom))
		}
		scroll := moveTargetRow - prevViewportBottom
		buffer.WriteString(strings.Repeat("\r\n", scroll))
		prevViewportTop += scroll
		viewportTop += scroll
		hardwareCursorRow = moveTargetRow
	}

	// Move cursor to first changed line
	lineDiff := computeLineDiff(moveTargetRow)
	if lineDiff > 0 {
		buffer.WriteString(fmt.Sprintf("\x1b[%dB", lineDiff))
	} else if lineDiff < 0 {
		buffer.WriteString(fmt.Sprintf("\x1b[%dA", -lineDiff))
	}

	if appendStart {
		buffer.WriteString("\r\n")
	} else {
		buffer.WriteString("\r")
	}

	// Only render changed lines
	renderEnd := min(lastChanged, len(newLines)-1)
	for i := firstChanged; i <= renderEnd; i++ {
		if i > firstChanged {
			buffer.WriteString("\r\n")
		}
		buffer.WriteString("\x1b[2K") // Clear current line
		writeLineWithReset(&buffer, newLines[i])
	}

	finalCursorRow := renderEnd

	// Clear extra lines if content shrunk
	if len(t.previousLines) > len(newLines) {
		if renderEnd < len(newLines)-1 {
			moveDown := len(newLines) - 1 - renderEnd
			buffer.WriteString(fmt.Sprintf("\x1b[%dB", moveDown))
			finalCursorRow = len(newLines) - 1
		}
		extraLines := len(t.previousLines) - len(newLines)
		for i := len(newLines); i < len(t.previousLines); i++ {
			buffer.WriteString("\r\n\x1b[2K")
		}
		buffer.WriteString(fmt.Sprintf("\x1b[%dA", extraLines))
	}

	buffer.WriteString("\x1b[?2026l")
	t.Terminal.Write(buffer.String())

	t.cursorRow = max(0, len(newLines)-1)
	t.hardwareCursorRow = finalCursorRow
	t.maxLinesRendered = max(t.maxLinesRendered, len(newLines))
	t.previousViewportTop = max(0, t.maxLinesRendered-height)

	var cp *struct{ Row, Col int }
	if hasCursor {
		cp = &cursorPos
	}
	t.positionHardwareCursor(cp, len(newLines))

	t.previousLines = newLines
	t.previousWidth = width
	t.previousHeight = height
}

func (t *TUI) positionHardwareCursor(cursorPos *struct{ Row, Col int }, totalLines int) {
	if cursorPos == nil || totalLines <= 0 {
		t.Terminal.HideCursor()
		return
	}

	targetRow := max(0, min(cursorPos.Row, totalLines-1))
	targetCol := max(0, cursorPos.Col)

	rowDelta := targetRow - t.hardwareCursorRow
	var buffer strings.Builder
	if rowDelta > 0 {
		buffer.WriteString(fmt.Sprintf("\x1b[%dB", rowDelta))
	} else if rowDelta < 0 {
		buffer.WriteString(fmt.Sprintf("\x1b[%dA", -rowDelta))
	}
	buffer.WriteString(fmt.Sprintf("\x1b[%dG", targetCol+1))

	if buffer.Len() > 0 {
		t.Terminal.Write(buffer.String())
	}

	t.hardwareCursorRow = targetRow
	if t.showHardwareCursor {
		t.Terminal.ShowCursor()
	} else {
		t.Terminal.HideCursor()
	}
}
