package llm

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"charm.land/fantasy/providers/anthropic"
	"charm.land/fantasy/providers/google"
	"charm.land/fantasy/providers/openaicompat"
	"charm.land/fantasy/providers/openrouter"
	"charm.land/fantasy/providers/vercel"
)

func TestNormalizeProviderOptions_OpenAICodexForcesResponsesAPI(t *testing.T) {
	t.Parallel()

	options := normalizeProviderOptions("openai-codex", nil)
	if got, ok := options["useResponsesAPI"].(bool); !ok || !got {
		t.Fatalf("useResponsesAPI = %#v, want true", options["useResponsesAPI"])
	}
}

func TestNormalizeProviderOptions_DoesNotOverrideExplicitValue(t *testing.T) {
	t.Parallel()

	options := normalizeProviderOptions("openai-codex", map[string]any{"useResponsesAPI": false})
	if got, ok := options["useResponsesAPI"].(bool); !ok || got {
		t.Fatalf("useResponsesAPI = %#v, want false", options["useResponsesAPI"])
	}
}

func TestNormalizeProviderOptions_DoesNotMutateOtherProviders(t *testing.T) {
	t.Parallel()

	if options := normalizeProviderOptions("openai", nil); options != nil {
		t.Fatalf("expected nil options for non-codex provider, got %#v", options)
	}
}

func TestProviderMaxOutputTokens_OpenAICodexClearsValue(t *testing.T) {
	t.Parallel()

	requested := int64(1234)
	if got := providerMaxOutputTokens("openai-codex", &requested); got != nil {
		t.Fatalf("provider max output tokens = %#v, want nil", got)
	}
}

func TestProviderMaxOutputTokens_OtherProvidersPreserveValue(t *testing.T) {
	t.Parallel()

	requested := int64(1234)
	if got := providerMaxOutputTokens("openai", &requested); got != &requested {
		t.Fatalf("provider max output tokens pointer mismatch: got %p want %p", got, &requested)
	}
}

func TestProviderStreamOptions_OpenAICodexIncludesResponsesOptions(t *testing.T) {
	t.Parallel()

	opts := providerStreamOptions(ProviderOpenAI, "openai-codex", "gpt-5.3-codex", ThinkingLevelMedium, "system")
	if len(opts) == 0 {
		t.Fatal("expected codex stream options to be populated")
	}
}

func TestProviderStreamOptions_OtherProvidersAreNil(t *testing.T) {
	t.Parallel()

	if opts := providerStreamOptions(ProviderOpenAI, "openai", "gpt-5", ThinkingLevelOff, "system"); opts != nil {
		t.Fatalf("expected non-codex stream options to be nil, got %#v", opts)
	}
}

func TestProviderStreamOptions_AnthropicThinkingOffSuppressesReasoning(t *testing.T) {
	t.Parallel()

	opts := providerStreamOptions(ProviderAnthropic, "anthropic", "claude-4", ThinkingLevelOff, "system")
	providerOpts, ok := opts[anthropic.Name].(*anthropic.ProviderOptions)
	if !ok || providerOpts == nil || providerOpts.SendReasoning == nil {
		t.Fatalf("expected anthropic provider options with send_reasoning, got %#v", opts[anthropic.Name])
	}
	if *providerOpts.SendReasoning {
		t.Fatal("expected anthropic send_reasoning=false for thinking off")
	}
	if providerOpts.Thinking == nil || providerOpts.Thinking.BudgetTokens != 0 {
		t.Fatalf("expected anthropic thinking budget_tokens=0, got %#v", providerOpts.Thinking)
	}
}

func TestProviderStreamOptions_AnthropicThinkingEnabled(t *testing.T) {
	t.Parallel()

	opts := providerStreamOptions(ProviderAnthropic, "anthropic", "claude-4", ThinkingLevelHigh, "system")
	if len(opts) == 0 {
		t.Fatal("expected anthropic options")
	}
	if _, ok := opts[anthropic.Name].(*anthropic.ProviderOptions); !ok {
		t.Fatalf("expected anthropic provider options type, got %#v", opts[anthropic.Name])
	}
}

func TestProviderStreamOptions_AnthropicThinkingLevelMapsBudget(t *testing.T) {
	t.Parallel()

	opts := providerStreamOptions(ProviderAnthropic, "anthropic", "claude-4", ThinkingLevelMax, "system")
	providerOpts, ok := opts[anthropic.Name].(*anthropic.ProviderOptions)
	if !ok || providerOpts == nil || providerOpts.Thinking == nil {
		t.Fatalf("expected anthropic thinking options, got %#v", opts[anthropic.Name])
	}
	if providerOpts.Thinking.BudgetTokens != thinkingBudgetMax {
		t.Fatalf("anthropic thinking budget = %d, want %d", providerOpts.Thinking.BudgetTokens, thinkingBudgetMax)
	}
}

