package config

import (
	"testing"

	"github.com/francescoalemanno/raijin-mono/llmbridge/pkg/catalog"
	"github.com/francescoalemanno/raijin-mono/llmbridge/pkg/llm"
)

func TestModelConfigToSelectedModel_EnablesThinkForOpenAICodex(t *testing.T) {
	t.Parallel()

	selected := (ModelConfig{Provider: catalog.OpenAICodexProviderID, Model: "gpt-5.3-codex"}).ToSelectedModel()
	if selected.ThinkingLevel != llm.ThinkingLevelMedium {
		t.Fatal("expected openai-codex models to default thinking on")
	}
}

func TestModelConfigToSelectedModel_PreservesThinkForOtherProviders(t *testing.T) {
	t.Parallel()

	selected := (ModelConfig{Provider: "openai", Model: "gpt-5"}).ToSelectedModel()
	if selected.ThinkingLevel != llm.ThinkingLevelOff {
		t.Fatal("expected non-codex providers to preserve configured think flag")
	}
}

func TestModelConfigToSelectedModel_PrefersThinkingLevel(t *testing.T) {
	t.Parallel()

	selected := (ModelConfig{Provider: "openai", Model: "gpt-5", ThinkingLevel: llm.ThinkingLevelHigh}).ToSelectedModel()
	if selected.ThinkingLevel != llm.ThinkingLevelHigh {
		t.Fatal("expected thinking_level to control normalized model thinking")
	}
}
