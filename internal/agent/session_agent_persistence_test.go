package agent

import (
	"context"
	"testing"

	libagent "github.com/francescoalemanno/raijin-mono/libagent"
)

func newRunStateForTest(msgSvc libagent.MessageService) *runState {
	ag := &SessionAgent{messages: msgSvc}
	return &runState{
		agent: ag,
		call:  SessionAgentCall{SessionID: "s1"},
		model: libagent.RuntimeModel{ModelCfg: libagent.ModelConfig{
			Provider: "mock",
			Model:    "mock",
		}},
		genCtx: context.Background(),
	}
}

func TestRunState_DoesNotPersistAssistantBeforeMessageEnd(t *testing.T) {
	msgSvc := libagent.NewInMemoryMessageService()
	rs := newRunStateForTest(msgSvc)

	if err := rs.handleEvent(context.Background(), libagent.AgentEvent{
		Type:    libagent.AgentEventTypeMessageStart,
		Message: &libagent.AssistantMessage{},
	}); err != nil {
		t.Fatalf("message start: %v", err)
	}
	if err := rs.handleEvent(context.Background(), libagent.AgentEvent{
		Type:    libagent.AgentEventTypeMessageUpdate,
		Message: &libagent.AssistantMessage{},
		Delta:   &libagent.StreamDelta{Type: "tool_input_start", ID: "call-1", ToolName: "read"},
	}); err != nil {
		t.Fatalf("tool_input_start: %v", err)
	}

	msgs, err := msgSvc.List(context.Background(), "s1")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(msgs) != 0 {
		t.Fatalf("persisted messages=%d want 0", len(msgs))
	}
}

func TestRunState_PersistsAssistantOnlyAtMessageEnd(t *testing.T) {
	msgSvc := libagent.NewInMemoryMessageService()
	rs := newRunStateForTest(msgSvc)

	if err := rs.handleEvent(context.Background(), libagent.AgentEvent{
		Type:    libagent.AgentEventTypeMessageStart,
		Message: &libagent.AssistantMessage{},
	}); err != nil {
		t.Fatalf("message start: %v", err)
	}
	if err := rs.handleEvent(context.Background(), libagent.AgentEvent{
		Type:    libagent.AgentEventTypeMessageUpdate,
		Message: &libagent.AssistantMessage{},
		Delta:   &libagent.StreamDelta{Type: "text_delta", Delta: "hello"},
	}); err != nil {
		t.Fatalf("text_delta: %v", err)
	}
	if err := rs.handleEvent(context.Background(), libagent.AgentEvent{
		Type: libagent.AgentEventTypeMessageEnd,
		Message: &libagent.AssistantMessage{
			FinishReason: "stop",
		},
	}); err != nil {
		t.Fatalf("message end: %v", err)
	}

	msgs, err := msgSvc.List(context.Background(), "s1")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(msgs) != 1 {
		t.Fatalf("persisted messages=%d want 1", len(msgs))
	}
	am, ok := msgs[0].(*libagent.AssistantMessage)
	if !ok {
		t.Fatalf("message type=%T want *libagent.AssistantMessage", msgs[0])
	}
	if am.GetRole() != "assistant" {
		t.Fatalf("role=%s want assistant", am.GetRole())
	}
	if am.Text != "hello" {
		t.Fatalf("assistant text=%q want %q", am.Text, "hello")
	}
}
