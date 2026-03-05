package persist

import (
	"encoding/json"
	"strings"
	"time"

	libagent "github.com/francescoalemanno/raijin-mono/libagent"
)

func messageToWalMsg(m libagent.Message) walMessage {
	switch msg := m.(type) {
	case *libagent.UserMessage:
		clone := *msg
		return walMessage{Kind: "user", User: &clone}
	case *libagent.AssistantMessage:
		clone := *msg
		clone.ToolCalls = append([]libagent.ToolCallItem(nil), msg.ToolCalls...)
		return walMessage{Kind: "assistant", Assistant: &clone}
	case *libagent.ToolResultMessage:
		clone := *msg
		clone.Data = append([]byte(nil), msg.Data...)
		return walMessage{Kind: "tool_result", ToolResult: &clone}
	default:
		return walMessage{}
	}
}

func walMsgToMessage(wm walMessage) (libagent.Message, bool) {
	switch wm.Kind {
	case "user":
		if wm.User != nil {
			return wm.User, true
		}
	case "assistant":
		if wm.Assistant != nil {
			return wm.Assistant, true
		}
	case "tool_result":
		if wm.ToolResult != nil {
			return wm.ToolResult, true
		}
	}

	return decodeLegacyWalMessage(wm)
}

func decodeLegacyWalMessage(wm walMessage) (libagent.Message, bool) {
	meta := libagent.MessageMeta{
		ID:        wm.ID,
		SessionID: wm.SessionID,
		Model:     wm.Model,
		Provider:  wm.Provider,
		CreatedAt: wm.CreatedAt,
		UpdatedAt: wm.UpdatedAt,
	}
	if meta.UpdatedAt == 0 {
		meta.UpdatedAt = meta.CreatedAt
	}
	ts := libagent.UnixMilliToTime(meta.CreatedAt)
	if meta.CreatedAt == 0 {
		ts = time.Now()
	}

	switch strings.ToLower(strings.TrimSpace(wm.Role)) {
	case "user":
		var text string
		files := make([]libagent.FilePart, 0)
		for _, raw := range wm.Parts {
			if part, ok := decodeWalPart(raw); ok {
				switch part.Kind {
				case "text":
					if part.Text != nil {
						text += part.Text.Text
					}
				case "binary":
					if part.Binary != nil {
						files = append(files, libagent.FilePart{Filename: part.Binary.Path, MediaType: part.Binary.MIMEType, Data: append([]byte(nil), part.Binary.Data...)})
					}
				}
			}
		}
		m := &libagent.UserMessage{Role: "user", Content: text, Files: files, Timestamp: ts, Meta: meta}
		return m, true
	case "assistant":
		am := &libagent.AssistantMessage{Role: "assistant", Timestamp: ts, Meta: meta}
		var legacyFinish *walLegacyFinish
		for _, raw := range wm.Parts {
			if part, ok := decodeWalPart(raw); ok {
				switch part.Kind {
				case "text":
					if part.Text != nil {
						am.Text += part.Text.Text
					}
				case "reasoning":
					if part.Reasoning != nil {
						am.Reasoning += part.Reasoning.Thinking
					}
				case "tool_call":
					if part.ToolCall != nil {
						am.ToolCalls = append(am.ToolCalls, libagent.ToolCallItem{ID: part.ToolCall.ID, Name: part.ToolCall.Name, Input: part.ToolCall.Input, ProviderExecuted: part.ToolCall.ProviderExecuted})
					}
				case "finish":
					if part.Finish != nil {
						cp := *part.Finish
						legacyFinish = &cp
					}
				}
			}
		}
		if wm.Completion != nil {
			am.Completed = wm.Completion.Finished
			am.CompleteReason = wm.Completion.Reason
			am.CompleteMessage = wm.Completion.Message
			am.CompleteDetails = wm.Completion.Details
		} else if legacyFinish != nil {
			am.Completed = true
			am.CompleteReason = legacyFinish.Reason
			am.CompleteMessage = legacyFinish.Message
			am.CompleteDetails = legacyFinish.Details
		}
		return am, true
	case "tool":
		for _, raw := range wm.Parts {
			part, ok := decodeWalPart(raw)
			if !ok || part.Kind != "tool_result" || part.ToolResult == nil {
				continue
			}
			tr := part.ToolResult
			m := &libagent.ToolResultMessage{
				Role:       "toolResult",
				ToolCallID: tr.ToolCallID,
				ToolName:   tr.Name,
				Content:    tr.Content,
				IsError:    tr.IsError,
				Data:       libagent.DecodeDataString(tr.Data),
				MIMEType:   tr.MIMEType,
				Metadata:   tr.Metadata,
				Timestamp:  ts,
				Meta:       meta,
			}
			return m, true
		}
	}
	return nil, false
}