func TestProviderStreamOptions_GoogleThinkingEnabled(t *testing.T) {
	t.Parallel()

	opts := providerStreamOptions(ProviderGoogle, "gemini", "gemini-2.5-pro", ThinkingLevelHigh, "system")
	if len(opts) == 0 {
		t.Fatal("expected google options")
	}
	if _, ok := opts[google.Name].(*google.ProviderOptions); !ok {
		t.Fatalf("expected google provider options type, got %#v", opts[google.Name])
	}
}

func TestProviderStreamOptions_OpenAICompatReasoningEnabled(t *testing.T) {
	t.Parallel()

	opts := providerStreamOptions(ProviderOpenAICompat, "opencode", "kimi-k2", ThinkingLevelHigh, "system")
	if len(opts) == 0 {
		t.Fatal("expected openai-compat options")
	}
	if _, ok := opts[openaicompat.Name].(*openaicompat.ProviderOptions); !ok {
		t.Fatalf("expected openai-compat provider options type, got %#v", opts[openaicompat.Name])
	}
}

func TestProviderStreamOptions_OpenRouterReasoningEnabled(t *testing.T) {
	t.Parallel()

	opts := providerStreamOptions(ProviderOpenRouter, "openrouter", "openai/gpt-5", ThinkingLevelMedium, "system")
	if len(opts) == 0 {
		t.Fatal("expected openrouter options")
	}
	if _, ok := opts[openrouter.Name].(*openrouter.ProviderOptions); !ok {
		t.Fatalf("expected openrouter provider options type, got %#v", opts[openrouter.Name])
	}
}

func TestProviderStreamOptions_VercelReasoningEnabled(t *testing.T) {
	t.Parallel()

	opts := providerStreamOptions(ProviderVercel, "vercel", "openai/gpt-5", ThinkingLevelMedium, "system")
	if len(opts) == 0 {
		t.Fatal("expected vercel options")
	}
	if _, ok := opts[vercel.Name].(*vercel.ProviderOptions); !ok {
		t.Fatalf("expected vercel provider options type, got %#v", opts[vercel.Name])
	}
}

func TestThinkingBudgetForLevel(t *testing.T) {
	t.Parallel()

	if got := thinkingBudgetForLevel(ThinkingLevelLow); got != thinkingBudgetLow {
		t.Fatalf("low budget = %d, want %d", got, thinkingBudgetLow)
	}
	if got := thinkingBudgetForLevel(ThinkingLevelMedium); got != thinkingBudgetMedium {
		t.Fatalf("medium budget = %d, want %d", got, thinkingBudgetMedium)
	}
	if got := thinkingBudgetForLevel(ThinkingLevelHigh); got != thinkingBudgetHigh {
		t.Fatalf("high budget = %d, want %d", got, thinkingBudgetHigh)
	}
	if got := thinkingBudgetForLevel(ThinkingLevelMax); got != thinkingBudgetMax {
		t.Fatalf("max budget = %d, want %d", got, thinkingBudgetMax)
	}
}

func TestToVercelReasoningEffort_MaxMapsToXHigh(t *testing.T) {
	t.Parallel()

	effort := toVercelReasoningEffort(ThinkingLevelMax)
	if effort == nil || *effort != vercel.ReasoningEffortXHigh {
		t.Fatalf("effort = %#v, want %q", effort, vercel.ReasoningEffortXHigh)
	}
}

func TestTokenLikelyExpired(t *testing.T) {
	t.Parallel()

	now := time.Unix(1_700_000_000, 0)
	if !tokenLikelyExpired(testJWTWithExp(t, now.Add(-time.Minute).Unix()), now) {
		t.Fatal("expected token to be considered expired")
	}
	if tokenLikelyExpired(testJWTWithExp(t, now.Add(time.Minute).Unix()), now) {
		t.Fatal("expected token to be considered valid")
	}
	if tokenLikelyExpired("not-a-jwt", now) {
		t.Fatal("expected malformed token to be treated as unknown validity")
	}
}

func TestRuntimeStream_RefreshFailureWithExpiredTokenReturnsError(t *testing.T) {
	t.Parallel()

	rt := &runtime{
		providerID: "openai-codex",
		apiKey:     testJWTWithExp(t, time.Now().Add(-time.Minute).Unix()),
		tokenRefresh: func(string) (string, error) {
			return "", errors.New("refresh unavailable")
		},
	}

	_, err := rt.Stream(context.Background(), StreamRequest{})
	if err == nil {
		t.Fatal("expected stream to fail when token refresh fails for an expired token")
	}
	if !strings.Contains(err.Error(), "refresh token before stream") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func testJWTWithExp(t *testing.T, exp int64) string {
	t.Helper()
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"none","typ":"JWT"}`))
	payloadJSON, err := json.Marshal(map[string]any{"exp": exp})
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	payload := base64.RawURLEncoding.EncodeToString(payloadJSON)
	return header + "." + payload + ".sig"
}
