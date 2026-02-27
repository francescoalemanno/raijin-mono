package libagent

import (
	"charm.land/catwalk/pkg/catwalk"
	"charm.land/fantasy"
	"charm.land/fantasy/providers/openaicompat"
)

// SyntheticProviderID is the stable identifier for the Synthetic custom provider.
const SyntheticProviderID = "synthetic"

const syntheticAPIEndpoint = "https://api.synthetic.new/openai/v1"

//go:generate go run ./cmd/synthetic -output synthetic_models_generated.go

// SyntheticProvider returns a CustomProvider for Synthetic.
func SyntheticProvider() CustomProvider {
	return CustomProvider{
		ID:          SyntheticProviderID,
		Name:        "Synthetic",
		Type:        catwalk.TypeOpenAICompat,
		APIKey:      "$SYNTHETIC_API_KEY",
		APIEndpoint: syntheticAPIEndpoint,
		Models:      syntheticGeneratedModels,
		Build:       buildSyntheticProvider,
	}
}

func buildSyntheticProvider(apiKey string) (fantasy.Provider, error) {
	return openaicompat.New(
		openaicompat.WithAPIKey(apiKey),
		openaicompat.WithBaseURL(syntheticAPIEndpoint),
		openaicompat.WithHTTPClient(http1OnlyClient),
	)
}
