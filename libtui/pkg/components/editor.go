package components

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"unicode"

	"github.com/francescoalemanno/raijin-mono/libtui/pkg/autocomplete"
	"github.com/francescoalemanno/raijin-mono/libtui/pkg/keys"
	"github.com/francescoalemanno/raijin-mono/libtui/pkg/tui"
	"github.com/francescoalemanno/raijin-mono/libtui/pkg/utils"

	"github.com/rivo/uniseg"
)

var (
	atReferencePattern     = regexp.MustCompile(`(?:^|[\s])@[^\s]*$`)
	dollarReferencePattern = regexp.MustCompile(`(?:^|[\s])\$[^\s]*$`)
)

// TextChunk represents a chunk of text for word-wrap layout
type TextChunk struct {
	Text       string
	StartIndex int
	EndIndex   int
}

// WordWrapLine splits a line into word-wrapped chunks
func WordWrapLine(line string, maxWidth int) []TextChunk {
	if line == "" || maxWidth <= 0 {
		return []TextChunk{{Text: "", StartIndex: 0, EndIndex: 0}}
	}

	lineWidth := utils.VisibleWidth(line)
	if lineWidth <= maxWidth {
		return []TextChunk{{Text: line, StartIndex: 0, EndIndex: len(line)}}
	}

	var chunks []TextChunk
	gr := uniseg.NewGraphemes(line)
	segments := make([]struct {
		segment string
		index   int
	}, 0)

	i := 0
	for gr.Next() {
		segments = append(segments, struct {
			segment string
			index   int
		}{
			segment: gr.Str(),
			index:   i,
		})
		i += len(gr.Str())
	}

	currentWidth := 0
	chunkStart := 0
	wrapOppIndex := -1
	wrapOppWidth := 0

	for i := 0; i < len(segments); i++ {
		seg := segments[i]
		grapheme := seg.segment
		gWidth := utils.VisibleWidth(grapheme)
		charIndex := seg.index
		runes := []rune(grapheme)
		isWs := len(runes) > 0 && utils.IsWhitespaceChar(runes[0])

		// Overflow check before advancing
		if currentWidth+gWidth > maxWidth {
			if wrapOppIndex >= 0 {
				// Backtrack to last wrap opportunity
				chunks = append(chunks, TextChunk{
					Text:       line[chunkStart:wrapOppIndex],
					StartIndex: chunkStart,
					EndIndex:   wrapOppIndex,
				})
				chunkStart = wrapOppIndex
				currentWidth -= wrapOppWidth
			} else if chunkStart < charIndex {
				// No wrap opportunity: force-break at current position
				chunks = append(chunks, TextChunk{
					Text:       line[chunkStart:charIndex],
					StartIndex: chunkStart,
					EndIndex:   charIndex,
				})
				chunkStart = charIndex
				currentWidth = 0
			}
			wrapOppIndex = -1
		}

		// Advance
		currentWidth += gWidth

		// Record wrap opportunity: whitespace followed by non-whitespace
		nextIdx := i + 1
		if isWs && nextIdx < len(segments) {
			nextSeg := segments[nextIdx]
			nextRunes := []rune(nextSeg.segment)
			if len(nextRunes) > 0 && !utils.IsWhitespaceChar(nextRunes[0]) {
				wrapOppIndex = nextSeg.index
				wrapOppWidth = currentWidth
			}
		}
	}

	// Push final chunk
	chunks = append(chunks, TextChunk{
		Text:       line[chunkStart:],
		StartIndex: chunkStart,
		EndIndex:   len(line),
	})

	return chunks
}

// Kitty CSI-u sequences for printable keys
var kittyCSIURegex = regexp.MustCompile(`^\x1b\[(\d+)(?::(\d*))?(?::(\d+))?(?:;(\d+))?(?::(\d+))?u$`)

const (
	kittyModShift = 1
	kittyModAlt   = 2
	kittyModCtrl  = 4

	largePasteLineThreshold = 10
	largePasteCharThreshold = 1000
)

// DecodeKittyPrintable decodes a printable CSI-u sequence
func DecodeKittyPrintable(data string) string {
	match := kittyCSIURegex.FindStringSubmatch(data)
	if match == nil {
		return ""
	}

	codepoint := parseInt(match[1])
	if codepoint == 0 {
		return ""
	}

	shiftedKey := 0
	if match[2] != "" {
		shiftedKey = parseInt(match[2])
	}

	modValue := 1
	if match[4] != "" {
		modValue = parseInt(match[4])
	}
	modifier := modValue - 1 // Modifiers are 1-indexed in CSI-u

	// Ignore CSI-u sequences used for Alt/Ctrl shortcuts
	if modifier&(kittyModAlt|kittyModCtrl) != 0 {
		return ""
	}

	// Prefer the shifted keycode when Shift is held
	effectiveCodepoint := codepoint
	if modifier&kittyModShift != 0 && shiftedKey != 0 {
		effectiveCodepoint = shiftedKey
	}

	// Drop control characters or invalid codepoints
	if effectiveCodepoint < 32 {
		return ""
	}

	return string(rune(effectiveCodepoint))
}

func parseInt(s string) int {
	if s == "" {
		return 0
	}
	var result int
	for _, ch := range s {
		if ch < '0' || ch > '9' {
			return 0
		}
		result = result*10 + int(ch-'0')
	}
	return result
}

func isWordRune(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_'
}

// EditorState represents the editor state for undo/redo
type EditorState struct {
	Lines      []string
	CursorLine int
	CursorCol  int // This is now RUNE index, not byte offset
}

// LayoutLine represents a line in the rendered layout
type LayoutLine struct {
	Text        string
	HasCursor   bool
	CursorPos   int // This is byte position in the wrapped text
	IsShellLine bool
}

// EditorTheme defines the visual theme for the editor
type EditorTheme struct {
	BorderColor    func(string) string
	Foreground     func(string) string
	ShellLineColor func(string) string
	SelectList     SelectListTheme
}

// EditorOptions contains optional configuration for the editor
type EditorOptions struct {
	AutocompleteMaxVisible int
	PaddingX               int
}

// Editor is a multi-line text editor component
type Editor struct {
	tui                    UILike
	theme                  EditorTheme
	lines                  []string
	cursorLine             int
	cursorCol              int // This is RUNE index, not byte offset
	paddingX               int
	borderColor            func(string) string
	lastWidth              int
	scrollOffset           int
	autocompleteProvider   autocomplete.AutocompleteProvider
	autocompleteList       *SelectList
	autocompleteState      string
	autocompletePrefix     string
	autocompleteMaxVisible int
	pastes                 map[int]string
	pasteCounter           int
	pasteBuffer            string
	isInPaste              bool
	history                []string
	historyIndex           int
	killRing               *utils.KillRing
	lastAction             string
	jumpMode               string
	preferredVisualCol     *int
	undoStack              *utils.UndoStack[EditorState]
	onSubmit               func(string)
	onChange               func(string)
	disableSubmit          bool
	focused                bool
	// Async completion fields
	acCancelFunc context.CancelFunc
	acMutex      sync.Mutex
	acInFlight   bool
}

// NewEditor creates a new Editor component
func NewEditor(tui UILike, theme EditorTheme, options ...EditorOptions) *Editor {
	opts := EditorOptions{}
	if len(options) > 0 {
		opts = options[0]
	}

	paddingX := opts.PaddingX
	if paddingX < 0 {
		paddingX = 0
	}

	maxVisible := opts.AutocompleteMaxVisible
	if maxVisible < 3 {
		maxVisible = 5
	}

	e := &Editor{
		tui:                    tui,
		theme:                  theme,
		lines:                  []string{""},
		paddingX:               paddingX,
		borderColor:            theme.BorderColor,
		lastWidth:              80,
		autocompleteState:      "",
		autocompleteMaxVisible: maxVisible,
		pastes:                 make(map[int]string),
		historyIndex:           -1,
		killRing:               utils.NewKillRing(),
		undoStack:              utils.NewUndoStack[EditorState](),
	}

	return e
}

// GetPaddingX returns the horizontal padding
func (e *Editor) GetPaddingX() int {
	return e.paddingX
}

// SetPaddingX sets the horizontal padding
func (e *Editor) SetPaddingX(padding int) {
	if padding < 0 {
		padding = 0
	}
	if e.paddingX != padding {
		e.paddingX = padding
		if e.tui != nil {
			e.tui.RequestRender()
		}
	}
}

