package chat

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/francescoalemanno/raijin-mono/libtui/pkg/components"
	"github.com/francescoalemanno/raijin-mono/libtui/pkg/tui"

	"github.com/francescoalemanno/raijin-mono/internal/theme"
	"github.com/francescoalemanno/raijin-mono/internal/tools"

	"github.com/francescoalemanno/raijin-mono/llmbridge/pkg/llm"
)

const toolPreviewLines = 10

// ToolExecutionComponent renders a tool call with background color that
// reflects its lifecycle state (pending → success/error).
type ToolExecutionComponent struct {
	toolName string
	args     json.RawMessage
	rawInput string // accumulated streaming input deltas
	result   *toolResult

	// The registered tool definition (for Render calls).
	tool llm.Tool

	status *StatusBlock
}

type toolResult struct {
	output  string
	isError bool
	media   *Payload
}

// NewToolExecution creates a new tool execution component.
// It starts in "pending" state with a spinner.
func NewToolExecution(toolName string, args json.RawMessage, tool llm.Tool, ui components.UILike) *ToolExecutionComponent {
	t := &ToolExecutionComponent{
		toolName: toolName,
		args:     args,
		tool:     tool,
		status:   NewStatusBlock(ui, theme.ColorAccent, theme.ColorMuted, "running…"),
	}
	t.updateContent()
	return t
}

// MarkCancelled stops the spinner and marks the tool as cancelled.
// Used when the run is interrupted or completes without a result event.
func (t *ToolExecutionComponent) MarkCancelled() {
	if t.result != nil {
		return
	}
	t.result = &toolResult{output: "(cancelled)", isError: true}
	t.status.Transition(StatusError, t.buildContent())
}

// UpdateArgs updates the tool arguments as they stream in.
func (t *ToolExecutionComponent) UpdateArgs(args json.RawMessage) {
	t.args = args
	t.updateContent()
}

// AppendInputDelta appends a streaming delta to the accumulated raw input.
func (t *ToolExecutionComponent) AppendInputDelta(delta string) {
	t.rawInput += delta
	t.updateContent()
}

// UpdateResult sets the final result and stops the spinner.
func (t *ToolExecutionComponent) UpdateResult(output string, isError bool) {
	t.UpdateResultWithMedia(output, isError, "", "")
}

// UpdateResultWithMedia sets the final result, optionally carrying media payload.
func (t *ToolExecutionComponent) UpdateResultWithMedia(output string, isError bool, mimeType, dataBase64 string) {
	var payload *Payload
	if normalized, ok := NormalizeImagePayload(mimeType, dataBase64); ok {
		payload = &Payload{
			MIMEType: mimeType,
			Data:     normalized,
			Source:   "tool_result",
		}
	}

	t.result = &toolResult{output: output, isError: isError, media: payload}
	st := StatusSuccess
	if isError {
		st = StatusError
	}
	t.status.Transition(st, t.buildContent())
}

// SetExpanded controls whether the tool output is fully expanded.
func (t *ToolExecutionComponent) SetExpanded(expanded bool) {
	t.status.SetExpanded(expanded)
	t.updateContent()
}

// IsExpanded returns whether the component is expanded.
func (t *ToolExecutionComponent) IsExpanded() bool {
	return t.status.IsExpanded()
}

// IsPending returns whether the tool is still executing.
func (t *ToolExecutionComponent) IsPending() bool {
	return t.result == nil
}

// ---------------------------------------------------------------------------
// Content rendering
// ---------------------------------------------------------------------------

func (t *ToolExecutionComponent) updateContent() {
	t.status.SetText(t.buildContent())
}

func (t *ToolExecutionComponent) buildContent() string {
	var args json.RawMessage
	var output string
	isError := false

	if t.result != nil {
		args = t.bestArgs()
		output = t.result.output
		isError = t.result.isError
	} else {
		args = t.bestArgs()
	}

	title, body := t.renderParts(args, output, isError)
	if t.result != nil && t.result.media != nil && strings.TrimSpace(body) == "" {
		body = fmt.Sprintf("media attached (%s)", t.result.media.MIMEType)
	}

	icon := "⟳"
	if t.result != nil {
		if t.result.isError {
			icon = "✗"
		} else {
			icon = "✓"
		}
	}
	header := fmt.Sprintf("%s %s", icon, theme.ColorToolTitle(title))

	if body != "" {
		return header + "\n" + t.truncateContent(body)
	}
	return header
}

// bestArgs returns the best available args: complete args if present,
// otherwise attempts to close the partial streaming JSON.
func (t *ToolExecutionComponent) bestArgs() json.RawMessage {
	if len(t.args) > 0 {
		return t.args
	}
	if t.rawInput != "" {
		return closePartialJSON(t.rawInput)
	}
	return nil
}

