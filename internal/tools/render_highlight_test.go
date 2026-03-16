package tools

import (
	"regexp"
	"strings"
	"testing"
)

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
	if addedLine == "" {
		t.Fatalf("expected added line in diff, got %q", diff)
	}
	if removedLine == "" {
		t.Fatalf("expected removed line in diff, got %q", diff)
	}
}