// GetAutocompleteMaxVisible returns the max visible autocomplete items
func (e *Editor) GetAutocompleteMaxVisible() int {
	return e.autocompleteMaxVisible
}

// SetAutocompleteMaxVisible sets the max visible autocomplete items
func (e *Editor) SetAutocompleteMaxVisible(maxVisible int) {
	if maxVisible < 3 {
		maxVisible = 3
	}
	if maxVisible > 20 {
		maxVisible = 20
	}
	if e.autocompleteMaxVisible != maxVisible {
		e.autocompleteMaxVisible = maxVisible
		if e.tui != nil {
			e.tui.RequestRender()
		}
	}
}

// SetAutocompleteProvider sets the autocomplete provider
func (e *Editor) SetAutocompleteProvider(provider autocomplete.AutocompleteProvider) {
	e.autocompleteProvider = provider
}

// AddToHistory adds a prompt to history
func (e *Editor) AddToHistory(text string) {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return
	}
	// Don't add consecutive duplicates
	if len(e.history) > 0 && e.history[0] == trimmed {
		return
	}
	e.history = append([]string{trimmed}, e.history...)
	// Limit history size
	if len(e.history) > 100 {
		e.history = e.history[:100]
	}
}

// ClearHistory clears the prompt history
func (e *Editor) ClearHistory() {
	e.history = []string{}
	e.historyIndex = -1
}

// isEditorEmpty returns true if the editor has no content
func (e *Editor) isEditorEmpty() bool {
	return len(e.lines) == 1 && e.lines[0] == ""
}

// setTextInternal sets text without resetting history state.
// Always positions cursor at end of text (matching TS behavior).
func (e *Editor) setTextInternal(text string, cursorAtEnd ...bool) {
	// Normalize line endings
	normalized := strings.ReplaceAll(text, "\r\n", "\n")
	normalized = strings.ReplaceAll(normalized, "\r", "\n")

	lines := strings.Split(normalized, "\n")
	if len(lines) == 0 {
		lines = []string{""}
	}
	e.lines = lines
	if len(e.lines) == 1 && e.lines[0] == "" {
		e.pastes = make(map[int]string)
		e.pasteCounter = 0
	}
	e.cursorLine = len(e.lines) - 1
	e.setCursorCol(e.runeCount(e.lines[e.cursorLine]))
	e.scrollOffset = 0

	if e.onChange != nil {
		e.onChange(e.GetText())
	}
}

// runeCount returns the number of runes in a string
func (e *Editor) runeCount(s string) int {
	return len([]rune(s))
}

// SetFocused sets the focused state (implements Focusable).
func (e *Editor) SetFocused(focused bool) {
	e.focused = focused
}

// IsFocused returns the focused state (implements Focusable).
func (e *Editor) IsFocused() bool {
	return e.focused
}

// Invalidate clears cached render state
func (e *Editor) Invalidate() {
	// Nothing to invalidate yet
}

// GetText returns the current text content
func (e *Editor) GetText() string {
	return strings.Join(e.lines, "\n")
}

// GetExpandedText returns text with special characters expanded
func (e *Editor) GetExpandedText() string {
	return e.expandPasteMarkers(e.GetText())
}

func (e *Editor) expandPasteMarkers(text string) string {
	result := text
	for pasteID, pasteContent := range e.pastes {
		markerPattern := fmt.Sprintf(`\[paste #%d( (\+\d+ lines|\d+ chars))?\]`, pasteID)
		re := regexp.MustCompile(markerPattern)
		result = re.ReplaceAllString(result, pasteContent)
	}
	return result
}

// GetLines returns a defensive copy of the lines
func (e *Editor) GetLines() []string {
	result := make([]string, len(e.lines))
	copy(result, e.lines)
	return result
}

// GetCursor returns the current cursor position (line, rune column)
func (e *Editor) GetCursor() (line, col int) {
	return e.cursorLine, e.cursorCol
}

// SetCursor moves the cursor to the given line and rune column, clamping to
// valid bounds.
func (e *Editor) SetCursor(line, col int) {
	if line < 0 {
		line = 0
	}
	if line >= len(e.lines) {
		line = len(e.lines) - 1
	}
	e.cursorLine = line
	e.setCursorCol(col)
}

// SetText sets the text content
func (e *Editor) SetText(text string) {
	e.lastAction = ""
	e.historyIndex = -1
	if e.GetText() != text {
		e.pushUndoSnapshot()
	}
	e.setTextInternal(text)
}

// InsertTextAtCursor inserts text at the current cursor position
func (e *Editor) InsertTextAtCursor(text string) {
	e.pushUndoSnapshot()
	e.insertTextAtCursorInternal(text)
	if e.onChange != nil {
		e.onChange(e.GetText())
	}
}

// insertTextAtCursorInternal inserts text without pushing undo
func (e *Editor) insertTextAtCursorInternal(text string) {
	if text == "" {
		return
	}

	// Normalize line endings
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")

	// Exit history mode when typing
	if e.historyIndex != -1 {
		e.historyIndex = -1
	}

	// Handle newlines
	if strings.Contains(text, "\n") {
		parts := strings.Split(text, "\n")
		line := e.lines[e.cursorLine]
		runes := []rune(line)
		before := string(runes[:e.cursorCol])
		after := string(runes[e.cursorCol:])

		// First part goes in current line
		e.lines[e.cursorLine] = before + parts[0]

		// Middle parts become new lines
		newLines := make([]string, 0, len(parts)-1)
		for i := 1; i < len(parts)-1; i++ {
			newLines = append(newLines, parts[i])
		}

		// Last part + after goes in new line
		if len(parts) > 1 {
			newLines = append(newLines, parts[len(parts)-1]+after)
		}

		// Insert new lines after current
		e.lines = append(e.lines[:e.cursorLine+1], append(newLines, e.lines[e.cursorLine+1:]...)...)

		e.cursorLine += len(parts) - 1
		e.cursorCol = e.runeCount(parts[len(parts)-1])
	} else {
		// Single line insert - work with runes
		line := e.lines[e.cursorLine]
		runes := []rune(line)
		insertRunes := []rune(text)
		newRunes := make([]rune, 0, len(runes)+len(insertRunes))
		newRunes = append(newRunes, runes[:e.cursorCol]...)
		newRunes = append(newRunes, insertRunes...)
		newRunes = append(newRunes, runes[e.cursorCol:]...)
		e.lines[e.cursorLine] = string(newRunes)
		e.cursorCol += len(insertRunes)
	}

	if e.tui != nil {
		e.tui.RequestRender()
	}
}

// insertCharacter inserts a single character with undo coalescing
func (e *Editor) insertCharacter(char string, skipUndoCoalescing ...bool) {
	e.historyIndex = -1

	if len(skipUndoCoalescing) == 0 || !skipUndoCoalescing[0] {
		if utils.IsWhitespaceChar(char) || e.lastAction != "type-word" {
			e.pushUndoSnapshot()
		}
		e.lastAction = "type-word"
	}

	line := e.lines[e.cursorLine]
	runes := []rune(line)
	charRunes := []rune(char)
	newRunes := make([]rune, 0, len(runes)+len(charRunes))
	newRunes = append(newRunes, runes[:e.cursorCol]...)
	newRunes = append(newRunes, charRunes...)
	newRunes = append(newRunes, runes[e.cursorCol:]...)
	e.lines[e.cursorLine] = string(newRunes)
	e.setCursorCol(e.cursorCol + len(charRunes))

	if e.onChange != nil {
		e.onChange(e.GetText())
	}

	// Check if we should trigger or update autocomplete (pi-mono pattern)
	if e.autocompleteState == "" {
		if char == "/" && e.isAtStartOfMessage() {
			e.tryTriggerAutocomplete(false)
		} else if char == "@" {
			line := e.lines[e.cursorLine]
			r := []rune(line)
			col := e.cursorCol
			if col > len(r) {
				col = len(r)
			}
			textBeforeCursor := string(r[:col])
			charBeforeAt := ""
			if len(textBeforeCursor) >= 2 {
				charBeforeAt = string([]rune(textBeforeCursor)[len([]rune(textBeforeCursor))-2])
			}
			if len([]rune(textBeforeCursor)) == 1 || charBeforeAt == " " || charBeforeAt == "\t" {
				e.tryTriggerAutocomplete(false)
			}
		} else if char == "$" {
			line := e.lines[e.cursorLine]
			r := []rune(line)
			col := e.cursorCol
			if col > len(r) {
				col = len(r)
			}
			textBeforeCursor := string(r[:col])
			charBeforeDollar := ""
			if len(textBeforeCursor) >= 2 {
				charBeforeDollar = string([]rune(textBeforeCursor)[len([]rune(textBeforeCursor))-2])
			}
			if len([]rune(textBeforeCursor)) == 1 || charBeforeDollar == " " || charBeforeDollar == "\t" {
				e.tryTriggerAutocomplete(false)
			}
		} else if regexp.MustCompile(`[a-zA-Z0-9.\-_/]`).MatchString(char) {
			line := e.lines[e.cursorLine]
			r := []rune(line)
			col := e.cursorCol
			if col > len(r) {
				col = len(r)
			}
			textBeforeCursor := string(r[:col])
			if e.isInSlashCommandContext(textBeforeCursor) {
				e.tryTriggerAutocomplete(false)
			} else if atReferencePattern.MatchString(textBeforeCursor) {
				e.tryTriggerAutocomplete(false)
			} else if dollarReferencePattern.MatchString(textBeforeCursor) {
				e.tryTriggerAutocomplete(false)
			}
		}
	} else {
		e.updateAutocomplete()
	}
}

