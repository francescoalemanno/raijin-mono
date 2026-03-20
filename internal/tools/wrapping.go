package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"unicode"

	"github.com/francescoalemanno/raijin-mono/libagent"
)

// toolWrapper wraps a libagent.Tool and applies unified output handling on Run.
type toolWrapper struct {
	libagent.Tool
	singleLinePreview func(toolCallParams string) string
	finalRender       func(toolCallParams string, toolResult string, toolResultMetadata string) string
}

// WrappedTool is a libagent.Tool enriched with rendering hooks used by UI renderers.
type WrappedTool interface {
	libagent.Tool
	SingleLinePreview(toolCallParams string) string
	FinalRender(toolCallParams string, toolResult string, toolResultMetadata string) string
}

// SingleLinePreview renders a compact, in-progress status line for a tool call.
func (t toolWrapper) SingleLinePreview(toolCallParams string) string {
	if t.singleLinePreview != nil {
		if rendered := strings.TrimSpace(t.singleLinePreview(toolCallParams)); rendered != "" {
			return rendered
		}
	}
	return RenderGenericSingleLinePreview(t.Info().Name, toolCallParams)
}

// FinalRender renders the final status line for a completed tool call.
// By default it falls back to SingleLinePreview unless the tool has richer
// metadata-driven rendering (for example edit/write diffs).
func (t toolWrapper) FinalRender(toolCallParams string, toolResult string, toolResultMetadata string) string {
	if t.finalRender != nil {
		if rendered := strings.TrimSpace(t.finalRender(toolCallParams, toolResult, toolResultMetadata)); rendered != "" {
			return rendered
		}
	}
	return t.SingleLinePreview(toolCallParams)
}

