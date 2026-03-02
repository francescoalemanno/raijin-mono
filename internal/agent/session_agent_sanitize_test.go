package agent

import (
	"testing"

	"github.com/francescoalemanno/raijin-mono/internal/message"
)

func TestSanitizeMessagesForModel_KeepsBijectiveToolPairs(t *testing.T) {
	msgs := []message.Message{
		{Role: message.User, Parts: []message.ContentPart{message.TextContent{Text: "q"}}},
		{Role: message.Assistant, Parts: []message.ContentPart{message.ToolCall{ID: "call-1", Name: "read", Input: `{"path":"a"}`}}},
		{Role: message.Tool, Parts: []message.ContentPart{message.ToolResult{ToolCallID: "call-1", Name: "read", Content: "ok"}}},
	}

	out := sanitizeMessagesForModel(msgs)
	if len(out) != len(msgs) {
		t.Fatalf("len(out)=%d want %d", len(out), len(msgs))
	}
	if len(out[1].ToolCalls()) != 1 {
		t.Fatalf("tool calls kept=%d want 1", len(out[1].ToolCalls()))
	}
	if len(out[2].ToolResults()) != 1 {
		t.Fatalf("tool results kept=%d want 1", len(out[2].ToolResults()))
	}
}

func TestSanitizeMessagesForModel_RemovesOrphansAndDuplicates(t *testing.T) {
	msgs := []message.Message{
		{Role: message.User, Parts: []message.ContentPart{message.TextContent{Text: "q"}}},
		// duplicate tool call id -> invalid
		{Role: message.Assistant, Parts: []message.ContentPart{
			message.ToolCall{ID: "dup", Name: "edit", Input: `{"path":"a"}`},
			message.ToolCall{ID: "dup", Name: "edit", Input: `{"path":"b"}`},
		}},
		// orphan result id -> invalid
		{Role: message.Tool, Parts: []message.ContentPart{
			message.ToolResult{ToolCallID: "orphan", Name: "edit", Content: "x"},
		}},
		// valid bijective pair should survive
		{Role: message.Assistant, Parts: []message.ContentPart{message.ToolCall{ID: "ok-1", Name: "read", Input: `{"path":"c"}`}}},
		{Role: message.Tool, Parts: []message.ContentPart{message.ToolResult{ToolCallID: "ok-1", Name: "read", Content: "ok"}}},
	}

	out := sanitizeMessagesForModel(msgs)

	if got := len(out[1].ToolCalls()); got != 0 {
		t.Fatalf("duplicate tool calls retained=%d want 0", got)
	}
	if got := len(out[2].ToolResults()); got != 0 {
		t.Fatalf("orphan tool results retained=%d want 0", got)
	}
	if got := len(out[3].ToolCalls()); got != 1 {
		t.Fatalf("valid tool calls retained=%d want 1", got)
	}
	if got := len(out[4].ToolResults()); got != 1 {
		t.Fatalf("valid tool results retained=%d want 1", got)
	}
}
