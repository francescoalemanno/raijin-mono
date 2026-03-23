package libagent

import (
	"context"
	"errors"
	"testing"
	"time"

	"charm.land/fantasy"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIsRetryableError(t *testing.T) {
	// DNS error should be retryable
	dnsErr := errors.New(`Post "https://api.z.ai/api/coding/paas/v4/chat/completions": dial tcp: lookup api.z.ai: no such host`)
	assert.True(t, isRetryableError(dnsErr), "DNS error should be retryable")

	// Context canceled should not be retryable
	ctxErr := context.Canceled
	assert.False(t, isRetryableError(ctxErr), "Context canceled should not be retryable")

	// Context deadline exceeded should not be retryable
	deadlineErr := context.DeadlineExceeded
	assert.False(t, isRetryableError(deadlineErr), "Context deadline exceeded should not be retryable")

	// Connection reset should be retryable
	connErr := errors.New("connection reset by peer")
	assert.True(t, isRetryableError(connErr), "Connection reset should be retryable")

	// Nil error should not be retryable
	assert.False(t, isRetryableError(nil), "Nil error should not be retryable")
}

// mockFailingModel is a model that fails a specified number of times before succeeding
type mockFailingModel struct {
	failCount    int
	currentCalls int
	successResp  fantasy.StreamResponse
}

func (m *mockFailingModel) Stream(ctx context.Context, call fantasy.Call) (fantasy.StreamResponse, error) {
	m.currentCalls++
	if m.currentCalls <= m.failCount {
		return nil, errors.New("connection refused")
	}
	return m.successResp, nil
}

func (m *mockFailingModel) Generate(ctx context.Context, call fantasy.Call) (*fantasy.Response, error) {
	return nil, errors.New("not implemented")
}

func (m *mockFailingModel) GenerateObject(ctx context.Context, call fantasy.ObjectCall) (*fantasy.ObjectResponse, error) {
	return nil, errors.New("not implemented")
}

func (m *mockFailingModel) StreamObject(ctx context.Context, call fantasy.ObjectCall) (fantasy.ObjectStreamResponse, error) {
	return nil, errors.New("not implemented")
}

func (m *mockFailingModel) Provider() string { return "mock" }
func (m *mockFailingModel) Model() string    { return "mock-model" }

func TestAgentLoop_RetriesInitialConnectionError(t *testing.T) {
	// Test that initial connection failures are retried through the outer loop
	model := &mockFailingModel{
		failCount: 2,
		successResp: func(yield func(fantasy.StreamPart) bool) {
			yield(fantasy.StreamPart{Type: fantasy.StreamPartTypeTextStart, ID: "0"})
			yield(fantasy.StreamPart{Type: fantasy.StreamPartTypeTextDelta, ID: "0", Delta: "hello"})
			yield(fantasy.StreamPart{Type: fantasy.StreamPartTypeTextEnd, ID: "0"})
			yield(fantasy.StreamPart{Type: fantasy.StreamPartTypeFinish, FinishReason: fantasy.FinishReasonStop})
		},
	}
	eventCh := make(chan AgentEvent, 64)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	cfg := AgentLoopConfig{Model: model}
	agentCtx := &AgentContext{}
	prompts := []Message{&UserMessage{Role: "user", Content: "hi", Timestamp: time.Now()}}

	_, err := AgentLoop(ctx, prompts, agentCtx, cfg, eventCh)
	close(eventCh)

	// Should succeed after retries
	require.NoError(t, err)
	assert.Equal(t, 3, model.currentCalls, "Should have made 3 calls (2 failures + 1 success)")

	// Check retry events
	var retryEvents int
	for e := range eventCh {
		if e.Type == AgentEventTypeRetry {
			retryEvents++
		}
	}
	assert.Equal(t, 2, retryEvents, "Should have 2 retry events")
}

func TestAgentLoop_RetriesInitialConnectionAllFail(t *testing.T) {
	model := &mockFailingModel{failCount: 10}
	eventCh := make(chan AgentEvent, 64)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	cfg := AgentLoopConfig{Model: model}
	agentCtx := &AgentContext{}
	prompts := []Message{&UserMessage{Role: "user", Content: "hi", Timestamp: time.Now()}}

	_, err := AgentLoop(ctx, prompts, agentCtx, cfg, eventCh)
	close(eventCh)

	require.Error(t, err)
	// Should have tried maxRetries times
	assert.GreaterOrEqual(t, model.currentCalls, 2)
}

type mockStreamPartErrorModel struct {
	currentCalls int
}

func (m *mockStreamPartErrorModel) Stream(ctx context.Context, call fantasy.Call) (fantasy.StreamResponse, error) {
	m.currentCalls++
	if m.currentCalls == 1 {
		return func(yield func(fantasy.StreamPart) bool) {
			yield(fantasy.StreamPart{Type: fantasy.StreamPartTypeError, Error: errors.New("temporary stream failure")})
		}, nil
	}
	return func(yield func(fantasy.StreamPart) bool) {
		yield(fantasy.StreamPart{Type: fantasy.StreamPartTypeTextStart, ID: "0"})
		yield(fantasy.StreamPart{Type: fantasy.StreamPartTypeTextDelta, ID: "0", Delta: "hello"})
		yield(fantasy.StreamPart{Type: fantasy.StreamPartTypeTextEnd, ID: "0"})
		yield(fantasy.StreamPart{Type: fantasy.StreamPartTypeFinish, FinishReason: fantasy.FinishReasonStop})
	}, nil
}

func (m *mockStreamPartErrorModel) Generate(ctx context.Context, call fantasy.Call) (*fantasy.Response, error) {
	return nil, errors.New("not implemented")
}

func (m *mockStreamPartErrorModel) GenerateObject(ctx context.Context, call fantasy.ObjectCall) (*fantasy.ObjectResponse, error) {
	return nil, errors.New("not implemented")
}

func (m *mockStreamPartErrorModel) StreamObject(ctx context.Context, call fantasy.ObjectCall) (fantasy.ObjectStreamResponse, error) {
	return nil, errors.New("not implemented")
}

func (m *mockStreamPartErrorModel) Provider() string { return "mock" }
func (m *mockStreamPartErrorModel) Model() string    { return "mock-model" }

func TestAgentLoop_RetriesImmediateStreamPartError(t *testing.T) {
	model := &mockStreamPartErrorModel{}
	eventCh := make(chan AgentEvent, 64)
	cfg := AgentLoopConfig{Model: model}
	agentCtx := &AgentContext{}
	prompts := []Message{&UserMessage{Role: "user", Content: "hi", Timestamp: time.Now()}}

	_, err := AgentLoop(context.Background(), prompts, agentCtx, cfg, eventCh)
	close(eventCh)
	require.NoError(t, err)
	assert.Equal(t, 2, model.currentCalls)

	retryEvents := 0
	assistantEnds := 0
	for ev := range eventCh {
		if ev.Type == AgentEventTypeRetry {
			retryEvents++
		}
		if ev.Type == AgentEventTypeMessageEnd {
			if _, ok := ev.Message.(*AssistantMessage); ok {
				assistantEnds++
			}
		}
	}
	assert.Equal(t, 1, retryEvents)
	assert.Equal(t, 1, assistantEnds)
}

type mockTextStartThenErrorModel struct {
	currentCalls int
}

func (m *mockTextStartThenErrorModel) Stream(ctx context.Context, call fantasy.Call) (fantasy.StreamResponse, error) {
	m.currentCalls++
	if m.currentCalls == 1 {
		return func(yield func(fantasy.StreamPart) bool) {
			yield(fantasy.StreamPart{Type: fantasy.StreamPartTypeTextStart, ID: "0"})
			yield(fantasy.StreamPart{Type: fantasy.StreamPartTypeError, Error: errors.New("read tcp timeout")})
		}, nil
	}
	return func(yield func(fantasy.StreamPart) bool) {
		yield(fantasy.StreamPart{Type: fantasy.StreamPartTypeTextStart, ID: "0"})
		yield(fantasy.StreamPart{Type: fantasy.StreamPartTypeTextDelta, ID: "0", Delta: "ok"})
		yield(fantasy.StreamPart{Type: fantasy.StreamPartTypeTextEnd, ID: "0"})
		yield(fantasy.StreamPart{Type: fantasy.StreamPartTypeFinish, FinishReason: fantasy.FinishReasonStop})
	}, nil
}

func (m *mockTextStartThenErrorModel) Generate(ctx context.Context, call fantasy.Call) (*fantasy.Response, error) {
	return nil, errors.New("not implemented")
}

func (m *mockTextStartThenErrorModel) GenerateObject(ctx context.Context, call fantasy.ObjectCall) (*fantasy.ObjectResponse, error) {
	return nil, errors.New("not implemented")
}

func (m *mockTextStartThenErrorModel) StreamObject(ctx context.Context, call fantasy.ObjectCall) (fantasy.ObjectStreamResponse, error) {
	return nil, errors.New("not implemented")
}

func (m *mockTextStartThenErrorModel) Provider() string { return "mock" }
func (m *mockTextStartThenErrorModel) Model() string    { return "mock-model" }

func TestAgentLoop_RetriesTextStartThenError(t *testing.T) {
	model := &mockTextStartThenErrorModel{}
	eventCh := make(chan AgentEvent, 64)
	cfg := AgentLoopConfig{Model: model}
	agentCtx := &AgentContext{}
	prompts := []Message{&UserMessage{Role: "user", Content: "hi", Timestamp: time.Now()}}

	_, err := AgentLoop(context.Background(), prompts, agentCtx, cfg, eventCh)
	close(eventCh)
	require.NoError(t, err)
	assert.Equal(t, 2, model.currentCalls)
}

type mockTextWithoutFinishThenSuccessModel struct {
	currentCalls int
}

func (m *mockTextWithoutFinishThenSuccessModel) Stream(ctx context.Context, call fantasy.Call) (fantasy.StreamResponse, error) {
	m.currentCalls++
	if m.currentCalls == 1 {
		return func(yield func(fantasy.StreamPart) bool) {
			yield(fantasy.StreamPart{Type: fantasy.StreamPartTypeTextStart, ID: "0"})
			yield(fantasy.StreamPart{Type: fantasy.StreamPartTypeTextDelta, ID: "0", Delta: "partial"})
			yield(fantasy.StreamPart{Type: fantasy.StreamPartTypeTextEnd, ID: "0"})
		}, nil
	}
	return func(yield func(fantasy.StreamPart) bool) {
		yield(fantasy.StreamPart{Type: fantasy.StreamPartTypeTextStart, ID: "0"})
		yield(fantasy.StreamPart{Type: fantasy.StreamPartTypeTextDelta, ID: "0", Delta: "complete"})
		yield(fantasy.StreamPart{Type: fantasy.StreamPartTypeTextEnd, ID: "0"})
		yield(fantasy.StreamPart{Type: fantasy.StreamPartTypeFinish, FinishReason: fantasy.FinishReasonStop})
	}, nil
}

func (m *mockTextWithoutFinishThenSuccessModel) Generate(ctx context.Context, call fantasy.Call) (*fantasy.Response, error) {
	return nil, errors.New("not implemented")
}

func (m *mockTextWithoutFinishThenSuccessModel) GenerateObject(ctx context.Context, call fantasy.ObjectCall) (*fantasy.ObjectResponse, error) {
	return nil, errors.New("not implemented")
}

func (m *mockTextWithoutFinishThenSuccessModel) StreamObject(ctx context.Context, call fantasy.ObjectCall) (fantasy.ObjectStreamResponse, error) {
	return nil, errors.New("not implemented")
}

func (m *mockTextWithoutFinishThenSuccessModel) Provider() string { return "mock" }
func (m *mockTextWithoutFinishThenSuccessModel) Model() string    { return "mock-model" }

func TestAgentLoop_RetriesTextWithoutFinish(t *testing.T) {
	model := &mockTextWithoutFinishThenSuccessModel{}
	eventCh := make(chan AgentEvent, 64)
	cfg := AgentLoopConfig{Model: model}
	agentCtx := &AgentContext{}
	prompts := []Message{&UserMessage{Role: "user", Content: "hi", Timestamp: time.Now()}}

	msgs, err := AgentLoop(context.Background(), prompts, agentCtx, cfg, eventCh)
	close(eventCh)
	require.NoError(t, err)
	assert.Equal(t, 2, model.currentCalls)

	require.Len(t, msgs, 2)
	assistant, ok := msgs[1].(*AssistantMessage)
	require.True(t, ok)
	assert.Equal(t, "complete", assistant.Content.Text())

	retryEvents := 0
	for ev := range eventCh {
		if ev.Type == AgentEventTypeRetry {
			retryEvents++
		}
	}
	assert.Equal(t, 1, retryEvents)
}
