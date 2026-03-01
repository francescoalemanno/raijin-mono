package libagent

import (
	"context"
	"fmt"
	"sync"
	"time"

	"charm.land/fantasy"
)

// Agent is a stateful, event-emitting agent that manages conversation history,
// tool execution, and streaming LLM responses.
type Agent struct {
	// mu protects fields that may be read concurrently while runLoop is active:
	// isStreaming, streamMsg, lastErr, pendingCalls, messages.
	mu sync.RWMutex

	// state
	systemPrompt string
	runtimeModel RuntimeModel
	tools        []Tool
	messages     []Message
	isStreaming  bool
	streamMsg    Message
	pendingCalls map[string]struct{}
	lastErr      error

	// configuration
	convertToLLM     ConvertToLLMFn
	transformContext TransformContextFn
	steeringMode     QueueMode
	followUpMode     QueueMode
	sessionID        string
	providerOptions  fantasy.ProviderOptions
	maxOutputTokens  int64

	// queues
	steeringQueue []Message
	followUpQueue []Message

	// event subscribers
	subscribers []chan AgentEvent

	// abort / idle coordination
	cancel      context.CancelFunc
	runningDone chan struct{}
}

// AgentOptions configures a new Agent.
type AgentOptions struct {
	// RuntimeModel sets the initial language model and capabilities metadata. Required.
	RuntimeModel RuntimeModel
	// SystemPrompt sets the initial system prompt.
	SystemPrompt string
	// Tools sets the initial tool list using the stdlib-only Tool interface.
	Tools []Tool
	// Messages pre-loads conversation history.
	Messages []Message

	// ConvertToLLM converts agent messages to LLM-compatible messages.
	// Defaults to DefaultConvertToLLM.
	ConvertToLLM ConvertToLLMFn
	// TransformContext optionally transforms messages before ConvertToLLM.
	TransformContext TransformContextFn

	// SteeringMode controls how steering queue drains. Defaults to QueueModeOneAtATime.
	SteeringMode QueueMode
	// FollowUpMode controls how follow-up queue drains. Defaults to QueueModeOneAtATime.
	FollowUpMode QueueMode

	// SessionID is forwarded to providers that support session-based caching.
	SessionID string

	// ProviderOptions are passed to every LLM call the agent makes.
	// Build with BuildProviderOptions or RuntimeModel.BuildCallProviderOptions.
	ProviderOptions fantasy.ProviderOptions

	// MaxOutputTokens caps each LLM response. When nil or 0 no limit is sent.
	// Set to 0 for providers like Codex that reject this field.
	MaxOutputTokens int64
}

// NewAgent creates a new Agent with the given options.
func NewAgent(opts AgentOptions) *Agent {
	if opts.SteeringMode == "" {
		opts.SteeringMode = QueueModeOneAtATime
	}
	if opts.FollowUpMode == "" {
		opts.FollowUpMode = QueueModeOneAtATime
	}
	if opts.ConvertToLLM == nil {
		opts.ConvertToLLM = DefaultConvertToLLM
	}
	opts.RuntimeModel.ModelInfo = normalizeModelInfo(opts.RuntimeModel.ModelInfo)
	a := &Agent{
		runtimeModel:     opts.RuntimeModel,
		systemPrompt:     opts.SystemPrompt,
		tools:            opts.Tools,
		messages:         append([]Message{}, opts.Messages...),
		convertToLLM:     opts.ConvertToLLM,
		transformContext: opts.TransformContext,
		steeringMode:     opts.SteeringMode,
		followUpMode:     opts.FollowUpMode,
		sessionID:        opts.SessionID,
		providerOptions:  opts.ProviderOptions,
		maxOutputTokens:  opts.MaxOutputTokens,
		pendingCalls:     make(map[string]struct{}),
	}
	return a
}

// ----- State accessors ---------------------------------------------------------

