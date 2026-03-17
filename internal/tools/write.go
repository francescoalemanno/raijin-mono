package tools

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"

	"github.com/francescoalemanno/raijin-mono/internal/vfs"
	"github.com/francescoalemanno/raijin-mono/libagent"
)

func RenderWriteSingleLinePreview(toolCallParams string) string {
	return renderSingleLineForTool("write", toolCallParams, renderWriteToolPreview)
}

func RenderWriteFinalRender(toolCallParams, _ string, toolResultMetadata string) string {
	return renderDiffAwareFinal(RenderWriteSingleLinePreview, toolCallParams, toolResultMetadata)
}

func renderWriteToolPreview(name string, params map[string]any) string {
	path := stringParam(params, "path")
	content := stringParam(params, "content")
	if path == "" {
		return renderGenericPreview(name, params)
	}
	if content == "" {
		return fmt.Sprintf("%s %s", name, path)
	}
	return fmt.Sprintf("%s %s (%d chars)", name, path, len(content))
}

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
		if vfs.IsEmbedded(params.Path) {
			return libagent.NewTextErrorResponse(vfs.DescribeAccessError(params.Path, vfs.ErrReadOnly)), nil
		}

		if ctx.Err() != nil {
			return libagent.ToolResponse{}, ctx.Err()
		}

		oldContent := ""
		oldExists := false
		if existing, err := v.ReadFile(params.Path); err == nil {
			oldContent = string(existing)
			oldExists = true
		} else if !errors.Is(err, vfs.ErrNotFound) {
			return libagent.NewTextErrorResponse(vfs.DescribeAccessError(params.Path, err)), nil
		}

		if err := v.MkdirAll(filepath.Dir(params.Path), defaultDirPerm); err != nil {
			return libagent.NewTextErrorResponse(vfs.DescribeAccessError(params.Path, err)), nil
		}

		if err := v.WriteFile(params.Path, []byte(params.Content), defaultFilePerm); err != nil {
			return libagent.NewTextErrorResponse(vfs.DescribeAccessError(params.Path, err)), nil
		}

		details := generateDiffString(oldContent, params.Content, 4)
		summary := fmt.Sprintf("Successfully wrote file %s.", params.Path)
		if !oldExists {
			summary = fmt.Sprintf("Successfully created file %s.", params.Path)
		}
		resp := libagent.NewTextResponse(summary)
		resp = libagent.WithResponseMetadata(resp, details)
		return resp, nil
	}

	return WrapTool(
		libagent.NewParallelTypedTool("write", "Write content to a file. creates the file if it doesn't exist, overwrites if it does. Automatically creates parent directories.", handler),
		RenderWriteSingleLinePreview,
		RenderWriteFinalRender,
	)
}
