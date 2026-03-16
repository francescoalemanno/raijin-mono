package oneshot

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/charmbracelet/lipgloss"
	"github.com/francescoalemanno/raijin-mono/internal/tools"
	libagent "github.com/francescoalemanno/raijin-mono/libagent"
)

var thinkingMutedStyle = oneshotMutedStyle

const userPromptGlyph = "❯"

// pendingLine tracks a tool or thinking event currently in progress.
type pendingLine struct {
	id        string
	toolName  string
	label     string
	args      json.RawMessage
	rawInput  strings.Builder
	startTime time.Time
	ended     bool
	endResult string
	endError  bool
}

// renderer writes streaming status lines to stderr and the final
// assistant response to stdout.
type renderer struct {
	mu      sync.Mutex
	stderr  io.Writer
	stdout  io.Writer
	tools   []libagent.Tool
	isTTY   bool
	started bool

	// state
	pending       map[string]*pendingLine // tool call ID -> pending
	pendingInline bool
	pendingWidth  int
	thinking      bool
	thinkingStart time.Time
	thinkingSeen  bool
	thinkingLine  strings.Builder
	replyMD       *lineMarkdownRenderer
	replyLine     strings.Builder
	replyText     strings.Builder
}

func newRenderer(stderr, stdout io.Writer, agentTools []libagent.Tool, isTTY bool) *renderer {
	return &renderer{
		stderr:  stderr,
		stdout:  stdout,
		tools:   agentTools,
		isTTY:   isTTY,
		pending: make(map[string]*pendingLine),
		replyMD: newLineMarkdownRenderer(),
	}
}

func (r *renderer) handleEvent(event libagent.AgentEvent) {
	r.mu.Lock()
	defer r.mu.Unlock()

	switch event.Type {
	case libagent.AgentEventTypeAgentStart:
		r.started = true

	case libagent.AgentEventTypeTurnStart:
		// No-op by design: keep stderr concise.

	case libagent.AgentEventTypeMessageStart:
		// No-op by design: rendering is driven by deltas and message_end.

	case libagent.AgentEventTypeMessageUpdate:
		r.onMessageUpdate(event)

	case libagent.AgentEventTypeToolExecutionStart:
		r.onToolStart(event.ToolCallID, event.ToolName, event.ToolArgs)

	case libagent.AgentEventTypeToolExecutionUpdate:
		r.onToolUpdate(event.ToolCallID, event.ToolName, event.ToolArgs)

	case libagent.AgentEventTypeToolExecutionEnd:
		r.onToolEnd(event.ToolCallID, event.ToolName, event.ToolArgs, event.ToolResult, event.ToolIsError)

	case libagent.AgentEventTypeMessageEnd:
		r.onMessageEnd(event)

	case libagent.AgentEventTypeTurnEnd:
		r.flushCompletedTools()

	case libagent.AgentEventTypeRetry:
		if event.RetryMessage != "" {
			r.emitStatus(renderStatusWarning("↻"), event.RetryMessage)
		}

	case libagent.AgentEventTypeAgentEnd:
		r.finalize()
	}
}

func (r *renderer) onMessageUpdate(event libagent.AgentEvent) {
	delta := event.Delta
	if delta == nil {
		return
	}
	switch delta.Type {
	case "text_start":
		// No-op: we only render content-bearing deltas.
	case "text_delta":
		r.appendReplyDelta(delta.Delta)
	case "text_end":
		r.flushReplyTail()
	case "reasoning_start":
		r.startThinking()
	case "reasoning_delta":
		r.startThinking()
		r.appendThinkingDelta(delta.Delta)
	case "reasoning_end":
		r.flushThinking()
	case "tool_input_start":
		r.onToolStart(delta.ID, delta.ToolName, "")
	case "tool_input_delta":
		r.onToolInputDelta(delta.ID, delta.Delta)
	case "tool_input_end":
		// No-op: execution-start/end events cover lifecycle; keep output compact.
	}
}

func (r *renderer) onToolStart(id, name, input string) {
	if p, ok := r.pending[id]; ok {
		if strings.TrimSpace(input) != "" {
			p.args = json.RawMessage(input)
		}
		p.label = r.renderToolLabel(p.toolName, r.bestArgs(p))
		return
	}
	r.flushThinking()
	p := &pendingLine{
		id:        id,
		toolName:  name,
		startTime: time.Now(),
	}
	if strings.TrimSpace(input) != "" {
		p.args = json.RawMessage(input)
	}
	p.label = r.renderToolLabel(name, r.bestArgs(p))
	r.pending[id] = p
	r.emitPending(renderStatusInfo("●"), p.label)
}

