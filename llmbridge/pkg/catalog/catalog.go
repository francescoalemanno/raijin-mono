package catalog

import (
	"context"
	"slices"
	"strings"

	"github.com/francescoalemanno/raijin-mono/llmbridge/pkg/llm"

	"charm.land/catwalk/pkg/catwalk"
	"charm.land/catwalk/pkg/embedded"
)

// Model is a provider-agnostic catalog model entry.
type Model struct {
	ID                     string
	Name                   string
	ContextWindow          int64
	DefaultMaxTokens       int64
	CanReason              bool
	SupportsImages         bool
	CostPer1MIn            float64
	CostPer1MOut           float64
	CostPer1MInCached      float64
	CostPer1MOutCached     float64
	ReasoningLevels        []string
	DefaultReasoningEffort string
}

// Provider is a provider-agnostic catalog provider entry.
type Provider struct {
	ID          string
	Name        string
	Type        llm.ProviderType
	APIEndpoint string
	Models      []Model
}

// Source provides model catalog lookups.
type Source interface {
	ListProviders(ctx context.Context) ([]Provider, error)
	FindModel(ctx context.Context, providerID, modelID string) (Model, bool, error)
}

// Overlay appends or overrides providers in the default catalog.
type Overlay struct {
	Providers []Provider
}

//go:generate go run ../../cmd/zen-models-gen -output zen_models_generated.go
//go:generate go run ../../cmd/synthetic-models-gen -output synthetic_models_generated.go

const (
	ZenProviderID         = "opencode"
	OpenAICodexProviderID = "openai-codex"
	SyntheticProviderID   = "synthetic"
)

type defaultSource struct {
	providers []Provider
}

// NewRaijinSource returns the default source with Raijin-specific provider overlays.
func NewRaijinSource() Source {
	return NewDefaultSource(Overlay{Providers: []Provider{
		ZenProvider(),
		OpenAICodexProvider(),
		SyntheticProvider(),
	}})
}

// IsZenProvider reports whether a provider is the built-in OpenCode Zen endpoint.
func IsZenProvider(providerID string) bool {
	return providerID == ZenProviderID
}

// OpenAICodexProvider returns a provider that uses ChatGPT OAuth-backed Codex access.
func OpenAICodexProvider() Provider {
	models := mergeOpenAICodexModels(openAICodexModelsFromEmbedded(), openAICodexModelsFromZen())

	return cloneProvider(Provider{
		Name:        "OpenAI Codex (ChatGPT OAuth)",
		ID:          OpenAICodexProviderID,
		APIEndpoint: "https://chatgpt.com/backend-api/codex",
		Type:        llm.ProviderOpenAI,
		Models:      models,
	})
}

// ZenProvider returns the OpenCode Zen provider overlay.
func ZenProvider() Provider {
	return cloneProvider(Provider{
		Name:        "OpenCode Zen",
		ID:          ZenProviderID,
		APIEndpoint: "https://opencode.ai/zen/v1",
		Type:        llm.ProviderOpenAICompat,
		Models:      zenGeneratedModels,
	})
}

// SyntheticProvider returns the Synthetic provider overlay.
func SyntheticProvider() Provider {
	return cloneProvider(Provider{
		Name:        "Synthetic",
		ID:          SyntheticProviderID,
		APIEndpoint: "https://api.synthetic.new/openai/v1",
		Type:        llm.ProviderOpenAICompat,
		Models:      syntheticGeneratedModels,
	})
}

// IsSyntheticProvider reports whether a provider is the built-in Synthetic endpoint.
func IsSyntheticProvider(providerID string) bool {
	return providerID == SyntheticProviderID
}

// NewDefaultSource returns a source backed by catwalk embedded providers.
func NewDefaultSource(overlay Overlay) Source {
	providerByID := make(map[string]Provider)

	for _, p := range embedded.GetAll() {
		provider := fromCatwalkProvider(p)
		if len(provider.Models) == 0 {
			continue
		}
		// Skip synthetic provider from catwalk - we use our own generated version
		if provider.ID == SyntheticProviderID {
			continue
		}
		providerByID[provider.ID] = provider
	}

	for _, p := range overlay.Providers {
		providerByID[p.ID] = cloneProvider(p)
	}

	providers := make([]Provider, 0, len(providerByID))
	for _, p := range providerByID {
		providers = append(providers, p)
	}

	slices.SortFunc(providers, func(a, b Provider) int {
		aName := strings.TrimSpace(a.Name)
		if aName == "" {
			aName = a.ID
		}
		bName := strings.TrimSpace(b.Name)
		if bName == "" {
			bName = b.ID
		}
		return strings.Compare(aName, bName)
	})

	return &defaultSource{providers: providers}
}

