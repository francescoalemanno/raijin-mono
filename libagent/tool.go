package libagent

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"

	"charm.land/fantasy"
	"charm.land/fantasy/schema"
)

// ToolInfo describes a callable tool using only stdlib types.
type ToolInfo struct {
	Name        string
	Description string
	Parameters  map[string]any
	Required    []string
	Parallel    bool
}

// ToolCall is a tool invocation from the LLM.
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

// ToolResponse is the response returned by a tool using only stdlib types.
type ToolResponse struct {
	Type      ToolResponseType
	Content   string
	Data      []byte
	MediaType string
	Metadata  string
	IsError   bool
}

// NewTextResponse creates a successful text tool response.
func NewTextResponse(content string) ToolResponse {
	return ToolResponse{Type: ToolResponseTypeText, Content: content}
}

// NewTextErrorResponse creates an error text tool response.
func NewTextErrorResponse(content string) ToolResponse {
	return ToolResponse{Type: ToolResponseTypeText, Content: content, IsError: true}
}

// NewMediaResponse creates a binary media tool response.
func NewMediaResponse(data []byte, mediaType string) ToolResponse {
	return ToolResponse{Type: ToolResponseTypeMedia, Data: data, MediaType: mediaType}
}

// WithResponseMetadata attaches JSON-marshalled metadata to a response.
func WithResponseMetadata(response ToolResponse, metadata any) ToolResponse {
	data, err := json.Marshal(metadata)
	if err != nil {
		return response
	}
	response.Metadata = string(data)
	return response
}

// Tool is the provider-agnostic tool interface implemented by callers.
// It uses only stdlib types so that packages outside libagent can implement
// it without importing charm.
type Tool interface {
	Info() ToolInfo
	Run(ctx context.Context, call ToolCall) (ToolResponse, error)
}

// StreamingTool optionally extends Tool with incremental update support.
type StreamingTool interface {
	Tool
	RunStreaming(ctx context.Context, call ToolCall, onUpdate func(ToolResponse)) (ToolResponse, error)
}

// Schema is an alias for a JSON Schema property map, matching the fantasy/schema format.
type Schema = map[string]any

// ParseJSONInput parses potentially malformed LLM-generated JSON into dst,
// using schema.ParsePartialJSON to repair the input before unmarshalling.
func ParseJSONInput(input []byte, dst any) error {
	parsed, _, err := schema.ParsePartialJSON(string(input))
	if err != nil {
		return fmt.Errorf("json repair failed: %w", err)
	}
	data, err := json.Marshal(parsed)
	if err != nil {
		return fmt.Errorf("json re-marshal failed: %w", err)
	}
	return json.Unmarshal(data, dst)
}

// NewTypedTool creates a Tool with automatic JSON schema generation from TInput.
// The handler receives a parsed TInput struct.
func NewTypedTool[TInput any](
	name, description string,
	fn func(ctx context.Context, input TInput, call ToolCall) (ToolResponse, error),
) Tool {
	return newTypedToolInternal(name, description, false, fn)
}

// NewParallelTypedTool creates a Tool marked as safe for parallel execution.
func NewParallelTypedTool[TInput any](
	name, description string,
	fn func(ctx context.Context, input TInput, call ToolCall) (ToolResponse, error),
) Tool {
	return newTypedToolInternal(name, description, true, fn)
}

func newTypedToolInternal[TInput any](
	name, description string,
	parallel bool,
	fn func(ctx context.Context, input TInput, call ToolCall) (ToolResponse, error),
) Tool {
	params, required := schemaFor[TInput]()
	return &typedTool[TInput]{
		info: ToolInfo{
			Name:        name,
			Description: description,
			Parameters:  params,
			Required:    required,
			Parallel:    parallel,
		},
		handler: fn,
	}
}

type typedTool[T any] struct {
	info    ToolInfo
	handler func(context.Context, T, ToolCall) (ToolResponse, error)
}

func (t *typedTool[T]) Info() ToolInfo { return t.info }

func (t *typedTool[T]) Run(ctx context.Context, params ToolCall) (ToolResponse, error) {
	var input T
	raw := params.Input
	if strings.TrimSpace(raw) == "" {
		raw = "{}"
	}
	if err := ParseJSONInput([]byte(raw), &input); err != nil {
		return NewTextErrorResponse(fmt.Sprintf("invalid tool input: %v", err)), nil
	}
	return t.handler(ctx, input, params)
}

func schemaFor[T any]() (map[string]any, []string) {
	inputType := reflect.TypeFor[T]()
	sch := schema.Generate(inputType)
	params := make(map[string]any, len(sch.Properties))
	for name, propSchema := range sch.Properties {
		if propSchema == nil {
			continue
		}
		params[name] = schema.ToMap(*propSchema)
	}
	required := append([]string(nil), sch.Required...)
	return params, required
}

// adaptTool wraps a Tool so it satisfies fantasy.AgentTool.
type adaptedTool struct {
	tool Tool
}

func (a *adaptedTool) Info() fantasy.ToolInfo {
	info := a.tool.Info()
	return fantasy.ToolInfo{
		Name:        info.Name,
		Description: info.Description,
		Parameters:  info.Parameters,
		Required:    info.Required,
		Parallel:    info.Parallel,
	}
}

func (a *adaptedTool) Run(ctx context.Context, params fantasy.ToolCall) (fantasy.ToolResponse, error) {
	resp, err := a.tool.Run(ctx, ToolCall{
		ID:    params.ID,
		Name:  params.Name,
		Input: params.Input,
	})
	if err != nil {
		return fantasy.ToolResponse{}, err
	}
	return adaptToolResponse(resp), nil
}

func (a *adaptedTool) RunStreaming(ctx context.Context, params fantasy.ToolCall, onUpdate ToolUpdateFn) (fantasy.ToolResponse, error) {
	st, ok := a.tool.(StreamingTool)
	if !ok {
		return a.Run(ctx, params)
	}
	resp, err := st.RunStreaming(ctx, ToolCall{
		ID:    params.ID,
		Name:  params.Name,
		Input: params.Input,
	}, func(partial ToolResponse) {
		onUpdate(adaptToolResponse(partial))
	})
	if err != nil {
		return fantasy.ToolResponse{}, err
	}
	return adaptToolResponse(resp), nil
}

func adaptToolResponse(resp ToolResponse) fantasy.ToolResponse {
	fr := fantasy.ToolResponse{
		Content:  resp.Content,
		Metadata: resp.Metadata,
		IsError:  resp.IsError,
	}
	if resp.Type == ToolResponseTypeMedia {
		fr.Type = "image"
		fr.Data = resp.Data
		fr.MediaType = resp.MediaType
	}
	return fr
}

func (a *adaptedTool) ProviderOptions() fantasy.ProviderOptions { return nil }

func (a *adaptedTool) SetProviderOptions(_ fantasy.ProviderOptions) {}

// AdaptTools converts a []Tool slice into []fantasy.AgentTool for use with libagent internals.
func AdaptTools(tools []Tool) []fantasy.AgentTool {
	if len(tools) == 0 {
		return nil
	}
	out := make([]fantasy.AgentTool, len(tools))
	for i, t := range tools {
		out[i] = &adaptedTool{tool: t}
	}
	return out
}
