package oneshot

import (
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/francescoalemanno/raijin-mono/internal/tools"
	libagent "github.com/francescoalemanno/raijin-mono/libagent"
	"golang.org/x/term"
)

var thinkingMutedStyle = oneshotMutedStyle

const userPromptGlyph = "❯"

var spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

const defaultSpinnerInterval = 120 * time.Millisecond

type rendererOptions struct {
	persistentSpinner bool
	now               func() time.Time
	spinnerInterval   time.Duration
	modelLabel        string
	contextWindow     int64
	initialMessages   []libagent.Message
}

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
	now     func() time.Time
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

	spinnerEnabled    bool
	spinnerVisible    bool
	spinnerWidth      int
	spinnerFrameIndex int
	spinnerLabel      string
	spinnerStateStart time.Time
	turnActive        bool
	replyStreaming    bool
	spinnerInterval   time.Duration
	spinnerStopCh     chan struct{}
	spinnerDoneCh     chan struct{}
	modelLabel        string
	contextWindow     int64
	contextMessages   []libagent.Message
}

func newRenderer(stderr, stdout io.Writer, agentTools []libagent.Tool, isTTY bool) *renderer {
	return newRendererWithOptions(stderr, stdout, agentTools, isTTY, rendererOptions{})
}

func newRendererWithOptions(stderr, stdout io.Writer, agentTools []libagent.Tool, isTTY bool, opts rendererOptions) *renderer {
	if opts.now == nil {
		opts.now = time.Now
	}
	if opts.spinnerInterval <= 0 {
		opts.spinnerInterval = defaultSpinnerInterval
	}
	termWidth, _, err := term.GetSize(int(os.Stdout.Fd()))
	if err != nil || termWidth <= 0 || termWidth > defaultTableMaxWidth {
		termWidth = defaultTableMaxWidth
	}
	return &renderer{
		stderr:          stderr,
		stdout:          stdout,
		tools:           agentTools,
		isTTY:           isTTY,
		now:             opts.now,
		pending:         make(map[string]*pendingLine),
		replyMD:         newLineMarkdownRendererWithWidth(termWidth),
		spinnerEnabled:  isTTY && opts.persistentSpinner,
		spinnerInterval: opts.spinnerInterval,
		modelLabel:      strings.TrimSpace(opts.modelLabel),
		contextWindow:   opts.contextWindow,
		contextMessages: append([]libagent.Message(nil), opts.initialMessages...),
	}
}

func (r *renderer) handleEvent(event libagent.AgentEvent) {
	r.mu.Lock()
	defer r.mu.Unlock()

	switch event.Type {
	case libagent.AgentEventTypeAgentStart:
		r.started = true

	case libagent.AgentEventTypeTurnStart:
		r.turnActive = true
		r.replyStreaming = false
		r.redrawSpinnerLocked()

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
		r.turnActive = false
		r.replyStreaming = false
		r.flushCompletedTools()
		r.redrawSpinnerLocked()

	case libagent.AgentEventTypeRetry:
		if event.RetryMessage != "" {
			r.emitStatus(renderStatusWarning("↻"), event.RetryMessage)
		}
		r.redrawSpinnerLocked()

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
		r.replyStreaming = true
		r.appendReplyDelta(delta.Delta)
		r.redrawSpinnerLocked()
	case "text_end":
		r.replyStreaming = false
		r.flushReplyTail()
		r.redrawSpinnerLocked()
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
		startTime: r.now(),
	}
	if strings.TrimSpace(input) != "" {
		p.args = input
	}
	p.label = r.renderToolLabel(name, r.bestParams(p))
	r.pending[id] = p
	r.replyStreaming = false
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
	r.redrawSpinnerLocked()
}

func (r *renderer) onMessageEnd(event libagent.AgentEvent) {
	switch m := event.Message.(type) {
	case *libagent.UserMessage:
		r.appendContextMessageLocked(m)
		r.flushReplyTail()
		r.flushThinking()
		r.renderUserMessage(m)
	case *libagent.ToolResultMessage:
		r.appendContextMessageLocked(m)
		if p, ok := r.pending[m.ToolCallID]; ok {
			label := r.renderToolLabelForResult(p.toolName, r.bestParams(p), m.Content, m.Metadata)
			dur := r.now().Sub(p.startTime)
			suffix := fmt.Sprintf(" (%s)", formatDuration(dur))
			if m.IsError {
				r.emitStatus(renderStatusError("✗"), label+suffix)
			} else {
				r.emitStatus(renderStatusSuccess("✓"), label+suffix)
			}
			delete(r.pending, m.ToolCallID)
		}
	case *libagent.AssistantMessage:
		r.appendContextMessageLocked(m)
		r.replyStreaming = false
		r.flushReplyTail()
		r.flushThinking()
	}
	r.redrawSpinnerLocked()
}

