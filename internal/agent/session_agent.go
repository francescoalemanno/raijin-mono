package agent

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/francescoalemanno/raijin-mono/internal/core"
	"github.com/francescoalemanno/raijin-mono/internal/message"
	"github.com/francescoalemanno/raijin-mono/internal/session"
	"github.com/francescoalemanno/raijin-mono/internal/skills"
	"github.com/francescoalemanno/raijin-mono/internal/tools"
	"github.com/francescoalemanno/raijin-mono/llmbridge/pkg/catalog"
	"github.com/francescoalemanno/raijin-mono/llmbridge/pkg/codec"
	bridgecfg "github.com/francescoalemanno/raijin-mono/llmbridge/pkg/config"
	"github.com/francescoalemanno/raijin-mono/llmbridge/pkg/llm"
)

const (
	largeContextWindowThreshold int64 = 200_000
	largeContextWindowBuffer    int64 = 20_000
	smallContextWindowRatio           = 0.2
)

// SessionAgentCall represents a request to run the agent.
type SessionAgentCall struct {
	SessionID        string
	Prompt           string
	Attachments      []message.BinaryContent
	Skills           []message.SkillContent
	AllowedTools     []string
	MaxOutputTokens  int64
	Temperature      *float64
	TopP             *float64
	TopK             *int64
	OnMessageCreated func(messageID string)
}

// SessionAgent orchestrates LLM interactions with proper message and session management.
type SessionAgent struct {
	mu            sync.RWMutex
	model         bridgecfg.RuntimeModel
	systemPrompt  string
	tools         []llm.Tool
	eventCallback core.AgentEventCallback

	activeRequests map[string]context.CancelFunc
	stopRequests   map[string]bool

	messages message.Service
	sessions session.Service

	modelSupportsImagesFn func(context.Context, string, string) (supports bool, known bool, err error)
}

// SessionAgentOptions configures a new SessionAgent.
type SessionAgentOptions struct {
	Model        bridgecfg.RuntimeModel
	SystemPrompt string
	Tools        []llm.Tool
	Messages     message.Service
	Sessions     session.Service
}

// NewSessionAgent creates a new SessionAgent with services.
func NewSessionAgent(opts SessionAgentOptions) *SessionAgent {
	tools := make([]llm.Tool, len(opts.Tools))
	copy(tools, opts.Tools)
	return &SessionAgent{
		model:                 opts.Model,
		systemPrompt:          opts.SystemPrompt,
		tools:                 tools,
		messages:              opts.Messages,
		sessions:              opts.Sessions,
		activeRequests:        make(map[string]context.CancelFunc),
		stopRequests:          make(map[string]bool),
		modelSupportsImagesFn: lookupModelSupportsImages,
	}
}

// SetEventCallback sets the callback for agent events.
func (a *SessionAgent) SetEventCallback(cb core.AgentEventCallback) {
	a.mu.Lock()
	a.eventCallback = cb
	a.mu.Unlock()
}

// EventCallback returns the current event callback.
func (a *SessionAgent) EventCallback() core.AgentEventCallback {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.eventCallback
}