func (t toolWrapper) Run(ctx context.Context, params libagent.ToolCall) (resp libagent.ToolResponse, err error) {
	toolName := t.Info().Name

	defer func() {
		if r := recover(); r != nil {
			// Panic in a tool should never crash the run; surface context to the model/user.
			resp = libagent.NewTextErrorResponse(fmt.Sprintf(
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
			return libagent.ToolResponse{}, err
		}
		return libagent.NewTextErrorResponse(
			fmt.Sprintf("tool %q failed: %s\n\nInput:\n%s", toolName, err.Error(), previewToolInput(params.Input)),
		), nil
	}

	if resp.Type == "" {
		return libagent.NewTextErrorResponse(
			fmt.Sprintf("tool %q returned an empty response.\n\nInput:\n%s", toolName, previewToolInput(params.Input)),
		), nil
	}

	if resp.Type != libagent.ToolResponseTypeText {
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

// WrapTool wraps an AgentTool, applies unified output handling on Run, and
// attaches optional render hooks for preview/final status output.
func WrapTool(
	tool libagent.Tool,
	singleLinePreview func(toolCallParams string) string,
	finalRender func(toolCallParams string, toolResult string, toolResultMetadata string) string,
) libagent.Tool {
	return toolWrapper{
		Tool:              tool,
		singleLinePreview: singleLinePreview,
		finalRender:       finalRender,
	}
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

// FindTool finds a tool by name in a slice of tools.
func FindTool(tools []libagent.Tool, name string) libagent.Tool {
	for _, t := range tools {
		if t.Info().Name == name {
			return t
		}
	}
	return nil
}

type parsedParamsRenderer func(name string, params map[string]any) string

// RenderGenericSingleLinePreview renders a compact one-line preview for tools
// without a specialized renderer.
func RenderGenericSingleLinePreview(toolName, toolCallParams string) string {
	name := strings.TrimSpace(toolName)
	if name == "" {
		name = "tool"
	}

	params, ok := parseToolParams(toolCallParams)
	if !ok {
		return renderRawPreview(name, toolCallParams)
	}
	return renderGenericPreview(name, params)
}

func renderSingleLineForTool(name, toolCallParams string, renderer parsedParamsRenderer) string {
	params, ok := parseToolParams(toolCallParams)
	if !ok {
		return renderRawPreview(name, toolCallParams)
	}
	if renderer == nil {
		return renderGenericPreview(name, params)
	}
	return renderer(name, params)
}

func parseToolParams(toolCallParams string) (map[string]any, bool) {
	trimmed := strings.TrimSpace(toolCallParams)
	if trimmed == "" {
		return nil, false
	}
	var parsed map[string]any
	if err := json.Unmarshal([]byte(trimmed), &parsed); err != nil {
		repaired := closeJSONDelimiters(trimmed)
		if repaired == "" {
			return nil, false
		}
		if err := json.Unmarshal([]byte(repaired), &parsed); err != nil {
			return nil, false
		}
	}
	return parsed, true
}

// closeJSONDelimiters attempts a minimal repair for streaming JSON fragments by
// closing unterminated strings and unmatched object/array delimiters.
func closeJSONDelimiters(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ""
	}

	closers := make([]rune, 0, 8)
	var b strings.Builder
	b.Grow(len(trimmed) + 8)

	inString := false
	escaped := false
	for _, r := range trimmed {
		b.WriteRune(r)
		if inString {
			switch {
			case escaped:
				escaped = false
			case r == '\\':
				escaped = true
			case r == '"':
				inString = false
			}
			continue
		}

		switch r {
		case '"':
			inString = true
		case '{':
			closers = append(closers, '}')
		case '[':
			closers = append(closers, ']')
		case '}', ']':
			if n := len(closers); n > 0 && closers[n-1] == r {
				closers = closers[:n-1]
			}
		}
	}

	if inString {
		b.WriteByte('"')
	}

	// Avoid producing invalid trailing-comma JSON when we append closers.
	current := strings.TrimRightFunc(b.String(), unicode.IsSpace)
	if strings.HasSuffix(current, ",") {
		current = strings.TrimRight(strings.TrimSuffix(current, ","), " \t\r\n")
	}
	b.Reset()
	b.WriteString(current)

	for i := len(closers) - 1; i >= 0; i-- {
		b.WriteRune(closers[i])
	}

	return b.String()
}

func renderRawPreview(toolName, toolCallParams string) string {
	name := strings.TrimSpace(toolName)
	if name == "" {
		name = "tool"
	}
	trimmed := strings.TrimSpace(toolCallParams)
	if trimmed == "" {
		return name
	}
	return fmt.Sprintf("%s %s", name, trimmed)
}

func renderGenericPreview(name string, params map[string]any) string {
	if len(params) == 0 {
		return name
	}
	keys := make([]string, 0, len(params))
	for key := range params {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		return keyPriority(keys[i]) < keyPriority(keys[j]) ||
			(keyPriority(keys[i]) == keyPriority(keys[j]) && keys[i] < keys[j])
	})
	maxKeys := min(3, len(keys))
	parts := make([]string, 0, maxKeys)
	for _, key := range keys[:maxKeys] {
		parts = append(parts, key+"="+renderParamValue(params[key]))
	}
	label := fmt.Sprintf("%s %s", name, strings.Join(parts, " "))
	if len(keys) > maxKeys {
		label += " ..."
	}
	return label
}

func keyPriority(key string) int {
	switch key {
	case "path":
		return 0
	case "pattern":
		return 1
	case "command":
		return 2
	case "include":
		return 3
	case "limit", "offset", "timeout":
		return 4
	case "content", "oldText", "newText":
		return 5
	default:
		return 100
	}
}

func renderParamValue(v any) string {
	switch x := v.(type) {
	case string:
		if x == "" {
			return `""`
		}
		return quoteIfNeeded(x)
	case float64:
		if x == float64(int64(x)) {
			return strconv.FormatInt(int64(x), 10)
		}
		return strconv.FormatFloat(x, 'f', -1, 64)
	case bool:
		return strconv.FormatBool(x)
	case nil:
		return "null"
	default:
		text := strings.TrimSpace(fmt.Sprintf("%v", x))
		if text == "" {
			return `""`
		}
		return quoteIfNeeded(text)
	}
}

func stringParam(params map[string]any, key string) string {
	raw, ok := params[key]
	if !ok {
		return ""
	}
	s, ok := raw.(string)
	if !ok {
		return ""
	}
	return strings.TrimSpace(s)
}

func intParam(params map[string]any, key string) int {
	raw, ok := params[key]
	if !ok {
		return 0
	}
	switch x := raw.(type) {
	case float64:
		return int(x)
	case int:
		return x
	case int32:
		return int(x)
	case int64:
		return int(x)
	default:
		return 0
	}
}

func extraKV(params map[string]any, keys ...string) string {
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		if value := intParam(params, key); value > 0 {
			parts = append(parts, fmt.Sprintf("%s=%d", key, value))
			continue
		}
		if text := stringParam(params, key); text != "" {
			parts = append(parts, fmt.Sprintf("%s=%s", key, quoteIfNeeded(text)))
		}
	}
	return strings.Join(parts, ", ")
}

func quoteIfNeeded(s string) string {
	if s == "" {
		return `""`
	}
	if strings.ContainsAny(s, " \t\n\"'") {
		return strconv.Quote(s)
	}
	return s
}
