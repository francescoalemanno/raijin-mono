package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/francescoalemanno/raijin-mono/llmbridge/pkg/llm"
)

func TestFromFile_ModelThinkingLevelPreferred(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "config.toml")
	content := "[model]\nprovider='openai'\nmodel='gpt-5'\nthinking_level='high'\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := FromFile(path)
	if err != nil {
		t.Fatalf("FromFile error: %v", err)
	}

	if cfg.Model.ThinkingLevel != llm.ThinkingLevelHigh {
		t.Fatalf("thinking level = %q, want high", cfg.Model.ThinkingLevel)
	}
}

func TestFromFile_ModelThinkingLevelDefaultsToOff(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "config.toml")
	content := "[model]\nprovider='openai'\nmodel='gpt-5'\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := FromFile(path)
	if err != nil {
		t.Fatalf("FromFile error: %v", err)
	}

	if cfg.Model.ThinkingLevel != llm.ThinkingLevelOff {
		t.Fatalf("thinking level = %q, want off", cfg.Model.ThinkingLevel)
	}
}
