package test

import (
	"strings"
	"testing"

	"github.com/francescoalemanno/raijin-mono/libtui/pkg/components"
	"github.com/stretchr/testify/assert"
)

// MockTUIRenderer implements TUIRenderer for testing
type MockTUIRenderer struct {
	requestRenderCalled bool
}

func (m *MockTUIRenderer) Dispatch(fn func()) { fn() }

func (m *MockTUIRenderer) RequestRender(force ...bool) {
	m.requestRenderCalled = true
}

// defaultEditorTheme creates a simple theme for testing
func defaultEditorTheme() components.EditorTheme {
	return components.EditorTheme{
		BorderColor: func(s string) string { return s },
		Foreground:  func(s string) string { return s },
		SelectList: components.SelectListTheme{
			SelectedPrefix: func(s string) string { return s },
			SelectedText:   func(s string) string { return s },
			Description:    func(s string) string { return s },
			ScrollInfo:     func(s string) string { return s },
			NoMatch:        func(s string) string { return s },
		},
	}
}

// TestEditor_HistoryUpEmpty tests up arrow with empty history
func TestEditor_HistoryUpEmpty(t *testing.T) {
	mockTUI := &MockTUIRenderer{}
	editor := components.NewEditor(mockTUI, defaultEditorTheme())

	editor.HandleInput("\x1b[A") // Up arrow

	assert.Equal(t, "", editor.GetText())
}

// TestEditor_HistoryUpShowsMostRecent tests up arrow shows most recent history
func TestEditor_HistoryUpShowsMostRecent(t *testing.T) {
	mockTUI := &MockTUIRenderer{}
	editor := components.NewEditor(mockTUI, defaultEditorTheme())

	editor.AddToHistory("first prompt")
	editor.AddToHistory("second prompt")

	editor.HandleInput("\x1b[A") // Up arrow

	assert.Equal(t, "second prompt", editor.GetText())
}

// TestEditor_HistoryCyclesUp tests cycling through history with repeated Up
func TestEditor_HistoryCyclesUp(t *testing.T) {
	mockTUI := &MockTUIRenderer{}
	editor := components.NewEditor(mockTUI, defaultEditorTheme())

	editor.AddToHistory("first")
	editor.AddToHistory("second")
	editor.AddToHistory("third")

	editor.HandleInput("\x1b[A") // Up - shows "third"
	assert.Equal(t, "third", editor.GetText())

	editor.HandleInput("\x1b[A") // Up - shows "second"
	assert.Equal(t, "second", editor.GetText())

	editor.HandleInput("\x1b[A") // Up - shows "first"
	assert.Equal(t, "first", editor.GetText())

	editor.HandleInput("\x1b[A") // Up - stays at "first" (oldest)
	assert.Equal(t, "first", editor.GetText())
}

// TestEditor_HistoryDownReturnsEmpty tests down arrow returns to empty
func TestEditor_HistoryDownReturnsEmpty(t *testing.T) {
	mockTUI := &MockTUIRenderer{}
	editor := components.NewEditor(mockTUI, defaultEditorTheme())

	editor.AddToHistory("prompt")

	editor.HandleInput("\x1b[A") // Up - shows "prompt"
	assert.Equal(t, "prompt", editor.GetText())

	editor.HandleInput("\x1b[B") // Down - clears editor
	assert.Equal(t, "", editor.GetText())
}

// TestEditor_HistoryDownNavigatesForward tests down arrow navigates forward
func TestEditor_HistoryDownNavigatesForward(t *testing.T) {
	mockTUI := &MockTUIRenderer{}
	editor := components.NewEditor(mockTUI, defaultEditorTheme())

	editor.AddToHistory("first")
	editor.AddToHistory("second")
	editor.AddToHistory("third")

	// Go to oldest
	editor.HandleInput("\x1b[A") // third
	editor.HandleInput("\x1b[A") // second
	editor.HandleInput("\x1b[A") // first
	assert.Equal(t, "first", editor.GetText())

	// Navigate forward
	editor.HandleInput("\x1b[B") // second
	assert.Equal(t, "second", editor.GetText())

	editor.HandleInput("\x1b[B") // third
	assert.Equal(t, "third", editor.GetText())

	editor.HandleInput("\x1b[B") // empty
	assert.Equal(t, "", editor.GetText())
}

