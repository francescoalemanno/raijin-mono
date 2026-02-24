package llm

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"os"
	"strings"
	"time"

	"charm.land/fantasy"
	"charm.land/fantasy/providers/anthropic"
	"charm.land/fantasy/providers/azure"
	"charm.land/fantasy/providers/bedrock"
	"charm.land/fantasy/providers/google"
	"charm.land/fantasy/providers/openai"
	"charm.land/fantasy/providers/openaicompat"
	"charm.land/fantasy/providers/openrouter"
	"charm.land/fantasy/providers/vercel"
)

type defaultFactory struct{}

const anthropicThinkingBeta = "interleaved-thinking-2025-05-14"

type runtime struct {
	model         fantasy.LanguageModel
	providerType  ProviderType
	providerID    string
	modelID       string
	thinkingLevel ThinkingLevel
	// Stored for potential token refresh
	apiKey          string
	baseURL         string
	extraHeaders    map[string]string
	providerOptions map[string]any
	// Callback to refresh OAuth token before API calls
	tokenRefresh TokenRefreshCallback
}

// NewDefaultFactory returns the default fantasy-backed RuntimeFactory.
func NewDefaultFactory() RuntimeFactory {
	return defaultFactory{}
}

// ProviderID returns the provider ID for this runtime.
func (r *runtime) ProviderID() string {
	return r.providerID
}

// RefreshAPIKey updates the stored API key and recreates the language model.
// This is used for OAuth token refresh scenarios (e.g., openai-codex).
func (r *runtime) RefreshAPIKey(ctx context.Context, newAPIKey string) error {
	r.apiKey = newAPIKey
	p, err := buildProvider(r.providerType, r.baseURL, r.apiKey, r.extraHeaders, r.providerOptions)
	if err != nil {
		return fmt.Errorf("rebuild provider with refreshed token: %w", err)
	}
	languageModel, err := p.LanguageModel(ctx, r.modelID)
	if err != nil {
		return fmt.Errorf("recreate language model with refreshed token: %w", err)
	}
	r.model = languageModel
	return nil
}

func (defaultFactory) NewRuntime(ctx context.Context, provider ProviderConfig, model ModelSelection, resolver SecretResolver) (Runtime, ModelMetadata, error) {
	headers := maps.Clone(provider.ExtraHeaders)
	if headers == nil {
		headers = make(map[string]string)
	}

	thinkingLevel := NormalizeThinkingLevel(model.ThinkingLevel)

	if provider.Type == ProviderAnthropic {
		applyAnthropicThinkingHeader(headers, thinkingLevel.Enabled())
	}

	apiKey := provider.APIKey
	baseURL := provider.BaseURL
	if resolver != nil {
		if v, err := resolver.Resolve(apiKey); err == nil {
			apiKey = v
		}
		if v, err := resolver.Resolve(baseURL); err == nil {
			baseURL = v
		}
	}

	providerOptions := normalizeProviderOptions(provider.ID, provider.ProviderOptions)
	p, err := buildProvider(provider.Type, baseURL, apiKey, headers, providerOptions)
	if err != nil {
		return nil, ModelMetadata{}, err
	}

	languageModel, err := p.LanguageModel(ctx, model.ModelID)
	if err != nil {
		return nil, ModelMetadata{}, err
	}

	meta := ModelMetadata{
		MaxOutput: model.MaxOutputTokens,
	}
	rt := &runtime{
		model:           languageModel,
		providerType:    provider.Type,
		providerID:      provider.ID,
		modelID:         model.ModelID,
		thinkingLevel:   thinkingLevel,
		apiKey:          apiKey,
		baseURL:         baseURL,
		extraHeaders:    maps.Clone(headers),
		providerOptions: maps.Clone(providerOptions),
	}

	// Set up token refresh callback for openai-codex provider.
	if provider.ID == openAICodexProviderID {
		rt.tokenRefresh = defaultOpenAICodexTokenRefresh
	}

	return rt, meta, nil
}

