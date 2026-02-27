package chat

import (
	"strings"
	"testing"

	"github.com/francescoalemanno/raijin-mono/internal/message"
)

func TestFindCompactionCutIndex(t *testing.T) {
	t.Parallel()

	msgs := []message.Message{
		{Role: message.User, Parts: []message.ContentPart{message.TextContent{Text: strings.Repeat("a", 1000)}}},
		{Role: message.Assistant, Parts: []message.ContentPart{message.TextContent{Text: strings.Repeat("b", 1000)}}},
		{Role: message.User, Parts: []message.ContentPart{message.TextContent{Text: strings.Repeat("c", 1000)}}},
	}

	cut := findCompactionCutIndex(msgs, 300)
	if cut <= 0 || cut >= len(msgs) {
		t.Fatalf("unexpected cut index: %d", cut)
	}
}

func TestFindCompactionCutIndex_AvoidsSplittingToolCallAndResult(t *testing.T) {
	t.Parallel()

	msgs := []message.Message{
		{Role: message.User, Parts: []message.ContentPart{message.TextContent{Text: strings.Repeat("old", 400)}}},
		{Role: message.Assistant, Parts: []message.ContentPart{message.TextContent{Text: strings.Repeat("old-reply", 300)}}},
		{Role: message.Assistant, Parts: []message.ContentPart{message.ToolCall{ID: "call-1", Name: "read", Input: `{"path":"a.go"}`, Finished: true}}},
		{Role: message.Tool, Parts: []message.ContentPart{message.ToolResult{ToolCallID: "call-1", Name: "read", Content: "file content"}}},
		{Role: message.User, Parts: []message.ContentPart{message.TextContent{Text: strings.Repeat("tail", 400)}}},
	}

	baseCut := findTokenBudgetCutIndex(msgs, estimateMessageTokens(msgs[3])+estimateMessageTokens(msgs[4]))
	if baseCut != 3 {
		t.Fatalf("base token cut = %d, want 3", baseCut)
	}

	cut := findCompactionCutIndex(msgs, estimateMessageTokens(msgs[3])+estimateMessageTokens(msgs[4]))
	if cut == 3 {
		t.Fatalf("cut index must not split assistant tool call and tool result, got %d", cut)
	}
	if !isValidCompactionCutIndex(msgs, cut) {
		t.Fatalf("cut index should be valid, got %d", cut)
	}
}

func TestIsValidCompactionCutIndex_RejectsOrphanToolResults(t *testing.T) {
	t.Parallel()

	msgs := []message.Message{
		{Role: message.User, Parts: []message.ContentPart{message.TextContent{Text: "old"}}},
		{Role: message.Assistant, Parts: []message.ContentPart{message.ToolCall{ID: "call-1", Name: "read", Input: `{}`, Finished: true}}},
		{Role: message.Tool, Parts: []message.ContentPart{message.ToolResult{ToolCallID: "call-1", Name: "read", Content: "result"}}},
	}

	if isValidCompactionCutIndex(msgs, 2) {
		t.Fatalf("cut index 2 should be invalid: kept tail starts with orphan tool result")
	}
}

func TestSerializeConversationForCompaction(t *testing.T) {
	t.Parallel()

	msgs := []message.Message{
		{Role: message.User, Parts: []message.ContentPart{message.TextContent{Text: "user prompt"}}},
		{Role: message.Assistant, Parts: []message.ContentPart{
			message.ReasoningContent{Thinking: "thinking"},
			message.TextContent{Text: "assistant reply"},
			message.ToolCall{ID: "1", Name: "read", Input: `{"path":"a.go"}`},
		}},
		{Role: message.Tool, Parts: []message.ContentPart{message.ToolResult{ToolCallID: "1", Name: "read", Content: "file content"}}},
	}

	got := serializeConversationForCompaction(msgs)
	checks := []string{
		"[User]: user prompt",
		"[Assistant thinking]: thinking",
		"[Assistant]: assistant reply",
		"[Assistant tool calls]: read(input={\"path\":\"a.go\"})",
		"[Tool result]: file content",
	}
	for _, want := range checks {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in serialized conversation: %q", want, got)
		}
	}
}

func TestCompactionKeepRecentTokens(t *testing.T) {
	t.Parallel()

	if got := compactionKeepRecentTokens(8_000, defaultCompactionReserveTokens); got >= 8_000 {
		t.Fatalf("keepRecentTokens should be below context window, got %d", got)
	}
	if got := compactionKeepRecentTokens(8_000, defaultCompactionReserveTokens); got != 4_000 {
		t.Fatalf("expected keepRecentTokens to clamp to 4000, got %d", got)
	}
	if got := compactionKeepRecentTokens(200_000, defaultCompactionReserveTokens); got != defaultCompactionKeepRecentTokens {
		t.Fatalf("expected default keepRecentTokens for large windows, got %d", got)
	}
}

func TestEstimateConversationTokens(t *testing.T) {
	t.Parallel()

	msgs := []message.Message{
		{Role: message.User, Parts: []message.ContentPart{message.TextContent{Text: "abcd"}}},
		{Role: message.Assistant, Parts: []message.ContentPart{message.TextContent{Text: "efgh"}}},
	}

	got := estimateConversationTokens(msgs)
	if got <= 0 {
		t.Fatalf("expected positive token estimate, got %d", got)
	}
}
