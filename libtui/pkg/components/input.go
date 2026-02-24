package components

import (
	"strings"

	"github.com/francescoalemanno/raijin-mono/libtui/pkg/keybindings"
	"github.com/francescoalemanno/raijin-mono/libtui/pkg/tui"
	"github.com/francescoalemanno/raijin-mono/libtui/pkg/utils"
)

// InputState represents the state of an input (for undo).
type InputState struct {
	Value  string
	Cursor int
}

// Input is a single-line text input with horizontal scrolling and bracketed paste support.
type Input struct {
	value    string
	cursor   int
	onSubmit func(string)
	onEscape func()
	focused  bool

	// Bracketed paste mode buffering
	pasteBuffer string
	isInPaste   bool

	// Kill ring for Emacs-style kill/yank operations
	killRing   *utils.KillRing
	lastAction string // "kill", "yank", "type-word", or ""

	// Undo support
	undoStack *utils.UndoStack[InputState]
}

// NewInput creates a new Input component.
func NewInput() *Input {
	return &Input{
		undoStack: utils.NewUndoStack[InputState](),
		killRing:  utils.NewKillRing(),
		focused:   false,
	}
}

// GetValue returns the current input value.
func (i *Input) GetValue() string {
	return i.value
}

// SetValue sets the input value and clamps the cursor.
func (i *Input) SetValue(value string) {
	i.value = value
	if i.cursor > len(value) {
		i.cursor = len(value)
	}
}

// SetOnSubmit sets the callback for when the input is submitted.
func (i *Input) SetOnSubmit(fn func(string)) {
	i.onSubmit = fn
}

// SetOnEscape sets the callback for when escape is pressed.
func (i *Input) SetOnEscape(fn func()) {
	i.onEscape = fn
}

// Invalidate satisfies Component interface.
func (i *Input) Invalidate() {
	// No cached state to invalidate currently
}

// HandleInput handles keyboard input.
func (i *Input) HandleInput(data string) {
	// Handle bracketed paste mode
	// Start of paste: \x1b[200~
	// End of paste: \x1b[201~

	// Check if we're starting a bracketed paste
	if strings.Contains(data, "\x1b[200~") {
		i.isInPaste = true
		i.pasteBuffer = ""
		data = strings.ReplaceAll(data, "\x1b[200~", "")
	}

	// If we're in a paste, buffer the data
	if i.isInPaste {
		i.pasteBuffer += data

		// Check if this chunk contains the end marker
		endIndex := strings.Index(i.pasteBuffer, "\x1b[201~")
		if endIndex >= 0 {
			// Extract the pasted content
			pasteContent := i.pasteBuffer[:endIndex]

			// Process the complete paste
			i.handlePaste(pasteContent)

			// Reset paste state
			i.isInPaste = false

			// Handle any remaining input after the paste marker
			remaining := i.pasteBuffer[endIndex+6:] // 6 = len "\x1b[201~"
			i.pasteBuffer = ""
			if remaining != "" {
				i.HandleInput(remaining)
			}
		}
		return
	}

	kb := keybindings.GetEditorKeybindings()

	// Escape/Cancel
	if kb.Matches(data, keybindings.ActionSelectCancel) {
		if i.onEscape != nil {
			i.onEscape()
		}
		return
	}

	// Undo
	if kb.Matches(data, keybindings.ActionUndo) {
		i.undo()
		return
	}

	// Submit
	if kb.Matches(data, keybindings.ActionSubmit) || data == "\n" {
		if i.onSubmit != nil {
			i.onSubmit(i.value)
		}
		return
	}

	// Deletion
	if kb.Matches(data, keybindings.ActionDeleteCharBackward) {
		i.handleBackspace()
		return
	}

	if kb.Matches(data, keybindings.ActionDeleteCharForward) {
		i.handleForwardDelete()
		return
	}

	if kb.Matches(data, keybindings.ActionDeleteWordBackward) {
		i.deleteWordBackwards()
		return
	}

	if kb.Matches(data, keybindings.ActionDeleteWordForward) {
		i.deleteWordForward()
		return
	}

	if kb.Matches(data, keybindings.ActionDeleteToLineStart) {
		i.deleteToLineStart()
		return
	}

	if kb.Matches(data, keybindings.ActionDeleteToLineEnd) {
		i.deleteToLineEnd()
		return
	}

	// Kill ring actions
	if kb.Matches(data, keybindings.ActionYank) {
		i.yank()
		return
	}

	if kb.Matches(data, keybindings.ActionYankPop) {
		i.yankPop()
		return
	}

	// Cursor movement
	if kb.Matches(data, keybindings.ActionCursorLeft) {
		i.lastAction = ""
		if i.cursor > 0 {
			beforeCursor := i.value[:i.cursor]
			graphemes := utils.GetGraphemes(beforeCursor)
			if len(graphemes) > 0 {
				lastGrapheme := graphemes[len(graphemes)-1]
				i.cursor -= len(lastGrapheme)
			}
		}
		return
	}

	if kb.Matches(data, keybindings.ActionCursorRight) {
		i.lastAction = ""
		if i.cursor < len(i.value) {
			afterCursor := i.value[i.cursor:]
			graphemes := utils.GetGraphemes(afterCursor)
			if len(graphemes) > 0 {
				firstGrapheme := graphemes[0]
				i.cursor += len(firstGrapheme)
			}
		}
		return
	}

	if kb.Matches(data, keybindings.ActionCursorLineStart) {
		i.lastAction = ""
		i.cursor = 0
		return
	}

	if kb.Matches(data, keybindings.ActionCursorLineEnd) {
		i.lastAction = ""
		i.cursor = len(i.value)
		return
	}

	if kb.Matches(data, keybindings.ActionCursorWordLeft) {
		i.moveWordBackwards()
		return
	}

	if kb.Matches(data, keybindings.ActionCursorWordRight) {
		i.moveWordForwards()
		return
	}

	// Regular character input - accept printable characters including Unicode,
	// but reject control characters (C0: 0x00-0x1F, DEL: 0x7F, C1: 0x80-0x9F)
	hasControlChars := false
	for _, ch := range data {
		code := ch
		if code < 32 || code == 0x7f || (code >= 0x80 && code <= 0x9f) {
			hasControlChars = true
			break
		}
	}
	if !hasControlChars {
		i.insertCharacter(data)
	}
}

