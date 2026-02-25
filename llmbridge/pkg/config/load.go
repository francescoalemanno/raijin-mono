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
	cfg := NewConfig()
	if _, err := toml.DecodeFile(path, cfg); err != nil {
		return nil, err
	}
	cfg.Model.ThinkingLevel = llm.NormalizeThinkingLevel(cfg.Model.ThinkingLevel)
	return cfg, nil
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

// ConfigPath returns the path to the config file.
func ConfigPath() string {
	return paths.RaijinConfigPath()
}

// Save writes the current config to the config file.
// It preserves existing data and only updates the UI section.
func (c *Config) Save() error {
	path := paths.RaijinConfigPath()
	if path == "" {
		return fmt.Errorf("failed to resolve config dir")
	}

	var cfg Config
	if existing, err := FromFile(path); err == nil {
		cfg = *existing
	}

	if c.UI.Theme != "" {
		cfg.UI = c.UI
	}

	tmpPath := path + ".tmp"
	f, err := os.Create(tmpPath)
	if err != nil {
		return fmt.Errorf("failed to create temp config: %w", err)
	}
	defer f.Close()

	encoder := toml.NewEncoder(f)
	if err := encoder.Encode(cfg); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("failed to encode config: %w", err)
	}
	f.Close()

	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}

	return nil
}
