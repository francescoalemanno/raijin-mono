package tools

import (
	"regexp"
	"strings"
	"testing"

	"github.com/francescoalemanno/raijin-mono/internal/theme"
)

func ansiPrefix(style func(string) string) string {
	const marker = "__marker__"
	styled := style(marker)
	return strings.TrimSuffix(styled, marker+"\x1b[0m")
}

func TestRenderCodePreviewHighlightsByPath(t *testing.T) {
	t.Parallel()

	preview := renderCodePreview("main.go", "package main\nfunc main() {}")
	if !strings.Contains(preview, "\x1b[") {
		t.Fatalf("expected ANSI-highlighted output, got %q", preview)
	}
}

func TestRenderDiffPreviewHighlightsContextCode(t *testing.T) {
	t.Parallel()

	oldText := "package main\nfunc main() {\n\tprintln(\"old\")\n}\n"
	newText := "package main\nfunc main() {\n\tprintln(\"new\")\n}\n"

	diff := renderDiffPreview("main.go", oldText, newText)
	if diff == "" {
		t.Fatalf("expected non-empty diff")
	}

	escapePattern := regexp.MustCompile(`\x1b\[[0-9;]*m`)

	var contextLine string
	for line := range strings.SplitSeq(diff, "\n") {
		plain := escapePattern.ReplaceAllString(line, "")
		if strings.Contains(plain, "package") && strings.Contains(plain, "main") && strings.HasPrefix(plain, "  ") {
			contextLine = line
			break
		}
	}
	if contextLine == "" {
		t.Fatalf("expected context line with code in diff: %q", diff)
	}
	if !strings.Contains(contextLine, "\x1b[") {
		t.Fatalf("expected syntax-highlighted context line, got %q", contextLine)
	}
}

func TestRenderDiffPreviewNeutralLinesAreMuted(t *testing.T) {
	t.Parallel()

	oldText := "package main\nfunc main() {\n\tprintln(\"old\")\n}\n"
	newText := "package main\nfunc main() {\n\tprintln(\"new\")\n}\n"

	diff := renderDiffPreview("main.go", oldText, newText)
	if diff == "" {
		t.Fatalf("expected non-empty diff")
	}

	escapePattern := regexp.MustCompile(`\x1b\[[0-9;]*m`)
	var contextLine string
	for line := range strings.SplitSeq(diff, "\n") {
		plain := escapePattern.ReplaceAllString(line, "")
		if strings.HasPrefix(plain, "  ") {
			contextLine = line
			break
		}
	}
	if contextLine == "" {
		t.Fatalf("expected context line in diff: %q", diff)
	}
	if !strings.Contains(contextLine, ansiPrefix(theme.Default.Muted.Ansi24)) {
		t.Fatalf("expected muted foreground in context line, got %q", contextLine)
	}
}

func TestRenderDiffPreviewColorsAddedAndRemovedWithForeground(t *testing.T) {
	t.Parallel()

	oldText := "package main\nfunc main() {\n\tprintln(\"old\")\n}\n"
	newText := "package main\nfunc main() {\n\tprintln(\"new\")\n}\n"

	diff := renderDiffPreview("main.go", oldText, newText)
	if diff == "" {
		t.Fatalf("expected non-empty diff")
	}

	escapePattern := regexp.MustCompile(`\x1b\[[0-9;]*m`)
	var addedLine, removedLine string
	for line := range strings.SplitSeq(diff, "\n") {
		plain := escapePattern.ReplaceAllString(line, "")
		if strings.HasPrefix(plain, "+") {
			addedLine = line
		}
		if strings.HasPrefix(plain, "-") {
			removedLine = line
		}
	}
	if addedLine == "" || !strings.Contains(addedLine, ansiPrefix(theme.Default.DiffAdded.Ansi24)) {
		t.Fatalf("expected added line with light green foreground, got %q", addedLine)
	}
	if removedLine == "" || !strings.Contains(removedLine, ansiPrefix(theme.Default.DiffRemoved.Ansi24)) {
		t.Fatalf("expected removed line with light red foreground, got %q", removedLine)
	}
	if strings.Contains(addedLine, "\x1b[48;2;") || strings.Contains(removedLine, "\x1b[48;2;") {
		t.Fatalf("did not expect background colors in diff lines")
	}
}
