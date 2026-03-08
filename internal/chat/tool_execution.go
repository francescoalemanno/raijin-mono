package chat

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"hash"
	"hash/fnv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/francescoalemanno/raijin-mono/libtui/pkg/components"
	"github.com/francescoalemanno/raijin-mono/libtui/pkg/tui"

	"github.com/francescoalemanno/raijin-mono/internal/theme"
	"github.com/francescoalemanno/raijin-mono/internal/tools"

	libagent "github.com/francescoalemanno/raijin-mono/libagent"
)

const (
	toolPreviewLines                  = 10
	toolStreamingRenderMinCheckpoint  = 10
	toolStreamingRenderMinInterval    = 100 * time.Millisecond
	toolStreamingRenderCheckpointByte = 640
)

// ToolExecutionComponent renders a tool call with background color that
// reflects its lifecycle state (pending → success/error).
type ToolExecutionComponent struct {
	toolName string
	args     json.RawMessage
	rawInput string // accumulated streaming input deltas
	result   *toolResult

	// The registered tool definition (for Render calls).
	tool libagent.Tool

	status *StatusBlock

	streamingInputBytes int
	streamingRenderGate *renderGate

	cachedContentHash uint64
	cachedContent     string
	hasCachedContent  bool
}

type toolResult struct {
	output  string
	isError bool
	media   *Payload
}

// NewToolExecution creates a new tool execution component.
// It starts in "pending" state with a spinner.
func NewToolExecution(toolName string, args json.RawMessage, tool libagent.Tool, ui components.UILike) *ToolExecutionComponent {
	t := &ToolExecutionComponent{
		toolName: toolName,
		args:     args,
		tool:     tool,
		status:   NewStatusBlock(ui, theme.Default.Accent.Ansi24, theme.Default.Muted.Ansi24, "running…"),
		streamingRenderGate: newRenderGate(
			toolStreamingRenderMinCheckpoint,
			toolStreamingRenderCheckpointByte,
			10,
			toolStreamingRenderMinInterval,
		),
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
	t.updateStreamingContent(true)
}

// AppendInputDelta appends a streaming delta to the accumulated raw input.
func (t *ToolExecutionComponent) AppendInputDelta(delta string) {
	wasEmpty := t.rawInput == ""
	t.rawInput += delta
	t.streamingInputBytes += len(delta)
	t.updateStreamingContent(wasEmpty)
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
	if t.status.IsExpanded() == expanded {
		return
	}
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

func (t *ToolExecutionComponent) updateStreamingContent(force bool) {
	if t.result != nil {
		t.updateContent()
		return
	}
	if t.streamingRenderGate.shouldRender(t.streamingInputBytes, force) {
		t.updateContent()
	}
}

func streamingRenderInterval(totalBytes int) int {
	return newRenderGate(
		toolStreamingRenderMinCheckpoint,
		toolStreamingRenderCheckpointByte,
		10,
		toolStreamingRenderMinInterval,
	).interval(totalBytes)
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

	contentHash := t.computeContentHash(args, output, isError)
	if t.hasCachedContent && contentHash == t.cachedContentHash {
		return t.cachedContent
	}

	title, body := t.renderParts(args, output, isError)
	if t.result != nil && t.result.media != nil && strings.TrimSpace(body) == "" {
		body = theme.Default.Foreground.Ansi24(fmt.Sprintf("media attached (%s)", t.result.media.MIMEType))
	}

	icon := theme.Default.Foreground.Ansi24("⟳")
	if t.result != nil {
		if t.result.isError {
			icon = theme.Default.Danger.Ansi24("✗")
		} else {
			icon = theme.Default.Success.Ansi24("✓")
		}
	}
	header := icon + theme.Default.Foreground.Ansi24(" ") + theme.Default.ToolTitle.AnsiBold(title)

	content := header
	if body != "" {
		content = header + "\n" + t.truncateContent(body)
	}

	t.cachedContentHash = contentHash
	t.cachedContent = content
	t.hasCachedContent = true
	return content
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
			title = theme.Default.ToolTitle.AnsiBold(title) + theme.Default.Foreground.Ansi24(" "+argsPreview)
		} else if strings.TrimSpace(t.rawInput) != "" {
			title = theme.Default.ToolTitle.AnsiBold(title) + theme.Default.Foreground.Ansi24(" (partial args)")
		}
	}

	// Always surface failures. Some tool renderers intentionally hide raw output.
	if isError && strings.TrimSpace(body) == "" {
		if strings.TrimSpace(output) != "" {
			body = theme.Default.Foreground.Ansi24(output)
		} else if strings.TrimSpace(t.rawInput) != "" && !argsValid {
			body = theme.Default.Foreground.Ansi24("partial arguments:\n" + strings.TrimSpace(t.rawInput))
		} else {
			body = theme.Default.Foreground.Ansi24("tool failed with no error details")
		}
	}

	return title, body
}

func (t *ToolExecutionComponent) computeContentHash(args json.RawMessage, output string, isError bool) uint64 {
	h := fnv.New64a()
	writeHashString(h, t.toolName)
	writeHashBytes(h, args)
	writeHashString(h, t.rawInput)
	writeHashString(h, output)
	if isError {
		_, _ = h.Write([]byte{1})
	} else {
		_, _ = h.Write([]byte{0})
	}
	if t.result != nil && t.result.media != nil {
		_, _ = h.Write([]byte{1})
		writeHashString(h, t.result.media.MIMEType)
	} else {
		_, _ = h.Write([]byte{0})
	}
	if t.status != nil && t.status.IsExpanded() {
		_, _ = h.Write([]byte{1})
	} else {
		_, _ = h.Write([]byte{0})
	}
	return h.Sum64()
}

func writeHashString(h hash.Hash64, s string) {
	var lenBuf [8]byte
	binary.LittleEndian.PutUint64(lenBuf[:], uint64(len(s)))
	_, _ = h.Write(lenBuf[:])
	_, _ = h.Write([]byte(s))
}

func writeHashBytes(h hash.Hash64, b []byte) {
	var lenBuf [8]byte
	binary.LittleEndian.PutUint64(lenBuf[:], uint64(len(b)))
	_, _ = h.Write(lenBuf[:])
	_, _ = h.Write(b)
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
		return theme.Default.Foreground.Ansi24(content)
	}

	preview := strings.Join(lines[:toolPreviewLines], "\n")
	remaining := len(lines) - toolPreviewLines
	hint := theme.Default.Muted.Ansi24(fmt.Sprintf("… (%d more lines, press 'ctrl+o' to expand)", remaining))
	return theme.Default.Foreground.Ansi24(preview) + "\n" + hint
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
