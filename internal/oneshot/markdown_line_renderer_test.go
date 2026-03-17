package oneshot

import (
	"regexp"
	"strings"
	"testing"
)

var ansiRE = regexp.MustCompile(`\x1b\[[0-9;]*m`)

func TestLineMarkdownRendererHeadingAndInline(t *testing.T) {
	r := newLineMarkdownRenderer()
	got := r.RenderLine("# Hello **World** and `code`")

	if !strings.Contains(got, "Hello") || !strings.Contains(got, "World") || !strings.Contains(got, "code") {
		t.Fatalf("expected rendered heading text, got %q", got)
	}
	if strings.Contains(got, "# ") || strings.Contains(got, "**World**") || strings.Contains(got, "`code`") {
		t.Fatalf("expected markdown markers removed, got %q", got)
	}
}

func TestLineMarkdownRendererFencesAndCodeState(t *testing.T) {
	r := newLineMarkdownRenderer()

	open := r.RenderLine("```go")
	if !r.inFence {
		t.Fatalf("expected renderer to enter fence state")
	}
	if !strings.Contains(open, "```go") {
		t.Fatalf("expected fence marker to render, got %q", open)
	}

	code := r.RenderLine(`fmt.Println("x")`)
	plain := ansiRE.ReplaceAllString(code, "")
	if !strings.Contains(plain, `fmt.Println("x")`) {
		t.Fatalf("expected code line content, got %q", code)
	}

	_ = r.RenderLine("```")
	if r.inFence {
		t.Fatalf("expected renderer to exit fence state")
	}
}

func TestLineMarkdownRendererTablesUseFixedColumnWidths(t *testing.T) {
	r := newLineMarkdownRenderer()

	header := ansiRE.ReplaceAllString(r.RenderLine("| Name | Score |"), "")
	row := ansiRE.ReplaceAllString(r.RenderLine("| Bob | 7 |"), "")
	sep := ansiRE.ReplaceAllString(r.RenderLine("| :--- | ---: |"), "")

	headerPipes := tableSepPositions(header)
	rowPipes := tableSepPositions(row)
	sepPipes := tableSepPositions(sep)

	if len(headerPipes) < 3 || len(rowPipes) != len(headerPipes) || len(sepPipes) != len(headerPipes) {
		t.Fatalf("expected consistent table boundaries, got header=%q row=%q sep=%q", header, row, sep)
	}
	for i := range headerPipes {
		if headerPipes[i] != rowPipes[i] || headerPipes[i] != sepPipes[i] {
			t.Fatalf("expected aligned column boundaries, got header=%v row=%v sep=%v", headerPipes, rowPipes, sepPipes)
		}
	}
}

func TestLineMarkdownRendererTablesWrapLongCells(t *testing.T) {
	r := newLineMarkdownRenderer()

	rendered := ansiRE.ReplaceAllString(r.RenderLine("| this is a very long table cell that should wrap | 7 |"), "")
	lines := strings.Split(rendered, "\n")
	if len(lines) < 2 {
		t.Fatalf("expected wrapped multi-line table row, got %q", rendered)
	}

	basePipes := tableSepPositions(lines[0])
	if len(basePipes) < 3 {
		t.Fatalf("expected table row boundaries, got %q", lines[0])
	}
	for _, line := range lines[1:] {
		pipes := tableSepPositions(line)
		if len(pipes) != len(basePipes) {
			t.Fatalf("expected same number of table separators, got first=%v line=%v", basePipes, pipes)
		}
		for i := range basePipes {
			if pipes[i] != basePipes[i] {
				t.Fatalf("expected wrapped table lines to align, got first=%v line=%v", basePipes, pipes)
			}
		}
	}
	if strings.Contains(rendered, "…") {
		t.Fatalf("expected wrapping instead of truncation, got %q", rendered)
	}
}

func TestLineMarkdownRendererTablesWithoutOuterPipesAlign(t *testing.T) {
	r := newLineMarkdownRenderer()

	header := ansiRE.ReplaceAllString(r.RenderLine("Name | Score"), "")
	row := ansiRE.ReplaceAllString(r.RenderLine("Bob | 7"), "")
	sep := ansiRE.ReplaceAllString(r.RenderLine(":--- | ---:"), "")

	headerPipes := tableSepPositions(header)
	rowPipes := tableSepPositions(row)
	sepPipes := tableSepPositions(sep)

	if len(headerPipes) < 2 || len(rowPipes) != len(headerPipes) || len(sepPipes) != len(headerPipes) {
		t.Fatalf("expected consistent table boundaries without outer pipes, got header=%q row=%q sep=%q", header, row, sep)
	}
	for i := range headerPipes {
		if headerPipes[i] != rowPipes[i] || headerPipes[i] != sepPipes[i] {
			t.Fatalf("expected aligned boundaries without outer pipes, got header=%v row=%v sep=%v", headerPipes, rowPipes, sepPipes)
		}
	}
}

func TestLineMarkdownRendererTablePaddingUsesVisibleWidth(t *testing.T) {
	r := newLineMarkdownRenderer()

	styled := ansiRE.ReplaceAllString(r.RenderLine("| **bold** | 1 |"), "")
	plain := ansiRE.ReplaceAllString(r.RenderLine("| bold | 1 |"), "")

	if tableSepPositions(styled)[1] != tableSepPositions(plain)[1] {
		t.Fatalf("expected markdown formatting not to affect column width, styled=%q plain=%q", styled, plain)
	}
}

func TestLineMarkdownRendererSingleColumnSeparatorRowRenders(t *testing.T) {
	r := newLineMarkdownRenderer()

	rendered := ansiRE.ReplaceAllString(r.RenderLine("| --- |"), "")
	if !strings.Contains(rendered, tableOutputRuleSeg) {
		t.Fatalf("expected separator content to render, got %q", rendered)
	}
	seps := tableSepPositions(rendered)
	if len(seps) != 2 {
		t.Fatalf("expected single-column table to render two vertical separators, got %q", rendered)
	}
}

func tableSepPositions(s string) []int {
	runes := []rune(s)
	sep := []rune(tableOutputSep)[0]
	positions := make([]int, 0, strings.Count(s, tableOutputSep))
	for i, ch := range runes {
		if ch == sep {
			positions = append(positions, i)
		}
	}
	return positions
}
