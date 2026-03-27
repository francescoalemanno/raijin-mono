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
	sessionID        string
	providerOptions  fantasy.ProviderOptions
	maxOutputTokens  int64
	onCompleteHook   OnCompleteHook

	// event subscribers (subMu protects subscribers slice)
	subMu       sync.RWMutex
	subscribers []*agentSubscriber

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

	// SessionID is forwarded to providers that support session-based caching.
	SessionID string

	// ProviderOptions are passed to every LLM call the agent makes.
	// Build with BuildProviderOptions or RuntimeModel.BuildCallProviderOptions.
	ProviderOptions fantasy.ProviderOptions

	// MaxOutputTokens caps each LLM response. When nil or 0 no limit is sent.
	// Set to 0 for providers like Codex that reject this field.
	MaxOutputTokens int64

	// OnCompleteHook optionally validates final assistant responses and may
	// inject a user follow-up to continue the same run.
	OnCompleteHook OnCompleteHook
}

// NewAgent creates a new Agent with the given options.
func NewAgent(opts AgentOptions) *Agent {
	if opts.ConvertToLLM == nil {
		opts.ConvertToLLM = defaultConvertToLLMForRuntime(opts.RuntimeModel.ProviderType, opts.ProviderOptions)
	}
	opts.RuntimeModel.ModelInfo = normalizeModelInfo(opts.RuntimeModel.ModelInfo)
	a := &Agent{
		runtimeModel:     opts.RuntimeModel,
		systemPrompt:     opts.SystemPrompt,
		tools:            opts.Tools,
		messages:         append([]Message{}, opts.Messages...),
		convertToLLM:     opts.ConvertToLLM,
		transformContext: opts.TransformContext,
		sessionID:        opts.SessionID,
		providerOptions:  opts.ProviderOptions,
		maxOutputTokens:  opts.MaxOutputTokens,
		onCompleteHook:   opts.OnCompleteHook,
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
}

// SetSessionID updates the session ID.
func (a *Agent) SetSessionID(id string) {
	a.sessionID = id
}

// SessionID returns the current session ID.
func (a *Agent) SessionID() string {
	return a.sessionID
}

// RuntimeModel returns the current runtime model and metadata.
func (a *Agent) RuntimeModel() RuntimeModel {
	return a.runtimeModel
}

// ----- Event subscription ------------------------------------------------------

// agentSubscriber is an unbounded, lossless event queue for a single subscriber.
// A relay goroutine buffers events in a slice so the emitter never blocks and
// events are never dropped regardless of how slowly the subscriber consumes them.
type agentSubscriber struct {
	in   chan AgentEvent // emitter writes here (unbuffered; relay goroutine is the buffer)
	out  chan AgentEvent // subscriber reads here (unbuffered; relay goroutine is the buffer)
	done chan struct{}   // closed by stop() to tear down the relay
}

func newAgentSubscriber() *agentSubscriber {
	s := &agentSubscriber{
		in:   make(chan AgentEvent),
		out:  make(chan AgentEvent),
		done: make(chan struct{}),
	}
	go s.relay()
	return s
}

// relay moves events from in → internal slice → out without ever dropping.
// When done is closed (stop called), the relay flushes any buffered events
// to out before closing it, so subscribers always see the full event stream.
func (s *agentSubscriber) relay() {
	defer close(s.out)
	var buf []AgentEvent
	for {
		if len(buf) == 0 {
			select {
			case v, ok := <-s.in:
				if !ok {
					return
				}
				buf = append(buf, v)
			case <-s.done:
				// Drain in without blocking, then flush buf, then exit.
				for {
					select {
					case v, ok := <-s.in:
						if !ok {
							goto flush
						}
						buf = append(buf, v)
					default:
						goto flush
					}
				}
			flush:
				for _, item := range buf {
					s.out <- item
				}
				return
			}
		} else {
			select {
			case v, ok := <-s.in:
				if !ok {
					// in closed; flush remaining buffer then exit.
					for _, item := range buf {
						s.out <- item
					}
					return
				}
				buf = append(buf, v)
			case s.out <- buf[0]:
				buf = buf[1:]
			case <-s.done:
				// Drain in without blocking, flush buf, then exit.
				for {
					select {
					case v, ok := <-s.in:
						if !ok {
							goto flush2
						}
						buf = append(buf, v)
					default:
						goto flush2
					}
				}
			flush2:
				for _, item := range buf {
					s.out <- item
				}
				return
			}
		}
	}
}

// send delivers an event to the subscriber. It blocks only until the relay
// goroutine receives it (typically nanoseconds), after which the event sits
// in the relay's unbounded internal slice.
func (s *agentSubscriber) send(event AgentEvent) {
	select {
	case s.in <- event:
	case <-s.done:
	}
}

// stop shuts down the relay goroutine and closes the output channel.
func (s *agentSubscriber) stop() {
	select {
	case <-s.done:
	default:
		close(s.done)
	}
}

// Subscribe registers a listener for agent events.
// Returns the event channel and an unsubscribe function.
// Events are delivered in order and never dropped; the channel is closed when
// unsubscribed. Subscribe and unsubscribe are safe to call concurrently with emit.
func (a *Agent) Subscribe() (<-chan AgentEvent, func()) {
	sub := newAgentSubscriber()
	a.subMu.Lock()
	a.subscribers = append(a.subscribers, sub)
	a.subMu.Unlock()
	remove := func() {
		a.subMu.Lock()
		for i, s := range a.subscribers {
			if s == sub {
				a.subscribers = append(a.subscribers[:i], a.subscribers[i+1:]...)
				break
			}
		}
		a.subMu.Unlock()
		sub.stop()
	}
	return sub.out, remove
}

// stopAllSubscribers stops and removes every active subscriber, closing their
// output channels. Called by runLoop's defer so range-evCh callers unblock.
func (a *Agent) stopAllSubscribers() {
	a.subMu.Lock()
	subs := a.subscribers
	a.subscribers = nil
	a.subMu.Unlock()
	for _, s := range subs {
		s.stop()
	}
}

func (a *Agent) emit(event AgentEvent) {
	a.subMu.RLock()
	subs := a.subscribers
	a.subMu.RUnlock()
	for _, s := range subs {
		s.send(event)
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
		return fmt.Errorf("agent is already processing a prompt. Wait for completion")
	}
	return a.runLoop(ctx, msgs)
}

// Continue resumes from the existing context without adding a new message.
// The last message must not be an assistant message (it must be user or toolResult).
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
		return fmt.Errorf("cannot continue from message role: assistant")
	}

	return a.runLoop(ctx, nil)
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
func (a *Agent) runLoop(ctx context.Context, prompts []Message) error {
	runtimeModel := a.runtimeModel
	model := runtimeModel.Model
	if model == nil {
		return fmt.Errorf("no model configured")
	}
	msgs := append([]Message{}, a.messages...)
	mediaSupport := mediaSupportFromModelInfo(runtimeModel.ModelInfo)
	tools := AdaptTools(a.tools)
	systemPrompt := a.systemPrompt
	transformContext := composeTransformContext(a.transformContext, runtimeMediaTransform(mediaSupport, runtimeModel.EffectiveMaxImages()))
	done := make(chan struct{})
	runCtx, cancel := context.WithCancel(ctx)
	a.mu.Lock()
	a.isStreaming = true
	a.streamMsg = nil
	a.lastErr = nil
	a.runningDone = done
	a.cancel = cancel
	a.mu.Unlock()

	// Declared here so the defer closure below can read them after the goroutine writes them.
	var loopErr error
	var newMessages []Message

	// Cleanup when we exit: emit a terminal AgentEnd if the loop errored (so
	// subscribers always see one), then close all active subscriber channels so
	// callers blocked on range-evCh unblock without any special-casing.
	defer func() {
		cancel()
		if loopErr != nil {
			a.mu.Lock()
			a.lastErr = loopErr
			a.mu.Unlock()
			a.emit(AgentEvent{Type: AgentEventTypeAgentEnd, Messages: newMessages})
		}
		a.mu.Lock()
		a.isStreaming = false
		a.streamMsg = nil
		a.pendingCalls = make(map[string]struct{})
		a.cancel = nil
		a.runningDone = nil
		a.mu.Unlock()
		close(done)
		a.stopAllSubscribers()
	}()

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
		OnCompleteHook:   a.onCompleteHook,
	}

	// Event channel: we read events from it and update state + emit to subscribers.
	eventCh := make(chan AgentEvent, 64)

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

	return loopErr
}
