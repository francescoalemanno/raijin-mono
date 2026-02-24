package chat

import "testing"

func TestResolveCatalogProviderAPIKey(t *testing.T) {
	t.Run("empty", func(t *testing.T) {
		if got := resolveCatalogProviderAPIKey("", ""); got != "" {
			t.Fatalf("expected empty, got %q", got)
		}
	})

	t.Run("literal", func(t *testing.T) {
		if got := resolveCatalogProviderAPIKey("", "literal-key"); got != "literal-key" {
			t.Fatalf("expected literal key, got %q", got)
		}
	})

	t.Run("env placeholder", func(t *testing.T) {
		t.Setenv("TEST_MODEL_ADD_API_KEY", "from-env")
		if got := resolveCatalogProviderAPIKey("", "$TEST_MODEL_ADD_API_KEY"); got != "from-env" {
			t.Fatalf("expected env value, got %q", got)
		}
	})

	t.Run("fallback env by provider id", func(t *testing.T) {
		t.Setenv("SYNTHETIC_API_KEY", "from-fallback")
		if got := resolveCatalogProviderAPIKey("synthetic", ""); got != "from-fallback" {
			t.Fatalf("expected fallback env value, got %q", got)
		}
	})

	t.Run("missing env", func(t *testing.T) {
		if got := resolveCatalogProviderAPIKey("", "$TEST_MODEL_ADD_MISSING_API_KEY"); got != "" {
			t.Fatalf("expected empty, got %q", got)
		}
	})
}

func TestShouldSkipAPIKeyPrompt(t *testing.T) {
	if !shouldSkipAPIKeyPrompt("openai-codex") {
		t.Fatalf("expected openai-codex to skip API key prompt")
	}
	if shouldSkipAPIKeyPrompt("openai") {
		t.Fatalf("expected openai not to skip API key prompt")
	}
}
