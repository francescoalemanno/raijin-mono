package oneshot

import (
	"bytes"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/alecthomas/chroma/v2/quick"
	"github.com/charmbracelet/lipgloss"
)

var (
	mdHeadingRE       = regexp.MustCompile(`^(#{1,6})\s+(.*)$`)
	mdQuoteRE         = regexp.MustCompile(`^\s*>\s?(.*)$`)
	mdListRE          = regexp.MustCompile(`^(\s*)([-*+]|\d+[.)])\s+(.*)$`)
	mdFenceRE         = regexp.MustCompile("^\\s*```([a-zA-Z0-9_+-]*)\\s*$")
	mdTableSepCellRE  = regexp.MustCompile(`^:?-{3,}:?$`)
	mdTableMarkerChar = '|'
	mdBRTagRE         = regexp.MustCompile(`(?i)<br\s*/?>`)
)

const defaultTableMaxWidth = 100

const (
	tableOutputSep     = "│"
	tableOutputRuleSeg = "─"
)

type lineMarkdownRenderer struct {
	inFence bool
	lang    string

	// table buffering: rows are accumulated until a non-table line arrives
	tableRows  [][]string // each element is the parsed cells for one row
	tableWidth int        // total width budget for the table

	// width cache: avoids repeated regex work during DP height-map computation
	widthCache map[string]int

	headingStyles []lipgloss.Style
	quoteStyle    lipgloss.Style
	listStyle     lipgloss.Style
	fenceStyle    lipgloss.Style
	inlineCode    lipgloss.Style
	boldStyle     lipgloss.Style
	italicStyle   lipgloss.Style
	linkStyle     lipgloss.Style
	codeFallback  lipgloss.Style
}

func newLineMarkdownRenderer() *lineMarkdownRenderer {
	return newLineMarkdownRendererWithWidth(defaultTableMaxWidth)
}

func newLineMarkdownRendererWithWidth(tableWidth int) *lineMarkdownRenderer {
	if tableWidth <= 0 {
		tableWidth = defaultTableMaxWidth
	}
	return &lineMarkdownRenderer{
		tableWidth: tableWidth,
		headingStyles: []lipgloss.Style{
			oneshotAccentStyle.Bold(true),
			oneshotAccentStyle.Bold(true),
			oneshotWarningStyle.Bold(true),
			oneshotWarningStyle.Bold(true),
			oneshotNormalStyle.Bold(true),
			oneshotNormalStyle.Bold(true),
		},
		quoteStyle:   oneshotMutedStyle,
		listStyle:    oneshotAccentStyle.Bold(true),
		fenceStyle:   oneshotMutedStyle,
		inlineCode:   oneshotWarningStyle,
		boldStyle:    lipgloss.NewStyle().Bold(true),
		italicStyle:  lipgloss.NewStyle().Italic(true),
		linkStyle:    oneshotAccentStyle.Underline(true),
		codeFallback: oneshotWarningStyle,
	}
}

func (r *lineMarkdownRenderer) RenderLine(line string) string {
	line = strings.TrimRight(line, "\r")

	if matches := mdFenceRE.FindStringSubmatch(line); matches != nil {
		var prefix string
		if len(r.tableRows) > 0 {
			prefix = r.flushTable() + "\n"
		}
		if r.inFence {
			r.inFence = false
			r.lang = ""
		} else {
			r.inFence = true
			r.lang = strings.TrimSpace(matches[1])
		}
		return prefix + r.fenceStyle.Render(strings.TrimSpace(line))
	}

	if r.inFence {
		return r.renderCodeLine(line)
	}
	return r.renderMarkdownLine(line)
}

// FlushTable renders any buffered table rows. Call when the stream ends.
func (r *lineMarkdownRenderer) FlushTable() string {
	if len(r.tableRows) == 0 {
		return ""
	}
	return r.flushTable()
}

func (r *lineMarkdownRenderer) renderCodeLine(line string) string {
	if strings.TrimSpace(line) == "" {
		return ""
	}
	if strings.TrimSpace(r.lang) == "" {
		return r.codeFallback.Render(line)
	}

	var out bytes.Buffer
	if err := quick.Highlight(&out, line, r.lang, "terminal16m", markdownCodeStyleName()); err != nil {
		return r.codeFallback.Render(line)
	}
	return strings.TrimRight(out.String(), "\n")
}

