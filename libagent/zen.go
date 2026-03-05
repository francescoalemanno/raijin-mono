package libagent

import (
	"context"
	"fmt"

	"charm.land/catwalk/pkg/catwalk"
	"charm.land/fantasy"
	"charm.land/fantasy/providers/anthropic"
	"charm.land/fantasy/providers/openaicompat"
)

// ZenProviderID is the stable identifier for the OpenCode Zen custom provider.
const ZenProviderID = "opencode"

// ZenGoProviderID is the stable identifier for the OpenCode Go custom provider.
const ZenGoProviderID = "opencode-go"

const (
	zenAPIEndpoint            = "https://opencode.ai/zen/v1"
	zenGoAPIEndpoint          = "https://opencode.ai/zen/go/v1"
	zenGoAnthropicAPIEndpoint = "https://opencode.ai/zen/go" // Without /v1, Anthropic lib adds it
)

//go:generate go run ./cmd/zen -output zen_models_generated.go

// Note: zenGoAnthropicModels map is auto-generated in zen_models_generated.go

// ZenProvider returns a CustomProvider for OpenCode Zen.
func ZenProvider() CustomProvider {
	return CustomProvider{
		ID:          ZenProviderID,
		Name:        "OpenCode Zen",
		Type:        catwalk.TypeOpenAICompat,
		APIKey:      "$OPENCODE_API_KEY",
		APIEndpoint: zenAPIEndpoint,
		Models:      zenGeneratedModels,
		Build:       buildZenProvider,
	}
}

// ZenGoProvider returns a CustomProvider for OpenCode Go.
// Go models use a different endpoint: https://opencode.ai/zen/go/v1
// MiniMax M2.5 uses Anthropic API format, while GLM-5 and Kimi K2.5 use OpenAI format.
func ZenGoProvider() CustomProvider {
	return CustomProvider{
		ID:          ZenGoProviderID,
		Name:        "OpenCode Go",
		Type:        catwalk.TypeOpenAICompat,
		APIKey:      "$OPENCODE_API_KEY",
		APIEndpoint: zenGoAPIEndpoint,
		Models:      zenGoGeneratedModels,
		Build:       buildZenGoProvider,
	}
}

func buildZenProvider(apiKey string) (fantasy.Provider, error) {
	return openaicompat.New(
		openaicompat.WithAPIKey(apiKey),
		openaicompat.WithBaseURL(zenAPIEndpoint),
		openaicompat.WithHTTPClient(http1OnlyClient),
	)
}

func buildZenGoProvider(apiKey string) (fantasy.Provider, error) {
	return newZenGoHybridProvider(apiKey)
}

// zenGoHybridProvider is a fantasy.Provider that routes to different backends
// based on the model ID. MiniMax M2.5 uses Anthropic API format, while
// GLM-5 and Kimi K2.5 use OpenAI-compatible format.
type zenGoHybridProvider struct {
	openaiProvider    fantasy.Provider
	anthropicProvider fantasy.Provider
}

func newZenGoHybridProvider(apiKey string) (fantasy.Provider, error) {
	openaiProv, err := openaicompat.New(
		openaicompat.WithAPIKey(apiKey),
		openaicompat.WithBaseURL(zenGoAPIEndpoint),
		openaicompat.WithHTTPClient(http1OnlyClient),
	)
	if err != nil {
		return nil, fmt.Errorf("create openai-compatible provider: %w", err)
	}

	// Anthropic provider adds its own /v1 path, so we use the endpoint without /v1
	anthropicProv, err := anthropic.New(
		anthropic.WithAPIKey(apiKey),
		anthropic.WithBaseURL(zenGoAnthropicAPIEndpoint),
		anthropic.WithHTTPClient(http1OnlyClient),
	)
	if err != nil {
		return nil, fmt.Errorf("create anthropic provider: %w", err)
	}

	return &zenGoHybridProvider{
		openaiProvider:    openaiProv,
		anthropicProvider: anthropicProv,
	}, nil
}

func (p *zenGoHybridProvider) Name() string {
	return "OpenCode Go"
}

func (p *zenGoHybridProvider) LanguageModel(ctx context.Context, modelID string) (fantasy.LanguageModel, error) {
	if zenGoAnthropicModels[modelID] {
		return p.anthropicProvider.LanguageModel(ctx, modelID)
	}
	return p.openaiProvider.LanguageModel(ctx, modelID)
}
