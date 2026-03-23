package libagent

import (
	"context"
	"strings"
	"testing"

	"charm.land/fantasy"
)

func TestAdaptTools_PropagatesResponseMetadata(t *testing.T) {
	tool := NewTypedTool(
		"meta",
		"returns metadata",
		func(_ context.Context, _ struct{}, _ ToolCall) (ToolResponse, error) {
			resp := NewTextResponse("ok")
			return WithResponseMetadata(resp, map[string]string{"diff": "+ 1 | new line"}), nil
		},
	)

	adapted := AdaptTools([]Tool{tool})
	if len(adapted) != 1 {
		t.Fatalf("AdaptTools() len = %d, want 1", len(adapted))
	}

	resp, err := adapted[0].Run(context.Background(), fantasy.ToolCall{
		ID:    "tc1",
		Name:  "meta",
		Input: "{}",
	})
	if err != nil {
		t.Fatalf("adapted tool Run() error = %v", err)
	}
	if strings.TrimSpace(resp.Metadata) == "" {
		t.Fatal("expected response metadata to be propagated")
	}
	if !strings.Contains(resp.Metadata, `"diff":"+ 1 | new line"`) {
		t.Fatalf("response metadata = %q, want diff payload", resp.Metadata)
	}
}

type streamingTestTool struct{}

func (streamingTestTool) Info() ToolInfo {
	return ToolInfo{Name: "stream", Description: "streaming test tool"}
}

func (streamingTestTool) Run(context.Context, ToolCall) (ToolResponse, error) {
	return NewTextResponse("final"), nil
}

func (streamingTestTool) RunStreaming(_ context.Context, _ ToolCall, onUpdate func(ToolResponse)) (ToolResponse, error) {
	onUpdate(NewTextResponse("step 1"))
	onUpdate(NewTextResponse("step 2"))
	return NewTextResponse("final"), nil
}

func TestAdaptTools_StreamingToolPropagatesUpdates(t *testing.T) {
	adapted := AdaptTools([]Tool{streamingTestTool{}})
	if len(adapted) != 1 {
		t.Fatalf("AdaptTools() len = %d, want 1", len(adapted))
	}

	streaming, ok := adapted[0].(StreamingAgentTool)
	if !ok {
		t.Fatalf("adapted tool does not implement StreamingAgentTool")
	}

	var updates []string
	resp, err := streaming.RunStreaming(context.Background(), fantasy.ToolCall{
		ID:    "tc1",
		Name:  "stream",
		Input: "{}",
	}, func(partial fantasy.ToolResponse) {
		updates = append(updates, partial.Content)
	})
	if err != nil {
		t.Fatalf("RunStreaming() error = %v", err)
	}
	if got, want := strings.Join(updates, ","), "step 1,step 2"; got != want {
		t.Fatalf("updates = %q, want %q", got, want)
	}
	if resp.Content != "final" {
		t.Fatalf("final content = %q, want %q", resp.Content, "final")
	}
}
