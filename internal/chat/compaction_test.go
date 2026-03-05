package chat

import (
	"testing"

	libagent "github.com/francescoalemanno/raijin-mono/libagent"
)

func TestIsValidCompactionCutIndex_BijectiveOnBothSides(t *testing.T) {
	msgs := []libagent.Message{
		&libagent.UserMessage{Role: "user", Content: "q"},
		&libagent.AssistantMessage{Role: "assistant", Completed: true, ToolCalls: []libagent.ToolCallItem{{ID: "call-1", Name: "tool"}}},
		&libagent.ToolResultMessage{Role: "toolResult", ToolCallID: "call-1", ToolName: "tool", Content: "ok"},
		&libagent.AssistantMessage{Role: "assistant", Completed: true, Text: "a"},
		&libagent.AssistantMessage{Role: "assistant", Completed: true, ToolCalls: []libagent.ToolCallItem{{ID: "call-2", Name: "tool"}}},
		&libagent.ToolResultMessage{Role: "toolResult", ToolCallID: "call-2", ToolName: "tool", Content: "ok"},
	}

	if !isValidCompactionCutIndex(msgs, 3) {
		t.Fatalf("expected cut at index 3 to be valid")
	}
}

func TestIsValidCompactionCutIndex_RejectsCrossBoundaryToolPair(t *testing.T) {
	msgs := []libagent.Message{
		&libagent.UserMessage{Role: "user", Content: "q"},
		&libagent.AssistantMessage{Role: "assistant", Completed: true, ToolCalls: []libagent.ToolCallItem{{ID: "call-1", Name: "tool"}}},
		&libagent.ToolResultMessage{Role: "toolResult", ToolCallID: "call-1", ToolName: "tool", Content: "ok"},
		&libagent.AssistantMessage{Role: "assistant", Completed: true, Text: "a"},
	}

	if isValidCompactionCutIndex(msgs, 2) {
		t.Fatalf("expected cut between tool call and tool result to be invalid")
	}
}

func TestIsValidCompactionCutIndex_RejectsDuplicateToolCallIDs(t *testing.T) {
	msgs := []libagent.Message{
		&libagent.UserMessage{Role: "user", Content: "q"},
		&libagent.AssistantMessage{Role: "assistant", Completed: true, ToolCalls: []libagent.ToolCallItem{{ID: "dup", Name: "tool"}}},
		&libagent.ToolResultMessage{Role: "toolResult", ToolCallID: "dup", ToolName: "tool", Content: "ok"},
		&libagent.AssistantMessage{Role: "assistant", Completed: true, ToolCalls: []libagent.ToolCallItem{{ID: "dup", Name: "tool"}}},
		&libagent.ToolResultMessage{Role: "toolResult", ToolCallID: "dup", ToolName: "tool", Content: "ok"},
	}

	if isValidCompactionCutIndex(msgs, 1) {
		t.Fatalf("expected cut to be invalid when tool call IDs are duplicated")
	}
}

func TestIsValidCompactionCutIndex_RejectsDuplicateToolResultIDs(t *testing.T) {
	msgs := []libagent.Message{
		&libagent.UserMessage{Role: "user", Content: "q"},
		&libagent.AssistantMessage{Role: "assistant", Completed: true, Text: "a"},
		&libagent.AssistantMessage{Role: "assistant", Completed: true, ToolCalls: []libagent.ToolCallItem{{ID: "call-1", Name: "tool"}}},
		&libagent.AssistantMessage{Role: "assistant", Completed: true, ToolCalls: []libagent.ToolCallItem{{ID: "call-2", Name: "tool"}}},
		&libagent.ToolResultMessage{Role: "toolResult", ToolCallID: "call-1", ToolName: "tool", Content: "ok"},
		&libagent.ToolResultMessage{Role: "toolResult", ToolCallID: "call-1", ToolName: "tool", Content: "ok-again"},
	}

	if isValidCompactionCutIndex(msgs, 2) {
		t.Fatalf("expected cut to be invalid when tool result IDs are duplicated")
	}
}
