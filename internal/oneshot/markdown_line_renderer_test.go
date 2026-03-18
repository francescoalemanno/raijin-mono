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

func TestLineMarkdownRendererTablesAlignColumns(t *testing.T) {
	r := newLineMarkdownRenderer()

	// Feed all table lines (they get buffered).
	r.RenderLine("| Name | Score |")
	r.RenderLine("| :--- | ---: |")
	r.RenderLine("| Bob | 7 |")
	rendered := ansiRE.ReplaceAllString(r.FlushTable(), "")

	lines := strings.Split(rendered, "\n")
	if len(lines) < 3 {
		t.Fatalf("expected at least 3 lines, got %d: %q", len(lines), rendered)
	}

	// All lines should have the same number of separators at the same positions.
	basePipes := tableSepPositions(lines[0])
	if len(basePipes) < 3 {
		t.Fatalf("expected at least 3 column boundaries, got %v in %q", basePipes, lines[0])
	}
	for _, line := range lines[1:] {
		pipes := tableSepPositions(line)
		if len(pipes) != len(basePipes) {
			t.Fatalf("expected same number of separators, got first=%v line=%v (%q)", basePipes, pipes, line)
		}
		for i := range basePipes {
			if pipes[i] != basePipes[i] {
				t.Fatalf("expected aligned boundaries, got first=%v line=%v", basePipes, pipes)
			}
		}
	}
}

func TestLineMarkdownRendererTablesWrapLongCells(t *testing.T) {
	r := newLineMarkdownRenderer()
	r.tableWidth = 40 // force wrapping

	r.RenderLine("| this is a very long table cell that should definitely wrap | 7 |")
	r.RenderLine("| --- | --- |")
	rendered := ansiRE.ReplaceAllString(r.FlushTable(), "")

	lines := strings.Split(rendered, "\n")
	// The first row should have wrapped to multiple visual lines.
	dataLines := 0
	for _, line := range lines {
		if strings.Contains(line, tableOutputSep) && !strings.Contains(line, tableOutputRuleSeg) {
			dataLines++
		}
	}
	if dataLines < 2 {
		t.Fatalf("expected wrapped multi-line table row, got %q", rendered)
	}
	if strings.Contains(rendered, "…") {
		t.Fatalf("expected wrapping instead of truncation, got %q", rendered)
	}
}

func TestLineMarkdownRendererTablesWithoutOuterPipesAlign(t *testing.T) {
	r := newLineMarkdownRenderer()

	r.RenderLine("Name | Score")
	r.RenderLine(":--- | ---:")
	r.RenderLine("Bob | 7")
	rendered := ansiRE.ReplaceAllString(r.FlushTable(), "")

	lines := strings.Split(rendered, "\n")
	if len(lines) < 3 {
		t.Fatalf("expected 3 lines, got %d: %q", len(lines), rendered)
	}

	basePipes := tableSepPositions(lines[0])
	if len(basePipes) < 2 {
		t.Fatalf("expected at least 2 boundaries, got %v in %q", basePipes, lines[0])
	}
	for _, line := range lines[1:] {
		pipes := tableSepPositions(line)
		if len(pipes) != len(basePipes) {
			t.Fatalf("expected same separators, got first=%v line=%v", basePipes, pipes)
		}
		for i := range basePipes {
			if pipes[i] != basePipes[i] {
				t.Fatalf("expected aligned boundaries, got first=%v line=%v", basePipes, pipes)
			}
		}
	}
}

func TestLineMarkdownRendererTablePaddingUsesVisibleWidth(t *testing.T) {
	r := newLineMarkdownRenderer()

	r.RenderLine("| **bold** | 1 |")
	r.RenderLine("| --- | --- |")
	styled := ansiRE.ReplaceAllString(r.FlushTable(), "")

	r2 := newLineMarkdownRenderer()
	r2.RenderLine("| bold | 1 |")
	r2.RenderLine("| --- | --- |")
	plain := ansiRE.ReplaceAllString(r2.FlushTable(), "")

	styledLines := strings.Split(styled, "\n")
	plainLines := strings.Split(plain, "\n")
	if len(styledLines) == 0 || len(plainLines) == 0 {
		t.Fatalf("expected non-empty output, styled=%q plain=%q", styled, plain)
	}

	// Column boundaries should be at the same positions for the first data row.
	sp := tableSepPositions(styledLines[0])
	pp := tableSepPositions(plainLines[0])
	if len(sp) != len(pp) {
		t.Fatalf("expected same separator count, styled=%v plain=%v", sp, pp)
	}
	for i := range sp {
		if sp[i] != pp[i] {
			t.Fatalf("expected markdown formatting not to affect column width, styled=%v plain=%v", sp, pp)
		}
	}
}

