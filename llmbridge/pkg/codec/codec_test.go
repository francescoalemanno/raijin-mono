package codec

import (
	"strings"
	"testing"

	"github.com/francescoalemanno/raijin-mono/llmbridge/pkg/llm"
)

func TestPrepareUserRequest_AppendsAllowedToolsNoticeAndKeepsNonTextFiles(t *testing.T) {
	t.Parallel()

	prepared := PrepareUserRequest(UserRequest{
		Prompt: "Review this file.",
		Attachments: []AppAttachment{
			{Path: "main.go", MIMEType: "text/plain", Data: []byte("package main\n")},
			{Path: "diagram.png", MIMEType: "image/png", Data: []byte{0x01, 0x02}},
		},
		AllowedTools: []string{"grep", "read"},
	})

	wantNotice := "<system_info>For this specific user request the only tools that are allowed are: grep, read.</system_info>"
	if !strings.Contains(prepared.Prompt, wantNotice) {
		t.Fatalf("prepared prompt missing allowed-tools notice.\nGot: %q\nWant to contain: %q", prepared.Prompt, wantNotice)
	}

	if len(prepared.Files) != 1 {
		t.Fatalf("expected one non-text file, got %d", len(prepared.Files))
	}
	if prepared.Files[0].Filename != "diagram.png" {
		t.Fatalf("unexpected file filename: %q", prepared.Files[0].Filename)
	}
}

func TestFromToolResult_MediaDefaultsToLoadedContent(t *testing.T) {
	t.Parallel()

	result := FromToolResult("read", llm.ToolResultPart{
		ToolCallID: "call-1",
		Output: llm.ToolResultOutput{
			Type:      llm.ToolResultOutputMedia,
			MediaType: "image/png",
			Data:      "ZmFrZQ==",
		},
	})

	if result.Content != "Loaded image/png content" {
		t.Fatalf("unexpected fallback content: %q", result.Content)
	}
	if result.MIMEType != "image/png" {
		t.Fatalf("unexpected mime type: %q", result.MIMEType)
	}
}

func TestToLLMMessages_AssistantPreservesToolCallInputVerbatim(t *testing.T) {
	t.Parallel()

	msgs := ToLLMMessages(AppMessage{
		Role: AppRoleAssistant,
		Text: "done",
		ToolCalls: []AppToolCall{
			{ID: "ok", Name: "read", Input: `{"path":"a.txt"}`},
			{ID: "bad", Name: "bash", Input: `{"cmd":"echo`},
		},
	})
	if len(msgs) != 1 {
		t.Fatalf("message count = %d, want 1", len(msgs))
	}
	if len(msgs[0].Content) != 3 {
		t.Fatalf("parts count = %d, want 3 (text + two tool calls)", len(msgs[0].Content))
	}

	call1, ok := msgs[0].Content[1].(llm.ToolCallPart)
	if !ok {
		t.Fatalf("part[1] type = %T, want llm.ToolCallPart", msgs[0].Content[1])
	}
	if call1.ToolCallID != "ok" {
		t.Fatalf("tool call id = %q, want %q", call1.ToolCallID, "ok")
	}
	if call1.InputJSON != `{"path":"a.txt"}` {
		t.Fatalf("tool call input = %q, want %q", call1.InputJSON, `{"path":"a.txt"}`)
	}

	call2, ok := msgs[0].Content[2].(llm.ToolCallPart)
	if !ok {
		t.Fatalf("part[2] type = %T, want llm.ToolCallPart", msgs[0].Content[2])
	}
	if call2.ToolCallID != "bad" {
		t.Fatalf("tool call id = %q, want %q", call2.ToolCallID, "bad")
	}
	if call2.InputJSON != `{"cmd":"echo` {
		t.Fatalf("tool call input = %q, want %q", call2.InputJSON, `{"cmd":"echo`)
	}
}

func TestToolResultMetadataRoundTripThroughCodec(t *testing.T) {
	t.Parallel()

	meta := `{"truncation":{"truncated":true}}`
	app := FromToolResult("bash", llm.ToolResultPart{
		ToolCallID: "tool-1",
		Output:     llm.ToolResultOutput{Type: llm.ToolResultOutputText, Text: "ok"},
		Metadata:   meta,
	})
	if app.Metadata != meta {
		t.Fatalf("app metadata = %q, want %q", app.Metadata, meta)
	}

	msgs := ToLLMMessages(AppMessage{Role: AppRoleTool, Results: []AppToolResult{app}})
	if len(msgs) != 1 || len(msgs[0].Content) != 1 {
		t.Fatalf("unexpected message shape: %#v", msgs)
	}
	part, ok := msgs[0].Content[0].(llm.ToolResultPart)
	if !ok {
		t.Fatalf("part type = %T, want llm.ToolResultPart", msgs[0].Content[0])
	}
	if part.Metadata != meta {
		t.Fatalf("llm metadata = %q, want %q", part.Metadata, meta)
	}
}
