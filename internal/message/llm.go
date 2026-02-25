package message

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/francescoalemanno/raijin-mono/llmbridge/pkg/llm"
)

// UserRequest is a user input payload ready for llm.StreamRequest preparation.
type UserRequest struct {
	Prompt       string
	Attachments  []BinaryContent
	Skills       []SkillContent
	AllowedTools []string
}

// PreparedUserRequest is the normalized request payload for llm.StreamRequest.
type PreparedUserRequest struct {
	Prompt string
	Files  []llm.FilePart
}

// PrepareUserRequest builds a prompt and file attachments from message input.
func PrepareUserRequest(req UserRequest) PreparedUserRequest {
	prompt := PromptWithUserAttachments(req.Prompt, req.Attachments, req.Skills)
	prompt = withAllowedToolsNotice(prompt, req.AllowedTools)
	return PreparedUserRequest{
		Prompt: prompt,
		Files:  nonTextFiles(req.Attachments),
	}
}

// ToLLMMessages converts one persisted message into llm messages.
func ToLLMMessages(msg Message) []llm.Message {
	switch msg.Role {
	case User:
		return toUserMessages(msg)
	case Assistant:
		return toAssistantMessages(msg)
	case Tool:
		return toToolMessages(msg)
	default:
		return nil
	}
}

func toUserMessages(msg Message) []llm.Message {
	parts := make([]llm.Part, 0, 1+len(msg.BinaryContent()))
	text := strings.TrimSpace(msg.Content().Text)
	text = PromptWithUserAttachments(text, msg.BinaryContent(), msg.SkillContent())
	if text != "" {
		parts = append(parts, llm.TextPart{Text: text})
	}
	for _, file := range msg.BinaryContent() {
		if strings.HasPrefix(file.MIMEType, "text/") {
			continue
		}
		parts = append(parts, llm.FilePart{
			Filename:  file.Path,
			MediaType: file.MIMEType,
			Data:      file.Data,
		})
	}
	return []llm.Message{{Role: llm.RoleUser, Content: parts}}
}

func toAssistantMessages(msg Message) []llm.Message {
	parts := make([]llm.Part, 0, 1+len(msg.ToolCalls()))
	text := strings.TrimSpace(msg.Content().Text)
	if text != "" {
		parts = append(parts, llm.TextPart{Text: text})
	}
	reasoning := msg.ReasoningContent()
	if reasoning.Thinking != "" {
		parts = append(parts, llm.ReasoningPart{
			Text:             reasoning.Thinking,
			ProviderMetadata: cloneProviderMetadata(reasoning.ProviderMetadata),
		})
	}
	for _, call := range msg.ToolCalls() {
		parts = append(parts, llm.ToolCallPart{
			ToolCallID:       call.ID,
			ToolName:         call.Name,
			InputJSON:        call.Input,
			ProviderExecuted: call.ProviderExecuted,
		})
	}
	return []llm.Message{{Role: llm.RoleAssistant, Content: parts}}
}

func toToolMessages(msg Message) []llm.Message {
	parts := make([]llm.Part, 0, len(msg.ToolResults()))
	for _, result := range msg.ToolResults() {
		output := llm.ToolResultOutput{Type: llm.ToolResultOutputText, Text: result.Content}
		if result.IsError {
			output = llm.ToolResultOutput{
				Type:      llm.ToolResultOutputError,
				ErrorText: result.Content,
			}
		} else if result.Data != "" {
			output = llm.ToolResultOutput{
				Type:      llm.ToolResultOutputMedia,
				Text:      result.Content,
				Data:      result.Data,
				MediaType: result.MIMEType,
			}
		}
		parts = append(parts, llm.ToolResultPart{
			ToolCallID: result.ToolCallID,
			Output:     output,
			Metadata:   result.Metadata,
		})
	}
	return []llm.Message{{Role: llm.RoleTool, Content: parts}}
}

// FromLLMToolResult converts an llm tool-result part into a persisted message part.
func FromLLMToolResult(toolName string, part llm.ToolResultPart) ToolResult {
	result := ToolResult{
		ToolCallID: part.ToolCallID,
		Name:       toolName,
		Metadata:   part.Metadata,
	}
	switch part.Output.Type {
	case llm.ToolResultOutputError:
		result.Content = part.Output.ErrorText
		result.IsError = true
	case llm.ToolResultOutputMedia:
		result.Content = part.Output.Text
		if result.Content == "" {
			result.Content = fmt.Sprintf("Loaded %s content", part.Output.MediaType)
		}
		result.Data = part.Output.Data
		result.MIMEType = part.Output.MediaType
	default:
		result.Content = part.Output.Text
	}
	return result
}

func nonTextFiles(attachments []BinaryContent) []llm.FilePart {
	files := make([]llm.FilePart, 0, len(attachments))
	for _, attachment := range attachments {
		if strings.HasPrefix(attachment.MIMEType, "text/") {
			continue
		}
		files = append(files, llm.FilePart{
			Filename:  attachment.Path,
			Data:      attachment.Data,
			MediaType: attachment.MIMEType,
		})
	}
	return files
}

func withAllowedToolsNotice(prompt string, allowedTools []string) string {
	if len(allowedTools) == 0 {
		return prompt
	}
	var b strings.Builder
	b.WriteString(prompt)
	if strings.TrimSpace(prompt) != "" {
		b.WriteString("\n")
	}
	b.WriteString("<system_info>For this specific user request the only tools that are allowed are: ")
	b.WriteString(strings.Join(allowedTools, ", "))
	b.WriteString(".</system_info>\n")
	return b.String()
}

// PromptWithUserAttachments injects structured attachment context in a user prompt.
func PromptWithUserAttachments(prompt string, attachments []BinaryContent, skills []SkillContent) string {
	var sb strings.Builder
	sb.WriteString(prompt)

	hasAttachments := len(attachments) > 0
	if hasAttachments {
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

	addedSkillHeader := false
	for _, skill := range skills {
		rendered := strings.TrimSpace(skill.Content)
		if rendered == "" {
			continue
		}
		if !addedSkillHeader {
			sb.WriteString("\n<system_info>The skills below were explicitly loaded by the user for this request. Follow them while solving the task.</system_info>\n")
			addedSkillHeader = true
		}
		sb.WriteString("<skill name=\"")
		sb.WriteString(skill.Name)
		sb.WriteString("\">\n\n")
		sb.WriteString(rendered)
		sb.WriteString("\n</skill>\n")
	}

	return sb.String()
}

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
