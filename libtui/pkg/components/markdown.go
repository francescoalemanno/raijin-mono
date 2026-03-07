package components

import (
	"bytes"
	"fmt"
	"hash/maphash"
	"strings"

	"github.com/francescoalemanno/raijin-mono/libtui/pkg/utils"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	extAst "github.com/yuin/goldmark/extension/ast"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/text"
)

// DefaultTextStyle defines styling applied to all text unless overridden
type DefaultTextStyle struct {
	Color         func(string) string
	BgColor       func(string) string
	Bold          bool
	Italic        bool
	Strikethrough bool
	Underline     bool
}

// MarkdownTheme defines theme functions for markdown elements
type MarkdownTheme struct {
	Heading         func(string) string
	Link            func(string) string
	LinkURL         func(string) string
	Code            func(string) string
	CodeBlock       func(string) string
	CodeBlockBorder func(string) string
	Quote           func(string) string
	QuoteBorder     func(string) string
	HR              func(string) string
	ListBullet      func(string) string
	Bold            func(string) string
	Italic          func(string) string
	Strikethrough   func(string) string
	Underline       func(string) string
	HighlightCode   func(code string, lang string) []string
	CodeBlockIndent string
}

// Markdown component renders markdown content with ANSI styling
type Markdown struct {
	text             string
	paddingX         int
	paddingY         int
	defaultTextStyle *DefaultTextStyle
	theme            MarkdownTheme

	// Cache
	cachedText  string
	cachedWidth int
	cachedLines []string

	// Computed style prefix
	defaultStylePrefix string

	// Cache highlighted code blocks by block order and content hash so completed
	// fenced blocks are not re-highlighted on subsequent renders.
	codeBlockCounter int
	codeBlockCache   map[int]codeBlockCacheEntry
}

type codeBlockCacheEntry struct {
	lang        string
	contentLen  int
	contentHash uint64
	lines       []string
}

var markdownCodeBlockHashSeed = maphash.MakeSeed()

// NewMarkdown creates a new Markdown component
func NewMarkdown(text string, paddingX, paddingY int, theme MarkdownTheme, defaultStyle *DefaultTextStyle) *Markdown {
	indent := theme.CodeBlockIndent
	if indent == "" {
		indent = "  "
	}
	theme.CodeBlockIndent = indent

	return &Markdown{
		text:             text,
		paddingX:         paddingX,
		paddingY:         paddingY,
		theme:            theme,
		defaultTextStyle: defaultStyle,
		codeBlockCache:   make(map[int]codeBlockCacheEntry),
	}
}

// SetText updates the markdown text
func (m *Markdown) SetText(text string) {
	if m.text == text {
		return
	}
	m.text = text
	m.invalidate()
}

// invalidate clears the cache
func (m *Markdown) invalidate() {
	m.cachedText = ""
	m.cachedWidth = 0
	m.cachedLines = nil
	m.defaultStylePrefix = ""
}

