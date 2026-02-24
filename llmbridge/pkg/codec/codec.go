package codec

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/francescoalemanno/raijin-mono/llmbridge/pkg/llm"
)

// AppRole is a message role in app-facing DTOs.
type AppRole string

const (
	AppRoleSystem    AppRole = "system"
	AppRoleUser      AppRole = "user"
	AppRoleAssistant AppRole = "assistant"
	AppRoleTool      AppRole = "tool"
)

// AppMessage is a lightweight application-facing DTO for codec helpers.
type AppMessage struct {
	Role        AppRole
	Text        string
	Reasoning   *AppReasoning
	Attachments []AppAttachment
	Skills      []AppSkill
	ToolCalls   []AppToolCall
	Results     []AppToolResult
}

// AppReasoning is reasoning metadata content.
type AppReasoning struct {
	Text             string
	ProviderMetadata map[string]json.RawMessage
}

// AppAttachment is a user attachment.
type AppAttachment struct {
	Path     string
	MIMEType string
	Data     []byte
}

// AppSkill is a loaded skill content block.
type AppSkill struct {
	Name    string
	Content string
}

// AppToolCall is a tool call entry.
type AppToolCall struct {
	ID               string
	Name             string
	Input            string
	ProviderExecuted bool
}

// AppToolResult is a tool result entry.
type AppToolResult struct {
	ToolCallID string
	Name       string
	Content    string
	Data       string
	MIMEType   string
	Metadata   string
	IsError    bool
}

// UserRequest is a user input payload ready for LLM request preparation.
type UserRequest struct {
	Prompt       string
	Attachments  []AppAttachment
	Skills       []AppSkill
	AllowedTools []string
}

// PreparedUserRequest is the normalized request payload for llm.StreamRequest.
type PreparedUserRequest struct {
	Prompt string
	Files  []llm.FilePart
}

// PrepareUserRequest builds a prompt and file attachments from app DTO input.
func PrepareUserRequest(req UserRequest) PreparedUserRequest {
	prompt := PromptWithUserAttachments(req.Prompt, req.Attachments, req.Skills)
	prompt = withAllowedToolsNotice(prompt, req.AllowedTools)
	return PreparedUserRequest{
		Prompt: prompt,
		Files:  nonTextFiles(req.Attachments),
	}
}

// ToLLMMessages converts one app DTO message into llm messages.
func ToLLMMessages(msg AppMessage) []llm.Message {
	switch msg.Role {
	case AppRoleUser:
		return toUserMessages(msg)
	case AppRoleAssistant:
		return toAssistantMessages(msg)
	case AppRoleTool:
		return toToolMessages(msg)
	default:
		return nil
	}
}

func toUserMessages(msg AppMessage) []llm.Message {
	parts := make([]llm.Part, 0, 1+len(msg.Attachments))
	text := strings.TrimSpace(msg.Text)
	text = PromptWithUserAttachments(text, msg.Attachments, msg.Skills)
	if text != "" {
		parts = append(parts, llm.TextPart{Text: text})
	}
	for _, file := range msg.Attachments {
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

func toAssistantMessages(msg AppMessage) []llm.Message {
	parts := make([]llm.Part, 0, 1+len(msg.ToolCalls))
	text := strings.TrimSpace(msg.Text)
	if text != "" {
		parts = append(parts, llm.TextPart{Text: text})
	}
	if msg.Reasoning != nil && msg.Reasoning.Text != "" {
		parts = append(parts, llm.ReasoningPart{
			Text:             msg.Reasoning.Text,
			ProviderMetadata: cloneProviderMetadata(msg.Reasoning.ProviderMetadata),
		})
	}
	for _, call := range msg.ToolCalls {
		parts = append(parts, llm.ToolCallPart{
			ToolCallID:       call.ID,
			ToolName:         call.Name,
			InputJSON:        call.Input,
			ProviderExecuted: call.ProviderExecuted,
		})
	}
	return []llm.Message{{Role: llm.RoleAssistant, Content: parts}}
}

func toToolMessages(msg AppMessage) []llm.Message {
	parts := make([]llm.Part, 0, len(msg.Results))
	for _, result := range msg.Results {
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
		parts = append(parts, llm.ToolResultPart{ToolCallID: result.ToolCallID, Output: output, Metadata: result.Metadata})
	}
	return []llm.Message{{Role: llm.RoleTool, Content: parts}}
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

// FromToolResult converts an llm tool-result part into app DTO form.
func FromToolResult(toolName string, part llm.ToolResultPart) AppToolResult {
	result := AppToolResult{
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

func nonTextFiles(attachments []AppAttachment) []llm.FilePart {
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
func PromptWithUserAttachments(prompt string, attachments []AppAttachment, skills []AppSkill) string {
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