func markdownCodeStyleName() string {
	if lipgloss.HasDarkBackground() {
		return "monokai"
	}
	return "github"
}

func (r *lineMarkdownRenderer) renderMarkdownLine(line string) string {
	if strings.TrimSpace(line) == "" {
		if len(r.tableRows) > 0 {
			return r.flushTable()
		}
		return ""
	}
	if cells, ok := parseMarkdownTableCells(line); ok {
		r.tableRows = append(r.tableRows, cells)
		return ""
	}

	var prefix string
	if len(r.tableRows) > 0 {
		prefix = r.flushTable() + "\n"
	}

	if m := mdHeadingRE.FindStringSubmatch(line); m != nil {
		level := len(m[1])
		text := r.renderInline(strings.TrimSpace(m[2]))
		idx := min(max(level, 1), len(r.headingStyles)) - 1
		return prefix + r.headingStyles[idx].Render(text)
	}

	if m := mdQuoteRE.FindStringSubmatch(line); m != nil {
		body := r.renderInline(strings.TrimSpace(m[1]))
		return prefix + r.quoteStyle.Render("│ ") + body
	}

	if m := mdListRE.FindStringSubmatch(line); m != nil {
		indent := m[1]
		marker := m[2]
		body := r.renderInline(strings.TrimSpace(m[3]))
		return prefix + indent + r.listStyle.Render(marker) + " " + body
	}

	return prefix + r.renderInline(line)
}

func parseMarkdownTableCells(line string) ([]string, bool) {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" {
		return nil, false
	}
	pipeCount := strings.Count(trimmed, string(mdTableMarkerChar))
	if pipeCount < 1 {
		return nil, false
	}

	hasLeading := strings.HasPrefix(trimmed, string(mdTableMarkerChar))
	hasTrailing := strings.HasSuffix(trimmed, string(mdTableMarkerChar))

	parts := strings.Split(trimmed, string(mdTableMarkerChar))
	start := 0
	end := len(parts)
	if hasLeading {
		start++
	}
	if hasTrailing {
		end--
	}
	minCells := 2
	if hasLeading && hasTrailing {
		minCells = 1
	}
	if end-start < minCells {
		return nil, false
	}

	cells := make([]string, 0, end-start)
	for _, part := range parts[start:end] {
		cells = append(cells, strings.TrimSpace(part))
	}
	return cells, true
}

// flushTable renders all buffered table rows using DP-optimal column widths
// and resets the buffer.
func (r *lineMarkdownRenderer) flushTable() string {
	rows := r.tableRows
	r.tableRows = nil
	if len(rows) == 0 {
		return ""
	}

	numCols := 0
	for _, row := range rows {
		if len(row) > numCols {
			numCols = len(row)
		}
	}
	if numCols == 0 {
		return ""
	}

	// Normalize: pad short rows with empty cells.
	for i := range rows {
		for len(rows[i]) < numCols {
			rows[i] = append(rows[i], "")
		}
	}

	// Separate content rows from separator rows.
	type rowInfo struct {
		cells []string
		isSep bool
	}
	rowInfos := make([]rowInfo, len(rows))
	for i, row := range rows {
		rowInfos[i] = rowInfo{cells: row, isSep: isTableSeparatorRow(row)}
	}

	// Compute content-only rows for height calculations (skip separator rows).
	var contentRows [][]string
	for _, ri := range rowInfos {
		if !ri.isSep {
			contentRows = append(contentRows, ri.cells)
		}
	}

	widths := r.optimalColumnWidths(contentRows, numCols)

	// Render the full table.
	var out strings.Builder
	for i, ri := range rowInfos {
		if i > 0 {
			out.WriteString("\n")
		}
		if ri.isSep {
			out.WriteString(r.renderTableSepRow(ri.cells, widths))
		} else {
			out.WriteString(r.renderTableDataRow(ri.cells, widths))
		}
	}
	return out.String()
}

