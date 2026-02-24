package chat

import (
	"testing"

	bridgecfg "github.com/francescoalemanno/raijin-mono/llmbridge/pkg/config"

	"github.com/francescoalemanno/raijin-mono/internal/core"
	"github.com/francescoalemanno/raijin-mono/libtui/pkg/tui"
)

func TestRunPromptWithOptions_QueuesSteeringWithoutImmediateToolCancel(t *testing.T) {
	t.Parallel()

	app := newChatApp(&chatNoopTerminal{}, nil, &bridgecfg.Config{}, nil)

	app.dispatchSync(func(_ tui.UIToken) {
		app.state = stateRunning
		comp := NewToolExecution("bash", nil, nil, app.ui)
		app.pendingTools["tool-1"] = comp
	})

	app.runPromptWithOptions("please do this instead", promptRunOptions{})

	var (
		state    chatState
		queueLen int
		pending  bool
	)
	app.dispatchSync(func(_ tui.UIToken) {
		state = app.state
		queueLen = app.steeringQueue.Len()
		comp, ok := app.pendingTools["tool-1"]
		if ok {
			pending = comp.IsPending()
		}
	})

	if state != stateRunning {
		t.Fatalf("state = %v, want running", state)
	}
	if queueLen != 1 {
		t.Fatalf("steering queue len = %d, want 1", queueLen)
	}
	if !pending {
		t.Fatalf("expected pending tool to remain pending (not cancelled immediately)")
	}
}

func TestHandleEventToolResult_WithQueuedSteeringIssuesInterrupt(t *testing.T) {
	t.Parallel()

	app := newChatApp(&chatNoopTerminal{}, nil, &bridgecfg.Config{}, nil)

	app.dispatchSync(func(_ tui.UIToken) {
		app.state = stateRunning
		comp := NewToolExecution("bash", nil, nil, app.ui)
		app.pendingTools["tool-1"] = comp
		app.steeringQueue = newPromptDeliveryQueue()
		app.steeringQueue.Enqueue(queuedPrompt{Input: "next", Opts: promptRunOptions{}})
		app.steeringInterruptIssued = false
	})

	app.handleEvent(core.AgentEvent{Kind: core.EventToolResult, ID: "tool-1", Output: "done"})

	var (
		toolStillPending        bool
		steeringInterruptIssued bool
	)
	app.dispatchSync(func(_ tui.UIToken) {
		_, toolStillPending = app.pendingTools["tool-1"]
		steeringInterruptIssued = app.steeringInterruptIssued
	})

	if toolStillPending {
		t.Fatalf("expected tool to be removed from pending map")
	}
	if !steeringInterruptIssued {
		t.Fatalf("expected steering interrupt to be issued after current tool finished")
	}
}
