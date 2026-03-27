package libagent

import (
	"context"
	"encoding/base64"
	"errors"
	"strings"
	"time"

	"charm.land/fantasy"
)

var ErrMessageNotFound = errors.New("message not found")

// MessageService defines runtime-message persistence operations.
type MessageService interface {
	Create(ctx context.Context, sessionID string, msg Message) (Message, error)
	Update(ctx context.Context, msg Message) error
	Get(ctx context.Context, id string) (Message, error)
	List(ctx context.Context, sessionID string) ([]Message, error)
}

func MessageID(m Message) string {
	switch msg := m.(type) {
	case *UserMessage:
		return msg.Meta.ID
	case *AssistantMessage:
		return msg.Meta.ID
	case *ToolResultMessage:
		return msg.Meta.ID
	default:
		return ""
	}
}

func MessageMetaOf(m Message) MessageMeta {
	switch msg := m.(type) {
	case *UserMessage:
		return msg.Meta
	case *AssistantMessage:
		return msg.Meta
	case *ToolResultMessage:
		return msg.Meta
	default:
		return MessageMeta{}
	}
}

func SetMessageMeta(m Message, meta MessageMeta) {
	switch msg := m.(type) {
	case *UserMessage:
		msg.Meta = meta
	case *AssistantMessage:
		msg.Meta = meta
	case *ToolResultMessage:
		msg.Meta = meta
	}
}

func CloneMessage(m Message) Message {
	switch msg := m.(type) {
	case *UserMessage:
		clone := *msg
		clone.Files = make([]FilePart, len(msg.Files))
		for i, f := range msg.Files {
			clone.Files[i] = FilePart{Filename: f.Filename, MediaType: f.MediaType, Data: append([]byte(nil), f.Data...)}
		}
		return &clone
	case *AssistantMessage:
		clone := *msg
		clone.ToolCalls = append([]ToolCallItem(nil), msg.ToolCalls...)
		clone.Content = append(clone.Content[:0:0], msg.Content...)
		return &clone
	case *ToolResultMessage:
		clone := *msg
		clone.Data = append([]byte(nil), msg.Data...)
		return &clone
	default:
		return m
	}
}

func SanitizeHistory(messages []Message) []Message {
	if len(messages) == 0 {
		return messages
	}

	out := make([]Message, 0, len(messages))
	for i := 0; i < len(messages); i++ {
		m := messages[i]
		switch msg := m.(type) {
		case *UserMessage:
			if strings.TrimSpace(msg.Content) == "" && len(msg.Files) == 0 {
				continue
			}
			out = append(out, CloneMessage(msg))
		case *AssistantMessage:
			if !msg.Completed {
				continue
			}
			clone := CloneMessage(msg).(*AssistantMessage)
			calls := AssistantToolCalls(clone)
			if len(calls) == 0 {
				if len(clone.Content) == 0 && strings.TrimSpace(clone.Text) == "" && strings.TrimSpace(clone.Reasoning) == "" {
					continue
				}
				out = append(out, clone)
				continue
			}
			j := i + 1
			results := make([]Message, 0, len(calls))
			matched := make([]bool, len(calls))
			for ; j < len(messages); j++ {
				trm, ok := messages[j].(*ToolResultMessage)
				if !ok {
					break
				}
				id := strings.TrimSpace(trm.ToolCallID)
				name := strings.TrimSpace(trm.ToolName)
				matchIdx := -1
				for callIdx, tc := range calls {
					if matched[callIdx] {
						continue
					}
					if strings.TrimSpace(tc.ID) == id && strings.TrimSpace(tc.Name) == name && id != "" && name != "" {
						matchIdx = callIdx
						break
					}
				}
				if matchIdx == -1 {
					break
				}
				matched[matchIdx] = true
				results = append(results, CloneMessage(trm))
			}
			allMatched := len(results) == len(calls)
			if !allMatched {
				continue
			}
			out = append(out, clone)
			out = append(out, results...)
			i = j - 1
		case *ToolResultMessage:
			// Tool results must be consumed contiguously by the immediately
			// preceding assistant turn. Standalone tool results are invalid.
			continue
		}
	}
	return out
}

