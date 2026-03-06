package libagent

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"

	"charm.land/fantasy"
	"charm.land/fantasy/providers/openaicompat"
)

func toBase64(data []byte) string {
	if len(data) == 0 {
		return ""
	}
	return base64.StdEncoding.EncodeToString(data)
}

// DefaultConvertToLLM is the default ConvertToLLMFn.
// It passes through user, assistant, and toolResult messages and drops everything else.
func DefaultConvertToLLM(_ context.Context, messages []Message) ([]fantasy.Message, error) {
	var out []fantasy.Message
	for _, m := range messages {
		switch msg := m.(type) {
		case *UserMessage:
			parts := make([]fantasy.MessagePart, 0, 1+len(msg.Files))
			parts = append(parts, fantasy.TextPart{Text: msg.Content})
			for _, f := range msg.Files {
				parts = append(parts, fantasy.FilePart{
					Filename:  f.Filename,
					MediaType: f.MediaType,
					Data:      f.Data,
				})
			}
			out = append(out, fantasy.Message{
				Role:    fantasy.MessageRoleUser,
				Content: parts,
			})

		case *AssistantMessage:
			var parts []fantasy.MessagePart
			for _, c := range msg.Content {
				switch v := c.(type) {
				case fantasy.TextContent:
					parts = append(parts, fantasy.TextPart{Text: v.Text})
				case fantasy.ReasoningContent:
					parts = append(parts, fantasy.ReasoningPart{Text: v.Text})
				case fantasy.ToolCallContent:
					input := normalizeToolCallJSON(v.Input)
					parts = append(parts, fantasy.ToolCallPart{
						ToolCallID:       v.ToolCallID,
						ToolName:         v.ToolName,
						Input:            input,
						ProviderExecuted: v.ProviderExecuted,
					})
				}
			}

			if len(parts) > 0 {
				out = append(out, fantasy.Message{
					Role:    fantasy.MessageRoleAssistant,
					Content: parts,
				})
			}

		case *ToolResultMessage:
			var output fantasy.ToolResultOutputContent
			if msg.IsError {
				output = fantasy.ToolResultOutputContentError{Error: fmt.Errorf("%s", msg.Content)} //nolint:err113
			} else if len(msg.Data) > 0 {
				output = fantasy.ToolResultOutputContentMedia{
					Text:      msg.Content,
					Data:      toBase64(msg.Data),
					MediaType: msg.MIMEType,
				}
			} else {
				output = fantasy.ToolResultOutputContentText{Text: msg.Content}
			}
			part := fantasy.ToolResultPart{
				ToolCallID: msg.ToolCallID,
				Output:     output,
			}
			out = append(out, fantasy.Message{
				Role:    fantasy.MessageRoleTool,
				Content: []fantasy.MessagePart{part},
			})
		}
	}
	return out, nil
}

func normalizeToolCallJSON(input string) string {
	raw := strings.TrimSpace(input)
	if raw == "" || !json.Valid([]byte(raw)) {
		return "{}"
	}
	return raw
}

func defaultConvertToLLMForRuntime(providerType string, providerOptions fantasy.ProviderOptions) ConvertToLLMFn {
	return func(ctx context.Context, messages []Message) ([]fantasy.Message, error) {
		prepared := messages
		if needsOpenAICompatReasoningPlaceholder(providerType, providerOptions) {
			prepared = withOpenAICompatReasoningPlaceholder(messages)
		}
		return DefaultConvertToLLM(ctx, prepared)
	}
}

func needsOpenAICompatReasoningPlaceholder(providerType string, providerOptions fantasy.ProviderOptions) bool {
	if !strings.EqualFold(strings.TrimSpace(providerType), "openai-compat") {
		return false
	}
	raw, ok := providerOptions[openaicompat.Name]
	if !ok || raw == nil {
		return false
	}
	switch v := raw.(type) {
	case *openaicompat.ProviderOptions:
		return v != nil && v.ReasoningEffort != nil
	default:
		return false
	}
}

func withOpenAICompatReasoningPlaceholder(messages []Message) []Message {
	if len(messages) == 0 {
		return messages
	}
	out := make([]Message, 0, len(messages))
	changed := false

	for _, m := range messages {
		am, ok := m.(*AssistantMessage)
		if !ok {
			out = append(out, m)
			continue
		}

		if !assistantNeedsReasoningPlaceholder(am) {
			out = append(out, m)
			continue
		}

		clone := CloneMessage(am).(*AssistantMessage)
		clone.Content = append(clone.Content, fantasy.ReasoningContent{Text: " "})
		out = append(out, clone)
		changed = true
	}

	if !changed {
		return messages
	}
	return out
}

func assistantNeedsReasoningPlaceholder(am *AssistantMessage) bool {
	if am == nil {
		return false
	}
	if strings.TrimSpace(AssistantReasoning(am)) != "" {
		return false
	}
	return len(AssistantToolCalls(am)) > 0
}
