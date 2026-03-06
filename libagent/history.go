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
	callCounts := map[string]int{}
	resultCounts := map[string]int{}
	for _, m := range messages {
		switch msg := m.(type) {
		case *AssistantMessage:
			for _, tc := range assistantToolCalls(msg) {
				id := strings.TrimSpace(tc.ID)
				if id != "" {
					callCounts[id]++
				}
			}
		case *ToolResultMessage:
			id := strings.TrimSpace(msg.ToolCallID)
			if id != "" {
				resultCounts[id]++
			}
		}
	}
	valid := map[string]struct{}{}
	for id, c := range callCounts {
		if c == 1 && resultCounts[id] == 1 {
			valid[id] = struct{}{}
		}
	}
	out := make([]Message, 0, len(messages))
	for _, m := range messages {
		switch msg := m.(type) {
		case *UserMessage:
			if strings.TrimSpace(msg.Content) == "" && len(msg.Files) == 0 {
				continue
			}
			out = append(out, CloneMessage(msg))
		case *AssistantMessage:
			text := assistantText(msg)
			reasoning := assistantReasoning(msg)
			hasText := strings.TrimSpace(text) != "" || strings.TrimSpace(reasoning) != ""
			rawCalls := assistantToolCalls(msg)
			calls := make([]ToolCallItem, 0, len(rawCalls))
			for _, tc := range rawCalls {
				id := strings.TrimSpace(tc.ID)
				name := strings.TrimSpace(tc.Name)
				if id == "" || name == "" {
					continue
				}
				if _, ok := valid[id]; !ok {
					continue
				}
				calls = append(calls, tc)
			}
			if !msg.Completed {
				continue
			}
			if !hasText && len(calls) == 0 {
				continue
			}
			clone := CloneMessage(msg).(*AssistantMessage)
			clone.Text = text
			clone.Reasoning = reasoning
			clone.ToolCalls = calls
			out = append(out, clone)
		case *ToolResultMessage:
			id := strings.TrimSpace(msg.ToolCallID)
			name := strings.TrimSpace(msg.ToolName)
			if id == "" || name == "" {
				continue
			}
			if _, ok := valid[id]; !ok {
				continue
			}
			out = append(out, CloneMessage(msg))
		}
	}
	return out
}

// HasBijectiveToolCoupling returns true when every assistant tool call ID has
// exactly one matching tool result ID, and vice versa.
func HasBijectiveToolCoupling(messages []Message) bool {
	callCounts := make(map[string]int)
	resultCounts := make(map[string]int)

	for _, msg := range messages {
		switch m := msg.(type) {
		case *AssistantMessage:
			for _, call := range assistantToolCalls(m) {
				id := strings.TrimSpace(call.ID)
				if id == "" {
					return false
				}
				callCounts[id]++
			}
		case *ToolResultMessage:
			id := strings.TrimSpace(m.ToolCallID)
			if id == "" {
				return false
			}
			resultCounts[id]++
		}
	}

	if len(callCounts) != len(resultCounts) {
		return false
	}
	for id, count := range callCounts {
		if count != 1 || resultCounts[id] != 1 {
			return false
		}
	}
	for id, count := range resultCounts {
		if count != 1 || callCounts[id] != 1 {
			return false
		}
	}
	return true
}

func assistantText(msg *AssistantMessage) string {
	if msg == nil {
		return ""
	}
	if strings.TrimSpace(msg.Text) != "" {
		return msg.Text
	}
	var sb strings.Builder
	for _, c := range msg.Content {
		if v, ok := c.(fantasy.TextContent); ok {
			sb.WriteString(v.Text)
		}
	}
	return sb.String()
}

func assistantReasoning(msg *AssistantMessage) string {
	if msg == nil {
		return ""
	}
	if strings.TrimSpace(msg.Reasoning) != "" {
		return msg.Reasoning
	}
	var sb strings.Builder
	for _, c := range msg.Content {
		if v, ok := c.(fantasy.ReasoningContent); ok {
			sb.WriteString(v.Text)
		}
	}
	return sb.String()
}

func assistantToolCalls(msg *AssistantMessage) []ToolCallItem {
	if msg == nil {
		return nil
	}
	out := make([]ToolCallItem, 0, len(msg.ToolCalls)+len(msg.Content.ToolCalls()))
	byID := make(map[string]int, len(msg.ToolCalls)+len(msg.Content.ToolCalls()))
	add := func(tc ToolCallItem) {
		id := strings.TrimSpace(tc.ID)
		if id == "" {
			out = append(out, tc)
			return
		}
		if idx, exists := byID[id]; exists {
			current := out[idx]
			if strings.TrimSpace(current.Name) == "" {
				current.Name = tc.Name
			}
			if strings.TrimSpace(current.Input) == "" {
				current.Input = tc.Input
			}
			current.ProviderExecuted = current.ProviderExecuted || tc.ProviderExecuted
			out[idx] = current
			return
		}
		byID[id] = len(out)
		out = append(out, tc)
	}

	for _, tc := range msg.ToolCalls {
		add(tc)
	}
	for _, tc := range msg.Content.ToolCalls() {
		add(ToolCallItem{
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
