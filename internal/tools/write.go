package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/francescoalemanno/raijin-mono/llmbridge/pkg/llm"
)

// NewWriteTool creates a write tool.
func NewWriteTool() llm.Tool {
	type writeToolParams struct {
		Path    string `json:"path" description:"Path to the file to write (relative or absolute)"`
		Content string `json:"content" description:"Content to write to the file"`
	}

	handler := func(ctx context.Context, params writeToolParams, call llm.ToolCall) (llm.ToolResponse, error) {
		if resp, blocked := toolExecutionGate(ctx, "write"); blocked {
			return resp, nil
		}
		if params.Path == "" {
			return llm.NewTextErrorResponse("path is required"), nil
		}

		if ctx.Err() != nil {
			return llm.ToolResponse{}, ctx.Err()
		}

		if err := os.MkdirAll(filepath.Dir(params.Path), defaultDirPerm); err != nil {
			return llm.NewTextErrorResponse(fmt.Sprintf("creating directories: %s", err)), nil
		}

		if err := os.WriteFile(params.Path, []byte(params.Content), defaultFilePerm); err != nil {
			return llm.NewTextErrorResponse(fmt.Sprintf("writing file: %s", err)), nil
		}
		resp := llm.NewTextResponse(fmt.Sprintf("Successfully created file %s.", params.Path))
		return resp, nil
	}

	renderFunc := func(input json.RawMessage, _ string, _ int) string {
		var params writeToolParams
		if err := llm.ParseJSONInput(input, &params); err != nil {
			return "create file (failed)"
		}
		lines := strings.Count(params.Content, "\n") + 1
		if params.Content == "" {
			lines = 0
		}
		path := RenderPath(params.Path)
		header := fmt.Sprintf("create %s (%d lines)", path, lines)
		if params.Content == "" {
			return header
		}
		return header + "\n" + renderCodePreview(params.Path, params.Content)
	}

	return WithRender(
		llm.NewAgentTool("write", "Write content to a file. Creates the file if it doesn't exist, overwrites if it does. Automatically creates parent directories.", handler),
		renderFunc,
	)
}