// Render renders the markdown to lines with the given width
func (m *Markdown) Render(width int) []string {
	// Check cache
	if m.cachedLines != nil && m.cachedText == m.text && m.cachedWidth == width {
		result := make([]string, len(m.cachedLines))
		copy(result, m.cachedLines)
		return result
	}

	// Calculate content width
	contentWidth := max(width-m.paddingX*2, 1)

	// Handle empty text
	if strings.TrimSpace(m.text) == "" {
		m.cachedText = m.text
		m.cachedWidth = width
		m.cachedLines = []string{}
		return []string{}
	}

	// Normalize tabs (must match source bytes used for Goldmark segment lookups)
	normalizedText := strings.ReplaceAll(m.text, "\t", "   ")
	source := []byte(normalizedText)

	// Parse markdown
	doc := m.parseMarkdown(normalizedText)
	m.codeBlockCounter = 0

	// Render tokens
	renderedLines := m.renderDocument(doc, contentWidth, source)
	m.pruneCodeBlockCache()

	// Wrap lines
	wrappedLines := m.wrapLines(renderedLines, contentWidth)

	// Add margins and background
	contentLines := m.addPaddingAndBackground(wrappedLines, width)

	// Add top/bottom padding
	emptyLine := strings.Repeat(" ", width)
	emptyLines := make([]string, m.paddingY)
	var bgFn func(string) string
	var fgFn func(string) string
	if m.defaultTextStyle != nil {
		bgFn = m.defaultTextStyle.BgColor
		fgFn = m.defaultTextStyle.Color
	}
	if fgFn != nil {
		emptyLine = fgFn(emptyLine)
	}
	for i := range emptyLines {
		if bgFn != nil {
			emptyLines[i] = m.applyBackgroundToLine(strings.Repeat(" ", width), width, bgFn)
		} else {
			emptyLines[i] = emptyLine
		}
	}

	// Combine
	result := make([]string, 0, len(emptyLines)+len(contentLines)+len(emptyLines))
	result = append(result, emptyLines...)
	result = append(result, contentLines...)
	result = append(result, emptyLines...)

	// Update cache
	m.cachedText = m.text
	m.cachedWidth = width
	m.cachedLines = make([]string, len(result))
	copy(m.cachedLines, result)

	if len(result) == 0 {
		return []string{""}
	}
	return result
}

// parseMarkdown parses markdown text into a Goldmark AST
func (m *Markdown) parseMarkdown(txt string) ast.Node {
	md := goldmark.New(
		goldmark.WithExtensions(
			extension.Table,
			extension.Strikethrough,
		),
		goldmark.WithParserOptions(
			parser.WithAutoHeadingID(),
		),
	)
	source := []byte(txt)
	reader := text.NewReader(source)
	return md.Parser().Parse(reader)
}

// renderDocument renders the document to lines
func (m *Markdown) renderDocument(doc ast.Node, width int, source []byte) []string {
	var lines []string

	for child := doc.FirstChild(); child != nil; child = child.NextSibling() {
		tokenLines := m.renderNode(child, width, source)
		lines = append(lines, tokenLines...)
	}

	return lines
}

// getTextFromLines extracts text from line segments
func (m *Markdown) getTextFromLines(lines *text.Segments, source []byte) string {
	if lines.Len() == 0 {
		return ""
	}
	var buf bytes.Buffer
	for i := 0; i < lines.Len(); i++ {
		seg := lines.At(i)
		if i > 0 && buf.Len() > 0 {
			lastChar := buf.Bytes()[buf.Len()-1]
			if lastChar != '\n' && lastChar != '\r' {
				buf.WriteByte('\n')
			}
		}
		buf.Write(seg.Value(source))
	}
	return buf.String()
}

// getCodeSpanText gets text content from a code span
func (m *Markdown) getCodeSpanText(node *ast.CodeSpan, source []byte) string {
	var buf bytes.Buffer
	for child := node.FirstChild(); child != nil; child = child.NextSibling() {
		if t, ok := child.(*ast.Text); ok {
			buf.Write(t.Value(source))
		}
	}
	return buf.String()
}

// getLinkText gets text content from a link
func (m *Markdown) getLinkText(node *ast.Link, source []byte) string {
	var buf bytes.Buffer
	for child := node.FirstChild(); child != nil; child = child.NextSibling() {
		if t, ok := child.(*ast.Text); ok {
			buf.Write(t.Value(source))
		}
	}
	return buf.String()
}

// renderNode renders a single AST node
func (m *Markdown) renderNode(node ast.Node, width int, source []byte) []string {
	switch n := node.(type) {
	case *ast.Heading:
		return m.renderHeading(n, source)
	case *ast.Paragraph:
		return m.renderParagraph(n, source)
	case *ast.FencedCodeBlock:
		return m.renderCodeBlock(n, source)
	case *ast.List:
		return m.renderList(n, 0, source)
	case *ast.Blockquote:
		return m.renderBlockquote(n, width, source)
	case *ast.ThematicBreak:
		return m.renderHR(width)
	case *ast.HTMLBlock:
		return m.renderHTMLBlock(n, source)
	case *ast.TextBlock:
		return []string{""}
	default:
		// Check for table kinds
		kind := node.Kind()
		if kind == extAst.KindTable {
			return m.renderTableNode(node, width, source)
		}
		return []string{}
	}
}

