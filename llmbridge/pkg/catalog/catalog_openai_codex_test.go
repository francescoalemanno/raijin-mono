package catalog

import (
	"context"
	"strings"
	"testing"
)

func TestNewRaijinSource_IncludesOpenAICodexProvider(t *testing.T) {
	t.Parallel()

	providers, err := NewRaijinSource().ListProviders(context.Background())
	if err != nil {
		t.Fatalf("ListProviders returned error: %v", err)
	}

	var found *Provider
	for i := range providers {
		if providers[i].ID == OpenAICodexProviderID {
			found = &providers[i]
			break
		}
	}
	if found == nil {
		t.Fatalf("expected %q provider to be present", OpenAICodexProviderID)
	}
	if found.APIEndpoint != "https://chatgpt.com/backend-api/codex" {
		t.Fatalf("unexpected openai-codex endpoint: %q", found.APIEndpoint)
	}
	if len(found.Models) == 0 {
		t.Fatal("expected openai-codex provider to include models")
	}
	var has53 bool
	for _, model := range found.Models {
		if !strings.Contains(strings.ToLower(model.ID), "codex") {
			t.Fatalf("unexpected non-codex model in openai-codex provider: %q", model.ID)
		}
		if !model.CanReason {
			t.Fatalf("expected codex model %q to support reasoning", model.ID)
		}
		if strings.TrimSpace(model.DefaultReasoningEffort) == "" {
			t.Fatalf("expected codex model %q to include default reasoning effort", model.ID)
		}
		if model.ID == "gpt-5.3-codex" {
			has53 = true
		}
	}
	if !has53 {
		t.Fatal("expected openai-codex provider to include gpt-5.3-codex")
	}
}