func (r *renderer) renderUserMessage(m *libagent.UserMessage) {
	if m == nil {
		return
	}
	prefix := renderUserPrefix()

	text := strings.TrimSpace(m.Content)
	if text != "" {
		r.prepareForStdoutLocked()
		for _, line := range strings.Split(strings.TrimRight(text, "\n"), "\n") {
			fmt.Fprint(r.stdout, prefix)
			fmt.Fprint(r.stdout, strings.TrimRight(line, "\r"))
			fmt.Fprint(r.stdout, "\n")
		}
		r.restoreAfterStdoutLocked()
	}

	for _, f := range m.Files {
		name := strings.TrimSpace(f.Filename)
		if name == "" {
			name = "(unnamed)"
		}
		mime := strings.TrimSpace(f.MediaType)
		r.prepareForStdoutLocked()
		if mime != "" {
			fmt.Fprintf(r.stdout, "%s[attached] %s (%s)\n", prefix, name, mime)
		} else {
			fmt.Fprintf(r.stdout, "%s[attached] %s\n", prefix, name)
		}
		r.restoreAfterStdoutLocked()
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
	r.thinkingStart = r.now()
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
	dur := r.now().Sub(r.thinkingStart)
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
		dur := r.now().Sub(p.startTime)
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
	r.thinkingSeen = true
	r.thinkingLine.WriteString(delta)
	r.flushThinkingBufferedLines()
}

func (r *renderer) flushReplyTail() {
	r.flushBufferedTail(&r.replyLine, func(s string) string { return r.replyMD.RenderLine(s) })
	if trailing := r.replyMD.FlushTable(); trailing != "" {
		r.prepareForStdoutLocked()
		fmt.Fprint(r.stdout, trailing)
		fmt.Fprint(r.stdout, "\n")
		r.restoreAfterStdoutLocked()
	}
}

func (r *renderer) flushThinkingTail() {
	if r.thinkingLine.Len() == 0 {
		return
	}
	tail := r.thinkingLine.String()
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
		r.prepareForStdoutLocked()
		for _, line := range strings.Split(strings.TrimSuffix(toWrite, "\n"), "\n") {
			rendered := render(strings.TrimRight(line, "\r"))
			if rendered == "" {
				continue
			}
			fmt.Fprint(r.stdout, rendered)
			fmt.Fprint(r.stdout, "\n")
		}
		r.restoreAfterStdoutLocked()
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
	r.prepareForStdoutLocked()
	for _, line := range strings.Split(tail, "\n") {
		rendered := render(strings.TrimRight(line, "\r"))
		if rendered == "" {
			continue
		}
		fmt.Fprint(r.stdout, rendered)
		fmt.Fprint(r.stdout, "\n")
	}
	r.restoreAfterStdoutLocked()
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
	line = strings.TrimRight(line, "\r")
	r.prepareForStdoutLocked()
	if line == "" {
		fmt.Fprint(r.stdout, "\n")
		r.restoreAfterStdoutLocked()
		return
	}
	fmt.Fprint(r.stdout, thinkingMutedStyle.Render(line))
	fmt.Fprint(r.stdout, "\n")
	r.restoreAfterStdoutLocked()
}

// FinalText returns the accumulated assistant text response.
func (r *renderer) FinalText() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return strings.TrimSpace(r.replyText.String())
}

// emitPending writes a pending (overwritable if TTY) status line.
func (r *renderer) emitPending(icon, text string) {
	if r.spinnerEnabled {
		r.redrawSpinnerLocked()
		return
	}
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
	suspendedSpinner := r.suspendSpinnerLocked()
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
	r.resumeSpinnerLocked(suspendedSpinner)
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

func (r *renderer) startPersistentSpinner() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.spinnerEnabled || !r.spinnerStateStart.IsZero() {
		return
	}

	r.spinnerStateStart = r.now()
	r.spinnerLabel = r.spinnerLabelLocked()
	r.redrawSpinnerLocked()

	stopCh := make(chan struct{})
	doneCh := make(chan struct{})
	r.spinnerStopCh = stopCh
	r.spinnerDoneCh = doneCh
	interval := r.spinnerInterval
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		defer close(doneCh)
		for {
			select {
			case <-stopCh:
				return
			case <-ticker.C:
				r.mu.Lock()
				if !r.spinnerEnabled || r.spinnerStopCh != stopCh {
					r.mu.Unlock()
					return
				}
				r.spinnerFrameIndex = (r.spinnerFrameIndex + 1) % len(spinnerFrames)
				r.redrawSpinnerLocked()
				r.mu.Unlock()
			}
		}
	}()
}

