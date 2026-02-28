package libagent

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"

	"charm.land/fantasy"
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
