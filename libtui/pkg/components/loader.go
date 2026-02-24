package components

import (
	"sync"
	"time"

	"github.com/francescoalemanno/raijin-mono/libtui/pkg/tui"
)

// UILike is the interface required by components that need to schedule work or
// trigger renders on the UI event-loop goroutine.
// In practice, *tui.TUI implements this directly.
type UILike interface {
	Dispatch(fn func())
	RequestRender(force ...bool)
}

// NewLoader creates a new Loader component.
func NewLoader(ui UILike, spinnerColorFn, messageColorFn func(string) string, message string) *Loader {
	l := &Loader{
		frames:         []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"},
		ui:             ui,
		spinnerColorFn: spinnerColorFn,
		messageColorFn: messageColorFn,
		message:        message,
		ticker:         time.NewTicker(80 * time.Millisecond),
		done:           make(chan struct{}),
	}
	l.Text = NewText("", 1, 0, nil)
	l.updateDisplay()
	return l
}

// Loader component that updates every 80ms with spinning animation.
// Extends Text for base functionality.
type Loader struct {
	*Text          // Embed Text as base
	frames         []string
	currentFrame   int
	ui             UILike
	spinnerColorFn func(string) string
	messageColorFn func(string) string
	message        string
	ticker         *time.Ticker
	done           chan struct{}
	stopOnce       sync.Once
}

// Start begins the spinner animation.
// Note: This is automatically called by NewLoader.
func (l *Loader) Start() {
	l.updateDisplay()
}

// Stop stops the spinner animation.
func (l *Loader) Stop() {
	l.stopOnce.Do(func() {
		if l.ticker != nil {
			l.ticker.Stop()
		}
		if l.done != nil {
			close(l.done)
		}
	})
}

// SetMessage updates the loader message.
func (l *Loader) SetMessage(message string) {
	l.message = message
	l.updateDisplay()
}

// HandleInput is provided for Component interface compliance (no-op for Loader).
func (l *Loader) HandleInput(data string) {
	// Loader doesn't handle input
}

// Invalidate clears the Text component's cache.
func (l *Loader) Invalidate() {
	l.Text.Invalidate()
}

// Render renders the loader with scrollback line.
func (l *Loader) Render(width int) []string {
	// Note: In TypeScript, render returns ["", ...super.render(width)]
	// to add a scrollback line. This is important for keeping content visible.
	textLines := l.Text.Render(width)
	result := make([]string, 0, len(textLines)+1)
	result = append(result, "")
	result = append(result, textLines...)
	return result
}

func (l *Loader) updateDisplay() {
	frame := l.frames[l.currentFrame]
	text := l.spinnerColorFn(frame) + " " + l.messageColorFn(l.message)
	l.Text.SetText(text)
}

// Loop runs the animation loop.
// This should be called in a goroutine after NewLoader.
func (l *Loader) Loop() {
	for {
		select {
		case <-l.ticker.C:
			tick := func() {
				l.currentFrame = (l.currentFrame + 1) % len(l.frames)
				l.updateDisplay()
			}
			if l.ui != nil {
				l.ui.Dispatch(tick)
			} else {
				tick()
			}
		case <-l.done:
			return
		}
	}
}

// Ensure Loader implements Component interface.
var _ tui.Component = (*Loader)(nil)
