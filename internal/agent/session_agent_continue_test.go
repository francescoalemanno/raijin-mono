package agent

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/francescoalemanno/raijin-mono/internal/persist"
	libagent "github.com/francescoalemanno/raijin-mono/libagent"
)

func continueTestAssistant(calls []libagent.ToolCallItem) *libagent.AssistantMessage {
	am := libagent.NewAssistantMessage("", "", calls, time.Now())
	am.Completed = true
	return am
}

func TestSessionAgentContinueReturnsImmediateErrorWithoutHanging(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	store, err := persist.OpenStore()
	if err != nil {
		t.Fatalf("OpenStore: %v", err)
	}
	sess, err := store.CreateEphemeral()
	if err != nil {
		t.Fatalf("CreateEphemeral: %v", err)
	}

	finalAssistant := libagent.NewAssistantMessage("done", "", nil, time.UnixMilli(1))
	finalAssistant.Completed = true
	if _, err := store.Messages().Create(context.Background(), sess.ID, finalAssistant); err != nil {
		t.Fatalf("create final assistant: %v", err)
	}

	ag, err := NewSessionAgentFromConfig(libagent.RuntimeModel{
		Model: &libagent.StaticTextModel{Response: "unused"},
		ModelCfg: libagent.ModelConfig{
			Provider: "mock",
			Model:    "mock",
		},
	}, store.Messages(), store)
	if err != nil {
		t.Fatalf("NewSessionAgentFromConfig: %v", err)
	}

	errCh := make(chan error, 1)
	go func() {
		errCh <- ag.Continue(context.Background(), SessionAgentCall{SessionID: sess.ID})
	}()

	select {
	case err := <-errCh:
		if err == nil {
			t.Fatal("Continue returned nil error, want assistant-last failure")
		}
		if !strings.Contains(err.Error(), "cannot continue from message role: assistant") {
			t.Fatalf("Continue error = %v, want assistant-last failure", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Continue hung instead of returning the immediate assistant-last error")
	}
}

func TestSessionAgentContinueSanitizesDanglingAssistantToolCallHistory(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	store, err := persist.OpenStore()
	if err != nil {
		t.Fatalf("OpenStore: %v", err)
	}
	sess, err := store.CreateEphemeral()
	if err != nil {
		t.Fatalf("CreateEphemeral: %v", err)
	}

	ctx := context.Background()
	msgs := store.Messages()
	if _, err := msgs.Create(ctx, sess.ID, &libagent.UserMessage{
		Role:    "user",
		Content: "start",
	}); err != nil {
		t.Fatalf("create user message: %v", err)
	}
	if _, err := msgs.Create(ctx, sess.ID, continueTestAssistant([]libagent.ToolCallItem{{
		ID:    "call-1",
		Name:  "read",
		Input: `{"path":"a.txt"}`,
	}})); err != nil {
		t.Fatalf("create assistant tool call: %v", err)
	}
	if _, err := msgs.Create(ctx, sess.ID, &libagent.ToolResultMessage{
		Role:       "toolResult",
		ToolCallID: "call-1",
		ToolName:   "read",
		Content:    "file contents",
	}); err != nil {
		t.Fatalf("create tool result: %v", err)
	}
	dangling, err := msgs.Create(ctx, sess.ID, continueTestAssistant([]libagent.ToolCallItem{{
		ID:    "call-2",
		Name:  "bash",
		Input: `{"command":"pwd"}`,
	}}))
	if err != nil {
		t.Fatalf("create dangling assistant tool call: %v", err)
	}

	model := &libagent.StaticTextModel{Response: "done"}
	ag, err := NewSessionAgentFromConfig(libagent.RuntimeModel{
		Model: model,
		ModelCfg: libagent.ModelConfig{
			Provider: "mock",
			Model:    "mock",
		},
	}, store.Messages(), store)
	if err != nil {
		t.Fatalf("NewSessionAgentFromConfig: %v", err)
	}

	if err := ag.Continue(ctx, SessionAgentCall{SessionID: sess.ID}); err != nil {
		t.Fatalf("Continue: %v", err)
	}
	if strings.Contains(model.PromptJSON, "call-2") {
		t.Fatalf("provider prompt still contains dangling tool call: %s", model.PromptJSON)
	}

	got, err := store.Messages().List(ctx, sess.ID)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 4 {
		t.Fatalf("len(got)=%d want 4", len(got))
	}
	if text := libagent.AssistantText(got[3].(*libagent.AssistantMessage)); text != "done" {
		t.Fatalf("final assistant text = %q, want %q", text, "done")
	}
	for _, msg := range got {
		if libagent.MessageID(msg) == libagent.MessageID(dangling) {
			t.Fatalf("dangling assistant tool-call should have been removed from active history")
		}
	}
}
