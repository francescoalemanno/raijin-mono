package chat

import (
	"testing"
	"time"
)

func TestRenderGateInterval(t *testing.T) {
	t.Parallel()

	g := newRenderGate(10, 640, 10, 100*time.Millisecond)
	tests := []struct {
		totalBytes int
		want       int
	}{
		{totalBytes: 0, want: 10},
		{totalBytes: 9, want: 10},
		{totalBytes: 100, want: 10},
		{totalBytes: 5000, want: 500},
		{totalBytes: 100000, want: 640},
	}

	for _, tt := range tests {
		if got := g.interval(tt.totalBytes); got != tt.want {
			t.Fatalf("interval(%d) = %d, want %d", tt.totalBytes, got, tt.want)
		}
	}
}

func TestRenderGateShouldRender_ForceAndRateLimit(t *testing.T) {
	t.Parallel()

	g := newRenderGate(10, 640, 10, 100*time.Millisecond)
	now := time.Unix(0, 0)
	g.now = func() time.Time { return now }

	if !g.shouldRender(1, true) {
		t.Fatalf("expected first force render to pass")
	}
	if g.shouldRender(1000, false) {
		t.Fatalf("expected second render to be rate-limited")
	}

	now = now.Add(100 * time.Millisecond)
	if !g.shouldRender(1000, false) {
		t.Fatalf("expected render after min interval")
	}
}
