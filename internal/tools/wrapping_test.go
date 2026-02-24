package tools

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/francescoalemanno/raijin-mono/llmbridge/pkg/llm"
)

type testInput struct {
	Value string `json:"value"`
}

func TestWithRenderConvertsUnexpectedToolErrorToTextErrorResponse(t *testing.T) {
	t.Parallel()

	tool := WithRender(
		llm.NewAgentTool("boom", "test", func(ctx context.Context, input testInput, call llm.ToolCall) (llm.ToolResponse, error) {
			return llm.ToolResponse{}, errors.New("kaboom")
		}),
		nil,
	)

	resp, err := tool.Run(context.Background(), llm.ToolCall{Input: `{"value":"x"}`})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !resp.IsError {
		t.Fatalf("expected error response")
	}
	if !strings.Contains(resp.Content, `tool "boom" failed: kaboom`) {
		t.Fatalf("expected informative failure text, got: %q", resp.Content)
	}
	if !strings.Contains(resp.Content, `"value":"x"`) {
		t.Fatalf("expected input preview in response, got: %q", resp.Content)
	}
}

func TestWithRenderConvertsPanicToTextErrorResponse(t *testing.T) {
	t.Parallel()

	tool := WithRender(
		llm.NewAgentTool("panic_tool", "test", func(ctx context.Context, input testInput, call llm.ToolCall) (llm.ToolResponse, error) {
			panic("boom")
		}),
		nil,
	)

	resp, err := tool.Run(context.Background(), llm.ToolCall{Input: `{"value":"x"}`})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !resp.IsError {
		t.Fatalf("expected error response")
	}
	if !strings.Contains(resp.Content, `tool "panic_tool" panicked`) {
		t.Fatalf("expected panic text, got: %q", resp.Content)
	}
}

func TestWithRenderPropagatesContextCancellation(t *testing.T) {
	t.Parallel()

	tool := WithRender(
		llm.NewAgentTool("cancel_tool", "test", func(ctx context.Context, input testInput, call llm.ToolCall) (llm.ToolResponse, error) {
			return llm.ToolResponse{}, ctx.Err()
		}),
		nil,
	)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := tool.Run(ctx, llm.ToolCall{Input: `{"value":"x"}`})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context canceled, got: %v", err)
	}
}

func TestWithRenderConvertsEmptyResponseToInformativeError(t *testing.T) {
	t.Parallel()

	tool := WithRender(
		llm.NewAgentTool("empty_tool", "test", func(ctx context.Context, input testInput, call llm.ToolCall) (llm.ToolResponse, error) {
			return llm.ToolResponse{}, nil
		}),
		nil,
	)

	resp, err := tool.Run(context.Background(), llm.ToolCall{Input: `{"value":"x"}`})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !resp.IsError {
		t.Fatalf("expected error response")
	}
	if !strings.Contains(resp.Content, `tool "empty_tool" returned an empty response`) {
		t.Fatalf("expected informative empty-response text, got: %q", resp.Content)
	}
}

func TestWithRenderFillsBlankErrorContent(t *testing.T) {
	t.Parallel()

	tool := WithRender(
		llm.NewAgentTool("blank_error", "test", func(ctx context.Context, input testInput, call llm.ToolCall) (llm.ToolResponse, error) {
			return llm.ToolResponse{Type: llm.ToolResponseTypeText, IsError: true}, nil
		}),
		nil,
	)

	resp, err := tool.Run(context.Background(), llm.ToolCall{Input: `{"value":"x"}`})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !resp.IsError {
		t.Fatalf("expected error response")
	}
	if strings.TrimSpace(resp.Content) == "" {
		t.Fatalf("expected non-empty content for blank error response")
	}
	if !strings.Contains(resp.Content, `tool "blank_error" failed with no error details`) {
		t.Fatalf("expected fallback error text, got: %q", resp.Content)
	}
}

func TestWithRenderSkipsAutoTruncationForReadTool(t *testing.T) {
	t.Parallel()

	content := strings.Repeat("line\n", DefaultMaxLines+50)
	tool := WithRender(
		llm.NewAgentTool("read", "test", func(ctx context.Context, input testInput, call llm.ToolCall) (llm.ToolResponse, error) {
			return llm.NewTextResponse(content), nil
		}),
		nil,
	)

	resp, err := tool.Run(context.Background(), llm.ToolCall{Input: `{"value":"x"}`})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.Contains(resp.Content, "[Output truncated:") {
		t.Fatalf("expected read output to skip wrapper truncation, got: %q", resp.Content)
	}
	if resp.Content != content {
		t.Fatalf("expected content unchanged")
	}
}
