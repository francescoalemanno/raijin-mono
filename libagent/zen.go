package libagent

import (
	"charm.land/catwalk/pkg/catwalk"
	"charm.land/fantasy"
	"charm.land/fantasy/providers/openaicompat"
)

// ZenProviderID is the stable identifier for the OpenCode Zen custom provider.
const ZenProviderID = "opencode"

const zenAPIEndpoint = "https://opencode.ai/zen/v1"

//go:generate go run ./cmd/zen -output zen_models_generated.go

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

func buildZenProvider(apiKey string) (fantasy.Provider, error) {
	return openaicompat.New(
		openaicompat.WithAPIKey(apiKey),
		openaicompat.WithBaseURL(zenAPIEndpoint),
		openaicompat.WithHTTPClient(http1OnlyClient),
	)
}
