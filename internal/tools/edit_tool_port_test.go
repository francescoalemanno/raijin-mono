package tools

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	libagent "github.com/francescoalemanno/raijin-mono/libagent"
)

func runEdit(t *testing.T, tool libagent.Tool, input map[string]any) (libagent.ToolResponse, error) {
	t.Helper()
	raw, err := json.Marshal(input)
	if err != nil {
		t.Fatalf("marshal input: %v", err)
	}
	return tool.Run(context.Background(), libagent.ToolCall{Input: string(raw)})
}

func mustWriteFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
}

func mustReadFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read file: %v", err)
	}
	return string(b)
}

func parseEditDetails(t *testing.T, metadata string) EditToolDetails {
	t.Helper()
	if strings.TrimSpace(metadata) == "" {
		t.Fatalf("expected metadata to be populated")
	}
	var d EditToolDetails
	if err := json.Unmarshal([]byte(metadata), &d); err != nil {
		t.Fatalf("unmarshal metadata: %v", err)
	}
	return d
}

func TestEditTool_SchemaUsesCamelCaseParameters(t *testing.T) {
	t.Parallel()

	info := NewEditTool().Info()
	if _, ok := info.Parameters["oldText"]; !ok {
		t.Fatalf("schema missing oldText parameter")
	}
	if _, ok := info.Parameters["newText"]; !ok {
		t.Fatalf("schema missing newText parameter")
	}
	if _, ok := info.Parameters["old_str"]; ok {
		t.Fatalf("schema should not expose legacy old_str parameter")
	}
	if _, ok := info.Parameters["new_str"]; ok {
		t.Fatalf("schema should not expose legacy new_str parameter")
	}
}

func TestEditTool_RejectsLegacySnakeCaseKeys(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	file := filepath.Join(dir, "legacy-keys.txt")
	mustWriteFile(t, file, "hello world\n")

	tool := NewEditTool()
	resp, err := runEdit(t, tool, map[string]any{
		"path":    file,
		"old_str": "world",
		"new_str": "there",
	})
	if err != nil {
		t.Fatalf("run edit: %v", err)
	}
	if !resp.IsError {
		t.Fatalf("expected error response")
	}
	if got := mustReadFile(t, file); got != "hello world\n" {
		t.Fatalf("file content = %q, want %q", got, "hello world\\n")
	}
}

func TestEditTool_ReplacesTextAndReturnsDiffDetails(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	file := filepath.Join(dir, "edit-test.txt")
	mustWriteFile(t, file, "Hello, world!\n")

	tool := NewEditTool()
	resp, err := runEdit(t, tool, map[string]any{
		"path":    file,
		"oldText": "world",
		"newText": "testing",
	})
	if err != nil {
		t.Fatalf("run edit: %v", err)
	}
	if resp.IsError {
		t.Fatalf("unexpected error response: %s", resp.Content)
	}
	if !strings.Contains(resp.Content, "Successfully replaced text in") {
		t.Fatalf("unexpected success content: %q", resp.Content)
	}
	got := mustReadFile(t, file)
	if got != "Hello, testing!\n" {
		t.Fatalf("file content = %q, want %q", got, "Hello, testing!\\n")
	}

	details := parseEditDetails(t, resp.Metadata)
	if details.Diff == "" {
		t.Fatalf("expected diff to be non-empty")
	}
	if !strings.Contains(details.Diff, "+2 Hello, testing!") && !strings.Contains(details.Diff, "testing") {
		t.Fatalf("diff does not include replacement text: %q", details.Diff)
	}
	if details.FirstChangedLine == nil || *details.FirstChangedLine <= 0 {
		t.Fatalf("firstChangedLine = %#v, want positive line number", details.FirstChangedLine)
	}
}

func TestEditTool_FileNotFound(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	missing := filepath.Join(dir, "missing.txt")

	tool := NewEditTool()
	resp, err := runEdit(t, tool, map[string]any{
		"path":    missing,
		"oldText": "x",
		"newText": "y",
	})
	if err != nil {
		t.Fatalf("run edit: %v", err)
	}
	if !resp.IsError {
		t.Fatalf("expected error response")
	}
	if !strings.Contains(resp.Content, "File not found") {
		t.Fatalf("unexpected error content: %q", resp.Content)
	}
}

