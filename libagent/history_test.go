package libagent

import (
	"testing"
	"time"

	"charm.land/fantasy"
	"charm.land/fantasy/providers/openai"
)

func TestSanitizeHistory_PreservesAssistantStructuredContent(t *testing.T) {
	t.Parallel()

	assistant := NewAssistantMessage("done", "thinking", []ToolCallItem{{
		ID:    "call-1",
		Name:  "read",
		Input: `{"path":"README.md"}`,
	}}, time.Now())
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
	if AssistantText(am) != "done" {
		t.Fatalf("assistant text=%q want %q", AssistantText(am), "done")
	}
	if AssistantReasoning(am) != "thinking" {
		t.Fatalf("assistant reasoning=%q want %q", AssistantReasoning(am), "thinking")
	}
	calls := AssistantToolCalls(am)
	if len(calls) != 1 {
		t.Fatalf("assistant tool calls=%d want 1", len(calls))
	}
	if calls[0].ID != "call-1" || calls[0].Name != "read" {
		t.Fatalf("assistant tool call=%+v", calls[0])
	}
}

func TestSanitizeHistory_PreservesAssistantTextFromStructuredContent(t *testing.T) {
	t.Parallel()

	assistant := NewAssistantMessage("hello from content", "", nil, time.Now())
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
	if AssistantText(am) != "hello from content" {
		t.Fatalf("assistant text=%q want %q", AssistantText(am), "hello from content")
	}
}

func TestSanitizeHistory_PreservesMetadataOnlyReasoningContent(t *testing.T) {
	t.Parallel()

	encrypted := "encrypted-signature"
	assistant := &AssistantMessage{
		Role:      "assistant",
		Completed: true,
		Content: fantasy.ResponseContent{
			fantasy.ReasoningContent{
				Text: "",
				ProviderMetadata: fantasy.ProviderMetadata{
					openai.Name: &openai.ResponsesReasoningMetadata{
						ItemID:           "reasoning-item-1",
						EncryptedContent: &encrypted,
						Summary:          []string{"summary"},
					},
				},
			},
		},
		Timestamp: time.Now(),
	}

	got := SanitizeHistory([]Message{assistant})
	if len(got) != 1 {
		t.Fatalf("len(got)=%d want 1", len(got))
	}

	am, ok := got[0].(*AssistantMessage)
	if !ok {
		t.Fatalf("message[0] type=%T want *AssistantMessage", got[0])
	}
	reasoning := am.Content.Reasoning()
	if len(reasoning) != 1 {
		t.Fatalf("reasoning parts=%d want 1", len(reasoning))
	}
	md := openai.GetReasoningMetadata(fantasy.ProviderOptions(reasoning[0].ProviderMetadata))
	if md == nil || md.EncryptedContent == nil {
		t.Fatalf("reasoning metadata missing after sanitize: %#v", md)
	}
	if got := *md.EncryptedContent; got != encrypted {
		t.Fatalf("encrypted content=%q want %q", got, encrypted)
	}
}

func TestHasBijectiveToolCoupling_AllowsDuplicateIDsWhenBalanced(t *testing.T) {
	t.Parallel()

	msgs := []Message{
		NewAssistantMessage("", "", []ToolCallItem{{ID: "dup", Name: "bash"}}, time.Now()),
		&ToolResultMessage{Role: "toolResult", ToolCallID: "dup", ToolName: "bash", Content: "ok"},
		NewAssistantMessage("", "", []ToolCallItem{{ID: "dup", Name: "bash"}}, time.Now()),
		&ToolResultMessage{Role: "toolResult", ToolCallID: "dup", ToolName: "bash", Content: "ok"},
	}
	msgs[0].(*AssistantMessage).Completed = true
	msgs[2].(*AssistantMessage).Completed = true

	if !HasBijectiveToolCoupling(msgs) {
		t.Fatalf("expected balanced duplicate tool-call IDs to be valid")
	}
}

