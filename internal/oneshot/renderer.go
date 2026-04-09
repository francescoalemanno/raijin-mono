package oneshot

import (
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/francescoalemanno/raijin-mono/internal/compaction"
	"github.com/francescoalemanno/raijin-mono/internal/tools"
	libagent "github.com/francescoalemanno/raijin-mono/libagent"
	"golang.org/x/term"
)

var thinkingMutedStyle = oneshotMutedStyle

const userPromptGlyph = "❯"

var spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

const defaultSpinnerInterval = 120 * time.Millisecond

var activeRendererStack struct {
	mu      sync.Mutex
	stack   []*renderer
	current *renderer
}

type rendererOptions struct {
	persistentSpinner bool
	deferSpinnerPaint bool
	now               func() time.Time
	spinnerInterval   time.Duration
	modelLabel        string
	contextWindow     int64
	initialMessages   []libagent.Message
	noThinking        bool
}

// pendingLine tracks a tool or thinking event currently in progress.
type pendingLine struct {
	id        string
	toolName  string
	label     string
	args      string
	rawInput  strings.Builder
	startTime time.Time
	executing bool
	ended     bool
	endResult string
	endError  bool
}

type spinnerPhase int

const (
	spinnerPhaseReasoning spinnerPhase = iota
	spinnerPhaseResponding
	spinnerPhaseTools
)

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
	pending          map[string]*pendingLine // tool call ID -> pending
	pendingInline    bool
	pendingWidth     int
	thinking         bool
	thinkingStart    time.Time
	thinkingLine     strings.Builder
	thinkingSeen     strings.Builder
	reasoningStarted bool // tracks if reasoning has started in current block (for first-delta trimming)
	replyMD          *lineMarkdownRenderer
	replyLine        strings.Builder
	replyText        strings.Builder
	replyStarted     bool // tracks if any reply content has been printed in current turn

	spinnerEnabled      bool
	spinnerVisible      bool
	spinnerWidth        int
	spinnerFrameIndex   int
	spinnerPhase        spinnerPhase
	spinnerLabel        string
	spinnerStateStart   time.Time
	nestedPendingInline bool
	nestedPendingWidth  int
	turnActive          bool
	replyStreaming      bool
	spinnerInterval     time.Duration
	spinnerStopCh       chan struct{}
	spinnerDoneCh       chan struct{}
	spinnerDeferred     bool
	modelLabel          string
	contextWindow       int64
	contextMessages     []libagent.Message
	interactiveDialogs  int
	noThinking          bool
}

func newRenderer(stderr, stdout io.Writer, agentTools []libagent.Tool, isTTY bool) *renderer {
	return newRendererWithOptions(stderr, stdout, agentTools, isTTY, rendererOptions{})
}

func pushActiveRenderer(r *renderer) func() {
	activeRendererStack.mu.Lock()
	activeRendererStack.stack = append(activeRendererStack.stack, r)
	activeRendererStack.current = r
	activeRendererStack.mu.Unlock()

	return func() {
		activeRendererStack.mu.Lock()
		defer activeRendererStack.mu.Unlock()
		if n := len(activeRendererStack.stack); n > 0 {
			activeRendererStack.stack = activeRendererStack.stack[:n-1]
		}
		if n := len(activeRendererStack.stack); n > 0 {
			activeRendererStack.current = activeRendererStack.stack[n-1]
			return
		}
		activeRendererStack.current = nil
	}
}

func beginCurrentRendererInteractiveDialog() func() {
	activeRendererStack.mu.Lock()
	r := activeRendererStack.current
	activeRendererStack.mu.Unlock()
	if r == nil {
		return func() {}
	}
	r.beginInteractiveDialog()
	return func() {
		r.endInteractiveDialog()
	}
}

