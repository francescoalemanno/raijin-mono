package agent

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/francescoalemanno/raijin-mono/internal/persist"
	libagent "github.com/francescoalemanno/raijin-mono/libagent"
)

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
