package tools

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"

	"github.com/francescoalemanno/raijin-mono/internal/subagents"
	"github.com/francescoalemanno/raijin-mono/libagent"
	"golang.org/x/term"
)

type subagentParams struct {
	Profile string `json:"profile" description:"Subagent profile name to run"`
	Message string `json:"message" description:"User message to send to the subagent"`
}

const subagentNestPrefix = "│ "

type subagentUpdateFn func(string)

type subagentTool struct {
	runtime RuntimeAccessor
	info    libagent.ToolInfo
}

func (t *subagentTool) Info() libagent.ToolInfo { return t.info }

func (t *subagentTool) Run(ctx context.Context, call libagent.ToolCall) (libagent.ToolResponse, error) {
	params, err := parseSubagentParams(call.Input)
	if err != nil {
		return libagent.NewTextErrorResponse(err.Error()), nil
	}
	output, err := ExecuteSubagent(ctx, t.runtime, params.Profile, params.Message, nil)
	if err != nil {
		return libagent.NewTextErrorResponse(err.Error()), nil
	}
	return libagent.NewTextResponse(output), nil
}

func (t *subagentTool) RunStreaming(ctx context.Context, call libagent.ToolCall, onUpdate func(libagent.ToolResponse)) (libagent.ToolResponse, error) {
	params, err := parseSubagentParams(call.Input)
	if err != nil {
		return libagent.NewTextErrorResponse(err.Error()), nil
	}
	output, err := ExecuteSubagent(ctx, t.runtime, params.Profile, params.Message, func(line string) {
		if strings.TrimSpace(line) == "" {
			return
		}
		onUpdate(libagent.NewTextResponse(line))
	})
	if err != nil {
		return libagent.NewTextErrorResponse(err.Error()), nil
	}
	return libagent.NewTextResponse(output), nil
}

// ExecuteSubagent runs a named subagent profile against the current runtime.
func ExecuteSubagent(ctx context.Context, runtime RuntimeAccessor, profileName, message string, onUpdate subagentUpdateFn) (string, error) {
	if runtime == nil {
		return "", fmt.Errorf("subagent runtime is unavailable")
	}

	profileName = strings.TrimSpace(profileName)
	message = strings.TrimSpace(message)
	if profileName == "" {
		return "", fmt.Errorf("profile is required")
	}
	if message == "" {
		return "", fmt.Errorf("message is required")
	}

	profile, ok := subagents.Find(profileName)
	if !ok {
		return "", fmt.Errorf("unknown subagent profile %q", profileName)
	}

	nestedTools, err := subagentTools(profile, runtime.Tools())
	if err != nil {
		return "", err
	}

	model := runtime.Model()
	if model.Model == nil {
		return "", fmt.Errorf("llm runtime is not configured")
	}

	maxOut := subagentMaxOutputTokens(model)
	providerOpts := model.BuildCallProviderOptions(profile.Prompt)
	if libagent.SkipMaxOutputTokens(model.ModelCfg.Provider) {
		maxOut = 0
	}

	agent := libagent.NewAgent(libagent.AgentOptions{
		RuntimeModel:    model,
		SystemPrompt:    profile.Prompt,
		Tools:           nestedTools,
		ProviderOptions: providerOpts,
		MaxOutputTokens: maxOut,
	})
	if onUpdate != nil {
		evCh, unsub := agent.Subscribe()
		defer unsub()
		done := make(chan struct{})
		emitter := newSubagentEmitter(onUpdate, nestedTools)
		go func() {
			defer close(done)
			emitter.consume(evCh)
		}()
		defer func() {
			<-done
		}()
	}
	if err := agent.Prompt(ctx, message); err != nil {
		return "", err
	}

	state := agent.State()
	for i := len(state.Messages) - 1; i >= 0; i-- {
		assistant, ok := state.Messages[i].(*libagent.AssistantMessage)
		if !ok {
			continue
		}
		text := strings.TrimSpace(libagent.AssistantText(assistant))
		if text == "" {
			break
		}
		return text, nil
	}
	return "", fmt.Errorf("subagent %q returned empty output", profile.Name)
}