// renderHeading renders a heading
func (m *Markdown) renderHeading(heading *ast.Heading, source []byte) []string {
	content := m.renderInlineContent(heading, source)
	var styledHeading string

	if heading.Level == 1 {
		styledHeading = m.theme.Heading(m.theme.Bold(m.theme.Underline(content)))
	} else if heading.Level == 2 {
		styledHeading = m.theme.Heading(m.theme.Bold(content))
	} else {
		prefix := strings.Repeat("#", heading.Level) + " "
		styledHeading = m.theme.Heading(m.theme.Bold(prefix + content))
	}

	return []string{styledHeading, ""}
}

// renderParagraph renders a paragraph
func (m *Markdown) renderParagraph(para *ast.Paragraph, source []byte) []string {
	content := m.renderInlineContent(para, source)
	return []string{content, ""}
}

// renderCodeBlock renders a fenced code block
func (m *Markdown) renderCodeBlock(code *ast.FencedCodeBlock, source []byte) []string {
	var lines []string

	lang := string(code.Language(source))
	lines = append(lines, m.theme.CodeBlockBorder(fmt.Sprintf("```%s", lang)))

	content := m.getTextFromLines(code.Lines(), source)
	lines = append(lines, m.renderCodeBlockContent(m.nextCodeBlockIndex(), content, lang)...)

	lines = append(lines, m.theme.CodeBlockBorder("```"), "")
	return lines
}

func (m *Markdown) renderCodeBlockContent(blockIndex int, content, lang string) []string {
	if content == "" {
		return []string{}
	}

	contentHash := hashString64(content)
	if entry, ok := m.codeBlockCache[blockIndex]; ok &&
		entry.lang == lang &&
		entry.contentLen == len(content) &&
		entry.contentHash == contentHash {
		return entry.lines
	}

	indent := m.theme.CodeBlockIndent
	if m.theme.HighlightCode != nil {
		highlighted := m.theme.HighlightCode(content, lang)
		if len(highlighted) > 0 {
			lines := make([]string, 0, len(highlighted))
			for _, hlLine := range highlighted {
				lines = append(lines, indent+hlLine)
			}
			m.codeBlockCache[blockIndex] = codeBlockCacheEntry{
				lang:        lang,
				contentLen:  len(content),
				contentHash: contentHash,
				lines:       lines,
			}
			return lines
		}
	}

	codeLines := strings.Split(content, "\n")
	lines := make([]string, 0, len(codeLines))
	for _, codeLine := range codeLines {
		lines = append(lines, indent+m.theme.CodeBlock(codeLine))
	}
	m.codeBlockCache[blockIndex] = codeBlockCacheEntry{
		lang:        lang,
		contentLen:  len(content),
		contentHash: contentHash,
		lines:       lines,
	}
	return lines
}

// renderBlockquote renders a blockquote
func (m *Markdown) renderBlockquote(quote *ast.Blockquote, width int, source []byte) []string {
	var lines []string

	quoteStyle := func(text string) string {
		return m.theme.Quote(m.theme.Italic(text))
	}

	// Calculate available width for content
	quoteContentWidth := max(
		// "│ " = 2 chars
		width-2, 1)

	// Render the blockquote content
	for child := quote.FirstChild(); child != nil; child = child.NextSibling() {
		childLines := m.renderNode(child, quoteContentWidth, source)
		for _, line := range childLines {
			if line == "" {
				lines = append(lines, m.theme.QuoteBorder("│ ")+quoteStyle(""))
			} else {
				// Wrap the styled line
				wrapped := utils.WrapTextWithAnsi(line, quoteContentWidth)
				for _, wrappedLine := range wrapped {
					lines = append(lines, m.theme.QuoteBorder("│ ")+quoteStyle(wrappedLine))
				}
			}
		}
	}

	lines = append(lines, "")
	return lines
}