// State returns a snapshot of the current agent state.
func (a *Agent) State() AgentState {
	a.mu.RLock()
	msgs := make([]Message, len(a.messages))
	copy(msgs, a.messages)
	pending := make(map[string]struct{}, len(a.pendingCalls))
	for k := range a.pendingCalls {
		pending[k] = struct{}{}
	}
	isStreaming := a.isStreaming
	streamMsg := a.streamMsg
	lastErr := a.lastErr
	a.mu.RUnlock()
	return AgentState{
		SystemPrompt:     a.systemPrompt,
		Model:            a.runtimeModel.Model,
		Tools:            AdaptTools(a.tools),
		Messages:         msgs,
		IsStreaming:      isStreaming,
		StreamMessage:    streamMsg,
		PendingToolCalls: pending,
		Error:            lastErr,
	}
}

// SetSystemPrompt replaces the system prompt.
func (a *Agent) SetSystemPrompt(v string) {
	a.systemPrompt = v
}

// SetRuntimeModel replaces the runtime model and metadata.
func (a *Agent) SetRuntimeModel(m RuntimeModel) {
	m.ModelInfo = normalizeModelInfo(m.ModelInfo)
	a.runtimeModel = m
}

// SetTools replaces the tool list.
func (a *Agent) SetTools(t []Tool) {
	a.tools = t
}

// ReplaceMessages replaces the message history with a copy of ms.
func (a *Agent) ReplaceMessages(ms []Message) {
	a.messages = append([]Message{}, ms...)
}

// AppendMessage appends a single message to the history.
func (a *Agent) AppendMessage(m Message) {
	a.messages = append(a.messages, m)
}

// ClearMessages removes all messages.
func (a *Agent) ClearMessages() {
	a.messages = nil
}

// Reset clears messages and resets streaming state.
func (a *Agent) Reset() {
	a.messages = nil
	a.isStreaming = false
	a.streamMsg = nil
	a.pendingCalls = make(map[string]struct{})
	a.lastErr = nil
	a.steeringQueue = nil
	a.followUpQueue = nil
}

// SetSessionID updates the session ID.
func (a *Agent) SetSessionID(id string) {
	a.sessionID = id
}

// SessionID returns the current session ID.
func (a *Agent) SessionID() string {
	return a.sessionID
}

// SetSteeringMode sets how the steering queue drains.
func (a *Agent) SetSteeringMode(m QueueMode) {
	a.steeringMode = m
}

// GetSteeringMode returns the current steering mode.
func (a *Agent) GetSteeringMode() QueueMode {
	return a.steeringMode
}

// SetFollowUpMode sets how the follow-up queue drains.
func (a *Agent) SetFollowUpMode(m QueueMode) {
	a.followUpMode = m
}

// GetFollowUpMode returns the current follow-up mode.
func (a *Agent) GetFollowUpMode() QueueMode {
	return a.followUpMode
}

// RuntimeModel returns the current runtime model and metadata.
func (a *Agent) RuntimeModel() RuntimeModel {
	return a.runtimeModel
}

// ----- Queueing ----------------------------------------------------------------

// Steer queues a steering message to interrupt the agent mid-run.
// Steering messages are injected after the current tool execution.
func (a *Agent) Steer(m Message) {
	a.steeringQueue = append(a.steeringQueue, m)
}

// FollowUp queues a follow-up message for after the agent finishes its current work.
func (a *Agent) FollowUp(m Message) {
	a.followUpQueue = append(a.followUpQueue, m)
}

// ClearSteeringQueue empties the steering queue.
func (a *Agent) ClearSteeringQueue() {
	a.steeringQueue = nil
}

// ClearFollowUpQueue empties the follow-up queue.
func (a *Agent) ClearFollowUpQueue() {
	a.followUpQueue = nil
}

// ClearAllQueues empties both queues.
func (a *Agent) ClearAllQueues() {
	a.steeringQueue = nil
	a.followUpQueue = nil
}

// HasQueuedMessages reports whether either queue is non-empty.
func (a *Agent) HasQueuedMessages() bool {
	return len(a.steeringQueue) > 0 || len(a.followUpQueue) > 0
}

