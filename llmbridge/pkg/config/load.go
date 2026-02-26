package config

import (
	"fmt"
	"os"
	"strings"

	"github.com/francescoalemanno/raijin-mono/llmbridge/pkg/catalog"
	"github.com/francescoalemanno/raijin-mono/llmbridge/pkg/llm"
)

const (
	DefaultMaxTokens     = 32000
	DefaultContextWindow = 200000
)

// NewConfig returns an empty bridge config.
func NewConfig() *Config {
	return &Config{
		Providers: make(map[string]ProviderConfig),
	}
}

// ConfigureProviders resolves env refs and infers provider defaults.
func (c *Config) ConfigureProviders() error {
	for id, pc := range c.Providers {
		if strings.HasPrefix(pc.APIKey, "$") {
			if val := os.Getenv(strings.TrimPrefix(pc.APIKey, "$")); val != "" {
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
	if c.Model.Provider == "" {
		return SelectedModel{}, ProviderConfig{}, false
	}
	pc, ok := c.GetProvider(c.Model.Provider)
	if !ok {
		return SelectedModel{}, ProviderConfig{}, false
	}
	return c.Model, pc, true
}

// MaxTokens returns the configured max output tokens for the active model.
func (c *Config) MaxTokens() int {
	if c.Model.MaxTokens > 0 {
		return int(c.Model.MaxTokens)
	}
	return DefaultMaxTokens
}