func (i *Input) insertCharacter(char string) {
	// Undo coalescing: consecutive word chars coalesce into one undo unit
	if utils.IsWhitespaceChar(char) || i.lastAction != "type-word" {
		i.pushUndo()
	}
	i.lastAction = "type-word"

	i.value = i.value[:i.cursor] + char + i.value[i.cursor:]
	i.cursor += len(char)
}

func (i *Input) handleBackspace() {
	i.lastAction = ""
	if i.cursor > 0 {
		i.pushUndo()
		beforeCursor := i.value[:i.cursor]
		graphemes := utils.GetGraphemes(beforeCursor)
		graphemeLength := 1
		if len(graphemes) > 0 {
			lastGrapheme := graphemes[len(graphemes)-1]
			graphemeLength = len(lastGrapheme)
		}
		i.value = i.value[:i.cursor-graphemeLength] + i.value[i.cursor:]
		i.cursor -= graphemeLength
	}
}

func (i *Input) handleForwardDelete() {
	i.lastAction = ""
	if i.cursor < len(i.value) {
		i.pushUndo()
		afterCursor := i.value[i.cursor:]
		graphemes := utils.GetGraphemes(afterCursor)
		graphemeLength := 1
		if len(graphemes) > 0 {
			firstGrapheme := graphemes[0]
			graphemeLength = len(firstGrapheme)
		}
		i.value = i.value[:i.cursor] + i.value[i.cursor+graphemeLength:]
	}
}

func (i *Input) deleteToLineStart() {
	if i.cursor == 0 {
		return
	}
	i.pushUndo()
	deletedText := i.value[:i.cursor]
	i.killRing.Push(deletedText, utils.PushOptions{Prepend: true, Accumulate: i.lastAction == "kill"})
	i.lastAction = "kill"
	i.value = i.value[i.cursor:]
	i.cursor = 0
}

func (i *Input) deleteToLineEnd() {
	if i.cursor >= len(i.value) {
		return
	}
	i.pushUndo()
	deletedText := i.value[i.cursor:]
	i.killRing.Push(deletedText, utils.PushOptions{Prepend: false, Accumulate: i.lastAction == "kill"})
	i.lastAction = "kill"
	i.value = i.value[:i.cursor]
}

func (i *Input) deleteWordBackwards() {
	if i.cursor == 0 {
		return
	}

	// Save lastAction before cursor movement (moveWordBackwards resets it)
	wasKill := i.lastAction == "kill"

	i.pushUndo()

	oldCursor := i.cursor
	i.moveWordBackwards()
	deleteFrom := i.cursor
	i.cursor = oldCursor

	deletedText := i.value[deleteFrom:i.cursor]
	i.killRing.Push(deletedText, utils.PushOptions{Prepend: true, Accumulate: wasKill})
	i.lastAction = "kill"

	i.value = i.value[:deleteFrom] + i.value[i.cursor:]
	i.cursor = deleteFrom
}

