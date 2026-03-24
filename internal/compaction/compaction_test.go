package compaction

import (
	"context"
	"strings"
	"testing"
	"time"

	libagent "github.com/francescoalemanno/raijin-mono/libagent"
)

func TestWatchTriggersOnContextFill(t *testing.T) {
	t.Parallel()

	msgs := []libagent.Message{
		&libagent.UserMessage{Role: "user", Content: strings.Repeat("a", 2000)},
	}

	watch := NewWatch(msgs, 4000)
	if !watch.TriggerByFill {
		t.Fatalf("expected context-fill trigger, got %#v", watch)
	}
	if watch.TriggerByTokens {
		t.Fatalf("did not expect token trigger, got %#v", watch)
	}
	if !watch.ShouldCompact() {
		t.Fatalf("expected watch to trigger compaction")
	}
}

func TestKeepRecentTokensTargetsTwentyPercentOfContextWindow(t *testing.T) {
	t.Parallel()

	got := KeepRecentTokens(200_000, DefaultReserveTokens)
	want := int64(37_600)
	if got != want {
		t.Fatalf("KeepRecentTokens = %d, want %d", got, want)
	}
}

func TestFindCutIndexRejectsInvalidBoundaries(t *testing.T) {
	t.Parallel()

	msgs := []libagent.Message{
		&libagent.UserMessage{Role: "user", Content: "start"},
		libagent.NewAssistantMessage("", "", []libagent.ToolCallItem{{
			ID:    "tool-1",
			Name:  "read",
			Input: `{"path":"README.md"}`,
		}}, time.Now()),
		&libagent.ToolResultMessage{Role: "toolResult", ToolCallID: "tool-1", ToolName: "read", Content: "ok"},
		libagent.NewAssistantMessage("", "", []libagent.ToolCallItem{{
			ID:    "tool-2",
			Name:  "read",
			Input: `{"path":"main.go"}`,
		}}, time.Now()),
	}
	if got := FindCutIndex(msgs, 1); got != 0 {
		t.Fatalf("FindCutIndex = %d, want 0 for invalid-only boundaries", got)
	}
}

func TestCompactFailsWithoutPersistedFirstKeptID(t *testing.T) {
	t.Parallel()

	msgs := make([]libagent.Message, 0, 12)
	for i := 0; i < 6; i++ {
		msgs = append(msgs, &libagent.UserMessage{Role: "user", Content: strings.Repeat("u", 1200)})
		reply := libagent.NewAssistantMessage(strings.Repeat("a", 1200), "", nil, time.Now())
		reply.Completed = true
		msgs = append(msgs, reply)
	}

	runtimeModel := libagent.RuntimeModel{
		Model: &libagent.StaticTextModel{Response: "checkpoint"},
		ModelCfg: libagent.ModelConfig{
			Provider:      "mock",
			Model:         "mock",
			ContextWindow: 6000,
		},
	}

	_, err := Compact(context.Background(), msgs, runtimeModel, Options{})
	if err == nil {
		t.Fatalf("expected missing persisted ID error")
	}
	if !strings.Contains(err.Error(), "no persisted message ID") {
		t.Fatalf("unexpected error: %v", err)
	}
}