func (r *renderer) stopPersistentSpinner() {
	r.mu.Lock()
	if !r.spinnerEnabled {
		r.mu.Unlock()
		return
	}
	r.spinnerEnabled = false
	stopCh := r.spinnerStopCh
	doneCh := r.spinnerDoneCh
	r.spinnerStopCh = nil
	r.spinnerDoneCh = nil
	r.clearSpinnerLocked()
	r.mu.Unlock()

	if stopCh != nil {
		close(stopCh)
	}
	if doneCh != nil {
		<-doneCh
	}
}

func (r *renderer) spinnerLabelLocked() string {
	if len(r.pending) > 0 {
		return "Tool calling"
	}
	if r.thinking {
		return "Thinking"
	}
	if r.replyStreaming {
		return "Responding"
	}
	if r.turnActive || r.spinnerEnabled {
		return "Thinking"
	}
	return "Thinking"
}

func (r *renderer) spinnerElapsedLocked() string {
	if r.spinnerStateStart.IsZero() {
		return "0.00s"
	}
	elapsed := r.now().Sub(r.spinnerStateStart)
	if elapsed < 0 {
		elapsed = 0
	}
	return fmt.Sprintf("%.2fs", elapsed.Seconds())
}

func (r *renderer) spinnerLineLocked() string {
	r.updateSpinnerPhaseLocked()
	frame := spinnerFrames[r.spinnerFrameIndex%len(spinnerFrames)]
	parts := []string{
		renderStatusInfo(frame),
		oneshotNormalStyle.Render(r.spinnerLabel),
		renderDimText(r.spinnerElapsedLocked()),
	}
	if r.modelLabel != "" {
		parts = append(parts, RenderThemedModel(r.modelLabel))
	}
	if ctxLabel := r.spinnerContextLabelLocked(); ctxLabel != "" {
		parts = append(parts, renderDimText(ctxLabel))
	}
	return strings.Join(parts, " ")
}

func (r *renderer) redrawSpinnerLocked() {
	if !r.spinnerEnabled || r.spinnerStateStart.IsZero() {
		return
	}
	line := r.spinnerLineLocked()
	lineWidth := lipgloss.Width(line)
	pad := ""
	if r.spinnerVisible && r.spinnerWidth > lineWidth {
		pad = strings.Repeat(" ", r.spinnerWidth-lineWidth)
	}
	if r.spinnerVisible {
		fmt.Fprintf(r.stderr, "\r%s%s", line, pad)
	} else {
		fmt.Fprint(r.stderr, line)
	}
	r.spinnerVisible = true
	r.spinnerWidth = lineWidth
}

func (r *renderer) clearSpinnerLocked() {
	if !r.spinnerVisible {
		return
	}
	fmt.Fprintf(r.stderr, "\r%s\r", strings.Repeat(" ", r.spinnerWidth))
	r.spinnerVisible = false
	r.spinnerWidth = 0
}

func (r *renderer) suspendSpinnerLocked() bool {
	if !r.spinnerEnabled || !r.spinnerVisible {
		return false
	}
	r.clearSpinnerLocked()
	return true
}

func (r *renderer) resumeSpinnerLocked(suspended bool) {
	if !suspended {
		return
	}
	r.redrawSpinnerLocked()
}

func (r *renderer) prepareForStdoutLocked() {
	_ = r.suspendSpinnerLocked()
	r.closePendingInlineForStdout()
}

func (r *renderer) restoreAfterStdoutLocked() {
	r.redrawSpinnerLocked()
}

func (r *renderer) updateSpinnerPhaseLocked() {
	label := r.spinnerLabelLocked()
	if label == r.spinnerLabel {
		return
	}
	r.spinnerLabel = label
	r.spinnerStateStart = r.now()
}

func (r *renderer) spinnerContextLabelLocked() string {
	if r.contextWindow <= 0 {
		return "ctx ?"
	}
	estimatedTokens := int64(2400) + estimateConversationTokens(r.contextMessages)
	usageTokens := latestAssistantUsageTokens(r.contextMessages)
	usedTokens := max(estimatedTokens, usageTokens)
	pct := float64(usedTokens) / float64(r.contextWindow) * 100
	return fmt.Sprintf("ctx %.1f%%", pct)
}

func (r *renderer) appendContextMessageLocked(msg libagent.Message) {
	if msg == nil {
		return
	}
	r.contextMessages = append(r.contextMessages, msg)
}

func formatDuration(d time.Duration) string {
	if d < time.Second {
		return fmt.Sprintf("%dms", d.Milliseconds())
	}
	return fmt.Sprintf("%.1fs", d.Seconds())
}
