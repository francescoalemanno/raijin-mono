package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	libagent "github.com/francescoalemanno/raijin-mono/libagent"
)

type globToolDetailsJSON struct {
	ResultLimitReached *int `json:"resultLimitReached,omitempty"`
	Truncation         *struct {
		Truncated bool `json:"truncated"`
	} `json:"truncation,omitempty"`
}

func TestGlobToolLimitAndAtPathExpansion(t *testing.T) {
	tmp := t.TempDir()
	oldCwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	defer func() {
		_ = os.Chdir(oldCwd)
	}()
	if err := os.Chdir(tmp); err != nil {
		t.Fatalf("chdir temp: %v", err)
	}

	sub := filepath.Join(tmp, "sub")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	now := time.Now()
	for i := 0; i < 12; i++ {
		name := fmt.Sprintf("f%02d.go", i)
		p := filepath.Join(sub, name)
		if err := os.WriteFile(p, []byte("package p\n"), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
		ts := now.Add(time.Duration(i) * time.Minute)
		_ = os.Chtimes(p, ts, ts)
	}

	tool := NewGlobTool()
	resp, err := tool.Run(context.Background(), libagent.ToolCall{Input: `{"pattern":"*.go","path":"@sub","limit":1}`})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.IsError {
		t.Fatalf("unexpected tool error: %s", resp.Content)
	}

	got := strings.TrimSpace(resp.Content)
	if got == "No files found matching pattern" {
		t.Fatalf("expected matches, got none")
	}

	outputSection := strings.Split(got, "\n\n[")[0]
	lines := strings.Split(strings.TrimSpace(outputSection), "\n")
	if len(lines) != 10 {
		t.Fatalf("expected 10 lines due to minimum limit clamp, got %d (%q)", len(lines), got)
	}
	if lines[0] != "f11.go" {
		t.Fatalf("expected newest file f11.go as first line, got %q", lines[0])
	}
	if !strings.Contains(got, "10 results limit reached") {
		t.Fatalf("expected limit notice with effective limit 10, got %q", got)
	}

	if resp.Metadata == "" {
		t.Fatalf("expected metadata for limit reach")
	}
	var details globToolDetailsJSON
	if err := json.Unmarshal([]byte(resp.Metadata), &details); err != nil {
		t.Fatalf("unmarshal metadata: %v", err)
	}
	if details.ResultLimitReached == nil || *details.ResultLimitReached != 10 {
		t.Fatalf("resultLimitReached = %#v, want 10", details.ResultLimitReached)
	}
}

func TestGlobToolPathNotFoundSoftError(t *testing.T) {
	t.Parallel()

	tool := NewGlobTool()
	missing := fmt.Sprintf("~/definitely-missing-%d", time.Now().UnixNano())
	resp, err := tool.Run(context.Background(), libagent.ToolCall{Input: fmt.Sprintf(`{"pattern":"*.go","path":%q}`, missing)})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !resp.IsError {
		t.Fatalf("expected error response")
	}
	if !strings.Contains(resp.Content, "Path not found:") {
		t.Fatalf("unexpected error content: %q", resp.Content)
	}
}

func TestGlobToolInvalidPatternSoftError(t *testing.T) {
	t.Parallel()

	tool := NewGlobTool()
	resp, err := tool.Run(context.Background(), libagent.ToolCall{Input: `{"pattern":"["}`})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !resp.IsError {
		t.Fatalf("expected error response")
	}
	if !strings.Contains(resp.Content, "invalid glob pattern") {
		t.Fatalf("unexpected error content: %q", resp.Content)
	}
}

func TestGlobToolPathMustBeDirectory(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	filePath := filepath.Join(tmp, "one.go")
	if err := os.WriteFile(filePath, []byte("package one\n"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	tool := NewGlobTool()
	resp, err := tool.Run(context.Background(), libagent.ToolCall{Input: fmt.Sprintf(`{"pattern":"*.go","path":%q}`, filePath)})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !resp.IsError {
		t.Fatalf("expected error response")
	}
	if !strings.Contains(resp.Content, "Path is not a directory:") {
		t.Fatalf("unexpected error content: %q", resp.Content)
	}
}
