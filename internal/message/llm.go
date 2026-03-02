package message

import (
	"encoding/json"
	"strings"
	"time" //nolint:depguard // used only inside this package via unixMilliToTime

	libagent "github.com/francescoalemanno/raijin-mono/libagent"
)

func cloneProviderMetadata(metadata map[string]json.RawMessage) map[string]json.RawMessage {
	if len(metadata) == 0 {
		return nil
	}
	out := make(map[string]json.RawMessage, len(metadata))
	for key, value := range metadata {
		out[key] = append(json.RawMessage(nil), value...)
	}
	return out
}

func unixMilliToTime(ms int64) time.Time {
	if ms == 0 {
		return time.Now()
	}
	return time.UnixMilli(ms)
}

// UserRequest is a user input payload for agent preparation.
type UserRequest struct {
	Prompt      string
	Attachments []BinaryContent
}

// PreparedUserRequest is the normalized request payload for the agent.
type PreparedUserRequest struct {
	Prompt string
	Files  []libagent.FilePart
}

// PrepareUserRequest builds a prompt and file attachments from message input.
func PrepareUserRequest(req UserRequest) PreparedUserRequest {
	prompt := PromptWithUserAttachments(req.Prompt, req.Attachments)
	return PreparedUserRequest{
		Prompt: prompt,
		Files:  nonTextFiles(req.Attachments),
	}
}

// ToAgentMessages converts one persisted message into libagent.Message slice.
func ToAgentMessages(msg Message) []libagent.Message {
	switch msg.Role {
	case User:
		return toUserAgentMessages(msg)
	case Assistant:
		return toAssistantAgentMessages(msg)
	case Tool:
		return toToolAgentMessages(msg)
	default:
		return nil
	}
}

func toUserAgentMessages(msg Message) []libagent.Message {
	text := strings.TrimSpace(msg.Content().Text)
	text = PromptWithUserAttachments(text, msg.BinaryContent())
	files := make([]libagent.FilePart, 0, len(msg.BinaryContent()))
	for _, file := range msg.BinaryContent() {
		if strings.HasPrefix(file.MIMEType, "text/") {
			continue
		}
		files = append(files, libagent.FilePart{
			Filename:  file.Path,
			MediaType: file.MIMEType,
			Data:      file.Data,
		})
	}
	return []libagent.Message{&libagent.UserMessage{
		Role:      "user",
		Content:   text,
		Files:     files,
		Timestamp: unixMilliToTime(msg.CreatedAt),
	}}
}

func toAssistantAgentMessages(msg Message) []libagent.Message {
	text := strings.TrimSpace(msg.Content().Text)
	reasoning := msg.ReasoningContent()
	toolCalls := make([]libagent.ToolCallItem, 0, len(msg.ToolCalls()))
	for _, call := range msg.ToolCalls() {
		toolCalls = append(toolCalls, libagent.ToolCallItem{
			ID:               call.ID,
			Name:             call.Name,
			Input:            call.Input,
			ProviderExecuted: call.ProviderExecuted,
		})
	}
	ts := unixMilliToTime(msg.CreatedAt)
	am := libagent.NewAssistantMessage(text, reasoning.Thinking, toolCalls, ts)
	if len(am.Content) == 0 {
		return nil
	}
	return []libagent.Message{am}
}

func toToolAgentMessages(msg Message) []libagent.Message {
	var out []libagent.Message
	ts := unixMilliToTime(msg.CreatedAt)
	for _, result := range msg.ToolResults() {
		tr := libagent.ToolResult{
			ToolCallID: result.ToolCallID,
			Name:       result.Name,
			Content:    result.Content,
			IsError:    result.IsError,
			Metadata:   result.Metadata,
		}
		if result.Data != "" {
			tr.Data = []byte(result.Data)
			tr.MIMEType = result.MIMEType
		}
		out = append(out, libagent.NewToolResultMessage(tr, ts))
	}
	return out
}

// FromAgentToolResult converts a libagent ToolResultMessage into a persisted ToolResult.
func FromAgentToolResult(msg *libagent.ToolResultMessage) ToolResult {
	result := ToolResult{
		ToolCallID: msg.ToolCallID,
		Name:       msg.ToolName,
		Metadata:   msg.Metadata,
	}
	if msg.IsError {
		result.Content = msg.Content
		result.IsError = true
	} else if len(msg.Data) > 0 {
		result.Content = msg.Content
		result.Data = string(msg.Data)
		result.MIMEType = msg.MIMEType
	} else {
		result.Content = msg.Content
	}
	return result
}

func nonTextFiles(attachments []BinaryContent) []libagent.FilePart {
	files := make([]libagent.FilePart, 0, len(attachments))
	for _, attachment := range attachments {
		if strings.HasPrefix(attachment.MIMEType, "text/") {
			continue
		}
		files = append(files, libagent.FilePart{
			Filename:  attachment.Path,
			Data:      attachment.Data,
			MediaType: attachment.MIMEType,
		})
	}
	return files
}

// PromptWithUserAttachments injects structured attachment context in a user prompt.
func PromptWithUserAttachments(prompt string, attachments []BinaryContent) string {
	var sb strings.Builder
	sb.WriteString(prompt)

	if len(attachments) > 0 {
		sb.WriteString("\n<system_info>Resolved @path attachments are already loaded. Use them directly; do not re-read the same files unless the user asks. If the user says \"this\", \"that\", \"these\", or \"attachment(s)\", assume all attachments unless narrowed. Text attachments are inlined in <file> blocks.</system_info>\n")
		sb.WriteString("<attached_files>\n")
		for _, content := range attachments {
			kind := "file"
			if strings.HasPrefix(content.MIMEType, "image/") {
				kind = "image"
			}
			sb.WriteString("- ")
			if content.Path != "" {
				sb.WriteString(content.Path)
			} else {
				sb.WriteString("(unnamed)")
			}
			sb.WriteString(" [")
			sb.WriteString(kind)
			if content.MIMEType != "" {
				sb.WriteString(", ")
				sb.WriteString(content.MIMEType)
			}
			sb.WriteString("]\n")
		}
		sb.WriteString("</attached_files>\n")
		for _, content := range attachments {
			sb.WriteString("<attachment")
			if content.Path != "" {
				sb.WriteString(" path=\"")
				sb.WriteString(content.Path)
				sb.WriteString("\"")
			}
			if content.MIMEType != "" {
				sb.WriteString(" mime_type=\"")
				sb.WriteString(content.MIMEType)
				sb.WriteString("\"")
			}
			kind := "binary"
			if strings.HasPrefix(content.MIMEType, "text/") {
				kind = "text"
			}
			sb.WriteString(" kind=\"")
			sb.WriteString(kind)
			sb.WriteString("\" />\n")
		}
		for _, content := range attachments {
			if !strings.HasPrefix(content.MIMEType, "text/") {
				continue
			}
			if content.Path != "" {
				sb.WriteString("<file path=\"")
				sb.WriteString(content.Path)
				sb.WriteString("\">\n")
			} else {
				sb.WriteString("<file>\n")
			}
			sb.WriteString("\n")
			sb.Write(content.Data)
			sb.WriteString("\n</file>\n")
		}
	}

	return sb.String()
}