// handlePaste handles pasted text
func (e *Editor) handlePaste(pastedText string) {
	e.historyIndex = -1
	e.lastAction = ""
	e.pushUndoSnapshot()

	// Normalize line endings and convert tabs for predictable paste output.
	cleanText := strings.ReplaceAll(pastedText, "\r\n", "\n")
	cleanText = strings.ReplaceAll(cleanText, "\r", "\n")
	tabExpandedText := strings.ReplaceAll(cleanText, "\t", "    ")

	// Keep only printable runes and newlines.
	var filteredBuilder strings.Builder
	for _, r := range tabExpandedText {
		if r == '\n' || r >= 32 {
			filteredBuilder.WriteRune(r)
		}
	}
	filteredText := filteredBuilder.String()

	// If a path-like paste is appended to a word, add a separating space.
	if strings.HasPrefix(filteredText, "/") || strings.HasPrefix(filteredText, "~") || strings.HasPrefix(filteredText, ".") {
		currentLine := e.lines[e.cursorLine]
		currentRunes := []rune(currentLine)
		if e.cursorCol > 0 && e.cursorCol <= len(currentRunes) && isWordRune(currentRunes[e.cursorCol-1]) {
			filteredText = " " + filteredText
		}
	}

	pastedLines := strings.Split(filteredText, "\n")
	totalChars := e.runeCount(filteredText)
	if len(pastedLines) > largePasteLineThreshold || totalChars > largePasteCharThreshold {
		e.pasteCounter++
		pasteID := e.pasteCounter
		e.pastes[pasteID] = filteredText

		var marker string
		if len(pastedLines) > largePasteLineThreshold {
			marker = fmt.Sprintf("[paste #%d +%d lines]", pasteID, len(pastedLines))
		} else {
			marker = fmt.Sprintf("[paste #%d %d chars]", pasteID, totalChars)
		}
		e.insertTextAtCursorInternal(marker)
		if e.onChange != nil {
			e.onChange(e.GetText())
		}
		return
	}

	if len(pastedLines) == 1 {
		// Insert single-line pastes character-by-character to keep autocomplete behavior.
		for _, r := range filteredText {
			e.insertCharacter(string(r), true)
		}
		return
	}

	e.insertTextAtCursorInternal(filteredText)
	if e.onChange != nil {
		e.onChange(e.GetText())
	}
}

// addNewLine inserts a new line
func (e *Editor) addNewLine() {
	e.pushUndoSnapshot()
	line := e.lines[e.cursorLine]
	runes := []rune(line)
	before := string(runes[:e.cursorCol])
	after := string(runes[e.cursorCol:])

	e.lines[e.cursorLine] = before
	e.lines = append(e.lines[:e.cursorLine+1], append([]string{after}, e.lines[e.cursorLine+1:]...)...)
	e.cursorLine++
	e.cursorCol = 0

	if e.tui != nil {
		e.tui.RequestRender()
	}
	if e.onChange != nil {
		e.onChange(e.GetText())
	}
}

// submitValue submits the current value
func (e *Editor) submitValue() {
	if e.disableSubmit {
		return
	}
	text := e.GetExpandedText()
	e.AddToHistory(text)
	if e.onSubmit != nil {
		e.onSubmit(text)
	}
}

// handleBackspace handles backspace key
func (e *Editor) handleBackspace() {
	e.historyIndex = -1
	e.lastAction = ""

	if e.cursorCol > 0 {
		e.pushUndoSnapshot()

		// Delete grapheme before cursor (handles emojis, combining characters, etc.)
		line := e.lines[e.cursorLine]
		runes := []rune(line)
		before := string(runes[:e.cursorCol])
		graphemes := utils.GetGraphemes(before)
		graphemeLength := 1
		if len(graphemes) > 0 {
			lastGrapheme := graphemes[len(graphemes)-1]
			graphemeLength = len([]rune(lastGrapheme))
		}

		newRunes := make([]rune, 0, len(runes)-graphemeLength)
		newRunes = append(newRunes, runes[:e.cursorCol-graphemeLength]...)
		newRunes = append(newRunes, runes[e.cursorCol:]...)
		e.lines[e.cursorLine] = string(newRunes)
		e.setCursorCol(e.cursorCol - graphemeLength)
	} else if e.cursorLine > 0 {
		e.pushUndoSnapshot()

		// Join with previous line
		prevLine := e.lines[e.cursorLine-1]
		currLine := e.lines[e.cursorLine]
		e.lines[e.cursorLine-1] = prevLine + currLine
		// Remove current line
		e.lines = append(e.lines[:e.cursorLine], e.lines[e.cursorLine+1:]...)
		e.cursorLine--
		e.setCursorCol(e.runeCount(prevLine))
	}

	if e.onChange != nil {
		e.onChange(e.GetText())
	}

	// Update or re-trigger autocomplete after backspace
	if e.autocompleteState != "" {
		e.updateAutocomplete()
	} else {
		e.retriggerAutocompleteIfNeeded()
	}
}

// setCursorCol sets the cursor column (rune index)
func (e *Editor) setCursorCol(col int) {
	lineLen := e.runeCount(e.lines[e.cursorLine])
	if col < 0 {
		col = 0
	} else if col > lineLen {
		col = lineLen
	}
	e.cursorCol = col
	e.preferredVisualCol = nil
	if e.tui != nil {
		e.tui.RequestRender()
	}
}

// moveToLineStart moves cursor to start of line
func (e *Editor) moveToLineStart() {
	e.cursorCol = 0
	e.preferredVisualCol = nil
	if e.tui != nil {
		e.tui.RequestRender()
	}
}

// moveToLineEnd moves cursor to end of line
func (e *Editor) moveToLineEnd() {
	e.cursorCol = e.runeCount(e.lines[e.cursorLine])
	e.preferredVisualCol = nil
	if e.tui != nil {
		e.tui.RequestRender()
	}
}

// deleteToStartOfLine deletes from cursor to start of line
func (e *Editor) deleteToStartOfLine() {
	e.historyIndex = -1
	line := e.lines[e.cursorLine]

	if e.cursorCol > 0 {
		e.pushUndoSnapshot()
		runes := []rune(line)
		deleted := string(runes[:e.cursorCol])
		e.killRing.Push(deleted, utils.PushOptions{Accumulate: e.lastAction == "kill", Prepend: true})
		e.lastAction = "kill"
		e.lines[e.cursorLine] = string(runes[e.cursorCol:])
		e.setCursorCol(0)
	} else if e.cursorLine > 0 {
		e.pushUndoSnapshot()
		// At start of line - merge with previous line
		e.killRing.Push("\n", utils.PushOptions{Accumulate: e.lastAction == "kill", Prepend: true})
		e.lastAction = "kill"
		prevLine := e.lines[e.cursorLine-1]
		e.lines[e.cursorLine-1] = prevLine + line
		e.lines = append(e.lines[:e.cursorLine], e.lines[e.cursorLine+1:]...)
		e.cursorLine--
		e.setCursorCol(e.runeCount(prevLine))
	}

	if e.onChange != nil {
		e.onChange(e.GetText())
	}
}

