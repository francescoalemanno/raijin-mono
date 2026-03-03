package tui

import (
	"testing"
	"time"
)

func TestDynamicGateDelay(t *testing.T) {
	t.Parallel()

	if got := dynamicGateDelay(0); got != 0 {
		t.Fatalf("dynamicGateDelay(0) = %v, want 0", got)
	}
	if got := dynamicGateDelay(100 * time.Millisecond); got != 150*time.Millisecond {
		t.Fatalf("dynamicGateDelay(100ms) = %v, want 150ms", got)
	}
}

func TestShouldRenderNow_UsesDynamicDelayAndForceBypass(t *testing.T) {
	t.Parallel()

	ui := &TUI{}
	ui.dynamicRenderDelayNS.Store(int64(100 * time.Millisecond))
	ui.lastRenderTime = time.Now().Add(-50 * time.Millisecond)

	ok, delay := ui.shouldRenderNow(false)
	if ok {
		t.Fatalf("expected non-forced render to be throttled")
	}
	if delay <= 0 || delay >= 100*time.Millisecond {
		t.Fatalf("unexpected delay: %v", delay)
	}

	ok, delay = ui.shouldRenderNow(true)
	if !ok {
		t.Fatalf("expected forced render to bypass throttle, delay=%v", delay)
	}
}
