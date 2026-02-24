package chat

import (
	"testing"

	"github.com/francescoalemanno/raijin-mono/libtui/pkg/terminal"
	"github.com/francescoalemanno/raijin-mono/libtui/pkg/tui"
)

type chatNoopTerminal struct{}

func (t *chatNoopTerminal) Start(onInput func(string), onResize func()) {}
func (t *chatNoopTerminal) Stop()                                       {}
func (t *chatNoopTerminal) DrainInput(maxMs, idleMs int) error          { return nil }
func (t *chatNoopTerminal) Write(data string)                           {}
func (t *chatNoopTerminal) Columns() int                                { return 120 }
func (t *chatNoopTerminal) Rows() int                                   { return 40 }
func (t *chatNoopTerminal) KittyProtocolActive() bool                   { return false }
func (t *chatNoopTerminal) MoveBy(lines int)                            {}
func (t *chatNoopTerminal) HideCursor()                                 {}
func (t *chatNoopTerminal) ShowCursor()                                 {}
func (t *chatNoopTerminal) ClearLine()                                  {}
func (t *chatNoopTerminal) ClearFromCursor()                            {}
func (t *chatNoopTerminal) ClearScreen()                                {}
func (t *chatNoopTerminal) SetTitle(title string)                       {}

var _ terminal.Terminal = (*chatNoopTerminal)(nil)

type expandableTestComponent struct{}

func (c *expandableTestComponent) Render(width int) []string { return []string{""} }
func (c *expandableTestComponent) HandleInput(data string)   {}
func (c *expandableTestComponent) Invalidate()               {}
func (c *expandableTestComponent) SetExpanded(expanded bool) {}

var _ tui.Component = (*expandableTestComponent)(nil)

func TestChatApp_ToggleBlocksExpandedReturnsCopy(t *testing.T) {
	c1 := &expandableTestComponent{}
	c2 := &expandableTestComponent{}
	app := &ChatApp{
		ui: tui.NewTUI(&chatNoopTerminal{}),
		items: []historyEntry{
			{component: c1},
			{component: c2},
		},
	}

	// toggleBlocksExpanded operates on a copy of items, so mutating the snapshot
	// (items slice copy) should not affect the original.
	originalItems := app.items
	app.toggleBlocksExpanded()

	if len(app.items) != 2 {
		t.Fatalf("expected items length 2, got %d", len(app.items))
	}
	got, ok := originalItems[0].component.(*expandableTestComponent)
	if !ok || got != c1 {
		t.Fatalf("expected original history entry to remain unchanged")
	}
}