func TestEditTool_FailsWhenTextNotFound(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	file := filepath.Join(dir, "sample.txt")
	mustWriteFile(t, file, "Hello, world!\n")

	tool := NewEditTool()
	resp, err := runEdit(t, tool, map[string]any{
		"path":    file,
		"oldText": "does-not-exist",
		"newText": "replacement",
	})
	if err != nil {
		t.Fatalf("run edit: %v", err)
	}
	if !resp.IsError {
		t.Fatalf("expected error response")
	}
	if !strings.Contains(resp.Content, "Could not find the exact text") {
		t.Fatalf("unexpected error content: %q", resp.Content)
	}
}

func TestEditTool_FailsWhenTextAppearsMultipleTimes(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	file := filepath.Join(dir, "dups.txt")
	mustWriteFile(t, file, "foo foo foo")

	tool := NewEditTool()
	resp, err := runEdit(t, tool, map[string]any{
		"path":    file,
		"oldText": "foo",
		"newText": "bar",
	})
	if err != nil {
		t.Fatalf("run edit: %v", err)
	}
	if !resp.IsError {
		t.Fatalf("expected error response")
	}
	if !strings.Contains(resp.Content, "Found 3 occurrences") {
		t.Fatalf("unexpected error content: %q", resp.Content)
	}
}

func TestEditTool_FuzzyMatchTrailingWhitespace(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	file := filepath.Join(dir, "trailing-ws.txt")
	mustWriteFile(t, file, "line one   \nline two  \nline three\n")

	tool := NewEditTool()
	resp, err := runEdit(t, tool, map[string]any{
		"path":    file,
		"oldText": "line one\nline two\n",
		"newText": "replaced\n",
	})
	if err != nil {
		t.Fatalf("run edit: %v", err)
	}
	if resp.IsError {
		t.Fatalf("unexpected error response: %s", resp.Content)
	}

	got := mustReadFile(t, file)
	if got != "replaced\nline three\n" {
		t.Fatalf("file content = %q, want %q", got, "replaced\\nline three\\n")
	}
}

func TestEditTool_FuzzyMatchSmartSingleQuotes(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	file := filepath.Join(dir, "smart-single.txt")
	mustWriteFile(t, file, "console.log(‘hello’);\n")

	tool := NewEditTool()
	resp, err := runEdit(t, tool, map[string]any{
		"path":    file,
		"oldText": "console.log('hello');",
		"newText": "console.log('world');",
	})
	if err != nil {
		t.Fatalf("run edit: %v", err)
	}
	if resp.IsError {
		t.Fatalf("unexpected error response: %s", resp.Content)
	}
	if !strings.Contains(mustReadFile(t, file), "world") {
		t.Fatalf("expected updated file to contain world")
	}
}

func TestEditTool_FuzzyMatchSmartDoubleQuotes(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	file := filepath.Join(dir, "smart-double.txt")
	mustWriteFile(t, file, "const msg = “Hello World”;\n")

	tool := NewEditTool()
	resp, err := runEdit(t, tool, map[string]any{
		"path":    file,
		"oldText": `const msg = "Hello World";`,
		"newText": `const msg = "Goodbye";`,
	})
	if err != nil {
		t.Fatalf("run edit: %v", err)
	}
	if resp.IsError {
		t.Fatalf("unexpected error response: %s", resp.Content)
	}
	if !strings.Contains(mustReadFile(t, file), "Goodbye") {
		t.Fatalf("expected updated file to contain Goodbye")
	}
}

func TestEditTool_FuzzyMatchUnicodeDashes(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	file := filepath.Join(dir, "unicode-dashes.txt")
	mustWriteFile(t, file, "range: 1–5\nbreak—here\n")

	tool := NewEditTool()
	resp, err := runEdit(t, tool, map[string]any{
		"path":    file,
		"oldText": "range: 1-5\nbreak-here",
		"newText": "range: 10-50\nbreak--here",
	})
	if err != nil {
		t.Fatalf("run edit: %v", err)
	}
	if resp.IsError {
		t.Fatalf("unexpected error response: %s", resp.Content)
	}
	content := mustReadFile(t, file)
	if !strings.Contains(content, "10-50") {
		t.Fatalf("expected updated file to contain 10-50, got %q", content)
	}
}

func TestEditTool_FuzzyMatchNonBreakingSpace(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	file := filepath.Join(dir, "nbsp.txt")
	mustWriteFile(t, file, "hello\u00A0world\n")

	tool := NewEditTool()
	resp, err := runEdit(t, tool, map[string]any{
		"path":    file,
		"oldText": "hello world",
		"newText": "hello universe",
	})
	if err != nil {
		t.Fatalf("run edit: %v", err)
	}
	if resp.IsError {
		t.Fatalf("unexpected error response: %s", resp.Content)
	}
	if !strings.Contains(mustReadFile(t, file), "universe") {
		t.Fatalf("expected updated file to contain universe")
	}
}

