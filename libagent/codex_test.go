package libagent_test

import (
	"strings"
	"testing"

	"github.com/francescoalemanno/raijin-mono/libagent"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCodexProvider_HasModels(t *testing.T) {
	p := libagent.CodexProvider()
	assert.Equal(t, libagent.CodexProviderID, p.ID)
	assert.NotEmpty(t, p.Name)
	require.NotEmpty(t, p.Models, "CodexProvider should have at least one model")
}

func TestCodexProvider_AllModelsAreOpenAIGPTModels(t *testing.T) {
	p := libagent.CodexProvider()
	for _, m := range p.Models {
		id := strings.ToLower(m.ModelID)
		assert.Regexp(t, `^gpt-\d+(?:\.\d+)?(?:-codex)?$`, id, "model %q should match bare GPT or GPT-codex pattern", m.ModelID)
	}
}

func TestCodexProvider_AllModelsCanReason(t *testing.T) {
	p := libagent.CodexProvider()
	for _, m := range p.Models {
		assert.True(t, m.CanReason, "codex model %q should have CanReason=true", m.ModelID)
	}
}

func TestCodexProvider_IncludesKnownModel(t *testing.T) {
	p := libagent.CodexProvider()
	found := false
	for _, m := range p.Models {
		if m.ModelID == "gpt-5.3-codex" {
			found = true
			break
		}
	}
	assert.True(t, found, "expected gpt-5.3-codex to be present in CodexProvider")
}

func TestCodexProvider_IncludesGPT54FromEmbeddedCatalog(t *testing.T) {
	p := libagent.CodexProvider()
	var found *libagent.ModelInfo
	for i := range p.Models {
		if p.Models[i].ModelID == "gpt-5.4" {
			found = &p.Models[i]
			break
		}
	}
	require.NotNil(t, found, "expected gpt-5.4 to be present in CodexProvider")
	assert.Equal(t, libagent.CodexProviderID, found.ProviderID)
	assert.Equal(t, int64(1050000), found.ContextWindow)
	assert.Equal(t, int64(128000), found.DefaultMaxTokens)
	assert.True(t, found.CanReason)
	assert.True(t, found.SupportsImages)
	assert.Equal(t, 2.5, found.CostPer1MIn)
	assert.Equal(t, 15.0, found.CostPer1MOut)
	assert.Equal(t, 0.25, found.CostPer1MInCached)
}

func TestCodexProvider_ExcludesNonMatchingGPTVariants(t *testing.T) {
	p := libagent.CodexProvider()
	seen := map[string]bool{}
	for _, m := range p.Models {
		seen[m.ModelID] = true
	}
	assert.False(t, seen["gpt-5.4-pro"], "gpt-5.4-pro should not be exposed by the OAuth provider")
	assert.False(t, seen["gpt-5.1-codex-max"], "gpt-5.1-codex-max should not be exposed by the OAuth provider")
}

func TestCatalog_DefaultCatalog_IncludesCodex(t *testing.T) {
	cat := libagent.DefaultCatalog()

	info, _, err := cat.FindModel(libagent.CodexProviderID, "gpt-5.3-codex")
	require.NoError(t, err)
	assert.Equal(t, libagent.CodexProviderID, info.ProviderID)
	assert.Equal(t, "gpt-5.3-codex", info.ModelID)
	assert.True(t, info.CanReason)
}

func TestCatalog_DefaultCatalog_IncludesGPT54InCodexProvider(t *testing.T) {
	cat := libagent.DefaultCatalog()

	info, _, err := cat.FindModel(libagent.CodexProviderID, "gpt-5.4")
	require.NoError(t, err)
	assert.Equal(t, libagent.CodexProviderID, info.ProviderID)
	assert.Equal(t, "gpt-5.4", info.ModelID)
	assert.Equal(t, int64(1050000), info.ContextWindow)
	assert.Equal(t, int64(128000), info.DefaultMaxTokens)
	assert.True(t, info.CanReason)
	assert.True(t, info.SupportsImages)
}

func TestCatalog_DefaultCatalog_CodexNotFoundUnknownModel(t *testing.T) {
	cat := libagent.DefaultCatalog()

	_, _, err := cat.FindModel(libagent.CodexProviderID, "nonexistent-model")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestCatalog_DefaultCatalog_ListProviders_IncludesCodex(t *testing.T) {
	cat := libagent.DefaultCatalog()

	providers := cat.ListProviders()
	var found bool
	for _, p := range providers {
		if string(p.ID) == libagent.CodexProviderID {
			found = true
		}
	}
	assert.True(t, found, "DefaultCatalog should include CodexProvider in ListProviders")
}

func TestCatalog_CustomProvider_TakesPrecedenceOverCatwalk(t *testing.T) {
	// If a custom provider shares an ID with a catwalk provider,
	// the custom one wins in FindModel.
	cat := libagent.DefaultCatalog()

	// We use nil Build — FindModel does not invoke it.
	cat.AddCustomProvider(libagent.CustomProvider{
		ID:   "openai",
		Name: "Custom OpenAI Override",
		Models: []libagent.ModelInfo{
			{ProviderID: "openai", ModelID: "my-custom-model"},
		},
		Build: nil,
	})

	info, catwalkP, err := cat.FindModel("openai", "my-custom-model")
	require.NoError(t, err)
	assert.Equal(t, "my-custom-model", info.ModelID)
	// catwalkP is zero-valued for custom providers.
	assert.Empty(t, string(catwalkP.ID))
}
