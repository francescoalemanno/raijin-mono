package llm

import (
	"maps"
	"strings"

	"charm.land/fantasy"
	"charm.land/fantasy/providers/anthropic"
	"charm.land/fantasy/providers/google"
	"charm.land/fantasy/providers/openai"
	"charm.land/fantasy/providers/openaicompat"
	"charm.land/fantasy/providers/openrouter"
	"charm.land/fantasy/providers/vercel"
)

const (
	openAICodexProviderID       = "openai-codex"
	thinkingBudgetLow     int64 = 2000
	thinkingBudgetMedium  int64 = 8000
	thinkingBudgetHigh    int64 = 16000
	thinkingBudgetMax     int64 = 32000
)

func normalizeProviderOptions(providerID string, options map[string]any) map[string]any {
	cloned := maps.Clone(options)
	if !isOpenAICodexProvider(providerID) {
		return cloned
	}
	if cloned == nil {
		cloned = make(map[string]any)
	}
	if _, exists := cloned["useResponsesAPI"]; !exists {
		cloned["useResponsesAPI"] = true
	}
	return cloned
}

func providerMaxOutputTokens(providerID string, requested *int64) *int64 {
	if !isOpenAICodexProvider(providerID) {
		return requested
	}
	// ChatGPT Codex backend rejects max_output_tokens; let backend defaults apply.
	return nil
}

func providerStreamOptions(providerType ProviderType, providerID, modelID string, thinkingLevel ThinkingLevel, systemPrompt string) fantasy.ProviderOptions {
	if isOpenAICodexProvider(providerID) {
		instructions := strings.TrimSpace(systemPrompt)
		if instructions == "" {
			instructions = "You are a helpful coding assistant."
		}
		parallelToolCalls := true
		options := &openai.ResponsesProviderOptions{
			Instructions:      &instructions,
			ParallelToolCalls: &parallelToolCalls,
			Include:           []openai.IncludeType{openai.IncludeReasoningEncryptedContent},
		}
		if effort := toOpenAIReasoningEffort(modelID, thinkingLevel); effort != nil {
			options.ReasoningEffort = effort
			summary := "auto"
			options.ReasoningSummary = &summary
		}
		return openai.NewResponsesProviderOptions(options)
	}

	if !thinkingLevel.Enabled() {
		if providerType != ProviderAnthropic {
			return nil
		}
	}

	switch providerType {
	case ProviderAnthropic:
		budget := thinkingBudgetForLevel(thinkingLevel)
		sendReasoning := thinkingLevel.Enabled()
		return anthropic.NewProviderOptions(&anthropic.ProviderOptions{
			SendReasoning: &sendReasoning,
			Thinking:      &anthropic.ThinkingProviderOption{BudgetTokens: budget},
		})
	case ProviderGoogle, ProviderVertexAI:
		includeThoughts := true
		budget := thinkingBudgetForLevel(thinkingLevel)
		return fantasy.ProviderOptions{google.Name: &google.ProviderOptions{
			ThinkingConfig: &google.ThinkingConfig{
				ThinkingBudget:  &budget,
				IncludeThoughts: &includeThoughts,
			},
		}}
	case ProviderOpenAICompat:
		if effort := toOpenAIReasoningEffort(modelID, thinkingLevel); effort != nil {
			return openaicompat.NewProviderOptions(&openaicompat.ProviderOptions{ReasoningEffort: effort})
		}
	case ProviderOpenRouter:
		effort := toOpenRouterReasoningEffort(thinkingLevel)
		enabled := true
		return openrouter.NewProviderOptions(&openrouter.ProviderOptions{
			Reasoning: &openrouter.ReasoningOptions{Enabled: &enabled, Effort: effort},
		})
	case ProviderVercel:
		effort := toVercelReasoningEffort(thinkingLevel)
		enabled := true
		return vercel.NewProviderOptions(&vercel.ProviderOptions{
			Reasoning: &vercel.ReasoningOptions{Enabled: &enabled, Effort: effort},
		})
	}

	return nil
}

func toOpenAIReasoningEffort(_ string, level ThinkingLevel) *openai.ReasoningEffort {
	switch level {
	case ThinkingLevelOff:
		return nil
	case ThinkingLevelLow:
		value := openai.ReasoningEffortLow
		return &value
	case ThinkingLevelHigh, ThinkingLevelMax:
		value := openai.ReasoningEffortHigh
		return &value
	case ThinkingLevelMedium:
		fallthrough
	default:
		value := openai.ReasoningEffortMedium
		return &value
	}
}

func isOpenAICodexProvider(providerID string) bool {
	return strings.EqualFold(strings.TrimSpace(providerID), openAICodexProviderID)
}

func toOpenRouterReasoningEffort(level ThinkingLevel) *openrouter.ReasoningEffort {
	switch level {
	case ThinkingLevelHigh, ThinkingLevelMax:
		v := openrouter.ReasoningEffortHigh
		return &v
	case ThinkingLevelLow:
		v := openrouter.ReasoningEffortLow
		return &v
	case ThinkingLevelOff:
		return nil
	default:
		v := openrouter.ReasoningEffortMedium
		return &v
	}
}

func toVercelReasoningEffort(level ThinkingLevel) *vercel.ReasoningEffort {
	switch level {
	case ThinkingLevelOff:
		v := vercel.ReasoningEffortNone
		return &v
	case ThinkingLevelMax:
		v := vercel.ReasoningEffortXHigh
		return &v
	case ThinkingLevelHigh:
		v := vercel.ReasoningEffortHigh
		return &v
	case ThinkingLevelLow:
		v := vercel.ReasoningEffortLow
		return &v
	default:
		v := vercel.ReasoningEffortMedium
		return &v
	}
}

func thinkingBudgetForLevel(level ThinkingLevel) int64 {
	switch level {
	case ThinkingLevelOff:
		return 0
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
