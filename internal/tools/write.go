package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/francescoalemanno/raijin-mono/internal/vfs"
	"github.com/francescoalemanno/raijin-mono/libagent"
)

// NewWriteTool creates a new tool for writing content to a file.
func NewWriteTool() libagent.Tool {
	type writeToolParams struct {
		Path    string `json:"path" description:"Path to the file to write (relative or absolute)"`
		Content string `json:"content" description:"Content to write to the file"`
	}

	v := vfs.NewFromWD()

	handler := func(ctx context.Context, params writeToolParams, call libagent.ToolCall) (libagent.ToolResponse, error) {
		if params.Path == "" {
			return libagent.NewTextErrorResponse("path is required"), nil
		}

		if ctx.Err() != nil {
			return libagent.ToolResponse{}, ctx.Err()
		}

		if err := v.MkdirAll(filepath.Dir(params.Path), defaultDirPerm); err != nil {
			return libagent.NewTextErrorResponse(vfs.DescribeAccessError(params.Path, err)), nil
		}

		if err := v.WriteFile(params.Path, []byte(params.Content), defaultFilePerm); err != nil {
			return libagent.NewTextErrorResponse(vfs.DescribeAccessError(params.Path, err)), nil
		}
		resp := libagent.NewTextResponse(fmt.Sprintf("Successfully wrote file %s.", params.Path))
		return resp, nil
	}

	renderFunc := func(input json.RawMessage, _ string, _ int) string {
		var params writeToolParams
		if err := libagent.ParseJSONInput(input, &params); err != nil {
			return "write file (failed)"
		}
		lines := strings.Count(params.Content, "\n") + 1
		if params.Content == "" {
			lines = 0
		}
		path := RenderPath(params.Path)
		header := fmt.Sprintf("wrote %s (%d lines)", path, lines)
		if params.Content == "" {
			return header
		}
		return header + "\n" + renderCodePreview(params.Path, params.Content)
	}

	return WithRender(
		libagent.NewParallelTypedTool("write", "Write content to a file. creates the file if it doesn't exist, overwrites if it does. Automatically creates parent directories.", handler),
		renderFunc,
	)
}
