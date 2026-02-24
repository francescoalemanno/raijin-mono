package config

import (
	"strings"

	"github.com/francescoalemanno/raijin-mono/llmbridge/pkg/catalog"
	"github.com/francescoalemanno/raijin-mono/llmbridge/pkg/llm"
)

// SelectedModel stores the selected model configuration.
type SelectedModel struct {
	Model    string `json:"model" toml:"model"`
	Provider string `json:"provider" toml:"provider"`

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

// ModelConfig stores app-level selectable model settings.
type ModelConfig struct {
	Name          string            `toml:"name"`
	Provider      string            `toml:"provider"`
	APIKey        string            `toml:"api_key"`
	Model         string            `toml:"model"`
	MaxTokens     int               `toml:"max_tokens"`
	ContextWindow int64             `toml:"context_window"`
	BaseURL       *string           `toml:"base_url"`
	ThinkingLevel llm.ThinkingLevel `toml:"thinking_level"`
}

// ToProviderConfig converts the stored model settings into provider settings.
func (m ModelConfig) ToProviderConfig() ProviderConfig {
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

// ToSelectedModel converts the stored model settings into a selected model.
func (m ModelConfig) ToSelectedModel() SelectedModel {
	thinkingLevel := llm.NormalizeThinkingLevel(m.ThinkingLevel)
	if strings.EqualFold(strings.TrimSpace(m.Provider), catalog.OpenAICodexProviderID) && !thinkingLevel.Enabled() {
		thinkingLevel = llm.ThinkingLevelMedium
	}

	return SelectedModel{
		Model:         m.Model,
		Provider:      m.Provider,
		MaxTokens:     int64(m.MaxTokens),
		ContextWindow: m.ContextWindow,
		ThinkingLevel: thinkingLevel,
	}
}

// Config is the bridge-owned plain configuration model.
type Config struct {
	Providers map[string]ProviderConfig
	Model     SelectedModel
}

// FileConfig is the on-disk TOML shape.
type FileConfig struct {
	Providers map[string]FileProviderConfig `toml:"providers"`
	Model     FileModelConfig               `toml:"model"`
}

// FileProviderConfig is the on-disk provider config shape.
type FileProviderConfig struct {
	Name               *string           `toml:"name"`
	Type               *string           `toml:"type"`
	APIKey             *string           `toml:"api_key"`
	BaseURL            *string           `toml:"base_url"`
	Disable            *bool             `toml:"disable"`
	SystemPromptPrefix *string           `toml:"system_prompt_prefix"`
	ExtraHeaders       map[string]string `toml:"extra_headers"`
	ProviderOptions    map[string]any    `toml:"provider_options"`
}

// FileModelConfig is the on-disk model config shape.
type FileModelConfig struct {
	ThinkingLevel *string  `toml:"thinking_level"`
	Model         *string  `toml:"model"`
	Provider      *string  `toml:"provider"`
	MaxTokens     *int64   `toml:"max_tokens"`
	Temperature   *float64 `toml:"temperature"`
	TopP          *float64 `toml:"top_p"`
	TopK          *int64   `toml:"top_k"`
}
