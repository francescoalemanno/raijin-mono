package libagent_test

import (
	"context"
	"iter"
	"testing"
	"time"

	"charm.land/fantasy"
	"github.com/francescoalemanno/raijin-mono/libagent"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---- mock language model -------------------------------------------------------

type mockModel struct {
	calls   int
	respFns []func(call int) fantasy.StreamResponse
}

func newMockModel(fns ...func(call int) fantasy.StreamResponse) *mockModel {
	return &mockModel{respFns: fns}
}

func (m *mockModel) Stream(_ context.Context, _ fantasy.Call) (fantasy.StreamResponse, error) {
	idx := m.calls
	m.calls++
	fn := m.respFns[min(idx, len(m.respFns)-1)]
	return fn(idx), nil
}

func (m *mockModel) Generate(_ context.Context, _ fantasy.Call) (*fantasy.Response, error) {
	return nil, nil
}

func (m *mockModel) GenerateObject(_ context.Context, _ fantasy.ObjectCall) (*fantasy.ObjectResponse, error) {
	return nil, nil
}

func (m *mockModel) StreamObject(_ context.Context, _ fantasy.ObjectCall) (fantasy.ObjectStreamResponse, error) {
	return nil, nil
}
func (m *mockModel) Provider() string { return "mock" }
func (m *mockModel) Model() string    { return "mock" }

type streamErrorModel struct {
	err error
}

func (m *streamErrorModel) Stream(_ context.Context, _ fantasy.Call) (fantasy.StreamResponse, error) {
	return nil, m.err
}

func (m *streamErrorModel) Generate(_ context.Context, _ fantasy.Call) (*fantasy.Response, error) {
	return nil, nil
}

func (m *streamErrorModel) GenerateObject(_ context.Context, _ fantasy.ObjectCall) (*fantasy.ObjectResponse, error) {
	return nil, nil
}

func (m *streamErrorModel) StreamObject(_ context.Context, _ fantasy.ObjectCall) (fantasy.ObjectStreamResponse, error) {
	return nil, nil
}

func (m *streamErrorModel) Provider() string { return "mock" }
func (m *streamErrorModel) Model() string    { return "mock" }

// textResponse creates a StreamResponse that yields one text block then stops.
func textResponse(text string) func(int) fantasy.StreamResponse {
	return func(_ int) fantasy.StreamResponse {
		return iter.Seq[fantasy.StreamPart](func(yield func(fantasy.StreamPart) bool) {
			if !yield(fantasy.StreamPart{Type: fantasy.StreamPartTypeTextStart, ID: "t1"}) {
				return
			}
			if !yield(fantasy.StreamPart{Type: fantasy.StreamPartTypeTextDelta, ID: "t1", Delta: text}) {
				return
			}
			if !yield(fantasy.StreamPart{Type: fantasy.StreamPartTypeTextEnd, ID: "t1"}) {
				return
			}
			yield(fantasy.StreamPart{
				Type:         fantasy.StreamPartTypeFinish,
				FinishReason: fantasy.FinishReasonStop,
			})
		})
	}
}

// toolCallResponse creates a StreamResponse that requests one tool call.
func toolCallResponse(toolCallID, toolName, input string) func(int) fantasy.StreamResponse {
	return func(_ int) fantasy.StreamResponse {
		return iter.Seq[fantasy.StreamPart](func(yield func(fantasy.StreamPart) bool) {
			if !yield(fantasy.StreamPart{
				Type:          fantasy.StreamPartTypeToolCall,
				ID:            toolCallID,
				ToolCallName:  toolName,
				ToolCallInput: input,
			}) {
				return
			}
			yield(fantasy.StreamPart{
				Type:         fantasy.StreamPartTypeFinish,
				FinishReason: fantasy.FinishReasonToolCalls,
			})
		})
	}
}

// toolInputOnlyResponse creates a response that streams tool input parts but
// does not emit an explicit StreamPartTypeToolCall event.
func toolInputOnlyResponse(toolCallID, toolName, input string) func(int) fantasy.StreamResponse {
	return func(_ int) fantasy.StreamResponse {
		return iter.Seq[fantasy.StreamPart](func(yield func(fantasy.StreamPart) bool) {
			if !yield(fantasy.StreamPart{Type: fantasy.StreamPartTypeToolInputStart, ID: toolCallID, ToolCallName: toolName}) {
				return
			}
			if !yield(fantasy.StreamPart{Type: fantasy.StreamPartTypeToolInputDelta, ID: toolCallID, Delta: input}) {
				return
			}
			if !yield(fantasy.StreamPart{Type: fantasy.StreamPartTypeToolInputEnd, ID: toolCallID}) {
				return
			}
			yield(fantasy.StreamPart{
				Type:         fantasy.StreamPartTypeFinish,
				FinishReason: fantasy.FinishReasonToolCalls,
			})
		})
	}
}

// twoToolCallsResponse creates a response with two sequential tool calls.
func twoToolCallsResponse(id1, name1, input1, id2, name2, input2 string) func(int) fantasy.StreamResponse {
	return func(_ int) fantasy.StreamResponse {
		return iter.Seq[fantasy.StreamPart](func(yield func(fantasy.StreamPart) bool) {
			if !yield(fantasy.StreamPart{Type: fantasy.StreamPartTypeToolCall, ID: id1, ToolCallName: name1, ToolCallInput: input1}) {
				return
			}
			if !yield(fantasy.StreamPart{Type: fantasy.StreamPartTypeToolCall, ID: id2, ToolCallName: name2, ToolCallInput: input2}) {
				return
			}
			yield(fantasy.StreamPart{Type: fantasy.StreamPartTypeFinish, FinishReason: fantasy.FinishReasonToolCalls})
		})
	}
}

// collectEvents reads all events from an agent subscription until agent_end.
func collectEvents(ch <-chan libagent.AgentEvent) []libagent.AgentEvent {
	var events []libagent.AgentEvent
	for e := range ch {
		events = append(events, e)
		if e.Type == libagent.AgentEventTypeAgentEnd {
			break
		}
	}
	return events
}

func eventTypes(events []libagent.AgentEvent) []libagent.AgentEventType {
	out := make([]libagent.AgentEventType, len(events))
	for i, e := range events {
		out[i] = e.Type
	}
	return out
}

// ---- tests ---------------------------------------------------------------------

func TestAgent_DefaultState(t *testing.T) {
	model := newMockModel()
	a := libagent.NewAgent(libagent.AgentOptions{RuntimeModel: libagent.RuntimeModel{Model: model}})

	s := a.State()
	assert.Equal(t, "", s.SystemPrompt)
	assert.NotNil(t, s.Model)
	assert.Empty(t, s.Tools)
	assert.Empty(t, s.Messages)
	assert.False(t, s.IsStreaming)
	assert.Nil(t, s.StreamMessage)
	assert.Empty(t, s.PendingToolCalls)
	assert.Nil(t, s.Error)
}

func TestAgent_Mutators(t *testing.T) {
	model := newMockModel()
	a := libagent.NewAgent(libagent.AgentOptions{RuntimeModel: libagent.RuntimeModel{Model: model}})

	a.SetSystemPrompt("hello")
	assert.Equal(t, "hello", a.State().SystemPrompt)

	model2 := newMockModel()
	a.SetRuntimeModel(libagent.RuntimeModel{Model: model2})
	assert.Equal(t, model2, a.State().Model)

	tool := libagent.NewTypedTool("t", "d", func(_ context.Context, _ struct{}, _ libagent.ToolCall) (libagent.ToolResponse, error) {
		return libagent.NewTextResponse("ok"), nil
	})
	a.SetTools([]libagent.Tool{tool})
	assert.Len(t, a.State().Tools, 1)

	msgs := []libagent.Message{&libagent.UserMessage{Role: "user", Content: "hi", Timestamp: time.Now()}}
	a.ReplaceMessages(msgs)
	assert.Len(t, a.State().Messages, 1)
	// Should be a copy.
	a.State().Messages[0] = nil
	assert.NotNil(t, a.State().Messages[0])

	a.AppendMessage(&libagent.UserMessage{Role: "user", Content: "bye", Timestamp: time.Now()})
	assert.Len(t, a.State().Messages, 2)

	a.ClearMessages()
	assert.Empty(t, a.State().Messages)

	a.SetSessionID("sess-1")
	assert.Equal(t, "sess-1", a.SessionID())
}

func TestAgent_QueueModes(t *testing.T) {
	model := newMockModel()
	a := libagent.NewAgent(libagent.AgentOptions{RuntimeModel: libagent.RuntimeModel{Model: model}})

	a.SetSteeringMode(libagent.QueueModeAll)
	assert.Equal(t, libagent.QueueModeAll, a.GetSteeringMode())

	a.SetFollowUpMode(libagent.QueueModeAll)
	assert.Equal(t, libagent.QueueModeAll, a.GetFollowUpMode())
}

func TestAgent_Queues(t *testing.T) {
	model := newMockModel()
	a := libagent.NewAgent(libagent.AgentOptions{RuntimeModel: libagent.RuntimeModel{Model: model}})

	assert.False(t, a.HasQueuedMessages())

	a.Steer(&libagent.UserMessage{Role: "user", Content: "steer", Timestamp: time.Now()})
	assert.True(t, a.HasQueuedMessages())
	assert.Empty(t, a.State().Messages) // not added to state yet

	a.FollowUp(&libagent.UserMessage{Role: "user", Content: "follow", Timestamp: time.Now()})

	a.ClearSteeringQueue()
	a.ClearFollowUpQueue()
	assert.False(t, a.HasQueuedMessages())

	a.Steer(&libagent.UserMessage{Role: "user", Content: "s", Timestamp: time.Now()})
	a.FollowUp(&libagent.UserMessage{Role: "user", Content: "f", Timestamp: time.Now()})
	a.ClearAllQueues()
	assert.False(t, a.HasQueuedMessages())
}

func TestAgent_Abort_NoOp(t *testing.T) {
	model := newMockModel()
	a := libagent.NewAgent(libagent.AgentOptions{RuntimeModel: libagent.RuntimeModel{Model: model}})
	// Should not panic when nothing is running.
	a.Abort()
}

func TestAgent_Prompt_BasicText(t *testing.T) {
	model := newMockModel(textResponse("Hi there!"))
	a := libagent.NewAgent(libagent.AgentOptions{
		RuntimeModel: libagent.RuntimeModel{Model: model},
		SystemPrompt: "You are helpful.",
	})

	ch, unsub := a.Subscribe()
	defer unsub()

	err := a.Prompt(context.Background(), "Hello")
	require.NoError(t, err)

	events := collectEvents(ch)
	types := eventTypes(events)

	assert.Contains(t, types, libagent.AgentEventTypeAgentStart)
	assert.Contains(t, types, libagent.AgentEventTypeTurnStart)
	assert.Contains(t, types, libagent.AgentEventTypeMessageStart)
	assert.Contains(t, types, libagent.AgentEventTypeMessageUpdate)
	assert.Contains(t, types, libagent.AgentEventTypeMessageEnd)
	assert.Contains(t, types, libagent.AgentEventTypeTurnEnd)
	assert.Contains(t, types, libagent.AgentEventTypeAgentEnd)

	s := a.State()
	assert.False(t, s.IsStreaming)
	assert.Len(t, s.Messages, 2) // user + assistant
	assert.Equal(t, "user", s.Messages[0].GetRole())
	assert.Equal(t, "assistant", s.Messages[1].GetRole())

	asst, ok := s.Messages[1].(*libagent.AssistantMessage)
	require.True(t, ok)
	assert.Equal(t, "Hi there!", asst.Content.Text())
}

func TestAgent_Prompt_WhileStreaming_Errors(t *testing.T) {
	// A model that blocks until we abort.
	blockCh := make(chan struct{})
	model := newMockModel(func(_ int) fantasy.StreamResponse {
		return iter.Seq[fantasy.StreamPart](func(yield func(fantasy.StreamPart) bool) {
			<-blockCh // block until closed
			yield(fantasy.StreamPart{Type: fantasy.StreamPartTypeFinish, FinishReason: fantasy.FinishReasonStop})
		})
	})

	a := libagent.NewAgent(libagent.AgentOptions{RuntimeModel: libagent.RuntimeModel{Model: model}})

	promptDone := make(chan error, 1)
	go func() {
		promptDone <- a.Prompt(context.Background(), "First")
	}()

	// Wait until streaming starts.
	require.Eventually(t, func() bool { return a.State().IsStreaming }, time.Second, 10*time.Millisecond)

	err := a.Prompt(context.Background(), "Second")
	assert.ErrorContains(t, err, "already processing")

	close(blockCh)
	<-promptDone
}

func TestAgent_Continue_NoMessages_Error(t *testing.T) {
	model := newMockModel()
	a := libagent.NewAgent(libagent.AgentOptions{RuntimeModel: libagent.RuntimeModel{Model: model}})

	err := a.Continue(context.Background())
	assert.ErrorContains(t, err, "no messages to continue from")
}

func TestAgent_Continue_LastIsAssistant_Error(t *testing.T) {
	model := newMockModel()
	a := libagent.NewAgent(libagent.AgentOptions{RuntimeModel: libagent.RuntimeModel{Model: model}})

	a.ReplaceMessages([]libagent.Message{
		&libagent.AssistantMessage{Role: "assistant", Timestamp: time.Now()},
	})

	err := a.Continue(context.Background())
	assert.ErrorContains(t, err, "cannot continue from message role: assistant")
}

func TestAgent_Continue_FromUserMessage(t *testing.T) {
	model := newMockModel(textResponse("Continuing..."))
	a := libagent.NewAgent(libagent.AgentOptions{RuntimeModel: libagent.RuntimeModel{Model: model}})

	a.ReplaceMessages([]libagent.Message{
		&libagent.UserMessage{Role: "user", Content: "tell me something", Timestamp: time.Now()},
	})

	ch, unsub := a.Subscribe()
	defer unsub()

	err := a.Continue(context.Background())
	require.NoError(t, err)
	collectEvents(ch)

	s := a.State()
	assert.Len(t, s.Messages, 2)
	assert.Equal(t, "user", s.Messages[0].GetRole())
	assert.Equal(t, "assistant", s.Messages[1].GetRole())
}

func TestAgent_ToolExecution(t *testing.T) {
	executed := make([]string, 0)
	echoTool := libagent.NewTypedTool(
		"echo",
		"Echo the value back",
		func(_ context.Context, input struct {
			Value string `json:"value"`
		}, _ libagent.ToolCall,
		) (libagent.ToolResponse, error) {
			executed = append(executed, input.Value)
			return libagent.NewTextResponse("echoed: " + input.Value), nil
		},
	)

	// First call: tool call. Second call: final text.
	model := newMockModel(
		toolCallResponse("tc1", "echo", `{"value":"hello"}`),
		textResponse("Done!"),
	)

	a := libagent.NewAgent(libagent.AgentOptions{
		RuntimeModel: libagent.RuntimeModel{Model: model},
		Tools:        []libagent.Tool{echoTool},
	})

	ch, unsub := a.Subscribe()
	defer unsub()

	err := a.Prompt(context.Background(), "Echo hello")
	require.NoError(t, err)

	events := collectEvents(ch)
	types := eventTypes(events)

	assert.Contains(t, types, libagent.AgentEventTypeToolExecutionStart)
	assert.Contains(t, types, libagent.AgentEventTypeToolExecutionEnd)

	assert.Equal(t, []string{"hello"}, executed)

	s := a.State()
	// user, assistant (with tool call), tool result, final assistant
	assert.GreaterOrEqual(t, len(s.Messages), 3)
	assert.Equal(t, "assistant", s.Messages[len(s.Messages)-1].GetRole())

	toolEndEvents := filterByType(events, libagent.AgentEventTypeToolExecutionEnd)
	require.Len(t, toolEndEvents, 1)
	assert.False(t, toolEndEvents[0].ToolIsError)
	assert.Equal(t, "echoed: hello", toolEndEvents[0].ToolResult)
}

func TestAgent_ToolExecution_FromToolInputOnlyStream(t *testing.T) {
	executed := make([]string, 0)
	echoTool := libagent.NewTypedTool(
		"echo",
		"Echo the value back",
		func(_ context.Context, input struct {
			Value string `json:"value"`
		}, _ libagent.ToolCall,
		) (libagent.ToolResponse, error) {
			executed = append(executed, input.Value)
			return libagent.NewTextResponse("echoed: " + input.Value), nil
		},
	)

	model := newMockModel(
		toolInputOnlyResponse("tc1", "echo", `{"value":"hello"}`),
		textResponse("Done!"),
	)

	a := libagent.NewAgent(libagent.AgentOptions{
		RuntimeModel: libagent.RuntimeModel{Model: model},
		Tools:        []libagent.Tool{echoTool},
	})

	ch, unsub := a.Subscribe()
	defer unsub()

	err := a.Prompt(context.Background(), "Echo hello")
	require.NoError(t, err)
	events := collectEvents(ch)

	assert.Equal(t, []string{"hello"}, executed)

	toolEndEvents := filterByType(events, libagent.AgentEventTypeToolExecutionEnd)
	require.Len(t, toolEndEvents, 1)
	assert.False(t, toolEndEvents[0].ToolIsError)
	assert.Equal(t, "echoed: hello", toolEndEvents[0].ToolResult)
}

func TestAgent_FinishReasonToolCallsWithoutToolCalls_Errors(t *testing.T) {
	model := newMockModel(func(_ int) fantasy.StreamResponse {
		return iter.Seq[fantasy.StreamPart](func(yield func(fantasy.StreamPart) bool) {
			yield(fantasy.StreamPart{
				Type:         fantasy.StreamPartTypeFinish,
				FinishReason: fantasy.FinishReasonToolCalls,
			})
		})
	})

	a := libagent.NewAgent(libagent.AgentOptions{RuntimeModel: libagent.RuntimeModel{Model: model}})
	err := a.Prompt(context.Background(), "do something")
	require.ErrorContains(t, err, "requested tool calls but returned none")
}

func TestAgent_ReasoningOnlyStop_Errors(t *testing.T) {
	model := newMockModel(func(_ int) fantasy.StreamResponse {
		return iter.Seq[fantasy.StreamPart](func(yield func(fantasy.StreamPart) bool) {
			if !yield(fantasy.StreamPart{Type: fantasy.StreamPartTypeReasoningStart, ID: "r1"}) {
				return
			}
			if !yield(fantasy.StreamPart{Type: fantasy.StreamPartTypeReasoningDelta, ID: "r1", Delta: "thinking only"}) {
				return
			}
			if !yield(fantasy.StreamPart{Type: fantasy.StreamPartTypeReasoningEnd, ID: "r1"}) {
				return
			}
			yield(fantasy.StreamPart{
				Type:         fantasy.StreamPartTypeFinish,
				FinishReason: fantasy.FinishReasonStop,
			})
		})
	})

	a := libagent.NewAgent(libagent.AgentOptions{RuntimeModel: libagent.RuntimeModel{Model: model}})
	err := a.Prompt(context.Background(), "answer")
	require.ErrorContains(t, err, "returned no final response")
}

func TestAgent_ToolNotFound(t *testing.T) {
	model := newMockModel(
		toolCallResponse("tc1", "nonexistent", `{}`),
		textResponse("I see."),
	)

	a := libagent.NewAgent(libagent.AgentOptions{RuntimeModel: libagent.RuntimeModel{Model: model}})

	ch, unsub := a.Subscribe()
	defer unsub()

	err := a.Prompt(context.Background(), "Do something")
	require.NoError(t, err)
	events := collectEvents(ch)

	toolEndEvents := filterByType(events, libagent.AgentEventTypeToolExecutionEnd)
	require.Len(t, toolEndEvents, 1)
	assert.True(t, toolEndEvents[0].ToolIsError)
	assert.Contains(t, toolEndEvents[0].ToolResult, "nonexistent")
}

func TestAgentLoop_SteeringSkipsRemainingToolCalls(t *testing.T) {
	executedTools := make([]string, 0)
	echoTool := libagent.NewTypedTool(
		"echo",
		"Echo",
		func(_ context.Context, input struct {
			Value string `json:"value"`
		}, _ libagent.ToolCall,
		) (libagent.ToolResponse, error) {
			executedTools = append(executedTools, input.Value)
			return libagent.NewTextResponse("ok:" + input.Value), nil
		},
	)

	callIdx := 0
	model := newMockModel(
		func(call int) fantasy.StreamResponse {
			callIdx++
			if call == 0 {
				return twoToolCallsResponse("tc1", "echo", `{"value":"first"}`, "tc2", "echo", `{"value":"second"}`)(call)
			}
			return textResponse("Interrupted!")(call)
		},
	)

	steeringDelivered := false
	steerMsg := &libagent.UserMessage{Role: "user", Content: "interrupt!", Timestamp: time.Now()}

	agentCtx := &libagent.AgentContext{
		Tools: libagent.AdaptTools([]libagent.Tool{echoTool}),
	}
	cfg := libagent.AgentLoopConfig{
		Model: model,
		// Return steering message after first tool executes.
		GetSteeringMessages: func(_ context.Context) ([]libagent.Message, error) {
			if len(executedTools) == 1 && !steeringDelivered {
				steeringDelivered = true
				return []libagent.Message{steerMsg}, nil
			}
			return nil, nil
		},
	}

	eventCh := make(chan libagent.AgentEvent, 128)
	prompt := &libagent.UserMessage{Role: "user", Content: "run two tools", Timestamp: time.Now()}
	msgs, err := libagent.AgentLoop(context.Background(), []libagent.Message{prompt}, agentCtx, cfg, eventCh)
	close(eventCh)

	require.NoError(t, err)

	var events []libagent.AgentEvent
	for e := range eventCh {
		events = append(events, e)
	}

	// Only the first tool should have run.
	assert.Equal(t, []string{"first"}, executedTools)

	// Second tool should be skipped (isError=true).
	toolEnds := filterByType(events, libagent.AgentEventTypeToolExecutionEnd)
	require.Len(t, toolEnds, 2)
	assert.False(t, toolEnds[0].ToolIsError)
	assert.True(t, toolEnds[1].ToolIsError)
	assert.Contains(t, toolEnds[1].ToolResult, "Skipped")

	// Interrupt message should appear in events.
	messageStarts := filterByType(events, libagent.AgentEventTypeMessageStart)
	found := false
	for _, e := range messageStarts {
		if um, ok := e.Message.(*libagent.UserMessage); ok && um.Content == "interrupt!" {
			found = true
		}
	}
	assert.True(t, found, "interrupt message should be in events")
	assert.True(t, len(msgs) > 0)
}

func TestAgent_FollowUpMessages(t *testing.T) {
	callCount := 0
	model := newMockModel(func(call int) fantasy.StreamResponse {
		callCount++
		return textResponse("response")(call)
	})

	a := libagent.NewAgent(libagent.AgentOptions{
		RuntimeModel: libagent.RuntimeModel{Model: model},
		FollowUpMode: libagent.QueueModeOneAtATime,
	})

	// Queue one follow-up.
	followUp := &libagent.UserMessage{Role: "user", Content: "follow", Timestamp: time.Now()}
	a.FollowUp(followUp)

	ch, unsub := a.Subscribe()
	defer unsub()

	err := a.Prompt(context.Background(), "first prompt")
	require.NoError(t, err)
	collectEvents(ch)

	// Should have called the model twice (once for prompt, once for follow-up).
	assert.Equal(t, 2, callCount)

	s := a.State()
	// user, asst, follow-up user, asst
	assert.Len(t, s.Messages, 4)
}

func TestAgent_MultiTurnConversation(t *testing.T) {
	model := newMockModel(textResponse("Alice"))

	a := libagent.NewAgent(libagent.AgentOptions{
		RuntimeModel: libagent.RuntimeModel{Model: model},
		SystemPrompt: "You remember names.",
	})

	ch, unsub := a.Subscribe()
	defer unsub()

	err := a.Prompt(context.Background(), "My name is Alice.")
	require.NoError(t, err)
	collectEvents(ch)
	assert.Len(t, a.State().Messages, 2)

	// Second prompt.
	model2 := newMockModel(textResponse("Your name is Alice."))
	a.SetRuntimeModel(libagent.RuntimeModel{Model: model2})

	ch2, unsub2 := a.Subscribe()
	defer unsub2()

	err = a.Prompt(context.Background(), "What is my name?")
	require.NoError(t, err)
	collectEvents(ch2)

	assert.Len(t, a.State().Messages, 4)
}

func TestAgent_CustomConvertToLLM(t *testing.T) {
	var capturedMessages []libagent.Message
	model := newMockModel(textResponse("ok"))

	a := libagent.NewAgent(libagent.AgentOptions{
		RuntimeModel: libagent.RuntimeModel{Model: model},
		ConvertToLLM: func(_ context.Context, msgs []libagent.Message) ([]fantasy.Message, error) {
			capturedMessages = msgs
			return libagent.DefaultConvertToLLM(context.Background(), msgs)
		},
	})

	// Add a custom message type (wrapped as UserMessage for simplicity) that
	// should be accessible via capturedMessages.
	a.ReplaceMessages([]libagent.Message{
		&libagent.UserMessage{Role: "user", Content: "initial", Timestamp: time.Now()},
	})

	ch, unsub := a.Subscribe()
	defer unsub()

	err := a.Prompt(context.Background(), "test")
	require.NoError(t, err)
	collectEvents(ch)

	// The captured messages should include the initial user message + the new prompt.
	assert.GreaterOrEqual(t, len(capturedMessages), 1)
}

func TestAgent_TransformContext(t *testing.T) {
	var transformedLen int
	model := newMockModel(textResponse("ok"))

	a := libagent.NewAgent(libagent.AgentOptions{
		RuntimeModel: libagent.RuntimeModel{Model: model},
		TransformContext: func(_ context.Context, msgs []libagent.Message) ([]libagent.Message, error) {
			// Keep only last 2 messages.
			if len(msgs) > 2 {
				msgs = msgs[len(msgs)-2:]
			}
			transformedLen = len(msgs)
			return msgs, nil
		},
	})

	// Pre-fill history with 4 messages.
	a.ReplaceMessages([]libagent.Message{
		&libagent.UserMessage{Role: "user", Content: "a", Timestamp: time.Now()},
		&libagent.AssistantMessage{Role: "assistant", Timestamp: time.Now()},
		&libagent.UserMessage{Role: "user", Content: "b", Timestamp: time.Now()},
		&libagent.AssistantMessage{Role: "assistant", Timestamp: time.Now()},
	})

	ch, unsub := a.Subscribe()
	defer unsub()

	err := a.Prompt(context.Background(), "new prompt")
	require.NoError(t, err)
	collectEvents(ch)

	// After pruning to last 2 from 5 total (4 history + 1 new prompt) = 2.
	assert.Equal(t, 2, transformedLen)
}

func TestAgent_WaitForIdle(t *testing.T) {
	done := make(chan struct{})
	model := newMockModel(func(_ int) fantasy.StreamResponse {
		return iter.Seq[fantasy.StreamPart](func(yield func(fantasy.StreamPart) bool) {
			// wait until the test signals
			<-done
			yield(fantasy.StreamPart{Type: fantasy.StreamPartTypeFinish, FinishReason: fantasy.FinishReasonStop})
		})
	})

	a := libagent.NewAgent(libagent.AgentOptions{RuntimeModel: libagent.RuntimeModel{Model: model}})

	promptDone := make(chan error, 1)
	go func() {
		promptDone <- a.Prompt(context.Background(), "hi")
	}()

	require.Eventually(t, func() bool { return a.State().IsStreaming }, time.Second, 10*time.Millisecond)

	// Close done to let the model respond.
	close(done)

	a.WaitForIdle()
	assert.False(t, a.State().IsStreaming)
	<-promptDone
}

func TestAgent_Abort(t *testing.T) {
	blockCh := make(chan struct{})
	model := newMockModel(func(_ int) fantasy.StreamResponse {
		return iter.Seq[fantasy.StreamPart](func(yield func(fantasy.StreamPart) bool) {
			select {
			case <-blockCh:
			}
			// After abort, just return error finish.
			yield(fantasy.StreamPart{Type: fantasy.StreamPartTypeFinish, FinishReason: fantasy.FinishReasonError})
		})
	})

	a := libagent.NewAgent(libagent.AgentOptions{RuntimeModel: libagent.RuntimeModel{Model: model}})

	promptDone := make(chan error, 1)
	go func() {
		promptDone <- a.Prompt(context.Background(), "hi")
	}()

	require.Eventually(t, func() bool { return a.State().IsStreaming }, time.Second, 10*time.Millisecond)

	a.Abort()
	close(blockCh)
	a.WaitForIdle()
	<-promptDone
	assert.False(t, a.State().IsStreaming)
}

func TestAgent_Continue_WithFollowUpFromAssistantTail(t *testing.T) {
	callCount := 0
	model := newMockModel(func(call int) fantasy.StreamResponse {
		callCount++
		return textResponse("processed")(call)
	})

	a := libagent.NewAgent(libagent.AgentOptions{RuntimeModel: libagent.RuntimeModel{Model: model}})

	// Seed with user + assistant messages.
	a.ReplaceMessages([]libagent.Message{
		&libagent.UserMessage{Role: "user", Content: "initial", Timestamp: time.Now()},
		&libagent.AssistantMessage{Role: "assistant", Timestamp: time.Now()},
	})

	// Queue follow-up.
	a.FollowUp(&libagent.UserMessage{Role: "user", Content: "follow-up", Timestamp: time.Now()})

	ch, unsub := a.Subscribe()
	defer unsub()

	err := a.Continue(context.Background())
	require.NoError(t, err)
	collectEvents(ch)

	assert.Equal(t, 1, callCount) // follow-up drains in one call
	s := a.State()
	// initial user, initial asst, follow-up user, response asst
	assert.Len(t, s.Messages, 4)
	assert.Equal(t, "assistant", s.Messages[len(s.Messages)-1].GetRole())
}

func TestAgent_SteeringMode_OneAtATime(t *testing.T) {
	callCount := 0
	model := newMockModel(func(call int) fantasy.StreamResponse {
		callCount++
		return textResponse("ok")(call)
	})

	a := libagent.NewAgent(libagent.AgentOptions{
		RuntimeModel: libagent.RuntimeModel{Model: model},
		SteeringMode: libagent.QueueModeOneAtATime,
	})

	// Seed with assistant tail.
	a.ReplaceMessages([]libagent.Message{
		&libagent.UserMessage{Role: "user", Content: "u", Timestamp: time.Now()},
		&libagent.AssistantMessage{Role: "assistant", Timestamp: time.Now()},
	})

	a.Steer(&libagent.UserMessage{Role: "user", Content: "s1", Timestamp: time.Now()})
	a.Steer(&libagent.UserMessage{Role: "user", Content: "s2", Timestamp: time.Now()})

	ch, unsub := a.Subscribe()
	defer unsub()

	err := a.Continue(context.Background())
	require.NoError(t, err)
	collectEvents(ch)

	// Both steering messages are delivered (one at a time per turn) → 2 LLM calls.
	assert.Equal(t, 2, callCount)
	assert.False(t, a.HasQueuedMessages())
}

func TestAgentLoop_BasicEvents(t *testing.T) {
	model := newMockModel(textResponse("hello"))

	agentCtx := &libagent.AgentContext{
		SystemPrompt: "sys",
		Messages:     nil,
		Tools:        nil,
	}
	cfg := libagent.AgentLoopConfig{
		Model: model,
	}

	eventCh := make(chan libagent.AgentEvent, 64)
	prompts := []libagent.Message{&libagent.UserMessage{Role: "user", Content: "hi", Timestamp: time.Now()}}

	msgs, err := libagent.AgentLoop(context.Background(), prompts, agentCtx, cfg, eventCh)
	close(eventCh)

	require.NoError(t, err)
	assert.Len(t, msgs, 2)
	assert.Equal(t, "user", msgs[0].GetRole())
	assert.Equal(t, "assistant", msgs[1].GetRole())

	var events []libagent.AgentEvent
	for e := range eventCh {
		events = append(events, e)
	}
	types := eventTypes(events)
	assert.Contains(t, types, libagent.AgentEventTypeAgentStart)
	assert.Contains(t, types, libagent.AgentEventTypeAgentEnd)
}

func TestAgentLoop_ErrorStillEmitsAgentEnd(t *testing.T) {
	model := newMockModel()
	agentCtx := &libagent.AgentContext{}
	cfg := libagent.AgentLoopConfig{
		Model: model,
		GetSteeringMessages: func(context.Context) ([]libagent.Message, error) {
			return nil, assert.AnError
		},
	}

	eventCh := make(chan libagent.AgentEvent, 64)
	prompts := []libagent.Message{&libagent.UserMessage{Role: "user", Content: "hi", Timestamp: time.Now()}}

	_, err := libagent.AgentLoop(context.Background(), prompts, agentCtx, cfg, eventCh)
	close(eventCh)
	require.Error(t, err)

	var events []libagent.AgentEvent
	for e := range eventCh {
		events = append(events, e)
	}
	types := eventTypes(events)
	assert.Contains(t, types, libagent.AgentEventTypeAgentStart)
	assert.Contains(t, types, libagent.AgentEventTypeAgentEnd)
}

func TestAgentLoopContinue_NoMessages_Error(t *testing.T) {
	model := newMockModel()
	agentCtx := &libagent.AgentContext{}
	cfg := libagent.AgentLoopConfig{Model: model}
	eventCh := make(chan libagent.AgentEvent, 64)

	_, err := libagent.AgentLoopContinue(context.Background(), agentCtx, cfg, eventCh)
	close(eventCh)
	require.ErrorContains(t, err, "no messages in context")
	var events []libagent.AgentEvent
	for e := range eventCh {
		events = append(events, e)
	}
	types := eventTypes(events)
	assert.Contains(t, types, libagent.AgentEventTypeAgentStart)
	assert.Contains(t, types, libagent.AgentEventTypeAgentEnd)
}

func TestAgentLoopContinue_AssistantLastMessage_Error(t *testing.T) {
	model := newMockModel()
	agentCtx := &libagent.AgentContext{
		Messages: []libagent.Message{
			&libagent.AssistantMessage{Role: "assistant", Timestamp: time.Now()},
		},
	}
	cfg := libagent.AgentLoopConfig{Model: model}
	eventCh := make(chan libagent.AgentEvent, 64)

	_, err := libagent.AgentLoopContinue(context.Background(), agentCtx, cfg, eventCh)
	close(eventCh)
	require.ErrorContains(t, err, "cannot continue from message role: assistant")
	var events []libagent.AgentEvent
	for e := range eventCh {
		events = append(events, e)
	}
	types := eventTypes(events)
	assert.Contains(t, types, libagent.AgentEventTypeAgentStart)
	assert.Contains(t, types, libagent.AgentEventTypeAgentEnd)
}

func TestAgentLoopContinue_FromUserMessage(t *testing.T) {
	model := newMockModel(textResponse("I continued"))

	agentCtx := &libagent.AgentContext{
		Messages: []libagent.Message{
			&libagent.UserMessage{Role: "user", Content: "continue from here", Timestamp: time.Now()},
		},
	}
	cfg := libagent.AgentLoopConfig{Model: model}
	eventCh := make(chan libagent.AgentEvent, 64)

	msgs, err := libagent.AgentLoopContinue(context.Background(), agentCtx, cfg, eventCh)
	close(eventCh)

	require.NoError(t, err)
	assert.Len(t, msgs, 1) // only the new assistant message
	assert.Equal(t, "assistant", msgs[0].GetRole())
}

func TestAgentLoopContinue_ErrorStillEmitsAgentEnd(t *testing.T) {
	model := newMockModel()
	agentCtx := &libagent.AgentContext{
		Messages: []libagent.Message{
			&libagent.UserMessage{Role: "user", Content: "continue from here", Timestamp: time.Now()},
		},
	}
	cfg := libagent.AgentLoopConfig{
		Model: model,
		GetSteeringMessages: func(context.Context) ([]libagent.Message, error) {
			return nil, assert.AnError
		},
	}
	eventCh := make(chan libagent.AgentEvent, 64)

	_, err := libagent.AgentLoopContinue(context.Background(), agentCtx, cfg, eventCh)
	close(eventCh)
	require.Error(t, err)

	var events []libagent.AgentEvent
	for e := range eventCh {
		events = append(events, e)
	}
	types := eventTypes(events)
	assert.Contains(t, types, libagent.AgentEventTypeAgentStart)
	assert.Contains(t, types, libagent.AgentEventTypeAgentEnd)
}

func TestAgent_ToolExecutionUpdate_StreamingTool(t *testing.T) {
	var emittedUpdates []string
	stool := newStreamingProgressTool(&emittedUpdates)

	model := newMockModel(
		toolCallResponse("tc1", "progress", `{}`),
		textResponse("great"),
	)

	agentCtx := &libagent.AgentContext{
		Tools: []fantasy.AgentTool{stool},
	}
	cfg := libagent.AgentLoopConfig{Model: model}
	eventCh := make(chan libagent.AgentEvent, 128)
	prompt := &libagent.UserMessage{Role: "user", Content: "run the progress tool", Timestamp: time.Now()}

	_, err := libagent.AgentLoop(context.Background(), []libagent.Message{prompt}, agentCtx, cfg, eventCh)
	require.NoError(t, err)
	close(eventCh)

	var events []libagent.AgentEvent
	for e := range eventCh {
		events = append(events, e)
	}

	// tool_execution_update events should be emitted in order.
	updateEvents := filterByType(events, libagent.AgentEventTypeToolExecutionUpdate)
	require.Len(t, updateEvents, 2)
	assert.Equal(t, "step 1", updateEvents[0].ToolResult)
	assert.Equal(t, "step 2", updateEvents[1].ToolResult)
	assert.False(t, updateEvents[0].ToolIsError)

	// tool_execution_end should carry the final result, not a partial.
	endEvents := filterByType(events, libagent.AgentEventTypeToolExecutionEnd)
	require.Len(t, endEvents, 1)
	assert.Equal(t, "final", endEvents[0].ToolResult)
	assert.False(t, endEvents[0].ToolIsError)

	// The onUpdate callback was actually invoked.
	assert.Equal(t, []string{"step 1", "step 2"}, emittedUpdates)
}

// streamingProgressTool is a concrete StreamingAgentTool for testing.
type streamingProgressTool struct {
	inner   fantasy.AgentTool
	updates *[]string
}

func newStreamingProgressTool(updates *[]string) *streamingProgressTool {
	inner := fantasy.NewAgentTool(
		"progress",
		"A tool that streams progress",
		func(_ context.Context, _ struct{}, _ fantasy.ToolCall) (fantasy.ToolResponse, error) {
			return fantasy.NewTextResponse("final"), nil
		},
	)
	return &streamingProgressTool{inner: inner, updates: updates}
}

func (s *streamingProgressTool) Info() fantasy.ToolInfo { return s.inner.Info() }
func (s *streamingProgressTool) ProviderOptions() fantasy.ProviderOptions {
	return s.inner.ProviderOptions()
}

func (s *streamingProgressTool) SetProviderOptions(o fantasy.ProviderOptions) {
	s.inner.SetProviderOptions(o)
}

func (s *streamingProgressTool) Run(ctx context.Context, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
	return s.inner.Run(ctx, call)
}

func (s *streamingProgressTool) RunStreaming(ctx context.Context, call fantasy.ToolCall, onUpdate libagent.ToolUpdateFn) (fantasy.ToolResponse, error) {
	onUpdate(fantasy.NewTextResponse("step 1"))
	*s.updates = append(*s.updates, "step 1")
	onUpdate(fantasy.NewTextResponse("step 2"))
	*s.updates = append(*s.updates, "step 2")
	return fantasy.NewTextResponse("final"), nil
}

func TestAgent_Reset(t *testing.T) {
	model := newMockModel(textResponse("hi"))
	a := libagent.NewAgent(libagent.AgentOptions{RuntimeModel: libagent.RuntimeModel{Model: model}})

	// Build up some state.
	err := a.Prompt(context.Background(), "hello")
	require.NoError(t, err)
	require.Len(t, a.State().Messages, 2)

	a.Steer(&libagent.UserMessage{Role: "user", Content: "s", Timestamp: time.Now()})
	a.FollowUp(&libagent.UserMessage{Role: "user", Content: "f", Timestamp: time.Now()})
	assert.True(t, a.HasQueuedMessages())

	a.Reset()

	s := a.State()
	assert.Empty(t, s.Messages)
	assert.False(t, s.IsStreaming)
	assert.Nil(t, s.StreamMessage)
	assert.Empty(t, s.PendingToolCalls)
	assert.Nil(t, s.Error)
	assert.False(t, a.HasQueuedMessages())
}

func TestAgent_FollowUpMode_All(t *testing.T) {
	callCount := 0
	model := newMockModel(func(call int) fantasy.StreamResponse {
		callCount++
		return textResponse("ok")(call)
	})

	a := libagent.NewAgent(libagent.AgentOptions{
		RuntimeModel: libagent.RuntimeModel{Model: model},
		FollowUpMode: libagent.QueueModeAll,
	})

	a.FollowUp(&libagent.UserMessage{Role: "user", Content: "f1", Timestamp: time.Now()})
	a.FollowUp(&libagent.UserMessage{Role: "user", Content: "f2", Timestamp: time.Now()})

	ch, unsub := a.Subscribe()
	defer unsub()

	err := a.Prompt(context.Background(), "initial")
	require.NoError(t, err)
	collectEvents(ch)

	// QueueModeAll sends both follow-ups in a single turn → 2 LLM calls total
	// (1 for initial prompt, 1 for the batch of follow-ups).
	assert.Equal(t, 2, callCount)
	assert.False(t, a.HasQueuedMessages())
	// initial user, initial asst, f1, f2, follow-up asst
	assert.Len(t, a.State().Messages, 5)
}

func TestAgent_SteeringMode_All(t *testing.T) {
	callCount := 0
	model := newMockModel(func(call int) fantasy.StreamResponse {
		callCount++
		return textResponse("ok")(call)
	})

	a := libagent.NewAgent(libagent.AgentOptions{
		RuntimeModel: libagent.RuntimeModel{Model: model},
		SteeringMode: libagent.QueueModeAll,
	})

	a.ReplaceMessages([]libagent.Message{
		&libagent.UserMessage{Role: "user", Content: "u", Timestamp: time.Now()},
		&libagent.AssistantMessage{Role: "assistant", Timestamp: time.Now()},
	})

	a.Steer(&libagent.UserMessage{Role: "user", Content: "s1", Timestamp: time.Now()})
	a.Steer(&libagent.UserMessage{Role: "user", Content: "s2", Timestamp: time.Now()})

	ch, unsub := a.Subscribe()
	defer unsub()

	err := a.Continue(context.Background())
	require.NoError(t, err)
	collectEvents(ch)

	// QueueModeAll delivers both steering messages in one turn → 1 LLM call.
	assert.Equal(t, 1, callCount)
	assert.False(t, a.HasQueuedMessages())
	// initial user, initial asst, s1, s2, response asst
	assert.Len(t, a.State().Messages, 5)
}

func TestAgent_LLMError_FinishReasonError(t *testing.T) {
	model := newMockModel(func(_ int) fantasy.StreamResponse {
		return iter.Seq[fantasy.StreamPart](func(yield func(fantasy.StreamPart) bool) {
			yield(fantasy.StreamPart{Type: fantasy.StreamPartTypeFinish, FinishReason: fantasy.FinishReasonError})
		})
	})

	a := libagent.NewAgent(libagent.AgentOptions{RuntimeModel: libagent.RuntimeModel{Model: model}})

	ch, unsub := a.Subscribe()
	defer unsub()

	err := a.Prompt(context.Background(), "hi")
	require.ErrorContains(t, err, "language model finished with error")

	events := collectEvents(ch)
	types := eventTypes(events)

	// turn_end and agent_end should still fire.
	assert.Contains(t, types, libagent.AgentEventTypeTurnEnd)
	assert.Contains(t, types, libagent.AgentEventTypeAgentEnd)

	s := a.State()
	assert.False(t, s.IsStreaming)
	// assistant message with error finish should still be appended.
	require.Len(t, s.Messages, 2)
	asst, ok := s.Messages[1].(*libagent.AssistantMessage)
	require.True(t, ok)
	assert.Equal(t, fantasy.FinishReasonError, asst.FinishReason)
}

func TestAgent_StreamSetupError_EmitsAssistantErrorMessage(t *testing.T) {
	a := libagent.NewAgent(libagent.AgentOptions{
		RuntimeModel: libagent.RuntimeModel{Model: &streamErrorModel{err: assert.AnError}},
	})

	ch, unsub := a.Subscribe()
	defer unsub()

	err := a.Prompt(context.Background(), "hi")
	require.ErrorIs(t, err, assert.AnError)

	events := collectEvents(ch)
	types := eventTypes(events)
	assert.Contains(t, types, libagent.AgentEventTypeMessageStart)
	assert.Contains(t, types, libagent.AgentEventTypeMessageEnd)
	assert.Contains(t, types, libagent.AgentEventTypeTurnEnd)
	assert.Contains(t, types, libagent.AgentEventTypeAgentEnd)

	s := a.State()
	require.Len(t, s.Messages, 2)
	asst, ok := s.Messages[1].(*libagent.AssistantMessage)
	require.True(t, ok)
	assert.Equal(t, fantasy.FinishReasonError, asst.FinishReason)
	assert.ErrorIs(t, asst.Error, assert.AnError)
}

func TestAgentLoopContinue_FromToolResultMessage(t *testing.T) {
	model := newMockModel(textResponse("Done after tool result"))

	agentCtx := &libagent.AgentContext{
		Messages: []libagent.Message{
			&libagent.UserMessage{Role: "user", Content: "run tool", Timestamp: time.Now()},
			&libagent.AssistantMessage{Role: "assistant", Timestamp: time.Now()},
			&libagent.ToolResultMessage{
				Role:       "toolResult",
				ToolCallID: "tc1",
				ToolName:   "mytool",
				Content:    "result",
				Timestamp:  time.Now(),
			},
		},
	}
	cfg := libagent.AgentLoopConfig{Model: model}
	eventCh := make(chan libagent.AgentEvent, 64)

	msgs, err := libagent.AgentLoopContinue(context.Background(), agentCtx, cfg, eventCh)
	close(eventCh)

	require.NoError(t, err)
	require.Len(t, msgs, 1)
	assert.Equal(t, "assistant", msgs[0].GetRole())
	asst := msgs[0].(*libagent.AssistantMessage)
	assert.Equal(t, "Done after tool result", asst.Content.Text())
}

func TestAgent_PromptMessages_Multiple(t *testing.T) {
	model := newMockModel(textResponse("got it"))
	a := libagent.NewAgent(libagent.AgentOptions{RuntimeModel: libagent.RuntimeModel{Model: model}})

	ch, unsub := a.Subscribe()
	defer unsub()

	err := a.PromptMessages(
		context.Background(),
		&libagent.UserMessage{Role: "user", Content: "first part", Timestamp: time.Now()},
		&libagent.UserMessage{Role: "user", Content: "second part", Timestamp: time.Now()},
	)
	require.NoError(t, err)
	collectEvents(ch)

	s := a.State()
	// two user messages + one assistant response
	assert.Len(t, s.Messages, 3)
	assert.Equal(t, "user", s.Messages[0].GetRole())
	assert.Equal(t, "user", s.Messages[1].GetRole())
	assert.Equal(t, "assistant", s.Messages[2].GetRole())
}

// ---- helpers -------------------------------------------------------------------

func filterByType(events []libagent.AgentEvent, t libagent.AgentEventType) []libagent.AgentEvent {
	var out []libagent.AgentEvent
	for _, e := range events {
		if e.Type == t {
			out = append(out, e)
		}
	}
	return out
}
