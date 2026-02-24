package test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

var tsHelperPath string

func init() {
	// Find the compiled TypeScript helper binary
	// Try multiple locations
	possiblePaths := []string{
		"../../tui-test-helper/dist/tui-test-helper",
		"../tui-test-helper/dist/tui-test-helper",
		"./tui-test-helper/dist/tui-test-helper",
	}

	if runtime.GOOS == "windows" {
		for i := range possiblePaths {
			possiblePaths[i] += ".exe"
		}
	}

	for _, p := range possiblePaths {
		absPath, err := filepath.Abs(p)
		if err == nil {
			if _, err := os.Stat(absPath); err == nil {
				tsHelperPath = absPath
				break
			}
		}
	}

	// Also check environment variable
	if envPath := os.Getenv("TUI_TEST_HELPER"); envPath != "" {
		if _, err := os.Stat(envPath); err == nil {
			tsHelperPath = envPath
		}
	}
}

// TsHelperAvailable checks if the TypeScript helper is available
func TsHelperAvailable() bool {
	return tsHelperPath != ""
}

// TsHelperPath returns the path to the TypeScript helper
func TsHelperPath() string {
	return tsHelperPath
}

// RunTsHelper runs the TypeScript helper with given arguments and stdin
func RunTsHelper(t *testing.T, stdin string, args ...string) (string, error) {
	if !TsHelperAvailable() {
		t.Skip("TypeScript helper not available. Run 'bun run compile' in packages/tui-test-helper")
	}

	cmd := exec.Command(tsHelperPath, args...)
	if stdin != "" {
		cmd.Stdin = strings.NewReader(stdin)
	}

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	if err != nil {
		return "", fmt.Errorf("ts-helper failed: %w\nstderr: %s", err, stderr.String())
	}

	return strings.TrimSpace(stdout.String()), nil
}

// TsVisibleWidth calculates visible width using TypeScript reference
func TsVisibleWidth(t *testing.T, text string) int {
	output, err := RunTsHelper(t, text, "visible-width")
	if err != nil {
		t.Fatalf("visible-width failed: %v", err)
	}

	var width int
	fmt.Sscanf(output, "%d", &width)
	return width
}

// TsTruncate truncates text using TypeScript reference
func TsTruncate(t *testing.T, text string, maxWidth int, ellipsis ...string) string {
	ell := "..."
	if len(ellipsis) > 0 {
		ell = ellipsis[0]
	}

	stdin := text
	output, err := RunTsHelper(t, stdin, "truncate", fmt.Sprintf("%d", maxWidth), ell)
	if err != nil {
		t.Fatalf("truncate failed: %v", err)
	}

	return output
}

// TsRenderMarkdown renders markdown using TypeScript reference
func TsRenderMarkdown(t *testing.T, content string, width int) []string {
	output, err := RunTsHelper(t, content, "render-markdown", fmt.Sprintf("%d", width))
	if err != nil {
		t.Fatalf("render-markdown failed: %v", err)
	}

	if output == "" {
		return []string{}
	}

	return strings.Split(output, "\n")
}

// TsXtermRender renders content through xterm and returns viewport
func TsXtermRender(t *testing.T, cols, rows int, content string) []string {
	output, err := RunTsHelper(t, content, "xterm-render", fmt.Sprintf("%d", cols), fmt.Sprintf("%d", rows))
	if err != nil {
		t.Fatalf("xterm-render failed: %v", err)
	}

	if output == "" {
		return []string{}
	}

	return strings.Split(output, "\n")
}

// XtermCellStyle represents cell style information
type XtermCellStyle struct {
	IsItalic int `json:"isItalic"`
}

// TsXtermCellStyle gets cell style at position using TypeScript reference
func TsXtermCellStyle(t *testing.T, cols, rows, checkRow, checkCol int, content string) *XtermCellStyle {
	output, err := RunTsHelper(t, content,
		"xterm-cell",
		fmt.Sprintf("%d", cols),
		fmt.Sprintf("%d", rows),
		fmt.Sprintf("%d", checkRow),
		fmt.Sprintf("%d", checkCol),
	)
	if err != nil {
		t.Fatalf("xterm-cell failed: %v", err)
	}

	var style XtermCellStyle
	if err := json.Unmarshal([]byte(output), &style); err != nil {
		t.Fatalf("Failed to parse xterm-cell output: %v\noutput: %s", err, output)
	}

	return &style
}

// CompareWithTolerance compares Go and TypeScript outputs with optional tolerance
func CompareWithTolerance(t *testing.T, goOutput, tsOutput, description string, tolerance ...func(string, string) bool) {
	if goOutput == tsOutput {
		return // Exact match
	}

	// Check custom tolerances
	for _, tol := range tolerance {
		if tol(goOutput, tsOutput) {
			return
		}
	}

	t.Errorf("%s mismatch:\nGo: %q\nTS: %q", description, goOutput, tsOutput)
}

// CompareLines compares Go and TypeScript line arrays
func CompareLines(t *testing.T, goLines, tsLines []string, description string) {
	if len(goLines) != len(tsLines) {
		t.Errorf("%s: line count mismatch: Go=%d, TS=%d", description, len(goLines), len(tsLines))
		return
	}

	for i := 0; i < len(goLines) && i < len(tsLines); i++ {
		if goLines[i] != tsLines[i] {
			t.Errorf("%s line %d mismatch:\nGo: %q\nTS: %q", description, i, goLines[i], tsLines[i])
		}
	}
}

// StripAnsi removes ANSI escape codes
func StripAnsi(s string) string {
	var result strings.Builder
	inEscape := false
	for _, r := range s {
		if inEscape {
			if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' {
				inEscape = false
			}
			continue
		}
		if r == '\x1b' {
			inEscape = true
			continue
		}
		result.WriteRune(r)
	}
	return result.String()
}