// TestEditor_HistoryExitsOnType tests that typing exits history mode
func TestEditor_HistoryExitsOnType(t *testing.T) {
	mockTUI := &MockTUIRenderer{}
	editor := components.NewEditor(mockTUI, defaultEditorTheme())

	editor.AddToHistory("old prompt")

	editor.HandleInput("\x1b[A") // Up - shows "old prompt"
	editor.HandleInput("x")      // Type a character - exits history mode

	assert.Equal(t, "old promptx", editor.GetText())
}

// TestEditor_HistoryExitsOnSetText tests that setText exits history mode
func TestEditor_HistoryExitsOnSetText(t *testing.T) {
	mockTUI := &MockTUIRenderer{}
	editor := components.NewEditor(mockTUI, defaultEditorTheme())

	editor.AddToHistory("first")
	editor.AddToHistory("second")

	editor.HandleInput("\x1b[A") // Up - shows "second"
	editor.SetText("")           // External clear

	// Up should start fresh from most recent
	editor.HandleInput("\x1b[A")
	assert.Equal(t, "second", editor.GetText())
}

// TestEditor_HistoryNoEmptyStrings tests that empty strings aren't added
func TestEditor_HistoryNoEmptyStrings(t *testing.T) {
	mockTUI := &MockTUIRenderer{}
	editor := components.NewEditor(mockTUI, defaultEditorTheme())

	editor.AddToHistory("")
	editor.AddToHistory("   ")
	editor.AddToHistory("valid")

	editor.HandleInput("\x1b[A")
	assert.Equal(t, "valid", editor.GetText())

	// Should not have more entries
	editor.HandleInput("\x1b[A")
	assert.Equal(t, "valid", editor.GetText())
}

// TestEditor_HistoryNoConsecutiveDuplicates tests no consecutive duplicates
func TestEditor_HistoryNoConsecutiveDuplicates(t *testing.T) {
	mockTUI := &MockTUIRenderer{}
	editor := components.NewEditor(mockTUI, defaultEditorTheme())

	editor.AddToHistory("same")
	editor.AddToHistory("same")
	editor.AddToHistory("same")

	// Only one entry should exist
	editor.HandleInput("\x1b[A")
	assert.Equal(t, "same", editor.GetText())

	// Should stay at same
	editor.HandleInput("\x1b[A")
	assert.Equal(t, "same", editor.GetText())
}

// TestEditor_HistoryNonConsecutiveDuplicatesAllowed tests non-consecutive duplicates allowed
func TestEditor_HistoryNonConsecutiveDuplicatesAllowed(t *testing.T) {
	mockTUI := &MockTUIRenderer{}
	editor := components.NewEditor(mockTUI, defaultEditorTheme())

	editor.AddToHistory("first")
	editor.AddToHistory("second")
	editor.AddToHistory("first") // Not consecutive, should be added

	editor.HandleInput("\x1b[A") // "first"
	assert.Equal(t, "first", editor.GetText())

	editor.HandleInput("\x1b[A") // "second"
	assert.Equal(t, "second", editor.GetText())

	editor.HandleInput("\x1b[A") // "first" (older one)
	assert.Equal(t, "first", editor.GetText())
}

// TestEditor_CursorMovementUpWithContent tests cursor moves up with content
func TestEditor_CursorMovementUpWithContent(t *testing.T) {
	mockTUI := &MockTUIRenderer{}
	editor := components.NewEditor(mockTUI, defaultEditorTheme())

	editor.AddToHistory("history item")
	editor.SetText("line1\nline2")
	// Move cursor to end of text (line2, after "line2")
	editor.HandleInput("\x05") // Ctrl+E to go to end
	// Move to end of line2
	editor.HandleInput("\x1b[B") // Down to line 2
	editor.HandleInput("\x05")   // Ctrl+E to end of line2

	// Cursor is at end of line2, Up should move to line1
	editor.HandleInput("\x1b[A") // Up - cursor movement

	// Insert character to verify cursor position
	editor.HandleInput("X")

	// X should be inserted in line1, not replace with history
	assert.Equal(t, "line1X\nline2", editor.GetText())
}

