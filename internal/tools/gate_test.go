package tools

import (
	"context"
	"fmt"
	"testing"

	"github.com/francescoalemanno/raijin-mono/llmbridge/pkg/llm"
)

func TestToolExecutionGate_DisablesNonAllowedTool(t *testing.T) {
	t.Parallel()

	tool := NewGlobTool()
	ctx := WithAllowedTools(context.Background(), []string{"read"})
	resp, err := tool.Run(ctx, llm.ToolCall{Input: `{"pattern":"*.go"}`})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !resp.IsError {
		t.Fatalf("expected error response")
	}
	want := fmt.Sprintf(toolTemporarilyDisabledMsg, "glob")
	if resp.Content != want {
		t.Fatalf("response = %q, want %q", resp.Content, want)
	}
}

func TestToolExecutionGate_AllowsListedTool(t *testing.T) {
	t.Parallel()

	tool := NewReadTool()
	ctx := WithAllowedTools(context.Background(), []string{"read"})
	resp, err := tool.Run(ctx, llm.ToolCall{Input: `{}`})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !resp.IsError {
		t.Fatalf("expected path validation error response")
	}
	disabled := fmt.Sprintf(toolTemporarilyDisabledMsg, "read")
	if resp.Content == disabled {
		t.Fatalf("read should not be disabled when allowlisted")
	}
}
