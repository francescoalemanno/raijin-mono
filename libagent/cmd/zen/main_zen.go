package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"go/format"
	"io"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"
)

const (
	defaultZenEndpoint         = "https://opencode.ai/zen/v1/models"
	defaultModelsDevEndpoint   = "https://models.dev/api.json"
	defaultOutput              = "zen_models_generated.go"
	fallbackContextWindow      = int64(200000)
	fallbackDefaultMaxTokens   = int64(16384)
	defaultFetchTimeoutSeconds = 15

	// Provider IDs used in generated code
	zenProviderID   = "ZenProviderID"
	zenGoProviderID = "ZenGoProviderID"
)

type zenModelsResponse struct {
	Data []zenModel `json:"data"`
}

type zenModel struct {
	ID                     string   `json:"id"`
	Name                   string   `json:"name"`
	ContextWindow          int64    `json:"context_window"`
	DefaultMaxTokens       int64    `json:"default_max_tokens"`
	MaxOutputTokens        int64    `json:"max_output_tokens"`
	CanReason              bool     `json:"can_reason"`
	SupportsImages         bool     `json:"supports_images"`
	CostPer1MIn            float64  `json:"cost_per_1m_in"`
	CostPer1MOut           float64  `json:"cost_per_1m_out"`
	CostPer1MInCached      float64  `json:"cost_per_1m_in_cached"`
	CostPer1MOutCached     float64  `json:"cost_per_1m_out_cached"`
	ReasoningLevels        []string `json:"reasoning_levels"`
	DefaultReasoningEffort string   `json:"default_reasoning_effort"`
}

type modelsDevResponse struct {
	OpenCode   modelsDevProviderInfo `json:"opencode"`
	OpenCodeGo modelsDevProviderInfo `json:"opencode-go"`
}

type modelsDevProviderInfo struct {
	ID     string                    `json:"id"`
	Name   string                    `json:"name"`
	API    string                    `json:"api"`
	NPM    string                    `json:"npm"`
	Models map[string]modelsDevModel `json:"models"`
}

type modelsDevModel struct {
	ID         string               `json:"id"`
	Name       string               `json:"name"`
	Status     string               `json:"status"`
	ToolCall   *bool                `json:"tool_call"`
	Reasoning  *bool                `json:"reasoning"`
	Modalities modelsDevModality    `json:"modalities"`
	Cost       modelsDevCost        `json:"cost"`
	Limit      modelsDevLimit       `json:"limit"`
	Provider   modelsDevProviderRef `json:"provider"`
}

type modelsDevProviderRef struct {
	NPM string `json:"npm"`
}

type modelsDevModality struct {
	Input []string `json:"input"`
}

type modelsDevCost struct {
	Input      *float64 `json:"input"`
	Output     *float64 `json:"output"`
	CacheRead  *float64 `json:"cache_read"`
	CacheWrite *float64 `json:"cache_write"`
}

type modelsDevLimit struct {
	Context *int64 `json:"context"`
	Output  *int64 `json:"output"`
}

func main() {
	if err := run(); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "zen-models-gen: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	zenEndpoint := flag.String("endpoint", defaultZenEndpoint, "OpenCode Zen models endpoint")
	metadataEndpoint := flag.String("metadata-endpoint", defaultModelsDevEndpoint, "models.dev catalog endpoint")
	output := flag.String("output", defaultOutput, "generated Go file path")
	timeout := flag.Int("timeout-seconds", defaultFetchTimeoutSeconds, "HTTP fetch timeout in seconds")
	flag.Parse()

	client := &http.Client{Timeout: time.Duration(*timeout) * time.Second}

	zenModels, err := fetchZenModels(client, *zenEndpoint)
	if err != nil {
		return err
	}

	modelsDev, err := fetchModelsDevData(client, *metadataEndpoint)
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "zen-models-gen: warning: models.dev metadata unavailable: %v\n", err)
		modelsDev = &modelsDevResponse{}
	}

	// Build Zen models from API + metadata
	zenMetadata := map[string]modelsDevModel{}
	if modelsDev.OpenCode.Models != nil {
		zenMetadata = normalizeModelsDevMap(modelsDev.OpenCode.Models)
	}
	models := buildModels(zenModels, zenMetadata)
	if len(models) == 0 {
		return fmt.Errorf("no models available after normalization")
	}

	// Build Go models from models.dev opencode-go provider
	goModels, goAnthropicModels := buildGoModelsFromModelsDev(modelsDev.OpenCodeGo)

	formatted, err := render(models, goModels, goAnthropicModels)
	if err != nil {
		return err
	}

	if err := os.WriteFile(*output, formatted, 0o644); err != nil {
		return fmt.Errorf("write generated file: %w", err)
	}
	return nil
}

