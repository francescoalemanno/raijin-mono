package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/francescoalemanno/raijin-mono/libagent"
)

// NewWriteTool creates a write tool.
func NewWriteTool() libagent.Tool {
	type writeToolParams struct {
		Path    string `json:"path" description:"Path to the file to write (relative or absolute)"`
		Content string `json:"content" description:"Content to write to the file"`
	}

	handler := func(ctx context.Context, params writeToolParams, call libagent.ToolCall) (libagent.ToolResponse, error) {
		if resp, blocked := toolExecutionGate(ctx, "write"); blocked {
			return resp, nil
		}
		if params.Path == "" {
			return libagent.NewTextErrorResponse("path is required"), nil
		}

		if ctx.Err() != nil {
			return libagent.ToolResponse{}, ctx.Err()
		}

		if err := os.MkdirAll(filepath.Dir(params.Path), defaultDirPerm); err != nil {
			return libagent.NewTextErrorResponse(fmt.Sprintf("creating directories: %s", err)), nil
		}

		if err := os.WriteFile(params.Path, []byte(params.Content), defaultFilePerm); err != nil {
			return libagent.NewTextErrorResponse(fmt.Sprintf("writing file: %s", err)), nil
		}
		resp := libagent.NewTextResponse(fmt.Sprintf("Successfully created file %s.", params.Path))
		return resp, nil
	}

	renderFunc := func(input json.RawMessage, _ string, _ int) string {
		var params writeToolParams
		if err := libagent.ParseJSONInput(input, &params); err != nil {
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
		libagent.NewTypedTool("write", "Write content to a file. Creates the file if it doesn't exist, overwrites if it does. Automatically creates parent directories.", handler),
		renderFunc,
	)
}