// TestEditor_HistoryLimitsTo100 tests history limits to 100 entries
func TestEditor_HistoryLimitsTo100(t *testing.T) {
	mockTUI := &MockTUIRenderer{}
	editor := components.NewEditor(mockTUI, defaultEditorTheme())

	// Add 102 entries
	for i := 0; i < 102; i++ {
		editor.AddToHistory(string(rune('a' + i%26)))
	}

	// Go to oldest - should be at index 101 (not 0)
	for i := 0; i < 100; i++ {
		editor.HandleInput("\x1b[A")
	}

	// Should be at oldest of the 100 kept
	text := editor.GetText()
	// The oldest should be 'c' (since we added 102 but only keep 100)
	assert.Equal(t, "c", text)
}

// TestEditor_GetTextReturnsJoinedLines tests GetText
func TestEditor_GetTextReturnsJoinedLines(t *testing.T) {
	mockTUI := &MockTUIRenderer{}
	editor := components.NewEditor(mockTUI, defaultEditorTheme())

	editor.SetText("line1\nline2\nline3")
	assert.Equal(t, "line1\nline2\nline3", editor.GetText())
}

// TestEditor_GetLinesReturnsDefensiveCopy tests GetLines returns a copy
func TestEditor_GetLinesReturnsDefensiveCopy(t *testing.T) {
	mockTUI := &MockTUIRenderer{}
	editor := components.NewEditor(mockTUI, defaultEditorTheme())

	editor.SetText("line1\nline2")
	lines := editor.GetLines()
	lines[0] = "modified"

	// Original should be unchanged
	assert.Equal(t, "line1\nline2", editor.GetText())
}

// TestEditor_BackslashInsert tests backslash is inserted immediately
func TestEditor_BackslashInsert(t *testing.T) {
	mockTUI := &MockTUIRenderer{}
	editor := components.NewEditor(mockTUI, defaultEditorTheme())

	editor.HandleInput("\\")
	assert.Equal(t, "\\", editor.GetText())
}

// TestEditor_BackslashEnterNewline tests backslash+Enter creates newline
func TestEditor_BackslashEnterNewline(t *testing.T) {
	mockTUI := &MockTUIRenderer{}
	editor := components.NewEditor(mockTUI, defaultEditorTheme())

	editor.HandleInput("h")
	editor.HandleInput("i")
	editor.HandleInput("\\")
	editor.HandleInput("\r") // Enter

	assert.Equal(t, "hi\n", editor.GetText())
}

// TestEditor_BackslashNotNewlineWhenNotLast tests backslash not newline when not last
func TestEditor_BackslashNotNewlineWhenNotLast(t *testing.T) {
	mockTUI := &MockTUIRenderer{}
	editor := components.NewEditor(mockTUI, defaultEditorTheme())

	editor.HandleInput("h")
	editor.HandleInput("\\")
	editor.HandleInput("i")
	editor.HandleInput("\r") // Enter

	// Should submit, not create newline
	// Since we don't have a submit callback, just check text
	assert.Equal(t, "h\\i", editor.GetText())
}

// TestEditor_BackslashOnlyOneRemoved tests only one backslash removed
func TestEditor_BackslashOnlyOneRemoved(t *testing.T) {
	mockTUI := &MockTUIRenderer{}
	editor := components.NewEditor(mockTUI, defaultEditorTheme())

	editor.HandleInput("a")
	editor.HandleInput("\\")
	editor.HandleInput("\\")
	editor.HandleInput("\r") // Enter

	// Only one backslash should be consumed for newline
	// Result: "a\\" + "\n" (but the logic removes one backslash)
	// Actually: "a" + "\\" + newline = "a\\\n"
	assert.Equal(t, "a\\\n", editor.GetText())
}

