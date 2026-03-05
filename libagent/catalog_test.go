package libagent_test

import (
	"testing"

	"github.com/francescoalemanno/raijin-mono/libagent"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"charm.land/catwalk/pkg/catwalk"
)

func TestCatalog_DefaultHasProviders(t *testing.T) {
	cat := libagent.DefaultCatalog()
	providers := cat.ListProviders()
	assert.NotEmpty(t, providers, "embedded catalog should have providers")
}

func TestCatalog_FindModel_KnownProvider(t *testing.T) {
	cat := libagent.DefaultCatalog()
	providers := cat.ListProviders()
	require.NotEmpty(t, providers)

	// Pick any provider that has at least one model.
	var found bool
	for _, p := range providers {
		if len(p.Models) == 0 {
			continue
		}
		m := p.Models[0]
		info, prov, err := cat.FindModel(string(p.ID), m.ID)
		require.NoError(t, err)
		assert.Equal(t, string(p.ID), info.ProviderID)
		assert.Equal(t, m.ID, info.ModelID)
		// Custom providers intentionally return a zero catwalk.Provider.
		if string(p.ID) == libagent.CodexProviderID ||
			string(p.ID) == libagent.SyntheticProviderID ||
			string(p.ID) == libagent.ZenProviderID {
			assert.Equal(t, catwalk.InferenceProvider(""), prov.ID)
		} else {
			assert.Equal(t, p.ID, prov.ID)
		}
		found = true
		break
	}
	assert.True(t, found, "should have found at least one model")
}

func TestCatalog_FindModel_UnknownProvider(t *testing.T) {
	cat := libagent.DefaultCatalog()
	_, _, err := cat.FindModel("nonexistent-provider", "some-model")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found in catalog")
}

func TestCatalog_FindModel_UnknownModel(t *testing.T) {
	cat := libagent.DefaultCatalog()
	providers := cat.ListProviders()
	require.NotEmpty(t, providers)

	// Find a provider with models.
	var providerID string
	for _, p := range providers {
		if len(p.Models) > 0 {
			providerID = string(p.ID)
			break
		}
	}
	require.NotEmpty(t, providerID)

	_, _, err := cat.FindModel(providerID, "nonexistent-model-xyz")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found in provider")
}

func TestCatalog_AddProvider_Override(t *testing.T) {
	cat := libagent.NewCatalog()

	custom := catwalk.Provider{
		ID:          "myprovider",
		Name:        "My Provider",
		Type:        catwalk.TypeOpenAICompat,
		APIEndpoint: "https://my.api/v1",
		Models: []catwalk.Model{
			{ID: "my-model", Name: "My Model", ContextWindow: 8192},
		},
	}
	cat.AddProvider(custom)

	info, prov, err := cat.FindModel("myprovider", "my-model")
	require.NoError(t, err)
	assert.Equal(t, "myprovider", info.ProviderID)
	assert.Equal(t, "my-model", info.ModelID)
	assert.Equal(t, int64(8192), info.ContextWindow)
	assert.Equal(t, "https://my.api/v1", prov.APIEndpoint)

	// Override with updated model list.
	updated := custom
	updated.Models = []catwalk.Model{
		{ID: "my-model-v2", Name: "My Model V2"},
	}
	cat.AddProvider(updated)

	_, _, err = cat.FindModel("myprovider", "my-model")
	assert.Error(t, err, "old model should no longer exist after override")

	_, _, err = cat.FindModel("myprovider", "my-model-v2")
	assert.NoError(t, err)
}

func TestCatalog_ModelInfo_Fields(t *testing.T) {
	cat := libagent.NewCatalog()
	cat.AddProvider(catwalk.Provider{
		ID:   "test",
		Name: "Test",
		Type: catwalk.TypeOpenAICompat,
		Models: []catwalk.Model{
			{
				ID:               "m1",
				Name:             "Model One",
				ContextWindow:    128000,
				DefaultMaxTokens: 4096,
				CanReason:        true,
				SupportsImages:   true,
				CostPer1MIn:      0.5,
				CostPer1MOut:     1.5,
			},
		},
	})

	info, _, err := cat.FindModel("test", "m1")
	require.NoError(t, err)
	assert.Equal(t, int64(128000), info.ContextWindow)
	assert.Equal(t, int64(4096), info.DefaultMaxTokens)
	assert.True(t, info.CanReason)
	assert.True(t, info.SupportsImages)
	assert.True(t, info.HasCapability(libagent.ModelCapabilityText))
	assert.True(t, info.HasCapability(libagent.ModelCapabilityImage))
	assert.Equal(t, 0.5, info.CostPer1MIn)
	assert.Equal(t, 1.5, info.CostPer1MOut)
}

func TestCatalog_DefaultCatalog_IncludesSyntheticCustomProvider(t *testing.T) {
	cat := libagent.DefaultCatalog()

	info, _, err := cat.FindModel(libagent.SyntheticProviderID, "hf:meta-llama/Llama-3.3-70B-Instruct")
	require.NoError(t, err)
	assert.Equal(t, libagent.SyntheticProviderID, info.ProviderID)
	assert.Equal(t, "hf:meta-llama/Llama-3.3-70B-Instruct", info.ModelID)
}

func TestCatalog_DefaultCatalog_IncludesZenCustomProvider(t *testing.T) {
	cat := libagent.DefaultCatalog()

	info, _, err := cat.FindModel(libagent.ZenProviderID, "claude-sonnet-4-5")
	require.NoError(t, err)
	assert.Equal(t, libagent.ZenProviderID, info.ProviderID)
	assert.Equal(t, "claude-sonnet-4-5", info.ModelID)
}

func TestCatalog_DefaultCatalog_IncludesZenGoCustomProvider(t *testing.T) {
	cat := libagent.DefaultCatalog()

	// Test GLM-5 via Go provider
	info, _, err := cat.FindModel(libagent.ZenGoProviderID, "glm-5")
	require.NoError(t, err)
	assert.Equal(t, libagent.ZenGoProviderID, info.ProviderID)
	assert.Equal(t, "glm-5", info.ModelID)

	// Test Kimi K2.5 via Go provider
	info, _, err = cat.FindModel(libagent.ZenGoProviderID, "kimi-k2.5")
	require.NoError(t, err)
	assert.Equal(t, libagent.ZenGoProviderID, info.ProviderID)
	assert.Equal(t, "kimi-k2.5", info.ModelID)

	// Test MiniMax M2.5 via Go provider
	info, _, err = cat.FindModel(libagent.ZenGoProviderID, "minimax-m2.5")
	require.NoError(t, err)
	assert.Equal(t, libagent.ZenGoProviderID, info.ProviderID)
	assert.Equal(t, "minimax-m2.5", info.ModelID)
}

func TestCatalog_FindModelOptions_CustomProviderType(t *testing.T) {
	cat := libagent.DefaultCatalog()

	providerType, opts := cat.FindModelOptions(libagent.ZenGoProviderID, "kimi-k2.5")
	assert.Equal(t, string(catwalk.TypeOpenAICompat), providerType)
	assert.Nil(t, opts)
}
