package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/francescoalemanno/raijin-mono/llmbridge/pkg/llm"
)

func TestReadToolLimitNoticeWithNonZeroOffset(t *testing.T) {
	t.Parallel()

	path := writeTempLinesFile(t, 60)
	tool := NewReadTool()

	resp, err := tool.Run(context.Background(), llm.ToolCall{
		Input: fmt.Sprintf(`{"path":%q,"offset":21,"limit":10}`, path),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.IsError {
		t.Fatalf("expected success, got error response: %s", resp.Content)
	}
	if !strings.Contains(resp.Content, "line-021") || !strings.Contains(resp.Content, "line-030") {
		t.Fatalf("expected selected line window in output, got: %q", resp.Content)
	}
	if !strings.Contains(resp.Content, "[30 more lines in file. Use offset=31 to continue.]") {
		t.Fatalf("expected continuation notice, got: %q", resp.Content)
	}
}

func TestReadToolOffsetBeyondEnd(t *testing.T) {
	t.Parallel()

	path := writeTempLinesFile(t, 3)
	tool := NewReadTool()

	resp, err := tool.Run(context.Background(), llm.ToolCall{
		Input: fmt.Sprintf(`{"path":%q,"offset":10}`, path),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !resp.IsError {
		t.Fatalf("expected error response")
	}
	if !strings.Contains(resp.Content, "offset 10 is beyond end of file (3 lines total)") {
		t.Fatalf("unexpected message: %q", resp.Content)
	}
}

func TestReadToolDefaultTruncationIncludesContinuationHint(t *testing.T) {
	t.Parallel()

	path := writeTempLinesFile(t, DefaultMaxLines+25)
	tool := NewReadTool()

	resp, err := tool.Run(context.Background(), llm.ToolCall{
		Input: fmt.Sprintf(`{"path":%q}`, path),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.IsError {
		t.Fatalf("expected success, got error response: %s", resp.Content)
	}

	if !strings.Contains(resp.Content, fmt.Sprintf("[Showing lines 1-%d of %d. Use offset=%d to continue.]", DefaultMaxLines, DefaultMaxLines+25, DefaultMaxLines+1)) {
		t.Fatalf("expected truncation hint, got: %q", resp.Content)
	}
}

func writeTempLinesFile(t *testing.T, lines int) string {
	t.Helper()

	dir := t.TempDir()
	path := filepath.Join(dir, "sample.txt")

	contentLines := make([]string, lines)
	for i := 1; i <= lines; i++ {
		contentLines[i-1] = fmt.Sprintf("line-%03d", i)
	}
	content := strings.Join(contentLines, "\n")

	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	return path
}
