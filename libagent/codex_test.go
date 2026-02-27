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

func TestCodexProvider_AllModelsAreCodex(t *testing.T) {
	p := libagent.CodexProvider()
	for _, m := range p.Models {
		id := strings.ToLower(m.ModelID)
		assert.True(t, strings.HasPrefix(id, "gpt-"), "model %q should start with gpt-", m.ModelID)
		assert.Contains(t, id, "codex", "model %q should contain 'codex'", m.ModelID)
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

func TestCatalog_DefaultCatalog_IncludesCodex(t *testing.T) {
	cat := libagent.DefaultCatalog()

	info, _, err := cat.FindModel(libagent.CodexProviderID, "gpt-5.3-codex")
	require.NoError(t, err)
	assert.Equal(t, libagent.CodexProviderID, info.ProviderID)
	assert.Equal(t, "gpt-5.3-codex", info.ModelID)
	assert.True(t, info.CanReason)
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
