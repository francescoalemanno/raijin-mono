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

func TestHasBijectiveToolCoupling_AllowsDuplicateIDsWhenBalanced(t *testing.T) {
	t.Parallel()

	msgs := []Message{
		&AssistantMessage{Role: "assistant", ToolCalls: []ToolCallItem{{ID: "dup", Name: "bash"}}, Completed: true},
		&ToolResultMessage{Role: "toolResult", ToolCallID: "dup", ToolName: "bash", Content: "ok"},
		&AssistantMessage{Role: "assistant", ToolCalls: []ToolCallItem{{ID: "dup", Name: "bash"}}, Completed: true},
		&ToolResultMessage{Role: "toolResult", ToolCallID: "dup", ToolName: "bash", Content: "ok"},
	}

	if !HasBijectiveToolCoupling(msgs) {
		t.Fatalf("expected balanced duplicate tool-call IDs to be valid")
	}
}

func TestHasBijectiveToolCoupling_RejectsUnbalancedOrder(t *testing.T) {
	t.Parallel()

	msgs := []Message{
		&ToolResultMessage{Role: "toolResult", ToolCallID: "call-1", ToolName: "bash", Content: "ok"},
		&AssistantMessage{Role: "assistant", ToolCalls: []ToolCallItem{{ID: "call-1", Name: "bash"}}, Completed: true},
	}

	if HasBijectiveToolCoupling(msgs) {
		t.Fatalf("expected tool result before matching tool call to be invalid")
	}
}

func TestHasBijectiveToolCoupling_RejectsUnclosedCalls(t *testing.T) {
	t.Parallel()

	msgs := []Message{
		&AssistantMessage{Role: "assistant", ToolCalls: []ToolCallItem{{ID: "call-1", Name: "bash"}}, Completed: true},
	}

	if HasBijectiveToolCoupling(msgs) {
		t.Fatalf("expected unmatched tool call to be invalid")
	}
}

func TestHasBijectiveToolCoupling_RejectsSameIDDifferentToolName(t *testing.T) {
	t.Parallel()

	msgs := []Message{
		&AssistantMessage{Role: "assistant", ToolCalls: []ToolCallItem{{ID: "call-1", Name: "read", Input: `{"path":"a.txt"}`}}, Completed: true},
		&ToolResultMessage{Role: "toolResult", ToolCallID: "call-1", ToolName: "bash", Content: "ok"},
	}

	if HasBijectiveToolCoupling(msgs) {
		t.Fatalf("expected mismatched tool name for same tool-call ID to be invalid")
	}
}

func TestHasBijectiveToolCoupling_AllowsSameIDSameToolDifferentInputWhenBalanced(t *testing.T) {
	t.Parallel()

	msgs := []Message{
		&AssistantMessage{Role: "assistant", ToolCalls: []ToolCallItem{{ID: "dup", Name: "bash", Input: `{"command":"ls"}`}}, Completed: true},
		&AssistantMessage{Role: "assistant", ToolCalls: []ToolCallItem{{ID: "dup", Name: "bash", Input: `{"command":"pwd"}`}}, Completed: true},
		&ToolResultMessage{Role: "toolResult", ToolCallID: "dup", ToolName: "bash", Content: "ok"},
		&ToolResultMessage{Role: "toolResult", ToolCallID: "dup", ToolName: "bash", Content: "ok"},
	}

	if !HasBijectiveToolCoupling(msgs) {
		t.Fatalf("expected balanced duplicate IDs to be valid regardless of input differences")
	}
}

func TestSanitizeHistory_PreservesBalancedDuplicateToolCallIDs(t *testing.T) {
	t.Parallel()

	msgs := []Message{
		&UserMessage{Role: "user", Content: "start", Timestamp: UnixMilliToTime(1), Meta: MessageMeta{ID: "u1"}},
		&AssistantMessage{
			Role: "assistant",
			ToolCalls: []ToolCallItem{
				{ID: "dup", Name: "bash", Input: `{"command":"one"}`},
			},
			Completed: true,
			Meta:      MessageMeta{ID: "a1"},
		},
		&ToolResultMessage{Role: "toolResult", ToolCallID: "dup", ToolName: "bash", Content: "ok-1", Timestamp: UnixMilliToTime(2), Meta: MessageMeta{ID: "t1"}},
		&AssistantMessage{
			Role: "assistant",
			ToolCalls: []ToolCallItem{
				{ID: "dup", Name: "bash", Input: `{"command":"two"}`},
			},
			Completed: true,
			Meta:      MessageMeta{ID: "a2"},
		},
		&ToolResultMessage{Role: "toolResult", ToolCallID: "dup", ToolName: "bash", Content: "ok-2", Timestamp: UnixMilliToTime(3), Meta: MessageMeta{ID: "t2"}},
	}

	got := SanitizeHistory(msgs)
	if len(got) != 5 {
		t.Fatalf("len(got)=%d want 5", len(got))
	}

	a1, ok := got[1].(*AssistantMessage)
	if !ok || len(a1.ToolCalls) != 1 {
		t.Fatalf("assistant[1] coupling lost: %#v", got[1])
	}
	a2, ok := got[3].(*AssistantMessage)
	if !ok || len(a2.ToolCalls) != 1 {
		t.Fatalf("assistant[3] coupling lost: %#v", got[3])
	}
}

func TestSanitizeHistory_DropsUnmatchedDuplicateTail(t *testing.T) {
	t.Parallel()

	msgs := []Message{
		&AssistantMessage{
			Role:      "assistant",
			ToolCalls: []ToolCallItem{{ID: "dup", Name: "bash"}},
			Completed: true,
		},
		&ToolResultMessage{Role: "toolResult", ToolCallID: "dup", ToolName: "bash", Content: "ok-1"},
		&AssistantMessage{
			Role:      "assistant",
			ToolCalls: []ToolCallItem{{ID: "dup", Name: "bash"}},
			Completed: true,
		},
	}

	got := SanitizeHistory(msgs)
	if len(got) != 2 {
		t.Fatalf("len(got)=%d want 2", len(got))
	}
	if _, ok := got[0].(*AssistantMessage); !ok {
		t.Fatalf("message[0] type=%T want *AssistantMessage", got[0])
	}
	if _, ok := got[1].(*ToolResultMessage); !ok {
		t.Fatalf("message[1] type=%T want *ToolResultMessage", got[1])
	}
}

func TestSanitizeHistory_DropsMismatchedToolNameWithSameID(t *testing.T) {
	t.Parallel()

	msgs := []Message{
		&AssistantMessage{
			Role:      "assistant",
			ToolCalls: []ToolCallItem{{ID: "call-1", Name: "read", Input: `{"path":"a.txt"}`}},
			Completed: true,
		},
		&ToolResultMessage{Role: "toolResult", ToolCallID: "call-1", ToolName: "bash", Content: "ok"},
	}

	got := SanitizeHistory(msgs)
	if len(got) != 0 {
		t.Fatalf("len(got)=%d want 0", len(got))
	}
}
