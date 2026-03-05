package libagent

import (
	"testing"
	"time"
)

func TestSanitizeHistory_PreservesAssistantStructuredContent(t *testing.T) {
	t.Parallel()

	assistant := NewAssistantMessage("done", "thinking", []ToolCallItem{{
		ID:    "call-1",
		Name:  "read",
		Input: `{"path":"README.md"}`,
	}}, time.Now())
	assistant.Text = ""
	assistant.Reasoning = ""
	assistant.ToolCalls = nil
	assistant.Completed = true
	assistant.Meta = MessageMeta{ID: "a1", SessionID: "s1", CreatedAt: 1, UpdatedAt: 1}

	msgs := []Message{
		&UserMessage{
			Role:      "user",
			Content:   "hello",
			Timestamp: UnixMilliToTime(1),
			Meta:      MessageMeta{ID: "u1", SessionID: "s1", CreatedAt: 1, UpdatedAt: 1},
		},
		assistant,
		&ToolResultMessage{
			Role:       "toolResult",
			ToolCallID: "call-1",
			ToolName:   "read",
			Content:    "ok",
			Timestamp:  UnixMilliToTime(1),
			Meta:       MessageMeta{ID: "t1", SessionID: "s1", CreatedAt: 1, UpdatedAt: 1},
		},
	}

	got := SanitizeHistory(msgs)
	if len(got) != 3 {
		t.Fatalf("len(got)=%d want 3", len(got))
	}

	am, ok := got[1].(*AssistantMessage)
	if !ok {
		t.Fatalf("message[1] type=%T want *AssistantMessage", got[1])
	}
	if am.Text != "done" {
		t.Fatalf("assistant text=%q want %q", am.Text, "done")
	}
	if am.Reasoning != "thinking" {
		t.Fatalf("assistant reasoning=%q want %q", am.Reasoning, "thinking")
	}
	if len(am.ToolCalls) != 1 {
		t.Fatalf("assistant tool calls=%d want 1", len(am.ToolCalls))
	}
	if am.ToolCalls[0].ID != "call-1" || am.ToolCalls[0].Name != "read" {
		t.Fatalf("assistant tool call=%+v", am.ToolCalls[0])
	}
}

func TestSanitizeHistory_PreservesAssistantTextFromStructuredContent(t *testing.T) {
	t.Parallel()

	assistant := NewAssistantMessage("hello from content", "", nil, time.Now())
	assistant.Text = ""
	assistant.Completed = true
	assistant.Meta = MessageMeta{ID: "a1", SessionID: "s1", CreatedAt: 1, UpdatedAt: 1}

	got := SanitizeHistory([]Message{assistant})
	if len(got) != 1 {
		t.Fatalf("len(got)=%d want 1", len(got))
	}

	am, ok := got[0].(*AssistantMessage)
	if !ok {
		t.Fatalf("message[0] type=%T want *AssistantMessage", got[0])
	}
	if am.Text != "hello from content" {
		t.Fatalf("assistant text=%q want %q", am.Text, "hello from content")
	}
}
