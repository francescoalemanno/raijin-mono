package tools

import (
	"context"
	"testing"

	libagent "github.com/francescoalemanno/raijin-mono/libagent"
)

func TestGlobToolRequiresPattern(t *testing.T) {
	t.Parallel()

	tool := NewGlobTool()
	resp, err := tool.Run(context.Background(), libagent.ToolCall{Input: `{}`})
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
	resp, err := tool.Run(context.Background(), libagent.ToolCall{Input: `{}`})
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
	resp, err := tool.Run(context.Background(), libagent.ToolCall{Input: `{}`})
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
	resp, err := tool.Run(context.Background(), libagent.ToolCall{Input: `{"path":"definitely-not-present-file-xyz"}`})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !resp.IsError {
		t.Fatalf("expected error response")
	}
}
