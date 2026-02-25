package tui

import (
	"context"
	"testing"
	"time"
)

func TestRequestRender_DoesNotBlockWhenQueueFull(t *testing.T) {
	t.Parallel()

	tuiInstance := &TUI{
		tasks:  make(chan uiTask, 1),
		doneCh: make(chan struct{}),
	}
	// Fill queue.
	tuiInstance.tasks <- uiTask{}

	done := make(chan struct{})
	go func() {
		tuiInstance.RequestRender()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("RequestRender blocked with full queue")
	}
}

func TestDispatch_DoesNotBlockWhenQueueFull(t *testing.T) {
	t.Parallel()

	tuiInstance := &TUI{
		tasks:  make(chan uiTask, 1),
		doneCh: make(chan struct{}),
	}
	// Fill queue so direct send would block.
	tuiInstance.tasks <- uiTask{}

	done := make(chan struct{})
	go func() {
		tuiInstance.Dispatch(func() {})
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("Dispatch blocked with full queue")
	}
}

func TestDispatchSync_RespectsContextWhenQueueFull(t *testing.T) {
	t.Parallel()

	tuiInstance := &TUI{
		tasks:  make(chan uiTask, 1),
		doneCh: make(chan struct{}),
	}
	// Fill queue so enqueue cannot proceed.
	tuiInstance.tasks <- uiTask{}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()

	ok := tuiInstance.DispatchSync(ctx, func(UIToken) {})
	if ok {
		t.Fatal("DispatchSync returned true, want false on context timeout")
	}
}