func TestEditTool_PrefersExactMatchOverFuzzyMatch(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	file := filepath.Join(dir, "exact-preferred.txt")
	mustWriteFile(t, file, "const x = 'exact';\nconst y = 'other';\n")

	tool := NewEditTool()
	resp, err := runEdit(t, tool, map[string]any{
		"path":    file,
		"oldText": "const x = 'exact';",
		"newText": "const x = 'changed';",
	})
	if err != nil {
		t.Fatalf("run edit: %v", err)
	}
	if resp.IsError {
		t.Fatalf("unexpected error response: %s", resp.Content)
	}
	got := mustReadFile(t, file)
	if got != "const x = 'changed';\nconst y = 'other';\n" {
		t.Fatalf("file content = %q", got)
	}
}

func TestEditTool_DetectsFuzzyDuplicates(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	file := filepath.Join(dir, "fuzzy-dups.txt")
	mustWriteFile(t, file, "hello world   \nhello world\n")

	tool := NewEditTool()
	resp, err := runEdit(t, tool, map[string]any{
		"path":    file,
		"oldText": "hello world",
		"newText": "replaced",
	})
	if err != nil {
		t.Fatalf("run edit: %v", err)
	}
	if !resp.IsError {
		t.Fatalf("expected error response")
	}
	if !strings.Contains(resp.Content, "Found 2 occurrences") {
		t.Fatalf("unexpected error content: %q", resp.Content)
	}
}

func TestEditTool_MatchesLFOldTextAgainstCRLFFileAndPreservesCRLF(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	file := filepath.Join(dir, "crlf.txt")
	mustWriteFile(t, file, "first\r\nsecond\r\nthird\r\n")

	tool := NewEditTool()
	resp, err := runEdit(t, tool, map[string]any{
		"path":    file,
		"oldText": "second\n",
		"newText": "REPLACED\n",
	})
	if err != nil {
		t.Fatalf("run edit: %v", err)
	}
	if resp.IsError {
		t.Fatalf("unexpected error response: %s", resp.Content)
	}
	got := mustReadFile(t, file)
	want := "first\r\nREPLACED\r\nthird\r\n"
	if got != want {
		t.Fatalf("content = %q, want %q", got, want)
	}
}

func TestEditTool_PreservesLFForLFFiles(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	file := filepath.Join(dir, "lf.txt")
	mustWriteFile(t, file, "first\nsecond\nthird\n")

	tool := NewEditTool()
	resp, err := runEdit(t, tool, map[string]any{
		"path":    file,
		"oldText": "second\n",
		"newText": "REPLACED\n",
	})
	if err != nil {
		t.Fatalf("run edit: %v", err)
	}
	if resp.IsError {
		t.Fatalf("unexpected error response: %s", resp.Content)
	}
	got := mustReadFile(t, file)
	want := "first\nREPLACED\nthird\n"
	if got != want {
		t.Fatalf("content = %q, want %q", got, want)
	}
}

func TestEditTool_DetectsDuplicatesAcrossCRLFAndLFVariants(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	file := filepath.Join(dir, "mixed-endings.txt")
	mustWriteFile(t, file, "hello\r\nworld\r\n---\r\nhello\nworld\n")

	tool := NewEditTool()
	resp, err := runEdit(t, tool, map[string]any{
		"path":    file,
		"oldText": "hello\nworld\n",
		"newText": "replaced\n",
	})
	if err != nil {
		t.Fatalf("run edit: %v", err)
	}
	if !resp.IsError {
		t.Fatalf("expected error response")
	}
	if !strings.Contains(resp.Content, "Found 2 occurrences") {
		t.Fatalf("unexpected error content: %q", resp.Content)
	}
}

func TestEditTool_PreservesUTF8BOM(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	file := filepath.Join(dir, "bom.txt")
	mustWriteFile(t, file, "\uFEFFfirst\r\nsecond\r\nthird\r\n")

	tool := NewEditTool()
	resp, err := runEdit(t, tool, map[string]any{
		"path":    file,
		"oldText": "second\n",
		"newText": "REPLACED\n",
	})
	if err != nil {
		t.Fatalf("run edit: %v", err)
	}
	if resp.IsError {
		t.Fatalf("unexpected error response: %s", resp.Content)
	}
	got := mustReadFile(t, file)
	want := "\uFEFFfirst\r\nREPLACED\r\nthird\r\n"
	if got != want {
		t.Fatalf("content = %q, want %q", got, want)
	}
}