// Run executes the agent with a user message.
// Messages are stored via the message service; session usage is tracked via session service.
func (a *SessionAgent) Run(ctx context.Context, call SessionAgentCall) (*llm.RunResult, error) {
	if call.Prompt == "" && len(call.Attachments) == 0 && len(call.Skills) == 0 {
		return nil, ErrEmptyPrompt
	}
	if call.SessionID == "" {
		return nil, ErrSessionMissing
	}

	// Copy mutable fields to avoid races
	a.mu.RLock()
	agentTools := make([]llm.Tool, len(a.tools))
	copy(agentTools, a.tools)
	model := a.model
	systemPrompt := a.systemPrompt
	a.mu.RUnlock()
	allowedTools := core.DedupeSorted(call.AllowedTools)
	if model.Runtime == nil {
		return nil, errors.New("llm runtime is not configured")
	}
	if err := a.validateImageAttachments(ctx, model, call.Attachments); err != nil {
		return nil, err
	}
	supportsImages, imageSupportKnown := a.resolveModelImageCapability(ctx, model)
	agentTools = adaptToolsForImageCapability(agentTools, supportsImages, imageSupportKnown)

	// Verify session exists
	if _, err := a.sessions.Get(ctx, call.SessionID); err != nil {
		return nil, fmt.Errorf("failed to get session: %w", err)
	}

	// Get existing messages from service
	msgs, err := a.messages.List(ctx, call.SessionID)
	if err != nil {
		return nil, fmt.Errorf("failed to list messages: %w", err)
	}

	// Create user message via service
	userMsg, err := a.createUserMessage(ctx, call)
	if err != nil {
		return nil, err
	}

	if call.OnMessageCreated != nil {
		call.OnMessageCreated(userMsg.ID)
	}

	// Set up cancellation
	genCtx, cancel := context.WithCancel(ctx)
	genCtx = tools.WithAllowedTools(genCtx, allowedTools)
	a.mu.Lock()
	a.activeRequests[call.SessionID] = cancel
	delete(a.stopRequests, call.SessionID)
	a.mu.Unlock()

	defer func() {
		cancel()
		a.mu.Lock()
		delete(a.activeRequests, call.SessionID)
		delete(a.stopRequests, call.SessionID)
		a.mu.Unlock()
	}()

	// Prepare history
	history := make([]llm.Message, 0, len(msgs))
	for _, m := range msgs {
		if isEmptyMessage(m) {
			continue
		}
		history = append(history, codec.ToLLMMessages(m.ToAppMessage())...)
	}
	preparedUser := codec.PrepareUserRequest(codec.UserRequest{
		Prompt:       call.Prompt,
		Attachments:  message.ToAppAttachments(call.Attachments),
		Skills:       message.ToAppSkills(call.Skills),
		AllowedTools: allowedTools,
	})

	history = a.normalizeHistoryForModelCapabilities(ctx, model, history)

	// Emit streaming event
	if cb := a.EventCallback(); cb != nil {
		cb(core.AgentEvent{Kind: core.EventStreaming})
	}

	// Resolve context window for threshold check
	contextWindow := model.Metadata.ContextWindow
	if contextWindow == 0 {
		contextWindow = int64(model.ModelCfg.ContextWindow)
	}
	if contextWindow == 0 {
		contextWindow = bridgecfg.DefaultContextWindow
	}

	effectiveMaxOutputTokens := call.MaxOutputTokens
	if effectiveMaxOutputTokens <= 0 {
		effectiveMaxOutputTokens = int64(bridgecfg.DefaultMaxTokens)
	}
	if contextWindow > 0 && effectiveMaxOutputTokens >= contextWindow {
		effectiveMaxOutputTokens = contextWindow / 2
		if effectiveMaxOutputTokens < 1 {
			effectiveMaxOutputTokens = 1
		}
	}

	// prepare: build the initial stream request from the prepared user turn.
	req := llm.StreamRequest{
		Prompt:          preparedUser.Prompt,
		SystemPrompt:    systemPrompt,
		Files:           preparedUser.Files,
		Messages:        history,
		Tools:           agentTools,
		MaxOutputTokens: &effectiveMaxOutputTokens,
		TopP:            call.TopP,
		Temperature:     call.Temperature,
		TopK:            call.TopK,
	}

	// stream: invoke the LLM, retrying once if purification removed broken tool calls.
	var result *llm.RunResult
	for {
		rs := &runState{
			agent:         a,
			call:          call,
			model:         model,
			contextWindow: contextWindow,
			genCtx:        genCtx,
			transientIDs:  make([]string, 0, 4),
		}
		req.Callbacks = llm.StreamCallbacks{
			PrepareStep:      rs.prepareStep,
			OnReasoningStart: rs.onReasoningStart,
			OnReasoningDelta: rs.onReasoningDelta,
			OnReasoningEnd:   rs.onReasoningEnd,
			OnTextDelta:      rs.onTextDelta,
			OnToolInputStart: rs.onToolInputStart,
			OnToolInputDelta: rs.onToolInputDelta,
			OnToolCall:       rs.onToolCall,
			OnToolResult:     rs.onToolResult,
			OnStepFinish:     rs.onStepFinish,
		}
		req.StopWhen = []llm.StopCondition{rs.shouldStop}

		streamResult, streamErr := model.Runtime.Stream(genCtx, req)

		// Purify right after stream completion so incomplete tool lifecycles are
		// evicted before any follow-up handling can surface them.
		elided, purifyErr := a.purifyToolCallLifecycle(context.WithoutCancel(ctx), call.SessionID)
		if purifyErr != nil {
			elided = nil // best effort: never surface purification failures
		}
		purified := len(elided) > 0

		if streamErr != nil {
			// For canceled runs, discard only in-flight artifacts from the interrupted step.
			// Completed earlier steps from the same run remain in history.
			if errors.Is(streamErr, context.Canceled) {
				for i := len(rs.transientIDs) - 1; i >= 0; i-- {
					_ = a.messages.Delete(ctx, rs.transientIDs[i])
				}
			} else if rs.currentAssistant != nil {
				if persisted, err := a.messages.Get(ctx, rs.currentAssistant.ID); err == nil {
					rs.currentAssistant = &persisted
					rs.currentAssistant.FinishThinking()
					rs.currentAssistant.AddFinish(message.FinishReasonError, streamErr.Error(), "")
					_ = a.messages.Update(ctx, *rs.currentAssistant)
				}
			}
			return nil, streamErr
		}

		if !purified {
			result = streamResult
			break
		}

		// Purification removed broken tool calls; store a continuation user message
		// and rebuild history from the store so the retry stream sees a clean context.
		retryText := buildPurificationRetryText(elided)
		retryUserMsg, err := a.messages.Create(context.WithoutCancel(ctx), call.SessionID, message.CreateParams{
			Role:  message.User,
			Parts: []message.ContentPart{message.TextContent{Text: retryText}},
		})
		if err != nil {
			return nil, fmt.Errorf("failed to create purification retry message: %w", err)
		}

		allMsgs, err := a.messages.List(context.WithoutCancel(ctx), call.SessionID)
		if err != nil {
			_ = a.messages.Delete(context.WithoutCancel(ctx), retryUserMsg.ID)
			return nil, fmt.Errorf("failed to list messages after purification: %w", err)
		}
		// Build history from all messages except the retry user message we just stored:
		// req.Prompt carries it as the current turn so it is not duplicated in history.
		nextHistory := make([]llm.Message, 0, len(allMsgs))
		for _, m := range allMsgs {
			if m.ID == retryUserMsg.ID || isEmptyMessage(m) {
				continue
			}
			nextHistory = append(nextHistory, codec.ToLLMMessages(m.ToAppMessage())...)
		}

		retryPrepared := codec.PrepareUserRequest(codec.UserRequest{
			Prompt:       retryText,
			AllowedTools: allowedTools,
		})

		// close previous turn; prepare next: prompt is the retry user message, history is everything before it.
		req.Messages = a.normalizeHistoryForModelCapabilities(ctx, model, nextHistory)
		req.Prompt = retryPrepared.Prompt
		req.Files = nil

		if cb := a.EventCallback(); cb != nil {
			cb(core.AgentEvent{Kind: core.EventStreaming})
		}
	}

	// close: return the final result.
	return result, nil
}

