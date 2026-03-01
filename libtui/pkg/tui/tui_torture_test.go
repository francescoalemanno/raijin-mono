package tui

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type tortureTerminal struct {
	mu       sync.RWMutex
	cols     int
	rows     int
	onInput  func(string)
	onResize func()
}

func newTortureTerminal(cols, rows int) *tortureTerminal {
	return &tortureTerminal{cols: cols, rows: rows}
}

func (t *tortureTerminal) Start(onInput func(string), onResize func()) {
	t.mu.Lock()
	t.onInput = onInput
	t.onResize = onResize
	t.mu.Unlock()
}

func (t *tortureTerminal) Stop() {}

func (t *tortureTerminal) DrainInput(maxMs, idleMs int) error { return nil }

func (t *tortureTerminal) Write(data string) {}

func (t *tortureTerminal) Columns() int {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.cols
}

func (t *tortureTerminal) Rows() int {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.rows
}

func (t *tortureTerminal) KittyProtocolActive() bool { return false }

func (t *tortureTerminal) MoveBy(lines int)      {}
func (t *tortureTerminal) HideCursor()           {}
func (t *tortureTerminal) ShowCursor()           {}
func (t *tortureTerminal) ClearLine()            {}
func (t *tortureTerminal) ClearFromCursor()      {}
func (t *tortureTerminal) ClearScreen()          {}
func (t *tortureTerminal) SetTitle(title string) {}

func (t *tortureTerminal) emitInput(data string) {
	t.mu.RLock()
	cb := t.onInput
	t.mu.RUnlock()
	if cb != nil {
		cb(data)
	}
}

func (t *tortureTerminal) emitResize(cols, rows int) {
	t.mu.Lock()
	t.cols = cols
	t.rows = rows
	cb := t.onResize
	t.mu.Unlock()
	if cb != nil {
		cb()
	}
}

type tortureComponent struct {
	inputs atomic.Int64
}

func (c *tortureComponent) Render(width int) []string { return []string{"torture"} }
func (c *tortureComponent) HandleInput(data string)   { c.inputs.Add(1) }
func (c *tortureComponent) Invalidate()               {}

func TestTUI_Torture_InputResizeRenderStorm_NoDeadlock(t *testing.T) {
	t.Parallel()

	term := newTortureTerminal(120, 40)
	ui := NewTUI(term)
	comp := &tortureComponent{}
	ui.AddChild(comp)
	ui.SetFocus(comp)
	ui.Start()
	defer ui.Stop()

	const n = 4000
	var wg sync.WaitGroup
	wg.Add(3)

	go func() {
		defer wg.Done()
		for i := 0; i < n; i++ {
			term.emitInput("a")
		}
	}()

	go func() {
		defer wg.Done()
		for i := 0; i < n; i++ {
			cols := 100 + (i % 40)
			rows := 30 + (i % 20)
			term.emitResize(cols, rows)
		}
	}()

	go func() {
		defer wg.Done()
		for i := 0; i < n; i++ {
			ui.RequestRender(i%13 == 0)
		}
	}()

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(6 * time.Second):
		t.Fatal("storm producers blocked (possible UI queue lockup)")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if !ui.DispatchSync(ctx, func(UIToken) {}) {
		t.Fatal("UI event loop not responsive after storm")
	}

	if comp.inputs.Load() == 0 {
		t.Fatal("expected input handler to receive events")
	}
}

func TestTUI_Torture_SustainedMixedLoad_RemainsResponsive(t *testing.T) {
	t.Parallel()

	term := newTortureTerminal(120, 40)
	ui := NewTUI(term)
	comp := &tortureComponent{}
	ui.AddChild(comp)
	ui.SetFocus(comp)
	ui.Start()
	defer ui.Stop()

	stop := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(3)

	go func() {
		defer wg.Done()
		payloads := []string{"paste-a", "paste-bb", "paste-ccc", "paste-dddd", "paste-eeeeee"}
		i := 0
		for {
			select {
			case <-stop:
				return
			default:
				term.emitInput(payloads[i%len(payloads)])
				i++
			}
		}
	}()

	go func() {
		defer wg.Done()
		i := 0
		for {
			select {
			case <-stop:
				return
			default:
				term.emitResize(90+(i%60), 24+(i%20))
				i++
			}
		}
	}()

	go func() {
		defer wg.Done()
		i := 0
		for {
			select {
			case <-stop:
				return
			default:
				ui.RequestRender(i%7 == 0)
				i++
			}
		}
	}()

	deadline := time.Now().Add(1500 * time.Millisecond)
	successes := 0
	for time.Now().Before(deadline) {
		ctx, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
		ok := ui.DispatchSync(ctx, func(UIToken) {})
		cancel()
		if ok {
			successes++
		}
		time.Sleep(20 * time.Millisecond)
	}

	close(stop)
	wg.Wait()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if !ui.DispatchSync(ctx, func(UIToken) {}) {
		t.Fatal("UI event loop failed to recover after sustained load")
	}

	if successes < 4 {
		t.Fatalf("expected repeated responsiveness during load, got %d", successes)
	}
}

func TestTUI_Torture_FocusedInputNotStarvedByDrainAndResizeStorm(t *testing.T) {
	t.Parallel()

	term := newTortureTerminal(120, 40)
	ui := NewTUI(term)
	comp := &tortureComponent{}
	ui.AddChild(comp)
	ui.SetFocus(comp)
	ui.Start()
	defer ui.Stop()

	stop := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(3)

	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
				// Keep enqueuing many input chunks.
				term.emitInput("chunk")
			}
		}
	}()

	go func() {
		defer wg.Done()
		i := 0
		for {
			select {
			case <-stop:
				return
			default:
				term.emitResize(80+(i%80), 20+(i%20))
				i++
			}
		}
	}()

	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
				ui.RequestRender()
			}
		}
	}()

	start := comp.inputs.Load()
	deadline := time.Now().Add(1400 * time.Millisecond)
	for time.Now().Before(deadline) {
		time.Sleep(40 * time.Millisecond)
		if comp.inputs.Load() > start+25 {
			close(stop)
			wg.Wait()
			return
		}
	}

	close(stop)
	wg.Wait()
	t.Fatalf("focused component input handler appeared starved; inputs=%d start=%d", comp.inputs.Load(), start)
}

func TestTUI_HeightChangeTriggersFullRedraw(t *testing.T) {
	t.Parallel()

	term := newTortureTerminal(100, 30)
	ui := NewTUI(term)
	ui.AddChild(&tortureComponent{})
	ui.Start()
	defer ui.Stop()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if !ui.DispatchSync(ctx, func(UIToken) {}) {
		t.Fatal("UI did not complete initial render")
	}

	initialRedraws := ui.FullRedraws()
	term.emitResize(100, 35)

	if !ui.DispatchSync(ctx, func(UIToken) {}) {
		t.Fatal("UI did not process resize render")
	}

	if got := ui.FullRedraws(); got <= initialRedraws {
		t.Fatalf("expected full redraw count to increase on height change; before=%d after=%d", initialRedraws, got)
	}
}
