package message

import (
	"encoding/json"
	"time"
)

type MessageRole string

const (
	Assistant MessageRole = "assistant"
	User      MessageRole = "user"
	System    MessageRole = "system"
	Tool      MessageRole = "tool"
)

type FinishReason string

const (
	FinishReasonEndTurn   FinishReason = "end_turn"
	FinishReasonMaxTokens FinishReason = "max_tokens"
	FinishReasonToolUse   FinishReason = "tool_use"
	FinishReasonError     FinishReason = "error"
	FinishReasonUnknown   FinishReason = "unknown"
)

type ContentPart interface {
	isPart()
}

type ReasoningContent struct {
	Thinking         string                     `json:"thinking"`
	ProviderMetadata map[string]json.RawMessage `json:"provider_metadata,omitempty"`
	StartedAt        int64                      `json:"started_at,omitempty"`
	FinishedAt       int64                      `json:"finished_at,omitempty"`
}

func (rc ReasoningContent) String() string {
	return rc.Thinking
}

func (ReasoningContent) isPart() {}

type TextContent struct {
	Text string `json:"text"`
}

func (tc TextContent) String() string {
	return tc.Text
}

func (TextContent) isPart() {}

type BinaryContent struct {
	Path     string
	MIMEType string
	Data     []byte
}

func (BinaryContent) isPart() {}

type SkillContent struct {
	Name    string `json:"name"`
	Content string `json:"content"`
}

func (sc SkillContent) String() string {
	return sc.Content
}

func (SkillContent) isPart() {}

type ToolCall struct {
	ID               string `json:"id"`
	Name             string `json:"name"`
	Input            string `json:"input"`
	ProviderExecuted bool   `json:"provider_executed"`
	Finished         bool   `json:"finished"`
}

func (ToolCall) isPart() {}

type ToolResult struct {
	ToolCallID string `json:"tool_call_id"`
	Name       string `json:"name"`
	Content    string `json:"content"`
	Data       string `json:"data"`
	MIMEType   string `json:"mime_type"`
	Metadata   string `json:"metadata"`
	IsError    bool   `json:"is_error"`
}

func (ToolResult) isPart() {}

type Finish struct {
	Reason  FinishReason `json:"reason"`
	Time    int64        `json:"time"`
	Message string       `json:"message,omitempty"`
	Details string       `json:"details,omitempty"`
}

func (Finish) isPart() {}

type Message struct {
	ID        string
	Role      MessageRole
	SessionID string
	Parts     []ContentPart
	Model     string
	Provider  string
	CreatedAt int64
	UpdatedAt int64
}

func (m *Message) Content() TextContent {
	content, _ := firstPart[TextContent](m.Parts)
	return content
}

func (m *Message) ReasoningContent() ReasoningContent {
	content, _ := firstPart[ReasoningContent](m.Parts)
	return content
}

func (m *Message) BinaryContent() []BinaryContent {
	return allParts[BinaryContent](m.Parts)
}

func (m *Message) SkillContent() []SkillContent {
	return allParts[SkillContent](m.Parts)
}

func (m *Message) ToolCalls() []ToolCall {
	return allParts[ToolCall](m.Parts)
}

func (m *Message) ToolResults() []ToolResult {
	return allParts[ToolResult](m.Parts)
}

func (m *Message) AppendContent(delta string) {
	for i, part := range m.Parts {
		if c, ok := part.(TextContent); ok {
			m.Parts[i] = TextContent{Text: c.Text + delta}
			return
		}
	}
	m.Parts = append(m.Parts, TextContent{Text: delta})
}

func (m *Message) AppendReasoningContent(delta string) {
	for i, part := range m.Parts {
		if c, ok := part.(ReasoningContent); ok {
			m.Parts[i] = ReasoningContent{
				Thinking:         c.Thinking + delta,
				ProviderMetadata: cloneProviderMetadata(c.ProviderMetadata),
				StartedAt:        c.StartedAt,
				FinishedAt:       c.FinishedAt,
			}
			return
		}
	}
	m.Parts = append(m.Parts, ReasoningContent{
		Thinking:  delta,
		StartedAt: time.Now().Unix(),
	})
}

func (m *Message) SetReasoningProviderMetadata(metadata map[string]json.RawMessage) {
	if len(metadata) == 0 {
		return
	}
	for i, part := range m.Parts {
		if c, ok := part.(ReasoningContent); ok {
			m.Parts[i] = ReasoningContent{
				Thinking:         c.Thinking,
				ProviderMetadata: cloneProviderMetadata(metadata),
				StartedAt:        c.StartedAt,
				FinishedAt:       c.FinishedAt,
			}
			return
		}
	}
	m.Parts = append(m.Parts, ReasoningContent{
		ProviderMetadata: cloneProviderMetadata(metadata),
	})
}

func (m *Message) FinishThinking() {
	for i, part := range m.Parts {
		if c, ok := part.(ReasoningContent); ok {
			if c.FinishedAt == 0 {
				m.Parts[i] = ReasoningContent{
					Thinking:         c.Thinking,
					ProviderMetadata: cloneProviderMetadata(c.ProviderMetadata),
					StartedAt:        c.StartedAt,
					FinishedAt:       time.Now().Unix(),
				}
			}
			return
		}
	}
}

func (m *Message) AppendToolCallInput(toolCallID string, inputDelta string) {
	for i, part := range m.Parts {
		if c, ok := part.(ToolCall); ok && c.ID == toolCallID {
			m.Parts[i] = ToolCall{
				ID:               c.ID,
				Name:             c.Name,
				Input:            c.Input + inputDelta,
				ProviderExecuted: c.ProviderExecuted,
				Finished:         c.Finished,
			}
			return
		}
	}
}

func (m *Message) AddToolCall(tc ToolCall) {
	for i, part := range m.Parts {
		if c, ok := part.(ToolCall); ok && c.ID == tc.ID {
			m.Parts[i] = tc
			return
		}
	}
	m.Parts = append(m.Parts, tc)
}

func (m *Message) Clone() Message {
	clone := *m
	clone.Parts = make([]ContentPart, len(m.Parts))
	for i, part := range m.Parts {
		if bc, ok := part.(BinaryContent); ok {
			bc.Data = append([]byte(nil), bc.Data...)
			clone.Parts[i] = bc
		} else {
			clone.Parts[i] = part
		}
	}
	return clone
}

func (m *Message) AddFinish(reason FinishReason, message, details string) {
	for i, part := range m.Parts {
		if _, ok := part.(Finish); ok {
			m.Parts = append(m.Parts[:i], m.Parts[i+1:]...)
			break
		}
	}
	m.Parts = append(m.Parts, Finish{
		Reason:  reason,
		Time:    time.Now().Unix(),
		Message: message,
		Details: details,
	})
}

func firstPart[T any](parts []ContentPart) (T, bool) {
	var zero T
	for _, part := range parts {
		if typed, ok := part.(T); ok {
			return typed, true
		}
	}
	return zero, false
}

func allParts[T any](parts []ContentPart) []T {
	out := make([]T, 0)
	for _, part := range parts {
		if typed, ok := part.(T); ok {
			out = append(out, typed)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