func TestHasBijectiveToolCoupling_RejectsInterveningUserBeforeToolResult(t *testing.T) {
	t.Parallel()

	msgs := []Message{
		NewAssistantMessage("", "", []ToolCallItem{{ID: "bash:7", Name: "bash"}}, time.Now()),
		&UserMessage{Role: "user", Content: "go on"},
		&ToolResultMessage{Role: "toolResult", ToolCallID: "bash:7", ToolName: "bash", Content: "ok"},
	}
	msgs[0].(*AssistantMessage).Completed = true

	if HasBijectiveToolCoupling(msgs) {
		t.Fatalf("expected user message between tool call and tool result to be invalid")
	}
}

func TestHasBijectiveToolCoupling_RejectsUnbalancedOrder(t *testing.T) {
	t.Parallel()

	msgs := []Message{
		&ToolResultMessage{Role: "toolResult", ToolCallID: "call-1", ToolName: "bash", Content: "ok"},
		NewAssistantMessage("", "", []ToolCallItem{{ID: "call-1", Name: "bash"}}, time.Now()),
	}
	msgs[1].(*AssistantMessage).Completed = true

	if HasBijectiveToolCoupling(msgs) {
		t.Fatalf("expected tool result before matching tool call to be invalid")
	}
}

func TestHasBijectiveToolCoupling_RejectsUnclosedCalls(t *testing.T) {
	t.Parallel()

	msgs := []Message{
		NewAssistantMessage("", "", []ToolCallItem{{ID: "call-1", Name: "bash"}}, time.Now()),
	}
	msgs[0].(*AssistantMessage).Completed = true

	if HasBijectiveToolCoupling(msgs) {
		t.Fatalf("expected unmatched tool call to be invalid")
	}
}

func TestHasBijectiveToolCoupling_RejectsSameIDDifferentToolName(t *testing.T) {
	t.Parallel()

	msgs := []Message{
		NewAssistantMessage("", "", []ToolCallItem{{ID: "call-1", Name: "read", Input: `{"path":"a.txt"}`}}, time.Now()),
		&ToolResultMessage{Role: "toolResult", ToolCallID: "call-1", ToolName: "bash", Content: "ok"},
	}
	msgs[0].(*AssistantMessage).Completed = true

	if HasBijectiveToolCoupling(msgs) {
		t.Fatalf("expected mismatched tool name for same tool-call ID to be invalid")
	}
}

func TestHasBijectiveToolCoupling_RejectsSeparatedDuplicateIDsEvenWhenEventuallyBalanced(t *testing.T) {
	t.Parallel()

	msgs := []Message{
		NewAssistantMessage("", "", []ToolCallItem{{ID: "dup", Name: "bash", Input: `{"command":"ls"}`}}, time.Now()),
		NewAssistantMessage("", "", []ToolCallItem{{ID: "dup", Name: "bash", Input: `{"command":"pwd"}`}}, time.Now()),
		&ToolResultMessage{Role: "toolResult", ToolCallID: "dup", ToolName: "bash", Content: "ok"},
		&ToolResultMessage{Role: "toolResult", ToolCallID: "dup", ToolName: "bash", Content: "ok"},
	}
	msgs[0].(*AssistantMessage).Completed = true
	msgs[1].(*AssistantMessage).Completed = true

	if HasBijectiveToolCoupling(msgs) {
		t.Fatalf("expected separated duplicate IDs to be invalid when tool results are not contiguous")
	}
}

func TestSanitizeHistory_PreservesBalancedDuplicateToolCallIDs(t *testing.T) {
	t.Parallel()

	msgs := []Message{
		&UserMessage{Role: "user", Content: "start", Timestamp: UnixMilliToTime(1), Meta: MessageMeta{ID: "u1"}},
		NewAssistantMessage("", "", []ToolCallItem{{ID: "dup", Name: "bash", Input: `{"command":"one"}`}}, time.Now()),
		&ToolResultMessage{Role: "toolResult", ToolCallID: "dup", ToolName: "bash", Content: "ok-1", Timestamp: UnixMilliToTime(2), Meta: MessageMeta{ID: "t1"}},
		NewAssistantMessage("", "", []ToolCallItem{{ID: "dup", Name: "bash", Input: `{"command":"two"}`}}, time.Now()),
		&ToolResultMessage{Role: "toolResult", ToolCallID: "dup", ToolName: "bash", Content: "ok-2", Timestamp: UnixMilliToTime(3), Meta: MessageMeta{ID: "t2"}},
	}
	msgs[1].(*AssistantMessage).Completed = true
	msgs[1].(*AssistantMessage).Meta = MessageMeta{ID: "a1"}
	msgs[3].(*AssistantMessage).Completed = true
	msgs[3].(*AssistantMessage).Meta = MessageMeta{ID: "a2"}

	got := SanitizeHistory(msgs)
	if len(got) != 5 {
		t.Fatalf("len(got)=%d want 5", len(got))
	}

	a1, ok := got[1].(*AssistantMessage)
	if !ok || len(AssistantToolCalls(a1)) != 1 {
		t.Fatalf("assistant[1] coupling lost: %#v", got[1])
	}
	a2, ok := got[3].(*AssistantMessage)
	if !ok || len(AssistantToolCalls(a2)) != 1 {
		t.Fatalf("assistant[3] coupling lost: %#v", got[3])
	}
}

