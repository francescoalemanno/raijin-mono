package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/francescoalemanno/raijin-mono/llmbridge/pkg/llm"
)

// RenderableTool extends llm.Tool with UI rendering capability.
// Tools can optionally implement this interface for custom UI display.
// The output parameter is the tool's result text (empty during tool call, populated on tool result).
// The width parameter is the available rendering width (0 if unknown).
type RenderableTool interface {
	llm.Tool
	Render(input json.RawMessage, output string, width int) string
}

// toolWithRender wraps a llm.Tool with a render function.
// It also applies unified output truncation on Run.
type toolWithRender struct {
	llm.Tool
	render func(json.RawMessage, string, int) string
}

func (t toolWithRender) Run(ctx context.Context, params llm.ToolCall) (resp llm.ToolResponse, err error) {
	toolName := t.Info().Name

	defer func() {
		if r := recover(); r != nil {
			// Panic in a tool should never crash the run; surface context to the model/user.
			resp = llm.NewTextErrorResponse(fmt.Sprintf(
				"tool %q panicked: %v\n\nInput:\n%s\n\nRetry with corrected arguments. If this persists, report this tool failure.",
				toolName,
				r,
				previewToolInput(params.Input),
			))
			err = nil
		}
	}()

	resp, err = t.Tool.Run(ctx, params)
	if err != nil {
		if shouldPropagateToolError(ctx, err) {
			return llm.ToolResponse{}, err
		}
		return llm.NewTextErrorResponse(
			fmt.Sprintf("tool %q failed: %s\n\nInput:\n%s", toolName, err.Error(), previewToolInput(params.Input)),
		), nil
	}

	if resp.Type == "" {
		return llm.NewTextErrorResponse(
			fmt.Sprintf("tool %q returned an empty response.\n\nInput:\n%s", toolName, previewToolInput(params.Input)),
		), nil
	}

	if resp.Type != llm.ToolResponseTypeText {
		return resp, nil
	}

	if strings.TrimSpace(resp.Content) == "" {
		if resp.IsError {
			resp.Content = fmt.Sprintf("tool %q failed with no error details.\n\nInput:\n%s", toolName, previewToolInput(params.Input))
		} else {
			resp.Content = "(no output)"
		}
	}
	if !shouldSkipAutoTruncation(toolName) {
		resp.Content = truncateOutput(resp.Content)
	}
	return resp, nil
}

func shouldPropagateToolError(ctx context.Context, err error) bool {
	return errors.Is(err, context.Canceled) ||
		errors.Is(err, context.DeadlineExceeded) ||
		errors.Is(err, ErrCancelled) ||
		ctx.Err() != nil
}

func previewToolInput(input string) string {
	trimmed := strings.TrimSpace(input)
	if trimmed == "" {
		return "(empty)"
	}
	const maxInputPreviewRunes = 500
	runes := []rune(trimmed)
	if len(runes) <= maxInputPreviewRunes {
		return trimmed
	}
	return string(runes[:maxInputPreviewRunes]) + "…"
}

func (t toolWithRender) Render(input json.RawMessage, output string, width int) string {
	if t.render == nil {
		return t.Info().Name
	}
	return t.render(input, output, width)
}

// WithRender wraps an AgentTool with a render function for UI display.
// The returned tool also applies unified output truncation on Run.
func WithRender(tool llm.Tool, render func(json.RawMessage, string, int) string) llm.Tool {
	return toolWithRender{Tool: tool, render: render}
}

func truncateOutput(content string) string {
	result := applyToolTruncation(content)
	if !result.Truncated {
		return content
	}

	tempPath := writeFullOutputToTempFile(content)
	notice := buildTruncationNotice(result, tempPath)

	if strings.TrimSpace(result.Content) == "" {
		return notice
	}
	return result.Content + "\n\n" + notice
}

func shouldSkipAutoTruncation(toolName string) bool {
	// read, bash, and glob perform their own truncation and continuation hints.
	return toolName == "read" || toolName == "bash" || toolName == "glob"
}

func applyToolTruncation(content string) TruncationResult {
	opts := TruncationOptions{MaxLines: DefaultMaxLines, MaxBytes: DefaultMaxBytes}
	return TruncateHead(content, opts)
}

func writeFullOutputToTempFile(content string) string {
	tempFile, err := os.CreateTemp("", toolOutputTempPrefix)
	if err != nil {
		return ""
	}

	_, _ = tempFile.WriteString(content)
	_ = tempFile.Close()
	return tempFile.Name()
}

func buildTruncationNotice(result TruncationResult, tempPath string) string {
	var message string

	switch {
	case result.FirstLineExceedsLimit:
		message = fmt.Sprintf(
			"[Output truncated: first line exceeds %s limit]",
			FormatSize(result.MaxBytes),
		)
	default:
		message = fmt.Sprintf(
			"[Output truncated: showing first %d of %d lines (%d-line, %s limits)]",
			result.OutputLines,
			result.TotalLines,
			result.MaxLines,
			FormatSize(result.MaxBytes),
		)
	}

	if result.TruncatedBy == "bytes" && !result.FirstLineExceedsLimit {
		message += " [byte limit reached]"
	}
	if result.LastLinePartial {
		message += " [partial line shown]"
	}
	if tempPath != "" {
		message += fmt.Sprintf(" [full output: %s]", tempPath)
	}
	return message
}

// RenderTool attempts to render a tool call for UI display.
// Returns the tool name if the tool doesn't implement RenderableTool.
func RenderTool(tool llm.Tool, input json.RawMessage, output string, width int) string {
	if rt, ok := tool.(RenderableTool); ok {
		return rt.Render(input, output, width)
	}
	return tool.Info().Name
}

// FindTool finds a tool by name in a slice of tools.
func FindTool(tools []llm.Tool, name string) llm.Tool {
	for _, t := range tools {
		if t.Info().Name == name {
			return t
		}
	}
	return nil
}