func (s *defaultSource) ListProviders(context.Context) ([]Provider, error) {
	out := make([]Provider, 0, len(s.providers))
	for _, p := range s.providers {
		out = append(out, cloneProvider(p))
	}
	return out, nil
}

func (s *defaultSource) FindModel(_ context.Context, providerID, modelID string) (Model, bool, error) {
	for _, provider := range s.providers {
		if provider.ID != providerID {
			continue
		}
		for _, model := range provider.Models {
			if model.ID == modelID {
				return cloneModel(model), true, nil
			}
		}
		return Model{}, false, nil
	}
	return Model{}, false, nil
}

func fromCatwalkProvider(provider catwalk.Provider) Provider {
	models := make([]Model, 0, len(provider.Models))
	for _, model := range provider.Models {
		models = append(models, fromCatwalkModel(model))
	}
	return Provider{
		ID:          string(provider.ID),
		Name:        provider.Name,
		Type:        llm.ProviderType(provider.Type),
		APIEndpoint: provider.APIEndpoint,
		Models:      models,
	}
}

func fromCatwalkModel(model catwalk.Model) Model {
	return Model{
		ID:                     model.ID,
		Name:                   model.Name,
		ContextWindow:          model.ContextWindow,
		DefaultMaxTokens:       model.DefaultMaxTokens,
		CanReason:              model.CanReason,
		SupportsImages:         model.SupportsImages,
		CostPer1MIn:            model.CostPer1MIn,
		CostPer1MOut:           model.CostPer1MOut,
		CostPer1MInCached:      model.CostPer1MInCached,
		CostPer1MOutCached:     model.CostPer1MOutCached,
		ReasoningLevels:        append([]string(nil), model.ReasoningLevels...),
		DefaultReasoningEffort: model.DefaultReasoningEffort,
	}
}

func cloneProvider(provider Provider) Provider {
	copied := provider
	copied.Models = make([]Model, len(provider.Models))
	for i := range provider.Models {
		copied.Models[i] = cloneModel(provider.Models[i])
	}
	return copied
}

func cloneModel(model Model) Model {
	copied := model
	copied.ReasoningLevels = append([]string(nil), model.ReasoningLevels...)
	return copied
}

func openAICodexModelsFromEmbedded() []Model {
	var models []Model
	for _, provider := range embedded.GetAll() {
		for _, model := range provider.Models {
			modelID := strings.ToLower(model.ID)
			if strings.Contains(modelID, "codex") && strings.HasPrefix(modelID, "gpt-") {
				models = append(models, fromCatwalkModel(model))
			}
		}
	}
	return models
}

func openAICodexModelsFromZen() []Model {
	var models []Model
	for _, model := range zenGeneratedModels {
		if strings.Contains(strings.ToLower(model.ID), "codex") {
			models = append(models, cloneModel(model))
		}
	}
	return models
}

func mergeOpenAICodexModels(primary []Model, fallback []Model) []Model {
	byID := make(map[string]Model, len(primary)+len(fallback))
	for _, model := range primary {
		byID[model.ID] = normalizeOpenAICodexModel(model)
	}
	for _, model := range fallback {
		if _, exists := byID[model.ID]; exists {
			continue
		}
		byID[model.ID] = normalizeOpenAICodexModel(model)
	}

	out := make([]Model, 0, len(byID))
	for _, model := range byID {
		out = append(out, model)
	}
	slices.SortFunc(out, func(a, b Model) int {
		return strings.Compare(a.ID, b.ID)
	})
	return out
}

func normalizeOpenAICodexModel(model Model) Model {
	model.CanReason = true
	if len(model.ReasoningLevels) == 0 {
		model.ReasoningLevels = []string{"low", "medium", "high"}
	}
	if strings.TrimSpace(model.DefaultReasoningEffort) == "" {
		model.DefaultReasoningEffort = "medium"
	}
	return model
}
