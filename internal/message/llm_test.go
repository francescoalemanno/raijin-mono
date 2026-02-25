package message

import (
	"strings"
	"testing"

	"github.com/francescoalemanno/raijin-mono/llmbridge/pkg/llm"
)

func TestPromptWithUserAttachmentsTextFiles(t *testing.T) {
	t.Parallel()

	attachments := []BinaryContent{
		{
			Path:     "main.go",
			MIMEType: "text/plain",
			Data:     []byte("package main\n"),
		},
		{
			Path:     "README.md",
			MIMEType: "text/markdown",
			Data:     []byte("# Title\n"),
		},
		{
			Path:     "diagram.png",
			MIMEType: "image/png",
			Data:     []byte{0x01, 0x02, 0x03},
		},
	}

	got := PromptWithUserAttachments("Review these files", attachments, nil)

	if !strings.HasPrefix(got, "Review these files") {
		t.Fatalf("prompt prefix not preserved: %q", got)
	}
	if strings.Count(got, "<system_info>") != 1 {
		t.Fatalf("expected exactly one system_info header, got: %q", got)
	}
	if !strings.Contains(got, "<attached_files>") {
		t.Fatalf("missing attached files summary: %q", got)
	}
	if !strings.Contains(got, "- main.go [file, text/plain]") {
		t.Fatalf("missing text attachment in summary: %q", got)
	}
	if !strings.Contains(got, "- diagram.png [image, image/png]") {
		t.Fatalf("missing image attachment in summary: %q", got)
	}
	if !strings.Contains(got, `<attachment path="diagram.png" mime_type="image/png" kind="binary" />`) {
		t.Fatalf("missing binary attachment metadata: %q", got)
	}
	if !strings.Contains(got, "<file path=\"main.go\">") {
		t.Fatalf("missing first text attachment block: %q", got)
	}
	if !strings.Contains(got, "<file path=\"README.md\">") {
		t.Fatalf("missing second text attachment block: %q", got)
	}
	if strings.Contains(got, "<file path=\"diagram.png\">") {
		t.Fatalf("non-text attachment should not be inlined as a file block: %q", got)
	}
}

func TestToLLMMessagesUserTextAndImageAttachments(t *testing.T) {
	t.Parallel()

	msg := Message{
		Role: User,
		Parts: []ContentPart{
			TextContent{Text: "Analyze"},
			BinaryContent{
				Path:     "main.go",
				MIMEType: "text/plain",
				Data:     []byte("func main() {}\n"),
			},
			SkillContent{
				Name:    "commit",
				Content: "<instructions>Commit these changes.</instructions>",
			},
			BinaryContent{
				Path:     "diagram.png",
				MIMEType: "image/png",
				Data:     []byte{0x01, 0x02},
			},
		},
	}

	llmMessages := ToLLMMessages(msg)
	if len(llmMessages) != 1 {
		t.Fatalf("expected one LLM message, got %d", len(llmMessages))
	}

	var (
		textPart *llm.TextPart
		filePart *llm.FilePart
	)
	for _, p := range llmMessages[0].Content {
		if tp, ok := p.(llm.TextPart); ok {
			part := tp
			textPart = &part
			continue
		}
		if fp, ok := p.(llm.FilePart); ok {
			part := fp
			filePart = &part
		}
	}

	if textPart == nil {
		t.Fatalf("expected a text part")
	}
	if !strings.Contains(textPart.Text, "<system_info>") {
		t.Fatalf("expected text attachment injection in text part: %q", textPart.Text)
	}
	if !strings.Contains(textPart.Text, `<attachment path="diagram.png" mime_type="image/png" kind="binary" />`) {
		t.Fatalf("expected non-text attachment metadata in text part: %q", textPart.Text)
	}
	if !strings.Contains(textPart.Text, "<file path=\"main.go\">") {
		t.Fatalf("expected attached text file in text part: %q", textPart.Text)
	}
	if !strings.Contains(textPart.Text, `<skill name="commit">`) {
		t.Fatalf("expected attached skill in text part: %q", textPart.Text)
	}

	if filePart == nil {
		t.Fatalf("expected one non-text file part")
	}
	if filePart.Filename != "diagram.png" {
		t.Fatalf("unexpected file part filename: %q", filePart.Filename)
	}
}

func TestPrepareUserRequest_AppendsAllowedToolsNotice(t *testing.T) {
	t.Parallel()

	got := PrepareUserRequest(UserRequest{
		Prompt:       "Review this file.",
		AllowedTools: []string{"grep", "read"},
	}).Prompt
	want := "<system_info>For this specific user request the only tools that are allowed are: grep, read.</system_info>"
	if !strings.Contains(got, want) {
		t.Fatalf("prompt missing notice.\nGot: %q\nWant to contain: %q", got, want)
	}
}
