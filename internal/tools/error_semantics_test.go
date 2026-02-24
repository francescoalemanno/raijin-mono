package tools

import (
	"context"
	"testing"

	"github.com/francescoalemanno/raijin-mono/llmbridge/pkg/llm"
)

func TestGlobToolRequiresPattern(t *testing.T) {
	t.Parallel()

	tool := NewGlobTool()
	resp, err := tool.Run(context.Background(), llm.ToolCall{Input: `{}`})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !resp.IsError {
		t.Fatalf("expected error response")
	}
}

func TestGrepToolRequiresPattern(t *testing.T) {
	t.Parallel()

	tool := NewGrepTool()
	resp, err := tool.Run(context.Background(), llm.ToolCall{Input: `{}`})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !resp.IsError {
		t.Fatalf("expected error response")
	}
}

func TestReadToolRequiresPath(t *testing.T) {
	t.Parallel()

	tool := NewReadTool()
	resp, err := tool.Run(context.Background(), llm.ToolCall{Input: `{}`})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !resp.IsError {
		t.Fatalf("expected error response")
	}
}

func TestReadToolMissingFileIsError(t *testing.T) {
	t.Parallel()

	tool := NewReadTool()
	resp, err := tool.Run(context.Background(), llm.ToolCall{Input: `{"path":"definitely-not-present-file-xyz"}`})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !resp.IsError {
		t.Fatalf("expected error response")
	}
}

func TestWebFetchToolValidatesURL(t *testing.T) {
	t.Parallel()

	tool := NewWebFetchTool()
	resp, err := tool.Run(context.Background(), llm.ToolCall{Input: `{}`})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !resp.IsError {
		t.Fatalf("expected error response for empty URL")
	}

	resp, err = tool.Run(context.Background(), llm.ToolCall{Input: `{"url":"file:///tmp/a"}`})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !resp.IsError {
		t.Fatalf("expected error response for file:// URL")
	}
}
