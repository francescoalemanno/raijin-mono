package config

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/BurntSushi/toml"
	"github.com/francescoalemanno/raijin-mono/internal/paths"
	"github.com/francescoalemanno/raijin-mono/llmbridge/pkg/catalog"
	"github.com/francescoalemanno/raijin-mono/llmbridge/pkg/llm"
)

const (
	DefaultMaxTokens     = 32000
	DefaultContextWindow = 200000
)

var ErrConfigNotFound = errors.New("config file not found")

// NewConfig returns an empty bridge config.
func NewConfig() *Config {
	return &Config{
		Providers: make(map[string]ProviderConfig),
	}
}

// Load reads config from the default app path.
func Load() (*Config, error) {
	path := paths.RaijinConfigPath()
	if path == "" {
		return nil, fmt.Errorf("failed to resolve config dir")
	}
	if _, err := os.Stat(path); err != nil {
		return nil, ErrConfigNotFound
	}

	cfg, err := FromFile(path)
	if err != nil {
		return nil, err
	}
	if err := cfg.ConfigureProviders(); err != nil {
		return nil, fmt.Errorf("failed to configure providers: %w", err)
	}
	return cfg, nil
}

// FromFile parses TOML config from disk.
func FromFile(path string) (*Config, error) {
	var fileCfg FileConfig
	if _, err := toml.DecodeFile(path, &fileCfg); err != nil {
		return nil, err
	}

	cfg := NewConfig()

	for id, fp := range fileCfg.Providers {
		pc := ProviderConfig{
			ID:                 id,
			Name:               valueOrEmpty(fp.Name),
			BaseURL:            valueOrEmpty(fp.BaseURL),
			APIKey:             valueOrEmpty(fp.APIKey),
			SystemPromptPrefix: valueOrEmpty(fp.SystemPromptPrefix),
			ExtraHeaders:       fp.ExtraHeaders,
			ProviderOptions:    fp.ProviderOptions,
		}
		if fp.Type != nil {
			pc.Type = llm.ProviderType(*fp.Type)
		}
		if fp.Disable != nil {
			pc.Disable = *fp.Disable
		}
		cfg.Providers[id] = pc
	}

	fm := fileCfg.Model
	cfg.Model = SelectedModel{
		ThinkingLevel: llm.NormalizeThinkingLevel(llm.ThinkingLevel(valueOrEmpty(fm.ThinkingLevel))),
		Model:         valueOrEmpty(fm.Model),
		Provider:      valueOrEmpty(fm.Provider),
		Temperature:   fm.Temperature,
		TopP:          fm.TopP,
		TopK:          fm.TopK,
	}
	if fm.MaxTokens != nil {
		cfg.Model.MaxTokens = *fm.MaxTokens
	}

	return cfg, nil
}

// ConfigureProviders resolves env refs and infers provider defaults.
func (c *Config) ConfigureProviders() error {
	for id, pc := range c.Providers {
		if pc.APIKey != "" && strings.HasPrefix(pc.APIKey, "$") {
			envVar := strings.TrimPrefix(pc.APIKey, "$")
			if val := os.Getenv(envVar); val != "" {
				pc.APIKey = val
			}
		}

		if strings.EqualFold(id, catalog.OpenAICodexProviderID) {
			var err error
			pc, err = configureOpenAICodexProvider(pc)
			if err != nil {
				return fmt.Errorf("configure openai-codex provider: %w", err)
			}
		}

		if pc.Type == "" {
			pc.Type = llm.InferProviderType(id)
		}
		if pc.Name == "" {
			pc.Name = id
		}
		c.Providers[id] = pc
	}
	return nil
}

// IsConfigured reports whether a model and provider are configured.
func (c *Config) IsConfigured() bool {
	return len(c.Providers) > 0 && c.Model.Provider != ""
}

// GetProvider returns provider config by ID.
func (c *Config) GetProvider(providerID string) (ProviderConfig, bool) {
	pc, ok := c.Providers[providerID]
	return pc, ok
}

// GetModel returns a provider model from provider metadata.
func (c *Config) GetModel(providerID, modelID string) *catalog.Model {
	pc, ok := c.Providers[providerID]
	if !ok {
		return nil
	}
	for i := range pc.Models {
		if pc.Models[i].ID == modelID {
			return &pc.Models[i]
		}
	}
	return nil
}

// Resolve resolves "$ENV" values.
func (c *Config) Resolve(value string) (string, error) {
	if strings.HasPrefix(value, "$") {
		envVar := strings.TrimPrefix(value, "$")
		if val := os.Getenv(envVar); val != "" {
			return val, nil
		}
		return "", fmt.Errorf("environment variable %s not set", envVar)
	}
	return value, nil
}

// ActiveModel returns the configured model and its provider.
func (c *Config) ActiveModel() (SelectedModel, ProviderConfig, bool) {
	sm := c.Model
	if sm.Provider == "" {
		return SelectedModel{}, ProviderConfig{}, false
	}
	pc, ok := c.GetProvider(sm.Provider)
	if !ok {
		return SelectedModel{}, ProviderConfig{}, false
	}
	return sm, pc, true
}

// MaxTokens returns the configured max output tokens for the active model.
func (c *Config) MaxTokens() int {
	if c.Model.MaxTokens > 0 {
		return int(c.Model.MaxTokens)
	}
	return DefaultMaxTokens
}

func valueOrEmpty(val *string) string {
	if val == nil {
		return ""
	}
	return *val
}