type walLegacyPart struct {
	Kind       string               `json:"kind"`
	Text       *walLegacyText       `json:"text,omitempty"`
	Reasoning  *walLegacyReasoning  `json:"reasoning,omitempty"`
	Binary     *walLegacyBinary     `json:"binary,omitempty"`
	Skill      *walLegacySkill      `json:"skill,omitempty"`
	ToolCall   *walLegacyToolCall   `json:"tool_call,omitempty"`
	ToolResult *walLegacyToolResult `json:"tool_result,omitempty"`
	Finish     *walLegacyFinish     `json:"finish,omitempty"`
}

type walLegacyText struct{ Text string `json:"text"` }
type walLegacyReasoning struct{ Thinking string `json:"thinking"` }
type walLegacyBinary struct {
	Path     string `json:"path,omitempty"`
	MIMEType string `json:"mime_type,omitempty"`
	Data     []byte `json:"data,omitempty"`
}
type walLegacySkill struct {
	Name    string `json:"name"`
	Content string `json:"content"`
}
type walLegacyToolCall struct {
	ID               string `json:"id"`
	Name             string `json:"name"`
	Input            string `json:"input"`
	Finished         bool   `json:"finished"`
	ProviderExecuted bool   `json:"provider_executed,omitempty"`
}
type walLegacyToolResult struct {
	ToolCallID string `json:"tool_call_id"`
	Name       string `json:"name"`
	Content    string `json:"content"`
	IsError    bool   `json:"is_error,omitempty"`
	Data       string `json:"data,omitempty"`
	MIMEType   string `json:"mime_type,omitempty"`
	Metadata   string `json:"metadata,omitempty"`
}
type walLegacyFinish struct {
	Reason  string `json:"reason"`
	Time    int64  `json:"time"`
	Message string `json:"message,omitempty"`
	Details string `json:"details,omitempty"`
}

func decodeWalPart(raw json.RawMessage) (walLegacyPart, bool) {
	var part walLegacyPart
	if json.Unmarshal(raw, &part) == nil && validPart(part) {
		return part, true
	}

	var legacy legacyWalPart
	if json.Unmarshal(raw, &legacy) != nil {
		return walLegacyPart{}, false
	}
	return decodeLegacyWalPart(legacy)
}

func validPart(part walLegacyPart) bool {
	switch part.Kind {
	case "text":
		return part.Text != nil
	case "reasoning":
		return part.Reasoning != nil
	case "binary":
		return part.Binary != nil
	case "skill":
		return part.Skill != nil
	case "tool_call":
		return part.ToolCall != nil
	case "tool_result":
		return part.ToolResult != nil
	case "finish":
		return part.Finish != nil
	default:
		return false
	}
}

func decodeLegacyWalPart(wp legacyWalPart) (walLegacyPart, bool) {
	switch wp.T {
	case legacyWalPartText:
		var v walLegacyText
		if json.Unmarshal(wp.Data, &v) != nil {
			return walLegacyPart{}, false
		}
		return walLegacyPart{Kind: "text", Text: &v}, true
	case legacyWalPartReasoning:
		var v walLegacyReasoning
		if json.Unmarshal(wp.Data, &v) != nil {
			return walLegacyPart{}, false
		}
		return walLegacyPart{Kind: "reasoning", Reasoning: &v}, true
	case legacyWalPartToolCall:
		var v walLegacyToolCall
		if json.Unmarshal(wp.Data, &v) != nil {
			return walLegacyPart{}, false
		}
		return walLegacyPart{Kind: "tool_call", ToolCall: &v}, true
	case legacyWalPartToolResult:
		var v walLegacyToolResult
		if json.Unmarshal(wp.Data, &v) != nil {
			return walLegacyPart{}, false
		}
		return walLegacyPart{Kind: "tool_result", ToolResult: &v}, true
	case legacyWalPartBinary:
		var v walLegacyBinary
		if json.Unmarshal(wp.Data, &v) != nil {
			return walLegacyPart{}, false
		}
		return walLegacyPart{Kind: "binary", Binary: &v}, true
	case legacyWalPartSkill:
		var v walLegacySkill
		if json.Unmarshal(wp.Data, &v) != nil {
			return walLegacyPart{}, false
		}
		return walLegacyPart{Kind: "skill", Skill: &v}, true
	case legacyWalPartFinish:
		var v walLegacyFinish
		if json.Unmarshal(wp.Data, &v) != nil {
			return walLegacyPart{}, false
		}
		return walLegacyPart{Kind: "finish", Finish: &v}, true
	}
	return walLegacyPart{}, false
}