func applyAnthropicThinkingHeader(headers map[string]string, enabled bool) {
	const header = "anthropic-beta"
	current := strings.TrimSpace(headers[header])
	parts := strings.Split(current, ",")
	out := make([]string, 0, len(parts)+1)
	for _, part := range parts {
		value := strings.TrimSpace(part)
		if value == "" || strings.EqualFold(value, anthropicThinkingBeta) {
			continue
		}
		out = append(out, value)
	}

	if enabled {
		out = append(out, anthropicThinkingBeta)
	}

	if len(out) == 0 {
		delete(headers, header)
		return
	}
	headers[header] = strings.Join(out, ",")
}

func buildProvider(providerType ProviderType, baseURL, apiKey string, headers map[string]string, options map[string]any) (fantasy.Provider, error) {
	switch providerType {
	case ProviderOpenAI:
		opts := []openai.Option{openai.WithAPIKey(apiKey)}
		if baseURL != "" {
			opts = append(opts, openai.WithBaseURL(baseURL))
		}
		if useResponses, ok := optionBool(options, "useResponsesAPI"); ok && useResponses {
			opts = append(opts, openai.WithUseResponsesAPI())
		}
		if len(headers) > 0 {
			opts = append(opts, openai.WithHeaders(headers))
		}
		return openai.New(opts...)
	case ProviderAnthropic:
		opts := []anthropic.Option{anthropic.WithAPIKey(apiKey)}
		if baseURL != "" {
			opts = append(opts, anthropic.WithBaseURL(baseURL))
		}
		if len(headers) > 0 {
			opts = append(opts, anthropic.WithHeaders(headers))
		}
		return anthropic.New(opts...)
	case ProviderOpenRouter:
		opts := []openrouter.Option{openrouter.WithAPIKey(apiKey)}
		if len(headers) > 0 {
			opts = append(opts, openrouter.WithHeaders(headers))
		}
		return openrouter.New(opts...)
	case ProviderGoogle:
		opts := []google.Option{google.WithGeminiAPIKey(apiKey)}
		if baseURL != "" {
			opts = append(opts, google.WithBaseURL(baseURL))
		}
		if len(headers) > 0 {
			opts = append(opts, google.WithHeaders(headers))
		}
		return google.New(opts...)
	case ProviderOpenAICompat:
		opts := []openaicompat.Option{openaicompat.WithAPIKey(apiKey)}
		if baseURL != "" {
			opts = append(opts, openaicompat.WithBaseURL(baseURL))
		}
		if len(headers) > 0 {
			opts = append(opts, openaicompat.WithHeaders(headers))
		}
		return openaicompat.New(opts...)
	case ProviderAzure:
		opts := []azure.Option{
			azure.WithBaseURL(baseURL),
			azure.WithAPIKey(apiKey),
			azure.WithUseResponsesAPI(),
		}
		if apiVersion, ok := optionString(options, "apiVersion"); ok {
			opts = append(opts, azure.WithAPIVersion(apiVersion))
		}
		if len(headers) > 0 {
			opts = append(opts, azure.WithHeaders(headers))
		}
		return azure.New(opts...)
	case ProviderBedrock:
		var opts []bedrock.Option
		if len(headers) > 0 {
			opts = append(opts, bedrock.WithHeaders(headers))
		}
		bearerToken := os.Getenv("AWS_BEARER_TOKEN_BEDROCK")
		if bearerToken != "" {
			opts = append(opts, bedrock.WithAPIKey(bearerToken))
		}
		return bedrock.New(opts...)
	case ProviderVercel:
		opts := []vercel.Option{vercel.WithAPIKey(apiKey)}
		if len(headers) > 0 {
			opts = append(opts, vercel.WithHeaders(headers))
		}
		return vercel.New(opts...)
	case ProviderVertexAI:
		var opts []google.Option
		if len(headers) > 0 {
			opts = append(opts, google.WithHeaders(headers))
		}
		project, _ := optionString(options, "project")
		location, _ := optionString(options, "location")
		opts = append(opts, google.WithVertex(project, location))
		return google.New(opts...)
	default:
		return nil, fmt.Errorf("provider type not supported: %q", providerType)
	}
}

func optionString(options map[string]any, key string) (string, bool) {
	if options == nil {
		return "", false
	}
	v, ok := options[key]
	if !ok {
		return "", false
	}
	s, ok := v.(string)
	return s, ok
}

