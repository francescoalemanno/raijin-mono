package chat

import (
	"testing"

	"github.com/francescoalemanno/raijin-mono/internal/message"
)

func TestCollectForkCandidates_NewestFirstAndUserOnly(t *testing.T) {
	t.Parallel()

	msgs := []message.Message{
		{ID: "u1", Role: message.User, Parts: []message.ContentPart{message.TextContent{Text: "first prompt"}}},
		{ID: "a1", Role: message.Assistant, Parts: []message.ContentPart{message.TextContent{Text: "answer"}}},
		{ID: "u2", Role: message.User, Parts: []message.ContentPart{message.TextContent{Text: "second\n prompt"}}},
		{ID: "t1", Role: message.Tool, Parts: []message.ContentPart{message.ToolResult{Name: "read", Content: "ok"}}},
		{ID: "u3", Role: message.User, Parts: []message.ContentPart{message.TextContent{Text: "third"}}},
		{ID: "u-empty", Role: message.User, Parts: []message.ContentPart{message.TextContent{Text: "   "}}},
	}

	candidates := collectForkCandidates(msgs)
	if len(candidates) != 3 {
		t.Fatalf("candidate count = %d, want 3", len(candidates))
	}

	if candidates[0].MessageID != "u3" || candidates[0].Ordinal != 3 {
		t.Fatalf("first candidate = %#v, want newest user message u3 ordinal 3", candidates[0])
	}
	if candidates[1].MessageID != "u2" || candidates[1].Ordinal != 2 {
		t.Fatalf("second candidate = %#v, want u2 ordinal 2", candidates[1])
	}
	if candidates[2].MessageID != "u1" || candidates[2].Ordinal != 1 {
		t.Fatalf("third candidate = %#v, want u1 ordinal 1", candidates[2])
	}

	if candidates[1].Preview != "second prompt" {
		t.Fatalf("normalized preview = %q, want %q", candidates[1].Preview, "second prompt")
	}
}

func TestBuildForkPreview_Truncates(t *testing.T) {
	t.Parallel()

	got := buildForkPreview("one two three four", 8)
	if got != "one two…" {
		t.Fatalf("preview = %q, want %q", got, "one two…")
	}
}