// optimalColumnWidths uses DP to find column widths that minimize total table height.
func (r *lineMarkdownRenderer) optimalColumnWidths(contentRows [][]string, numCols int) []int {
	r.widthCache = make(map[string]int)
	defer func() { r.widthCache = nil }()
	// Overhead per column: "│ " before + " " after = 3 chars, plus final "│" = 1.
	// Total overhead = numCols*3 + 1.
	overhead := numCols*3 + 1
	available := r.tableWidth - overhead
	if available < numCols {
		available = numCols // at least 1 char per column
	}

	// For each column, compute the minimum width (longest single word) and
	// the natural width (longest unwrapped line).
	minWidths := make([]int, numCols)
	natWidths := make([]int, numCols)
	for col := 0; col < numCols; col++ {
		minW := 1
		natW := 1
		for _, row := range contentRows {
			// Split on <br> to get individual segments; natural width
			// is the max segment width, not the whole cell.
			segments := mdBRTagRE.Split(row[col], -1)
			for _, seg := range segments {
				seg = strings.TrimSpace(seg)
				segW := r.inlineDisplayWidth(seg)
				if segW > natW {
					natW = segW
				}
				for _, word := range strings.Fields(seg) {
					ww := r.inlineDisplayWidth(word)
					if ww > minW {
						minW = ww
					}
				}
			}
		}
		minWidths[col] = minW
		natWidths[col] = natW
	}

	// Clamp min widths so they don't exceed available.
	totalMin := 0
	for _, mw := range minWidths {
		totalMin += mw
	}
	if totalMin > available {
		// Proportionally scale down.
		for i := range minWidths {
			minWidths[i] = max(1, minWidths[i]*available/totalMin)
		}
	}

	// If all columns fit at natural width, use that.
	totalNat := 0
	for _, nw := range natWidths {
		totalNat += nw
	}
	if totalNat <= available {
		return natWidths
	}

	// Build height maps: heightMap[col][w] = max lines across all content rows
	// for that column at width w. Width ranges from minWidths[col]..available.
	type heightMap struct {
		minW    int
		heights []int // indexed by w - minW
	}
	hMaps := make([]heightMap, numCols)
	for col := 0; col < numCols; col++ {
		mw := minWidths[col]
		maxW := available
		if natWidths[col] < maxW {
			maxW = natWidths[col]
		}
		h := heightMap{minW: mw, heights: make([]int, maxW-mw+1)}
		for wi := range h.heights {
			w := mw + wi
			rowH := 0
			for _, row := range contentRows {
				lines := len(r.wrapCellToWidth(row[col], w))
				if lines > rowH {
					rowH = lines
				}
			}
			h.heights[wi] = rowH
		}
		hMaps[col] = h
	}

	getHeight := func(col, w int) int {
		hm := hMaps[col]
		if w >= hm.minW+len(hm.heights) {
			return hm.heights[len(hm.heights)-1]
		}
		if w < hm.minW {
			return hm.heights[0]
		}
		return hm.heights[w-hm.minW]
	}

	// DP: dp[w] = minimum total height for columns 0..k-1 using total width w.
	// We iterate column by column, swapping between two slices.
	W := available + 1
	prev := make([]int, W)
	curr := make([]int, W)
	const inf = 1 << 30

	// Initialize for column 0.
	for w := 0; w < W; w++ {
		if w < minWidths[0] {
			prev[w] = inf
		} else {
			prev[w] = getHeight(0, w)
		}
	}

	// Fill for remaining columns.
	for col := 1; col < numCols; col++ {
		for w := 0; w < W; w++ {
			curr[w] = inf
		}
		mw := minWidths[col]
		maxW := natWidths[col]
		for w := 0; w < W; w++ {
			if prev[w] >= inf {
				continue
			}
			// Try allocating j to current column.
			for j := mw; j <= maxW && w+j < W; j++ {
				h := getHeight(col, j)
				total := prev[w] + h
				if total < curr[w+j] {
					curr[w+j] = total
				}
			}
		}
		prev, curr = curr, prev
	}

	// Find the best total width.
	bestW := 0
	bestH := inf
	for w := 0; w < W; w++ {
		if prev[w] < bestH {
			bestH = prev[w]
			bestW = w
		}
	}

	// Backtrack to find per-column widths.
	// Re-run DP storing the dp tables for backtracking.
	dpTables := make([][]int, numCols)
	dpTables[0] = make([]int, W)
	for w := 0; w < W; w++ {
		if w < minWidths[0] {
			dpTables[0][w] = inf
		} else {
			dpTables[0][w] = getHeight(0, w)
		}
	}
	for col := 1; col < numCols; col++ {
		dpTables[col] = make([]int, W)
		for w := 0; w < W; w++ {
			dpTables[col][w] = inf
		}
		mw := minWidths[col]
		maxW := natWidths[col]
		for w := 0; w < W; w++ {
			if dpTables[col-1][w] >= inf {
				continue
			}
			for j := mw; j <= maxW && w+j < W; j++ {
				h := getHeight(col, j)
				total := dpTables[col-1][w] + h
				if total < dpTables[col][w+j] {
					dpTables[col][w+j] = total
				}
			}
		}
	}

	widths := make([]int, numCols)
	remaining := bestW
	for col := numCols - 1; col >= 1; col-- {
		mw := minWidths[col]
		maxW := natWidths[col]
		bestJ := mw
		bestVal := inf
		for j := mw; j <= maxW && j <= remaining; j++ {
			prev := remaining - j
			if prev < 0 || dpTables[col-1][prev] >= inf {
				continue
			}
			val := dpTables[col-1][prev] + getHeight(col, j)
			if val < bestVal {
				bestVal = val
				bestJ = j
			}
		}
		widths[col] = bestJ
		remaining -= bestJ
	}
	widths[0] = remaining

	return widths
}