func NewSubagentTool(runtime RuntimeAccessor) libagent.Tool {
	return &subagentTool{
		runtime: runtime,
		info: libagent.ToolInfo{
			Name:        "subagent",
			Description: "Run a named subagent profile with an isolated nested agent using the current session model. The profile defines the system prompt and allowed tools.",
			Parameters: map[string]any{
				"profile": map[string]any{
					"type":        "string",
					"description": "Subagent profile name to run",
				},
				"message": map[string]any{
					"type":        "string",
					"description": "User message to send to the subagent",
				},
			},
			Required: []string{"profile", "message"},
		},
	}
}

func subagentTools(profile subagents.Subagent, available []libagent.Tool) ([]libagent.Tool, error) {
	if len(profile.Tools) == 0 {
		return nil, nil
	}

	var (
		selected []libagent.Tool
		invalid  []string
	)
	for _, name := range profile.Tools {
		if name == "subagent" {
			return nil, fmt.Errorf("subagent profile %q cannot whitelist the subagent tool", profile.Name)
		}
		tool := FindTool(available, name)
		if tool == nil {
			invalid = append(invalid, name)
			continue
		}
		selected = append(selected, tool)
	}
	if len(invalid) > 0 {
		return nil, fmt.Errorf("subagent profile %q references unknown tools: %s", profile.Name, strings.Join(invalid, ", "))
	}
	return selected, nil
}

func subagentMaxOutputTokens(model libagent.RuntimeModel) int64 {
	contextWindow := model.EffectiveContextWindow()
	if contextWindow == 0 {
		contextWindow = libagent.DefaultContextWindow
	}
	maxOut := model.ModelCfg.MaxTokens
	if maxOut <= 0 {
		maxOut = libagent.DefaultMaxTokens
	}
	if contextWindow > 0 && maxOut >= contextWindow {
		maxOut = max(contextWindow/2, 1)
	}
	return maxOut
}

func parseSubagentParams(input string) (subagentParams, error) {
	var params subagentParams
	raw := strings.TrimSpace(input)
	if raw == "" {
		raw = "{}"
	}
	if err := libagent.ParseJSONInput([]byte(raw), &params); err != nil {
		return subagentParams{}, fmt.Errorf("invalid tool input: %v", err)
	}
	return params, nil
}

type subagentEmitter struct {
	onUpdate subagentUpdateFn
	tools    []libagent.Tool
	pending  map[string]subagentPendingTool
}

type subagentPendingTool struct {
	name string
	args string
}

func newSubagentEmitter(onUpdate subagentUpdateFn, tools []libagent.Tool) *subagentEmitter {
	return &subagentEmitter{
		onUpdate: onUpdate,
		tools:    tools,
		pending:  make(map[string]subagentPendingTool),
	}
}

func (e *subagentEmitter) consume(events <-chan libagent.AgentEvent) {
	for event := range events {
		e.handle(event)
	}
}

func (e *subagentEmitter) handle(event libagent.AgentEvent) {
	if e == nil || e.onUpdate == nil {
		return
	}
	switch event.Type {
	case libagent.AgentEventTypeToolExecutionStart:
		e.pending[event.ToolCallID] = subagentPendingTool{name: event.ToolName, args: event.ToolArgs}
		e.emitLine("● " + renderNestedToolLabel(event.ToolName, event.ToolArgs, e.tools))
	case libagent.AgentEventTypeToolExecutionEnd:
		pending := e.pending[event.ToolCallID]
		if strings.TrimSpace(pending.name) == "" {
			pending = subagentPendingTool{name: event.ToolName, args: event.ToolArgs}
		}
		icon := "✓ "
		if event.ToolIsError {
			icon = "✗ "
		}
		e.emitLine(icon + renderNestedToolLabel(pending.name, pending.args, e.tools))
		delete(e.pending, event.ToolCallID)
	case libagent.AgentEventTypeRetry:
		if strings.TrimSpace(event.RetryMessage) != "" {
			e.emitLine("↻ " + strings.TrimSpace(event.RetryMessage))
		}
	}
}

