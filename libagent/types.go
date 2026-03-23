// Package libagent provides a stateful agent with tool execution and event streaming.
// It is built on charm.land/fantasy for LLM access.
package libagent

import (
	"context"
	"time"

	"charm.land/fantasy"
)

// FilePart is a binary file attachment using only stdlib types.
// It mirrors fantasy.FilePart but avoids the charm import in callers.
type FilePart struct {
	Filename  string
	MediaType string
	Data      []byte
}

// ToolCallItem carries the data for a single assistant tool call,
// used when constructing AssistantMessages from stored history.
type ToolCallItem struct {
	ID               string
	Name             string
	Input            string
	ProviderExecuted bool
}

// ToolResult carries the output of a single tool execution,
// used when constructing ToolResultMessages from stored history.
type ToolResult struct {
	ToolCallID string
	Name       string
	Content    string
	IsError    bool
	Data       []byte
	MIMEType   string
	Metadata   string
}

// Message represents a conversation message that the agent can work with.
// It can be a standard LLM message (user, assistant, toolResult) or a
// custom app-specific message type registered via CustomAgentMessage.
type Message interface {
	// GetRole returns the role of the message.
	GetRole() string
	// GetTimestamp returns when the message was created.
	GetTimestamp() time.Time
}

// MessageMeta carries persistence metadata so runtime messages can be stored
// directly without sidecar structs.
type MessageMeta struct {
	ID        string
	SessionID string
	Model     string
	Provider  string
	CreatedAt int64
	UpdatedAt int64
}

// UserMessage is a standard user message.
type UserMessage struct {
	Role      string
	Content   string
	Files     []FilePart
	Timestamp time.Time
	Meta      MessageMeta
}

func (m *UserMessage) GetRole() string         { return "user" }
func (m *UserMessage) GetTimestamp() time.Time { return m.Timestamp }

// AssistantMessage is a completed assistant response.
type AssistantMessage struct {
	Role            string
	Text            string
	Reasoning       string
	ToolCalls       []ToolCallItem
	Completed       bool
	CompleteReason  string
	CompleteMessage string
	CompleteDetails string
	Content         fantasy.ResponseContent
	FinishReason    fantasy.FinishReason
	Usage           fantasy.Usage
	Error           error
	Timestamp       time.Time
	Meta            MessageMeta
}

func (m *AssistantMessage) GetRole() string         { return "assistant" }
func (m *AssistantMessage) GetTimestamp() time.Time { return m.Timestamp }

// ToolResultMessage carries the result of a tool execution.
type ToolResultMessage struct {
	Role       string
	ToolCallID string
	ToolName   string
	Content    string
	IsError    bool
	Data       []byte
	MIMEType   string
	Metadata   string
	Timestamp  time.Time
	Meta       MessageMeta
}

func (m *ToolResultMessage) GetRole() string         { return "toolResult" }
func (m *ToolResultMessage) GetTimestamp() time.Time { return m.Timestamp }

// NewAssistantMessage builds an AssistantMessage from plain-Go data.
// It constructs the internal fantasy.ResponseContent without requiring callers to import charm.
func NewAssistantMessage(text, reasoning string, toolCalls []ToolCallItem, ts time.Time) *AssistantMessage {
	var content fantasy.ResponseContent
	if reasoning != "" {
		content = append(content, fantasy.ReasoningContent{Text: reasoning})
	}
	if text != "" {
		content = append(content, fantasy.TextContent{Text: text})
	}
	for _, tc := range toolCalls {
		content = append(content, fantasy.ToolCallContent{
			ToolCallID:       tc.ID,
			ToolName:         tc.Name,
			Input:            tc.Input,
			ProviderExecuted: tc.ProviderExecuted,
		})
	}
	return &AssistantMessage{
		Role:      "assistant",
		Text:      text,
		Reasoning: reasoning,
		ToolCalls: append([]ToolCallItem(nil), toolCalls...),
		Content:   content,
		Timestamp: ts,
	}
}

// SetAssistantUsage updates provider-reported token accounting without
// exposing fantasy types to callers outside libagent.
func SetAssistantUsage(m *AssistantMessage, inputTokens, cacheReadTokens, outputTokens int64) {
	if m == nil {
		return
	}
	m.Usage.InputTokens = inputTokens
	m.Usage.CacheReadTokens = cacheReadTokens
	m.Usage.OutputTokens = outputTokens
}

// NewToolResultMessage builds a ToolResultMessage from plain-Go data.
func NewToolResultMessage(tr ToolResult, ts time.Time) *ToolResultMessage {
	return &ToolResultMessage{
		Role:       "toolResult",
		ToolCallID: tr.ToolCallID,
		ToolName:   tr.Name,
		Content:    tr.Content,
		IsError:    tr.IsError,
		Data:       tr.Data,
		MIMEType:   tr.MIMEType,
		Metadata:   tr.Metadata,
		Timestamp:  ts,
	}
}

// AgentEventType identifies the type of an AgentEvent.
type AgentEventType string

