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

	type toolCouplingKey struct {
		id   string
		name string
	}
	type callRef struct {
		msgIdx  int
		callIdx int
	}
	pendingByKey := map[toolCouplingKey][]callRef{}
	validCalls := map[callRef]struct{}{}
	validResults := map[int]struct{}{}
	for msgIdx, m := range messages {
		switch msg := m.(type) {
		case *AssistantMessage:
			for callIdx, tc := range AssistantToolCalls(msg) {
				id := strings.TrimSpace(tc.ID)
				name := strings.TrimSpace(tc.Name)
				if id == "" || name == "" {
					continue
				}
				key := toolCouplingKey{id: id, name: name}
				pendingByKey[key] = append(pendingByKey[key], callRef{msgIdx: msgIdx, callIdx: callIdx})
			}
		case *ToolResultMessage:
			id := strings.TrimSpace(msg.ToolCallID)
			name := strings.TrimSpace(msg.ToolName)
			if id == "" || name == "" {
				continue
			}
			key := toolCouplingKey{id: id, name: name}
			pending := pendingByKey[key]
			if len(pending) == 0 {
				continue
			}
			match := pending[0]
			pending = pending[1:]
			if len(pending) == 0 {
				delete(pendingByKey, key)
			} else {
				pendingByKey[key] = pending
			}
			validCalls[match] = struct{}{}
			validResults[msgIdx] = struct{}{}
		}
	}

	out := make([]Message, 0, len(messages))
	for msgIdx, m := range messages {
		switch msg := m.(type) {
		case *UserMessage:
			if strings.TrimSpace(msg.Content) == "" && len(msg.Files) == 0 {
				continue
			}
			out = append(out, CloneMessage(msg))
		case *AssistantMessage:
			text := AssistantText(msg)
			reasoning := AssistantReasoning(msg)
			hasText := strings.TrimSpace(text) != "" || strings.TrimSpace(reasoning) != ""
			rawCalls := AssistantToolCalls(msg)
			calls := make([]ToolCallItem, 0, len(rawCalls))
			validCallIdx := make(map[int]struct{}, len(rawCalls))
			for callIdx, tc := range rawCalls {
				id := strings.TrimSpace(tc.ID)
				name := strings.TrimSpace(tc.Name)
				if id == "" || name == "" {
					continue
				}
				if _, ok := validCalls[callRef{msgIdx: msgIdx, callIdx: callIdx}]; !ok {
					continue
				}
				calls = append(calls, tc)
				validCallIdx[callIdx] = struct{}{}
			}
			if !msg.Completed {
				continue
			}
			if !hasText && len(calls) == 0 {
				continue
			}
			clone := CloneMessage(msg).(*AssistantMessage)
			filtered := make(fantasy.ResponseContent, 0, len(clone.Content))
			callIdx := 0
			for _, part := range clone.Content {
				tc, ok := part.(fantasy.ToolCallContent)
				if !ok {
					filtered = append(filtered, part)
					continue
				}
				id := strings.TrimSpace(tc.ToolCallID)
				name := strings.TrimSpace(tc.ToolName)
				keep := false
				if id != "" && name != "" {
					_, keep = validCallIdx[callIdx]
				}
				callIdx++
				if keep {
					filtered = append(filtered, tc)
				}
			}
			clone.Content = filtered
			clone.Text = AssistantText(clone)
			clone.Reasoning = AssistantReasoning(clone)
			clone.ToolCalls = nil
			out = append(out, clone)
		case *ToolResultMessage:
			if _, ok := validResults[msgIdx]; !ok {
				continue
			}
			out = append(out, CloneMessage(msg))
		}
	}
	return out
}

// HasBijectiveToolCoupling returns true when assistant tool calls and tool
// results are balanced in-order:
//   - every tool call has a matching tool result with the same ID and tool name
//   - every tool result matches a previously seen unmatched tool call
//   - all call IDs and tool names are non-empty
//
// This allows repeated tool-call IDs over time, as long as they remain
// balanced on the inspected message path.
func HasBijectiveToolCoupling(messages []Message) bool {
	type toolCouplingKey struct {
		id   string
		name string
	}
	pendingByKey := make(map[toolCouplingKey]int)

	for _, msg := range messages {
		switch m := msg.(type) {
		case *AssistantMessage:
			for _, call := range AssistantToolCalls(m) {
				id := strings.TrimSpace(call.ID)
				name := strings.TrimSpace(call.Name)
				if id == "" || name == "" {
					return false
				}
				key := toolCouplingKey{id: id, name: name}
				pendingByKey[key]++
			}
		case *ToolResultMessage:
			id := strings.TrimSpace(m.ToolCallID)
			name := strings.TrimSpace(m.ToolName)
			if id == "" || name == "" {
				return false
			}
			key := toolCouplingKey{id: id, name: name}
			pending := pendingByKey[key]
			if pending <= 0 {
				return false
			}
			pending--
			if pending == 0 {
				delete(pendingByKey, key)
			} else {
				pendingByKey[key] = pending
			}
		}
	}
	for _, pending := range pendingByKey {
		if pending != 0 {
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