// renderParts calls the tool's Render once and splits the result into a
// one-line title and an optional multi-line body. This is the single
// entry point for all tool rendering — both streaming and completed states.
func (t *ToolExecutionComponent) renderParts(args json.RawMessage, output string, isError bool) (title, body string) {
	title = t.toolName

	argsValid := json.Valid(args)

	rt, ok := t.tool.(tools.RenderableTool)
	if ok && argsValid {
		rendered := rt.Render(args, output, 0)
		if rendered != "" {
			first, rest, hasSep := strings.Cut(rendered, "\n")
			if strings.TrimSpace(first) != "" {
				title = first
			}
			if hasSep {
				body = rest
			}
		}
	}

	// Fallback title when a tool has no renderer or args are malformed.
	if title == t.toolName {
		if argsPreview := compactJSON(args, 72); argsPreview != "" {
			title = fmt.Sprintf("%s %s", title, argsPreview)
		} else if strings.TrimSpace(t.rawInput) != "" {
			title = title + " (partial args)"
		}
	}

	// Always surface failures. Some tool renderers intentionally hide raw output.
	if isError && strings.TrimSpace(body) == "" {
		if strings.TrimSpace(output) != "" {
			body = output
		} else if strings.TrimSpace(t.rawInput) != "" && !argsValid {
			body = "partial arguments:\n" + strings.TrimSpace(t.rawInput)
		} else {
			body = "tool failed with no error details"
		}
	}

	return title, body
}

func compactJSON(raw json.RawMessage, maxRunes int) string {
	if len(raw) == 0 || !json.Valid(raw) || maxRunes <= 0 {
		return ""
	}
	var buf bytes.Buffer
	if err := json.Compact(&buf, raw); err != nil {
		return ""
	}
	s := buf.String()
	if utf8.RuneCountInString(s) <= maxRunes {
		return s
	}
	runes := []rune(s)
	if maxRunes == 1 {
		return "…"
	}
	return string(runes[:maxRunes-1]) + "…"
}

// truncateContent applies the expand/collapse truncation to multi-line content.
func (t *ToolExecutionComponent) truncateContent(content string) string {
	lines := strings.Split(content, "\n")
	if t.status.IsExpanded() || len(lines) <= toolPreviewLines {
		return content
	}

	preview := strings.Join(lines[:toolPreviewLines], "\n")
	remaining := len(lines) - toolPreviewLines
	hint := theme.ColorMuted(fmt.Sprintf("… (%d more lines, press 'ctrl+o' to expand)", remaining))
	return preview + "\n" + hint
}

// ---------------------------------------------------------------------------
// Partial JSON closing
// ---------------------------------------------------------------------------

// closePartialJSON attempts to make partial JSON valid by appending closing
// delimiters (close quotes, braces, brackets). Returns nil if the input
// cannot be salvaged into valid JSON. Used only for UI preview during streaming.
func closePartialJSON(partial string) json.RawMessage {
	s := strings.TrimRight(partial, " \t\n\r")
	if s == "" {
		return nil
	}

	// Track what needs closing: scan character by character respecting strings.
	var stack []byte // pending closers: '}', ']'
	inString := false
	escaped := false

	for i := 0; i < len(s); i++ {
		c := s[i]
		if escaped {
			escaped = false
			continue
		}
		if c == '\\' && inString {
			escaped = true
			continue
		}
		if c == '"' {
			inString = !inString
			continue
		}
		if inString {
			continue
		}
		switch c {
		case '{':
			stack = append(stack, '}')
		case '[':
			stack = append(stack, ']')
		case '}', ']':
			if len(stack) > 0 && stack[len(stack)-1] == c {
				stack = stack[:len(stack)-1]
			}
		}
	}

	var buf strings.Builder
	buf.WriteString(s)

	// If we're inside a string, close it
	if inString {
		if escaped {
			// Dangling backslash inside string — drop it to keep JSON valid
			buf.Reset()
			buf.WriteString(s[:len(s)-1])
		}
		buf.WriteByte('"')
	}

	// Handle trailing structural characters
	closed := strings.TrimRight(buf.String(), " \t\n\r")
	if len(closed) > 0 {
		last := closed[len(closed)-1]
		switch last {
		case ',':
			// Trailing comma: remove it (e.g. {"path":"x",)
			buf.Reset()
			buf.WriteString(closed[:len(closed)-1])
		case ':':
			// Trailing colon: append null (works for any field type)
			buf.WriteString("null")
		}
	}

	// Close remaining open brackets/braces in reverse order
	for i := len(stack) - 1; i >= 0; i-- {
		buf.WriteByte(stack[i])
	}

	result := []byte(buf.String())
	if !json.Valid(result) {
		return nil
	}
	return json.RawMessage(result)
}

// --- Component interface ---

func (t *ToolExecutionComponent) Render(width int) []string { return t.status.Render(width) }
func (t *ToolExecutionComponent) HandleInput(data string)   {}
func (t *ToolExecutionComponent) Invalidate()               { t.status.Invalidate() }

var _ tui.Component = (*ToolExecutionComponent)(nil)
