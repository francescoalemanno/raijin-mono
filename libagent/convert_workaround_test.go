package libagent

import (
	"context"
	"testing"
	"time"

	"charm.land/fantasy"
	"charm.land/fantasy/providers/openai"
	"charm.land/fantasy/providers/openaicompat"
)

func TestDefaultConvertToLLMForRuntime_OpenAICompatAddsReasoningPlaceholderForToolCalls(t *testing.T) {
	effort := openai.ReasoningEffortMedium
	convert := defaultConvertToLLMForRuntime("openai-compat", fantasy.ProviderOptions{
		openaicompat.Name: &openaicompat.ProviderOptions{
			ReasoningEffort: &effort,
		},
	})

	msgs := []Message{
		NewAssistantMessage("", "", []ToolCallItem{{
			ID:    "tc1",
			Name:  "bash",
			Input: `{"command":"ls"}`,
		}}, time.Now()),
	}

	out, err := convert(context.Background(), msgs)
	if err != nil {
		t.Fatalf("convert: %v", err)
	}
	if len(out) != 1 {
		t.Fatalf("messages len=%d want 1", len(out))
	}
	if len(out[0].Content) != 2 {
		t.Fatalf("content len=%d want 2 (tool call + reasoning)", len(out[0].Content))
	}

	foundReasoning := false
	for _, part := range out[0].Content {
		if reasoning, ok := part.(fantasy.ReasoningPart); ok {
			if reasoning.Text != " " {
				t.Fatalf("reasoning placeholder=%q want single space", reasoning.Text)
			}
			foundReasoning = true
		}
	}
	if !foundReasoning {
		t.Fatal("expected reasoning placeholder part")
	}
}

func TestDefaultConvertToLLMForRuntime_DoesNotAddPlaceholderWithoutReasoningEffort(t *testing.T) {
	convert := defaultConvertToLLMForRuntime("openai-compat", fantasy.ProviderOptions{
		openaicompat.Name: &openaicompat.ProviderOptions{},
	})

	msgs := []Message{
		NewAssistantMessage("", "", []ToolCallItem{{
			ID:    "tc1",
			Name:  "bash",
			Input: `{"command":"ls"}`,
		}}, time.Now()),
	}

	out, err := convert(context.Background(), msgs)
	if err != nil {
		t.Fatalf("convert: %v", err)
	}
	if len(out) != 1 {
		t.Fatalf("messages len=%d want 1", len(out))
	}
	if len(out[0].Content) != 1 {
		t.Fatalf("content len=%d want 1 (tool call only)", len(out[0].Content))
	}
	if _, ok := out[0].Content[0].(fantasy.ToolCallPart); !ok {
		t.Fatalf("content[0] type=%T want fantasy.ToolCallPart", out[0].Content[0])
	}
}
