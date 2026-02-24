package message

import (
	"encoding/json"
	"slices"
	"time"

	"github.com/francescoalemanno/raijin-mono/llmbridge/pkg/codec"
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
	FinishReasonEndTurn          FinishReason = "end_turn"
	FinishReasonMaxTokens        FinishReason = "max_tokens"
	FinishReasonToolUse          FinishReason = "tool_use"
	FinishReasonCanceled         FinishReason = "canceled"
	FinishReasonError            FinishReason = "error"
	FinishReasonPermissionDenied FinishReason = "permission_denied"
	FinishReasonUnknown          FinishReason = "unknown"
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

type ImageURLContent struct {
	URL    string `json:"url"`
	Detail string `json:"detail,omitempty"`
}

func (iuc ImageURLContent) String() string {
	return iuc.URL
}

func (ImageURLContent) isPart() {}

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
	for _, part := range m.Parts {
		if c, ok := part.(TextContent); ok {
			return c
		}
	}
	return TextContent{}
}

func (m *Message) ReasoningContent() ReasoningContent {
	for _, part := range m.Parts {
		if c, ok := part.(ReasoningContent); ok {
			return c
		}
	}
	return ReasoningContent{}
}

func (m *Message) ImageURLContent() []ImageURLContent {
	var contents []ImageURLContent
	for _, part := range m.Parts {
		if c, ok := part.(ImageURLContent); ok {
			contents = append(contents, c)
		}
	}
	return contents
}

func (m *Message) BinaryContent() []BinaryContent {
	var contents []BinaryContent
	for _, part := range m.Parts {
		if c, ok := part.(BinaryContent); ok {
			contents = append(contents, c)
		}
	}
	return contents
}

func (m *Message) SkillContent() []SkillContent {
	var contents []SkillContent
	for _, part := range m.Parts {
		if c, ok := part.(SkillContent); ok {
			contents = append(contents, c)
		}
	}
	return contents
}

func (m *Message) ToolCalls() []ToolCall {
	var calls []ToolCall
	for _, part := range m.Parts {
		if c, ok := part.(ToolCall); ok {
			calls = append(calls, c)
		}
	}
	return calls
}

func (m *Message) ToolResults() []ToolResult {
	var results []ToolResult
	for _, part := range m.Parts {
		if c, ok := part.(ToolResult); ok {
			results = append(results, c)
		}
	}
	return results
}

func (m *Message) IsFinished() bool {
	for _, part := range m.Parts {
		if _, ok := part.(Finish); ok {
			return true
		}
	}
	return false
}

func (m *Message) FinishReason() FinishReason {
	for _, part := range m.Parts {
		if c, ok := part.(Finish); ok {
			return c.Reason
		}
	}
	return ""
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
			m.Parts = slices.Delete(m.Parts, i, i+1)
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

// ToAppMessage converts a persisted message to codec DTO form.
func (m *Message) ToAppMessage() codec.AppMessage {
	out := codec.AppMessage{
		Role:        toAppRole(m.Role),
		Text:        m.Content().Text,
		Attachments: ToAppAttachments(m.BinaryContent()),
		Skills:      ToAppSkills(m.SkillContent()),
	}

	reasoning := m.ReasoningContent()
	if reasoning.Thinking != "" || len(reasoning.ProviderMetadata) > 0 {
		out.Reasoning = &codec.AppReasoning{
			Text:             reasoning.Thinking,
			ProviderMetadata: cloneProviderMetadata(reasoning.ProviderMetadata),
		}
	}

	for _, call := range m.ToolCalls() {
		out.ToolCalls = append(out.ToolCalls, codec.AppToolCall{
			ID:               call.ID,
			Name:             call.Name,
			Input:            call.Input,
			ProviderExecuted: call.ProviderExecuted,
		})
	}

	for _, result := range m.ToolResults() {
		out.Results = append(out.Results, codec.AppToolResult{
			ToolCallID: result.ToolCallID,
			Name:       result.Name,
			Content:    result.Content,
			Data:       result.Data,
			MIMEType:   result.MIMEType,
			Metadata:   result.Metadata,
			IsError:    result.IsError,
		})
	}

	return out
}

func toAppRole(role MessageRole) codec.AppRole {
	switch role {
	case System:
		return codec.AppRoleSystem
	case User:
		return codec.AppRoleUser
	case Tool:
		return codec.AppRoleTool
	default:
		return codec.AppRoleAssistant
	}
}

// ToAppAttachments converts message binary contents to codec DTOs.
func ToAppAttachments(attachments []BinaryContent) []codec.AppAttachment {
	if len(attachments) == 0 {
		return nil
	}
	out := make([]codec.AppAttachment, 0, len(attachments))
	for _, attachment := range attachments {
		out = append(out, codec.AppAttachment{
			Path:     attachment.Path,
			MIMEType: attachment.MIMEType,
			Data:     attachment.Data,
		})
	}
	return out
}

// ToAppSkills converts message skill contents to codec DTOs.
func ToAppSkills(skills []SkillContent) []codec.AppSkill {
	if len(skills) == 0 {
		return nil
	}
	out := make([]codec.AppSkill, 0, len(skills))
	for _, skill := range skills {
		out = append(out, codec.AppSkill{
			Name:    skill.Name,
			Content: skill.Content,
		})
	}
	return out
}

// ToolResultFromApp converts a codec tool result into a persisted message part.
func ToolResultFromApp(result codec.AppToolResult) ToolResult {
	return ToolResult{
		ToolCallID: result.ToolCallID,
		Name:       result.Name,
		Content:    result.Content,
		Data:       result.Data,
		MIMEType:   result.MIMEType,
		Metadata:   result.Metadata,
		IsError:    result.IsError,
	}
}

func cloneProviderMetadata(metadata map[string]json.RawMessage) map[string]json.RawMessage {
	if len(metadata) == 0 {
		return nil
	}
	cloned := make(map[string]json.RawMessage, len(metadata))
	for key, value := range metadata {
		cloned[key] = append(json.RawMessage(nil), value...)
	}
	return cloned
}
