package llm

import "testing"

func TestApplyAnthropicThinkingHeader_EnabledAddsToken(t *testing.T) {
	t.Parallel()

	headers := map[string]string{"anthropic-beta": "prompt-caching-2024-07-31"}
	applyAnthropicThinkingHeader(headers, true)

	got := headers["anthropic-beta"]
	if got != "prompt-caching-2024-07-31,"+anthropicThinkingBeta {
		t.Fatalf("anthropic-beta = %q", got)
	}
}

func TestApplyAnthropicThinkingHeader_DisabledRemovesThinkingToken(t *testing.T) {
	t.Parallel()

	headers := map[string]string{"anthropic-beta": "prompt-caching-2024-07-31," + anthropicThinkingBeta}
	applyAnthropicThinkingHeader(headers, false)

	if got := headers["anthropic-beta"]; got != "prompt-caching-2024-07-31" {
		t.Fatalf("anthropic-beta = %q", got)
	}
}

func TestApplyAnthropicThinkingHeader_DisabledDeletesHeaderWhenOnlyThinkingToken(t *testing.T) {
	t.Parallel()

	headers := map[string]string{"anthropic-beta": anthropicThinkingBeta}
	applyAnthropicThinkingHeader(headers, false)

	if _, ok := headers["anthropic-beta"]; ok {
		t.Fatalf("expected anthropic-beta header to be removed, got %q", headers["anthropic-beta"])
	}
}