// runState holds the mutable per-run state shared by all stream callbacks.
// Each callback is a method on *runState, eliminating the need for closures
// that capture multiple mutable variables.
type runState struct {
	agent         *SessionAgent
	call          SessionAgentCall
	model         bridgecfg.RuntimeModel
	contextWindow int64
	genCtx        context.Context

	currentAssistant *message.Message
	transientIDs     []string // message IDs from the in-flight step only
	promptTokens     int64
	completionTokens int64
	textStarted      bool // true once the first text character has been written
}

func (rs *runState) prepareStep(callCtx context.Context, messages []llm.Message) ([]llm.Message, error) {
	if rs.currentAssistant != nil && len(rs.currentAssistant.Parts) > 0 {
		if err := rs.agent.messages.Update(callCtx, *rs.currentAssistant); err != nil {
			return nil, err
		}
	}
	rs.transientIDs = rs.transientIDs[:0]

	assistantMsg, err := rs.agent.messages.Create(callCtx, rs.call.SessionID, message.CreateParams{
		Role:     message.Assistant,
		Parts:    []message.ContentPart{},
		Model:    rs.model.ModelCfg.Model,
		Provider: rs.model.ModelCfg.Provider,
	})
	if err != nil {
		return nil, err
	}
	rs.currentAssistant = &assistantMsg
	rs.transientIDs = append(rs.transientIDs, assistantMsg.ID)
	rs.textStarted = false
	return messages, nil
}

func (rs *runState) onReasoningStart(_ string, reasoning llm.ReasoningPart) error {
	rs.currentAssistant.AppendReasoningContent(reasoning.Text)
	rs.currentAssistant.SetReasoningProviderMetadata(reasoning.ProviderMetadata)
	return rs.agent.messages.Update(rs.genCtx, *rs.currentAssistant)
}

func (rs *runState) onReasoningDelta(_ string, text string) error {
	rs.currentAssistant.AppendReasoningContent(text)
	if cb := rs.agent.EventCallback(); cb != nil {
		cb(core.AgentEvent{Kind: core.EventThinking, Text: text})
	}
	return rs.agent.messages.Update(rs.genCtx, *rs.currentAssistant)
}

func (rs *runState) onReasoningEnd(_ string, reasoning llm.ReasoningPart) error {
	rs.currentAssistant.SetReasoningProviderMetadata(reasoning.ProviderMetadata)
	rs.currentAssistant.FinishThinking()
	return rs.agent.messages.Update(rs.genCtx, *rs.currentAssistant)
}

func (rs *runState) onTextDelta(_ string, text string) error {
	if !rs.textStarted {
		text = strings.TrimPrefix(text, "\n")
		rs.textStarted = true
	}
	rs.currentAssistant.AppendContent(text)
	if cb := rs.agent.EventCallback(); cb != nil {
		cb(core.AgentEvent{Kind: core.EventTextDelta, Text: text})
	}
	return rs.agent.messages.Update(rs.genCtx, *rs.currentAssistant)
}

func (rs *runState) onToolInputStart(id string, toolName string) error {
	rs.currentAssistant.AddToolCall(message.ToolCall{ID: id, Name: toolName, Finished: false})
	if cb := rs.agent.EventCallback(); cb != nil {
		cb(core.AgentEvent{Kind: core.EventToolCall, ID: id, Name: toolName})
	}
	return rs.agent.messages.Update(rs.genCtx, *rs.currentAssistant)
}