// renderHR renders a horizontal rule
func (m *Markdown) renderHR(width int) []string {
	line := strings.Repeat("─", min(width, 80))
	return []string{m.theme.HR(line), ""}
}

// renderHTMLBlock renders an HTML block
func (m *Markdown) renderHTMLBlock(html *ast.HTMLBlock, source []byte) []string {
	return []string{m.applyDefaultStyle(strings.TrimSpace(m.getTextFromLines(html.Lines(), source)))}
}

// renderList renders a list with proper nesting
func (m *Markdown) renderList(list *ast.List, depth int, source []byte) []string {
	var lines []string
	indent := strings.Repeat("  ", depth)

	itemIndex := 0
	for child := list.FirstChild(); child != nil; child = child.NextSibling() {
		if listItem, ok := child.(*ast.ListItem); ok {
			var bullet string
			if list.IsOrdered() {
				// Start from list.Start if available
				start := 1
				if list.Start > 0 {
					start = list.Start
				}
				bullet = fmt.Sprintf("%d. ", start+itemIndex)
			} else {
				bullet = "- "
			}

			itemLines := m.renderListItem(listItem, depth, source)
			if len(itemLines) > 0 {
				// First line - check if it's a nested list
				firstLine := itemLines[0]
				isNestedList := len(firstLine) > 0 && strings.HasPrefix(firstLine, "  ")

				if isNestedList {
					lines = append(lines, firstLine)
				} else {
					lines = append(lines, indent+m.theme.ListBullet(bullet)+firstLine)
				}

				// Rest of lines
				for i := 1; i < len(itemLines); i++ {
					line := itemLines[i]
					isNestedListLine := len(line) > 0 && strings.HasPrefix(line, "  ")
					if isNestedListLine {
						lines = append(lines, line)
					} else {
						lines = append(lines, indent+"  "+line)
					}
				}
			} else {
				lines = append(lines, indent+m.theme.ListBullet(bullet))
			}

			itemIndex++
		}
	}

	return lines
}

// renderListItem renders a list item
func (m *Markdown) renderListItem(item *ast.ListItem, parentDepth int, source []byte) []string {
	var lines []string

	for child := item.FirstChild(); child != nil; child = child.NextSibling() {
		switch n := child.(type) {
		case *ast.List:
			// Nested list
			nestedLines := m.renderList(n, parentDepth+1, source)
			lines = append(lines, nestedLines...)
		case *ast.Paragraph:
			content := m.renderInlineContent(n, source)
			if content != "" {
				lines = append(lines, content)
			}
		case *ast.FencedCodeBlock:
			lang := string(n.Language(source))
			lines = append(lines, m.theme.CodeBlockBorder(fmt.Sprintf("```%s", lang)))
			content := m.getTextFromLines(n.Lines(), source)
			lines = append(lines, m.renderCodeBlockContent(m.nextCodeBlockIndex(), content, lang)...)
			lines = append(lines, m.theme.CodeBlockBorder("```"))
		default:
			// Other token types - try to render as inline
			content := m.renderInlineContent(child, source)
			if content != "" {
				lines = append(lines, content)
			}
		}
	}

	return lines
}

func (m *Markdown) nextCodeBlockIndex() int {
	idx := m.codeBlockCounter
	m.codeBlockCounter++
	return idx
}

func (m *Markdown) pruneCodeBlockCache() {
	for idx := range m.codeBlockCache {
		if idx >= m.codeBlockCounter {
			delete(m.codeBlockCache, idx)
		}
	}
}

func hashString64(s string) uint64 {
	var h maphash.Hash
	h.SetSeed(markdownCodeBlockHashSeed)
	_, _ = h.WriteString(s)
	return h.Sum64()
}

// renderInlineContent renders inline content from a node
func (m *Markdown) renderInlineContent(node ast.Node, source []byte) string {
	return m.renderInlineNodes(node, nil, source)
}

// renderInlineNodes renders inline nodes with styling context
type inlineStyleContext struct {
	applyText   func(string) string
	stylePrefix string
}

