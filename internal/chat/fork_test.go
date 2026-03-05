package chat

import (
	"testing"

	libagent "github.com/francescoalemanno/raijin-mono/libagent"
)

func TestCollectForkCandidates_NewestFirstAndUserOnly(t *testing.T) {
	t.Parallel()

	msgs := []libagent.Message{
		&libagent.UserMessage{Meta: libagent.MessageMeta{ID: "u1"}, Role: "user", Content: "first prompt"},
		&libagent.AssistantMessage{Meta: libagent.MessageMeta{ID: "a1"}, Role: "assistant", Text: "answer", Completed: true},
		&libagent.UserMessage{Meta: libagent.MessageMeta{ID: "u2"}, Role: "user", Content: "second\n prompt"},
		&libagent.ToolResultMessage{Meta: libagent.MessageMeta{ID: "t1"}, Role: "toolResult", ToolName: "read", Content: "ok"},
		&libagent.UserMessage{Meta: libagent.MessageMeta{ID: "u3"}, Role: "user", Content: "third"},
		&libagent.UserMessage{Meta: libagent.MessageMeta{ID: "u-empty"}, Role: "user", Content: "   "},
	}

	candidates := collectForkCandidates(msgs)
	if len(candidates) != 4 {
		t.Fatalf("candidate count = %d, want 4", len(candidates))
	}

	if !candidates[0].IsHead {
		t.Fatalf("first candidate should be head option, got %#v", candidates[0])
	}
	if candidates[1].MessageID != "u3" || candidates[1].Ordinal != 3 {
		t.Fatalf("second candidate = %#v, want newest user message u3 ordinal 3", candidates[1])
	}
	if candidates[2].MessageID != "u2" || candidates[2].Ordinal != 2 {
		t.Fatalf("third candidate = %#v, want u2 ordinal 2", candidates[2])
	}
	if candidates[3].MessageID != "u1" || candidates[3].Ordinal != 1 {
		t.Fatalf("fourth candidate = %#v, want u1 ordinal 1", candidates[3])
	}

	if candidates[2].Preview != "second prompt" {
		t.Fatalf("normalized preview = %q, want %q", candidates[2].Preview, "second prompt")
	}
}

func TestBuildForkPreview_Truncates(t *testing.T) {
	t.Parallel()

	got := buildForkPreview("one two three four", 8)
	if got != "one two…" {
		t.Fatalf("preview = %q, want %q", got, "one two…")
	}
}
