package chat

import (
	"testing"

	"github.com/francescoalemanno/raijin-mono/internal/message"
)

func TestIsValidCompactionCutIndex_BijectiveOnBothSides(t *testing.T) {
	msgs := []message.Message{
		{Role: message.User, Parts: []message.ContentPart{message.TextContent{Text: "q"}}},
		{Role: message.Assistant, Parts: []message.ContentPart{message.ToolCall{ID: "call-1", Name: "tool"}}},
		{Role: message.Tool, Parts: []message.ContentPart{message.ToolResult{ToolCallID: "call-1", Name: "tool", Content: "ok"}}},
		{Role: message.Assistant, Parts: []message.ContentPart{message.TextContent{Text: "a"}}},
		{Role: message.Assistant, Parts: []message.ContentPart{message.ToolCall{ID: "call-2", Name: "tool"}}},
		{Role: message.Tool, Parts: []message.ContentPart{message.ToolResult{ToolCallID: "call-2", Name: "tool", Content: "ok"}}},
	}

	if !isValidCompactionCutIndex(msgs, 3) {
		t.Fatalf("expected cut at index 3 to be valid")
	}
}

func TestIsValidCompactionCutIndex_RejectsCrossBoundaryToolPair(t *testing.T) {
	msgs := []message.Message{
		{Role: message.User, Parts: []message.ContentPart{message.TextContent{Text: "q"}}},
		{Role: message.Assistant, Parts: []message.ContentPart{message.ToolCall{ID: "call-1", Name: "tool"}}},
		{Role: message.Tool, Parts: []message.ContentPart{message.ToolResult{ToolCallID: "call-1", Name: "tool", Content: "ok"}}},
		{Role: message.Assistant, Parts: []message.ContentPart{message.TextContent{Text: "a"}}},
	}

	if isValidCompactionCutIndex(msgs, 2) {
		t.Fatalf("expected cut between tool call and tool result to be invalid")
	}
}

func TestIsValidCompactionCutIndex_RejectsDuplicateToolCallIDs(t *testing.T) {
	msgs := []message.Message{
		{Role: message.User, Parts: []message.ContentPart{message.TextContent{Text: "q"}}},
		{Role: message.Assistant, Parts: []message.ContentPart{message.ToolCall{ID: "dup", Name: "tool"}}},
		{Role: message.Tool, Parts: []message.ContentPart{message.ToolResult{ToolCallID: "dup", Name: "tool", Content: "ok"}}},
		{Role: message.Assistant, Parts: []message.ContentPart{message.ToolCall{ID: "dup", Name: "tool"}}},
		{Role: message.Tool, Parts: []message.ContentPart{message.ToolResult{ToolCallID: "dup", Name: "tool", Content: "ok"}}},
	}

	if isValidCompactionCutIndex(msgs, 1) {
		t.Fatalf("expected cut to be invalid when tool call IDs are duplicated")
	}
}

func TestIsValidCompactionCutIndex_RejectsDuplicateToolResultIDs(t *testing.T) {
	msgs := []message.Message{
		{Role: message.User, Parts: []message.ContentPart{message.TextContent{Text: "q"}}},
		{Role: message.Assistant, Parts: []message.ContentPart{message.TextContent{Text: "a"}}},
		{Role: message.Assistant, Parts: []message.ContentPart{message.ToolCall{ID: "call-1", Name: "tool"}}},
		{Role: message.Assistant, Parts: []message.ContentPart{message.ToolCall{ID: "call-2", Name: "tool"}}},
		{Role: message.Tool, Parts: []message.ContentPart{message.ToolResult{ToolCallID: "call-1", Name: "tool", Content: "ok"}}},
		{Role: message.Tool, Parts: []message.ContentPart{message.ToolResult{ToolCallID: "call-1", Name: "tool", Content: "ok-again"}}},
	}

	if isValidCompactionCutIndex(msgs, 2) {
		t.Fatalf("expected cut to be invalid when tool result IDs are duplicated")
	}
}
