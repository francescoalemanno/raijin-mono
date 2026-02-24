package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"

	"charm.land/fantasy/schema"
)

type typedTool[T any] struct {
	info            ToolInfo
	handler         func(context.Context, T, ToolCall) (ToolResponse, error)
	providerOptions ProviderOptions
}

func (t *typedTool[T]) Info() ToolInfo {
	return t.info
}

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

func (t *typedTool[T]) ProviderOptions() ProviderOptions {
	if len(t.providerOptions) == 0 {
		return nil
	}
	out := make(ProviderOptions, len(t.providerOptions))
	for k, v := range t.providerOptions {
		out[k] = append(json.RawMessage(nil), v...)
	}
	return out
}

func (t *typedTool[T]) SetProviderOptions(opts ProviderOptions) {
	if len(opts) == 0 {
		t.providerOptions = nil
		return
	}
	t.providerOptions = make(ProviderOptions, len(opts))
	for k, v := range opts {
		t.providerOptions[k] = append(json.RawMessage(nil), v...)
	}
}

// NewAgentTool creates a typed tool with automatic schema generation.
func NewAgentTool[TInput any](
	name string,
	description string,
	fn func(ctx context.Context, input TInput, call ToolCall) (ToolResponse, error),
) Tool {
	return newTypedTool(name, description, false, fn)
}

// NewParallelAgentTool creates a typed tool marked as parallelizable.
func NewParallelAgentTool[TInput any](
	name string,
	description string,
	fn func(ctx context.Context, input TInput, call ToolCall) (ToolResponse, error),
) Tool {
	return newTypedTool(name, description, true, fn)
}

// NewTool creates a tool from explicit metadata and a typed handler.
func NewTool[TInput any](
	info ToolInfo,
	fn func(ctx context.Context, input TInput, call ToolCall) (ToolResponse, error),
) Tool {
	if info.Parameters == nil || info.Required == nil {
		params, required := schemaFor[TInput]()
		if info.Parameters == nil {
			info.Parameters = params
		}
		if info.Required == nil {
			info.Required = required
		}
	}
	return &typedTool[TInput]{info: info, handler: fn}
}

func newTypedTool[TInput any](
	name string,
	description string,
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

func schemaFor[T any]() (map[string]any, []string) {
	inputType := reflect.TypeOf((*T)(nil)).Elem()
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

// NewTextResponse creates a text tool response.
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

// WithResponseMetadata attaches JSON metadata to a response.
func WithResponseMetadata(response ToolResponse, metadata any) ToolResponse {
	data, err := json.Marshal(metadata)
	if err != nil {
		return response
	}
	response.Metadata = string(data)
	return response
}
