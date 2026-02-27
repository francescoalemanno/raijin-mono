package libagent_test

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/francescoalemanno/raijin-mono/libagent"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// All Codex e2e tests require LIBAGENT_TEST_CODEX=1.
// On first run the OAuth login flow opens a browser window automatically.
// Credentials are persisted to ~/.config/libagent/oauth_credentials.json so
// subsequent runs authenticate without user interaction.
//
// Run all:
//
//	LIBAGENT_TEST_CODEX=1 go test -run TestE2E_Codex -v -timeout 120s ./...

const codexModelRef = "openai-codex/gpt-5.3-codex"

var codexCatalog = sync_once_catalog()

// sync_once_catalog returns a shared DefaultCatalog so all tests share the
// same credential store and avoid redundant auth flows.
func sync_once_catalog() func() *libagent.Catalog {
	var cat *libagent.Catalog
	return func() *libagent.Catalog {
		if cat == nil {
			cat = libagent.DefaultCatalog()
		}
		return cat
	}
}

func codexRuntimeModel(t *testing.T) libagent.RuntimeModel {
	t.Helper()
	if os.Getenv("LIBAGENT_TEST_CODEX") == "" {
		t.Skip("set LIBAGENT_TEST_CODEX=1 to run")
	}
	providerID, modelID := mustSplitProviderModelRef(t, codexModelRef)
	cat := codexCatalog()
	info, _, err := cat.FindModel(providerID, modelID)
	require.NoError(t, err)
	model, err := cat.NewModel(context.Background(), providerID, modelID, "")
	require.NoError(t, err)
	return libagent.RuntimeModel{
		Model:     model,
		ModelInfo: info,
		ModelCfg:  libagent.ModelConfig{Provider: providerID, Model: modelID},
	}
}

func mustSplitProviderModelRef(t *testing.T, ref string) (providerID, modelID string) {
	t.Helper()
	providerID, modelID, ok := strings.Cut(strings.TrimSpace(ref), "/")
	if !ok || strings.TrimSpace(providerID) == "" || strings.TrimSpace(modelID) == "" {
		t.Fatalf("invalid provider/model ref %q, expected provider/model", ref)
	}
	return providerID, modelID
}

// TestE2E_Codex_HelloWorld is the basic smoke test.
func TestE2E_Codex_HelloWorld(t *testing.T) {
	a := libagent.NewAgent(libagent.AgentOptions{
		RuntimeModel: codexRuntimeModel(t),
		SystemPrompt: "You are a helpful assistant. Reply as concisely as possible.",
	})

	err := a.Prompt(context.Background(), "Say exactly: Hello, World!")
	require.NoError(t, err)

	s := a.State()
	assert.False(t, s.IsStreaming)
	require.Len(t, s.Messages, 2)
	assert.Equal(t, "assistant", s.Messages[1].GetRole())

	asst := s.Messages[1].(*libagent.AssistantMessage)
	assert.True(t,
		strings.Contains(strings.ToLower(asst.Content.Text()), "hello"),
		"expected response to contain 'hello', got: %q", asst.Content.Text(),
	)
}

// TestE2E_Codex_MultiTurn verifies that conversation history is maintained
// across multiple prompts.
func TestE2E_Codex_MultiTurn(t *testing.T) {
	a := libagent.NewAgent(libagent.AgentOptions{
		RuntimeModel: codexRuntimeModel(t),
		SystemPrompt: "You are a helpful assistant with perfect memory.",
	})

	err := a.Prompt(context.Background(), "My secret number is 42. Remember it.")
	require.NoError(t, err)
	assert.Len(t, a.State().Messages, 2)

	err = a.Prompt(context.Background(), "What is my secret number? Reply with just the number.")
	require.NoError(t, err)
	assert.Len(t, a.State().Messages, 4)

	last := a.State().Messages[3].(*libagent.AssistantMessage)
	assert.Contains(t, last.Content.Text(), "42",
		"expected model to recall the secret number, got: %q", last.Content.Text())
}