func optionBool(options map[string]any, key string) (bool, bool) {
	if options == nil {
		return false, false
	}
	v, ok := options[key]
	if !ok {
		return false, false
	}
	b, ok := v.(bool)
	return b, ok
}

// defaultOpenAICodexTokenRefresh is the default token refresh callback for openai-codex.
// It can be overridden for testing.
var defaultOpenAICodexTokenRefresh TokenRefreshCallback

func init() {
	defaultOpenAICodexTokenRefresh = func(providerID string) (string, error) {
		// This will be set by the config package to avoid circular imports.
		return "", nil
	}
}

// RegisterOpenAICodexTokenRefresh sets the token refresh callback for openai-codex.
// This is called by the config package during initialization.
func RegisterOpenAICodexTokenRefresh(callback TokenRefreshCallback) {
	defaultOpenAICodexTokenRefresh = callback
}

func tokenLikelyExpired(token string, now time.Time) bool {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return false
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return false
	}
	var claims map[string]any
	if err := json.Unmarshal(payload, &claims); err != nil {
		return false
	}
	expRaw, ok := claims["exp"]
	if !ok {
		return false
	}
	var exp int64
	switch v := expRaw.(type) {
	case float64:
		exp = int64(v)
	case int64:
		exp = v
	case json.Number:
		parsed, err := v.Int64()
		if err != nil {
			return false
		}
		exp = parsed
	default:
		return false
	}
	return now.Unix() >= exp
}

func (r *runtime) Stream(ctx context.Context, req StreamRequest) (*RunResult, error) {
	// Check and refresh OAuth token if needed.
	if r.tokenRefresh != nil {
		newToken, err := r.tokenRefresh(r.providerID)
		if err != nil {
			if tokenLikelyExpired(r.apiKey, time.Now()) {
				return nil, fmt.Errorf("refresh token before stream: %w", err)
			}
			slog.Debug("token refresh failed, proceeding with current token", "provider", r.providerID, "err", err)
		} else if newToken != "" && newToken != r.apiKey {
			if refreshErr := r.RefreshAPIKey(ctx, newToken); refreshErr != nil {
				return nil, fmt.Errorf("refresh token before stream: %w", refreshErr)
			}
		}
	}

	mediaStrategy := newProviderMediaStrategy(r.providerType)
	messages, err := toFantasyMessages(mediaStrategy.AdaptMessages(req.Messages))
	if err != nil {
		return nil, err
	}
	tools := toFantasyTools(req.Tools)
	files := toFantasyFiles(req.Files)

	agent := fantasy.NewAgent(
		r.model,
		fantasy.WithSystemPrompt(req.SystemPrompt),
		fantasy.WithTools(tools...),
	)

	streamCall := fantasy.AgentStreamCall{
		Prompt:          req.Prompt,
		Files:           files,
		Messages:        messages,
		MaxOutputTokens: providerMaxOutputTokens(r.providerID, req.MaxOutputTokens),
		TopP:            req.TopP,
		Temperature:     req.Temperature,
		TopK:            req.TopK,
		StopWhen:        toFantasyStopConditions(req.StopWhen),
		ProviderOptions: providerStreamOptions(r.providerType, r.providerID, r.modelID, r.thinkingLevel, req.SystemPrompt),
	}

	if req.Callbacks.PrepareStep != nil {
		streamCall.PrepareStep = func(callCtx context.Context, opts fantasy.PrepareStepFunctionOptions) (context.Context, fantasy.PrepareStepResult, error) {
			converted := fromFantasyMessages(opts.Messages)
			nextMessages, err := req.Callbacks.PrepareStep(callCtx, converted)
			if err != nil {
				return callCtx, fantasy.PrepareStepResult{}, err
			}
			messages, err := toFantasyMessages(mediaStrategy.AdaptMessages(nextMessages))
			if err != nil {
				return callCtx, fantasy.PrepareStepResult{}, err
			}
			return callCtx, fantasy.PrepareStepResult{Messages: messages}, nil
		}
	}
	if req.Callbacks.OnReasoningStart != nil {
		streamCall.OnReasoningStart = func(id string, reasoning fantasy.ReasoningContent) error {
			return req.Callbacks.OnReasoningStart(id, fromFantasyReasoning(reasoning))
		}
	}
	if req.Callbacks.OnReasoningDelta != nil {
		streamCall.OnReasoningDelta = req.Callbacks.OnReasoningDelta
	}
	if req.Callbacks.OnReasoningEnd != nil {
		streamCall.OnReasoningEnd = func(id string, reasoning fantasy.ReasoningContent) error {
			return req.Callbacks.OnReasoningEnd(id, fromFantasyReasoning(reasoning))
		}
	}
	if req.Callbacks.OnTextDelta != nil {
		streamCall.OnTextDelta = req.Callbacks.OnTextDelta
	}
	if req.Callbacks.OnToolInputStart != nil {
		streamCall.OnToolInputStart = req.Callbacks.OnToolInputStart
	}
	if req.Callbacks.OnToolInputDelta != nil {
		streamCall.OnToolInputDelta = req.Callbacks.OnToolInputDelta
	}
	if req.Callbacks.OnToolCall != nil {
		streamCall.OnToolCall = func(toolCall fantasy.ToolCallContent) error {
			return req.Callbacks.OnToolCall(ToolCallPart{
				ToolCallID:       toolCall.ToolCallID,
				ToolName:         toolCall.ToolName,
				InputJSON:        toolCall.Input,
				ProviderExecuted: toolCall.ProviderExecuted,
			})
		}
	}
	if req.Callbacks.OnToolResult != nil {
		streamCall.OnToolResult = func(result fantasy.ToolResultContent) error {
			return req.Callbacks.OnToolResult(fromFantasyToolResult(result), result.ToolName)
		}
	}
	if req.Callbacks.OnStepFinish != nil {
		streamCall.OnStepFinish = func(step fantasy.StepResult) error {
			return req.Callbacks.OnStepFinish(fromFantasyStepResult(step))
		}
	}

	result, err := agent.Stream(ctx, streamCall)
	if err != nil {
		return nil, err
	}
	if result == nil {
		return &RunResult{}, nil
	}

	steps := make([]StepResult, 0, len(result.Steps))
	for _, step := range result.Steps {
		steps = append(steps, fromFantasyStepResult(step))
	}
	return &RunResult{Steps: steps}, nil
}