func (rs *runState) onToolInputDelta(id, delta string) error {
	rs.currentAssistant.AppendToolCallInput(id, delta)
	if cb := rs.agent.EventCallback(); cb != nil {
		cb(core.AgentEvent{Kind: core.EventToolInputDelta, ID: id, Input: delta})
	}
	return nil
}

func (rs *runState) onToolCall(tc llm.ToolCallPart) error {
	rs.currentAssistant.AddToolCall(message.ToolCall{
		ID:       tc.ToolCallID,
		Name:     tc.ToolName,
		Input:    tc.InputJSON,
		Finished: true,
	})
	if cb := rs.agent.EventCallback(); cb != nil {
		cb(core.AgentEvent{
			Kind:  core.EventToolCall,
			ID:    tc.ToolCallID,
			Name:  tc.ToolName,
			Input: tc.InputJSON,
		})
	}
	return rs.agent.messages.Update(rs.genCtx, *rs.currentAssistant)
}

func (rs *runState) onToolResult(tr llm.ToolResultPart, toolName string) error {
	toolResult := message.ToolResultFromApp(codec.FromToolResult(toolName, tr))
	created, err := rs.agent.messages.Create(rs.genCtx, rs.call.SessionID, message.CreateParams{
		Role:  message.Tool,
		Parts: []message.ContentPart{toolResult},
	})
	if err != nil {
		return err
	}
	rs.transientIDs = append(rs.transientIDs, created.ID)
	if cb := rs.agent.EventCallback(); cb != nil {
		cb(core.AgentEvent{
			Kind:            core.EventToolResult,
			ID:              tr.ToolCallID,
			Name:            toolName,
			Output:          toolResult.Content,
			MediaDataBase64: toolResult.Data,
			MediaType:       toolResult.MIMEType,
			Metadata:        toolResult.Metadata,
			IsError:         toolResult.IsError,
		})
	}
	return nil
}

func (rs *runState) onStepFinish(stepResult llm.StepResult) error {
	finishReason := message.FinishReasonUnknown
	switch stepResult.FinishReason {
	case llm.FinishReasonLength:
		finishReason = message.FinishReasonMaxTokens
	case llm.FinishReasonStop:
		finishReason = message.FinishReasonEndTurn
	case llm.FinishReasonToolCalls:
		finishReason = message.FinishReasonToolUse
	}
	rs.currentAssistant.AddFinish(finishReason, "", "")

	rs.promptTokens = stepResult.Usage.InputTokens + stepResult.Usage.CacheReadTokens
	rs.completionTokens = stepResult.Usage.OutputTokens

	if cb := rs.agent.EventCallback(); cb != nil {
		cb(core.AgentEvent{
			Kind:          core.EventTotalTokens,
			TotalTokens:   rs.promptTokens + rs.completionTokens,
			ContextWindow: rs.contextWindow,
		})
	}

	if err := rs.agent.messages.Update(rs.genCtx, *rs.currentAssistant); err != nil {
		return err
	}
	rs.transientIDs = rs.transientIDs[:0]
	return nil
}

func (rs *runState) shouldStop(_ []llm.StepResult) bool {
	rs.agent.mu.RLock()
	stop := rs.agent.stopRequests[rs.call.SessionID]
	rs.agent.mu.RUnlock()
	if stop {
		return true
	}
	if rs.contextWindow <= 0 {
		return false
	}
	tokens := rs.promptTokens + rs.completionTokens
	remaining := rs.contextWindow - tokens
	var threshold int64
	if rs.contextWindow > largeContextWindowThreshold {
		threshold = largeContextWindowBuffer
	} else {
		threshold = int64(float64(rs.contextWindow) * smallContextWindowRatio)
	}
	if remaining <= threshold {
		return true
	}
	return false
}

