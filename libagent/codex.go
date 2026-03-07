package libagent

import (
	"context"
	"maps"
	"strings"

	"charm.land/catwalk/pkg/catwalk"
	"charm.land/catwalk/pkg/embedded"
	"charm.land/fantasy"
	"charm.land/fantasy/providers/openai"
)

// CodexProviderID is the stable identifier for the OpenAI Codex custom provider.
const CodexProviderID = "openai-codex"

// codexAPIEndpoint is the ChatGPT OAuth-backed Codex inference endpoint.
// It uses the OpenAI Responses API, not the standard Chat Completions API.
const codexAPIEndpoint = "https://chatgpt.com/backend-api/codex"

// CodexProvider returns a CustomProvider for OpenAI Codex (ChatGPT OAuth).
//
// Models are sourced from catwalk's embedded OpenAI provider: any model whose
// ID matches "gpt-*codex*" (case-insensitive) is included.  All Codex models
// are marked CanReason=true.
//
// Authentication uses the OpenAI Codex OAuth flow from the oauth package.
// Register it with the Catalog via AddCustomProvider and call
// SetLoginCallbacks so that NewModel can trigger authentication automatically
// when no credentials are stored.
func CodexProvider() CustomProvider {
	return CustomProvider{
		ID:          CodexProviderID,
		Name:        "OpenAI Codex (ChatGPT OAuth)",
		Type:        catwalk.TypeOpenAI,
		APIEndpoint: codexAPIEndpoint,
		Models:      codexModels(),
		Build:       buildCodexProvider,
	}
}

func buildCodexProvider(apiKey string) (fantasy.Provider, error) {
	inner, err := openai.New(
		openai.WithAPIKey(apiKey),
		openai.WithBaseURL(codexAPIEndpoint),
		openai.WithUseResponsesAPI(),
		openai.WithHTTPClient(http1OnlyClient),
	)
	if err != nil {
		return nil, err
	}
	return &codexProvider{inner: inner}, nil
}

// codexProvider wraps the openai fantasy.Provider and returns codexLanguageModel
// instances that inject Codex-specific call options automatically.
type codexProvider struct{ inner fantasy.Provider }

func (p *codexProvider) Name() string { return p.inner.Name() }

func (p *codexProvider) LanguageModel(ctx context.Context, modelID string) (fantasy.LanguageModel, error) {
	m, err := p.inner.LanguageModel(ctx, modelID)
	if err != nil {
		return nil, err
	}
	return &codexLanguageModel{inner: m}, nil
}

// codexLanguageModel wraps a fantasy.LanguageModel and injects the
// ResponsesProviderOptions required by the Codex backend on every call:
//
//   - Instructions: system prompt extracted from the call (Codex prefers it
//     over the developer-role message that the fantasy library otherwise sends)
//   - ParallelToolCalls: true
//   - Include: reasoning.encrypted_content (needed for multi-turn reasoning)
//   - ReasoningEffort: medium / ReasoningSummary: auto
//   - MaxOutputTokens is stripped (the Codex backend rejects it)
type codexLanguageModel struct{ inner fantasy.LanguageModel }

func (m *codexLanguageModel) Provider() string { return m.inner.Provider() }
func (m *codexLanguageModel) Model() string    { return m.inner.Model() }

func (m *codexLanguageModel) Stream(ctx context.Context, call fantasy.Call) (fantasy.StreamResponse, error) {
	return m.inner.Stream(ctx, codexInjectOptions(call))
}

func (m *codexLanguageModel) Generate(ctx context.Context, call fantasy.Call) (*fantasy.Response, error) {
	return m.inner.Generate(ctx, codexInjectOptions(call))
}

func (m *codexLanguageModel) GenerateObject(ctx context.Context, call fantasy.ObjectCall) (*fantasy.ObjectResponse, error) {
	return m.inner.GenerateObject(ctx, call)
}

func (m *codexLanguageModel) StreamObject(ctx context.Context, call fantasy.ObjectCall) (fantasy.ObjectStreamResponse, error) {
	return m.inner.StreamObject(ctx, call)
}

// codexInjectOptions applies Codex-specific call options, mirroring what
// llmbridge's providerStreamOptions does for the openai-codex provider.
func codexInjectOptions(call fantasy.Call) fantasy.Call {
	// Codex backend rejects max_output_tokens.
	call.MaxOutputTokens = nil

	// Extract system prompt from the call so it can be sent as Instructions.
	instructions := ""
	for _, msg := range call.Prompt {
		if msg.Role == fantasy.MessageRoleSystem {
			for _, part := range msg.Content {
				if tp, ok := fantasy.AsMessagePart[fantasy.TextPart](part); ok {
					instructions = tp.Text
					break
				}
			}
			break
		}
	}
	if instructions == "" {
		instructions = "You are a helpful coding assistant."
	}

	parallelToolCalls := true
	effort := openai.ReasoningEffortMedium
	summary := "auto"

	opts := openai.NewResponsesProviderOptions(&openai.ResponsesProviderOptions{
		Instructions:      &instructions,
		ParallelToolCalls: &parallelToolCalls,
		Include:           []openai.IncludeType{openai.IncludeReasoningEncryptedContent},
		ReasoningEffort:   &effort,
		ReasoningSummary:  &summary,
	})

	// Merge with any caller-supplied options; caller values take precedence.
	merged := make(fantasy.ProviderOptions)
	maps.Copy(merged, opts)
	maps.Copy(merged, call.ProviderOptions)
	call.ProviderOptions = merged
	return call
}

// codexModels collects all gpt-*codex* models from the embedded catwalk catalog
// and normalises them for the Codex endpoint.
func codexModels() []ModelInfo {
	seen := map[string]bool{}
	var out []ModelInfo

	for _, p := range embedded.GetAll() {
		for _, m := range p.Models {
			id := strings.ToLower(m.ID)
			if !strings.HasPrefix(id, "gpt-") || !strings.Contains(id, "codex") {
				continue
			}
			if seen[m.ID] {
				continue
			}
			seen[m.ID] = true
			out = append(out, ModelInfo{
				ProviderID:       CodexProviderID,
				ModelID:          m.ID,
				Name:             m.Name,
				ContextWindow:    m.ContextWindow,
				DefaultMaxTokens: m.DefaultMaxTokens,
				CanReason:        true, // all Codex models support reasoning
				SupportsImages:   m.SupportsImages,
				CostPer1MIn:      m.CostPer1MIn,
				CostPer1MOut:     m.CostPer1MOut,
			})
		}
	}

	return out
}