func (r *lineMarkdownRenderer) renderTableSepRow(cells []string, widths []int) string {
	var out strings.Builder
	out.WriteString(tableOutputSep)
	for i, cell := range cells {
		w := widths[i]
		out.WriteString(" ")
		out.WriteString(renderTableSeparatorCell(cell, w))
		out.WriteString(" ")
		out.WriteString(tableOutputSep)
	}
	return out.String()
}

func (r *lineMarkdownRenderer) renderTableDataRow(cells []string, widths []int) string {
	wrapped := make([][]string, len(cells))
	maxLines := 1
	for i, cell := range cells {
		wrapped[i] = r.wrapCellToWidth(cell, widths[i])
		if len(wrapped[i]) > maxLines {
			maxLines = len(wrapped[i])
		}
	}

	var out strings.Builder
	for row := 0; row < maxLines; row++ {
		if row > 0 {
			out.WriteString("\n")
		}
		out.WriteString(tableOutputSep)
		for col := range cells {
			segment := ""
			if row < len(wrapped[col]) {
				segment = wrapped[col][row]
			}
			out.WriteString(" ")
			if segment != "" {
				rendered := r.renderInline(segment)
				out.WriteString(rendered)
				if pad := widths[col] - r.inlineDisplayWidth(segment); pad > 0 {
					out.WriteString(strings.Repeat(" ", pad))
				}
			} else {
				out.WriteString(strings.Repeat(" ", widths[col]))
			}
			out.WriteString(" ")
			out.WriteString(tableOutputSep)
		}
	}
	return out.String()
}

func isTableSeparatorRow(cells []string) bool {
	if len(cells) == 0 {
		return false
	}
	for _, cell := range cells {
		if !mdTableSepCellRE.MatchString(cell) {
			return false
		}
	}
	return true
}

func renderTableSeparatorCell(cell string, width int) string {
	if width <= 0 {
		return ""
	}
	left := strings.HasPrefix(cell, ":")
	right := strings.HasSuffix(cell, ":")
	switch {
	case left && right:
		if width == 1 {
			return ":"
		}
		if width == 2 {
			return "::"
		}
		return ":" + strings.Repeat(tableOutputRuleSeg, width-2) + ":"
	case left:
		if width == 1 {
			return ":"
		}
		return ":" + strings.Repeat(tableOutputRuleSeg, width-1)
	case right:
		if width == 1 {
			return ":"
		}
		return strings.Repeat(tableOutputRuleSeg, width-1) + ":"
	default:
		return strings.Repeat(tableOutputRuleSeg, width)
	}
}

// wrapCellToWidth splits s on <br> tags first, then word-wraps each segment.
func (r *lineMarkdownRenderer) wrapCellToWidth(s string, width int) []string {
	segments := mdBRTagRE.Split(s, -1)
	if len(segments) <= 1 {
		return r.wrapToWidth(s, width)
	}
	var lines []string
	for _, seg := range segments {
		lines = append(lines, r.wrapToWidth(strings.TrimSpace(seg), width)...)
	}
	if len(lines) == 0 {
		return []string{""}
	}
	return lines
}

