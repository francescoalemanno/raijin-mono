package chat

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/francescoalemanno/raijin-mono/internal/tools"
	libagent "github.com/francescoalemanno/raijin-mono/libagent"
)

func TestStreamingRenderInterval(t *testing.T) {
	t.Parallel()

	tests := []struct {
		totalBytes int
		want       int
	}{
		{totalBytes: 0, want: 10},
		{totalBytes: 1, want: 10},
		{totalBytes: 9, want: 10},
		{totalBytes: 10, want: 10},
		{totalBytes: 99, want: 10},
		{totalBytes: 100, want: 10},
		{totalBytes: 5000, want: 500},
		{totalBytes: 6399, want: 639},
		{totalBytes: 6400, want: 640},
		{totalBytes: 100000, want: 640},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(fmt.Sprintf("total_%d", tt.totalBytes), func(t *testing.T) {
			t.Parallel()
			if got := streamingRenderInterval(tt.totalBytes); got != tt.want {
				t.Fatalf("streamingRenderInterval(%d) = %d, want %d", tt.totalBytes, got, tt.want)
			}
		})
	}
}

func TestToolExecutionAppendInputDelta_UsesMessageTenthIntervals(t *testing.T) {
	t.Parallel()

	baseTool := libagent.NewTypedTool("dummy", "test", func(ctx context.Context, input map[string]any, call libagent.ToolCall) (libagent.ToolResponse, error) {
		return libagent.NewTextResponse("ok"), nil
	})
	tool := tools.WithRender(baseTool, func(input json.RawMessage, output string, _ int) string {
		var payload struct {
			X string `json:"x"`
		}
		_ = json.Unmarshal(input, &payload)
		return "dummy\n" + payload.X
	})

	comp := NewToolExecution("dummy", nil, tool, noopUI{})
	now := time.Unix(0, 0)
	comp.streamingRenderGate.now = func() time.Time { return now }

	comp.AppendInputDelta(`{"x":"`)
	initial := strings.Join(comp.Render(1000), "\n")
	if strings.Contains(initial, "AAAAA") {
		t.Fatalf("unexpected content before data deltas are checkpointed")
	}

	comp.AppendInputDelta(strings.Repeat("A", 1000))
	firstStillLimited := strings.Join(comp.Render(1000), "\n")
	if strings.Contains(firstStillLimited, "AAAAA") {
		t.Fatalf("content rendered before timing checkpoint on large payload")
	}

	now = now.Add(toolStreamingRenderMinInterval)
	comp.AppendInputDelta("A")
	firstRender := strings.Join(comp.Render(1000), "\n")
	if !strings.Contains(firstRender, "AAAAA") {
		t.Fatalf("content did not render after large first payload and timing checkpoint")
	}

	comp.AppendInputDelta(strings.Repeat("B", 50))
	beforeThreshold := strings.Join(comp.Render(1000), "\n")
	if strings.Contains(beforeThreshold, "BBBBB") {
		t.Fatalf("content rendered before crossing len(message)/10 threshold")
	}

	comp.AppendInputDelta(strings.Repeat("B", 61))
	stillRateLimited := strings.Join(comp.Render(1000), "\n")
	if strings.Contains(stillRateLimited, "BBBBB") {
		t.Fatalf("content rendered before timing checkpoint (10 updates/s)")
	}

	now = now.Add(toolStreamingRenderMinInterval)
	comp.AppendInputDelta("B")
	atThreshold := strings.Join(comp.Render(1000), "\n")
	if !strings.Contains(atThreshold, "BBBBB") {
		t.Fatalf("content did not render after crossing byte and timing checkpoints")
	}
}

func TestToolExecutionAppendInputDelta_RateLimitedToTenHz(t *testing.T) {
	t.Parallel()

	baseTool := libagent.NewTypedTool("dummy", "test", func(ctx context.Context, input map[string]any, call libagent.ToolCall) (libagent.ToolResponse, error) {
		return libagent.NewTextResponse("ok"), nil
	})
	tool := tools.WithRender(baseTool, func(input json.RawMessage, output string, _ int) string {
		var payload struct {
			X string `json:"x"`
		}
		_ = json.Unmarshal(input, &payload)
		return "dummy\n" + payload.X
	})

	comp := NewToolExecution("dummy", nil, tool, noopUI{})
	now := time.Unix(0, 0)
	comp.streamingRenderGate.now = func() time.Time { return now }

	comp.AppendInputDelta(`{"x":"` + strings.Repeat("A", 1000))
	first := strings.Join(comp.Render(1000), "\n")
	if !strings.Contains(first, "AAAAA") {
		t.Fatalf("expected first streaming render")
	}

	comp.AppendInputDelta(strings.Repeat("B", 1000))
	second := strings.Join(comp.Render(1000), "\n")
	if strings.Contains(second, "BBBBB") {
		t.Fatalf("expected render to be rate-limited before 100ms")
	}

	now = now.Add(toolStreamingRenderMinInterval)
	comp.AppendInputDelta("B")
	third := strings.Join(comp.Render(1000), "\n")
	if !strings.Contains(third, "BBBBB") {
		t.Fatalf("expected render after 100ms interval")
	}
}