// deleteToEndOfLine deletes from cursor to end of line
func (e *Editor) deleteToEndOfLine() {
	e.historyIndex = -1
	line := e.lines[e.cursorLine]
	runes := []rune(line)

	if e.cursorCol < len(runes) {
		e.pushUndoSnapshot()
		deleted := string(runes[e.cursorCol:])
		e.killRing.Push(deleted, utils.PushOptions{Accumulate: e.lastAction == "kill", Prepend: false})
		e.lastAction = "kill"
		e.lines[e.cursorLine] = string(runes[:e.cursorCol])
	} else if e.cursorLine < len(e.lines)-1 {
		e.pushUndoSnapshot()
		// At end of line - merge with next line
		e.killRing.Push("\n", utils.PushOptions{Accumulate: e.lastAction == "kill", Prepend: false})
		e.lastAction = "kill"
		nextLine := e.lines[e.cursorLine+1]
		e.lines[e.cursorLine] = line + nextLine
		e.lines = append(e.lines[:e.cursorLine+1], e.lines[e.cursorLine+2:]...)
	}

	if e.onChange != nil {
		e.onChange(e.GetText())
	}
}

// deleteWordBackwards deletes the word before cursor
func (e *Editor) deleteWordBackwards() {
	e.historyIndex = -1
	line := e.lines[e.cursorLine]

	if e.cursorCol == 0 {
		if e.cursorLine > 0 {
			e.pushUndoSnapshot()
			// At start of line - merge with previous line
			e.killRing.Push("\n", utils.PushOptions{Accumulate: e.lastAction == "kill", Prepend: true})
			e.lastAction = "kill"
			prevLine := e.lines[e.cursorLine-1]
			e.lines[e.cursorLine-1] = prevLine + line
			e.lines = append(e.lines[:e.cursorLine], e.lines[e.cursorLine+1:]...)
			e.cursorLine--
			e.setCursorCol(e.runeCount(prevLine))
		}
	} else {
		e.pushUndoSnapshot()
		wasKill := e.lastAction == "kill"

		oldCursorCol := e.cursorCol
		e.moveWordBackwards()
		deleteFrom := e.cursorCol
		e.cursorCol = oldCursorCol

		runes := []rune(line)
		deleted := string(runes[deleteFrom:e.cursorCol])
		e.killRing.Push(deleted, utils.PushOptions{Accumulate: wasKill, Prepend: true})
		e.lastAction = "kill"

		newRunes := make([]rune, 0, len(runes)-(e.cursorCol-deleteFrom))
		newRunes = append(newRunes, runes[:deleteFrom]...)
		newRunes = append(newRunes, runes[e.cursorCol:]...)
		e.lines[e.cursorLine] = string(newRunes)
		e.setCursorCol(deleteFrom)
	}

	if e.onChange != nil {
		e.onChange(e.GetText())
	}
}

// deleteWordForward deletes the word after cursor
func (e *Editor) deleteWordForward() {
	e.historyIndex = -1
	line := e.lines[e.cursorLine]
	runes := []rune(line)

	if e.cursorCol >= len(runes) {
		if e.cursorLine < len(e.lines)-1 {
			e.pushUndoSnapshot()
			// At end of line - merge with next line
			e.killRing.Push("\n", utils.PushOptions{Accumulate: e.lastAction == "kill", Prepend: false})
			e.lastAction = "kill"
			nextLine := e.lines[e.cursorLine+1]
			e.lines[e.cursorLine] = line + nextLine
			e.lines = append(e.lines[:e.cursorLine+1], e.lines[e.cursorLine+2:]...)
		}
	} else {
		e.pushUndoSnapshot()
		wasKill := e.lastAction == "kill"

		oldCursorCol := e.cursorCol
		e.moveWordForwards()
		deleteTo := e.cursorCol
		e.cursorCol = oldCursorCol

		deleted := string(runes[e.cursorCol:deleteTo])
		e.killRing.Push(deleted, utils.PushOptions{Accumulate: wasKill, Prepend: false})
		e.lastAction = "kill"

		newRunes := make([]rune, 0, len(runes)-(deleteTo-e.cursorCol))
		newRunes = append(newRunes, runes[:e.cursorCol]...)
		newRunes = append(newRunes, runes[deleteTo:]...)
		e.lines[e.cursorLine] = string(newRunes)
	}

	if e.onChange != nil {
		e.onChange(e.GetText())
	}
}

// handleForwardDelete handles forward delete key
func (e *Editor) handleForwardDelete() {
	e.historyIndex = -1
	e.lastAction = ""

	line := e.lines[e.cursorLine]
	runes := []rune(line)
	if e.cursorCol < len(runes) {
		e.pushUndoSnapshot()

		// Delete grapheme at cursor position (handles emojis, combining characters, etc.)
		after := string(runes[e.cursorCol:])
		graphemes := utils.GetGraphemes(after)
		graphemeLength := 1
		if len(graphemes) > 0 {
			firstGrapheme := graphemes[0]
			graphemeLength = len([]rune(firstGrapheme))
		}

		newRunes := make([]rune, 0, len(runes)-graphemeLength)
		newRunes = append(newRunes, runes[:e.cursorCol]...)
		newRunes = append(newRunes, runes[e.cursorCol+graphemeLength:]...)
		e.lines[e.cursorLine] = string(newRunes)
	} else if e.cursorLine < len(e.lines)-1 {
		e.pushUndoSnapshot()

		// Join with next line
		nextLine := e.lines[e.cursorLine+1]
		e.lines[e.cursorLine] = line + nextLine
		e.lines = append(e.lines[:e.cursorLine+1], e.lines[e.cursorLine+2:]...)
	}

	if e.onChange != nil {
		e.onChange(e.GetText())
	}

	// Update or re-trigger autocomplete after forward delete
	if e.autocompleteState != "" {
		e.updateAutocomplete()
	} else {
		e.retriggerAutocompleteIfNeeded()
	}
}

// moveWordBackwards moves cursor back one word
func (e *Editor) moveWordBackwards() {
	line := e.lines[e.cursorLine]
	runes := []rune(line)
	if e.cursorCol == 0 {
		if e.cursorLine > 0 {
			e.cursorLine--
			e.cursorCol = e.runeCount(e.lines[e.cursorLine])
		}
		e.preferredVisualCol = nil
		if e.tui != nil {
			e.tui.RequestRender()
		}
		return
	}

	// Skip whitespace
	for e.cursorCol > 0 && utils.IsWhitespaceChar(runes[e.cursorCol-1]) {
		e.cursorCol--
	}
	// Skip word
	for e.cursorCol > 0 && !utils.IsWhitespaceChar(runes[e.cursorCol-1]) {
		e.cursorCol--
	}

	e.preferredVisualCol = nil
	if e.tui != nil {
		e.tui.RequestRender()
	}
}

// moveWordForwards moves cursor forward one word
func (e *Editor) moveWordForwards() {
	line := e.lines[e.cursorLine]
	runes := []rune(line)
	if e.cursorCol >= len(runes) {
		if e.cursorLine < len(e.lines)-1 {
			e.cursorLine++
			e.cursorCol = 0
		}
		e.preferredVisualCol = nil
		if e.tui != nil {
			e.tui.RequestRender()
		}
		return
	}

	// Skip whitespace
	for e.cursorCol < len(runes) && utils.IsWhitespaceChar(runes[e.cursorCol]) {
		e.cursorCol++
	}
	// Skip word
	for e.cursorCol < len(runes) && !utils.IsWhitespaceChar(runes[e.cursorCol]) {
		e.cursorCol++
	}

	e.preferredVisualCol = nil
	if e.tui != nil {
		e.tui.RequestRender()
	}
}

// yank inserts the most recent kill ring entry
func (e *Editor) yank() {
	text := e.killRing.Yank()
	if text != "" {
		e.pushUndoSnapshot()
		e.insertTextAtCursorInternal(text)
		e.lastAction = "yank"
		if e.onChange != nil {
			e.onChange(e.GetText())
		}
	}
}