func (r *renderer) onToolUpdate(id, name, input string) {
	if p, ok := r.pending[id]; ok {
		if strings.TrimSpace(input) != "" {
			p.args = json.RawMessage(input)
		}
		p.label = r.renderToolLabel(p.toolName, r.bestArgs(p))
		return
	}
	if strings.TrimSpace(id) == "" {
		return
	}
	r.onToolStart(id, name, input)
}

func (r *renderer) onToolInputDelta(id, delta string) {
	// Update label but don't re-render every delta to avoid flicker
	if p, ok := r.pending[id]; ok {
		p.rawInput.WriteString(delta)
		// Prefer complete args from execution-start if present.
		if len(p.args) == 0 {
			p.label = r.renderToolLabel(p.toolName, r.bestArgs(p))
		}
	}
}

func (r *renderer) onToolEnd(id, name, input, output string, isError bool) {
	if strings.TrimSpace(id) == "" {
		return
	}
	p, ok := r.pending[id]
	if !ok {
		r.onToolStart(id, name, input)
		p, ok = r.pending[id]
		if !ok {
			return
		}
	}
	if strings.TrimSpace(input) != "" {
		p.args = json.RawMessage(input)
	}
	p.label = r.renderToolLabel(p.toolName, r.bestArgs(p))
	p.ended = true
	p.endResult = output
	p.endError = isError
}

func (r *renderer) onMessageEnd(event libagent.AgentEvent) {
	switch m := event.Message.(type) {
	case *libagent.UserMessage:
		r.flushReplyTail()
		r.flushThinking()
		r.renderUserMessage(m)
	case *libagent.ToolResultMessage:
		if p, ok := r.pending[m.ToolCallID]; ok {
			label := r.renderToolLabelForResult(p.toolName, r.bestArgs(p), m.Content, m.Metadata)
			dur := time.Since(p.startTime)
			suffix := fmt.Sprintf(" (%s)", formatDuration(dur))
			if m.IsError {
				r.emitStatus(renderStatusError("✗"), label+suffix)
			} else {
				r.emitStatus(renderStatusSuccess("✓"), label+suffix)
			}
			delete(r.pending, m.ToolCallID)
		}
	case *libagent.AssistantMessage:
		r.flushReplyTail()
		r.flushThinking()
	}
}

func (r *renderer) renderUserMessage(m *libagent.UserMessage) {
	if m == nil {
		return
	}
	prefix := renderUserPrefix()

	text := strings.TrimSpace(m.Content)
	if text != "" {
		r.closePendingInlineForStdout()
		for _, line := range strings.Split(strings.TrimRight(text, "\n"), "\n") {
			fmt.Fprint(r.stdout, prefix)
			fmt.Fprint(r.stdout, strings.TrimRight(line, "\r"))
			fmt.Fprint(r.stdout, "\n")
		}
	}

	for _, f := range m.Files {
		name := strings.TrimSpace(f.Filename)
		if name == "" {
			name = "(unnamed)"
		}
		mime := strings.TrimSpace(f.MediaType)
		if mime != "" {
			fmt.Fprintf(r.stdout, "%s[attached] %s (%s)\n", prefix, name, mime)
		} else {
			fmt.Fprintf(r.stdout, "%s[attached] %s\n", prefix, name)
		}
	}
}

func (r *renderer) bestArgs(p *pendingLine) json.RawMessage {
	if p == nil {
		return nil
	}
	if len(p.args) > 0 && json.Valid(p.args) {
		return p.args
	}
	raw := strings.TrimSpace(p.rawInput.String())
	if raw == "" {
		return nil
	}
	rawArgs := json.RawMessage(raw)
	if !json.Valid(rawArgs) {
		return nil
	}
	return rawArgs
}

func (r *renderer) startThinking() {
	if r.thinking {
		return
	}
	r.thinking = true
	r.thinkingStart = time.Now()
	r.thinkingSeen = false
	r.thinkingLine.Reset()
	r.emitPending(renderStatusInfo("⟳"), "Thinking: ")
}

func (r *renderer) flushThinking() {
	r.flushThinkingTail()
	if !r.thinking {
		return
	}
	r.thinking = false
	r.thinkingSeen = false
	dur := time.Since(r.thinkingStart)
	r.emitStatus(renderStatusSuccess("✓"), fmt.Sprintf("Thinking (%s)", formatDuration(dur)))
}