func fetchZenModels(client *http.Client, endpoint string) ([]zenModel, error) {
	var parsed zenModelsResponse
	if err := fetchJSON(client, endpoint, &parsed); err != nil {
		return nil, fmt.Errorf("fetch zen models: %w", err)
	}
	return parsed.Data, nil
}

func fetchModelsDevData(client *http.Client, endpoint string) (*modelsDevResponse, error) {
	var parsed modelsDevResponse
	if err := fetchJSON(client, endpoint, &parsed); err != nil {
		return nil, fmt.Errorf("fetch models.dev catalog: %w", err)
	}
	return &parsed, nil
}

func normalizeModelsDevMap(models map[string]modelsDevModel) map[string]modelsDevModel {
	byID := make(map[string]modelsDevModel, len(models))
	for key, model := range models {
		modelID := strings.TrimSpace(model.ID)
		if modelID == "" {
			modelID = strings.TrimSpace(key)
		}
		if modelID == "" {
			continue
		}
		model.ID = modelID
		byID[modelID] = model
	}
	return byID
}

func fetchJSON(client *http.Client, endpoint string, target any) error {
	req, err := http.NewRequest(http.MethodGet, endpoint, nil)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("request %s: %w", endpoint, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("%s returned %s: %s", endpoint, resp.Status, strings.TrimSpace(string(body)))
	}

	if err := json.NewDecoder(resp.Body).Decode(target); err != nil {
		return fmt.Errorf("decode response from %s: %w", endpoint, err)
	}
	return nil
}

func buildModels(zenModels []zenModel, modelsDev map[string]modelsDevModel) []zenModel {
	byID := make(map[string]zenModel, len(zenModels))
	for _, item := range zenModels {
		normalized, ok := normalizeModel(item)
		if !ok {
			continue
		}
		byID[normalized.ID] = normalized
	}

	models := make([]zenModel, 0, len(byID))
	for _, model := range byID {
		if metadata, ok := modelsDev[model.ID]; ok {
			merged, keep := applyModelsDevMetadata(model, metadata)
			if !keep {
				continue
			}
			model = merged
		}

		model = applyKnownOverrides(model)
		normalized, ok := normalizeModel(model)
		if !ok {
			continue
		}
		models = append(models, normalized)
	}

	sort.Slice(models, func(i, j int) bool {
		return models[i].ID < models[j].ID
	})
	return models
}

