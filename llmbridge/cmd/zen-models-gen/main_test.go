package main

import "testing"

func TestBuildModelsUsesModelsDevMetadata(t *testing.T) {
	reasoning := true
	toolCall := true
	inCost := 0.4
	outCost := 2.5
	cacheRead := 0.1
	cacheWrite := 0.2
	context := int64(262144)
	maxOutput := int64(131072)

	models := buildModels(
		[]zenModel{{ID: "kimi-k2"}},
		map[string]modelsDevModel{
			"kimi-k2": {
				ID:        "kimi-k2",
				Name:      "Kimi K2",
				ToolCall:  &toolCall,
				Reasoning: &reasoning,
				Modalities: modelsDevModality{
					Input: []string{"text", "image"},
				},
				Cost: modelsDevCost{
					Input:      &inCost,
					Output:     &outCost,
					CacheRead:  &cacheRead,
					CacheWrite: &cacheWrite,
				},
				Limit: modelsDevLimit{
					Context: &context,
					Output:  &maxOutput,
				},
			},
		},
	)

	if len(models) != 1 {
		t.Fatalf("expected 1 model, got %d", len(models))
	}

	model := models[0]
	if model.Name != "Kimi K2" {
		t.Fatalf("expected name to come from models.dev, got %q", model.Name)
	}
	if !model.CanReason {
		t.Fatalf("expected CanReason=true from models.dev")
	}
	if !model.SupportsImages {
		t.Fatalf("expected SupportsImages=true from modalities")
	}
	if model.ContextWindow != context {
		t.Fatalf("expected context %d, got %d", context, model.ContextWindow)
	}
	if model.DefaultMaxTokens != maxOutput {
		t.Fatalf("expected max output %d, got %d", maxOutput, model.DefaultMaxTokens)
	}
	if model.CostPer1MIn != inCost || model.CostPer1MOut != outCost {
		t.Fatalf("expected cost input/output %.2f/%.2f, got %.2f/%.2f", inCost, outCost, model.CostPer1MIn, model.CostPer1MOut)
	}
	if model.CostPer1MInCached != cacheRead || model.CostPer1MOutCached != cacheWrite {
		t.Fatalf(
			"expected cache cost read/write %.2f/%.2f, got %.2f/%.2f",
			cacheRead,
			cacheWrite,
			model.CostPer1MInCached,
			model.CostPer1MOutCached,
		)
	}
}

func TestBuildModelsFiltersDeprecatedAndNoToolCall(t *testing.T) {
	toolCall := true
	noToolCall := false

	models := buildModels(
		[]zenModel{{ID: "keep-me"}, {ID: "deprecated-model"}, {ID: "no-tools"}},
		map[string]modelsDevModel{
			"keep-me": {
				ID:       "keep-me",
				ToolCall: &toolCall,
			},
			"deprecated-model": {
				ID:       "deprecated-model",
				ToolCall: &toolCall,
				Status:   "deprecated",
			},
			"no-tools": {
				ID:       "no-tools",
				ToolCall: &noToolCall,
			},
		},
	)

	if len(models) != 1 {
		t.Fatalf("expected 1 model after filtering, got %d", len(models))
	}
	if models[0].ID != "keep-me" {
		t.Fatalf("expected keep-me to remain, got %q", models[0].ID)
	}
}

func TestBuildModelsAppliesKnownContextOverrides(t *testing.T) {
	context := int64(1_000_000)
	toolCall := true

	models := buildModels(
		[]zenModel{{ID: "claude-sonnet-4-5"}, {ID: "claude-opus-4-6"}},
		map[string]modelsDevModel{
			"claude-sonnet-4-5": {
				ID:       "claude-sonnet-4-5",
				ToolCall: &toolCall,
				Limit: modelsDevLimit{
					Context: &context,
				},
			},
			"claude-opus-4-6": {
				ID:       "claude-opus-4-6",
				ToolCall: &toolCall,
				Limit: modelsDevLimit{
					Context: &context,
				},
			},
		},
	)

	if len(models) != 2 {
		t.Fatalf("expected 2 models, got %d", len(models))
	}
	for _, model := range models {
		if model.ContextWindow != 200000 {
			t.Fatalf("expected override context 200000 for %s, got %d", model.ID, model.ContextWindow)
		}
	}
}

func TestBuildModelsFallbackWhenMetadataMissing(t *testing.T) {
	models := buildModels([]zenModel{{ID: "bare-model"}}, map[string]modelsDevModel{})
	if len(models) != 1 {
		t.Fatalf("expected 1 model, got %d", len(models))
	}
	model := models[0]
	if model.Name != "bare-model" {
		t.Fatalf("expected fallback name bare-model, got %q", model.Name)
	}
	if model.ContextWindow != fallbackContextWindow {
		t.Fatalf("expected fallback context %d, got %d", fallbackContextWindow, model.ContextWindow)
	}
	if model.DefaultMaxTokens != fallbackDefaultMaxTokens {
		t.Fatalf("expected fallback max %d, got %d", fallbackDefaultMaxTokens, model.DefaultMaxTokens)
	}
}