// purifyToolCallLifecycle removes incomplete tool-call lifecycles from the
// message store and returns the elided ToolCall entries so callers can surface
// what was attempted to the LLM on retry.
func (a *SessionAgent) purifyToolCallLifecycle(ctx context.Context, sessionID string) ([]message.ToolCall, error) {
	msgs, err := a.messages.List(ctx, sessionID)
	if err != nil {
		return nil, fmt.Errorf("failed to list messages for purification: %w", err)
	}
	if len(msgs) == 0 {
		return nil, nil
	}

	resultByCallID := make(map[string]struct{})
	for _, msg := range msgs {
		if msg.Role != message.Tool {
			continue
		}
		for _, result := range msg.ToolResults() {
			toolCallID := strings.TrimSpace(result.ToolCallID)
			if toolCallID == "" {
				continue
			}
			resultByCallID[toolCallID] = struct{}{}
		}
	}

	incompleteCallIDs := make(map[string]struct{})
	for _, msg := range msgs {
		if msg.Role != message.Assistant {
			continue
		}
		for _, call := range msg.ToolCalls() {
			toolCallID := strings.TrimSpace(call.ID)
			if toolCallID == "" || !call.Finished {
				incompleteCallIDs[toolCallID] = struct{}{}
				continue
			}
			if _, ok := resultByCallID[toolCallID]; !ok {
				incompleteCallIDs[toolCallID] = struct{}{}
			}
		}
	}

	if len(incompleteCallIDs) == 0 {
		return nil, nil
	}

	// Collect the elided tool calls before removing them.
	var elided []message.ToolCall
	for _, msg := range msgs {
		if msg.Role != message.Assistant {
			continue
		}
		for _, call := range msg.ToolCalls() {
			if _, drop := incompleteCallIDs[strings.TrimSpace(call.ID)]; drop {
				elided = append(elided, call)
			}
		}
	}

	for _, msg := range msgs {
		switch msg.Role {
		case message.Assistant:
			filtered, changed := filterAssistantToolCalls(msg.Parts, incompleteCallIDs)
			if !changed {
				continue
			}
			msg.Parts = filtered
			if isEmptyMessage(msg) {
				if err := a.messages.Delete(ctx, msg.ID); err != nil && !errors.Is(err, message.ErrMessageNotFound) {
					return elided, fmt.Errorf("delete purified assistant message %s: %w", msg.ID, err)
				}
				continue
			}
			if err := a.messages.Update(ctx, msg); err != nil && !errors.Is(err, message.ErrMessageNotFound) {
				return elided, fmt.Errorf("update purified assistant message %s: %w", msg.ID, err)
			}
		case message.Tool:
			filtered, changed := filterToolResults(msg.Parts, incompleteCallIDs)
			if !changed {
				continue
			}
			if len(filtered) == 0 {
				if err := a.messages.Delete(ctx, msg.ID); err != nil && !errors.Is(err, message.ErrMessageNotFound) {
					return elided, fmt.Errorf("delete purified tool message %s: %w", msg.ID, err)
				}
				continue
			}
			msg.Parts = filtered
			if err := a.messages.Update(ctx, msg); err != nil && !errors.Is(err, message.ErrMessageNotFound) {
				return elided, fmt.Errorf("update purified tool message %s: %w", msg.ID, err)
			}
		}
	}

	return elided, nil
}

// buildPurificationRetryText constructs the <sys_info> message injected when
// purification removes broken tool calls, including what was being attempted.
func buildPurificationRetryText(elided []message.ToolCall) string {
	var sb strings.Builder
	sb.WriteString("<sys_info>")
	sb.WriteString("you attempted ")
	if len(elided) == 1 {
		sb.WriteString("a tool call")
	} else {
		fmt.Fprintf(&sb, "%d tool calls", len(elided))
	}
	sb.WriteString(" that did not complete and ")
	if len(elided) == 1 {
		sb.WriteString("was")
	} else {
		sb.WriteString("were")
	}
	sb.WriteString(" removed")
	if len(elided) > 0 {
		sb.WriteString("; the attempted calls were:")
		for _, tc := range elided {
			name := strings.TrimSpace(tc.Name)
			input := strings.TrimSpace(tc.Input)
			fmt.Fprintf(&sb, " %s", name)
			if input != "" && input != "{}" {
				fmt.Fprintf(&sb, "(%s)", input)
			}
		}
	}
	sb.WriteString("; fix the tool calls, and try again.</sys_info>")
	// lets write this text to a file for debugging purposes
	return sb.String()
}

func filterAssistantToolCalls(parts []message.ContentPart, incompleteCallIDs map[string]struct{}) ([]message.ContentPart, bool) {
	filtered := make([]message.ContentPart, 0, len(parts))
	changed := false
	for _, part := range parts {
		call, ok := part.(message.ToolCall)
		if !ok {
			filtered = append(filtered, part)
			continue
		}
		if _, drop := incompleteCallIDs[strings.TrimSpace(call.ID)]; drop {
			changed = true
			continue
		}
		filtered = append(filtered, part)
	}
	return filtered, changed
}

func filterToolResults(parts []message.ContentPart, incompleteCallIDs map[string]struct{}) ([]message.ContentPart, bool) {
	filtered := make([]message.ContentPart, 0, len(parts))
	changed := false
	for _, part := range parts {
		result, ok := part.(message.ToolResult)
		if !ok {
			filtered = append(filtered, part)
			continue
		}
		if _, drop := incompleteCallIDs[strings.TrimSpace(result.ToolCallID)]; drop {
			changed = true
			continue
		}
		filtered = append(filtered, part)
	}
	return filtered, changed
}

// createUserMessage creates and stores a user message.
func (a *SessionAgent) createUserMessage(ctx context.Context, call SessionAgentCall) (message.Message, error) {
	var parts []message.ContentPart
	if call.Prompt != "" {
		parts = append(parts, message.TextContent{Text: call.Prompt})
	}
	for _, att := range call.Attachments {
		parts = append(parts, att)
	}
	for _, skill := range call.Skills {
		parts = append(parts, skill)
	}

	return a.messages.Create(ctx, call.SessionID, message.CreateParams{
		Role:  message.User,
		Parts: parts,
	})
}