func toFantasyStopConditions(conditions []StopCondition) []fantasy.StopCondition {
	if len(conditions) == 0 {
		return nil
	}
	out := make([]fantasy.StopCondition, 0, len(conditions))
	for _, cond := range conditions {
		if cond == nil {
			continue
		}
		c := cond
		out = append(out, func(steps []fantasy.StepResult) bool {
			converted := make([]StepResult, 0, len(steps))
			for _, step := range steps {
				converted = append(converted, fromFantasyStepResult(step))
			}
			return c(converted)
		})
	}
	return out
}

func toFantasyFiles(files []FilePart) []fantasy.FilePart {
	if len(files) == 0 {
		return nil
	}
	out := make([]fantasy.FilePart, 0, len(files))
	for _, f := range files {
		out = append(out, fantasy.FilePart{
			Filename:  f.Filename,
			Data:      f.Data,
			MediaType: f.MediaType,
		})
	}
	return out
}

type ProviderMediaStrategy interface {
	SupportsToolResultMedia() bool
	AdaptMessages(messages []Message) []Message
}

type defaultProviderMediaStrategy struct {
	providerType ProviderType
}

func newProviderMediaStrategy(providerType ProviderType) ProviderMediaStrategy {
	return defaultProviderMediaStrategy{providerType: providerType}
}

func (s defaultProviderMediaStrategy) SupportsToolResultMedia() bool {
	return s.providerType == ProviderAnthropic || s.providerType == ProviderBedrock
}

