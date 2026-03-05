package chat

import (
	"context"
	"testing"

	"github.com/francescoalemanno/raijin-mono/libtui/pkg/tui"
	tuitest "github.com/francescoalemanno/raijin-mono/libtui/test"
)

func newVirtualTestUI(t *testing.T) *tui.TUI {
	t.Helper()
	term := tuitest.NewVirtualTerminal(120, 40)
	ui := tui.NewTUI(term)
	t.Cleanup(ui.Stop)
	return ui
}

func runOnUI(t *testing.T, ui *tui.TUI, fn func()) {
	t.Helper()
	ok := ui.DispatchSync(context.Background(), func(tui.UIToken) {
		fn()
	})
	if !ok {
		t.Fatalf("ui dispatch failed")
	}
}