func newRendererWithOptions(stderr, stdout io.Writer, agentTools []libagent.Tool, isTTY bool, opts rendererOptions) *renderer {
	if opts.now == nil {
		opts.now = time.Now
	}
	if opts.spinnerInterval <= 0 {
		opts.spinnerInterval = defaultSpinnerInterval
	}
	termWidth, _, err := term.GetSize(int(os.Stdout.Fd()))
	if err != nil || termWidth <= 0 {
		termWidth = defaultTableMaxWidth
	} else if termWidth > 4 {
		termWidth -= 2
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
		spinnerDeferred: isTTY && opts.persistentSpinner && opts.deferSpinnerPaint,
		modelLabel:      strings.TrimSpace(opts.modelLabel),
		contextWindow:     opts.contextWindow,
		contextMessages: append([]libagent.Message(nil), opts.initialMessages...),
		noThinking:      opts.noThinking,
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
		r.replyStarted = false
		r.redrawSpinnerLocked()

	case libagent.AgentEventTypeMessageStart:
		// No-op by design: rendering is driven by deltas and message_end.

	case libagent.AgentEventTypeMessageUpdate:
		r.onMessageUpdate(event)

	case libagent.AgentEventTypeToolExecutionStart:
		r.onToolStart(event.ToolCallID, event.ToolName, event.ToolArgs)

	case libagent.AgentEventTypeToolExecutionUpdate:
		r.onToolUpdate(event.ToolCallID, event.ToolName, event.ToolArgs, event.ToolResult)

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

	case libagent.AgentEventTypeContextCompaction:
		r.onContextCompaction(event)

	case libagent.AgentEventTypeAgentEnd:
		r.finalize()
	}
}

func (r *renderer) onContextCompaction(event libagent.AgentEvent) {
	r.activateSpinnerLocked()
	if event.ContextCompaction == nil {
		return
	}
	if event.ContextCompaction.Phase == libagent.ContextCompactionPhaseEnd {
		r.applyContextCompactionLocked(*event.ContextCompaction)
	}
	icon, text, ok := contextCompactionStatusParts(*event.ContextCompaction)
	if !ok {
		return
	}
	r.emitStatus(icon, text)
	r.redrawSpinnerLocked()
}

func (r *renderer) onMessageUpdate(event libagent.AgentEvent) {
	r.activateSpinnerLocked()
	delta := event.Delta
	if delta == nil {
		return
	}
	switch delta.Type {
	case "text_start":
		if r.thinking {
			r.flushThinking()
		}
	case "text_delta":
		if r.thinking {
			r.flushThinking()
		}
		r.replyStreaming = true
		r.appendReplyDelta(delta.Delta)
		r.redrawSpinnerLocked()
	case "text_end":
		r.replyStreaming = false
		r.flushReplyTail()
		r.redrawSpinnerLocked()
	case "reasoning_start":
		r.startThinking()
		r.appendThinkingDelta(delta.Delta)
	case "reasoning_delta":
		r.startThinking()
		r.appendThinkingDelta(delta.Delta)
	case "reasoning_end":
		if r.thinkingLine.Len() == 0 {
			r.flushThinking()
		} else {
			r.redrawSpinnerLocked()
		}
	case "tool_input_start":
		r.onToolPreviewStart(delta.ID, delta.ToolName, "")
	case "tool_input_delta":
		r.onToolInputDelta(delta.ID, delta.Delta)
	case "tool_input_end":
		// No-op: execution-start/end events cover lifecycle; keep output compact.
	}
}

func (r *renderer) onToolStart(id, name, input string) {
	r.activateSpinnerLocked()
	if p, ok := r.pending[id]; ok {
		p.executing = true
		if p.startTime.IsZero() {
			p.startTime = r.now()
		}
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
		executing: true,
	}
	if strings.TrimSpace(input) != "" {
		p.args = input
	}
	p.label = r.renderToolLabel(name, r.bestParams(p))
	r.pending[id] = p
	r.replyStreaming = false
	r.emitPending(renderStatusInfo("●"), p.label)
}

func (r *renderer) onToolPreviewStart(id, name, input string) {
	r.activateSpinnerLocked()
	if p, ok := r.pending[id]; ok {
		if strings.TrimSpace(input) != "" {
			p.args = input
		}
		r.updatePendingLabel(p)
		return
	}
	r.flushThinking()
	p := &pendingLine{
		id:       id,
		toolName: name,
	}
	if strings.TrimSpace(input) != "" {
		p.args = input
	}
	p.label = r.renderToolLabel(name, r.bestParams(p))
	r.pending[id] = p
	r.replyStreaming = false
	r.emitPending(renderStatusInfo("●"), p.label)
}