// isEmptyMessage checks if a message should be skipped when building history.
func isEmptyMessage(m message.Message) bool {
	if len(m.Parts) == 0 {
		return true
	}
	// Skip empty assistant messages (no content, no tool calls, no reasoning)
	if m.Role == message.Assistant && len(m.ToolCalls()) == 0 && m.Content().Text == "" && m.ReasoningContent().Thinking == "" {
		return true
	}
	return false
}

// Cancel cancels a running session.
func (a *SessionAgent) Cancel(sessionID string) {
	a.mu.RLock()
	cancel := a.activeRequests[sessionID]
	a.mu.RUnlock()
	if cancel != nil {
		cancel()
	}
}

// RequestStop asks a running session to stop at the next safe step boundary.
// This is used for steering: finish the current tool execution, then interrupt.
func (a *SessionAgent) RequestStop(sessionID string) {
	a.mu.Lock()
	_, busy := a.activeRequests[sessionID]
	if busy {
		a.stopRequests[sessionID] = true
	}
	a.mu.Unlock()
}

// CancelAll cancels all running sessions.
func (a *SessionAgent) CancelAll() {
	a.mu.RLock()
	ids := make([]string, 0, len(a.activeRequests))
	for id := range a.activeRequests {
		ids = append(ids, id)
	}
	a.mu.RUnlock()
	for _, id := range ids {
		a.Cancel(id)
	}
}

// IsBusy returns true if any session is running.
func (a *SessionAgent) IsBusy() bool {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return len(a.activeRequests) > 0
}

// IsSessionBusy returns true if the specified session is running.
func (a *SessionAgent) IsSessionBusy(sessionID string) bool {
	a.mu.RLock()
	defer a.mu.RUnlock()
	_, busy := a.activeRequests[sessionID]
	return busy
}

// Model returns the current model.
func (a *SessionAgent) Model() bridgecfg.RuntimeModel {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.model
}

// SetModel updates the model.
func (a *SessionAgent) SetModel(m bridgecfg.RuntimeModel) {
	a.mu.Lock()
	a.model = m
	a.mu.Unlock()
}

// SetSystemPrompt updates the system prompt.
func (a *SessionAgent) SetSystemPrompt(prompt string) {
	a.mu.Lock()
	a.systemPrompt = prompt
	a.mu.Unlock()
}

// Tools returns a copy of the current agent tools.
func (a *SessionAgent) Tools() []llm.Tool {
	a.mu.RLock()
	out := make([]llm.Tool, len(a.tools))
	copy(out, a.tools)
	a.mu.RUnlock()
	return out
}

// SetTools updates the agent tools.
func (a *SessionAgent) SetTools(t []llm.Tool) {
	cp := make([]llm.Tool, len(t))
	copy(cp, t)
	a.mu.Lock()
	a.tools = cp
	a.mu.Unlock()
}

// Messages returns the message service.
func (a *SessionAgent) Messages() message.Service {
	return a.messages
}

// Sessions returns the session service.
func (a *SessionAgent) Sessions() session.Service {
	return a.sessions
}

// NewSessionAgentFromConfig creates a SessionAgent from config, optionally reusing existing services.
// Pass nil for any service to create a fresh one.
func NewSessionAgentFromConfig(cfg *bridgecfg.Config, msgService message.Service, sessService session.Service) (*SessionAgent, error) {
	factory := llm.NewDefaultFactory()

	// Build the model from config
	largeModel, providerCfg, ok := cfg.ActiveModel()
	if !ok {
		return nil, ErrNoModelConfigured
	}

	_, ok = cfg.GetProvider(largeModel.Provider)
	if !ok {
		return nil, ErrProviderNotConfigured
	}

	runtime, metadata, err := factory.NewRuntime(context.Background(), llm.ProviderConfig{
		ID:              providerCfg.ID,
		Name:            providerCfg.Name,
		Type:            providerCfg.Type,
		APIKey:          providerCfg.APIKey,
		BaseURL:         providerCfg.BaseURL,
		ExtraHeaders:    providerCfg.ExtraHeaders,
		ProviderOptions: providerCfg.ProviderOptions,
	}, llm.ModelSelection{
		ProviderID:      largeModel.Provider,
		ModelID:         largeModel.Model,
		ThinkingLevel:   largeModel.ThinkingLevel,
		MaxOutputTokens: largeModel.MaxTokens,
		Temperature:     largeModel.Temperature,
		TopP:            largeModel.TopP,
		TopK:            largeModel.TopK,
	}, cfg)
	if err != nil {
		return nil, err
	}

	catalogModel := cfg.GetModel(largeModel.Provider, largeModel.Model)
	model := bridgecfg.RuntimeModel{
		Runtime:  runtime,
		Metadata: metadata,
		ModelCfg: largeModel,
	}
	if catalogModel != nil {
		model.Metadata.ContextWindow = catalogModel.ContextWindow
		model.Metadata.MaxOutput = catalogModel.DefaultMaxTokens
		model.Metadata.CanReason = catalogModel.CanReason
		model.Metadata.SupportsImage = catalogModel.SupportsImages
	}

	systemPrompt := BuildSystemPrompt()

	// Create services if not provided
	if msgService == nil {
		msgService = message.NewInMemoryService()
	}
	if sessService == nil {
		sessService = session.NewInMemoryService()
	}

	return NewSessionAgent(SessionAgentOptions{
		Model:        model,
		SystemPrompt: systemPrompt,

		Messages: msgService,
		Sessions: sessService,
	}), nil
}

