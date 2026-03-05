package test

import (
	"context"
	"fmt"
	"math/rand"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/francescoalemanno/raijin-mono/libtui/pkg/tui"
	"github.com/francescoalemanno/raijin-mono/libtui/pkg/utils"
)

type tortureStateComponent struct {
	lines      []string
	rendered   atomic.Int64
	inputCount atomic.Int64
}

func (c *tortureStateComponent) Render(_ int) []string {
	c.rendered.Add(1)
	out := make([]string, len(c.lines))
	copy(out, c.lines)
	return out
}

func (c *tortureStateComponent) HandleInput(_ string) {
	c.inputCount.Add(1)
}

func (c *tortureStateComponent) Invalidate() {}

func TestTUI_Torture_VirtualTerminal_MixedStormStaysResponsive(t *testing.T) {
	t.Parallel()

	term := NewVirtualTerminal(80, 20)
	ui := tui.NewTUI(term)
	comp := &tortureStateComponent{lines: []string{"boot"}}
	ui.AddChild(comp)
	ui.SetFocus(comp)
	ui.Start()
	defer ui.Stop()

	stop := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(4)

	go func() {
		defer wg.Done()
		for i := 0; ; i++ {
			select {
			case <-stop:
				return
			default:
				term.SimulateInput("x")
				if i%17 == 0 {
					term.SimulateInput("\x1b")
				}
			}
		}
	}()

	go func() {
		defer wg.Done()
		for i := 0; ; i++ {
			select {
			case <-stop:
				return
			default:
				term.Resize(70+(i%40), 14+(i%10))
			}
		}
	}()

	go func() {
		defer wg.Done()
		for i := 0; ; i++ {
			select {
			case <-stop:
				return
			default:
				idx := i
				ui.Dispatch(func() {
					comp.lines = []string{
						fmt.Sprintf("storm-%d", idx),
						strings.Repeat(".", 10+(idx%20)),
					}
				})
			}
		}
	}()

	go func() {
		defer wg.Done()
		for i := 0; ; i++ {
			select {
			case <-stop:
				return
			default:
				ui.RequestRender(i%9 == 0)
			}
		}
	}()

	deadline := time.Now().Add(1500 * time.Millisecond)
	responsiveChecks := 0
	for time.Now().Before(deadline) {
		ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
		ok := ui.DispatchSync(ctx, func(tui.UIToken) {})
		cancel()
		if ok {
			responsiveChecks++
		}
		time.Sleep(20 * time.Millisecond)
	}

	close(stop)
	wg.Wait()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	require.True(t, ui.DispatchSync(ctx, func(tui.UIToken) {}), "UI did not recover after mixed storm")
	require.Greater(t, responsiveChecks, 5, "expected repeated responsiveness checks")
	require.Greater(t, comp.inputCount.Load(), int64(100), "focused input handler was starved")
	require.Greater(t, comp.rendered.Load(), int64(10), "renderer was not exercised")
}

func TestTUI_Torture_VirtualTerminal_RendersAndTracksCursor(t *testing.T) {
	term := NewVirtualTerminal(40, 8)
	ui := tui.NewTUI(term, true)
	comp := &tortureStateComponent{
		lines: []string{
			"header",
			"ab" + tui.CURSOR_MARKER + "cd",
			"tail",
		},
	}
	ui.AddChild(comp)
	ui.SetFocus(comp)
	ui.Start()
	defer ui.Stop()

	require.Eventually(t, func() bool {
		line0 := term.GetLine(0)
		line1 := term.GetLine(1)
		line2 := term.GetLine(2)
		return line0 == "header" && line1 == "abcd" && line2 == "tail"
	}, time.Second, 10*time.Millisecond)

	// Force one final full render and wait until it is processed so cursor
	// coordinates are read from a deterministic end-of-frame state.
	ui.RequestRender(true)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	require.True(t, ui.DispatchSync(ctx, func(tui.UIToken) {}))

	row, col := term.GetCursor()
	assert.Equal(t, 1, row)
	assert.Equal(t, 2, col)
	assert.False(t, term.IsHidden(), "hardware cursor should be visible")
}

func TestTUI_Torture_VirtualTerminal_RenderConvergence(t *testing.T) {
	t.Parallel()

	term := NewVirtualTerminal(48, 16)
	ui := tui.NewTUI(term)
	comp := &tortureStateComponent{}
	ui.AddChild(comp)
	ui.SetFocus(comp)
	ui.Start()
	defer ui.Stop()

	rng := rand.New(rand.NewSource(42))

	for i := 0; i < 180; i++ {
		n := 1 + rng.Intn(8)
		next := make([]string, 0, n)
		for r := 0; r < n; r++ {
			base := fmt.Sprintf("r%03d-c%02d", i, r)
			padding := strings.Repeat("x", rng.Intn(18))
			line := base + padding
			if r%3 == 0 {
				line = "\x1b[32m" + line + "\x1b[0m"
			}
			line = utils.TruncateToWidth(line, 40)
			if r == n-1 && i%7 == 0 {
				line += tui.CURSOR_MARKER
			}
			next = append(next, line)
		}

		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		ok := ui.DispatchSync(ctx, func(tui.UIToken) {
			comp.lines = next
			ui.RequestRender(i%11 == 0)
		})
		cancel()
		require.True(t, ok)

		require.Eventually(t, func() bool {
			for row := 0; row < len(next); row++ {
				expected := strings.ReplaceAll(next[row], tui.CURSOR_MARKER, "")
				expected = strings.TrimRight(StripAnsi(expected), " ")
				if term.GetLine(row) != expected {
					return false
				}
			}
			return true
		}, time.Second, 10*time.Millisecond)
	}
}