// buildGoModelsFromModelsDev creates zenModel structs from models.dev opencode-go provider data.
// Returns the models and a list of model IDs that use Anthropic API format.
func buildGoModelsFromModelsDev(provider modelsDevProviderInfo) ([]zenModel, []string) {
	if provider.Models == nil {
		return nil, nil
	}

	var models []zenModel
	var anthropicModels []string

	for _, md := range provider.Models {
		model := zenModel{
			ID:   strings.TrimSpace(md.ID),
			Name: strings.TrimSpace(md.Name),
		}

		if model.ID == "" {
			continue
		}
		if model.Name == "" {
			model.Name = model.ID
		}

		// Skip deprecated models
		if strings.EqualFold(strings.TrimSpace(md.Status), "deprecated") {
			continue
		}

		// Check if this model uses Anthropic API format
		if isAnthropicNPM(md.Provider.NPM) {
			anthropicModels = append(anthropicModels, model.ID)
		}

		// Apply limits
		if md.Limit.Context != nil && *md.Limit.Context > 0 {
			model.ContextWindow = *md.Limit.Context
		}
		if md.Limit.Output != nil && *md.Limit.Output > 0 {
			model.DefaultMaxTokens = *md.Limit.Output
		}

		// Apply costs
		if md.Cost.Input != nil {
			model.CostPer1MIn = *md.Cost.Input
		}
		if md.Cost.Output != nil {
			model.CostPer1MOut = *md.Cost.Output
		}
		if md.Cost.CacheRead != nil {
			model.CostPer1MInCached = *md.Cost.CacheRead
		}
		if md.Cost.CacheWrite != nil {
			model.CostPer1MOutCached = *md.Cost.CacheWrite
		}

		// Apply capabilities
		if md.Reasoning != nil {
			model.CanReason = *md.Reasoning
		}
		if len(md.Modalities.Input) > 0 {
			model.SupportsImages = hasImageModality(md.Modalities.Input)
		}

		// Apply defaults if missing
		model, ok := normalizeModel(model)
		if !ok {
			continue
		}

		models = append(models, model)
	}

	sort.Slice(models, func(i, j int) bool {
		return models[i].ID < models[j].ID
	})

	return models, anthropicModels
}

func applyModelsDevMetadata(model zenModel, metadata modelsDevModel) (zenModel, bool) {
	if strings.EqualFold(strings.TrimSpace(metadata.Status), "deprecated") {
		return zenModel{}, false
	}
	if metadata.ToolCall != nil && !*metadata.ToolCall {
		return zenModel{}, false
	}

	if name := strings.TrimSpace(metadata.Name); name != "" {
		model.Name = name
	}
	if metadata.Reasoning != nil {
		model.CanReason = *metadata.Reasoning
	}
	if len(metadata.Modalities.Input) > 0 {
		model.SupportsImages = hasImageModality(metadata.Modalities.Input)
	}
	if metadata.Cost.Input != nil {
		model.CostPer1MIn = *metadata.Cost.Input
	}
	if metadata.Cost.Output != nil {
		model.CostPer1MOut = *metadata.Cost.Output
	}
	if metadata.Cost.CacheRead != nil {
		model.CostPer1MInCached = *metadata.Cost.CacheRead
	}
	if metadata.Cost.CacheWrite != nil {
		model.CostPer1MOutCached = *metadata.Cost.CacheWrite
	}
	if metadata.Limit.Context != nil && *metadata.Limit.Context > 0 {
		model.ContextWindow = *metadata.Limit.Context
	}
	if metadata.Limit.Output != nil && *metadata.Limit.Output > 0 {
		model.DefaultMaxTokens = *metadata.Limit.Output
	}
	return model, true
}

func applyKnownOverrides(model zenModel) zenModel {
	switch model.ID {
	case "claude-opus-4-6":
		model.ContextWindow = 200000
	case "claude-sonnet-4", "claude-sonnet-4-5":
		model.ContextWindow = 200000
	}
	return model
}

func hasImageModality(inputs []string) bool {
	for _, input := range inputs {
		if strings.EqualFold(strings.TrimSpace(input), "image") {
			return true
		}
	}
	return false
}

// isAnthropicNPM reports whether the npm package indicates Anthropic API format.
func isAnthropicNPM(npm string) bool {
	return strings.Contains(strings.ToLower(npm), "anthropic")
}