// BuildSystemPrompt constructs the system prompt with skills, tools,
// AGENTS.md content, and environment info using plain string concatenation.
func BuildSystemPrompt() string {
	sp := `<identity>
You are the best AI-Powered Coding Agent, operating inside Raijin a coding-agent harness.
</identity>

<communication_rules>
- Begin directly with substance.
- Keep wording tight and actionable.
- Prefer writing ASCII characters instead of using more complex symbols, both in prose and in the code you produce.
</communication_rules>

<code_references>
When referencing specific functions or code locations, use the pattern ` + "`file_path:line_number`" + ` to help users navigate:
- Example: "The error is handled in src/main.go:45"
- Example: "See the implementation in pkg/utils/helper.go:123-145"
</code_references>

<testing>
After significant code changes:
- Start testing as specifically as possible, then broaden.
- Run the relevant test suite: ` + "`cargo test`" + `.
- Run the linter: ` + "`cargo clippy`" + `.
- Check formatting: ` + "`cargo fmt --check`" + `.
- Don't fix unrelated bugs or broken tests.
</testing>`

	// Append available skills.
	allSkills := skills.GetSkills()
	hasVisible := false
	for _, s := range allSkills {
		if s.ShouldAdvertiseToLLM() {
			hasVisible = true
			break
		}
	}
	if hasVisible {
		sp += "\n\n<skills>\n"
		sp += "- Load a skill via the \"skill\" tool when the user's request matches one listed above.\n"
		sp += "- Wording that closely matches a skill name should be treated as an implicit request to load it.\n"
		for _, s := range allSkills {
			if s.ShouldAdvertiseToLLM() {
				sp += "  <skill name=\"" + s.Name + "\">" + s.PromptDescription() + "</skill>\n"
			}
		}
		sp += "</skills>"
	}

	// Append available tools.
	allTools := tools.RegisterDefaultTools(tools.NewPathRegistry())
	if len(allTools) > 0 {
		sp += "\n\n<tools>\nThe following tools are at your disposal\n"
		for _, t := range allTools {
			info := t.Info()
			sp += "  <tool name=\"" + info.Name + "\">" + info.Description + "</tool>\n"
		}
		sp += "</tools>"
		sp += buildToolPreferencesSection(allTools)
	}

	// Append AGENTS.md content.
	if file, ok := GetAgentsFile(); ok {
		cwd, _ := filepath.Abs(".")
		header := ""
		if !SameDir(file.Dir, cwd) {
			header = fmt.Sprintf("Note: this AGENTS.md was loaded from %q. Any relative paths in it are relative to that directory, not the current working directory.\n\n", file.Dir)
		}
		sp += "\n\n<memory>\n" + header + file.Content + "\n</memory>"
	}

	// Append environment section.
	cwd, err := os.Getwd()
	if err != nil {
		cwd = "unknown"
	}
	gitStatus := "no"
	if _, err := os.Stat(filepath.Join(cwd, ".git")); err == nil {
		gitStatus = "yes"
	}
	sp += "\n\n<env>\nWorking directory: " + cwd +
		"\nPlatform: " + runtime.GOOS + " (" + runtime.GOARCH + ")" +
		"\nToday's date: " + time.Now().Format("2006-01-02") +
		"\nIs git repo: " + gitStatus +
		"\n</env>"

	// fmt.Println(sp) //in case of debugging
	return sp
}

func buildToolPreferencesSection(allTools []llm.Tool) string {
	if len(allTools) == 0 {
		return ""
	}

	preferences := make([]string, 0, len(allTools))
	for _, t := range allTools {
		name := core.Normalize(t.Info().Name)
		if name == "" {
			continue
		}
		preferences = append(preferences, toolPreferenceFor(name))
	}
	if len(preferences) == 0 {
		return ""
	}

	sp := "\n\n<tool-preferences>\n"
	for _, pref := range preferences {
		sp += "- " + pref + "\n"
	}
	sp += "</tool-preferences>"
	return sp
}

