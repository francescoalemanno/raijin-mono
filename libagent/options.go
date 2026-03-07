package libagent

import (
	"encoding/json"
	"log/slog"
	"maps"
	"strings"

	"charm.land/catwalk/pkg/catwalk"
	"charm.land/fantasy"
	"charm.land/fantasy/providers/anthropic"
	"charm.land/fantasy/providers/google"
	"charm.land/fantasy/providers/openai"
	"charm.land/fantasy/providers/openaicompat"
	"charm.land/fantasy/providers/openrouter"
	"charm.land/fantasy/providers/vercel"
)

// Token budgets used when mapping ThinkingLevel to Anthropic/Google token budgets.
const (
	thinkingBudgetLow    int64 = 2_000
	thinkingBudgetMedium int64 = 8_000
	thinkingBudgetHigh   int64 = 16_000
	thinkingBudgetMax    int64 = 32_000
)

// BuildProviderOptions constructs a fantasy.ProviderOptions map for a single LLM
// call. It merges three sources in priority order (lowest → highest):
//
//  1. catalogDefaults  – raw map from the catwalk catalog (e.g. ThinkingConfig)
//  2. cfg.ProviderOptions – per-model raw overrides stored in ModelConfig
//  3. Derived options    – reasoning/thinking derived from cfg.ThinkingLevel
//
// The result is routed through each provider's ParseOptions / ParseResponsesOptions
// so that fantasy receives strongly-typed ProviderOptionsData values.
//
// Special cases:
//   - Codex (providerID == CodexProviderID): injects Instructions = systemPrompt,
//     ParallelToolCalls = true, Include = [reasoning_encrypted_content], and
//     a ReasoningEffort when ThinkingLevel is enabled.
//   - OpenAI Responses API models (detected via openai.IsResponsesModel): same
//     reasoning_summary + include treatment as Codex.
func BuildProviderOptions(
	providerID string,
	providerType string, // catwalk.Type string: "openai", "anthropic", "google", …
	modelID string,
	systemPrompt string,
	thinkingLevel ThinkingLevel,
	catalogDefaults map[string]any,
	rawOverrides map[string]any,
) fantasy.ProviderOptions {
	// Merge raw maps: catalog < overrides.
	merged := mergeRawOptions(catalogDefaults, rawOverrides)

	result := fantasy.ProviderOptions{}

	// Codex is a custom provider that uses the OpenAI Responses API with OAuth.
	if isCodexProvider(providerID) {
		return buildCodexProviderOptions(systemPrompt, thinkingLevel, merged)
	}

	switch catwalk.Type(providerType) {
	case catwalk.TypeOpenAI, catwalk.TypeAzure:
		if openai.IsResponsesModel(modelID) {
			applyOpenAIEffort(merged, thinkingLevel)
			if openai.IsResponsesReasoningModel(modelID) {
				merged["reasoning_summary"] = "auto"
				merged["include"] = []openai.IncludeType{openai.IncludeReasoningEncryptedContent}
			}
			parsed, err := openai.ParseResponsesOptions(merged)
			if err != nil {
				slog.Warn("BuildProviderOptions: failed to parse OpenAI Responses options", "err", err)
				return result
			}
			result[openai.Name] = parsed
		} else {
			applyOpenAIEffort(merged, thinkingLevel)
			parsed, err := openai.ParseOptions(merged)
			if err != nil {
				slog.Warn("BuildProviderOptions: failed to parse OpenAI options", "err", err)
				return result
			}
			result[openai.Name] = parsed
		}

	case catwalk.TypeAnthropic:
		applyAnthropicThinking(merged, thinkingLevel)
		parsed, err := anthropic.ParseOptions(merged)
		if err != nil {
			slog.Warn("BuildProviderOptions: failed to parse Anthropic options", "err", err)
			return result
		}
		result[anthropic.Name] = parsed

	case catwalk.TypeGoogle, catwalk.TypeVertexAI:
		applyGoogleThinking(merged, modelID, thinkingLevel)
		parsed, err := google.ParseOptions(merged)
		if err != nil {
			slog.Warn("BuildProviderOptions: failed to parse Google options", "err", err)
			return result
		}
		result[google.Name] = parsed

	case catwalk.TypeOpenRouter:
		applyOpenRouterReasoning(merged, thinkingLevel)
		parsed, err := openrouter.ParseOptions(merged)
		if err != nil {
			slog.Warn("BuildProviderOptions: failed to parse OpenRouter options", "err", err)
			return result
		}
		result[openrouter.Name] = parsed

	case catwalk.TypeVercel:
		applyVercelReasoning(merged, thinkingLevel)
		parsed, err := vercel.ParseOptions(merged)
		if err != nil {
			slog.Warn("BuildProviderOptions: failed to parse Vercel options", "err", err)
			return result
		}
		result[vercel.Name] = parsed

	case catwalk.TypeOpenAICompat:
		applyOpenAICompatEffort(merged, thinkingLevel)
		parsed, err := openaicompat.ParseOptions(merged)
		if err != nil {
			slog.Warn("BuildProviderOptions: failed to parse OpenAI-compat options", "err", err)
			return result
		}
		result[openaicompat.Name] = parsed
	}

	return result
}