func (s defaultProviderMediaStrategy) AdaptMessages(messages []Message) []Message {
	if len(messages) == 0 || s.SupportsToolResultMedia() {
		return messages
	}

	converted := make([]Message, 0, len(messages))
	for _, msg := range messages {
		if msg.Role != RoleTool {
			converted = append(converted, msg)
			continue
		}

		textParts := make([]Part, 0, len(msg.Content))
		mediaFiles := make([]FilePart, 0, len(msg.Content))

		for _, part := range msg.Content {
			toolResult, ok := part.(ToolResultPart)
			if !ok {
				textParts = append(textParts, part)
				continue
			}

			if toolResult.Output.Type != ToolResultOutputMedia {
				textParts = append(textParts, part)
				continue
			}

			decoded, err := base64.StdEncoding.DecodeString(toolResult.Output.Data)
			if err != nil {
				slog.Warn("failed to decode tool result media payload", "error", err)
				textParts = append(textParts, part)
				continue
			}

			mediaFiles = append(mediaFiles, FilePart{
				Filename:  fmt.Sprintf("tool-result-%s", toolResult.ToolCallID),
				Data:      decoded,
				MediaType: toolResult.Output.MediaType,
			})
			textParts = append(textParts, ToolResultPart{
				ToolCallID: toolResult.ToolCallID,
				Output: ToolResultOutput{
					Type: ToolResultOutputText,
					Text: "[Image/media content loaded - see attached file]",
				},
				Metadata: toolResult.Metadata,
			})
		}

		converted = append(converted, Message{
			Role:    RoleTool,
			Content: textParts,
		})

		if len(mediaFiles) == 0 {
			continue
		}

		converted = append(converted, Message{
			Role: RoleUser,
			Content: []Part{
				TextPart{Text: "Here is the media content from the tool result:"},
				mediaFiles[0],
			},
		})
		for i := 1; i < len(mediaFiles); i++ {
			converted = append(converted, Message{
				Role:    RoleUser,
				Content: []Part{mediaFiles[i]},
			})
		}
	}

	return converted
}

func toFantasyMessages(messages []Message) ([]fantasy.Message, error) {
	if len(messages) == 0 {
		return nil, nil
	}
	out := make([]fantasy.Message, 0, len(messages))
	for _, msg := range messages {
		role, err := toFantasyRole(msg.Role)
		if err != nil {
			return nil, err
		}
		parts := make([]fantasy.MessagePart, 0, len(msg.Content))
		for _, p := range msg.Content {
			switch part := p.(type) {
			case TextPart:
				parts = append(parts, fantasy.TextPart{Text: part.Text})
			case FilePart:
				parts = append(parts, fantasy.FilePart{Filename: part.Filename, Data: part.Data, MediaType: part.MediaType})
			case ReasoningPart:
				opts, err := toFantasyProviderOptions(part.ProviderMetadata)
				if err != nil {
					return nil, err
				}
				parts = append(parts, fantasy.ReasoningPart{Text: part.Text, ProviderOptions: opts})
			case ToolCallPart:
				parts = append(parts, fantasy.ToolCallPart{
					ToolCallID:       part.ToolCallID,
					ToolName:         part.ToolName,
					Input:            part.InputJSON,
					ProviderExecuted: part.ProviderExecuted,
				})
			case ToolResultPart:
				output, err := toFantasyToolResultOutput(part.Output)
				if err != nil {
					return nil, err
				}
				parts = append(parts, fantasy.ToolResultPart{ToolCallID: part.ToolCallID, Output: output})
			default:
				return nil, fmt.Errorf("unsupported message part type %T", p)
			}
		}
		out = append(out, fantasy.Message{Role: role, Content: parts})
	}
	return out, nil
}

