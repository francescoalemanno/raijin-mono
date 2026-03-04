package tools

import (
	"context"
	"fmt"
	"strings"
	"testing"

	libagent "github.com/francescoalemanno/raijin-mono/libagent"
)

func TestReadToolEmbeddedPath(t *testing.T) {
	t.Parallel()

	tool := NewReadTool()
	resp, err := tool.Run(context.Background(), libagent.ToolCall{Input: `{"path":"embedded://templates/init.md"}`})
	if err != nil {
		t.Fatalf("run read: %v", err)
	}
	if resp.IsError {
		t.Fatalf("expected success, got: %s", resp.Content)
	}
	if !strings.Contains(strings.ToLower(resp.Content), "template") && len(resp.Content) == 0 {
		t.Fatalf("unexpected content: %q", resp.Content)
	}
}

func TestGlobToolEmbeddedPath(t *testing.T) {
	t.Parallel()

	tool := NewGlobTool()
	resp, err := tool.Run(context.Background(), libagent.ToolCall{Input: `{"path":"embedded://skills","pattern":"**/SKILL.md"}`})
	if err != nil {
		t.Fatalf("run glob: %v", err)
	}
	if resp.IsError {
		t.Fatalf("expected success, got: %s", resp.Content)
	}
	if !strings.Contains(resp.Content, "commit/SKILL.md") {
		t.Fatalf("expected commit/SKILL.md in output, got: %q", resp.Content)
	}
}

func TestGrepToolEmbeddedPath(t *testing.T) {
	t.Parallel()

	tool := NewGrepTool()
	resp, err := tool.Run(context.Background(), libagent.ToolCall{Input: `{"path":"embedded://skills","pattern":"(?i)description","include":"**/SKILL.md"}`})
	if err != nil {
		t.Fatalf("run grep: %v", err)
	}
	if resp.IsError {
		t.Fatalf("expected success, got: %s", resp.Content)
	}
	if !strings.Contains(resp.Content, "embedded://skills") {
		t.Fatalf("expected embedded path in output, got: %q", resp.Content)
	}
}

func TestWriteToolRejectsEmbeddedPath(t *testing.T) {
	t.Parallel()

	tool := NewWriteTool()
	resp, err := tool.Run(context.Background(), libagent.ToolCall{Input: `{"path":"embedded://skills/new/SKILL.md","content":"x"}`})
	if err != nil {
		t.Fatalf("run write: %v", err)
	}
	if !resp.IsError {
		t.Fatalf("expected error response")
	}
	if !strings.Contains(strings.ToLower(resp.Content), "read-only") {
		t.Fatalf("unexpected response: %q", resp.Content)
	}
}

func TestEditToolRejectsEmbeddedPath(t *testing.T) {
	t.Parallel()

	tool := NewEditTool()
	input := fmt.Sprintf(`{"path":"embedded://skills/commit/SKILL.md","oldText":%q,"newText":%q}`, "old", "new")
	resp, err := tool.Run(context.Background(), libagent.ToolCall{Input: input})
	if err != nil {
		t.Fatalf("run edit: %v", err)
	}
	if !resp.IsError {
		t.Fatalf("expected error response")
	}
	if !strings.Contains(strings.ToLower(resp.Content), "read-only") {
		t.Fatalf("unexpected response: %q", resp.Content)
	}
}
