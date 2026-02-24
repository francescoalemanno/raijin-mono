package llm

import "strings"

// InferProviderType infers provider type from provider ID.
func InferProviderType(providerID string) ProviderType {
	switch strings.ToLower(strings.TrimSpace(providerID)) {
	case "anthropic":
		return ProviderAnthropic
	case "openai", "openai-codex":
		return ProviderOpenAI
	case "google", "gemini":
		return ProviderGoogle
	case "openrouter":
		return ProviderOpenRouter
	case "opencode", "opencode-zen-free":
		return ProviderOpenAICompat
	case "azure":
		return ProviderAzure
	case "bedrock":
		return ProviderBedrock
	case "vercel":
		return ProviderVercel
	case "google-vertex", "vertexai":
		return ProviderVertexAI
	default:
		return ProviderOpenAICompat
	}
}