func fromFantasyMessages(messages []fantasy.Message) []Message {
	if len(messages) == 0 {
		return nil
	}
	out := make([]Message, 0, len(messages))
	for _, msg := range messages {
		role := fromFantasyRole(msg.Role)
		parts := make([]Part, 0, len(msg.Content))
		for _, p := range msg.Content {
			if textPart, ok := fantasy.AsMessagePart[fantasy.TextPart](p); ok {
				parts = append(parts, TextPart{Text: textPart.Text})
				continue
			}
			if filePart, ok := fantasy.AsMessagePart[fantasy.FilePart](p); ok {
				parts = append(parts, FilePart{Filename: filePart.Filename, MediaType: filePart.MediaType, Data: filePart.Data})
				continue
			}
			if reasoningPart, ok := fantasy.AsMessagePart[fantasy.ReasoningPart](p); ok {
				parts = append(parts, fromFantasyReasoningPart(reasoningPart))
				continue
			}
			if callPart, ok := fantasy.AsMessagePart[fantasy.ToolCallPart](p); ok {
				parts = append(parts, ToolCallPart{
					ToolCallID:       callPart.ToolCallID,
					ToolName:         callPart.ToolName,
					InputJSON:        callPart.Input,
					ProviderExecuted: callPart.ProviderExecuted,
				})
				continue
			}
			if resultPart, ok := fantasy.AsMessagePart[fantasy.ToolResultPart](p); ok {
				parts = append(parts, ToolResultPart{ToolCallID: resultPart.ToolCallID, Output: fromFantasyToolResultOutput(resultPart.Output)})
				continue
			}
		}
		out = append(out, Message{Role: role, Content: parts})
	}
	return out
}

func toFantasyRole(role Role) (fantasy.MessageRole, error) {
	switch role {
	case RoleSystem:
		return fantasy.MessageRoleSystem, nil
	case RoleUser:
		return fantasy.MessageRoleUser, nil
	case RoleAssistant:
		return fantasy.MessageRoleAssistant, nil
	case RoleTool:
		return fantasy.MessageRoleTool, nil
	default:
		return "", fmt.Errorf("unsupported role %q", role)
	}
}

func fromFantasyRole(role fantasy.MessageRole) Role {
	switch role {
	case fantasy.MessageRoleSystem:
		return RoleSystem
	case fantasy.MessageRoleUser:
		return RoleUser
	case fantasy.MessageRoleAssistant:
		return RoleAssistant
	case fantasy.MessageRoleTool:
		return RoleTool
	default:
		return RoleAssistant
	}
}

func toFantasyProviderOptions(opts ProviderOptions) (fantasy.ProviderOptions, error) {
	if len(opts) == 0 {
		return nil, nil
	}
	raw := make(map[string]json.RawMessage, len(opts))
	for k, v := range opts {
		raw[k] = append(json.RawMessage(nil), v...)
	}
	parsed, err := fantasy.UnmarshalProviderOptions(raw)
	if err != nil {
		return nil, err
	}
	return parsed, nil
}