func (r *lineMarkdownRenderer) wrapToWidth(s string, width int) []string {
	if width <= 0 {
		return []string{""}
	}
	trimmed := strings.TrimSpace(s)
	if trimmed == "" {
		return []string{""}
	}
	words := strings.Fields(trimmed)
	if len(words) == 0 {
		return []string{""}
	}

	lines := make([]string, 0, 2)
	current := ""
	for _, word := range words {
		if current == "" {
			if r.inlineDisplayWidth(word) <= width {
				current = word
				continue
			}
			parts := r.splitTokenByWidth(word, width)
			if len(parts) > 1 {
				lines = append(lines, parts[:len(parts)-1]...)
			}
			current = parts[len(parts)-1]
			continue
		}

		candidate := current + " " + word
		if r.inlineDisplayWidth(candidate) <= width {
			current = candidate
			continue
		}
		lines = append(lines, current)
		if r.inlineDisplayWidth(word) <= width {
			current = word
			continue
		}
		parts := r.splitTokenByWidth(word, width)
		if len(parts) > 1 {
			lines = append(lines, parts[:len(parts)-1]...)
		}
		current = parts[len(parts)-1]
	}

	if current != "" {
		lines = append(lines, current)
	}
	if len(lines) == 0 {
		return []string{""}
	}
	return lines
}

func (r *lineMarkdownRenderer) splitTokenByWidth(token string, width int) []string {
	if width <= 0 || token == "" {
		return []string{""}
	}

	parts := make([]string, 0, 2)
	current := ""

	for _, ch := range token {
		next := current + string(ch)
		if r.inlineDisplayWidth(next) <= width {
			current = next
			continue
		}

		if current != "" {
			parts = append(parts, current)
			current = string(ch)
			if r.inlineDisplayWidth(current) > width {
				parts = append(parts, current)
				current = ""
			}
			continue
		}
		parts = append(parts, string(ch))
	}
	if current != "" {
		parts = append(parts, current)
	}

	if len(parts) == 0 {
		return []string{""}
	}
	return parts
}

func (r *lineMarkdownRenderer) inlineDisplayWidth(s string) int {
	if r.widthCache != nil {
		if w, ok := r.widthCache[s]; ok {
			return w
		}
	}
	w := lipgloss.Width(r.renderInlinePlain(s))
	if r.widthCache != nil {
		r.widthCache[s] = w
	}
	return w
}

func (r *lineMarkdownRenderer) renderInlinePlain(line string) string {
	return r.renderInlineTokens(line, true)
}

func (r *lineMarkdownRenderer) renderInline(line string) string {
	return r.renderInlineTokens(line, false)
}

func (r *lineMarkdownRenderer) renderInlineTokens(line string, plain bool) string {
	var out strings.Builder
	for i := 0; i < len(line); {
		if end, rendered, ok := r.renderCodeSpanAt(line, i, plain); ok {
			out.WriteString(rendered)
			i = end
			continue
		}
		if end, rendered, ok := r.renderLinkAt(line, i, plain); ok {
			out.WriteString(rendered)
			i = end
			continue
		}
		if end, rendered, ok := r.renderEmphasisAt(line, i, plain); ok {
			out.WriteString(rendered)
			i = end
			continue
		}

		_, size := utf8.DecodeRuneInString(line[i:])
		if size <= 0 {
			size = 1
		}
		out.WriteString(line[i : i+size])
		i += size
	}
	return out.String()
}

func (r *lineMarkdownRenderer) renderCodeSpanAt(line string, start int, plain bool) (int, string, bool) {
	if start >= len(line) || line[start] != '`' {
		return 0, "", false
	}
	endRel := strings.IndexByte(line[start+1:], '`')
	if endRel < 0 {
		return 0, "", false
	}
	end := start + 1 + endRel
	content := line[start+1 : end]
	if plain {
		return end + 1, content, true
	}
	return end + 1, r.inlineCode.Render(content), true
}

func (r *lineMarkdownRenderer) renderLinkAt(line string, start int, plain bool) (int, string, bool) {
	if start >= len(line) || line[start] != '[' {
		return 0, "", false
	}
	labelEndRel := strings.IndexByte(line[start+1:], ']')
	if labelEndRel < 0 {
		return 0, "", false
	}
	labelEnd := start + 1 + labelEndRel
	if labelEnd+1 >= len(line) || line[labelEnd+1] != '(' {
		return 0, "", false
	}
	urlStart := labelEnd + 2
	urlEndRel := strings.IndexByte(line[urlStart:], ')')
	if urlEndRel < 0 {
		return 0, "", false
	}
	urlEnd := urlStart + urlEndRel
	label := r.renderInlinePlain(line[start+1 : labelEnd])
	url := line[urlStart:urlEnd]
	if plain {
		return urlEnd + 1, label + " (" + url + ")", true
	}
	return urlEnd + 1, r.linkStyle.Render(label) + " (" + url + ")", true
}