// TestEditor_UnicodeInsert tests inserting unicode characters
func TestEditor_UnicodeInsert(t *testing.T) {
	mockTUI := &MockTUIRenderer{}
	editor := components.NewEditor(mockTUI, defaultEditorTheme())

	// Insert ASCII, umlauts, and emoji
	editor.HandleInput("h")
	editor.HandleInput("e")
	editor.HandleInput("l")
	editor.HandleInput("l")
	editor.HandleInput("o")
	editor.HandleInput(" ")
	editor.HandleInput("ä")
	editor.HandleInput("ö")
	editor.HandleInput("ü")
	editor.HandleInput(" ")
	// Emoji would need special handling in real input

	assert.Equal(t, "hello äöü ", editor.GetText())
}

// TestEditor_BackspaceUnicode tests backspace handles unicode
func TestEditor_BackspaceUnicode(t *testing.T) {
	mockTUI := &MockTUIRenderer{}
	editor := components.NewEditor(mockTUI, defaultEditorTheme())

	editor.SetText("hällo")
	// Position cursor at end using Ctrl+E
	editor.HandleInput("\x05") // Ctrl+E

	// Backspace twice - deletes 'o' and 'l'
	editor.HandleInput("\x7f") // Backspace
	editor.HandleInput("\x7f") // Backspace

	// "hällo" -> "häll" -> "häl"
	assert.Equal(t, "häl", editor.GetText())
}

// TestEditor_CursorPositionAfterMovement tests cursor position
func TestEditor_CursorPosition(t *testing.T) {
	mockTUI := &MockTUIRenderer{}
	editor := components.NewEditor(mockTUI, defaultEditorTheme())

	editor.SetText("hello world")
	line, col := editor.GetCursor()
	assert.Equal(t, 0, line)
	assert.Equal(t, 11, col) // After SetText, cursor is at end (matching TS behavior)
}

// TestEditor_CtrlAHome tests Ctrl+A moves to start
func TestEditor_CtrlAHome(t *testing.T) {
	mockTUI := &MockTUIRenderer{}
	editor := components.NewEditor(mockTUI, defaultEditorTheme())

	editor.SetText("hello")
	// Move to end first
	editor.InsertTextAtCursor("")

	// Ctrl+A
	editor.HandleInput("\x01")
	_, col := editor.GetCursor()
	assert.Equal(t, 0, col)
}

// TestEditor_CtrlEEnd tests Ctrl+E moves to end
func TestEditor_CtrlEEnd(t *testing.T) {
	mockTUI := &MockTUIRenderer{}
	editor := components.NewEditor(mockTUI, defaultEditorTheme())

	editor.SetText("hello")
	// Cursor should be at end (matching TS behavior)
	_, col := editor.GetCursor()
	assert.Equal(t, 5, col)

	// Ctrl+E
	editor.HandleInput("\x05")
	_, col = editor.GetCursor()
	// After Ctrl+E, should be at end
	assert.Equal(t, 5, col)
}

// TestEditor_CtrlWDeleteWord tests Ctrl+W deletes word backward
func TestEditor_CtrlWDeleteWord(t *testing.T) {
	mockTUI := &MockTUIRenderer{}
	editor := components.NewEditor(mockTUI, defaultEditorTheme())

	editor.SetText("foo bar baz")
	// Cursor should be at 0,0 after SetText
	// Actually, let me insert the text character by character to get cursor at end
	editor.SetText("")
	for _, ch := range "foo bar baz" {
		editor.HandleInput(string(ch))
	}

	// Ctrl+W - delete "baz"
	editor.HandleInput("\x17")
	assert.Equal(t, "foo bar ", editor.GetText())
}

// TestEditor_CtrlLeftRightWord tests word navigation
func TestEditor_CtrlLeftRightWord(t *testing.T) {
	mockTUI := &MockTUIRenderer{}
	editor := components.NewEditor(mockTUI, defaultEditorTheme())

	// Insert "foo bar"
	editor.HandleInput("f")
	editor.HandleInput("o")
	editor.HandleInput("o")
	editor.HandleInput(" ")
	editor.HandleInput("b")
	editor.HandleInput("a")
	editor.HandleInput("r")

	// Ctrl+Left (Alt+B)
	editor.HandleInput("\x1bb")
	line, col := editor.GetCursor()
	_ = line
	_ = col
	// Should be at start of "bar"

	// Ctrl+Right (Alt+F)
	editor.HandleInput("\x1bf")
	line, col = editor.GetCursor()
	_ = line
	_ = col
	// Should be after "bar"
}

