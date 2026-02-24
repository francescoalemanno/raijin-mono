package llm

import (
	"context"
	"encoding/json"
)

// ProviderType identifies a provider family.
type ProviderType string

const (
	ProviderOpenAI       ProviderType = "openai"
	ProviderAnthropic    ProviderType = "anthropic"
	ProviderGoogle       ProviderType = "google"
	ProviderOpenRouter   ProviderType = "openrouter"
	ProviderOpenAICompat ProviderType = "openai_compat"
	ProviderAzure        ProviderType = "azure"
	ProviderBedrock      ProviderType = "bedrock"
	ProviderVercel       ProviderType = "vercel"
	ProviderVertexAI     ProviderType = "vertexai"
)

// ProviderConfig contains runtime provider settings.
type ProviderConfig struct {
	ID              string
	Name            string
	Type            ProviderType
	APIKey          string
	BaseURL         string
	ExtraHeaders    map[string]string
	ProviderOptions map[string]any
}

// ModelSelection selects a model for generation.
type ModelSelection struct {
	ProviderID      string
	ModelID         string
	ThinkingLevel   ThinkingLevel
	MaxOutputTokens int64
	Temperature     *float64
	TopP            *float64
	TopK            *int64
}

// ModelMetadata is optional static metadata for the selected model.
type ModelMetadata struct {
	ContextWindow int64
	MaxOutput     int64
	CanReason     bool
	SupportsImage bool
}

// Role is the role of an LLM message.
type Role string

const (
	RoleSystem    Role = "system"
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
	RoleTool      Role = "tool"
)

// Message represents a provider-agnostic chat message.
type Message struct {
	Role    Role
	Content []Part
}

// Part is a message part.
type Part interface {
	isPart()
}

// TextPart is plain text content.
type TextPart struct {
	Text string
}

func (TextPart) isPart() {}

// FilePart is binary attachment content.
type FilePart struct {
	Filename  string
	MediaType string
	Data      []byte
}

func (FilePart) isPart() {}

// ReasoningPart represents model reasoning.
type ReasoningPart struct {
	Text             string
	ProviderMetadata map[string]json.RawMessage
}

func (ReasoningPart) isPart() {}

// ToolCallPart is an assistant tool call.
type ToolCallPart struct {
	ToolCallID       string
	ToolName         string
	InputJSON        string
	ProviderExecuted bool
}

func (ToolCallPart) isPart() {}

// ToolResultOutputType identifies a tool result payload type.
type ToolResultOutputType string

const (
	ToolResultOutputText  ToolResultOutputType = "text"
	ToolResultOutputError ToolResultOutputType = "error"
	ToolResultOutputMedia ToolResultOutputType = "media"
)

// ToolResultOutput is provider-agnostic tool output.
type ToolResultOutput struct {
	Type      ToolResultOutputType
	Text      string
	ErrorText string
	Data      string
	MediaType string
}

// ToolResultPart is a tool role message part.
type ToolResultPart struct {
	ToolCallID string
	Output     ToolResultOutput
	Metadata   string
}

func (ToolResultPart) isPart() {}

// ToolInfo describes a callable tool.
type ToolInfo struct {
	Name        string
	Description string
	Parameters  map[string]any
	Required    []string
	Parallel    bool
}

// ToolCall is a tool invocation.
type ToolCall struct {
	ID    string
	Name  string
	Input string
}

// ToolResponseType identifies the payload kind returned by tools.
type ToolResponseType string

const (
	ToolResponseTypeText  ToolResponseType = "text"
	ToolResponseTypeMedia ToolResponseType = "media"
)

// ToolResponse is the response returned by a tool.
type ToolResponse struct {
	Type      ToolResponseType
	Content   string
	Data      []byte
	MediaType string
	Metadata  string
	IsError   bool
}

// ProviderOptions are raw provider-specific options keyed by provider name.
type ProviderOptions map[string]json.RawMessage

// Tool is the provider-agnostic tool interface.
type Tool interface {
	Info() ToolInfo
	Run(ctx context.Context, params ToolCall) (ToolResponse, error)
	ProviderOptions() ProviderOptions
	SetProviderOptions(opts ProviderOptions)
}

// FinishReason indicates why a step ended.
type FinishReason string

const (
	FinishReasonStop      FinishReason = "stop"
	FinishReasonLength    FinishReason = "length"
	FinishReasonToolCalls FinishReason = "tool_calls"
	FinishReasonError     FinishReason = "error"
)

// Usage reports token usage.
type Usage struct {
	InputTokens     int64
	OutputTokens    int64
	CacheReadTokens int64
}

// StepResult represents one streamed agent step.
type StepResult struct {
	FinishReason FinishReason
	Usage        Usage
}

// StopCondition can stop a stream after each step.
type StopCondition func(history []StepResult) bool

// StreamCallbacks receives streamed events.
type StreamCallbacks struct {
	PrepareStep      func(context.Context, []Message) ([]Message, error)
	OnReasoningStart func(id string, part ReasoningPart) error
	OnReasoningDelta func(id string, delta string) error
	OnReasoningEnd   func(id string, part ReasoningPart) error
	OnTextDelta      func(id string, delta string) error
	OnToolInputStart func(id string, toolName string) error
	OnToolInputDelta func(id string, delta string) error
	OnToolCall       func(call ToolCallPart) error
	OnToolResult     func(result ToolResultPart, toolName string) error
	OnStepFinish     func(step StepResult) error
}

// StreamRequest is a runtime stream call.
type StreamRequest struct {
	Prompt          string
	SystemPrompt    string
	Files           []FilePart
	Messages        []Message
	Tools           []Tool
	MaxOutputTokens *int64
	Temperature     *float64
	TopP            *float64
	TopK            *int64
	StopWhen        []StopCondition
	Callbacks       StreamCallbacks
}

// RunResult is the stream result summary.
type RunResult struct {
	Steps []StepResult
}

// Runtime executes streaming generation.
type Runtime interface {
	Stream(ctx context.Context, req StreamRequest) (*RunResult, error)
	// ProviderID returns the provider ID for this runtime.
	ProviderID() string
	// RefreshAPIKey updates the API key and recreates the underlying model.
	// Used for OAuth token refresh scenarios.
	RefreshAPIKey(ctx context.Context, newAPIKey string) error
}

// SecretResolver resolves references like $ENV placeholders.
type SecretResolver interface {
	Resolve(raw string) (string, error)
}

// TokenRefreshCallback is called before API requests to refresh OAuth tokens if needed.
// It should return the new access token (or the same one if no refresh was needed) and any error.
type TokenRefreshCallback func(providerID string) (newAccessToken string, err error)

// RuntimeFactory builds Runtime instances for provider/model selections.
type RuntimeFactory interface {
	NewRuntime(ctx context.Context, provider ProviderConfig, model ModelSelection, resolver SecretResolver) (Runtime, ModelMetadata, error)
}