// yankPop cycles through kill ring entries
func (e *Editor) yankPop() {
	if e.lastAction != "yank" {
		return
	}
	text := e.killRing.YankPop()
	if text != "" {
		e.deleteYankedText()
		e.insertTextAtCursorInternal(text)
		if e.onChange != nil {
			e.onChange(e.GetText())
		}
	}
}

// deleteYankedText deletes the previously yanked text
func (e *Editor) deleteYankedText() {
	// This is a simplified version - in practice we'd track what was yanked
}

// pushUndoSnapshot saves the current state for undo
func (e *Editor) pushUndoSnapshot() {
	linesCopy := make([]string, len(e.lines))
	copy(linesCopy, e.lines)
	e.undoStack.Push(EditorState{
		Lines:      linesCopy,
		CursorLine: e.cursorLine,
		CursorCol:  e.cursorCol,
	})
}

// undo reverts the last change
func (e *Editor) undo() {
	e.historyIndex = -1
	state, ok := e.undoStack.PopWithOK()
	if !ok {
		return
	}
	e.lines = state.Lines
	e.cursorLine = state.CursorLine
	e.cursorCol = state.CursorCol
	e.lastAction = ""
	e.preferredVisualCol = nil
	if e.tui != nil {
		e.tui.RequestRender()
	}
	if e.onChange != nil {
		e.onChange(e.GetText())
	}
}

// jumpToChar jumps to the first occurrence of a character in the specified direction.
// Multi-line search. Case-sensitive. Skips the current cursor position.
func (e *Editor) jumpToChar(char string, direction string) {
	e.lastAction = ""
	isForward := direction == "forward"

	var end, step int
	if isForward {
		end = len(e.lines)
		step = 1
	} else {
		end = -1
		step = -1
	}

	for lineIdx := e.cursorLine; lineIdx != end; lineIdx += step {
		line := e.lines[lineIdx]
		isCurrentLine := lineIdx == e.cursorLine

		if isForward {
			searchFrom := 0
			if isCurrentLine {
				searchFrom = e.cursorCol + 1
			}
			runes := []rune(line)
			for i := searchFrom; i < len(runes); i++ {
				if string(runes[i]) == char {
					e.cursorLine = lineIdx
					e.setCursorCol(i)
					return
				}
			}
		} else {
			searchTo := len([]rune(line)) - 1
			if isCurrentLine {
				searchTo = e.cursorCol - 1
			}
			runes := []rune(line)
			for i := searchTo; i >= 0; i-- {
				if string(runes[i]) == char {
					e.cursorLine = lineIdx
					e.setCursorCol(i)
					return
				}
			}
		}
	}
}

// moveCursor moves the cursor by delta lines/columns
func (e *Editor) moveCursor(deltaLine, deltaCol int) {
	e.lastAction = ""

	if deltaLine != 0 {
		targetLine := e.cursorLine + deltaLine
		if targetLine >= 0 && targetLine < len(e.lines) {
			e.cursorLine = targetLine
			// Clamp cursor column to line length
			lineLen := e.runeCount(e.lines[e.cursorLine])
			if e.cursorCol > lineLen {
				e.cursorCol = lineLen
			}
		}
	}

	if deltaCol != 0 {
		if deltaCol > 0 {
			// Moving right
			lineLen := e.runeCount(e.lines[e.cursorLine])
			if e.cursorCol < lineLen {
				e.cursorCol++
			} else if e.cursorLine < len(e.lines)-1 {
				e.cursorLine++
				e.cursorCol = 0
			}
		} else {
			// Moving left
			if e.cursorCol > 0 {
				e.cursorCol--
			} else if e.cursorLine > 0 {
				e.cursorLine--
				e.cursorCol = e.runeCount(e.lines[e.cursorLine])
			}
		}
	}

	e.preferredVisualCol = nil
	if e.tui != nil {
		e.tui.RequestRender()
	}
}

// HandleInput processes keyboard input
func (e *Editor) HandleInput(data string) {
	// Parse key ID using keys package (supports both legacy and Kitty keyboard protocol)
	keyID := keys.ParseKey(data)

	// Handle character jump mode
	if e.jumpMode != "" {
		// Cancel if jump hotkey pressed again
		if keyID == "ctrl+]" || keyID == "ctrl+alt+]" {
			e.jumpMode = ""
			return
		}
		if len(data) == 1 && data[0] >= 32 {
			direction := e.jumpMode
			e.jumpMode = ""
			e.jumpToChar(data, direction)
			return
		}
		e.jumpMode = ""
	}

	// Handle bracketed paste
	if strings.Contains(data, "\x1b[200~") {
		e.isInPaste = true
		e.pasteBuffer = ""
		data = strings.Replace(data, "\x1b[200~", "", -1)
	}

	if e.isInPaste {
		e.pasteBuffer += data
		endIndex := strings.Index(e.pasteBuffer, "\x1b[201~")
		if endIndex != -1 {
			pasteContent := e.pasteBuffer[:endIndex]
			if pasteContent != "" {
				e.handlePaste(pasteContent)
			}
			e.isInPaste = false
			remaining := e.pasteBuffer[endIndex+6:]
			e.pasteBuffer = ""
			if remaining != "" {
				e.HandleInput(remaining)
			}
		}
		return
	}

	// Ctrl+C - let parent handle
	if keyID == "ctrl+c" {
		return
	}

	// Undo (legacy and modern aliases)
	if keyID == "ctrl+-" || keyID == "ctrl+z" {
		e.undo()
		return
	}

	// Handle autocomplete mode
	if e.autocompleteState != "" && e.autocompleteList != nil {
		if keyID == "escape" || keyID == "ctrl+c" {
			e.cancelAutocomplete()
			if e.tui != nil {
				e.tui.RequestRender()
			}
			return
		}
		if keyID == "up" || keyID == "down" || keyID == "pageUp" || keyID == "pageDown" {
			e.autocompleteList.HandleInput(data)
			return
		}
		if keyID == "tab" {
			e.applyAutocomplete()
			e.cancelAutocomplete()
			if e.onChange != nil {
				e.onChange(e.GetText())
			}
			return
		}
		if keyID == "enter" {
			isSlash := strings.HasPrefix(e.autocompletePrefix, "/")
			e.applyAutocomplete()
			e.cancelAutocomplete()
			if isSlash {
				// Fall through to submit for slash commands
			} else {
				if e.onChange != nil {
					e.onChange(e.GetText())
				}
				return
			}
		}
	}

	// Tab without autocomplete — trigger completion or insert literal tab
	if keyID == "tab" && e.autocompleteState == "" {
		if e.autocompleteProvider != nil {
			e.handleTabCompletion()
		} else {
			e.pushUndoSnapshot()
			e.insertTextAtCursorInternal("\t")
		}
		return
	}

	// Delete to line end
	if keyID == "ctrl+k" {
		e.deleteToEndOfLine()
		return
	}

	// Delete to line start
	if keyID == "ctrl+u" {
		e.deleteToStartOfLine()
		return
	}

	// Delete word backward
	if keyID == "ctrl+w" || keyID == "alt+backspace" || keyID == "ctrl+backspace" {
		e.deleteWordBackwards()
		return
	}

	// Delete word forward
	if keyID == "alt+w" || keyID == "alt+d" || keyID == "alt+delete" || keyID == "ctrl+delete" {
		e.deleteWordForward()
		return
	}

	// Backspace
	if keyID == "backspace" {
		e.handleBackspace()
		return
	}

	// Delete
	if keyID == "delete" {
		e.handleForwardDelete()
		return
	}

	// Yank
	if keyID == "ctrl+y" {
		e.yank()
		return
	}

	// Yank pop
	if keyID == "alt+y" {
		e.yankPop()
		return
	}

	// Cursor line start
	if keyID == "home" || keyID == "ctrl+a" {
		e.moveToLineStart()
		return
	}

	// Cursor line end
	if keyID == "end" || keyID == "ctrl+e" {
		e.moveToLineEnd()
		return
	}

	// Cursor word left
	if keyID == "alt+b" || keyID == "alt+left" || keyID == "ctrl+left" {
		e.moveWordBackwards()
		return
	}

	// Cursor word right
	if keyID == "alt+f" || keyID == "alt+right" || keyID == "ctrl+right" {
		e.moveWordForwards()
		return
	}

	// Shift+Enter, Alt+Enter, Ctrl+Enter — insert newline without submitting
	if keyID == "shift+enter" || keyID == "alt+enter" || keyID == "ctrl+enter" {
		e.addNewLine()
		return
	}

	// Submit on Enter
	if keyID == "enter" {
		// Check for backslash+enter behavior
		line := e.lines[e.cursorLine]
		if e.cursorCol > 0 {
			// Need to check the rune at cursorCol-1
			runes := []rune(line)
			if e.cursorCol <= len(runes) && runes[e.cursorCol-1] == '\\' {
				// Remove backslash and add newline
				newRunes := make([]rune, 0, len(runes)-1)
				newRunes = append(newRunes, runes[:e.cursorCol-1]...)
				newRunes = append(newRunes, runes[e.cursorCol:]...)
				e.lines[e.cursorLine] = string(newRunes)
				e.cursorCol--
				e.addNewLine()
				return
			}
		}
		e.submitValue()
		return
	}

	// Cursor up (with history support)
	if keyID == "up" {
		if e.isEditorEmpty() {
			e.navigateHistory(-1)
		} else if e.historyIndex > -1 && e.cursorLine == 0 {
			e.navigateHistory(-1)
		} else if e.cursorLine == 0 {
			e.moveToLineStart()
		} else {
			e.moveCursor(-1, 0)
		}
		return
	}

	// Cursor down (with history support)
	if keyID == "down" {
		if e.historyIndex > -1 && e.cursorLine == len(e.lines)-1 {
			e.navigateHistory(1)
		} else if e.cursorLine == len(e.lines)-1 {
			e.moveToLineEnd()
		} else {
			e.moveCursor(1, 0)
		}
		return
	}

	// Cursor left
	if keyID == "left" || keyID == "ctrl+b" {
		e.moveCursor(0, -1)
		return
	}

	// Cursor right
	if keyID == "right" || keyID == "ctrl+f" {
		e.moveCursor(0, 1)
		return
	}

	// Jump forward
	if keyID == "ctrl+]" {
		e.jumpMode = "forward"
		return
	}

	// Jump backward
	if keyID == "ctrl+alt+]" {
		e.jumpMode = "backward"
		return
	}

	// Check for Kitty CSI-u printable sequences
	if decoded := DecodeKittyPrintable(data); decoded != "" {
		e.insertCharacter(decoded)
		return
	}

	// Regular text input - accept printable characters
	// Check ALL runes for control characters (matching TS behavior)
	if len(data) > 0 {
		hasControlChars := false
		for _, r := range data {
			if r < 32 || r == 0x7f || (r >= 0x80 && r <= 0x9f) {
				hasControlChars = true
				break
			}
		}
		if !hasControlChars {
			e.insertCharacter(data)
		}
	}
}

