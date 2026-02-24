package tools

import (
	"context"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/francescoalemanno/raijin-mono/llmbridge/pkg/llm"
)

func TestBashToolCancelStopsBackgroundChildrenQuickly(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("uses unix sleep/wait semantics")
	}

	tool := NewBashTool(nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	type result struct {
		resp llm.ToolResponse
		err  error
	}
	resultCh := make(chan result, 1)
	start := time.Now()

	go func() {
		resp, err := tool.Run(ctx, llm.ToolCall{Input: `{"command":"sleep 4 & wait"}`})
		resultCh <- result{resp: resp, err: err}
	}()

	time.Sleep(120 * time.Millisecond)
	cancel()

	select {
	case got := <-resultCh:
		if got.err != nil {
			t.Fatalf("unexpected error: %v", got.err)
		}
		if !got.resp.IsError {
			t.Fatalf("expected tool error response after cancellation")
		}
		if !strings.Contains(got.resp.Content, "Command aborted") {
			t.Fatalf("expected cancellation message, got: %q", got.resp.Content)
		}
		if elapsed := time.Since(start); elapsed > 2*time.Second {
			t.Fatalf("cancellation took too long: %v", elapsed)
		}
	case <-time.After(3 * time.Second):
		t.Fatalf("tool did not return promptly after cancellation")
	}
}
