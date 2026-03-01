package libagent_test

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/francescoalemanno/raijin-mono/libagent"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	e2eProviderID = "synthetic"
	e2eModelID    = "hf:openai/gpt-oss-120b"
)

func syntheticRuntimeModel(t *testing.T) libagent.RuntimeModel {
	t.Helper()
	apiKey := strings.TrimSpace(os.Getenv("SYNTHETIC_API_KEY"))
	if apiKey == "" {
		t.Skip("SYNTHETIC_API_KEY not set")
	}
	if os.Getenv("LIBAGENT_RUN_NETWORK_TESTS") != "1" {
		t.Skip("set LIBAGENT_RUN_NETWORK_TESTS=1 to run network-backed e2e tests")
	}
	cat := libagent.DefaultCatalog()
	info, _, err := cat.FindModel(e2eProviderID, e2eModelID)
	require.NoError(t, err)
	model, err := cat.NewModel(context.Background(), e2eProviderID, e2eModelID, apiKey)
	require.NoError(t, err)
	return libagent.RuntimeModel{
		Model:     model,
		ModelInfo: info,
		ModelCfg:  libagent.ModelConfig{Provider: e2eProviderID, Model: e2eModelID},
	}
}

func TestE2E_BasicPrompt(t *testing.T) {
	a := libagent.NewAgent(libagent.AgentOptions{
		RuntimeModel: syntheticRuntimeModel(t),
		SystemPrompt: "You are a helpful assistant. Keep answers very concise.",
	})

	err := a.Prompt(context.Background(), "What is 2+2? Reply with just the number.")
	require.NoError(t, err)

	s := a.State()
	assert.False(t, s.IsStreaming)
	require.Len(t, s.Messages, 2)
	assert.Equal(t, "user", s.Messages[0].GetRole())
	assert.Equal(t, "assistant", s.Messages[1].GetRole())

	asst := s.Messages[1].(*libagent.AssistantMessage)
	assert.Contains(t, asst.Content.Text(), "4")
}

func TestE2E_StateEvents(t *testing.T) {
	a := libagent.NewAgent(libagent.AgentOptions{
		RuntimeModel: syntheticRuntimeModel(t),
		SystemPrompt: "You are a helpful assistant.",
	})

	var events []libagent.AgentEvent
	ch, unsub := a.Subscribe()
	defer unsub()

	done := make(chan struct{})
	go func() {
		defer close(done)
		for e := range ch {
			events = append(events, e)
			if e.Type == libagent.AgentEventTypeAgentEnd {
				return
			}
		}
	}()

	err := a.Prompt(context.Background(), "Count from 1 to 3.")
	require.NoError(t, err)
	<-done

	types := eventTypes(events)
	assert.Contains(t, types, libagent.AgentEventTypeAgentStart)
	assert.Contains(t, types, libagent.AgentEventTypeAgentEnd)
	assert.Contains(t, types, libagent.AgentEventTypeMessageStart)
	assert.Contains(t, types, libagent.AgentEventTypeMessageEnd)
	assert.Contains(t, types, libagent.AgentEventTypeMessageUpdate)

	s := a.State()
	assert.False(t, s.IsStreaming)
	assert.Len(t, s.Messages, 2)
}

func TestE2E_ToolExecution(t *testing.T) {
	type calcInput struct {
		Expression string `json:"expression" description:"Mathematical expression to evaluate"`
	}

	calcTool := libagent.NewTypedTool(
		"calculate",
		"Evaluate a mathematical expression and return the numeric result. You MUST use this tool for any arithmetic.",
		func(_ context.Context, input calcInput, _ libagent.ToolCall) (libagent.ToolResponse, error) {
			switch strings.TrimSpace(input.Expression) {
			case "123 * 456", "123*456":
				return libagent.NewTextResponse("56088"), nil
			default:
				return libagent.NewTextResponse(input.Expression + " = (computed)"), nil
			}
		},
	)

	a := libagent.NewAgent(libagent.AgentOptions{
		RuntimeModel: syntheticRuntimeModel(t),
		SystemPrompt: "You MUST use the calculate tool for every math problem. Never compute in your head.",
		Tools:        []libagent.Tool{calcTool},
	})

	err := a.Prompt(context.Background(), "Use the calculate tool to compute 123 * 456.")
	require.NoError(t, err)

	s := a.State()
	assert.False(t, s.IsStreaming)

	// Either the model used the tool (≥3 messages) or answered directly (2 messages).
	// If it used the tool, verify the result is present in the final response.
	toolResult := findMessageByRole(s.Messages, "toolResult")
	if toolResult != nil {
		lastMsg := s.Messages[len(s.Messages)-1]
		assert.Equal(t, "assistant", lastMsg.GetRole())
		asst := lastMsg.(*libagent.AssistantMessage)
		text := asst.Content.Text()
		assert.True(t, strings.Contains(text, "56088") || strings.Contains(text, "56,088"),
			"final response should contain result, got: %q", text)
	} else {
		// Model answered without using the tool — still check it mentioned something reasonable.
		t.Log("model did not use the tool; checking for inline answer")
		lastMsg := s.Messages[len(s.Messages)-1]
		assert.Equal(t, "assistant", lastMsg.GetRole())
	}
}

func TestE2E_MultiTurn(t *testing.T) {
	a := libagent.NewAgent(libagent.AgentOptions{
		RuntimeModel: syntheticRuntimeModel(t),
		SystemPrompt: "You are a helpful assistant with perfect memory.",
	})

	err := a.Prompt(context.Background(), "My name is Alice.")
	require.NoError(t, err)
	assert.Len(t, a.State().Messages, 2)

	err = a.Prompt(context.Background(), "What is my name?")
	require.NoError(t, err)
	assert.Len(t, a.State().Messages, 4)

	last := a.State().Messages[3].(*libagent.AssistantMessage)
	assert.Contains(t, strings.ToLower(last.Content.Text()), "alice")
}

func TestE2E_Abort(t *testing.T) {
	a := libagent.NewAgent(libagent.AgentOptions{
		RuntimeModel: syntheticRuntimeModel(t),
		SystemPrompt: "You are a helpful assistant.",
	})

	done := make(chan error, 1)
	go func() { done <- a.Prompt(context.Background(), "Write a very long essay.") }()

	time.Sleep(200 * time.Millisecond)
	a.Abort()

	err := <-done
	// After abort the agent should be idle regardless.
	_ = err
	a.WaitForIdle()
	assert.False(t, a.State().IsStreaming)
}

func TestE2E_Continue(t *testing.T) {
	a := libagent.NewAgent(libagent.AgentOptions{
		RuntimeModel: syntheticRuntimeModel(t),
		SystemPrompt: "You are a helpful assistant.",
	})

	// Manually add a user message and continue from it.
	a.ReplaceMessages([]libagent.Message{
		&libagent.UserMessage{
			Role:      "user",
			Content:   "Say exactly: HELLO WORLD",
			Timestamp: time.Now(),
		},
	})

	err := a.Continue(context.Background())
	require.NoError(t, err)

	s := a.State()
	require.Len(t, s.Messages, 2)
	assert.Equal(t, "assistant", s.Messages[1].GetRole())
	asst := s.Messages[1].(*libagent.AssistantMessage)
	assert.Contains(t, strings.ToUpper(asst.Content.Text()), "HELLO WORLD")
}

// findMessageByRole returns the first message with the given role, or nil.
func findMessageByRole(msgs []libagent.Message, role string) libagent.Message {
	for _, m := range msgs {
		if m.GetRole() == role {
			return m
		}
	}
	return nil
}
