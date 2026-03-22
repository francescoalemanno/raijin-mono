package tools

import (
	"context"
	"runtime"
	"strings"
	"testing"
	"time"

	libagent "github.com/francescoalemanno/raijin-mono/libagent"
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
		resp libagent.ToolResponse
		err  error
	}
	resultCh := make(chan result, 1)
	start := time.Now()

	go func() {
		resp, err := tool.Run(ctx, libagent.ToolCall{Input: `{"command":"sleep 4 & wait"}`})
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

func TestBashToolIgnoresUserShellEnv(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("bash tool uses cmd on windows")
	}

	t.Setenv("SHELL", "/bin/zsh")

	tool := NewBashTool(nil)
	resp, err := tool.Run(context.Background(), libagent.ToolCall{Input: `{"command":"printf '%s' \"$BASH_VERSION\""}`})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.IsError {
		t.Fatalf("expected success response, got error: %s", resp.Content)
	}
	if strings.TrimSpace(resp.Content) == "" {
		t.Fatalf("expected BASH_VERSION output, got %q", resp.Content)
	}
}
