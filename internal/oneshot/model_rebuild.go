package oneshot

import (
	"context"
	"fmt"
	"os"
	"strings"

	libagent "github.com/francescoalemanno/raijin-mono/libagent"
)

// RebuildRuntimeModel creates a new RuntimeModel from a ModelConfig using the catalog.
func RebuildRuntimeModel(cfg libagent.ModelConfig) (libagent.RuntimeModel, error) {
	cfg = cfg.Normalize()
	cat := libagent.DefaultCatalog()
	apiKey := cfg.APIKey
	if after, ok := strings.CutPrefix(apiKey, "$"); ok {
		apiKey = os.Getenv(after)
	}
	model, err := cat.NewModel(context.Background(), cfg.Provider, cfg.Model, apiKey)
	if err != nil {
		return libagent.RuntimeModel{}, fmt.Errorf("building model %s/%s: %w", cfg.Provider, cfg.Model, err)
	}
	info, _, _ := cat.FindModel(cfg.Provider, cfg.Model)
	providerType, catalogOpts := cat.FindModelOptions(cfg.Provider, cfg.Model)
	return libagent.RuntimeModel{
		Model:                  model,
		ModelInfo:              info,
		ModelCfg:               cfg,
		ProviderType:           providerType,
		CatalogProviderOptions: catalogOpts,
	}, nil
}