func (m *Markdown) renderInlineNodes(node ast.Node, styleCtx *inlineStyleContext, source []byte) string {
	if styleCtx == nil {
		styleCtx = &inlineStyleContext{
			applyText:   m.applyDefaultStyle,
			stylePrefix: m.getDefaultStylePrefix(),
		}
	}

	var result strings.Builder

	for child := node.FirstChild(); child != nil; child = child.NextSibling() {
		switch n := child.(type) {
		case *ast.Text:
			txt := string(n.Value(source))
			if n.SoftLineBreak() || n.HardLineBreak() {
				txt = txt + "\n"
			}
			result.WriteString(m.applyTextWithNewlines(txt, styleCtx.applyText))

		case *ast.String:
			result.WriteString(m.applyTextWithNewlines(string(n.Value), styleCtx.applyText))

		case *ast.Emphasis:
			content := m.renderInlineNodes(n, styleCtx, source)
			if n.Level == 2 {
				// Strong emphasis (bold)
				result.WriteString(m.theme.Bold(content))
			} else {
				// Regular emphasis (italic)
				result.WriteString(m.theme.Italic(content))
			}
			result.WriteString(styleCtx.stylePrefix)

		case *ast.CodeSpan:
			content := m.getCodeSpanText(n, source)
			result.WriteString(m.theme.Code(content))
			result.WriteString(styleCtx.stylePrefix)

		case *ast.Link:
			content := m.renderInlineNodes(n, styleCtx, source)
			href := string(n.Destination)
			textContent := m.getLinkText(n, source)

			// Check if link text matches href (for autolinks)
			hrefForComparison := href
			if after, ok := strings.CutPrefix(href, "mailto:"); ok {
				hrefForComparison = after
			}

			if textContent == href || textContent == hrefForComparison {
				result.WriteString(m.theme.Link(m.theme.Underline(content)))
			} else {
				result.WriteString(m.theme.Link(m.theme.Underline(content)))
				result.WriteString(m.theme.LinkURL(fmt.Sprintf(" (%s)", href)))
			}
			result.WriteString(styleCtx.stylePrefix)

		case *ast.AutoLink:
			url := string(n.Label(source))
			result.WriteString(m.theme.Link(m.theme.Underline(url)))
			result.WriteString(styleCtx.stylePrefix)

		default:
			// Check for strikethrough
			if child.Kind() == extAst.KindStrikethrough {
				content := m.renderInlineNodes(child, styleCtx, source)
				result.WriteString(m.theme.Strikethrough(content))
				result.WriteString(styleCtx.stylePrefix)
			} else if raw, ok := n.(*ast.RawHTML); ok {
				var htmlBuf bytes.Buffer
				for i := 0; i < raw.Segments.Len(); i++ {
					seg := raw.Segments.At(i)
					htmlBuf.Write(seg.Value(source))
				}
				result.WriteString(m.applyTextWithNewlines(htmlBuf.String(), styleCtx.applyText))
			} else if textNode, ok := n.(*ast.Text); ok {
				result.WriteString(m.applyTextWithNewlines(string(textNode.Value(source)), styleCtx.applyText))
			}
		}
	}

	return result.String()
}

// applyTextWithNewlines applies a function to text segments, preserving newlines
func (m *Markdown) applyTextWithNewlines(text string, fn func(string) string) string {
	segments := strings.Split(text, "\n")
	for i, seg := range segments {
		segments[i] = fn(seg)
	}
	return strings.Join(segments, "\n")
}

// applyDefaultStyle applies default text style
func (m *Markdown) applyDefaultStyle(text string) string {
	if m.defaultTextStyle == nil {
		return text
	}

	styled := text

	if m.defaultTextStyle.Color != nil {
		styled = m.defaultTextStyle.Color(styled)
	}
	if m.defaultTextStyle.Bold {
		styled = m.theme.Bold(styled)
	}
	if m.defaultTextStyle.Italic {
		styled = m.theme.Italic(styled)
	}
	if m.defaultTextStyle.Strikethrough {
		styled = m.theme.Strikethrough(styled)
	}
	if m.defaultTextStyle.Underline {
		styled = m.theme.Underline(styled)
	}

	return styled
}