func (r *renderer) finalize() {
	r.flushReplyTail()
	r.flushThinking()
	r.flushCompletedTools()
	// Finalize any remaining pending tools.
	for id, p := range r.pending {
		r.emitStatus(renderStatusError("✗"), p.label+" (cancelled)")
		delete(r.pending, id)
	}
}

func (r *renderer) flushCompletedTools() {
	for id, p := range r.pending {
		if !p.ended {
			continue
		}
		label := r.renderToolLabelForResult(p.toolName, r.bestArgs(p), p.endResult, "")
		dur := time.Since(p.startTime)
		suffix := fmt.Sprintf(" (%s)", formatDuration(dur))
		if p.endError {
			r.emitStatus(renderStatusError("✗"), label+suffix)
		} else {
			r.emitStatus(renderStatusSuccess("✓"), label+suffix)
		}
		delete(r.pending, id)
	}
}

func (r *renderer) appendReplyDelta(delta string) {
	if delta == "" {
		return
	}
	r.replyText.WriteString(delta)
	r.replyLine.WriteString(delta)
	r.flushBufferedLines(&r.replyLine, func(s string) string { return r.replyMD.RenderLine(s) })
}

func (r *renderer) appendThinkingDelta(delta string) {
	if delta == "" {
		return
	}
	if !r.thinkingSeen {
		delta = strings.TrimLeftFunc(delta, unicode.IsSpace)
	}
	if delta == "" {
		return
	}
	r.thinkingSeen = true
	r.thinkingLine.WriteString(delta)
	r.flushThinkingBufferedLines()
}

func (r *renderer) flushReplyTail() {
	r.flushBufferedTail(&r.replyLine, func(s string) string { return r.replyMD.RenderLine(s) })
}

func (r *renderer) flushThinkingTail() {
	if r.thinkingLine.Len() == 0 {
		return
	}
	tail := strings.TrimSpace(r.thinkingLine.String())
	r.thinkingLine.Reset()
	if tail == "" {
		return
	}
	r.writeThinkingLine(tail)
}

func (r *renderer) flushBufferedLines(buf *strings.Builder, render func(string) string) {
	if buf == nil || buf.Len() == 0 {
		return
	}
	content := buf.String()
	lastNL := strings.LastIndexByte(content, '\n')
	if lastNL < 0 {
		return
	}
	toWrite := content[:lastNL+1]
	rest := content[lastNL+1:]
	if toWrite != "" {
		r.closePendingInlineForStdout()
		for _, line := range strings.Split(strings.TrimSuffix(toWrite, "\n"), "\n") {
			fmt.Fprint(r.stdout, render(strings.TrimRight(line, "\r")))
			fmt.Fprint(r.stdout, "\n")
		}
	}
	buf.Reset()
	if rest != "" {
		buf.WriteString(rest)
	}
}

func (r *renderer) flushBufferedTail(buf *strings.Builder, render func(string) string) {
	if buf == nil || buf.Len() == 0 {
		return
	}
	tail := buf.String()
	buf.Reset()
	if tail == "" {
		return
	}
	tail = strings.TrimRight(tail, "\n")
	r.closePendingInlineForStdout()
	for _, line := range strings.Split(tail, "\n") {
		fmt.Fprint(r.stdout, render(strings.TrimRight(line, "\r")))
		fmt.Fprint(r.stdout, "\n")
	}
}

func (r *renderer) flushThinkingBufferedLines() {
	if r.thinkingLine.Len() == 0 {
		return
	}
	content := r.thinkingLine.String()
	lastNL := strings.LastIndexByte(content, '\n')
	if lastNL < 0 {
		return
	}
	complete := content[:lastNL+1]
	rest := content[lastNL+1:]
	r.thinkingLine.Reset()
	if rest != "" {
		r.thinkingLine.WriteString(rest)
	}
	trimmedComplete := strings.TrimSuffix(complete, "\n")
	for _, line := range strings.Split(trimmedComplete, "\n") {
		r.writeThinkingLine(line)
	}
}

func (r *renderer) writeThinkingLine(line string) {
	line = strings.TrimSpace(line)
	r.closePendingInlineForStdout()
	if line == "" {
		fmt.Fprint(r.stdout, "\n")
		return
	}
	fmt.Fprint(r.stdout, thinkingMutedStyle.Render(line))
	fmt.Fprint(r.stdout, "\n")
}

