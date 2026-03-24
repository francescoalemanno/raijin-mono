package agent

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/francescoalemanno/raijin-mono/internal/compaction"
	"github.com/francescoalemanno/raijin-mono/internal/persist"
	libagent "github.com/francescoalemanno/raijin-mono/libagent"
)

func TestAutoCompactTransformCompactsAndPersistsCheckpoint(t *testing.T) {
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
	userText := strings.Repeat("u", 1200)
	assistantText := strings.Repeat("a", 1200)
	for i := 0; i < 6; i++ {
		if _, err := store.Messages().Create(ctx, sess.ID, &libagent.UserMessage{
			Role:      "user",
			Content:   userText,
			Timestamp: time.Now(),
		}); err != nil {
			t.Fatalf("create user %d: %v", i, err)
		}
		msg := libagent.NewAssistantMessage(assistantText, "", nil, time.Now())
		msg.Completed = true
		if _, err := store.Messages().Create(ctx, sess.ID, msg); err != nil {
			t.Fatalf("create assistant %d: %v", i, err)
		}
	}

	runtimeModel := libagent.RuntimeModel{
		Model: &libagent.StaticTextModel{Response: "checkpoint"},
		ModelCfg: libagent.ModelConfig{
			Provider:      "mock",
			Model:         "mock",
			ContextWindow: 6000,
		},
	}
	ag, err := NewSessionAgentFromConfig(runtimeModel, store.Messages(), store)
	if err != nil {
		t.Fatalf("NewSessionAgentFromConfig: %v", err)
	}

	msgs, err := store.Messages().List(ctx, sess.ID)
	if err != nil {
		t.Fatalf("ListMessages before: %v", err)
	}

	transform := ag.autoCompactTransform(sess.ID, runtimeModel, newMessageIDIndex())
	compacted, err := transform(ctx, msgs)
	if err != nil {
		t.Fatalf("transform: %v", err)
	}
	first, ok := compacted[0].(*libagent.UserMessage)
	if !ok {
		t.Fatalf("first transformed message type = %T, want *libagent.UserMessage", compacted[0])
	}
	if !strings.HasPrefix(first.Content, compaction.CheckpointPrefix) {
		t.Fatalf("expected transformed context checkpoint, got %q", first.Content)
	}

	persisted, err := store.Messages().List(ctx, sess.ID)
	if err != nil {
		t.Fatalf("ListMessages after: %v", err)
	}
	firstPersisted, ok := persisted[0].(*libagent.UserMessage)
	if !ok {
		t.Fatalf("first persisted message type = %T, want *libagent.UserMessage", persisted[0])
	}
	if !strings.HasPrefix(firstPersisted.Content, compaction.CheckpointPrefix) {
		t.Fatalf("expected persisted context checkpoint, got %q", firstPersisted.Content)
	}

	replayItems, err := store.ListReplayItems(sess.ID)
	if err != nil {
		t.Fatalf("ListReplayItems: %v", err)
	}
	var phases []libagent.ContextCompactionPhase
	for _, item := range replayItems {
		if item.ContextCompaction == nil {
			continue
		}
		phases = append(phases, item.ContextCompaction.Phase)
	}
	if len(phases) != 2 {
		t.Fatalf("compaction replay event count = %d, want 2", len(phases))
	}
	if phases[0] != libagent.ContextCompactionPhaseStart || phases[1] != libagent.ContextCompactionPhaseEnd {
		t.Fatalf("compaction replay phases = %#v, want start/end", phases)
	}
}
