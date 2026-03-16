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