// FinalText returns the accumulated assistant text response.
func (r *renderer) FinalText() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return strings.TrimSpace(r.replyText.String())
}

// emitPending writes a pending (overwritable if TTY) status line.
func (r *renderer) emitPending(icon, text string) {
	ts := time.Now().Format("15:04:05")
	line := fmt.Sprintf("%s %s %s", icon, renderStatusTimestamp("["+ts+"]"), text)
	lineWidth := lipgloss.Width(line)
	if r.isTTY {
		pad := ""
		if r.pendingInline && r.pendingWidth > lineWidth {
			pad = strings.Repeat(" ", r.pendingWidth-lineWidth)
		}
		if r.pendingInline {
			fmt.Fprintf(r.stderr, "\r%s%s", line, pad)
		} else {
			fmt.Fprintf(r.stderr, "%s", line)
		}
		r.pendingInline = true
		r.pendingWidth = lineWidth
	} else {
		fmt.Fprintf(r.stderr, "%s\n", line)
		r.pendingInline = false
		r.pendingWidth = 0
	}
}

// emitStatus writes a finalized status line (always ends with newline).
func (r *renderer) emitStatus(icon, text string) {
	ts := time.Now().Format("15:04:05")
	line := fmt.Sprintf("%s %s %s", icon, renderStatusTimestamp("["+ts+"]"), text)
	lineWidth := lipgloss.Width(line)
	if r.isTTY {
		if r.pendingInline {
			pad := ""
			if r.pendingWidth > lineWidth {
				pad = strings.Repeat(" ", r.pendingWidth-lineWidth)
			}
			fmt.Fprintf(r.stderr, "\r%s%s\n", line, pad)
		} else {
			fmt.Fprintf(r.stderr, "%s\n", line)
		}
	} else {
		fmt.Fprintf(r.stderr, "%s\n", line)
	}
	r.pendingInline = false
	r.pendingWidth = 0
}

func (r *renderer) closePendingInlineForStdout() {
	if !r.isTTY || !r.pendingInline {
		return
	}
	fmt.Fprint(r.stderr, "\n")
	r.pendingInline = false
	r.pendingWidth = 0
}

func (r *renderer) renderToolLabel(name string, args json.RawMessage) string {
	return r.renderToolLabelWithOutput(name, args, "", "")
}

func (r *renderer) renderToolLabelForResult(name string, args json.RawMessage, output, metadata string) string {
	return r.renderToolLabelWithOutput(name, args, output, metadata)
}

func (r *renderer) renderToolLabelWithOutput(name string, args json.RawMessage, output, metadata string) string {
	includeOutput := shouldIncludeToolResultPreview(name)
	renderOutput := ""
	if includeOutput {
		renderOutput = output
	}

	tool := tools.FindTool(r.tools, name)
	if tool == nil {
		label := renderBoldText(name) + r.compactArgs(args)
		if includeOutput {
			if diff := tools.DiffFromMetadata(metadata); diff != "" {
				return label + "\n" + tools.RenderDiffText(diff)
			}
		}
		return label
	}
	argsForRender := args
	if len(argsForRender) == 0 {
		argsForRender = json.RawMessage("{}")
	}
	if rt, ok := tool.(tools.RenderableTool); ok && json.Valid(argsForRender) {
		rendered := rt.Render(argsForRender, renderOutput, 0)
		if first, rest, hasRest := strings.Cut(rendered, "\n"); strings.TrimSpace(first) != "" {
			label := renderBoldText(first)
			if includeOutput {
				if diff := tools.DiffFromMetadata(metadata); diff != "" {
					return label + "\n" + tools.RenderDiffText(diff)
				}
			}
			if includeOutput && hasRest {
				body := strings.TrimSpace(rest)
				if body != "" {
					return label + "\n" + body
				}
			}
			return label
		}
	}
	return renderBoldText(name) + r.compactArgs(args)
}

func shouldIncludeToolResultPreview(name string) bool {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "edit", "write", "create":
		return true
	default:
		return false
	}
}

func (r *renderer) compactArgs(args json.RawMessage) string {
	if len(args) == 0 || !json.Valid(args) {
		return ""
	}
	s := string(args)
	if len(s) > 80 {
		s = s[:77] + "..."
	}
	return renderDimText(" " + s)
}

func formatDuration(d time.Duration) string {
	if d < time.Second {
		return fmt.Sprintf("%dms", d.Milliseconds())
	}
	return fmt.Sprintf("%.1fs", d.Seconds())
}