// getDefaultStylePrefix computes the ANSI prefix for default style
func (m *Markdown) getDefaultStylePrefix() string {
	if m.defaultStylePrefix != "" || m.defaultTextStyle == nil {
		return m.defaultStylePrefix
	}

	sentinel := "\x00"
	styled := sentinel

	if m.defaultTextStyle.Color != nil {
		styled = m.defaultTextStyle.Color(styled)
	}
	if m.defaultTextStyle.Bold {
		styled = m.theme.Bold(styled)
	}
	if m.defaultTextStyle.Italic {
		styled = m.theme.Italic(styled)
	}
	if m.defaultTextStyle.Strikethrough {
		styled = m.theme.Strikethrough(styled)
	}
	if m.defaultTextStyle.Underline {
		styled = m.theme.Underline(styled)
	}

	before, _, ok := strings.Cut(styled, sentinel)
	if ok {
		m.defaultStylePrefix = before
	}

	return m.defaultStylePrefix
}

// wrapLines wraps rendered lines to fit content width
func (m *Markdown) wrapLines(lines []string, width int) []string {
	var wrapped []string
	for _, line := range lines {
		wrapped = append(wrapped, utils.WrapTextWithAnsi(line, width)...)
	}
	return wrapped
}

// addPaddingAndBackground adds horizontal padding and background color
func (m *Markdown) addPaddingAndBackground(lines []string, width int) []string {
	leftMargin := strings.Repeat(" ", m.paddingX)
	rightMargin := strings.Repeat(" ", m.paddingX)
	var bgFn func(string) string
	var fgFn func(string) string
	if m.defaultTextStyle != nil {
		bgFn = m.defaultTextStyle.BgColor
		fgFn = m.defaultTextStyle.Color
	}
	// Apply foreground color to margins if available
	if fgFn != nil {
		leftMargin = fgFn(leftMargin)
		rightMargin = fgFn(rightMargin)
	}

	result := make([]string, len(lines))
	for i, line := range lines {
		lineWithMargins := leftMargin + line + rightMargin

		if bgFn != nil {
			result[i] = m.applyBackgroundToLine(lineWithMargins, width, bgFn)
		} else {
			visibleLen := utils.VisibleWidth(lineWithMargins)
			paddingNeeded := width - visibleLen
			if paddingNeeded > 0 {
				padding := strings.Repeat(" ", paddingNeeded)
				if fgFn != nil {
					padding = fgFn(padding)
				}
				result[i] = lineWithMargins + padding
			} else {
				result[i] = lineWithMargins
			}
		}
	}

	return result
}

// getForegroundFunc returns the foreground color function if available
func (m *Markdown) getForegroundFunc() func(string) string {
	if m.defaultTextStyle != nil && m.defaultTextStyle.Color != nil {
		return m.defaultTextStyle.Color
	}
	return nil
}

// applyBackgroundToLine applies background color to a full line
func (m *Markdown) applyBackgroundToLine(line string, width int, bgFn func(string) string) string {
	visibleLen := utils.VisibleWidth(line)
	paddingNeeded := width - visibleLen
	if paddingNeeded > 0 {
		line = line + strings.Repeat(" ", paddingNeeded)
	}
	return bgFn(line)
}