func (e *subagentEmitter) emitLine(line string) {
	if e == nil || e.onUpdate == nil {
		return
	}
	line = strings.TrimSpace(line)
	if line == "" {
		return
	}
	e.onUpdate(subagentNestPrefix + line)
}

func renderNestedToolLabel(name, params string, available []libagent.Tool) string {
	tool := FindTool(available, name)
	if wrapped, ok := tool.(WrappedTool); ok {
		return wrapped.SingleLinePreview(params)
	}
	return RenderGenericSingleLinePreview(name, params)
}

func WriteSubagentUpdates(w io.Writer) subagentUpdateFn {
	renderer := newSubagentLineWriter(w)
	return func(line string) {
		renderer.WriteLine(line)
	}
}

type subagentLineWriter struct {
	mu      sync.Mutex
	w       io.Writer
	isTTY   bool
	inline  bool
	width   int
	pending string
}

func newSubagentLineWriter(w io.Writer) *subagentLineWriter {
	return &subagentLineWriter{
		w:     w,
		isTTY: writerIsTTY(w),
	}
}

func (w *subagentLineWriter) WriteLine(line string) {
	if w == nil || w.w == nil {
		return
	}
	line = strings.TrimRight(strings.ReplaceAll(line, "\r\n", "\n"), "\n")
	if strings.TrimSpace(line) == "" {
		return
	}

	w.mu.Lock()
	defer w.mu.Unlock()

	switch classifyNestedLine(line) {
	case nestedLinePending:
		if !w.isTTY {
			_, _ = fmt.Fprintln(w.w, line)
			return
		}
		w.writeInline(line)
	case nestedLineFinal:
		if w.isTTY && w.inline {
			w.writeFinal(line)
			return
		}
		_, _ = fmt.Fprintln(w.w, line)
	default:
		if w.isTTY && w.inline {
			_, _ = fmt.Fprint(w.w, "\n")
			w.inline = false
			w.width = 0
			w.pending = ""
		}
		_, _ = fmt.Fprintln(w.w, line)
	}
}

func (w *subagentLineWriter) writeInline(line string) {
	width := visibleWidth(line)
	pad := ""
	if w.inline && w.width > width {
		pad = strings.Repeat(" ", w.width-width)
	}
	if w.inline {
		_, _ = fmt.Fprintf(w.w, "\r%s%s", line, pad)
	} else {
		_, _ = fmt.Fprint(w.w, line)
	}
	w.inline = true
	w.width = width
	w.pending = line
}

func (w *subagentLineWriter) writeFinal(line string) {
	width := visibleWidth(line)
	pad := ""
	if w.width > width {
		pad = strings.Repeat(" ", w.width-width)
	}
	_, _ = fmt.Fprintf(w.w, "\r%s%s\n", line, pad)
	w.inline = false
	w.width = 0
	w.pending = ""
}

type nestedLineKind int

const (
	nestedLineOther nestedLineKind = iota
	nestedLinePending
	nestedLineFinal
)

func classifyNestedLine(line string) nestedLineKind {
	trimmed := strings.TrimSpace(line)
	switch {
	case strings.HasPrefix(trimmed, subagentNestPrefix+"● "):
		return nestedLinePending
	case strings.HasPrefix(trimmed, subagentNestPrefix+"✓ "), strings.HasPrefix(trimmed, subagentNestPrefix+"✗ "):
		return nestedLineFinal
	default:
		return nestedLineOther
	}
}

func writerIsTTY(w io.Writer) bool {
	file, ok := w.(*os.File)
	if !ok {
		return false
	}
	return term.IsTerminal(int(file.Fd()))
}

func visibleWidth(line string) int {
	return len([]rune(line))
}
