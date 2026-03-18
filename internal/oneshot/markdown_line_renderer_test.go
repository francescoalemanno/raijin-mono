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

func TestLineMarkdownRendererKeepsUnderscoresInsideIdentifiers(t *testing.T) {
	r := newLineMarkdownRenderer()
	got := ansiRE.ReplaceAllString(r.RenderLine("Use $RAIJIN_BIN and foo_bar_baz and internal/shellinit/shellinit_test.go"), "")

	if !strings.Contains(got, "$RAIJIN_BIN") {
		t.Fatalf("expected env var to remain intact, got %q", got)
	}
	if !strings.Contains(got, "foo_bar_baz") {
		t.Fatalf("expected identifier with underscores to remain intact, got %q", got)
	}
	if !strings.Contains(got, "shellinit_test.go") {
		t.Fatalf("expected file name with underscores to remain intact, got %q", got)
	}
}

func TestLineMarkdownRendererUnderscoreEmphasisStillWorksAtWordBoundaries(t *testing.T) {
	r := newLineMarkdownRenderer()
	got := ansiRE.ReplaceAllString(r.RenderLine("Use _italics_ and __bold__ here"), "")

	if !strings.Contains(got, "Use italics and bold here") {
		t.Fatalf("expected underscore emphasis to render, got %q", got)
	}
	if strings.Contains(got, "_italics_") || strings.Contains(got, "__bold__") {
		t.Fatalf("expected underscore markers removed, got %q", got)
	}
}

func TestLineMarkdownRendererKeepsLiteralGlobCharacters(t *testing.T) {
	r := newLineMarkdownRenderer()
	got := ansiRE.ReplaceAllString(r.RenderLine("Handle special chars like ?, *, and foo*bar literally."), "")

	if !strings.Contains(got, "?, *, and foo*bar") {
		t.Fatalf("expected literal glob characters to remain intact, got %q", got)
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
	if len(lines) < 5 {
		t.Fatalf("expected boxed table with at least 5 lines, got %d: %q", len(lines), rendered)
	}
	if !strings.HasPrefix(lines[0], tableTopLeft) || !strings.HasSuffix(lines[0], tableTopRight) {
		t.Fatalf("expected top border, got %q", lines[0])
	}
	if !strings.HasPrefix(lines[len(lines)-1], tableBottomLeft) || !strings.HasSuffix(lines[len(lines)-1], tableBottomRight) {
		t.Fatalf("expected bottom border, got %q", lines[len(lines)-1])
	}

	// All lines should have the same number of separators at the same positions.
	basePipes := tableBoundaryPositions(lines[0])
	if len(basePipes) < 3 {
		t.Fatalf("expected at least 3 column boundaries, got %v in %q", basePipes, lines[0])
	}
	for _, line := range lines[1:] {
		pipes := tableBoundaryPositions(line)
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

func TestLineMarkdownRendererRespectsRightAlignedColumns(t *testing.T) {
	r := newLineMarkdownRenderer()

	r.RenderLine("| Name | Score |")
	r.RenderLine("| :--- | ---: |")
	r.RenderLine("| Bob | 7 |")
	rendered := ansiRE.ReplaceAllString(r.FlushTable(), "")

	lines := strings.Split(rendered, "\n")
	if len(lines) < 5 {
		t.Fatalf("expected boxed table with at least 5 lines, got %q", rendered)
	}

	dataLine := findLineContaining(lines, "Bob")
	if dataLine == "" {
		t.Fatalf("expected data row containing Bob, got %q", rendered)
	}
	cells := strings.Split(dataLine, tableOutputSep)
	if len(cells) < 4 {
		t.Fatalf("expected row with two cells, got %q", dataLine)
	}

	scoreCell := cells[2]
	if strings.TrimSpace(scoreCell) != "7" {
		t.Fatalf("expected score cell content to remain 7, got %q", scoreCell)
	}
	if !strings.HasPrefix(scoreCell, "  ") {
		t.Fatalf("expected right-aligned cell to include left padding, got %q", scoreCell)
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
	if !strings.Contains(rendered, tableRuleMid) {
		t.Fatalf("expected joined separator row, got %q", rendered)
	}
}

func TestLineMarkdownRendererTablesWithoutOuterPipesAlign(t *testing.T) {
	r := newLineMarkdownRenderer()

	r.RenderLine("Name | Score")
	r.RenderLine(":--- | ---:")
	r.RenderLine("Bob | 7")
	rendered := ansiRE.ReplaceAllString(r.FlushTable(), "")

	lines := strings.Split(rendered, "\n")
	if len(lines) < 5 {
		t.Fatalf("expected boxed table with at least 5 lines, got %d: %q", len(lines), rendered)
	}

	basePipes := tableBoundaryPositions(lines[0])
	if len(basePipes) < 2 {
		t.Fatalf("expected at least 2 boundaries, got %v in %q", basePipes, lines[0])
	}
	for _, line := range lines[1:] {
		pipes := tableBoundaryPositions(line)
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
	sp := tableBoundaryPositions(styledLines[0])
	pp := tableBoundaryPositions(plainLines[0])
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
	if !strings.Contains(rendered, tableTopLeft) || !strings.Contains(rendered, tableBottomLeft) {
		t.Fatalf("expected boxed table borders, got %q", rendered)
	}
	// Find the separator line.
	for _, line := range strings.Split(rendered, "\n") {
		if strings.HasPrefix(line, tableRuleLeft) {
			if strings.Contains(line, " "+tableOutputRuleSeg) || strings.Contains(line, tableOutputRuleSeg+" ") {
				t.Fatalf("expected joined separator row without padding spaces, got %q", line)
			}
			seps := tableBoundaryPositions(line)
			if len(seps) != 2 {
				t.Fatalf("expected single-column table to render two outer boundaries, got %q", line)
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
	if len(lines) < 5 {
		t.Fatalf("expected boxed table with at least 5 lines, got %q", rendered)
	}

	// Verify all lines have consistent separators.
	basePipes := tableBoundaryPositions(lines[0])
	for _, line := range lines[1:] {
		pipes := tableBoundaryPositions(line)
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

func tableBoundaryPositions(s string) []int {
	runes := []rune(s)
	boundaries := map[rune]struct{}{
		[]rune(tableOutputSep)[0]:   {},
		[]rune(tableTopLeft)[0]:     {},
		[]rune(tableTopMid)[0]:      {},
		[]rune(tableTopRight)[0]:    {},
		[]rune(tableRuleLeft)[0]:    {},
		[]rune(tableRuleMid)[0]:     {},
		[]rune(tableRuleRight)[0]:   {},
		[]rune(tableBottomLeft)[0]:  {},
		[]rune(tableBottomMid)[0]:   {},
		[]rune(tableBottomRight)[0]: {},
	}
	positions := make([]int, 0, len(runes))
	for i, ch := range runes {
		if _, ok := boundaries[ch]; ok {
			positions = append(positions, i)
		}
	}
	return positions
}

func findLineContaining(lines []string, needle string) string {
	for _, line := range lines {
		if strings.Contains(line, needle) {
			return line
		}
	}
	return ""
}