// TestE2E_Codex_ToolUse verifies that the Codex model can call a tool and
// incorporate the result into its final response.
func TestE2E_Codex_ToolUse(t *testing.T) {
	type addInput struct {
		A int `json:"a" description:"First number"`
		B int `json:"b" description:"Second number"`
	}

	addTool := libagent.NewTypedTool(
		"add",
		"Add two integers and return their sum.",
		func(_ context.Context, in addInput, _ libagent.ToolCall) (libagent.ToolResponse, error) {
			return libagent.NewTextResponse(strings.Join([]string{
				"sum:", itoa(in.A + in.B),
			}, " ")), nil
		},
	)

	a := libagent.NewAgent(libagent.AgentOptions{
		RuntimeModel: codexRuntimeModel(t),
		SystemPrompt: "You MUST use the add tool for every arithmetic question. Never compute in your head.",
		Tools:        []libagent.Tool{addTool},
	})

	err := a.Prompt(context.Background(), "Use the add tool to compute 17 + 25.")
	require.NoError(t, err)

	s := a.State()
	assert.False(t, s.IsStreaming)

	// Verify a tool result message is present.
	var toolResultFound bool
	for _, msg := range s.Messages {
		if msg.GetRole() == "toolResult" {
			toolResultFound = true
		}
	}
	assert.True(t, toolResultFound, "expected at least one toolResult message")

	// Final assistant message should mention the result.
	last := s.Messages[len(s.Messages)-1].(*libagent.AssistantMessage)
	assert.Contains(t, last.Content.Text(), "42",
		"expected final response to contain the result 42, got: %q", last.Content.Text())
}

// TestE2E_Codex_ReasoningContent verifies that reasoning content is returned
// (because we include reasoning.encrypted_content in every Codex call).
func TestE2E_Codex_ReasoningContent(t *testing.T) {
	a := libagent.NewAgent(libagent.AgentOptions{
		RuntimeModel: codexRuntimeModel(t),
		SystemPrompt: "You are a careful reasoner.",
	})

	err := a.Prompt(context.Background(), "What is the 10th Fibonacci number? Show your reasoning.")
	require.NoError(t, err)

	s := a.State()
	require.Len(t, s.Messages, 2)
	asst := s.Messages[1].(*libagent.AssistantMessage)

	// The model should return reasoning content alongside text.
	assert.NotEmpty(t, asst.Content.ReasoningText(),
		"expected reasoning content in response")
	assert.NotEmpty(t, asst.Content.Text(),
		"expected text content in response")
}

// TestE2E_Codex_Continue verifies that AgentLoopContinue works — the model
// picks up from an existing user message without re-sending a new prompt.
func TestE2E_Codex_Continue(t *testing.T) {
	a := libagent.NewAgent(libagent.AgentOptions{
		RuntimeModel: codexRuntimeModel(t),
		SystemPrompt: "You are a helpful assistant.",
	})

	a.ReplaceMessages([]libagent.Message{
		&libagent.UserMessage{
			Role:    "user",
			Content: "Say exactly the word: CONTINUE",
		},
	})

	err := a.Continue(context.Background())
	require.NoError(t, err)

	s := a.State()
	require.Len(t, s.Messages, 2)
	assert.Equal(t, "assistant", s.Messages[1].GetRole())

	asst := s.Messages[1].(*libagent.AssistantMessage)
	assert.Contains(t, strings.ToUpper(asst.Content.Text()), "CONTINUE",
		"expected response to contain CONTINUE, got: %q", asst.Content.Text())
}

// TestE2E_Codex_Events verifies that all expected agent events are emitted.
func TestE2E_Codex_Events(t *testing.T) {
	a := libagent.NewAgent(libagent.AgentOptions{
		RuntimeModel: codexRuntimeModel(t),
	})

	ch, unsub := a.Subscribe()
	defer unsub()

	done := make(chan struct{})
	var events []libagent.AgentEvent
	go func() {
		defer close(done)
		for e := range ch {
			events = append(events, e)
			if e.Type == libagent.AgentEventTypeAgentEnd {
				return
			}
		}
	}()

	err := a.Prompt(context.Background(), "Count from 1 to 3, one number per line.")
	require.NoError(t, err)
	<-done

	types := eventTypes(events)
	assert.Contains(t, types, libagent.AgentEventTypeAgentStart)
	assert.Contains(t, types, libagent.AgentEventTypeTurnStart)
	assert.Contains(t, types, libagent.AgentEventTypeMessageStart)
	assert.Contains(t, types, libagent.AgentEventTypeMessageUpdate)
	assert.Contains(t, types, libagent.AgentEventTypeMessageEnd)
	assert.Contains(t, types, libagent.AgentEventTypeTurnEnd)
	assert.Contains(t, types, libagent.AgentEventTypeAgentEnd)

	assert.False(t, a.State().IsStreaming)
}

// itoa converts an int to its decimal string representation.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	buf := make([]byte, 0, 10)
	for n > 0 {
		buf = append([]byte{byte('0' + n%10)}, buf...)
		n /= 10
	}
	if neg {
		buf = append([]byte{'-'}, buf...)
	}
	return string(buf)
}