// navigateHistory handles history navigation
func (e *Editor) navigateHistory(direction int) {
	e.lastAction = ""
	if len(e.history) == 0 {
		return
	}

	newIndex := e.historyIndex - direction // Up(-1) increases index, Down(1) decreases
	if newIndex < -1 || newIndex >= len(e.history) {
		return
	}

	// Capture state when first entering history
	if e.historyIndex == -1 && newIndex >= 0 {
		e.pushUndoSnapshot()
	}

	e.historyIndex = newIndex

	if e.historyIndex == -1 {
		e.setTextInternal("")
	} else {
		e.setTextInternal(e.history[e.historyIndex])
	}
}

// cancelAutocomplete cancels autocomplete and any in-flight async completion.
func (e *Editor) cancelAutocomplete() {
	e.acMutex.Lock()
	if e.acCancelFunc != nil {
		e.acCancelFunc()
		e.acCancelFunc = nil
	}
	e.acInFlight = false
	e.acMutex.Unlock()
	e.autocompleteState = ""
	e.autocompleteList = nil
	e.autocompletePrefix = ""
}

// IsShowingAutocomplete returns whether the autocomplete popup is visible.
func (e *Editor) IsShowingAutocomplete() bool {
	return e.autocompleteState != ""
}

// GetAutocompleteList returns the current autocomplete SelectList, or nil.
func (e *Editor) GetAutocompleteList() *SelectList {
	if e.autocompleteState == "" {
		return nil
	}
	return e.autocompleteList
}

// isSlashMenuAllowed returns true if slash commands are allowed (cursor on first line).
func (e *Editor) isSlashMenuAllowed() bool {
	return e.cursorLine == 0
}

// isAtStartOfMessage returns true if the cursor is at the start of the message
// (for slash command detection).
func (e *Editor) isAtStartOfMessage() bool {
	if !e.isSlashMenuAllowed() {
		return false
	}
	line := e.lines[e.cursorLine]
	runes := []rune(line)
	col := e.cursorCol
	if col > len(runes) {
		col = len(runes)
	}
	beforeCursor := strings.TrimSpace(string(runes[:col]))
	return beforeCursor == "" || beforeCursor == "/"
}

// isInSlashCommandContext returns true if textBeforeCursor starts with '/'.
func (e *Editor) isInSlashCommandContext(textBeforeCursor string) bool {
	return e.isSlashMenuAllowed() && strings.HasPrefix(strings.TrimLeft(textBeforeCursor, " \t"), "/")
}

// tryTriggerAutocomplete asks the provider for suggestions and opens the popup.
// This runs completion asynchronously so the TUI remains responsive.
func (e *Editor) tryTriggerAutocomplete(explicitTab bool) {
	if e.autocompleteProvider == nil {
		return
	}

	if explicitTab {
		type forceChecker interface {
			ShouldTriggerFileCompletion(lines []string, cursorLine, cursorCol int) bool
		}
		if fc, ok := e.autocompleteProvider.(forceChecker); ok {
			if !fc.ShouldTriggerFileCompletion(e.lines, e.cursorLine, e.cursorCol) {
				return
			}
		}
	}

	// Cancel any existing async completion
	e.acMutex.Lock()
	if e.acCancelFunc != nil {
		e.acCancelFunc()
	}
	ctx, cancel := context.WithCancel(context.Background())
	e.acCancelFunc = cancel
	e.acInFlight = true
	e.acMutex.Unlock()

	// Capture current state for the async operation
	lines := make([]string, len(e.lines))
	copy(lines, e.lines)
	cursorLine := e.cursorLine
	cursorCol := e.cursorCol
	provider := e.autocompleteProvider
	maxVisible := e.autocompleteMaxVisible
	theme := e.theme.SelectList

	// Run completion in a goroutine
	go func() {
		// Check if the provider supports context-aware completion
		type contextProvider interface {
			GetSuggestionsWithContext(ctx context.Context, lines []string, cursorLine, cursorCol int) *autocomplete.SuggestionsResult
		}

		var suggestions *autocomplete.SuggestionsResult
		if cp, ok := provider.(contextProvider); ok {
			suggestions = cp.GetSuggestionsWithContext(ctx, lines, cursorLine, cursorCol)
		} else {
			// Fallback to synchronous completion for providers that don't support context
			suggestions = provider.GetSuggestions(lines, cursorLine, cursorCol)
		}

		// Check if context was cancelled before applying results
		select {
		case <-ctx.Done():
			return
		default:
		}

		// Apply results on the main thread via RequestRender callback mechanism
		// We use a closure to update the editor state
		if suggestions != nil && len(suggestions.Items) > 0 {
			// Update editor state - need to check if this completion is still valid
			e.acMutex.Lock()
			isCurrent := e.acInFlight && e.acCancelFunc != nil
			e.acInFlight = false
			e.acCancelFunc = nil
			e.acMutex.Unlock()

			if isCurrent && e.tui != nil {
				e.autocompletePrefix = suggestions.Prefix
				e.autocompleteList = NewSelectList(
					autocompleteItemsToSelectItems(suggestions.Items),
					maxVisible,
					theme,
				)
				e.autocompleteState = "regular"
				e.tui.RequestRender()
			}
		} else {
			e.acMutex.Lock()
			e.acInFlight = false
			e.acCancelFunc = nil
			e.acMutex.Unlock()
			// Only cancel if we're still the current completion
			if e.tui != nil {
				e.tui.RequestRender()
			}
		}
	}()
}