// SkipMaxOutputTokens reports whether the provider should not send max_output_tokens.
// Codex's ChatGPT backend rejects this field.
func SkipMaxOutputTokens(providerID string) bool {
	return isCodexProvider(providerID)
}

func isCodexProvider(providerID string) bool {
	return strings.EqualFold(strings.TrimSpace(providerID), CodexProviderID)
}

// buildCodexProviderOptions builds Responses API options for the Codex provider.
func buildCodexProviderOptions(systemPrompt string, level ThinkingLevel, merged map[string]any) fantasy.ProviderOptions {
	parallelToolCalls := true
	instructions := strings.TrimSpace(systemPrompt)
	if instructions == "" {
		instructions = "You are a helpful coding assistant."
	}
	merged["instructions"] = instructions
	merged["parallel_tool_calls"] = parallelToolCalls
	merged["include"] = []openai.IncludeType{openai.IncludeReasoningEncryptedContent}
	merged["reasoning_summary"] = "auto"
	effort := thinkingLevelToOpenAIEffort(level)
	if effort != "" {
		merged["reasoning_effort"] = string(effort)
	}
	parsed, err := openai.ParseResponsesOptions(merged)
	if err != nil {
		slog.Warn("BuildProviderOptions: failed to parse Codex options", "err", err)
		return fantasy.ProviderOptions{}
	}
	return fantasy.ProviderOptions{openai.Name: parsed}
}

// applyOpenAIEffort sets reasoning_effort in the merged map if thinking is enabled
// and no override is already present.
func applyOpenAIEffort(merged map[string]any, level ThinkingLevel) {
	if _, already := merged["reasoning_effort"]; already {
		return
	}
	effort := thinkingLevelToOpenAIEffort(level)
	if effort != "" {
		merged["reasoning_effort"] = string(effort)
	}
}

// applyAnthropicThinking sets thinking/send_reasoning in the merged map.
func applyAnthropicThinking(merged map[string]any, level ThinkingLevel) {
	_, hasThinking := merged["thinking"]
	if hasThinking {
		return
	}
	budget := thinkingBudgetForLevel(level)
	merged["thinking"] = map[string]any{"budget_tokens": budget}
	merged["send_reasoning"] = true
}

// applyGoogleThinking sets thinking_config in the merged map.
func applyGoogleThinking(merged map[string]any, modelID string, level ThinkingLevel) {
	if _, already := merged["thinking_config"]; already {
		return
	}
	includeThoughts := true
	// Gemini 2.x uses token budget; Gemini 3+ uses thinking_level string.
	if strings.HasPrefix(modelID, "gemini-2") || strings.HasPrefix(modelID, "models/gemini-2") {
		budget := thinkingBudgetForLevel(level)
		merged["thinking_config"] = map[string]any{
			"thinking_budget":  budget,
			"include_thoughts": includeThoughts,
		}
	} else {
		merged["thinking_config"] = map[string]any{
			"thinking_level":   thinkingLevelToEffortString(level),
			"include_thoughts": includeThoughts,
		}
	}
}

