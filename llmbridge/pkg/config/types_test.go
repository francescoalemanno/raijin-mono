package config

import (
	"testing"

	"github.com/francescoalemanno/raijin-mono/llmbridge/pkg/catalog"
	"github.com/francescoalemanno/raijin-mono/llmbridge/pkg/llm"
)

func TestSelectedModelNormalize_EnablesThinkForOpenAICodex(t *testing.T) {
	t.Parallel()

	selected := (SelectedModel{Provider: catalog.OpenAICodexProviderID, Model: "gpt-5.3-codex"}).Normalize()
	if selected.ThinkingLevel != llm.ThinkingLevelMedium {
		t.Fatal("expected openai-codex models to default thinking on")
	}
}

func TestSelectedModelNormalize_PreservesThinkForOtherProviders(t *testing.T) {
	t.Parallel()

	selected := (SelectedModel{Provider: "openai", Model: "gpt-5"}).Normalize()
	if selected.ThinkingLevel != llm.ThinkingLevelOff {
		t.Fatal("expected non-codex providers to preserve configured think flag")
	}
}

func TestSelectedModelNormalize_PrefersThinkingLevel(t *testing.T) {
	t.Parallel()

	selected := (SelectedModel{Provider: "openai", Model: "gpt-5", ThinkingLevel: llm.ThinkingLevelHigh}).Normalize()
	if selected.ThinkingLevel != llm.ThinkingLevelHigh {
		t.Fatal("expected thinking_level to control normalized model thinking")
	}
}
