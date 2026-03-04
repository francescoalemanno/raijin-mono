package message

import "testing"

func TestSanitizeHistory_KeepsValidConversation(t *testing.T) {
	msgs := []Message{
		{Role: User, Parts: []ContentPart{TextContent{Text: "hello"}}},
		{Role: Assistant, Parts: []ContentPart{
			ReasoningContent{Thinking: "thinking"},
			TextContent{Text: "I will read a file"},
			ToolCall{ID: "call-1", Name: "read", Input: `{"path":"README.md"}`},
			Finish{Reason: FinishReasonToolUse},
		}},
		{Role: Tool, Parts: []ContentPart{ToolResult{ToolCallID: "call-1", Name: "read", Content: "ok"}}},
	}

	out := SanitizeHistory(msgs)
	if len(out) != 3 {
		t.Fatalf("len(out)=%d want 3", len(out))
	}
}

func TestSanitizeHistory_DropsOrphanToolMessages(t *testing.T) {
	msgs := []Message{
		{Role: User, Parts: []ContentPart{TextContent{Text: "hello"}}},
		{Role: Assistant, Parts: []ContentPart{ToolCall{ID: "call-1", Name: "read"}, Finish{Reason: FinishReasonToolUse}}},
		{Role: Tool, Parts: []ContentPart{ToolResult{ToolCallID: "orphan", Name: "read", Content: "x"}}},
	}

	out := SanitizeHistory(msgs)
	if len(out) != 1 {
		t.Fatalf("len(out)=%d want 1", len(out))
	}
	if out[0].Role != User {
		t.Fatalf("remaining role=%s want user", out[0].Role)
	}
}

func TestSanitizeHistory_DropsIncompleteAssistant(t *testing.T) {
	msgs := []Message{
		{Role: Assistant, Parts: []ContentPart{TextContent{Text: "partial without finish"}}},
	}

	out := SanitizeHistory(msgs)
	if len(out) != 0 {
		t.Fatalf("len(out)=%d want 0", len(out))
	}
}

func TestSanitizeHistory_UserAttachmentsWithoutTextAreKept(t *testing.T) {
	msgs := []Message{
		{Role: User, Parts: []ContentPart{BinaryContent{Path: "img.png", MIMEType: "image/png", Data: []byte("x")}}},
	}

	out := SanitizeHistory(msgs)
	if len(out) != 1 {
		t.Fatalf("len(out)=%d want 1", len(out))
	}
	if out[0].Role != User {
		t.Fatalf("role=%s want user", out[0].Role)
	}
}