// applyOpenRouterReasoning sets reasoning in the merged map.
func applyOpenRouterReasoning(merged map[string]any, level ThinkingLevel) {
	if _, already := merged["reasoning"]; already {
		return
	}
	merged["reasoning"] = map[string]any{
		"enabled": true,
		"effort":  thinkingLevelToEffortString(level),
	}
}

// applyVercelReasoning sets reasoning in the merged map.
func applyVercelReasoning(merged map[string]any, level ThinkingLevel) {
	if _, already := merged["reasoning"]; already {
		return
	}
	effort := thinkingLevelToVercelEffort(level)
	merged["reasoning"] = map[string]any{
		"enabled": true,
		"effort":  string(effort),
	}
}

// applyOpenAICompatEffort sets reasoning_effort for OpenAI-compatible providers.
func applyOpenAICompatEffort(merged map[string]any, level ThinkingLevel) {
	if _, already := merged["reasoning_effort"]; already {
		return
	}
	effort := thinkingLevelToOpenAIEffort(level)
	if effort != "" {
		merged["reasoning_effort"] = string(effort)
	}
}

// thinkingLevelToOpenAIEffort maps a ThinkingLevel to an OpenAI ReasoningEffort.
// Returns the provider effort corresponding to the requested thinking level.
func thinkingLevelToOpenAIEffort(level ThinkingLevel) openai.ReasoningEffort {
	switch level {
	case ThinkingLevelLow:
		return openai.ReasoningEffortLow
	case ThinkingLevelHigh, ThinkingLevelMax:
		return openai.ReasoningEffortHigh
	default: // medium
		return openai.ReasoningEffortMedium
	}
}

// thinkingLevelToEffortString returns the provider-agnostic effort string
// ("low", "medium", "high") for providers that use plain strings.
func thinkingLevelToEffortString(level ThinkingLevel) string {
	switch level {
	case ThinkingLevelLow:
		return "low"
	case ThinkingLevelHigh, ThinkingLevelMax:
		return "high"
	default:
		return "medium"
	}
}

// thinkingLevelToVercelEffort maps a ThinkingLevel to a Vercel ReasoningEffort.
func thinkingLevelToVercelEffort(level ThinkingLevel) vercel.ReasoningEffort {
	switch level {
	case ThinkingLevelLow:
		return vercel.ReasoningEffortLow
	case ThinkingLevelHigh:
		return vercel.ReasoningEffortHigh
	case ThinkingLevelMax:
		return vercel.ReasoningEffortXHigh
	default:
		return vercel.ReasoningEffortMedium
	}
}

// thinkingBudgetForLevel maps ThinkingLevel to an Anthropic/Google token budget.
func thinkingBudgetForLevel(level ThinkingLevel) int64 {
	switch level {
	case ThinkingLevelLow:
		return thinkingBudgetLow
	case ThinkingLevelHigh:
		return thinkingBudgetHigh
	case ThinkingLevelMax:
		return thinkingBudgetMax
	default:
		return thinkingBudgetMedium
	}
}

// mergeRawOptions merges src maps left-to-right (later keys win) using JSON
// round-trip so nested maps are merged rather than replaced.
func mergeRawOptions(layers ...map[string]any) map[string]any {
	result := map[string]any{}
	for _, layer := range layers {
		if len(layer) == 0 {
			continue
		}
		// JSON round-trip to normalise types and do a shallow merge.
		data, err := json.Marshal(layer)
		if err != nil {
			continue
		}
		var decoded map[string]any
		if err := json.Unmarshal(data, &decoded); err != nil {
			continue
		}
		maps.Copy(result, decoded)
	}
	return result
}
