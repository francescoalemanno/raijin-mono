package oneshot

import (
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
	args      string
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
			p.args = input
		}
		r.updatePendingLabel(p)
		return
	}
	r.flushThinking()
	p := &pendingLine{
		id:        id,
		toolName:  name,
		startTime: time.Now(),
	}
	if strings.TrimSpace(input) != "" {
		p.args = input
	}
	p.label = r.renderToolLabel(name, r.bestParams(p))
	r.pending[id] = p
	r.emitPending(renderStatusInfo("●"), p.label)
}

func (r *renderer) onToolUpdate(id, name, input string) {
	if p, ok := r.pending[id]; ok {
		if strings.TrimSpace(input) != "" {
			p.args = input
		}
		r.updatePendingLabel(p)
		return
	}
	if strings.TrimSpace(id) == "" {
		return
	}
	r.onToolStart(id, name, input)
}

func (r *renderer) onToolInputDelta(id, delta string) {
	if p, ok := r.pending[id]; ok {
		p.rawInput.WriteString(delta)
		r.updatePendingLabel(p)
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
		p.args = input
	}
	p.label = r.renderToolLabel(p.toolName, r.bestParams(p))
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
			label := r.renderToolLabelForResult(p.toolName, r.bestParams(p), m.Content, m.Metadata)
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

func (r *renderer) bestParams(p *pendingLine) string {
	if p == nil {
		return ""
	}
	if strings.TrimSpace(p.args) != "" {
		return p.args
	}
	return p.rawInput.String()
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
		label := r.renderToolLabelForResult(p.toolName, r.bestParams(p), p.endResult, "")
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

func (r *renderer) renderToolLabel(name, params string) string {
	return r.renderToolLabelWithOutput(name, params, "", "")
}

func (r *renderer) renderToolLabelForResult(name, params, output, metadata string) string {
	return r.renderToolLabelWithOutput(name, params, output, metadata)
}

func (r *renderer) renderToolLabelWithOutput(name, params, output, metadata string) string {
	tool := tools.FindTool(r.tools, name)
	if wrapped, ok := tool.(tools.WrappedTool); ok {
		if strings.TrimSpace(output) == "" && strings.TrimSpace(metadata) == "" {
			return wrapped.SingleLinePreview(params)
		}
		return wrapped.FinalRender(params, output, metadata)
	}
	return tools.RenderGenericSingleLinePreview(name, params)
}

func (r *renderer) updatePendingLabel(p *pendingLine) {
	if p == nil {
		return
	}
	next := r.renderToolLabel(p.toolName, r.bestParams(p))
	if next == p.label {
		return
	}
	p.label = next
	if r.isTTY {
		r.emitPending(renderStatusInfo("●"), p.label)
	}
}

func formatDuration(d time.Duration) string {
	if d < time.Second {
		return fmt.Sprintf("%dms", d.Milliseconds())
	}
	return fmt.Sprintf("%.1fs", d.Seconds())
}