func toolPreferenceFor(name string) string {
	switch name {
	case "read":
		return "Use the read tool instead of shelling out with cat/sed/head/tail/ls for inspecting files and directories."
	case "glob":
		return "Use the glob tool instead of find/ls pipelines when locating files by pattern."
	case "grep":
		return "Use the grep tool instead of running grep/ripgrep in bash for content search."
	case "edit":
		return "Use the edit tool instead of perl/sed/awk/python one-liners for surgical in-place edits."
	case "write":
		return "Use the write tool instead of shell redirection (>, >>, cat <<EOF) when creating or overwriting files."
	case "bash":
		return "Use the bash tool only when needed for commands that have no dedicated built-in tool equivalent."
	case "skill":
		return "Use the skill tool to load reusable workflows instead of manually reproducing those steps in bash."
	case "webfetch":
		return "Use the webfetch tool instead of curl/wget in bash for retrieving web content."
	default:
		return "Use the " + name + " tool instead of using bash or shell scripts as equivalents for that task."
	}
}

func (a *SessionAgent) validateImageAttachments(ctx context.Context, model bridgecfg.RuntimeModel, attachments []message.BinaryContent) error {
	if !hasImageAttachments(attachments) {
		return nil
	}
	if a.modelSupportsImagesFn == nil {
		return nil
	}

	supports, known, err := a.modelSupportsImagesFn(ctx, model.ModelCfg.Provider, model.ModelCfg.Model)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrImageSupportLookupFailed, err)
	}
	if known && !supports {
		return fmt.Errorf("%w: %s/%s", ErrModelNoImageSupport, model.ModelCfg.Provider, model.ModelCfg.Model)
	}
	return nil
}

func hasImageAttachments(attachments []message.BinaryContent) bool {
	for _, att := range attachments {
		if strings.HasPrefix(strings.ToLower(strings.TrimSpace(att.MIMEType)), "image/") {
			return true
		}
	}
	return false
}

func lookupModelSupportsImages(ctx context.Context, providerID, modelID string) (supports bool, known bool, err error) {
	source := catalog.NewRaijinSource()
	model, found, err := source.FindModel(ctx, providerID, modelID)
	if err != nil {
		return false, false, err
	}
	if !found {
		return false, false, nil
	}
	return model.SupportsImages, true, nil
}

func (a *SessionAgent) normalizeHistoryForModelCapabilities(ctx context.Context, model bridgecfg.RuntimeModel, history []llm.Message) []llm.Message {
	if len(history) == 0 {
		return history
	}
	supports, known := a.resolveModelImageCapability(ctx, model)
	if !known || supports {
		return history
	}
	return stripMediaFromMessages(history)
}

func (a *SessionAgent) resolveModelImageCapability(ctx context.Context, model bridgecfg.RuntimeModel) (supports bool, known bool) {
	if a.modelSupportsImagesFn == nil {
		return false, false
	}
	supports, known, err := a.modelSupportsImagesFn(ctx, model.ModelCfg.Provider, model.ModelCfg.Model)
	if err != nil {
		return false, false
	}
	return supports, known
}

func adaptToolsForImageCapability(tools []llm.Tool, supportsImages, known bool) []llm.Tool {
	if !known || supportsImages || len(tools) == 0 {
		return tools
	}
	adapted := make([]llm.Tool, 0, len(tools))
	for _, tool := range tools {
		adapted = append(adapted, mediaDisabledTool{Tool: tool})
	}
	return adapted
}

type mediaDisabledTool struct{ llm.Tool }

func (t mediaDisabledTool) Run(ctx context.Context, params llm.ToolCall) (llm.ToolResponse, error) {
	resp, err := t.Tool.Run(ctx, params)
	if err != nil {
		return resp, err
	}
	if resp.Type != llm.ToolResponseTypeMedia {
		return resp, nil
	}
	note := "[Media output omitted: selected model does not support image/media inputs]"
	if strings.TrimSpace(resp.MediaType) != "" {
		note = fmt.Sprintf("[Media output omitted (%s): selected model does not support image/media inputs]", resp.MediaType)
	}
	return llm.NewTextResponse(note), nil
}

func stripMediaFromMessages(messages []llm.Message) []llm.Message {
	out := make([]llm.Message, 0, len(messages))
	for _, msg := range messages {
		parts := make([]llm.Part, 0, len(msg.Content))
		for _, part := range msg.Content {
			switch p := part.(type) {
			case llm.FilePart:
				note := "[Attachment omitted: selected model does not support image/media inputs]"
				if p.Filename != "" || p.MediaType != "" {
					note = fmt.Sprintf("[Attachment omitted: %s %s]", p.Filename, p.MediaType)
				}
				parts = append(parts, llm.TextPart{Text: strings.TrimSpace(note)})
			case llm.ToolResultPart:
				if p.Output.Type == llm.ToolResultOutputMedia {
					parts = append(parts, llm.ToolResultPart{
						ToolCallID: p.ToolCallID,
						Output: llm.ToolResultOutput{
							Type: llm.ToolResultOutputText,
							Text: "[Image/media tool result omitted: selected model does not support image/media inputs]",
						},
						Metadata: p.Metadata,
					})
					continue
				}
				parts = append(parts, part)
			default:
				parts = append(parts, part)
			}
		}
		out = append(out, llm.Message{Role: msg.Role, Content: parts})
	}
	return out
}
