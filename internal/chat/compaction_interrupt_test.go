package chat

import (
	"testing"

	bridgecfg "github.com/francescoalemanno/raijin-mono/llmbridge/pkg/config"

	"github.com/francescoalemanno/raijin-mono/libtui/pkg/tui"
)

func TestInterruptRun_NoOpWhileCompacting(t *testing.T) {
	t.Parallel()

	app := newChatApp(&chatNoopTerminal{}, nil, &bridgecfg.Config{}, nil)

	var beforeItems int
	app.dispatchSync(func(_ tui.UIToken) {
		beforeItems = len(app.items)
		app.state = stateRunning
		app.compacting = true
		app.steeringQueue = newPromptDeliveryQueue()
		app.steeringQueue.Enqueue(queuedPrompt{Input: "next", Opts: promptRunOptions{}})
		comp := NewToolExecution("bash", nil, nil, app.ui)
		app.pendingTools["tool-1"] = comp
	})

	app.dispatchSync(func(_ tui.UIToken) {
		app.interruptRun()
	})

	app.dispatchSync(func(_ tui.UIToken) {
		if app.steeringQueue.Len() != 1 {
			t.Fatalf("steering queue len = %d, want 1", app.steeringQueue.Len())
		}
		comp, ok := app.pendingTools["tool-1"]
		if !ok {
			t.Fatalf("expected pending tool to remain pending")
		}
		if !comp.IsPending() {
			t.Fatalf("expected pending tool to remain pending")
		}
		if len(app.items) != beforeItems {
			t.Fatalf("history items changed during compaction interrupt: got %d, want %d", len(app.items), beforeItems)
		}
	})
}

func TestGlobalKeyListener_CtrlCConsumedWhileCompacting(t *testing.T) {
	t.Parallel()

	app := newChatApp(&chatNoopTerminal{}, nil, &bridgecfg.Config{}, nil)

	var res *tui.InputListenerResult
	app.dispatchSync(func(_ tui.UIToken) {
		app.compacting = true
		app.state = stateRunning
		res = app.globalKeyListener("\x03") // ctrl+c
	})

	if res == nil || !res.Consume {
		t.Fatalf("expected ctrl+c to be consumed while compacting")
	}
}

func TestGlobalKeyListener_CtrlDConsumedWhileCompacting(t *testing.T) {
	t.Parallel()

	app := newChatApp(&chatNoopTerminal{}, nil, &bridgecfg.Config{}, nil)

	var (
		res      *tui.InputListenerResult
		doneOpen bool
	)
	app.dispatchSync(func(_ tui.UIToken) {
		app.compacting = true
		res = app.globalKeyListener("\x04") // ctrl+d
		select {
		case <-app.done:
			doneOpen = false
		default:
			doneOpen = true
		}
	})

	if res == nil || !res.Consume {
		t.Fatalf("expected ctrl+d to be consumed while compacting")
	}
	if !doneOpen {
		t.Fatalf("expected app not to quit while compacting")
	}
}
