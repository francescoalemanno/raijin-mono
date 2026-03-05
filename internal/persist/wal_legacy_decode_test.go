package persist

import (
	"encoding/json"
	"testing"

	libagent "github.com/francescoalemanno/raijin-mono/libagent"
)

func TestWalMsgToMessage_LegacyAssistantFinishPreserved(t *testing.T) {
	wm := walMessage{
		ID:        "a1",
		Role:      "assistant",
		SessionID: "s1",
		Parts: []json.RawMessage{
			json.RawMessage(`{"kind":"text","text":{"text":"partial"}}`),
			json.RawMessage(`{"t":"finish","d":{"reason":"error","time":123,"message":"boom","details":"trace"}}`),
		},
		CreatedAt: 10,
		UpdatedAt: 11,
	}

	m, ok := walMsgToMessage(wm)
	if !ok {
		t.Fatal("expected decode success")
	}
	am, ok := m.(*libagent.AssistantMessage)
	if !ok {
		t.Fatalf("type=%T want *libagent.AssistantMessage", m)
	}
	if !am.Completed {
		t.Fatal("expected completed=true from legacy finish part")
	}
	if am.CompleteReason != "error" {
		t.Fatalf("reason=%q want error", am.CompleteReason)
	}
	if am.CompleteMessage != "boom" || am.CompleteDetails != "trace" {
		t.Fatalf("message/details=(%q,%q) want (boom,trace)", am.CompleteMessage, am.CompleteDetails)
	}
}

func TestWalMsgToMessage_LegacyAssistantWithoutFinishNotAutoCompleted(t *testing.T) {
	wm := walMessage{
		ID:        "a2",
		Role:      "assistant",
		SessionID: "s1",
		Parts: []json.RawMessage{
			json.RawMessage(`{"kind":"text","text":{"text":"streaming"}}`),
		},
		CreatedAt: 10,
		UpdatedAt: 11,
	}

	m, ok := walMsgToMessage(wm)
	if !ok {
		t.Fatal("expected decode success")
	}
	am, ok := m.(*libagent.AssistantMessage)
	if !ok {
		t.Fatalf("type=%T want *libagent.AssistantMessage", m)
	}
	if am.Completed {
		t.Fatal("expected completed=false without explicit completion/finish")
	}
}

func TestWalMsgToMessage_LegacyUserAndToolDecode(t *testing.T) {
	uwm := walMessage{
		ID:        "u1",
		Role:      "user",
		SessionID: "s1",
		Parts: []json.RawMessage{
			json.RawMessage(`{"kind":"text","text":{"text":"hello"}}`),
		},
	}
	um, ok := walMsgToMessage(uwm)
	if !ok {
		t.Fatal("expected user decode success")
	}
	if _, ok := um.(*libagent.UserMessage); !ok {
		t.Fatalf("user type=%T", um)
	}

	twm := walMessage{
		ID:        "t1",
		Role:      "tool",
		SessionID: "s1",
		Parts: []json.RawMessage{
			json.RawMessage(`{"kind":"tool_result","tool_result":{"tool_call_id":"c1","name":"read","content":"ok"}}`),
		},
	}
	tm, ok := walMsgToMessage(twm)
	if !ok {
		t.Fatal("expected tool decode success")
	}
	tr, ok := tm.(*libagent.ToolResultMessage)
	if !ok {
		t.Fatalf("tool type=%T", tm)
	}
	if tr.ToolCallID != "c1" || tr.ToolName != "read" || tr.Content != "ok" {
		t.Fatalf("unexpected tool result decode: %+v", tr)
	}
}