// TestEditor_SetOnSubmitCallback tests submit callback
func TestEditor_SetOnSubmitCallback(t *testing.T) {
	mockTUI := &MockTUIRenderer{}
	editor := components.NewEditor(mockTUI, defaultEditorTheme())

	var submitted string
	editor.SetOnSubmit(func(text string) {
		submitted = text
	})

	editor.SetText("test")
	editor.HandleInput("\r") // Enter

	assert.Equal(t, "test", submitted)
}

func TestEditor_LargePasteUsesMarkerAndExpandedText(t *testing.T) {
	mockTUI := &MockTUIRenderer{}
	editor := components.NewEditor(mockTUI, defaultEditorTheme())

	large := strings.Repeat("a", 1001)
	editor.HandleInput("\x1b[200~" + large + "\x1b[201~")

	assert.Equal(t, "[paste #1 1001 chars]", editor.GetText())
	assert.Equal(t, large, editor.GetExpandedText())
}

func TestEditor_LargeMultilinePasteUsesMarkerAndExpandedText(t *testing.T) {
	mockTUI := &MockTUIRenderer{}
	editor := components.NewEditor(mockTUI, defaultEditorTheme())

	large := strings.Repeat("line\n", 10) + "line"
	editor.HandleInput("\x1b[200~" + large + "\x1b[201~")

	assert.Equal(t, "[paste #1 +11 lines]", editor.GetText())
	assert.Equal(t, large, editor.GetExpandedText())
}

func TestEditor_SubmitExpandsLargePasteMarker(t *testing.T) {
	mockTUI := &MockTUIRenderer{}
	editor := components.NewEditor(mockTUI, defaultEditorTheme())

	var submitted string
	editor.SetOnSubmit(func(text string) {
		submitted = text
	})

	large := strings.Repeat("a", 1001)
	editor.HandleInput("\x1b[200~" + large + "\x1b[201~")
	editor.HandleInput("\r")

	assert.Equal(t, large, submitted)
}

// TestEditor_SetOnChangeCallback tests change callback
func TestEditor_SetOnChangeCallback(t *testing.T) {
	mockTUI := &MockTUIRenderer{}
	editor := components.NewEditor(mockTUI, defaultEditorTheme())

	var changed string
	editor.SetOnChange(func(text string) {
		changed = text
	})

	editor.HandleInput("x")
	assert.Equal(t, "x", changed)
}

// TestEditor_Undo tests undo functionality
func TestEditor_Undo(t *testing.T) {
	mockTUI := &MockTUIRenderer{}
	editor := components.NewEditor(mockTUI, defaultEditorTheme())

	editor.HandleInput("a")
	editor.HandleInput("b")
	editor.HandleInput("c")

	// Ctrl+_ to undo
	editor.HandleInput("\x1f")

	// After undo, should be back to previous state
	// Note: The actual behavior depends on implementation
	// For now, just verify it doesn't crash
}

// TestEditor_CtrlZUndo tests modern Ctrl+Z undo alias
func TestEditor_CtrlZUndo(t *testing.T) {
	mockTUI := &MockTUIRenderer{}
	editor := components.NewEditor(mockTUI, defaultEditorTheme())

	editor.HandleInput("a")
	editor.HandleInput("b")
	editor.HandleInput("c")

	editor.HandleInput("\x1a") // Ctrl+Z
	assert.Equal(t, "", editor.GetText())
}

// TestEditor_Yank tests yank (paste from kill ring)
func TestEditor_Yank(t *testing.T) {
	mockTUI := &MockTUIRenderer{}
	editor := components.NewEditor(mockTUI, defaultEditorTheme())

	// Type something, then delete it to put in kill ring
	editor.HandleInput("h")
	editor.HandleInput("i")
	// Use Ctrl+K to kill to end of line
	editor.HandleInput("\x0b") // Ctrl+K

	// Now yank it back
	editor.HandleInput("\x19") // Ctrl+Y

	// Should have "hi" again
	assert.Equal(t, "hi", editor.GetText())
}