// handleTabCompletion handles Tab key when no autocomplete is showing.
func (e *Editor) handleTabCompletion() {
	if e.autocompleteProvider == nil {
		return
	}
	line := e.lines[e.cursorLine]
	runes := []rune(line)
	col := e.cursorCol
	if col > len(runes) {
		col = len(runes)
	}
	beforeCursor := string(runes[:col])
	if e.isInSlashCommandContext(beforeCursor) && !strings.Contains(strings.TrimLeft(beforeCursor, " \t"), " ") {
		e.tryTriggerAutocomplete(true)
	} else {
		e.forceFileAutocomplete(true)
	}
}

// forceFileAutocomplete forces file completion using GetForceFileSuggestions.
// Delegates to the async version.
func (e *Editor) forceFileAutocomplete(explicitTab bool) {
	e.forceFileAutocompleteAsync(explicitTab)
}

// forceFileAutocompleteAsync runs file completion asynchronously.
func (e *Editor) forceFileAutocompleteAsync(explicitTab bool) {
	if e.autocompleteProvider == nil {
		return
	}
	type forceProvider interface {
		GetForceFileSuggestions(lines []string, cursorLine, cursorCol int) *autocomplete.SuggestionsResult
	}
	fp, ok := e.autocompleteProvider.(forceProvider)
	if !ok {
		e.tryTriggerAutocomplete(true)
		return
	}

	// Cancel any existing async completion
	e.acMutex.Lock()
	if e.acCancelFunc != nil {
		e.acCancelFunc()
	}
	ctx, cancel := context.WithCancel(context.Background())
	e.acCancelFunc = cancel
	e.acInFlight = true
	e.acMutex.Unlock()

	// Capture current state for the async operation
	lines := make([]string, len(e.lines))
	copy(lines, e.lines)
	cursorLine := e.cursorLine
	cursorCol := e.cursorCol
	maxVisible := e.autocompleteMaxVisible
	theme := e.theme.SelectList
	doExplicitTab := explicitTab

	// Run completion in a goroutine
	go func() {
		suggestions := fp.GetForceFileSuggestions(lines, cursorLine, cursorCol)

		// Check if context was cancelled before applying results
		select {
		case <-ctx.Done():
			return
		default:
		}

		// Apply results on the main thread via RequestRender callback mechanism
		if suggestions != nil && len(suggestions.Items) > 0 {
			// Handle single-item auto-apply case
			if doExplicitTab && len(suggestions.Items) == 1 {
				// Update state under lock to ensure consistency
				e.acMutex.Lock()
				isCurrent := e.acInFlight
				e.acInFlight = false
				e.acCancelFunc = nil
				e.acMutex.Unlock()

				if isCurrent && e.tui != nil {
					e.pushUndoSnapshot()
					e.lastAction = ""
					item := suggestions.Items[0]
					newLines, newCursorLine, newCursorCol := e.autocompleteProvider.ApplyCompletion(
						e.lines, e.cursorLine, e.cursorCol, item, suggestions.Prefix,
					)
					e.lines = newLines
					e.cursorLine = newCursorLine
					e.setCursorCol(newCursorCol)
					if e.onChange != nil {
						e.onChange(e.GetText())
					}
					e.tui.RequestRender()
				}
				return
			}

			// Multiple items - show the list
			e.acMutex.Lock()
			isCurrent := e.acInFlight && e.acCancelFunc != nil
			e.acInFlight = false
			e.acCancelFunc = nil
			e.acMutex.Unlock()

			if isCurrent && e.tui != nil {
				e.autocompletePrefix = suggestions.Prefix
				e.autocompleteList = NewSelectList(
					autocompleteItemsToSelectItems(suggestions.Items),
					maxVisible,
					theme,
				)
				e.autocompleteState = "force"
				e.tui.RequestRender()
			}
		} else {
			e.acMutex.Lock()
			isCurrent := e.acInFlight
			e.acInFlight = false
			e.acCancelFunc = nil
			e.acMutex.Unlock()

			if isCurrent && e.tui != nil {
				e.cancelAutocomplete()
				e.tui.RequestRender()
			}
		}
	}()
}

// updateAutocomplete re-queries the provider to refresh the popup.
// This runs completion asynchronously so the TUI remains responsive.
func (e *Editor) updateAutocomplete() {
	if e.autocompleteState == "" || e.autocompleteProvider == nil {
		return
	}
	if e.autocompleteState == "force" {
		e.forceFileAutocompleteAsync(false)
		return
	}

	// Cancel any existing async completion
	e.acMutex.Lock()
	if e.acCancelFunc != nil {
		e.acCancelFunc()
	}
	ctx, cancel := context.WithCancel(context.Background())
	e.acCancelFunc = cancel
	e.acInFlight = true
	e.acMutex.Unlock()

	// Capture current state for the async operation
	lines := make([]string, len(e.lines))
	copy(lines, e.lines)
	cursorLine := e.cursorLine
	cursorCol := e.cursorCol
	provider := e.autocompleteProvider
	maxVisible := e.autocompleteMaxVisible
	theme := e.theme.SelectList

	// Run completion in a goroutine
	go func() {
		// Check if the provider supports context-aware completion
		type contextProvider interface {
			GetSuggestionsWithContext(ctx context.Context, lines []string, cursorLine, cursorCol int) *autocomplete.SuggestionsResult
		}

		var suggestions *autocomplete.SuggestionsResult
		if cp, ok := provider.(contextProvider); ok {
			suggestions = cp.GetSuggestionsWithContext(ctx, lines, cursorLine, cursorCol)
		} else {
			suggestions = provider.GetSuggestions(lines, cursorLine, cursorCol)
		}

		// Check if context was cancelled before applying results
		select {
		case <-ctx.Done():
			return
		default:
		}

		// Apply results on the main thread via RequestRender callback mechanism
		if suggestions != nil && len(suggestions.Items) > 0 {
			e.acMutex.Lock()
			isCurrent := e.acInFlight && e.acCancelFunc != nil
			e.acInFlight = false
			e.acCancelFunc = nil
			e.acMutex.Unlock()

			if isCurrent && e.tui != nil {
				e.autocompletePrefix = suggestions.Prefix
				e.autocompleteList = NewSelectList(
					autocompleteItemsToSelectItems(suggestions.Items),
					maxVisible,
					theme,
				)
				e.autocompleteState = "regular"
				e.tui.RequestRender()
			}
		} else {
			e.acMutex.Lock()
			isCurrent := e.acInFlight
			e.acInFlight = false
			e.acCancelFunc = nil
			e.acMutex.Unlock()

			if isCurrent && e.tui != nil {
				e.cancelAutocomplete()
			}
		}
	}()
}

// applyAutocomplete applies the selected autocomplete item.
func (e *Editor) applyAutocomplete() {
	if e.autocompleteList == nil || e.autocompleteProvider == nil {
		return
	}
	selected := e.autocompleteList.GetSelectedItem()
	if selected == nil {
		e.cancelAutocomplete()
		return
	}
	e.pushUndoSnapshot()
	e.lastAction = ""
	item := autocomplete.AutocompleteItem{
		Value:       selected.Value,
		Label:       selected.Label,
		Description: selected.Description,
	}
	newLines, newCursorLine, newCursorCol := e.autocompleteProvider.ApplyCompletion(
		e.lines, e.cursorLine, e.cursorCol, item, e.autocompletePrefix,
	)
	e.lines = newLines
	e.cursorLine = newCursorLine
	e.setCursorCol(newCursorCol)
}

// retriggerAutocompleteIfNeeded re-triggers autocomplete after backspace/delete
// if the cursor is in a completable context.
func (e *Editor) retriggerAutocompleteIfNeeded() {
	line := e.lines[e.cursorLine]
	runes := []rune(line)
	col := e.cursorCol
	if col > len(runes) {
		col = len(runes)
	}
	textBeforeCursor := string(runes[:col])
	if e.isInSlashCommandContext(textBeforeCursor) {
		e.tryTriggerAutocomplete(false)
		return
	}
	// @ file reference context
	if atReferencePattern.MatchString(textBeforeCursor) {
		e.tryTriggerAutocomplete(false)
		return
	}
	// $ skill context
	if dollarReferencePattern.MatchString(textBeforeCursor) {
		e.tryTriggerAutocomplete(false)
	}
}

