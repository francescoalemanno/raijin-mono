package libagent

import (
	"context"
	"strings"

	"charm.land/fantasy"
)

const (
	DefaultMaxTokens     = 32000
	DefaultContextWindow = 200000
	DefaultMaxImages     = 20
)

// LanguageModel re-exports the runtime model interface so packages outside
// libagent don't need to import fantasy directly.
type LanguageModel = fantasy.LanguageModel

// ModelConfig stores a selected model configuration for serialization and runtime use.
// It uses only stdlib types so packages that don't import charm can hold it.
type ModelConfig struct {
	// Name is the display/store identifier (e.g. "anthropic/claude-opus-4-5").
	Name string `json:"name,omitempty" toml:"name,omitempty"`

	Provider string `json:"provider" toml:"provider"`
	Model    string `json:"model" toml:"model"`

	APIKey  string  `json:"api_key,omitempty" toml:"api_key,omitempty"`
	BaseURL *string `json:"base_url,omitempty" toml:"base_url,omitempty"`

	// ThinkingLevel controls reasoning intensity across all providers.
	// Mapped to provider-native options by BuildProviderOptions.
	ThinkingLevel ThinkingLevel `json:"thinking_level,omitempty" toml:"thinking_level,omitempty"`

	// ProviderOptions holds raw provider-specific option overrides.
	// These are merged on top of catalog defaults and ThinkingLevel-derived
	// options when BuildProviderOptions is called.
	ProviderOptions map[string]any `json:"provider_options,omitempty" toml:"provider_options,omitempty"`

	// MaxImages caps how many image attachments from recent history are kept in
	// runtime context for this model. Nil uses the default runtime budget.
	MaxImages *int `json:"max_images,omitempty" toml:"max_images,omitempty"`

	MaxTokens     int64    `json:"max_tokens,omitempty" toml:"max_tokens,omitempty"`
	ContextWindow int64    `json:"context_window,omitempty" toml:"context_window,omitempty"`
	Temperature   *float64 `json:"temperature,omitempty" toml:"temperature,omitempty"`
	TopP          *float64 `json:"top_p,omitempty" toml:"top_p,omitempty"`
	TopK          *int64   `json:"top_k,omitempty" toml:"top_k,omitempty"`
}

// Normalize returns a copy of ModelConfig with defaults applied.
func (m ModelConfig) Normalize() ModelConfig {
	m.ThinkingLevel = NormalizeThinkingLevel(m.ThinkingLevel)
	if m.MaxImages != nil {
		value := *m.MaxImages
		if value < 0 {
			m.MaxImages = nil
		} else {
			m.MaxImages = &value
		}
	}
	return m
}

// EffectiveMaxImages returns the configured image attachment budget.
func (m ModelConfig) EffectiveMaxImages() int {
	if m.MaxImages == nil || *m.MaxImages < 0 {
		return DefaultMaxImages
	}
	return *m.MaxImages
}

// RuntimeModel bundles a resolved language model with its config and catalog metadata.
type RuntimeModel struct {
	Model     fantasy.LanguageModel
	ModelInfo ModelInfo
	ModelCfg  ModelConfig
	// ProviderType is the catwalk provider type string (e.g. "openai", "anthropic").
	// Used by BuildProviderOptions to route to the correct provider option parser.
	ProviderType string
	// CatalogProviderOptions are default provider options from the catwalk catalog entry.
	CatalogProviderOptions map[string]any
}

// BuildCallProviderOptions constructs fantasy.ProviderOptions for a single LLM call
// using the model's provider type, thinking level, and raw overrides.
func (r RuntimeModel) BuildCallProviderOptions(systemPrompt string) fantasy.ProviderOptions {
	return BuildProviderOptions(
		r.ModelCfg.Provider,
		r.ProviderType,
		r.ModelCfg.Model,
		systemPrompt,
		r.ModelCfg.ThinkingLevel,
		r.CatalogProviderOptions,
		r.ModelCfg.ProviderOptions,
	)
}

// EffectiveContextWindow returns the best-known context window for this model,
// preferring the catalog value over the stored config value.
func (r RuntimeModel) EffectiveContextWindow() int64 {
	if r.ModelInfo.ContextWindow > 0 {
		return r.ModelInfo.ContextWindow
	}
	return r.ModelCfg.ContextWindow
}

// EffectiveMaxImages returns the configured image attachment budget for runtime context.
func (r RuntimeModel) EffectiveMaxImages() int {
	return r.ModelCfg.EffectiveMaxImages()
}

// MediaSupport reports runtime media capability metadata derived from catalog model info.
// When model identity is unknown, Known is false and callers should avoid destructive filtering.
func (r RuntimeModel) MediaSupport() MediaSupport {
	known := strings.TrimSpace(r.ModelInfo.ProviderID) != "" && strings.TrimSpace(r.ModelInfo.ModelID) != ""
	return MediaSupport{
		Known:   known,
		Enabled: r.ModelInfo.HasCapability(ModelCapabilityImage),
	}
}

// StreamText runs a single non-tool LLM call and collects text deltas via onDelta.
// It is a convenience helper for compaction and other one-shot generation needs.
func StreamText(ctx context.Context, model fantasy.LanguageModel, systemPrompt, prompt string, maxOutputTokens int64, onDelta func(string)) error {
	var msgs fantasy.Prompt
	if systemPrompt != "" {
		msgs = append(msgs, fantasy.NewSystemMessage(systemPrompt))
	}
	msgs = append(msgs, fantasy.NewUserMessage(prompt))

	call := fantasy.Call{
		Prompt: msgs,
	}
	if maxOutputTokens > 0 {
		call.MaxOutputTokens = &maxOutputTokens
	}

	stream, err := model.Stream(ctx, call)
	if err != nil {
		return err
	}
	for part := range stream {
		if part.Type == fantasy.StreamPartTypeTextDelta {
			onDelta(part.Delta)
		}
		if part.Type == fantasy.StreamPartTypeError {
			return part.Error
		}
	}
	return nil
}
