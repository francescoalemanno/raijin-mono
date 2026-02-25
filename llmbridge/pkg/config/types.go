package config

import (
	"strings"

	"github.com/francescoalemanno/raijin-mono/llmbridge/pkg/catalog"
	"github.com/francescoalemanno/raijin-mono/llmbridge/pkg/llm"
)

// Config is the runtime configuration model.
// Model state is loaded from models.toml, not from disk.
type Config struct {
	Providers map[string]ProviderConfig
	Model     SelectedModel
}

// SelectedModel stores the selected model configuration.
type SelectedModel struct {
	// Name is the identifier for this model configuration in the store.
	Name string `json:"name,omitempty" toml:"name,omitempty"`

	Model    string `json:"model" toml:"model"`
	Provider string `json:"provider" toml:"provider"`

	// APIKey is the API key for the provider (storage only, not used in runtime Config).
	APIKey string `json:"api_key,omitempty" toml:"api_key,omitempty"`
	// BaseURL is an optional custom base URL for the provider.
	BaseURL *string `json:"base_url,omitempty" toml:"base_url,omitempty"`

	ThinkingLevel llm.ThinkingLevel `json:"thinking_level,omitempty" toml:"thinking_level,omitempty"`

	MaxTokens     int64    `json:"max_tokens,omitempty" toml:"max_tokens,omitempty"`
	ContextWindow int64    `json:"context_window,omitempty" toml:"context_window,omitempty"`
	Temperature   *float64 `json:"temperature,omitempty" toml:"temperature,omitempty"`
	TopP          *float64 `json:"top_p,omitempty" toml:"top_p,omitempty"`
	TopK          *int64   `json:"top_k,omitempty" toml:"top_k,omitempty"`

	ProviderOptions map[string]any `json:"provider_options,omitempty" toml:"provider_options,omitempty"`
}

// RuntimeModel bridges runtime state with selected model settings.
type RuntimeModel struct {
	Runtime  llm.Runtime
	Metadata llm.ModelMetadata
	ModelCfg SelectedModel
}

// ProviderConfig stores provider settings.
type ProviderConfig struct {
	ID      string           `json:"id,omitempty" toml:"id,omitempty"`
	Name    string           `json:"name,omitempty" toml:"name,omitempty"`
	BaseURL string           `json:"base_url,omitempty" toml:"base_url,omitempty"`
	Type    llm.ProviderType `json:"type,omitempty" toml:"type,omitempty"`

	APIKey string `json:"api_key,omitempty" toml:"api_key,omitempty"`

	Disable            bool   `json:"disable,omitempty" toml:"disable,omitempty"`
	SystemPromptPrefix string `json:"system_prompt_prefix,omitempty" toml:"system_prompt_prefix,omitempty"`

	ExtraHeaders    map[string]string `json:"extra_headers,omitempty" toml:"extra_headers,omitempty"`
	ProviderOptions map[string]any    `json:"provider_options,omitempty" toml:"provider_options,omitempty"`

	Models []catalog.Model `json:"models,omitempty" toml:"models,omitempty"`
}

// ToProviderConfig converts the selected model into provider settings.
func (m SelectedModel) ToProviderConfig() ProviderConfig {
	pc := ProviderConfig{
		ID:     m.Provider,
		Name:   m.Provider,
		APIKey: m.APIKey,
		Type:   llm.InferProviderType(m.Provider),
	}
	if m.BaseURL != nil {
		pc.BaseURL = *m.BaseURL
	}
	return pc
}

// Normalize returns a normalized copy of the selected model with defaults applied.
func (m SelectedModel) Normalize() SelectedModel {
	thinkingLevel := llm.NormalizeThinkingLevel(m.ThinkingLevel)
	if strings.EqualFold(strings.TrimSpace(m.Provider), catalog.OpenAICodexProviderID) && !thinkingLevel.Enabled() {
		thinkingLevel = llm.ThinkingLevelMedium
	}

	return SelectedModel{
		Name:            m.Name,
		Model:           m.Model,
		Provider:        m.Provider,
		APIKey:          m.APIKey,
		BaseURL:         m.BaseURL,
		MaxTokens:       m.MaxTokens,
		ContextWindow:   m.ContextWindow,
		ThinkingLevel:   thinkingLevel,
		Temperature:     m.Temperature,
		TopP:            m.TopP,
		TopK:            m.TopK,
		ProviderOptions: m.ProviderOptions,
	}
}
