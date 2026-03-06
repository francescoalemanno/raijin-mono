package chat

import (
	"testing"

	libagent "github.com/francescoalemanno/raijin-mono/libagent"
)

func TestIsValidCompactionCutIndex_BijectiveOnBothSides(t *testing.T) {
	msgs := []libagent.Message{
		&libagent.UserMessage{Role: "user", Content: "q"},
		libagent.NewAssistantMessage("", "", []libagent.ToolCallItem{{ID: "call-1", Name: "tool"}}, libagent.UnixMilliToTime(1)),
		&libagent.ToolResultMessage{Role: "toolResult", ToolCallID: "call-1", ToolName: "tool", Content: "ok"},
		libagent.NewAssistantMessage("a", "", nil, libagent.UnixMilliToTime(1)),
		libagent.NewAssistantMessage("", "", []libagent.ToolCallItem{{ID: "call-2", Name: "tool"}}, libagent.UnixMilliToTime(1)),
		&libagent.ToolResultMessage{Role: "toolResult", ToolCallID: "call-2", ToolName: "tool", Content: "ok"},
	}
	msgs[1].(*libagent.AssistantMessage).Completed = true
	msgs[3].(*libagent.AssistantMessage).Completed = true
	msgs[4].(*libagent.AssistantMessage).Completed = true

	if !isValidCompactionCutIndex(msgs, 3) {
		t.Fatalf("expected cut at index 3 to be valid")
	}
}

func TestIsValidCompactionCutIndex_RejectsCrossBoundaryToolPair(t *testing.T) {
	msgs := []libagent.Message{
		&libagent.UserMessage{Role: "user", Content: "q"},
		libagent.NewAssistantMessage("", "", []libagent.ToolCallItem{{ID: "call-1", Name: "tool"}}, libagent.UnixMilliToTime(1)),
		&libagent.ToolResultMessage{Role: "toolResult", ToolCallID: "call-1", ToolName: "tool", Content: "ok"},
		libagent.NewAssistantMessage("a", "", nil, libagent.UnixMilliToTime(1)),
	}
	msgs[1].(*libagent.AssistantMessage).Completed = true
	msgs[3].(*libagent.AssistantMessage).Completed = true

	if isValidCompactionCutIndex(msgs, 2) {
		t.Fatalf("expected cut between tool call and tool result to be invalid")
	}
}

func TestIsValidCompactionCutIndex_AllowsDuplicateToolCallIDsWhenBalanced(t *testing.T) {
	msgs := []libagent.Message{
		&libagent.UserMessage{Role: "user", Content: "q"},
		libagent.NewAssistantMessage("", "", []libagent.ToolCallItem{{ID: "dup", Name: "tool"}}, libagent.UnixMilliToTime(1)),
		&libagent.ToolResultMessage{Role: "toolResult", ToolCallID: "dup", ToolName: "tool", Content: "ok"},
		libagent.NewAssistantMessage("", "", []libagent.ToolCallItem{{ID: "dup", Name: "tool"}}, libagent.UnixMilliToTime(1)),
		&libagent.ToolResultMessage{Role: "toolResult", ToolCallID: "dup", ToolName: "tool", Content: "ok"},
	}
	msgs[1].(*libagent.AssistantMessage).Completed = true
	msgs[3].(*libagent.AssistantMessage).Completed = true

	if !isValidCompactionCutIndex(msgs, 1) {
		t.Fatalf("expected cut to be valid when duplicate tool call IDs are balanced")
	}
}

func TestIsValidCompactionCutIndex_RejectsDuplicateToolResultIDs(t *testing.T) {
	msgs := []libagent.Message{
		&libagent.UserMessage{Role: "user", Content: "q"},
		libagent.NewAssistantMessage("a", "", nil, libagent.UnixMilliToTime(1)),
		libagent.NewAssistantMessage("", "", []libagent.ToolCallItem{{ID: "call-1", Name: "tool"}}, libagent.UnixMilliToTime(1)),
		libagent.NewAssistantMessage("", "", []libagent.ToolCallItem{{ID: "call-2", Name: "tool"}}, libagent.UnixMilliToTime(1)),
		&libagent.ToolResultMessage{Role: "toolResult", ToolCallID: "call-1", ToolName: "tool", Content: "ok"},
		&libagent.ToolResultMessage{Role: "toolResult", ToolCallID: "call-1", ToolName: "tool", Content: "ok-again"},
	}
	msgs[1].(*libagent.AssistantMessage).Completed = true
	msgs[2].(*libagent.AssistantMessage).Completed = true
	msgs[3].(*libagent.AssistantMessage).Completed = true

	if isValidCompactionCutIndex(msgs, 2) {
		t.Fatalf("expected cut to be invalid when tool result IDs are duplicated")
	}
}