func TestLineMarkdownRendererSingleColumnSeparatorRowRenders(t *testing.T) {
	r := newLineMarkdownRenderer()

	r.RenderLine("| A |")
	r.RenderLine("| --- |")
	r.RenderLine("| B |")
	rendered := ansiRE.ReplaceAllString(r.FlushTable(), "")

	if !strings.Contains(rendered, tableOutputRuleSeg) {
		t.Fatalf("expected separator content to render, got %q", rendered)
	}
	// Find the separator line.
	for _, line := range strings.Split(rendered, "\n") {
		if strings.Contains(line, tableOutputRuleSeg) {
			seps := tableSepPositions(line)
			if len(seps) != 2 {
				t.Fatalf("expected single-column table to render two vertical separators, got %q", line)
			}
			break
		}
	}
}

func TestLineMarkdownRendererTableFlushOnNonTableLine(t *testing.T) {
	r := newLineMarkdownRenderer()

	// Table lines are buffered.
	if got := r.RenderLine("| A | B |"); got != "" {
		t.Fatalf("expected empty string while buffering table, got %q", got)
	}
	if got := r.RenderLine("| --- | --- |"); got != "" {
		t.Fatalf("expected empty string while buffering table, got %q", got)
	}
	if got := r.RenderLine("| 1 | 2 |"); got != "" {
		t.Fatalf("expected empty string while buffering table, got %q", got)
	}

	// Non-table line flushes the table.
	got := r.RenderLine("some text after table")
	plain := ansiRE.ReplaceAllString(got, "")
	if !strings.Contains(plain, tableOutputSep) {
		t.Fatalf("expected flushed table to contain separators, got %q", plain)
	}
	if !strings.Contains(plain, "some text after table") {
		t.Fatalf("expected non-table line to appear after flushed table, got %q", plain)
	}
}

func TestLineMarkdownRendererOptimalWidthsNarrowTable(t *testing.T) {
	r := newLineMarkdownRenderer()
	r.tableWidth = 50

	r.RenderLine("| Short | This is a much longer column that needs more space |")
	r.RenderLine("| --- | --- |")
	r.RenderLine("| x | Another long cell with plenty of text |")
	rendered := ansiRE.ReplaceAllString(r.FlushTable(), "")

	lines := strings.Split(rendered, "\n")
	if len(lines) < 3 {
		t.Fatalf("expected at least 3 lines, got %q", rendered)
	}

	// Verify all lines have consistent separators.
	basePipes := tableSepPositions(lines[0])
	for _, line := range lines[1:] {
		pipes := tableSepPositions(line)
		if len(pipes) != len(basePipes) {
			t.Fatalf("inconsistent separators across lines: first=%v this=%v", basePipes, pipes)
		}
		for i := range basePipes {
			if pipes[i] != basePipes[i] {
				t.Fatalf("misaligned columns: first=%v this=%v", basePipes, pipes)
			}
		}
	}
}

func TestLineMarkdownRendererTableBRTag(t *testing.T) {
	r := newLineMarkdownRenderer()

	r.RenderLine("| Name | Info |")
	r.RenderLine("| --- | --- |")
	r.RenderLine("| Alice | Line one<br>Line two |")
	rendered := ansiRE.ReplaceAllString(r.FlushTable(), "")

	if !strings.Contains(rendered, "Line one") || !strings.Contains(rendered, "Line two") {
		t.Fatalf("expected both lines from <br> split, got %q", rendered)
	}
	if strings.Contains(rendered, "<br>") {
		t.Fatalf("expected <br> tag to be removed, got %q", rendered)
	}

	// The <br> should cause a line break — "Line one" and "Line two" should
	// be on separate visual lines within the same cell.
	lines := strings.Split(rendered, "\n")
	foundOne := -1
	foundTwo := -1
	for i, line := range lines {
		if strings.Contains(line, "Line one") {
			foundOne = i
		}
		if strings.Contains(line, "Line two") {
			foundTwo = i
		}
	}
	if foundOne == foundTwo {
		t.Fatalf("expected <br> to produce separate visual lines, got %q", rendered)
	}
}

func TestLineMarkdownRendererTableBRVariants(t *testing.T) {
	for _, tag := range []string{"<br>", "<br/>", "<br />", "<BR>", "<BR/>"} {
		r := newLineMarkdownRenderer()
		r.RenderLine("| X |")
		r.RenderLine("| --- |")
		r.RenderLine("| A" + tag + "B |")
		rendered := ansiRE.ReplaceAllString(r.FlushTable(), "")

		if strings.Contains(rendered, tag) {
			t.Errorf("tag %q should be stripped, got %q", tag, rendered)
		}
		lines := strings.Split(rendered, "\n")
		foundA := -1
		foundB := -1
		for i, line := range lines {
			if strings.Contains(line, "A") {
				foundA = i
			}
			if strings.Contains(line, "B") {
				foundB = i
			}
		}
		if foundA == foundB {
			t.Errorf("tag %q should produce separate lines, got %q", tag, rendered)
		}
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