func (a *Agent) dequeueSteeringMessages() []Message {
	if a.steeringMode == QueueModeOneAtATime {
		if len(a.steeringQueue) > 0 {
			first := a.steeringQueue[0]
			a.steeringQueue = a.steeringQueue[1:]
			return []Message{first}
		}
		return nil
	}
	all := a.steeringQueue
	a.steeringQueue = nil
	return all
}

func (a *Agent) dequeueFollowUpMessages() []Message {
	if a.followUpMode == QueueModeOneAtATime {
		if len(a.followUpQueue) > 0 {
			first := a.followUpQueue[0]
			a.followUpQueue = a.followUpQueue[1:]
			return []Message{first}
		}
		return nil
	}
	all := a.followUpQueue
	a.followUpQueue = nil
	return all
}

// ----- Event subscription ------------------------------------------------------

// Subscribe registers a listener for agent events.
// Returns an unsubscribe function that removes the listener.
// Events are delivered on a buffered channel; if the channel is full the event is dropped.
func (a *Agent) Subscribe() (<-chan AgentEvent, func()) {
	ch := make(chan AgentEvent, 64)
	a.subscribers = append(a.subscribers, ch)
	remove := func() {
		for i, s := range a.subscribers {
			if s == ch {
				a.subscribers = append(a.subscribers[:i], a.subscribers[i+1:]...)
				close(ch)
				return
			}
		}
	}
	return ch, remove
}

func (a *Agent) emit(event AgentEvent) {
	subs := a.subscribers
	for _, ch := range subs {
		select {
		case ch <- event:
		default:
		}
	}
}

// ----- Prompting ---------------------------------------------------------------

// Prompt sends a text prompt (and optional file attachments) to the agent.
// Blocks until the agent finishes or the context is cancelled.
// Returns an error if the agent is already streaming.
func (a *Agent) Prompt(ctx context.Context, text string, files ...FilePart) error {
	msg := &UserMessage{
		Role:      "user",
		Content:   text,
		Files:     files,
		Timestamp: time.Now(),
	}
	return a.PromptMessages(ctx, msg)
}

// PromptMessages sends one or more pre-built messages as the next prompt.
func (a *Agent) PromptMessages(ctx context.Context, msgs ...Message) error {
	if a.isStreaming {
		return fmt.Errorf("agent is already processing a prompt. Use Steer() or FollowUp() to queue messages, or wait for completion")
	}
	return a.runLoop(ctx, msgs, false)
}

// Continue resumes from the existing context without adding a new message.
// The last message must not be an assistant message (it must be user or toolResult).
// If the last message is assistant and the steering or follow-up queues are non-empty,
// those are drained first.
func (a *Agent) Continue(ctx context.Context) error {
	if a.isStreaming {
		return fmt.Errorf("agent is already processing. Wait for completion before continuing")
	}
	msgs := a.messages
	if len(msgs) == 0 {
		return fmt.Errorf("no messages to continue from")
	}
	last := msgs[len(msgs)-1]

	if last.GetRole() == "assistant" {
		// Try to drain steering/follow-up queues before giving up.
		steering := a.dequeueSteeringMessages()
		if len(steering) > 0 {
			return a.runLoop(ctx, steering, true)
		}
		followUp := a.dequeueFollowUpMessages()
		if len(followUp) > 0 {
			return a.runLoop(ctx, followUp, false)
		}
		return fmt.Errorf("cannot continue from message role: assistant")
	}

	return a.runLoop(ctx, nil, false)
}

// Abort cancels the current streaming operation.
func (a *Agent) Abort() {
	a.mu.RLock()
	cancel := a.cancel
	a.mu.RUnlock()
	if cancel != nil {
		cancel()
	}
}

// WaitForIdle blocks until the agent is not streaming.
func (a *Agent) WaitForIdle() {
	a.mu.RLock()
	done := a.runningDone
	a.mu.RUnlock()
	if done != nil {
		<-done
	}
}