func (r *lineMarkdownRenderer) renderEmphasisAt(line string, start int, plain bool) (int, string, bool) {
	if start >= len(line) {
		return 0, "", false
	}
	marker := line[start]
	if marker != '*' && marker != '_' {
		return 0, "", false
	}

	runLen := emphasisDelimiterRunLength(line, start, marker)
	if runLen >= 2 {
		if end, rendered, ok := r.renderEmphasisRunAt(line, start, marker, 2, plain); ok {
			return end, rendered, true
		}
	}
	if end, rendered, ok := r.renderEmphasisRunAt(line, start, marker, 1, plain); ok {
		return end, rendered, true
	}
	return 0, "", false
}

func (r *lineMarkdownRenderer) renderEmphasisRunAt(line string, start int, marker byte, runLen int, plain bool) (int, string, bool) {
	canOpen, _ := emphasisDelimiterCapabilities(line, start, runLen, marker)
	if !canOpen {
		return 0, "", false
	}
	end := r.findClosingEmphasis(line, start+runLen, marker, runLen)
	if end < 0 {
		return 0, "", false
	}

	content := r.renderInlineTokens(line[start+runLen:end], plain)
	if plain {
		return end + runLen, content, true
	}
	if runLen == 2 {
		return end + runLen, r.boldStyle.Render(content), true
	}
	return end + runLen, r.italicStyle.Render(content), true
}

func (r *lineMarkdownRenderer) findClosingEmphasis(line string, start int, marker byte, runLen int) int {
	for i := start; i < len(line); {
		if end, _, ok := r.renderCodeSpanAt(line, i, true); ok {
			i = end
			continue
		}
		if end, _, ok := r.renderLinkAt(line, i, true); ok {
			i = end
			continue
		}
		if line[i] != marker {
			_, size := utf8.DecodeRuneInString(line[i:])
			if size <= 0 {
				size = 1
			}
			i += size
			continue
		}
		if emphasisDelimiterRunLength(line, i, marker) < runLen {
			i++
			continue
		}
		if _, canClose := emphasisDelimiterCapabilities(line, i, runLen, marker); canClose {
			return i
		}
		i++
	}
	return -1
}

func emphasisDelimiterRunLength(line string, start int, marker byte) int {
	runLen := 0
	for start+runLen < len(line) && line[start+runLen] == marker {
		runLen++
	}
	return runLen
}

// CommonMark-style flanking rules prevent `_` and `*` inside identifiers from
// being treated as emphasis delimiters.
func emphasisDelimiterCapabilities(line string, start, runLen int, marker byte) (bool, bool) {
	prev, hasPrev := previousRune(line, start)
	next, hasNext := nextRune(line, start+runLen)

	prevIsSpace := !hasPrev || unicode.IsSpace(prev)
	nextIsSpace := !hasNext || unicode.IsSpace(next)
	prevIsPunct := hasPrev && unicode.IsPunct(prev)
	nextIsPunct := hasNext && unicode.IsPunct(next)

	leftFlanking := !nextIsSpace && (!nextIsPunct || prevIsSpace || prevIsPunct)
	rightFlanking := !prevIsSpace && (!prevIsPunct || nextIsSpace || nextIsPunct)

	if marker == '_' {
		return leftFlanking && (!rightFlanking || prevIsPunct), rightFlanking && (!leftFlanking || nextIsPunct)
	}
	return leftFlanking && !(prevIsSpace && nextIsPunct), rightFlanking
}

func previousRune(s string, end int) (rune, bool) {
	if end <= 0 || end > len(s) {
		return 0, false
	}
	r, _ := utf8.DecodeLastRuneInString(s[:end])
	if r == utf8.RuneError && end == 0 {
		return 0, false
	}
	return r, true
}

func nextRune(s string, start int) (rune, bool) {
	if start < 0 || start >= len(s) {
		return 0, false
	}
	r, _ := utf8.DecodeRuneInString(s[start:])
	if r == utf8.RuneError && start >= len(s) {
		return 0, false
	}
	return r, true
}