func fromFantasyProviderOptions(opts fantasy.ProviderOptions) ProviderOptions {
	if len(opts) == 0 {
		return nil
	}
	out := make(ProviderOptions, len(opts))
	for k, v := range opts {
		data, err := json.Marshal(v)
		if err != nil {
			continue
		}
		out[k] = data
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func fromFantasyProviderMetadata(metadata fantasy.ProviderMetadata) ProviderOptions {
	if len(metadata) == 0 {
		return nil
	}
	out := make(ProviderOptions, len(metadata))
	for k, v := range metadata {
		data, err := json.Marshal(v)
		if err != nil {
			continue
		}
		out[k] = data
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func toFantasyToolResultOutput(output ToolResultOutput) (fantasy.ToolResultOutputContent, error) {
	switch output.Type {
	case ToolResultOutputText:
		return fantasy.ToolResultOutputContentText{Text: output.Text}, nil
	case ToolResultOutputError:
		return fantasy.ToolResultOutputContentError{Error: errors.New(output.ErrorText)}, nil
	case ToolResultOutputMedia:
		return fantasy.ToolResultOutputContentMedia{Text: output.Text, Data: output.Data, MediaType: output.MediaType}, nil
	default:
		return nil, fmt.Errorf("unsupported tool result output type %q", output.Type)
	}
}

func fromFantasyToolResultOutput(output fantasy.ToolResultOutputContent) ToolResultOutput {
	switch output.GetType() {
	case fantasy.ToolResultContentTypeText:
		if text, ok := fantasy.AsToolResultOutputType[fantasy.ToolResultOutputContentText](output); ok {
			return ToolResultOutput{Type: ToolResultOutputText, Text: text.Text}
		}
	case fantasy.ToolResultContentTypeError:
		if terr, ok := fantasy.AsToolResultOutputType[fantasy.ToolResultOutputContentError](output); ok {
			message := ""
			if terr.Error != nil {
				message = terr.Error.Error()
			}
			return ToolResultOutput{Type: ToolResultOutputError, ErrorText: message}
		}
	case fantasy.ToolResultContentTypeMedia:
		if media, ok := fantasy.AsToolResultOutputType[fantasy.ToolResultOutputContentMedia](output); ok {
			return ToolResultOutput{Type: ToolResultOutputMedia, Text: media.Text, Data: media.Data, MediaType: media.MediaType}
		}
	}
	return ToolResultOutput{}
}

func fromFantasyReasoning(content fantasy.ReasoningContent) ReasoningPart {
	return ReasoningPart{Text: content.Text, ProviderMetadata: fromFantasyProviderMetadata(content.ProviderMetadata)}
}

func fromFantasyReasoningPart(part fantasy.ReasoningPart) ReasoningPart {
	return ReasoningPart{Text: part.Text, ProviderMetadata: fromFantasyProviderOptions(part.ProviderOptions)}
}

func fromFantasyToolResult(result fantasy.ToolResultContent) ToolResultPart {
	return ToolResultPart{
		ToolCallID: result.ToolCallID,
		Output:     fromFantasyToolResultOutput(result.Result),
		Metadata:   result.ClientMetadata,
	}
}

func fromFantasyStepResult(step fantasy.StepResult) StepResult {
	return StepResult{
		FinishReason: fromFantasyFinishReason(step.FinishReason),
		Usage: Usage{
			InputTokens:     step.Usage.InputTokens,
			OutputTokens:    step.Usage.OutputTokens,
			CacheReadTokens: step.Usage.CacheReadTokens,
		},
	}
}

func fromFantasyFinishReason(reason fantasy.FinishReason) FinishReason {
	switch reason {
	case fantasy.FinishReasonStop:
		return FinishReasonStop
	case fantasy.FinishReasonLength:
		return FinishReasonLength
	case fantasy.FinishReasonToolCalls:
		return FinishReasonToolCalls
	default:
		return FinishReasonError
	}
}

type fantasyToolAdapter struct {
	tool Tool
}

func toFantasyTools(tools []Tool) []fantasy.AgentTool {
	if len(tools) == 0 {
		return nil
	}
	out := make([]fantasy.AgentTool, 0, len(tools))
	for _, tool := range tools {
		out = append(out, &fantasyToolAdapter{tool: tool})
	}
	return out
}

func (t *fantasyToolAdapter) Info() fantasy.ToolInfo {
	info := t.tool.Info()
	return fantasy.ToolInfo{
		Name:        info.Name,
		Description: info.Description,
		Parameters:  info.Parameters,
		Required:    info.Required,
		Parallel:    info.Parallel,
	}
}

func (t *fantasyToolAdapter) Run(ctx context.Context, params fantasy.ToolCall) (fantasy.ToolResponse, error) {
	resp, err := t.tool.Run(ctx, ToolCall{ID: params.ID, Name: params.Name, Input: params.Input})
	if err != nil {
		return fantasy.ToolResponse{}, err
	}
	return fantasy.ToolResponse{
		Type:      toFantasyToolResponseType(resp.Type),
		Content:   resp.Content,
		Data:      resp.Data,
		MediaType: resp.MediaType,
		Metadata:  resp.Metadata,
		IsError:   resp.IsError,
	}, nil
}

func (t *fantasyToolAdapter) ProviderOptions() fantasy.ProviderOptions {
	opts, err := toFantasyProviderOptions(t.tool.ProviderOptions())
	if err != nil {
		return nil
	}
	return opts
}

func (t *fantasyToolAdapter) SetProviderOptions(opts fantasy.ProviderOptions) {
	t.tool.SetProviderOptions(fromFantasyProviderOptions(opts))
}

func toFantasyToolResponseType(responseType ToolResponseType) string {
	switch responseType {
	case ToolResponseTypeText:
		return "text"
	case ToolResponseTypeMedia:
		// fantasy currently expects media tool responses with type "image".
		return "image"
	default:
		return string(responseType)
	}
}