func (i *Input) deleteWordForward() {
	if i.cursor >= len(i.value) {
		return
	}

	// Save lastAction before cursor movement (moveWordForwards resets it)
	wasKill := i.lastAction == "kill"

	i.pushUndo()

	oldCursor := i.cursor
	i.moveWordForwards()
	deleteTo := i.cursor
	i.cursor = oldCursor

	deletedText := i.value[i.cursor:deleteTo]
	i.killRing.Push(deletedText, utils.PushOptions{Prepend: false, Accumulate: wasKill})
	i.lastAction = "kill"

	i.value = i.value[:i.cursor] + i.value[deleteTo:]
}

func (i *Input) yank() {
	text, hasText := i.killRing.PeekWithOK()
	if !hasText {
		return
	}

	i.pushUndo()

	i.value = i.value[:i.cursor] + text + i.value[i.cursor:]
	i.cursor += len(text)
	i.lastAction = "yank"
}

func (i *Input) yankPop() {
	if i.lastAction != "yank" || i.killRing.Length() <= 1 {
		return
	}

	i.pushUndo()

	// Delete the previously yanked text (still at end of ring before rotation)
	prevText, _ := i.killRing.PeekWithOK()
	i.value = i.value[:i.cursor-len(prevText)] + i.value[i.cursor:]
	i.cursor -= len(prevText)

	// Rotate and insert new entry
	i.killRing.Rotate()
	text, _ := i.killRing.PeekWithOK()
	i.value = i.value[:i.cursor] + text + i.value[i.cursor:]
	i.cursor += len(text)
	i.lastAction = "yank"
}

func (i *Input) pushUndo() {
	i.undoStack.Push(InputState{Value: i.value, Cursor: i.cursor})
}

func (i *Input) undo() {
	state, hasState := i.undoStack.PopWithOK()
	if !hasState {
		return
	}
	i.value = state.Value
	i.cursor = state.Cursor
	i.lastAction = ""
}

func (i *Input) moveWordBackwards() {
	if i.cursor == 0 {
		return
	}

	i.lastAction = ""
	textBeforeCursor := i.value[:i.cursor]
	graphemes := utils.GetGraphemes(textBeforeCursor)

	// Skip trailing whitespace
	for len(graphemes) > 0 {
		grapheme := graphemes[len(graphemes)-1]
		if len(grapheme) > 0 && utils.IsWhitespaceChar(rune(grapheme[0])) {
			i.cursor -= len(grapheme)
			graphemes = graphemes[:len(graphemes)-1]
		} else {
			break
		}
	}

	if len(graphemes) > 0 {
		lastGrapheme := graphemes[len(graphemes)-1]
		if len(lastGrapheme) > 0 && utils.IsPunctuationChar(rune(lastGrapheme[0])) {
			// Skip punctuation run
			for len(graphemes) > 0 {
				g := graphemes[len(graphemes)-1]
				if len(g) > 0 && utils.IsPunctuationChar(rune(g[0])) {
					i.cursor -= len(g)
					graphemes = graphemes[:len(graphemes)-1]
				} else {
					break
				}
			}
		} else {
			// Skip word run
			for len(graphemes) > 0 {
				g := graphemes[len(graphemes)-1]
				if len(g) > 0 &&
					!utils.IsWhitespaceChar(rune(g[0])) &&
					!utils.IsPunctuationChar(rune(g[0])) {
					i.cursor -= len(g)
					graphemes = graphemes[:len(graphemes)-1]
				} else {
					break
				}
			}
		}
	}
}

func (i *Input) moveWordForwards() {
	if i.cursor >= len(i.value) {
		return
	}

	i.lastAction = ""
	textAfterCursor := i.value[i.cursor:]
	graphemes := utils.GetGraphemes(textAfterCursor)

	// Skip leading whitespace
	for len(graphemes) > 0 {
		grapheme := graphemes[0]
		if len(grapheme) > 0 && utils.IsWhitespaceChar(rune(grapheme[0])) {
			i.cursor += len(grapheme)
			graphemes = graphemes[1:]
		} else {
			break
		}
	}

	if len(graphemes) > 0 {
		firstGrapheme := graphemes[0]
		if len(firstGrapheme) > 0 && utils.IsPunctuationChar(rune(firstGrapheme[0])) {
			// Skip punctuation run
			for len(graphemes) > 0 {
				g := graphemes[0]
				if len(g) > 0 && utils.IsPunctuationChar(rune(g[0])) {
					i.cursor += len(g)
					graphemes = graphemes[1:]
				} else {
					break
				}
			}
		} else {
			// Skip word run
			for len(graphemes) > 0 {
				g := graphemes[0]
				if len(g) > 0 &&
					!utils.IsWhitespaceChar(rune(g[0])) &&
					!utils.IsPunctuationChar(rune(g[0])) {
					i.cursor += len(g)
					graphemes = graphemes[1:]
				} else {
					break
				}
			}
		}
	}
}

