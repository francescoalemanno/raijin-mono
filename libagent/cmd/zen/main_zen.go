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
	OpenCode modelsDevProvider `json:"opencode"`
}

type modelsDevProvider struct {
	Models map[string]modelsDevModel `json:"models"`
}

type modelsDevModel struct {
	ID         string            `json:"id"`
	Name       string            `json:"name"`
	Status     string            `json:"status"`
	ToolCall   *bool             `json:"tool_call"`
	Reasoning  *bool             `json:"reasoning"`
	Modalities modelsDevModality `json:"modalities"`
	Cost       modelsDevCost     `json:"cost"`
	Limit      modelsDevLimit    `json:"limit"`
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

	metadata, err := fetchModelsDevModels(client, *metadataEndpoint)
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "zen-models-gen: warning: models.dev metadata unavailable: %v\n", err)
		metadata = map[string]modelsDevModel{}
	}

	models := buildModels(zenModels, metadata)
	if len(models) == 0 {
		return fmt.Errorf("no models available after normalization")
	}

	formatted, err := render(models)
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

func fetchModelsDevModels(client *http.Client, endpoint string) (map[string]modelsDevModel, error) {
	var parsed modelsDevResponse
	if err := fetchJSON(client, endpoint, &parsed); err != nil {
		return nil, fmt.Errorf("fetch models.dev catalog: %w", err)
	}

	byID := make(map[string]modelsDevModel, len(parsed.OpenCode.Models))
	for key, model := range parsed.OpenCode.Models {
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
	return byID, nil
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

func render(models []zenModel) ([]byte, error) {
	var out bytes.Buffer
	out.WriteString("// Code generated by go generate; DO NOT EDIT.\n\n")
	out.WriteString("package libagent\n\n")
	out.WriteString("var zenGeneratedModels = []ModelInfo{\n")
	for _, model := range models {
		writeModelLiteral(&out, model)
	}
	out.WriteString("}\n")

	formatted, err := format.Source(out.Bytes())
	if err != nil {
		return nil, fmt.Errorf("format generated source: %w", err)
	}
	return formatted, nil
}

func writeModelLiteral(out *bytes.Buffer, model zenModel) {
	_, _ = fmt.Fprintf(out, "\t{\n")
	_, _ = fmt.Fprintf(out, "\t\tProviderID:       ZenProviderID,\n")
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
	_, _ = fmt.Fprintf(out, "\t},\n")
}

func formatFloat(value float64) string {
	return strings.TrimRight(strings.TrimRight(fmt.Sprintf("%.6f", value), "0"), ".")
}