func (r *renderer) onToolUpdate(id, name, input, update string) {
	if p, ok := r.pending[id]; ok {
		if strings.TrimSpace(input) != "" {
			p.args = input
		}
		if strings.TrimSpace(update) != "" {
			r.emitNestedToolUpdate(update)
		}
		r.updatePendingLabel(p)
		return
	}
	if strings.TrimSpace(id) == "" {
		return
	}
	r.onToolStart(id, name, input)
	if strings.TrimSpace(update) != "" {
		r.emitNestedToolUpdate(update)
	}
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
		r.activateSpinnerLocked()
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
		r.reconcileAssistantReasoning(m)
		r.replyStreaming = false
		r.flushReplyTail()
		r.flushThinking()
		r.clearPreviewOnlyPendingTools()
	}
	r.redrawSpinnerLocked()
}

func (r *renderer) renderUserMessage(m *libagent.UserMessage) {
	if m == nil {
		return
	}
	prefix := renderUserPrefix()
	separator := renderUserSeparator()
	prepared := false
	prepare := func() {
		if prepared {
			return
		}
		r.prepareForStdoutLocked()
		prepared = true
	}
	defer func() {
		if prepared {
			r.restoreAfterStdoutLocked()
		}
	}()
	printedSeparator := false
	printSeparator := func() {
		if printedSeparator {
			return
		}
		prepare()
		fmt.Fprint(r.stdout, separator)
		fmt.Fprint(r.stdout, "\n")
		printedSeparator = true
	}

	text := strings.TrimSpace(m.Content)
	if text != "" {
		printSeparator()
		prepare()
		for _, line := range strings.Split(strings.TrimRight(text, "\n"), "\n") {
			fmt.Fprint(r.stdout, prefix)
			fmt.Fprint(r.stdout, strings.TrimRight(line, "\r"))
			fmt.Fprint(r.stdout, "\n")
		}
	}

	for _, f := range m.Files {
		printSeparator()
		name := strings.TrimSpace(f.Filename)
		if name == "" {
			name = "(unnamed)"
		}
		mime := strings.TrimSpace(f.MediaType)
		prepare()
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
	if r.noThinking {
		return
	}
	if r.thinking {
		return
	}
	r.thinking = true
	r.thinkingStart = r.now()
	r.reasoningStarted = false
	r.thinkingLine.Reset()
	r.thinkingSeen.Reset()
}

func (r *renderer) flushThinking() {
	if r.noThinking {
		return
	}
	r.flushThinkingTail()
	if !r.thinking {
		return
	}
	r.thinking = false
	dur := r.now().Sub(r.thinkingStart)
	r.emitStatus(renderStatusSuccess("✓"), fmt.Sprintf("Reasoning (%s)", formatDuration(dur)))
}

func (r *renderer) finalize() {
	r.flushReplyTail()
	r.flushThinking()
	r.flushCompletedTools()
	// Finalize any remaining pending tools.
	for id, p := range r.pending {
		if !p.executing {
			delete(r.pending, id)
			continue
		}
		r.emitStatus(renderStatusError("✗"), p.label+" (cancelled)")
		delete(r.pending, id)
	}
}

func (r *renderer) flushCompletedTools() {
	for id, p := range r.pending {
		if !p.executing || !p.ended {
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

func (r *renderer) clearPreviewOnlyPendingTools() {
	for id, p := range r.pending {
		if p.executing || p.ended {
			continue
		}
		delete(r.pending, id)
	}
}

func (r *renderer) appendReplyDelta(delta string) {
	if delta == "" {
		return
	}
	// Trim leading whitespace from the first response delta in each turn.
	if !r.replyStarted {
		delta = strings.TrimLeft(delta, " \t\n\r")
	}
	r.replyStarted = true
	r.replyText.WriteString(delta)
	r.replyLine.WriteString(delta)
	r.flushBufferedLines(&r.replyLine, func(s string) string { return r.replyMD.RenderLine(s) })
}

func (r *renderer) appendThinkingDelta(delta string) {
	if r.noThinking {
		return
	}
	if delta == "" {
		return
	}
	// Trim leading whitespace from the first reasoning delta in each block.
	if !r.reasoningStarted {
		delta = strings.TrimLeft(delta, " \t\n\r")
	}
	if delta == "" {
		return
	}
	r.reasoningStarted = true
	r.thinkingSeen.WriteString(delta)
	r.thinkingLine.WriteString(delta)
	r.flushThinkingBufferedLines()
}

func (r *renderer) reconcileAssistantReasoning(m *libagent.AssistantMessage) {
	if m == nil {
		return
	}
	finalReasoning := libagent.AssistantReasoning(m)
	if finalReasoning == "" {
		return
	}
	seen := r.thinkingSeen.String()
	if finalReasoning == seen {
		return
	}

	switch {
	case seen == "":
		r.startThinking()
		r.appendThinkingDelta(finalReasoning)
	case strings.HasPrefix(finalReasoning, seen):
		r.thinkingLine.WriteString(finalReasoning[len(seen):])
		r.thinkingSeen.WriteString(finalReasoning[len(seen):])
	default:
		return
	}

	if !r.thinking {
		r.flushThinkingTail()
	}
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
	tail = sanitizeReasoningTailForDisplay(tail)
	if tail == "" {
		return
	}
	r.writeThinkingLine(tail)
}

func sanitizeReasoningTailForDisplay(tail string) string {
	tail = strings.TrimRight(tail, "\r\n")
	if tail == "" {
		return ""
	}
	last := tail[len(tail)-1]
	if strings.ContainsRune(".!?:)]}\"'", rune(last)) {
		return tail
	}

	lastBoundary := -1
	for i := 0; i < len(tail); i++ {
		if strings.ContainsRune(".!?", rune(tail[i])) {
			lastBoundary = i
		}
	}
	if lastBoundary < 0 {
		return tail
	}
	return strings.TrimRight(tail[:lastBoundary+1], " \t")
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
	if r.noThinking {
		return
	}
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
	r.closeNestedPendingInlineLocked()
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
	r.closeNestedPendingInlineLocked()
	line := formatStatusLine(icon, text)
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

func formatStatusLine(icon, text string) string {
	ts := time.Now().Format("15:04:05")
	return fmt.Sprintf("%s %s %s", icon, renderStatusTimestamp("["+ts+"]"), text)
}

func contextCompactionStatusParts(ev libagent.ContextCompactionEvent) (icon, text string, ok bool) {
	switch ev.Phase {
	case libagent.ContextCompactionPhaseStart:
		icon = renderStatusInfo("●")
		if ev.Mode == libagent.ContextCompactionModeAuto {
			text = "Auto-compacting session"
		} else {
			text = "Compacting session"
		}
		if trigger := contextCompactionTriggerLabel(ev); trigger != "" {
			text += " (" + trigger + ")"
		}
		text += "…"
		return icon, text, true
	case libagent.ContextCompactionPhaseEnd:
		icon = renderStatusSuccess("✓")
		if ev.Mode == libagent.ContextCompactionModeAuto {
			text = fmt.Sprintf("Context auto-compacted: summarized %d messages, kept %d", ev.Summarized, ev.Kept)
		} else {
			text = fmt.Sprintf("Context compacted: summarized %d messages, kept %d", ev.Summarized, ev.Kept)
		}
		return icon, text, true
	case libagent.ContextCompactionPhaseFailed:
		icon = renderStatusWarning("●")
		if ev.Mode == libagent.ContextCompactionModeAuto {
			text = "Auto-compaction failed"
		} else {
			text = "Compaction failed"
		}
		if errMsg := strings.TrimSpace(ev.ErrorMessage); errMsg != "" {
			text += ": " + errMsg
		}
		return icon, text, true
	default:
		return "", "", false
	}
}

func contextCompactionTriggerLabel(ev libagent.ContextCompactionEvent) string {
	parts := make([]string, 0, 2)
	if ev.TriggerContextPercent > 0 {
		parts = append(parts, fmt.Sprintf("ctx %.1f%%", ev.TriggerContextPercent))
	}
	if ev.TriggerEstimatedTokens > 0 {
		parts = append(parts, fmt.Sprintf("%s estimated tokens", compaction.FormatTokenCount(ev.TriggerEstimatedTokens)))
	}
	return strings.Join(parts, ", ")
}

func (r *renderer) emitNestedToolUpdate(text string) {
	text = strings.TrimSpace(text)
	if text == "" {
		return
	}
	suspendedSpinner := r.suspendSpinnerLocked()
	if r.isTTY && r.pendingInline {
		fmt.Fprint(r.stderr, "\n")
	}
	r.pendingInline = false
	r.pendingWidth = 0
	for _, line := range strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n") {
		line = strings.TrimRight(line, "\r")
		if strings.TrimSpace(line) == "" {
			continue
		}
		rendered := oneshotMutedStyle.Render(line)
		switch classifyNestedRendererLine(line) {
		case nestedRendererPending:
			r.emitNestedPendingLocked(rendered)
		case nestedRendererFinal:
			r.emitNestedFinalLocked(rendered)
		default:
			r.closeNestedPendingInlineLocked()
			fmt.Fprintln(r.stderr, rendered)
		}
	}
	r.resumeSpinnerLocked(suspendedSpinner)
}

func (r *renderer) closePendingInlineForStdout() {
	if !r.isTTY || !r.pendingInline {
		r.closeNestedPendingInlineLocked()
		return
	}
	fmt.Fprint(r.stderr, "\n")
	r.pendingInline = false
	r.pendingWidth = 0
	r.closeNestedPendingInlineLocked()
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

	if !r.spinnerDeferred {
		r.spinnerStateStart = r.now()
		r.spinnerPhase = r.spinnerPhaseLocked()
		r.spinnerLabel = r.spinnerLabelLocked()
		r.redrawSpinnerLocked()
	}

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

func (r *renderer) activateSpinnerLocked() {
	if !r.spinnerEnabled || !r.spinnerStateStart.IsZero() {
		return
	}
	r.spinnerDeferred = false
	r.spinnerStateStart = r.now()
	r.spinnerPhase = r.spinnerPhaseLocked()
	r.spinnerLabel = r.spinnerLabelLocked()
	r.redrawSpinnerLocked()
}

func (r *renderer) spinnerLabelLocked() string {
	if len(r.pending) > 0 {
		return r.pendingToolsLabelLocked()
	}
	if r.thinking {
		return "Reasoning"
	}
	if r.replyStreaming {
		return "Responding"
	}
	if r.turnActive || r.spinnerEnabled {
		return "Reasoning"
	}
	return "Reasoning"
}

func (r *renderer) spinnerPhaseLocked() spinnerPhase {
	if len(r.pending) > 0 {
		return spinnerPhaseTools
	}
	if r.replyStreaming {
		return spinnerPhaseResponding
	}
	return spinnerPhaseReasoning
}

// pendingToolsLabelLocked generates a compact label for pending tools.
// Uses a short format to avoid line wrapping issues with long JSON params.
func (r *renderer) pendingToolsLabelLocked() string {
	if len(r.pending) == 0 {
		return "Tool calling"
	}

	// Collect pending tools with their info for stable sorting
	type pendingInfo struct {
		id     string
		name   string
		params string
	}
	pendingList := make([]pendingInfo, 0, len(r.pending))
	for id, p := range r.pending {
		if !p.ended {
			pendingList = append(pendingList, pendingInfo{
				id:     id,
				name:   p.toolName,
				params: r.bestParams(p),
			})
		}
	}

	if len(pendingList) == 0 {
		return "Tool calling"
	}

	// Sort by ID for deterministic ordering
	sort.Slice(pendingList, func(i, j int) bool {
		return pendingList[i].id < pendingList[j].id
	})

	// Calculate total parameter bytes
	totalBytes := 0
	for _, p := range pendingList {
		totalBytes += len(p.params)
	}

	// Build compact label
	if len(pendingList) == 1 {
		return fmt.Sprintf("%s (%s)", pendingList[0].name, formatByteSize(totalBytes))
	}
	return fmt.Sprintf("Tool calls (%d, %s)", len(pendingList), formatByteSize(totalBytes))
}

// formatByteSize returns a human-readable byte size string.
func formatByteSize(bytes int) string {
	switch {
	case bytes < 1024:
		return fmt.Sprintf("%d bytes", bytes)
	case bytes < 1024*1024:
		return fmt.Sprintf("%.1fKB", float64(bytes)/1024)
	default:
		return fmt.Sprintf("%.1fMB", float64(bytes)/(1024*1024))
	}
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
	if !r.spinnerEnabled || r.spinnerStateStart.IsZero() || r.nestedPendingInline || r.interactiveDialogs > 0 {
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

func (r *renderer) beginInteractiveDialog() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.interactiveDialogs++
	if r.interactiveDialogs == 1 {
		r.clearSpinnerLocked()
	}
}

func (r *renderer) endInteractiveDialog() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.interactiveDialogs == 0 {
		return
	}
	r.interactiveDialogs--
	if r.interactiveDialogs == 0 {
		r.redrawSpinnerLocked()
	}
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

type nestedRendererLineKind int

const (
	nestedRendererOther nestedRendererLineKind = iota
	nestedRendererPending
	nestedRendererFinal
)

func classifyNestedRendererLine(line string) nestedRendererLineKind {
	trimmed := strings.TrimSpace(line)
	switch {
	case strings.HasPrefix(trimmed, "│ ● "):
		return nestedRendererPending
	case strings.HasPrefix(trimmed, "│ ✓ "), strings.HasPrefix(trimmed, "│ ✗ "):
		return nestedRendererFinal
	default:
		return nestedRendererOther
	}
}

func (r *renderer) emitNestedPendingLocked(line string) {
	r.closeNestedPendingInlineLocked()
	width := lipgloss.Width(line)
	if r.isTTY {
		fmt.Fprint(r.stderr, line)
		r.nestedPendingInline = true
		r.nestedPendingWidth = width
		return
	}
	fmt.Fprintln(r.stderr, line)
}

func (r *renderer) emitNestedFinalLocked(line string) {
	width := lipgloss.Width(line)
	if r.isTTY && r.nestedPendingInline {
		pad := ""
		if r.nestedPendingWidth > width {
			pad = strings.Repeat(" ", r.nestedPendingWidth-width)
		}
		fmt.Fprintf(r.stderr, "\r%s%s\n", line, pad)
		r.nestedPendingInline = false
		r.nestedPendingWidth = 0
		return
	}
	fmt.Fprintln(r.stderr, line)
}

func (r *renderer) closeNestedPendingInlineLocked() {
	if !r.isTTY || !r.nestedPendingInline {
		return
	}
	fmt.Fprint(r.stderr, "\n")
	r.nestedPendingInline = false
	r.nestedPendingWidth = 0
}

func (r *renderer) updateSpinnerPhaseLocked() {
	phase := r.spinnerPhaseLocked()
	label := r.spinnerLabelLocked()
	if phase != r.spinnerPhase {
		r.spinnerPhase = phase
		r.spinnerStateStart = r.now()
	}
	r.spinnerLabel = label
}

func (r *renderer) spinnerContextLabelLocked() string {
	if r.contextWindow <= 0 {
		return "ctx ?"
	}
	usedTokens := compaction.ApproximateConversationUsageTokens(r.contextMessages)
	pct := float64(usedTokens) / float64(r.contextWindow) * 100
	return fmt.Sprintf("ctx %.1f%%", pct)
}

func (r *renderer) appendContextMessageLocked(msg libagent.Message) {
	if msg == nil {
		return
	}
	r.contextMessages = append(r.contextMessages, msg)
}

func (r *renderer) applyContextCompactionLocked(ev libagent.ContextCompactionEvent) {
	if ev.Kept < 0 {
		ev.Kept = 0
	}
	keptStart := len(r.contextMessages) - ev.Kept
	if keptStart < 0 {
		keptStart = 0
	}
	next := make([]libagent.Message, 0, 1+max(len(r.contextMessages)-keptStart, 0))
	next = append(next, &libagent.UserMessage{
		Role:      "user",
		Content:   compaction.CheckpointPrefix + "(summary checkpoint)",
		Timestamp: r.now(),
	})
	for _, msg := range r.contextMessages[keptStart:] {
		next = append(next, libagent.CloneMessage(msg))
	}
	r.contextMessages = next
}

func formatDuration(d time.Duration) string {
	if d < time.Second {
		return fmt.Sprintf("%dms", d.Milliseconds())
	}
	return fmt.Sprintf("%.1fs", d.Seconds())
}