func (i *Input) handlePaste(pastedText string) {
	i.lastAction = ""
	i.pushUndo()

	// Clean the pasted text - remove newlines and carriage returns
	cleanText := pastedText
	cleanText = strings.ReplaceAll(cleanText, "\r\n", "")
	cleanText = strings.ReplaceAll(cleanText, "\r", "")
	cleanText = strings.ReplaceAll(cleanText, "\n", "")

	// Insert at cursor position
	i.value = i.value[:i.cursor] + cleanText + i.value[i.cursor:]
	i.cursor += len(cleanText)
}

// Render renders the Input component with cursor.
func (i *Input) Render(width int) []string {
	if width < 1 {
		width = 1
	}
	// Calculate visible window
	const prompt = "> "
	availableWidth := width - len(prompt)

	if availableWidth <= 0 {
		return []string{utils.TruncateToWidth(prompt, width, "")}
	}

	visibleText := ""
	cursorDisplay := i.cursor

	if len(i.value) < availableWidth {
		// Everything fits (leave room for cursor at end)
		visibleText = i.value
	} else {
		// Need horizontal scrolling
		// Reserve one character for cursor if it's at the end
		scrollWidth := availableWidth
		if i.cursor == len(i.value) {
			scrollWidth = availableWidth - 1
		}
		halfWidth := scrollWidth / 2

		findValidStart := func(start int) int {
			for start < len(i.value) {
				code := rune(i.value[start])
				// Low surrogate (0xDC00-0xDFFF) is not a valid start
				if code >= 0xDC00 && code < 0xE000 {
					start++
					continue
				}
				break
			}
			return start
		}

		findValidEnd := func(end int) int {
			for end > 0 {
				code := rune(i.value[end-1])
				// High surrogate (0xD800-0xDBFF) might be split
				if code >= 0xD800 && code < 0xDC00 {
					end--
					continue
				}
				break
			}
			return end
		}

		if i.cursor < halfWidth {
			// Cursor near start
			visibleText = i.value[:findValidEnd(scrollWidth)]
			cursorDisplay = i.cursor
		} else if i.cursor > len(i.value)-halfWidth {
			// Cursor near end
			start := findValidStart(len(i.value) - scrollWidth)
			visibleText = i.value[start:]
			cursorDisplay = i.cursor - start
		} else {
			// Cursor in middle
			start := findValidStart(i.cursor - halfWidth)
			visibleText = i.value[start:findValidEnd(start+scrollWidth)]
			cursorDisplay = halfWidth
		}
	}

	// Build line with fake cursor
	// Insert cursor character at cursor position
	afterCursorPart := visibleText[cursorDisplay:]
	graphemes := utils.GetGraphemes(afterCursorPart)
	cursorChar := " "
	if len(graphemes) > 0 {
		cursorChar = graphemes[0]
	}

	beforeCursor := visibleText[:cursorDisplay]
	afterEnd := cursorDisplay + len(cursorChar)
	afterCursor := ""
	if afterEnd <= len(visibleText) {
		afterCursor = visibleText[afterEnd:]
	}

	// Hardware cursor marker (zero-width, emitted before fake cursor for IME positioning)
	marker := ""
	if i.focused {
		marker = tui.CURSOR_MARKER
	}

	// Use inverse video to show cursor - use reverse video ANSI codes
	cursorPosAnsi := "\x1b[7m" + cursorChar + "\x1b[27m"
	textWithCursor := beforeCursor + marker + cursorPosAnsi + afterCursor

	// Calculate visual width
	visualLength := utils.VisibleWidth(textWithCursor)
	padding := ""
	if availableWidth-visualLength > 0 {
		padding = strings.Repeat(" ", availableWidth-visualLength)
	}
	line := prompt + textWithCursor + padding

	return []string{line}
}

// SetFocused sets the focused state.
func (i *Input) SetFocused(focused bool) {
	i.focused = focused
}

// GetFocused returns the focused state.
func (i *Input) GetFocused() bool {
	return i.focused
}
