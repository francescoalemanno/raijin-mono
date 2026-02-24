package core

type AgentEventKind string

const (
	EventTextDelta      AgentEventKind = "text_delta"
	EventThinking       AgentEventKind = "thinking_delta"
	EventToolCall       AgentEventKind = "tool_call"
	EventToolInputDelta AgentEventKind = "tool_input_delta"
	EventToolResult     AgentEventKind = "tool_result"
	EventContextChars   AgentEventKind = "context_chars"
	EventTotalTokens    AgentEventKind = "total_tokens"
	EventCancelled      AgentEventKind = "cancelled"
	EventStreaming      AgentEventKind = "streaming"
)

type AgentEvent struct {
	Kind            AgentEventKind
	ID              string // Unique ID for tool calls (ToolUseID)
	Text            string
	Name            string
	Description     string
	Input           string
	Output          string
	MediaDataBase64 string
	MediaType       string
	Metadata        string
	IsError         bool
	Chars           int
	TotalTokens     int64 // Cumulative tokens used in current run
	ContextWindow   int64 // Context window size for fill percentage
}

type AgentEventCallback func(AgentEvent)