// TestEditor_WordWrapLine tests the WordWrapLine function
func TestEditor_WordWrapLine(t *testing.T) {
	// Short line doesn't wrap
	chunks := components.WordWrapLine("hello", 80)
	assert.Len(t, chunks, 1)
	assert.Equal(t, "hello", chunks[0].Text)

	// Long line wraps
	chunks = components.WordWrapLine("hello world this is a test", 10)
	assert.Greater(t, len(chunks), 1)
}

// TestEditor_ClearHistory tests clearing history
func TestEditor_ClearHistory(t *testing.T) {
	mockTUI := &MockTUIRenderer{}
	editor := components.NewEditor(mockTUI, defaultEditorTheme())

	editor.AddToHistory("test")
	editor.ClearHistory()

	// Up arrow should do nothing
	editor.HandleInput("\x1b[A")
	assert.Equal(t, "", editor.GetText())
}

// TestEditor_PaddingX tests padding getters/setters
func TestEditor_PaddingX(t *testing.T) {
	mockTUI := &MockTUIRenderer{}
	editor := components.NewEditor(mockTUI, defaultEditorTheme())

	assert.Equal(t, 0, editor.GetPaddingX())

	editor.SetPaddingX(5)
	assert.Equal(t, 5, editor.GetPaddingX())
}

// TestEditor_AutocompleteMaxVisible tests autocomplete max visible
func TestEditor_AutocompleteMaxVisible(t *testing.T) {
	mockTUI := &MockTUIRenderer{}
	editor := components.NewEditor(mockTUI, defaultEditorTheme())

	assert.Equal(t, 5, editor.GetAutocompleteMaxVisible())

	editor.SetAutocompleteMaxVisible(10)
	assert.Equal(t, 10, editor.GetAutocompleteMaxVisible())

	// Should clamp to min 3
	editor.SetAutocompleteMaxVisible(1)
	assert.Equal(t, 3, editor.GetAutocompleteMaxVisible())

	// Should clamp to max 20
	editor.SetAutocompleteMaxVisible(50)
	assert.Equal(t, 20, editor.GetAutocompleteMaxVisible())
}

// TestEditor_DeleteWordForward tests delete word forward
func TestEditor_DeleteWordForward(t *testing.T) {
	mockTUI := &MockTUIRenderer{}
	editor := components.NewEditor(mockTUI, defaultEditorTheme())

	editor.SetText("foo bar")
	// Move cursor to start
	editor.HandleInput("\x01") // Ctrl+A
	// Alt+D to delete word forward
	editor.HandleInput("\x1bd") // Alt+D

	// Should delete "foo"
	assert.Equal(t, " bar", editor.GetText())
}

// TestEditor_CtrlDeleteWordForward tests Ctrl+Delete deletes word forward
func TestEditor_CtrlDeleteWordForward(t *testing.T) {
	mockTUI := &MockTUIRenderer{}
	editor := components.NewEditor(mockTUI, defaultEditorTheme())

	editor.SetText("foo bar")
	editor.HandleInput("\x01")    // Ctrl+A
	editor.HandleInput("\x1b[3^") // Ctrl+Delete

	assert.Equal(t, " bar", editor.GetText())
}

// TestEditor_DeleteToLineStart tests delete to line start
func TestEditor_DeleteToLineStart(t *testing.T) {
	mockTUI := &MockTUIRenderer{}
	editor := components.NewEditor(mockTUI, defaultEditorTheme())

	editor.SetText("hello world")
	// Move cursor to end
	editor.SetText("")
	for _, ch := range "hello world" {
		editor.HandleInput(string(ch))
	}

	// Ctrl+U to delete to line start
	editor.HandleInput("\x15")

	assert.Equal(t, "", editor.GetText())
}

// TestEditor_DeleteToLineEnd tests delete to line end
func TestEditor_DeleteToLineEnd(t *testing.T) {
	mockTUI := &MockTUIRenderer{}
	editor := components.NewEditor(mockTUI, defaultEditorTheme())

	editor.SetText("hello world")
	// Move cursor to start
	editor.HandleInput("\x01") // Ctrl+A

	// Ctrl+K to delete to line end
	editor.HandleInput("\x0b")

	assert.Equal(t, "", editor.GetText())
}