// renderTableNode renders a markdown table using kind checking
func (m *Markdown) renderTableNode(table ast.Node, availableWidth int, source []byte) []string {
	var lines []string

	// Get header and rows
	header := table.FirstChild()
	if header == nil {
		return lines
	}

	// Check if it's a table header
	if header.Kind() != extAst.KindTableHeader {
		return lines
	}

	// Count columns
	numCols := 0
	for child := header.FirstChild(); child != nil; child = child.NextSibling() {
		if child.Kind() == extAst.KindTableCell {
			numCols++
		}
	}

	if numCols == 0 {
		return lines
	}

	// Border overhead: "│ " + (n-1) * " │ " + " │" = 3n + 1
	borderOverhead := 3*numCols + 1
	availableForCells := availableWidth - borderOverhead
	if availableForCells < numCols {
		// Too narrow - fall back to raw markdown
		return []string{m.applyDefaultStyle(string(table.Lines().Value(source))), ""}
	}

	// Get all data rows
	var dataRows []ast.Node
	for row := header.NextSibling(); row != nil; row = row.NextSibling() {
		if row.Kind() == extAst.KindTableRow {
			dataRows = append(dataRows, row)
		}
	}

	// Calculate natural column widths
	naturalWidths := make([]int, numCols)
	minWordWidths := make([]int, numCols)
	maxUnbrokenWordWidth := 30

	// Helper to get cell text
	getCellText := func(cell ast.Node) string {
		return m.renderInlineContent(cell, source)
	}

	// Header widths
	colIdx := 0
	for cell := header.FirstChild(); cell != nil && colIdx < numCols; cell = cell.NextSibling() {
		if cell.Kind() == extAst.KindTableCell {
			t := getCellText(cell)
			naturalWidths[colIdx] = utils.VisibleWidth(t)
			minWordWidths[colIdx] = m.getLongestWordWidth(t, maxUnbrokenWordWidth)
			colIdx++
		}
	}

	// Data row widths
	for _, row := range dataRows {
		colIdx = 0
		for cell := row.FirstChild(); cell != nil && colIdx < numCols; cell = cell.NextSibling() {
			if cell.Kind() == extAst.KindTableCell {
				t := getCellText(cell)
				naturalWidths[colIdx] = max(naturalWidths[colIdx], utils.VisibleWidth(t))
				minWordWidths[colIdx] = max(minWordWidths[colIdx], m.getLongestWordWidth(t, maxUnbrokenWordWidth))
				colIdx++
			}
		}
	}

	// Calculate final column widths
	minCellsWidth := sumInt(minWordWidths)
	columnWidths := make([]int, numCols)

	if minCellsWidth > availableForCells {
		// Need to shrink
		for i := range columnWidths {
			columnWidths[i] = 1
		}
		remaining := availableForCells - numCols
		if remaining > 0 {
			totalWeight := 0
			for i, w := range minWordWidths {
				totalWeight += max(0, w-1)
				minWordWidths[i] = max(0, w-1)
			}
			if totalWeight > 0 {
				for i := range columnWidths {
					columnWidths[i] += (minWordWidths[i] * remaining) / totalWeight
				}
			}
		}
	} else {
		// Distribute extra space
		totalNatural := sumInt(naturalWidths) + borderOverhead
		if totalNatural <= availableWidth {
			// Everything fits
			for i := range columnWidths {
				columnWidths[i] = max(naturalWidths[i], minWordWidths[i])
			}
		} else {
			// Need to shrink proportionally
			extraWidth := availableForCells - minCellsWidth
			totalGrowPotential := 0
			for i := range naturalWidths {
				totalGrowPotential += max(0, naturalWidths[i]-minWordWidths[i])
			}
			if totalGrowPotential > 0 {
				for i := range columnWidths {
					grow := (max(0, naturalWidths[i]-minWordWidths[i]) * extraWidth) / totalGrowPotential
					columnWidths[i] = minWordWidths[i] + grow
				}
			} else {
				for i := range columnWidths {
					columnWidths[i] = minWordWidths[i]
				}
			}
		}
	}

	// Get foreground color function for borders
	fgFn := m.getForegroundFunc()

	// Render top border
	topBorderCells := make([]string, numCols)
	for i, w := range columnWidths {
		topBorderCells[i] = strings.Repeat("─", w)
	}
	topBorder := fmt.Sprintf("┌─%s─┐", strings.Join(topBorderCells, "─┬─"))
	if fgFn != nil {
		topBorder = fgFn(topBorder)
	}
	lines = append(lines, topBorder)

	// Render header
	headerCells := make([][]string, numCols)
	colIdx = 0
	for cell := header.FirstChild(); cell != nil && colIdx < numCols; cell = cell.NextSibling() {
		if cell.Kind() == extAst.KindTableCell {
			t := getCellText(cell)
			headerCells[colIdx] = utils.WrapTextWithAnsi(t, columnWidths[colIdx])
			colIdx++
		}
	}

	headerLineCount := 0
	for _, cells := range headerCells {
		headerLineCount = max(headerLineCount, len(cells))
	}

	for lineIdx := 0; lineIdx < headerLineCount; lineIdx++ {
		rowParts := make([]string, numCols)
		for i := 0; i < numCols; i++ {
			t := ""
			if lineIdx < len(headerCells[i]) {
				t = headerCells[i][lineIdx]
			}
			padding := strings.Repeat(" ", max(0, columnWidths[i]-utils.VisibleWidth(t)))
			if fgFn != nil {
				padding = fgFn(padding)
			}
			padded := t + padding
			rowParts[i] = m.theme.Bold(padded)
		}
		rowLine := fmt.Sprintf("│ %s │", strings.Join(rowParts, " │ "))
		if fgFn != nil {
			rowLine = fgFn(rowLine)
		}
		lines = append(lines, rowLine)
	}

	// Render separator
	sepCells := make([]string, numCols)
	for i, w := range columnWidths {
		sepCells[i] = strings.Repeat("─", w)
	}
	sepLine := fmt.Sprintf("├─%s─┤", strings.Join(sepCells, "─┼─"))
	if fgFn != nil {
		sepLine = fgFn(sepLine)
	}
	lines = append(lines, sepLine)

	// Render rows
	for rowIdx, row := range dataRows {
		rowCells := make([][]string, numCols)
		colIdx = 0
		for cell := row.FirstChild(); cell != nil && colIdx < numCols; cell = cell.NextSibling() {
			if cell.Kind() == extAst.KindTableCell {
				t := getCellText(cell)
				rowCells[colIdx] = utils.WrapTextWithAnsi(t, columnWidths[colIdx])
				colIdx++
			}
		}

		rowLineCount := 0
		for _, cells := range rowCells {
			rowLineCount = max(rowLineCount, len(cells))
		}

		for lineIdx := 0; lineIdx < rowLineCount; lineIdx++ {
			rowParts := make([]string, numCols)
			for i := 0; i < numCols; i++ {
				t := ""
				if lineIdx < len(rowCells[i]) {
					t = rowCells[i][lineIdx]
				}
				padding := strings.Repeat(" ", max(0, columnWidths[i]-utils.VisibleWidth(t)))
				if fgFn != nil {
					padding = fgFn(padding)
				}
				rowParts[i] = t + padding
			}
			rowLine := fmt.Sprintf("│ %s │", strings.Join(rowParts, " │ "))
			if fgFn != nil {
				rowLine = fgFn(rowLine)
			}
			lines = append(lines, rowLine)
		}

		if rowIdx < len(dataRows)-1 {
			lines = append(lines, sepLine)
		}
	}

	// Render bottom border
	bottomBorderCells := make([]string, numCols)
	for i, w := range columnWidths {
		bottomBorderCells[i] = strings.Repeat("─", w)
	}
	bottomBorder := fmt.Sprintf("└─%s─┘", strings.Join(bottomBorderCells, "─┴─"))
	if fgFn != nil {
		bottomBorder = fgFn(bottomBorder)
	}
	lines = append(lines, bottomBorder)
	lines = append(lines, "")

	return lines
}

// getLongestWordWidth gets the visible width of the longest word
func (m *Markdown) getLongestWordWidth(text string, maxWidth int) int {
	words := strings.Fields(text)
	longest := 0
	for _, word := range words {
		w := utils.VisibleWidth(word)
		if w > longest {
			longest = w
		}
	}
	if maxWidth == 0 || longest <= maxWidth {
		return longest
	}
	return maxWidth
}

func sumInt(nums []int) int {
	s := 0
	for _, n := range nums {
		s += n
	}
	return s
}