func normalizeModel(model zenModel) (zenModel, bool) {
	model.ID = strings.TrimSpace(model.ID)
	if model.ID == "" {
		return zenModel{}, false
	}

	model.Name = strings.TrimSpace(model.Name)
	if model.Name == "" {
		model.Name = model.ID
	}

	if model.ContextWindow <= 0 {
		model.ContextWindow = fallbackContextWindow
	}

	if model.DefaultMaxTokens <= 0 {
		if model.MaxOutputTokens > 0 {
			model.DefaultMaxTokens = model.MaxOutputTokens
		} else {
			model.DefaultMaxTokens = fallbackDefaultMaxTokens
		}
	}

	if len(model.ReasoningLevels) > 0 {
		levels := make([]string, 0, len(model.ReasoningLevels))
		for _, level := range model.ReasoningLevels {
			level = strings.TrimSpace(level)
			if level != "" {
				levels = append(levels, level)
			}
		}
		model.ReasoningLevels = levels
	}

	model.DefaultReasoningEffort = strings.TrimSpace(model.DefaultReasoningEffort)
	return model, true
}

func render(zenModels []zenModel, goModels []zenModel, goAnthropicModels []string) ([]byte, error) {
	var out bytes.Buffer
	out.WriteString("// Code generated by go generate; DO NOT EDIT.\n\n")
	out.WriteString("package libagent\n\n")

	// Write all Zen models
	out.WriteString("var zenGeneratedModels = []ModelInfo{\n")
	for _, model := range zenModels {
		writeModelLiteral(&out, model, zenProviderID)
	}
	out.WriteString("}\n\n")

	// Write Go models
	out.WriteString("var zenGoGeneratedModels = []ModelInfo{\n")
	for _, model := range goModels {
		writeModelLiteral(&out, model, zenGoProviderID)
	}
	out.WriteString("}\n\n")

	// Write Anthropic models map for Go provider
	out.WriteString("// zenGoAnthropicModels lists Go models that use Anthropic API format.\n")
	out.WriteString("// All other Go models use OpenAI-compatible format.\n")
	out.WriteString("var zenGoAnthropicModels = map[string]bool{\n")
	for _, modelID := range goAnthropicModels {
		_, _ = fmt.Fprintf(&out, "\t%q: true,\n", modelID)
	}
	out.WriteString("}\n")

	formatted, err := format.Source(out.Bytes())
	if err != nil {
		return nil, fmt.Errorf("format generated source: %w", err)
	}
	return formatted, nil
}

func writeModelLiteral(out *bytes.Buffer, model zenModel, providerID string) {
	_, _ = fmt.Fprintf(out, "\t{\n")
	_, _ = fmt.Fprintf(out, "\t\tProviderID:       %s,\n", providerID)
	_, _ = fmt.Fprintf(out, "\t\tModelID:          %q,\n", model.ID)
	_, _ = fmt.Fprintf(out, "\t\tName:             %q,\n", model.Name)
	_, _ = fmt.Fprintf(out, "\t\tContextWindow:    %d,\n", model.ContextWindow)
	_, _ = fmt.Fprintf(out, "\t\tDefaultMaxTokens: %d,\n", model.DefaultMaxTokens)
	if model.CanReason {
		_, _ = fmt.Fprintf(out, "\t\tCanReason:        true,\n")
	}
	if model.SupportsImages {
		_, _ = fmt.Fprintf(out, "\t\tSupportsImages:   true,\n")
	}
	if model.CostPer1MIn != 0 {
		_, _ = fmt.Fprintf(out, "\t\tCostPer1MIn:      %s,\n", formatFloat(model.CostPer1MIn))
	}
	if model.CostPer1MOut != 0 {
		_, _ = fmt.Fprintf(out, "\t\tCostPer1MOut:     %s,\n", formatFloat(model.CostPer1MOut))
	}
	if model.CostPer1MInCached != 0 {
		_, _ = fmt.Fprintf(out, "\t\tCostPer1MInCached:  %s,\n", formatFloat(model.CostPer1MInCached))
	}
	if model.CostPer1MOutCached != 0 {
		_, _ = fmt.Fprintf(out, "\t\tCostPer1MOutCached: %s,\n", formatFloat(model.CostPer1MOutCached))
	}
	_, _ = fmt.Fprintf(out, "\t},\n")
}

func formatFloat(value float64) string {
	return strings.TrimRight(strings.TrimRight(fmt.Sprintf("%.6f", value), "0"), ".")
}