// HasBijectiveToolCoupling returns true when assistant tool calls and tool
// results are balanced in contiguous turns:
//   - every assistant tool-call message is immediately followed by matching
//     tool-result messages before any user/assistant message appears
//   - every tool call has a matching tool result with the same ID and tool name
//   - all call IDs and tool names are non-empty
func HasBijectiveToolCoupling(messages []Message) bool {
	for i := 0; i < len(messages); i++ {
		switch m := messages[i].(type) {
		case *AssistantMessage:
			calls := AssistantToolCalls(m)
			if len(calls) == 0 {
				continue
			}
			for _, call := range calls {
				id := strings.TrimSpace(call.ID)
				name := strings.TrimSpace(call.Name)
				if id == "" || name == "" {
					return false
				}
			}
			matched := make([]bool, len(calls))
			j := i + 1
			for ; j < len(messages); j++ {
				trm, ok := messages[j].(*ToolResultMessage)
				if !ok {
					break
				}
				id := strings.TrimSpace(trm.ToolCallID)
				name := strings.TrimSpace(trm.ToolName)
				if id == "" || name == "" {
					return false
				}
				matchIdx := -1
				for callIdx, call := range calls {
					if matched[callIdx] {
						continue
					}
					if strings.TrimSpace(call.ID) == id && strings.TrimSpace(call.Name) == name {
						matchIdx = callIdx
						break
					}
				}
				if matchIdx == -1 {
					return false
				}
				matched[matchIdx] = true
			}
			for _, ok := range matched {
				if !ok {
					return false
				}
			}
			i = j - 1
		case *ToolResultMessage:
			return false
		}
	}
	return true
}

func AssistantText(msg *AssistantMessage) string {
	if msg == nil {
		return ""
	}
	var sb strings.Builder
	for _, c := range msg.Content {
		if v, ok := c.(fantasy.TextContent); ok {
			sb.WriteString(v.Text)
		}
	}
	return sb.String()
}

func AssistantReasoning(msg *AssistantMessage) string {
	if msg == nil {
		return ""
	}
	var sb strings.Builder
	for _, c := range msg.Content {
		if v, ok := c.(fantasy.ReasoningContent); ok {
			sb.WriteString(v.Text)
		}
	}
	return sb.String()
}

func AssistantToolCalls(msg *AssistantMessage) []ToolCallItem {
	if msg == nil {
		return nil
	}
	out := make([]ToolCallItem, 0, len(msg.Content.ToolCalls()))
	for _, tc := range msg.Content.ToolCalls() {
		out = append(out, ToolCallItem{
			ID:               tc.ToolCallID,
			Name:             tc.ToolName,
			Input:            tc.Input,
			ProviderExecuted: tc.ProviderExecuted,
		})
	}
	return out
}

func PromptWithUserAttachments(prompt string, attachments []FilePart) string {
	var sb strings.Builder
	sb.WriteString(prompt)
	if len(attachments) == 0 {
		return sb.String()
	}
	sb.WriteString("\n<attached_files>\n")
	for _, f := range attachments {
		kind := "file"
		if strings.HasPrefix(f.MediaType, "image/") {
			kind = "image"
		}
		sb.WriteString("- ")
		if f.Filename != "" {
			sb.WriteString(f.Filename)
		} else {
			sb.WriteString("(unnamed)")
		}
		sb.WriteString(" [")
		sb.WriteString(kind)
		if f.MediaType != "" {
			sb.WriteString(", ")
			sb.WriteString(f.MediaType)
		}
		sb.WriteString("]\n")
	}
	sb.WriteString("</attached_files>\n")
	for _, f := range attachments {
		sb.WriteString("<attachment")
		if f.Filename != "" {
			sb.WriteString(" path=\"")
			sb.WriteString(f.Filename)
			sb.WriteString("\"")
		}
		if f.MediaType != "" {
			sb.WriteString(" mime_type=\"")
			sb.WriteString(f.MediaType)
			sb.WriteString("\"")
		}
		kind := "binary"
		if strings.HasPrefix(f.MediaType, "text/") {
			kind = "text"
		}
		sb.WriteString(" kind=\"")
		sb.WriteString(kind)
		sb.WriteString("\" />\n")
	}
	for _, f := range attachments {
		if !strings.HasPrefix(f.MediaType, "text/") {
			continue
		}
		if f.Filename != "" {
			sb.WriteString("<file path=\"")
			sb.WriteString(f.Filename)
			sb.WriteString("\">\n")
		} else {
			sb.WriteString("<file>\n")
		}
		sb.WriteString("\n")
		sb.Write(f.Data)
		sb.WriteString("\n</file>\n")
	}
	return sb.String()
}

func NonTextFiles(files []FilePart) []FilePart {
	out := make([]FilePart, 0, len(files))
	for _, f := range files {
		if strings.HasPrefix(f.MediaType, "text/") {
			continue
		}
		out = append(out, FilePart{Filename: f.Filename, MediaType: f.MediaType, Data: append([]byte(nil), f.Data...)})
	}
	return out
}

func EncodeDataString(data []byte) string {
	if len(data) == 0 {
		return ""
	}
	return base64.StdEncoding.EncodeToString(data)
}

func DecodeDataString(data string) []byte {
	if strings.TrimSpace(data) == "" {
		return nil
	}
	decoded, err := base64.StdEncoding.DecodeString(data)
	if err == nil {
		return decoded
	}
	return []byte(data)
}

func UnixMilliToTime(ms int64) time.Time {
	if ms == 0 {
		return time.Now()
	}
	return time.UnixMilli(ms)
}