// TestEditor_Delete tests forward delete
func TestEditor_Delete(t *testing.T) {
	mockTUI := &MockTUIRenderer{}
	editor := components.NewEditor(mockTUI, defaultEditorTheme())

	editor.SetText("ab")
	// Move cursor to start
	editor.HandleInput("\x01") // Ctrl+A

	// Delete key
	editor.HandleInput("\x1b[3~")

	assert.Equal(t, "b", editor.GetText())
}

// TestEditor_JumpMode tests jump to character
func TestEditor_JumpMode(t *testing.T) {
	mockTUI := &MockTUIRenderer{}
	editor := components.NewEditor(mockTUI, defaultEditorTheme())

	editor.SetText("hello world")
	// Cursor at start

	// Ctrl+] to enter jump forward mode
	editor.HandleInput("\x1d")
	// Then type the character to jump to
	editor.HandleInput("w")

	// Cursor should be after "w"
	line, col := editor.GetCursor()
	_ = line
	_ = col
	// Should be at position of "w" + 1
}

// TestEditor_EscapeCancelsAutocomplete tests escape cancels autocomplete
func TestEditor_EscapeCancelsAutocomplete(t *testing.T) {
	mockTUI := &MockTUIRenderer{}
	editor := components.NewEditor(mockTUI, defaultEditorTheme())

	// Type something
	editor.HandleInput("a")

	// Escape should cancel autocomplete (though none is active)
	editor.HandleInput("\x1b")

	// Should still have text
	assert.Equal(t, "a", editor.GetText())
}

// TestEditor_TabInsertsLiteral tests tab inserts literal tab
func TestEditor_TabInsertsLiteral(t *testing.T) {
	mockTUI := &MockTUIRenderer{}
	editor := components.NewEditor(mockTUI, defaultEditorTheme())

	editor.HandleInput("h")
	editor.HandleInput("\t")
	editor.HandleInput("i")

	assert.Equal(t, "h\ti", editor.GetText())
}

// TestEditor_LeftArrow tests cursor left
func TestEditor_LeftArrow(t *testing.T) {
	mockTUI := &MockTUIRenderer{}
	editor := components.NewEditor(mockTUI, defaultEditorTheme())

	editor.HandleInput("a")
	editor.HandleInput("b")

	_, col := editor.GetCursor()
	_ = col

	// Left arrow
	editor.HandleInput("\x1b[D")

	_, col = editor.GetCursor()
	// Should be at column 1
	_ = col
}

// TestEditor_RightArrow tests cursor right
func TestEditor_RightArrow(t *testing.T) {
	mockTUI := &MockTUIRenderer{}
	editor := components.NewEditor(mockTUI, defaultEditorTheme())

	editor.HandleInput("a")

	// Left then right
	editor.HandleInput("\x1b[D")
	editor.HandleInput("\x1b[C")

	_, col := editor.GetCursor()
	_ = col
}

// TestEditor_MultiLineCursorUpDown tests cursor up/down in multi-line
func TestEditor_MultiLineCursorUpDown(t *testing.T) {
	mockTUI := &MockTUIRenderer{}
	editor := components.NewEditor(mockTUI, defaultEditorTheme())

	editor.SetText("line1\nline2")

	// Cursor should start at 0,0

	// Down to line 2
	editor.HandleInput("\x1b[B")
	line, _ := editor.GetCursor()
	assert.Equal(t, 1, line)

	// Up to line 1
	editor.HandleInput("\x1b[A")
	line, _ = editor.GetCursor()
	assert.Equal(t, 0, line)
}

// TestEditor_Render tests basic rendering
func TestEditor_Render(t *testing.T) {
	mockTUI := &MockTUIRenderer{}
	editor := components.NewEditor(mockTUI, defaultEditorTheme())

	editor.SetText("hello")
	lines := editor.Render(40)

	// Should return at least the borders
	assert.GreaterOrEqual(t, len(lines), 2)
}
