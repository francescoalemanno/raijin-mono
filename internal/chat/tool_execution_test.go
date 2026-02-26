package chat

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/francescoalemanno/raijin-mono/internal/tools"
	"github.com/francescoalemanno/raijin-mono/libtui/pkg/utils"
	"github.com/francescoalemanno/raijin-mono/llmbridge/pkg/llm"
)

func TestToolExecutionRenderPartsShowsErrorOutputWhenRendererBodyIsEmpty(t *testing.T) {
	t.Parallel()

	tool := tools.WithRender(
		llm.NewAgentTool("read", "test", func(ctx context.Context, input map[string]any, call llm.ToolCall) (llm.ToolResponse, error) {
			return llm.NewTextResponse("ok"), nil
		}),
		func(input json.RawMessage, output string, width int) string {
			return "read ./file.txt"
		},
	)

	comp := &ToolExecutionComponent{
		toolName: "read",
		tool:     tool,
	}

	title, body := comp.renderParts(json.RawMessage(`{"path":"./file.txt"}`), "path is required", true)
	if utils.StripAnsiCodes(title) != "read ./file.txt" {
		t.Fatalf("unexpected title: %q", title)
	}
	if utils.StripAnsiCodes(body) != "path is required" {
		t.Fatalf("expected fallback error body, got: %q", body)
	}
}

func TestToolExecutionRenderPartsIncludesArgsPreviewWithoutRenderer(t *testing.T) {
	t.Parallel()

	tool := llm.NewAgentTool("grep", "test", func(ctx context.Context, input map[string]any, call llm.ToolCall) (llm.ToolResponse, error) {
		return llm.NewTextResponse("ok"), nil
	})

	comp := &ToolExecutionComponent{
		toolName: "grep",
		tool:     tool,
	}

	title, _ := comp.renderParts(json.RawMessage(`{"pattern":"foo","path":"."}`), "", false)
	titlePlain := utils.StripAnsiCodes(title)
	if !strings.HasPrefix(titlePlain, "grep ") {
		t.Fatalf("expected title prefix with tool name, got: %q", titlePlain)
	}
	if !strings.Contains(titlePlain, `"pattern":"foo"`) {
		t.Fatalf("expected args preview in title, got: %q", titlePlain)
	}
}

func TestToolExecutionRenderPartsShowsPartialArgsOnError(t *testing.T) {
	t.Parallel()

	tool := llm.NewAgentTool("read", "test", func(ctx context.Context, input map[string]any, call llm.ToolCall) (llm.ToolResponse, error) {
		return llm.NewTextResponse("ok"), nil
	})

	comp := &ToolExecutionComponent{
		toolName: "read",
		tool:     tool,
		rawInput: `{"path":"internal/tools/read.go"`,
	}

	title, body := comp.renderParts(nil, "", true)
	if !strings.Contains(title, "partial args") {
		t.Fatalf("expected partial args marker in title, got: %q", title)
	}
	if !strings.Contains(body, "partial arguments") {
		t.Fatalf("expected partial args body, got: %q", body)
	}
}