// ----- Internal run loop -------------------------------------------------------

// runLoop is the internal driver. When prompts is nil/empty, it continues from context.
// skipInitialSteeringPoll skips the very first GetSteeringMessages call (used when
// steering messages are already provided as prompts).
func (a *Agent) runLoop(ctx context.Context, prompts []Message, skipInitialSteeringPoll bool) error {
	runtimeModel := a.runtimeModel
	model := runtimeModel.Model
	if model == nil {
		return fmt.Errorf("no model configured")
	}
	msgs := append([]Message{}, a.messages...)
	mediaSupport := mediaSupportFromModelInfo(runtimeModel.ModelInfo)
	tools := AdaptTools(a.tools)
	systemPrompt := a.systemPrompt
	transformContext := composeTransformContext(a.transformContext, runtimeMediaTransform(mediaSupport))
	done := make(chan struct{})
	runCtx, cancel := context.WithCancel(ctx)
	a.mu.Lock()
	a.isStreaming = true
	a.streamMsg = nil
	a.lastErr = nil
	a.runningDone = done
	a.cancel = cancel
	a.mu.Unlock()

	// Close done and cleanup when we exit.
	defer func() {
		cancel()
		a.mu.Lock()
		a.isStreaming = false
		a.streamMsg = nil
		a.pendingCalls = make(map[string]struct{})
		a.cancel = nil
		a.runningDone = nil
		a.mu.Unlock()
		close(done)
	}()

	skipFirst := skipInitialSteeringPoll
	agentCtx := &AgentContext{
		SystemPrompt: systemPrompt,
		Messages:     msgs,
		Tools:        tools,
	}
	var maxOut *int64
	if a.maxOutputTokens > 0 {
		v := a.maxOutputTokens
		maxOut = &v
	}
	cfg := AgentLoopConfig{
		Model:            model,
		ConvertToLLM:     a.convertToLLM,
		TransformContext: transformContext,
		ProviderOptions:  a.providerOptions,
		MaxOutputTokens:  maxOut,
		GetSteeringMessages: func(ctx context.Context) ([]Message, error) {
			if skipFirst {
				skipFirst = false
				return nil, nil
			}
			return a.dequeueSteeringMessages(), nil
		},
		GetFollowUpMessages: func(ctx context.Context) ([]Message, error) {
			return a.dequeueFollowUpMessages(), nil
		},
	}

	// Event channel: we read events from it and update state + emit to subscribers.
	eventCh := make(chan AgentEvent, 64)

	var loopErr error
	var newMessages []Message

	go func() {
		if len(prompts) > 0 {
			newMessages, loopErr = AgentLoop(runCtx, prompts, agentCtx, cfg, eventCh)
		} else {
			newMessages, loopErr = AgentLoopContinue(runCtx, agentCtx, cfg, eventCh)
		}
		close(eventCh)
	}()

	for event := range eventCh {
		// Update internal state.
		a.mu.Lock()
		switch event.Type {
		case AgentEventTypeMessageStart:
			a.streamMsg = event.Message

		case AgentEventTypeMessageUpdate:
			a.streamMsg = event.Message

		case AgentEventTypeMessageEnd:
			a.streamMsg = nil
			a.messages = append(a.messages, event.Message)

		case AgentEventTypeToolExecutionStart:
			a.pendingCalls[event.ToolCallID] = struct{}{}

		case AgentEventTypeToolExecutionEnd:
			delete(a.pendingCalls, event.ToolCallID)

		case AgentEventTypeAgentEnd:
			a.isStreaming = false
		}
		a.mu.Unlock()

		// Forward to subscribers.
		a.emit(event)
	}

	if loopErr != nil {
		a.mu.Lock()
		a.lastErr = loopErr
		a.mu.Unlock()
		// Emit an error agent_end so subscribers know it finished.
		a.emit(AgentEvent{Type: AgentEventTypeAgentEnd, Messages: newMessages})
	}

	return loopErr
}
