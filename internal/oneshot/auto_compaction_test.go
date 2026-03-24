package oneshot

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/francescoalemanno/raijin-mono/internal/persist"
	libagent "github.com/francescoalemanno/raijin-mono/libagent"
)

const compactionCheckpointPrefix = "[Context checkpoint created by /compact]"

func TestAutoCompactionWatchTriggersOnContextFill(t *testing.T) {
	t.Parallel()

	msgs := []libagent.Message{
		&libagent.UserMessage{Role: "user", Content: strings.Repeat("a", 2000)},
	}

	watch := newAutoCompactionWatch(msgs, 4000)
	if !watch.triggerByFill {
		t.Fatalf("expected context-fill trigger, got %#v", watch)
	}
	if watch.triggerByTokens {
		t.Fatalf("did not expect token trigger, got %#v", watch)
	}
	if !watch.shouldCompact() {
		t.Fatalf("expected shouldCompact to be true")
	}
}

func TestAutoCompactionWatchTriggersOnTokenThresholdWithoutContextWindow(t *testing.T) {
	t.Parallel()

	msgs := []libagent.Message{
		&libagent.UserMessage{Role: "user", Content: strings.Repeat("a", 600_000)},
		assistantMessage(strings.Repeat("b", 32)),
	}

	watch := newAutoCompactionWatch(msgs, 0)
	if watch.triggerByFill {
		t.Fatalf("did not expect context-fill trigger without context window, got %#v", watch)
	}
	if !watch.triggerByTokens {
		t.Fatalf("expected token trigger, got %#v", watch)
	}
	if !watch.shouldCompact() {
		t.Fatalf("expected shouldCompact to be true")
	}
}

func TestCompactionKeepRecentTokensTargetsTwentyPercentOfContextWindow(t *testing.T) {
	t.Parallel()

	got := compactionKeepRecentTokens(200_000, defaultCompactionReserveTokens)
	want := int64(37_600) // 20% of 200k minus the fixed estimator overhead.
	if got != want {
		t.Fatalf("keepRecent = %d, want %d", got, want)
	}
}

func TestMaybeAutoCompactSessionCompactsThresholdedSession(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	key := bindTestContext(t)

	store, err := persist.OpenStore()
	if err != nil {
		t.Fatalf("OpenStore: %v", err)
	}
	sessMeta, err := store.CreateEphemeral()
	if err != nil {
		t.Fatalf("CreateEphemeral: %v", err)
	}
	seedConversation(t, store.Messages(), sessMeta.ID, 6, 1200)
	bindSession(t, key, store, sessMeta)

	opts := Options{
		RuntimeModel: libagent.RuntimeModel{
			Model: &libagent.StaticTextModel{Response: "checkpoint"},
			ModelCfg: libagent.ModelConfig{
				Provider:      "mock",
				Model:         "mock",
				ContextWindow: 6000,
			},
		},
		ModelCfg: libagent.ModelConfig{
			Provider:      "mock",
			Model:         "mock",
			ContextWindow: 6000,
		},
	}

	sess, err := openSession(opts, false, false)
	if err != nil {
		t.Fatalf("openSession: %v", err)
	}

	var status bytes.Buffer
	compacted, err := maybeAutoCompactSession(context.Background(), sess, opts.RuntimeModel, effectiveContextWindow(opts), &status)
	if err != nil {
		t.Fatalf("maybeAutoCompactSession: %v", err)
	}
	if !compacted {
		t.Fatalf("expected session to auto-compact")
	}
	if !strings.Contains(status.String(), "Context auto-compacted") {
		t.Fatalf("expected compaction status output, got %q", status.String())
	}

	msgs, err := sess.ListMessages(context.Background())
	if err != nil {
		t.Fatalf("ListMessages: %v", err)
	}
	if len(msgs) >= 12 {
		t.Fatalf("expected compacted path to be shorter than original history, got %d messages", len(msgs))
	}
	first, ok := msgs[0].(*libagent.UserMessage)
	if !ok {
		t.Fatalf("first message type = %T, want *libagent.UserMessage", msgs[0])
	}
	if !strings.HasPrefix(first.Content, compactionCheckpointPrefix) {
		t.Fatalf("expected first message to be compaction summary, got %q", first.Content)
	}
}

func TestRunPromptAutoCompactsThresholdedSession(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	key := bindTestContext(t)

	store, err := persist.OpenStore()
	if err != nil {
		t.Fatalf("OpenStore: %v", err)
	}
	sessMeta, err := store.CreateEphemeral()
	if err != nil {
		t.Fatalf("CreateEphemeral: %v", err)
	}
	seedConversation(t, store.Messages(), sessMeta.ID, 6, 1200)
	bindSession(t, key, store, sessMeta)

	opts := Options{
		RuntimeModel: libagent.RuntimeModel{
			Model: &libagent.StaticTextModel{Response: "done"},
			ModelCfg: libagent.ModelConfig{
				Provider:      "mock",
				Model:         "mock",
				ContextWindow: 6000,
			},
		},
		ModelCfg: libagent.ModelConfig{
			Provider:      "mock",
			Model:         "mock",
			ContextWindow: 6000,
		},
	}

	out := captureStdout(t, func() {
		if err := Run(opts, "continue"); err != nil {
			t.Fatalf("Run(prompt): %v", err)
		}
	})
	if !strings.Contains(out, "done") {
		t.Fatalf("expected assistant output, got %q", out)
	}

	reloaded, err := persist.OpenStore()
	if err != nil {
		t.Fatalf("OpenStore reload: %v", err)
	}
	if err := reloaded.OpenSession(sessMeta.ID); err != nil {
		t.Fatalf("OpenSession reload: %v", err)
	}
	msgs, err := reloaded.Messages().List(context.Background(), sessMeta.ID)
	if err != nil {
		t.Fatalf("List reload: %v", err)
	}
	first, ok := msgs[0].(*libagent.UserMessage)
	if !ok {
		t.Fatalf("first message type = %T, want *libagent.UserMessage", msgs[0])
	}
	if !strings.HasPrefix(first.Content, compactionCheckpointPrefix) {
		t.Fatalf("expected first message to be compaction summary, got %q", first.Content)
	}
	last, ok := msgs[len(msgs)-1].(*libagent.AssistantMessage)
	if !ok {
		t.Fatalf("last message type = %T, want *libagent.AssistantMessage", msgs[len(msgs)-1])
	}
	if text := libagent.AssistantText(last); text != "done" {
		t.Fatalf("final assistant text = %q, want %q", text, "done")
	}
}

func seedConversation(t *testing.T, ms libagent.MessageService, sessionID string, pairs int, chars int) {
	t.Helper()

	ctx := context.Background()
	userText := strings.Repeat("u", chars)
	assistantText := strings.Repeat("a", chars)
	for i := 0; i < pairs; i++ {
		if _, err := ms.Create(ctx, sessionID, &libagent.UserMessage{
			Role:      "user",
			Content:   userText,
			Timestamp: time.Now(),
		}); err != nil {
			t.Fatalf("create user %d: %v", i, err)
		}
		if _, err := ms.Create(ctx, sessionID, assistantMessage(assistantText)); err != nil {
			t.Fatalf("create assistant %d: %v", i, err)
		}
	}
}

func assistantMessage(text string) *libagent.AssistantMessage {
	msg := libagent.NewAssistantMessage(text, "", nil, time.Now())
	msg.Completed = true
	return msg
}