func TestSanitizeHistory_DropsUnresolvedAssistantWhenUserInterruptsToolTurn(t *testing.T) {
	t.Parallel()

	msgs := []Message{
		&UserMessage{Role: "user", Content: "start", Timestamp: UnixMilliToTime(1), Meta: MessageMeta{ID: "u1"}},
		NewAssistantMessage("", "", []ToolCallItem{{ID: "bash:7", Name: "bash", Input: `{"command":"install"}`}}, time.Now()),
		&UserMessage{Role: "user", Content: "the server is off", Timestamp: UnixMilliToTime(2), Meta: MessageMeta{ID: "u2"}},
		NewAssistantMessage("Let me restart it", "", []ToolCallItem{{ID: "bash:7", Name: "bash", Input: `{"command":"nohup server"}`}}, time.Now()),
		&ToolResultMessage{Role: "toolResult", ToolCallID: "bash:7", ToolName: "bash", Content: "200", Timestamp: UnixMilliToTime(3), Meta: MessageMeta{ID: "t1"}},
	}
	msgs[1].(*AssistantMessage).Completed = true
	msgs[1].(*AssistantMessage).Meta = MessageMeta{ID: "a1"}
	msgs[3].(*AssistantMessage).Completed = true
	msgs[3].(*AssistantMessage).Meta = MessageMeta{ID: "a2"}

	got := SanitizeHistory(msgs)
	if len(got) != 4 {
		t.Fatalf("len(got)=%d want 4", len(got))
	}
	if id := MessageID(got[1]); id != "u2" {
		t.Fatalf("message[1] id=%q want %q", id, "u2")
	}
	am, ok := got[2].(*AssistantMessage)
	if !ok {
		t.Fatalf("message[2] type=%T want *AssistantMessage", got[2])
	}
	if calls := AssistantToolCalls(am); len(calls) != 1 || calls[0].ID != "bash:7" {
		t.Fatalf("assistant calls=%+v", calls)
	}
	trm, ok := got[3].(*ToolResultMessage)
	if !ok {
		t.Fatalf("message[3] type=%T want *ToolResultMessage", got[3])
	}
	if trm.ToolCallID != "bash:7" {
		t.Fatalf("tool result id=%q want %q", trm.ToolCallID, "bash:7")
	}
}

func TestSanitizeHistory_DropsUnmatchedDuplicateTail(t *testing.T) {
	t.Parallel()

	msgs := []Message{
		NewAssistantMessage("", "", []ToolCallItem{{ID: "dup", Name: "bash"}}, time.Now()),
		&ToolResultMessage{Role: "toolResult", ToolCallID: "dup", ToolName: "bash", Content: "ok-1"},
		NewAssistantMessage("", "", []ToolCallItem{{ID: "dup", Name: "bash"}}, time.Now()),
	}
	msgs[0].(*AssistantMessage).Completed = true
	msgs[2].(*AssistantMessage).Completed = true

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
		NewAssistantMessage("", "", []ToolCallItem{{ID: "call-1", Name: "read", Input: `{"path":"a.txt"}`}}, time.Now()),
		&ToolResultMessage{Role: "toolResult", ToolCallID: "call-1", ToolName: "bash", Content: "ok"},
	}
	msgs[0].(*AssistantMessage).Completed = true

	got := SanitizeHistory(msgs)
	if len(got) != 0 {
		t.Fatalf("len(got)=%d want 0", len(got))
	}
}