func TestEditTool_NoChangesMadeErrorWhenReplacementIsIdentical(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	file := filepath.Join(dir, "no-change.txt")
	mustWriteFile(t, file, "alpha\n")

	tool := NewEditTool()
	resp, err := runEdit(t, tool, map[string]any{
		"path":    file,
		"oldText": "alpha",
		"newText": "alpha",
	})
	if err != nil {
		t.Fatalf("run edit: %v", err)
	}
	if !resp.IsError {
		t.Fatalf("expected error response")
	}
	if !strings.Contains(resp.Content, "No changes made") {
		t.Fatalf("unexpected error content: %q", resp.Content)
	}
}

func TestEditTool_RespectsContextCancellation(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	file := filepath.Join(dir, "cancel.txt")
	mustWriteFile(t, file, "hello world\n")

	tool := NewEditTool()
	raw, err := json.Marshal(map[string]any{
		"path":    file,
		"oldText": "world",
		"newText": "there",
	})
	if err != nil {
		t.Fatalf("marshal input: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err = tool.Run(ctx, libagent.ToolCall{Input: string(raw)})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context.Canceled", err)
	}
}

func TestEditTool_RejectsEmptyOldText(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	file := filepath.Join(dir, "empty-old.txt")
	mustWriteFile(t, file, "hello world\n")

	tool := NewEditTool()
	resp, err := runEdit(t, tool, map[string]any{
		"path":    file,
		"oldText": "",
		"newText": "replacement",
	})
	if err != nil {
		t.Fatalf("run edit: %v", err)
	}
	if !resp.IsError {
		t.Fatalf("expected error response")
	}
	if !strings.Contains(resp.Content, "oldText and newText cannot be empty") {
		t.Fatalf("unexpected error content: %q", resp.Content)
	}
	if !strings.Contains(resp.Content, "surrounding context") {
		t.Fatalf("expected advice about context in error: %q", resp.Content)
	}
}

func TestEditTool_RejectsEmptyNewText(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	file := filepath.Join(dir, "empty-new.txt")
	mustWriteFile(t, file, "hello world\n")

	tool := NewEditTool()
	resp, err := runEdit(t, tool, map[string]any{
		"path":    file,
		"oldText": "world",
		"newText": "",
	})
	if err != nil {
		t.Fatalf("run edit: %v", err)
	}
	if !resp.IsError {
		t.Fatalf("expected error response")
	}
	if !strings.Contains(resp.Content, "oldText and newText cannot be empty") {
		t.Fatalf("unexpected error content: %q", resp.Content)
	}
	if !strings.Contains(resp.Content, "surrounding context") {
		t.Fatalf("expected advice about context in error: %q", resp.Content)
	}
}

func TestEditTool_RejectsWhitespaceOnlyOldText(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	file := filepath.Join(dir, "ws-old.txt")
	mustWriteFile(t, file, "hello world\n")

	tool := NewEditTool()
	resp, err := runEdit(t, tool, map[string]any{
		"path":    file,
		"oldText": "   \n\t  ",
		"newText": "replacement",
	})
	if err != nil {
		t.Fatalf("run edit: %v", err)
	}
	if !resp.IsError {
		t.Fatalf("expected error response")
	}
	if !strings.Contains(resp.Content, "oldText and newText cannot be empty") {
		t.Fatalf("unexpected error content: %q", resp.Content)
	}
	if !strings.Contains(resp.Content, "surrounding context") {
		t.Fatalf("expected advice about context in error: %q", resp.Content)
	}
}

func TestEditTool_RejectsWhitespaceOnlyNewText(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	file := filepath.Join(dir, "ws-new.txt")
	mustWriteFile(t, file, "hello world\n")

	tool := NewEditTool()
	resp, err := runEdit(t, tool, map[string]any{
		"path":    file,
		"oldText": "world",
		"newText": "   \n\t  ",
	})
	if err != nil {
		t.Fatalf("run edit: %v", err)
	}
	if !resp.IsError {
		t.Fatalf("expected error response")
	}
	if !strings.Contains(resp.Content, "oldText and newText cannot be empty") {
		t.Fatalf("unexpected error content: %q", resp.Content)
	}
	if !strings.Contains(resp.Content, "surrounding context") {
		t.Fatalf("expected advice about context in error: %q", resp.Content)
	}
}
