package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	libagent "github.com/francescoalemanno/raijin-mono/libagent"
)

func runWrite(t *testing.T, tool libagent.Tool, input map[string]any) (libagent.ToolResponse, error) {
	t.Helper()
	raw, err := json.Marshal(input)
	if err != nil {
		t.Fatalf("marshal input: %v", err)
	}
	return tool.Run(context.Background(), libagent.ToolCall{Input: string(raw)})
}

var ansiEscapeRE = regexp.MustCompile(`\x1b\[[0-9;]*m`)

func TestWriteTool_OverwriteReturnsDiffInContentAndMetadata(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	file := filepath.Join(dir, "sample.txt")
	if err := os.WriteFile(file, []byte("alpha\n"), 0o644); err != nil {
		t.Fatalf("write seed file: %v", err)
	}

	tool := NewWriteTool()
	resp, err := runWrite(t, tool, map[string]any{
		"path":    file,
		"content": "beta\n",
	})
	if err != nil {
		t.Fatalf("run write: %v", err)
	}
	if resp.IsError {
		t.Fatalf("unexpected error response: %q", resp.Content)
	}
	if !strings.Contains(resp.Content, "Successfully wrote file") {
		t.Fatalf("unexpected success content: %q", resp.Content)
	}
	if strings.Contains(resp.Content, "Diff:") {
		t.Fatalf("did not expect diff marker in response content: %q", resp.Content)
	}

	var details EditToolDetails
	if err := json.Unmarshal([]byte(resp.Metadata), &details); err != nil {
		t.Fatalf("unmarshal metadata: %v", err)
	}
	if !regexp.MustCompile(`(?m)^-\s+\d+\s+\|\s+alpha$`).MatchString(details.Diff) ||
		!regexp.MustCompile(`(?m)^\+\s+\d+\s+\|\s+beta$`).MatchString(details.Diff) {
		t.Fatalf("expected diff in metadata, got %q", details.Diff)
	}
}

func TestWriteTool_CreateReturnsDiffAgainstEmptyFile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	file := filepath.Join(dir, "created.txt")

	tool := NewWriteTool()
	resp, err := runWrite(t, tool, map[string]any{
		"path":    file,
		"content": "first line\nsecond line\n",
	})
	if err != nil {
		t.Fatalf("run write: %v", err)
	}
	if resp.IsError {
		t.Fatalf("unexpected error response: %q", resp.Content)
	}
	if !strings.Contains(resp.Content, "Successfully created file") {
		t.Fatalf("expected create message, got %q", resp.Content)
	}
	if strings.Contains(resp.Content, "Diff:") {
		t.Fatalf("did not expect diff marker in response content: %q", resp.Content)
	}
}

func TestWriteTool_RenderStaysInputOnlyOnCompletion(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	file := filepath.Join(dir, "render.txt")
	if err := os.WriteFile(file, []byte("before\n"), 0o644); err != nil {
		t.Fatalf("write seed file: %v", err)
	}

	tool := NewWriteTool()
	input := map[string]any{
		"path":    file,
		"content": "after\n",
	}
	resp, err := runWrite(t, tool, input)
	if err != nil {
		t.Fatalf("run write: %v", err)
	}
	raw, err := json.Marshal(input)
	if err != nil {
		t.Fatalf("marshal input: %v", err)
	}

	rt, ok := tool.(RenderableTool)
	if !ok {
		t.Fatalf("write tool should implement RenderableTool")
	}
	rendered := ansiEscapeRE.ReplaceAllString(rt.Render(raw, resp.Content, 0), "")
	if !strings.Contains(rendered, "wrote") || !strings.Contains(rendered, "render.txt") {
		t.Fatalf("unexpected render header: %q", rendered)
	}
	if regexp.MustCompile(`(?m)^-\s+\d+\s+\|`).MatchString(rendered) ||
		regexp.MustCompile(`(?m)^\+\s+\d+\s+\|`).MatchString(rendered) {
		t.Fatalf("did not expect completion diff in render output, got %q", rendered)
	}
}