const (
	// AgentEventTypeAgentStart is emitted when the agent begins processing.
	AgentEventTypeAgentStart AgentEventType = "agent_start"
	// AgentEventTypeAgentEnd is emitted when the agent completes with all new messages.
	AgentEventTypeAgentEnd AgentEventType = "agent_end"
	// AgentEventTypeTurnStart is emitted when a new turn begins (one LLM call + tool executions).
	AgentEventTypeTurnStart AgentEventType = "turn_start"
	// AgentEventTypeTurnEnd is emitted when a turn completes.
	AgentEventTypeTurnEnd AgentEventType = "turn_end"
	// AgentEventTypeMessageStart is emitted when any message begins.
	AgentEventTypeMessageStart AgentEventType = "message_start"
	// AgentEventTypeMessageUpdate is emitted during assistant streaming (text deltas etc.).
	AgentEventTypeMessageUpdate AgentEventType = "message_update"
	// AgentEventTypeMessageEnd is emitted when a message is fully received.
	AgentEventTypeMessageEnd AgentEventType = "message_end"
	// AgentEventTypeToolExecutionStart is emitted when a tool begins executing.
	AgentEventTypeToolExecutionStart AgentEventType = "tool_execution_start"
	// AgentEventTypeToolExecutionUpdate is emitted when a tool streams progress.
	AgentEventTypeToolExecutionUpdate AgentEventType = "tool_execution_update"
	// AgentEventTypeToolExecutionEnd is emitted when a tool completes.
	AgentEventTypeToolExecutionEnd AgentEventType = "tool_execution_end"
	// AgentEventTypeRetry is emitted when retrying a failed stream due to connection error.
	AgentEventTypeRetry AgentEventType = "retry"
)

// StreamDelta describes a single streaming increment from the assistant.
type StreamDelta struct {
	// Type is one of: "text_delta", "reasoning_delta", "tool_input_delta",
	// "text_start", "text_end", "reasoning_start", "reasoning_end",
	// "tool_input_start", "tool_input_end".
	Type string
	// ID is the content block ID.
	ID string
	// Delta is the incremental text (for delta events).
	Delta string
	// ToolName is the tool name (for tool_input_start).
	ToolName string
}

// AgentEvent is emitted by the agent loop to describe what is happening.
type AgentEvent struct {
	Type AgentEventType

	// AgentEnd
	Messages []Message

	// TurnEnd
	TurnMessage Message
	ToolResults []*ToolResultMessage

	// MessageStart / MessageUpdate / MessageEnd
	Message Message
	// MessageUpdate only: streaming increment
	Delta *StreamDelta

	// ToolExecutionStart / ToolExecutionUpdate / ToolExecutionEnd
	ToolCallID  string
	ToolName    string
	ToolArgs    string
	ToolResult  string
	ToolIsError bool

	// Retry only: message describing the retry attempt
	RetryMessage string
}

// AgentContext carries the complete conversation state passed to the loop.
type AgentContext struct {
	SystemPrompt string
	Messages     []Message
	Tools        []fantasy.AgentTool
}

// ToolUpdateFn is the callback a streaming tool calls to push partial results.
// The partial argument carries the same type as the final ToolResponse.
type ToolUpdateFn func(partial fantasy.ToolResponse)

// StreamingAgentTool is an optional extension of fantasy.AgentTool for tools
// that want to stream incremental updates during execution.
// If a tool implements this interface, libagent calls RunStreaming instead of Run
// and emits tool_execution_update events for each call to onUpdate.
type StreamingAgentTool interface {
	fantasy.AgentTool
	RunStreaming(ctx context.Context, params fantasy.ToolCall, onUpdate ToolUpdateFn) (fantasy.ToolResponse, error)
}

// ConvertToLLMFn converts AgentMessages to fantasy.Message slices before each LLM call.
// Custom message types should be converted or filtered here.
type ConvertToLLMFn func(ctx context.Context, messages []Message) ([]fantasy.Message, error)

// TransformContextFn optionally transforms messages before ConvertToLLMFn.
// Use it for context-window pruning or injecting external context.
type TransformContextFn func(ctx context.Context, messages []Message) ([]Message, error)

// AgentLoopConfig is the configuration for agentLoop / agentLoopContinue.
type AgentLoopConfig struct {
	Model fantasy.LanguageModel

	// ConvertToLLM converts AgentMessages to LLM-compatible fantasy.Messages.
	// If nil, only user/assistant/toolResult messages are forwarded.
	ConvertToLLM ConvertToLLMFn

	// TransformContext is an optional pre-pass over messages before ConvertToLLM.
	TransformContext TransformContextFn

	// SystemPromptOverride replaces context.SystemPrompt if set (used internally by Agent).
	SystemPromptOverride *string

	// ProviderOptions are passed to every fantasy.Call made by the loop.
	// Build them with BuildProviderOptions or RuntimeModel.BuildCallProviderOptions.
	ProviderOptions fantasy.ProviderOptions

	// MaxOutputTokens caps each LLM response. When nil no limit is sent.
	MaxOutputTokens *int64
}

// AgentState holds the full runtime state of an Agent.
type AgentState struct {
	SystemPrompt     string
	Model            fantasy.LanguageModel
	Tools            []fantasy.AgentTool
	Messages         []Message
	IsStreaming      bool
	StreamMessage    Message
	PendingToolCalls map[string]struct{}
	Error            error
}

// MediaSupport configures whether image/video inputs should be included at runtime.
// This is applied dynamically per run and does not mutate persisted message history.
type MediaSupport struct {
	Known bool
	// Enabled reports whether media inputs are supported by the active runtime model.
	Enabled bool
}