// autocompleteItemsToSelectItems converts autocomplete items to select items.
func autocompleteItemsToSelectItems(items []autocomplete.AutocompleteItem) []SelectItem {
	result := make([]SelectItem, len(items))
	for i, item := range items {
		result[i] = SelectItem{
			Value:       item.Value,
			Label:       item.Label,
			Description: item.Description,
		}
	}
	return result
}

// SetOnSubmit sets the submit callback
func (e *Editor) SetOnSubmit(fn func(string)) {
	e.onSubmit = fn
}

// SetOnChange sets the change callback
func (e *Editor) SetOnChange(fn func(string)) {
	e.onChange = fn
}

// Render renders the editor to lines
func (e *Editor) Render(width int) []string {
	if width < 1 {
		width = 1
	}

	maxPadding := max(0, (width-1)/2)
	paddingX := min(e.paddingX, maxPadding)
	contentWidth := max(1, width-paddingX*2)

	// Layout width: with padding the cursor can overflow into it,
	// without padding we reserve 1 column for the cursor.
	layoutWidth := contentWidth
	if paddingX == 0 {
		layoutWidth = max(1, contentWidth-1)
	}

	// Store for cursor navigation (must match wrapping width)
	e.lastWidth = layoutWidth

	leftPadding := strings.Repeat(" ", paddingX)
	rightPadding := strings.Repeat(" ", paddingX)

	// truncLine ensures a line never exceeds the terminal width
	truncLine := func(line string) string {
		return utils.TruncateToWidth(line, width, "")
	}

	// Build layout lines
	layoutLines := e.buildLayoutLines(layoutWidth)

	// Find cursor line
	cursorLineIndex := -1
	for i, line := range layoutLines {
		if line.HasCursor {
			cursorLineIndex = i
			break
		}
	}
	if cursorLineIndex == -1 {
		cursorLineIndex = 0
	}

	// Calculate scroll offset
	visibleHeight := 24 // Default
	if cursorLineIndex < e.scrollOffset {
		e.scrollOffset = cursorLineIndex
	} else if cursorLineIndex >= e.scrollOffset+visibleHeight {
		e.scrollOffset = cursorLineIndex - visibleHeight + 1
	}

	// Get visible lines
	endOffset := e.scrollOffset + visibleHeight
	if endOffset > len(layoutLines) {
		endOffset = len(layoutLines)
	}
	visibleLines := layoutLines[e.scrollOffset:endOffset]

	// Build result
	var result []string

	// Top border with scroll indicator
	horizontal := strings.Repeat("─", width)
	if e.scrollOffset > 0 {
		indicator := fmt.Sprintf("─── ↑ %d more ", e.scrollOffset)
		remaining := width - utils.VisibleWidth(indicator)
		if remaining < 0 {
			remaining = 0
		}
		result = append(result, truncLine(e.borderColor(indicator+strings.Repeat("─", remaining))))
	} else {
		result = append(result, e.borderColor(horizontal))
	}

	// Content lines
	// Emit hardware cursor marker only when focused and not showing autocomplete
	emitCursorMarker := e.focused && e.autocompleteState == ""

	applyShellColor := e.theme.ShellLineColor
	if applyShellColor == nil {
		applyShellColor = func(s string) string { return s }
	}

	applyForeground := e.theme.Foreground
	if applyForeground == nil {
		applyForeground = func(s string) string { return s }
	}

	for _, layoutLine := range visibleLines {
		text := layoutLine.Text
		displayText := text
		lineVisibleWidth := utils.VisibleWidth(text)
		cursorInPadding := false

		if layoutLine.HasCursor {
			before := text[:layoutLine.CursorPos]
			after := text[layoutLine.CursorPos:]

			marker := ""
			if emitCursorMarker {
				marker = tui.CURSOR_MARKER
			}

			if len(after) > 0 {
				// Cursor is on a character — highlight it with inverse video
				graphemes := utils.GetGraphemes(after)
				atCursor := " "
				if len(graphemes) > 0 {
					atCursor = graphemes[0]
				}
				restAfter := after[len(atCursor):]
				cursor := "\x1b[7m" + atCursor + "\x1b[0m"
				if layoutLine.IsShellLine {
					displayText = applyShellColor(before) + marker + cursor + applyShellColor(restAfter)
				} else {
					displayText = applyForeground(before) + marker + cursor + applyForeground(restAfter)
				}
				// lineVisibleWidth stays the same — we're replacing, not adding
			} else {
				// Cursor at end of line — add highlighted space
				cursor := "\x1b[7m \x1b[0m"
				if layoutLine.IsShellLine {
					displayText = applyShellColor(before) + marker + cursor
				} else {
					displayText = applyForeground(before) + marker + cursor
				}
				lineVisibleWidth = lineVisibleWidth + 1
				// If cursor overflows content width into the padding, flag it
				if lineVisibleWidth > contentWidth && e.paddingX > 0 {
					cursorInPadding = true
				}
			}
		} else if layoutLine.IsShellLine {
			displayText = applyShellColor(text)
		} else {
			displayText = applyForeground(text)
		}

		padding := strings.Repeat(" ", max(0, contentWidth-lineVisibleWidth))
		lineRightPadding := rightPadding
		if cursorInPadding {
			if len(rightPadding) > 0 {
				lineRightPadding = rightPadding[1:]
			}
		}
		result = append(result, truncLine(leftPadding+displayText+padding+lineRightPadding))
	}

	// Bottom border
	linesBelow := len(layoutLines) - (e.scrollOffset + len(visibleLines))
	if linesBelow > 0 {
		indicator := fmt.Sprintf("─── ↓ %d more ", linesBelow)
		remaining := width - utils.VisibleWidth(indicator)
		if remaining < 0 {
			remaining = 0
		}
		result = append(result, truncLine(e.borderColor(indicator+strings.Repeat("─", remaining))))
	} else {
		result = append(result, e.borderColor(horizontal))
	}

	// Add autocomplete list if active
	if e.autocompleteState != "" && e.autocompleteList != nil {
		acLines := e.autocompleteList.Render(contentWidth)
		for _, acLine := range acLines {
			lineWidth := utils.VisibleWidth(acLine)
			linePadding := strings.Repeat(" ", max(0, contentWidth-lineWidth))
			result = append(result, truncLine(leftPadding+acLine+linePadding+rightPadding))
		}
	}

	return result
}

// buildLayoutLines builds the layout from logical lines
func (e *Editor) buildLayoutLines(contentWidth int) []LayoutLine {
	var lines []LayoutLine

	for logicalLine, text := range e.lines {
		isShell := strings.HasPrefix(strings.TrimLeftFunc(text, unicode.IsSpace), "~~")
		if logicalLine == e.cursorLine {
			// This line has the cursor
			// Need to convert rune-based cursorCol to byte-based for WordWrapLine
			// Then map back for display
			chunks := WordWrapLine(text, contentWidth)
			cursorByteOffset := e.getByteOffsetForLine(logicalLine, e.cursorCol)

			for _, chunk := range chunks {
				// Check if cursor is in this chunk (by byte range)
				hasCursor := cursorByteOffset >= chunk.StartIndex && cursorByteOffset <= chunk.EndIndex
				cursorPos := 0
				if hasCursor {
					cursorPos = cursorByteOffset - chunk.StartIndex
					if cursorPos > len(chunk.Text) {
						cursorPos = len(chunk.Text)
					}
				}
				lines = append(lines, LayoutLine{
					Text:        chunk.Text,
					HasCursor:   hasCursor,
					CursorPos:   cursorPos,
					IsShellLine: isShell,
				})
			}
		} else {
			// Line without cursor
			chunks := WordWrapLine(text, contentWidth)
			for _, chunk := range chunks {
				lines = append(lines, LayoutLine{
					Text:        chunk.Text,
					HasCursor:   false,
					IsShellLine: isShell,
				})
			}
		}
	}

	return lines
}

// getByteOffsetForLine returns the byte offset for a given rune index in a specific line
func (e *Editor) getByteOffsetForLine(lineIdx, runeIdx int) int {
	if lineIdx < 0 || lineIdx >= len(e.lines) {
		return 0
	}
	line := e.lines[lineIdx]
	runes := []rune(line)
	if runeIdx <= 0 {
		return 0
	}
	if runeIdx >= len(runes) {
		return len(line)
	}
	offset := 0
	for i := 0; i < runeIdx && i < len(runes); i++ {
		offset += len(string(runes[i]))
	}
	return offset
}
