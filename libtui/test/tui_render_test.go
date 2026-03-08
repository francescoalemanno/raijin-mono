package test

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/francescoalemanno/raijin-mono/libtui/pkg/components"
	"github.com/francescoalemanno/raijin-mono/libtui/pkg/tui"
	"github.com/francescoalemanno/raijin-mono/libtui/pkg/utils"
)

// TestComponent is a simple component for testing.
type TestComponent struct {
	lines []string
}

func (c *TestComponent) Render(_ int) []string {
	return c.lines
}

func (c *TestComponent) HandleInput(_ string) {}

func (c *TestComponent) Invalidate() {}

func TestTUI_Create(t *testing.T) {
	terminal := NewVirtualTerminal(40, 10)
	tuiInstance := tui.NewTUI(terminal)
	assert.NotNil(t, tuiInstance)
}

func TestContainer_Render_SkipsTypedNilChild(t *testing.T) {
	var nilText *components.Text
	container := &tui.Container{}
	container.AddChild(nilText)
	container.AddChild(&TestComponent{lines: []string{"ok"}})

	assert.NotPanics(t, func() {
		lines := container.Render(40)
		assert.Equal(t, []string{"ok"}, lines)
	})
}

// TestContainer_SequentialMutationAndRender_NoPanic verifies that Container
// handles typed-nil children, AddChild/RemoveChild/Clear, and Invalidate
// correctly when called from a single goroutine (the documented contract).
func TestContainer_SequentialMutationAndRender_NoPanic(t *testing.T) {
	container := &tui.Container{}

	assert.NotPanics(t, func() {
		for i := range 3000 {
			text := components.NewText(fmt.Sprintf("item-%d", i), 0, 0, nil)
			container.AddChild(text)

			if i%2 == 0 {
				container.RemoveChild(text)
			}
			if i%17 == 0 {
				container.Clear()
			}
			if i%7 == 0 {
				var nilText *components.Text
				container.AddChild(nilText)
			}

			container.Invalidate()
			_ = container.Render(80)
		}
	})
}

func TestTUI_ClipsRenderedLinesToTerminalWidth(t *testing.T) {
	term := NewVirtualTerminal(10, 4)
	ui := tui.NewTUI(term)
	ui.AddChild(&TestComponent{lines: []string{"01234567890123456789", "short"}})
	ui.Start()
	defer ui.Stop()

	assert.Eventually(t, func() bool {
		return term.GetLine(0) != ""
	}, time.Second, 10*time.Millisecond)

	viewport := term.GetViewport()
	if len(viewport) == 0 {
		t.Fatalf("expected viewport lines")
	}

	for _, line := range viewport {
		if strings.TrimSpace(line) == "" {
			continue
		}
		assert.LessOrEqual(t, utils.VisibleWidth(line), 10, "visible width should be clipped to terminal width")
	}
	assert.Equal(t, "0123456789", term.GetLine(0))
}
