package test

import (
	"testing"

	"github.com/francescoalemanno/raijin-mono/libtui/pkg/components"
	"github.com/stretchr/testify/assert"
)

func TestInput_SubmitsValueIncludingBackslashOnEnter(t *testing.T) {
	input := components.NewInput()
	var submitted string

	input.SetOnSubmit(func(value string) {
		submitted = value
	})

	// Type hello, then backslash, then Enter
	input.HandleInput("h")
	input.HandleInput("e")
	input.HandleInput("l")
	input.HandleInput("l")
	input.HandleInput("o")
	input.HandleInput("\\")
	input.HandleInput("\r")

	// Input is single-line, no backslash+Enter workaround
	assert.Equal(t, "hello\\", submitted)
}

func TestInput_InsertsBackslashAsRegularCharacter(t *testing.T) {
	input := components.NewInput()

	input.HandleInput("\\")
	input.HandleInput("x")

	assert.Equal(t, "\\x", input.GetValue())
}

// Kill ring tests

func TestInput_CtrlW_SaveAndYank(t *testing.T) {
	input := components.NewInput()

	input.SetValue("foo bar baz")
	// Move cursor to end
	input.HandleInput("\x05") // Ctrl+E

	input.HandleInput("\x17") // Ctrl+W - deletes "baz"
	assert.Equal(t, "foo bar ", input.GetValue())

	// Move to beginning and yank
	input.HandleInput("\x01") // Ctrl+A
	input.HandleInput("\x19") // Ctrl+Y
	assert.Equal(t, "bazfoo bar ", input.GetValue())
}

func TestInput_CtrlU_SavesToKillRing(t *testing.T) {
	input := components.NewInput()

	input.SetValue("hello world")
	// Move cursor to after "hello "
	input.HandleInput("\x01") // Ctrl+A
	for range 6 {
		input.HandleInput("\x1b[C") // Right arrow
	}

	input.HandleInput("\x15") // Ctrl+U - deletes "hello "
	assert.Equal(t, "world", input.GetValue())

	input.HandleInput("\x19") // Ctrl+Y
	assert.Equal(t, "hello world", input.GetValue())
}

func TestInput_CtrlK_SavesToKillRing(t *testing.T) {
	input := components.NewInput()

	input.SetValue("hello world")
	input.HandleInput("\x01") // Ctrl+A
	input.HandleInput("\x0b") // Ctrl+K - deletes "hello world"

	assert.Equal(t, "", input.GetValue())

	input.HandleInput("\x19") // Ctrl+Y
	assert.Equal(t, "hello world", input.GetValue())
}

func TestInput_CtrlY_DoesNothingWhenKillRingIsEmpty(t *testing.T) {
	input := components.NewInput()

	input.SetValue("test")
	input.HandleInput("\x05") // Ctrl+E
	input.HandleInput("\x19") // Ctrl+Y
	assert.Equal(t, "test", input.GetValue())
}

func TestInput_AltY_CyclesThroughKillRingAfterCtrlY(t *testing.T) {
	input := components.NewInput()

	// Create kill ring with multiple entries
	input.SetValue("first")
	input.HandleInput("\x05") // Ctrl+E
	input.HandleInput("\x17") // Ctrl+W - deletes "first"
	input.SetValue("second")
	input.HandleInput("\x05") // Ctrl+E
	input.HandleInput("\x17") // Ctrl+W - deletes "second"
	input.SetValue("third")
	input.HandleInput("\x05") // Ctrl+E
	input.HandleInput("\x17") // Ctrl+W - deletes "third"

	assert.Equal(t, "", input.GetValue())

	input.HandleInput("\x19") // Ctrl+Y - yanks "third"
	assert.Equal(t, "third", input.GetValue())

	input.HandleInput("\x1by") // Alt+Y - cycles to "second"
	assert.Equal(t, "second", input.GetValue())

	input.HandleInput("\x1by") // Alt+Y - cycles to "first"
	assert.Equal(t, "first", input.GetValue())

	input.HandleInput("\x1by") // Alt+Y - cycles back to "third"
	assert.Equal(t, "third", input.GetValue())
}

func TestInput_AltY_DoesNothingIfNotPrecededByYank(t *testing.T) {
	input := components.NewInput()

	input.SetValue("test")
	input.HandleInput("\x05") // Ctrl+E
	input.HandleInput("\x17") // Ctrl+W - deletes "test"
	input.SetValue("other")
	input.HandleInput("\x05") // Ctrl+E

	// Type something to break the yank chain
	input.HandleInput("x")
	assert.Equal(t, "otherx", input.GetValue())

	input.HandleInput("\x1by") // Alt+Y - should do nothing
	assert.Equal(t, "otherx", input.GetValue())
}

func TestInput_AltY_DoesNothingIfKillRingHasOneEntry(t *testing.T) {
	input := components.NewInput()

	input.SetValue("only")
	input.HandleInput("\x05") // Ctrl+E
	input.HandleInput("\x17") // Ctrl+W - deletes "only"

	input.HandleInput("\x19") // Ctrl+Y - yanks "only"
	assert.Equal(t, "only", input.GetValue())

	input.HandleInput("\x1by") // Alt+Y - should do nothing
	assert.Equal(t, "only", input.GetValue())
}

func TestInput_ConsecutiveCtrlW_AccumulatesIntoOneKillRingEntry(t *testing.T) {
	input := components.NewInput()

	input.SetValue("one two three")
	input.HandleInput("\x05") // Ctrl+E
	input.HandleInput("\x17") // Ctrl+W - deletes "three"
	input.HandleInput("\x17") // Ctrl+W - deletes "two "
	input.HandleInput("\x17") // Ctrl+W - deletes "one "

	assert.Equal(t, "", input.GetValue())

	input.HandleInput("\x19") // Ctrl+Y
	assert.Equal(t, "one two three", input.GetValue())
}

func TestInput_NonDeleteActionsBreakKillAccumulation(t *testing.T) {
	input := components.NewInput()

	input.SetValue("foo bar baz")
	input.HandleInput("\x05") // Ctrl+E
	input.HandleInput("\x17") // Ctrl+W - deletes "baz"
	assert.Equal(t, "foo bar ", input.GetValue())

	input.HandleInput("x") // Typing breaks accumulation
	assert.Equal(t, "foo bar x", input.GetValue())

	input.HandleInput("\x17") // Ctrl+W - deletes "x" (separate entry)
	assert.Equal(t, "foo bar ", input.GetValue())

	input.HandleInput("\x19") // Ctrl+Y - most recent is "x"
	assert.Equal(t, "foo bar x", input.GetValue())

	input.HandleInput("\x1by") // Alt+Y - cycle to "baz"
	assert.Equal(t, "foo bar baz", input.GetValue())
}

func TestInput_NonYankActionsBreakAltYChain(t *testing.T) {
	input := components.NewInput()

	input.SetValue("first")
	input.HandleInput("\x05") // Ctrl+E
	input.HandleInput("\x17") // Ctrl+W
	input.SetValue("second")
	input.HandleInput("\x05") // Ctrl+E
	input.HandleInput("\x17") // Ctrl+W
	input.SetValue("")

	input.HandleInput("\x19") // Ctrl+Y - yanks "second"
	assert.Equal(t, "second", input.GetValue())

	input.HandleInput("x") // Breaks yank chain
	assert.Equal(t, "secondx", input.GetValue())

	input.HandleInput("\x1by") // Alt+Y - should do nothing
	assert.Equal(t, "secondx", input.GetValue())
}

func TestInput_KillRingRotationPersistsAfterCycling(t *testing.T) {
	input := components.NewInput()

	input.SetValue("first")
	input.HandleInput("\x05") // Ctrl+E
	input.HandleInput("\x17") // deletes "first"
	input.SetValue("second")
	input.HandleInput("\x05") // Ctrl+E
	input.HandleInput("\x17") // deletes "second"
	input.SetValue("third")
	input.HandleInput("\x05") // Ctrl+E
	input.HandleInput("\x17") // deletes "third"
	input.SetValue("")

	input.HandleInput("\x19")  // Ctrl+Y - yanks "third"
	input.HandleInput("\x1by") // Alt+Y - cycles to "second"
	assert.Equal(t, "second", input.GetValue())

	// Break chain and start fresh
	input.HandleInput("x")
	input.SetValue("")

	// New yank should get "second" (now at end after rotation)
	input.HandleInput("\x19") // Ctrl+Y
	assert.Equal(t, "second", input.GetValue())
}

func TestInput_BackwardDeletionsPrependForwardDeletionsAppendDuringAccumulation(t *testing.T) {
	input := components.NewInput()

	input.SetValue("prefix|suffix")
	// Position cursor at "|"
	input.HandleInput("\x01") // Ctrl+A
	for range 6 {
		input.HandleInput("\x1b[C") // Move right 6
	}

	input.HandleInput("\x0b") // Ctrl+K - deletes "|suffix" (forward)
	assert.Equal(t, "prefix", input.GetValue())

	input.HandleInput("\x19") // Ctrl+Y
	assert.Equal(t, "prefix|suffix", input.GetValue())
}

func TestInput_AltD_DeletesWordForwardAndSavesToKillRing(t *testing.T) {
	input := components.NewInput()

	input.SetValue("hello world test")
	input.HandleInput("\x01") // Ctrl+A

	input.HandleInput("\x1bd") // Alt+D - deletes "hello"
	assert.Equal(t, " world test", input.GetValue())

	input.HandleInput("\x1bd") // Alt+D - deletes " world"
	assert.Equal(t, " test", input.GetValue())

	// Yank should get accumulated text
	input.HandleInput("\x19") // Ctrl+Y
	assert.Equal(t, "hello world test", input.GetValue())
}

func TestInput_HandlesYankInMiddleOfText(t *testing.T) {
	input := components.NewInput()

	input.SetValue("word")
	input.HandleInput("\x05") // Ctrl+E
	input.HandleInput("\x17") // Ctrl+W - deletes "word"
	input.SetValue("hello world")
	// Move to middle (after "hello ")
	input.HandleInput("\x01") // Ctrl+A
	for range 6 {
		input.HandleInput("\x1b[C")
	}

	input.HandleInput("\x19") // Ctrl+Y
	assert.Equal(t, "hello wordworld", input.GetValue())
}

func TestInput_HandlesYankPopInMiddleOfText(t *testing.T) {
	input := components.NewInput()

	// Create two kill ring entries
	input.SetValue("FIRST")
	input.HandleInput("\x05") // Ctrl+E
	input.HandleInput("\x17") // Ctrl+W - deletes "FIRST"
	input.SetValue("SECOND")
	input.HandleInput("\x05") // Ctrl+E
	input.HandleInput("\x17") // Ctrl+W - deletes "SECOND"

	// Set up "hello world" and position cursor after "hello "
	input.SetValue("hello world")
	input.HandleInput("\x01") // Ctrl+A
	for range 6 {
		input.HandleInput("\x1b[C")
	}

	input.HandleInput("\x19") // Ctrl+Y - yanks "SECOND"
	assert.Equal(t, "hello SECONDworld", input.GetValue())

	input.HandleInput("\x1by") // Alt+Y - replaces with "FIRST"
	assert.Equal(t, "hello FIRSTworld", input.GetValue())
}

// Undo tests

func TestInput_UndoDoesNothingWhenStackIsEmpty(t *testing.T) {
	input := components.NewInput()

	input.HandleInput("\x1b[45;5u") // Ctrl+- (undo)
	assert.Equal(t, "", input.GetValue())
}

func TestInput_UndoCoalescesConsecutiveWords(t *testing.T) {
	input := components.NewInput()

	input.HandleInput("h")
	input.HandleInput("e")
	input.HandleInput("l")
	input.HandleInput("l")
	input.HandleInput("o")
	input.HandleInput(" ")
	input.HandleInput("w")
	input.HandleInput("o")
	input.HandleInput("r")
	input.HandleInput("l")
	input.HandleInput("d")
	assert.Equal(t, "hello world", input.GetValue())

	// Undo removes " world"
	input.HandleInput("\x1b[45;5u") // Ctrl+- (undo)
	assert.Equal(t, "hello", input.GetValue())

	// Undo removes "hello"
	input.HandleInput("\x1b[45;5u") // Ctrl+- (undo)
	assert.Equal(t, "", input.GetValue())
}

func TestInput_UndoRemovesSpacesOneAtATime(t *testing.T) {
	input := components.NewInput()

	input.HandleInput("h")
	input.HandleInput("e")
	input.HandleInput("l")
	input.HandleInput("l")
	input.HandleInput("o")
	input.HandleInput(" ")
	input.HandleInput(" ")
	assert.Equal(t, "hello  ", input.GetValue())

	input.HandleInput("\x1b[45;5u") // Ctrl+- (undo) - removes second " "
	assert.Equal(t, "hello ", input.GetValue())

	input.HandleInput("\x1b[45;5u") // Ctrl+- (undo) - removes first " "
	assert.Equal(t, "hello", input.GetValue())

	input.HandleInput("\x1b[45;5u") // Ctrl+- (undo) - removes "hello"
	assert.Equal(t, "", input.GetValue())
}

func TestInput_UndoesBackspace(t *testing.T) {
	input := components.NewInput()

	input.HandleInput("h")
	input.HandleInput("e")
	input.HandleInput("l")
	input.HandleInput("l")
	input.HandleInput("o")
	input.HandleInput("\x7f") // Backspace
	assert.Equal(t, "hell", input.GetValue())

	input.HandleInput("\x1b[45;5u") // Ctrl+- (undo)
	assert.Equal(t, "hello", input.GetValue())
}

func TestInput_UndoesForwardDelete(t *testing.T) {
	input := components.NewInput()

	input.HandleInput("h")
	input.HandleInput("e")
	input.HandleInput("l")
	input.HandleInput("l")
	input.HandleInput("o")
	input.HandleInput("\x01")    // Ctrl+A - go to start
	input.HandleInput("\x1b[C")  // Right arrow
	input.HandleInput("\x1b[3~") // Delete key
	assert.Equal(t, "hllo", input.GetValue())

	input.HandleInput("\x1b[45;5u") // Ctrl+- (undo)
	assert.Equal(t, "hello", input.GetValue())
}

func TestInput_UndoesCtrlW(t *testing.T) {
	input := components.NewInput()

	input.HandleInput("h")
	input.HandleInput("e")
	input.HandleInput("l")
	input.HandleInput("l")
	input.HandleInput("o")
	input.HandleInput(" ")
	input.HandleInput("w")
	input.HandleInput("o")
	input.HandleInput("r")
	input.HandleInput("l")
	input.HandleInput("d")
	assert.Equal(t, "hello world", input.GetValue())

	input.HandleInput("\x17") // Ctrl+W
	assert.Equal(t, "hello ", input.GetValue())

	input.HandleInput("\x1b[45;5u") // Ctrl+- (undo)
	assert.Equal(t, "hello world", input.GetValue())
}

func TestInput_UndoesCtrlK(t *testing.T) {
	input := components.NewInput()

	input.HandleInput("h")
	input.HandleInput("e")
	input.HandleInput("l")
	input.HandleInput("l")
	input.HandleInput("o")
	input.HandleInput(" ")
	input.HandleInput("w")
	input.HandleInput("o")
	input.HandleInput("r")
	input.HandleInput("l")
	input.HandleInput("d")
	input.HandleInput("\x01") // Ctrl+A
	for range 6 {
		input.HandleInput("\x1b[C")
	}

	input.HandleInput("\x0b") // Ctrl+K
	assert.Equal(t, "hello ", input.GetValue())

	input.HandleInput("\x1b[45;5u") // Ctrl+- (undo)
	assert.Equal(t, "hello world", input.GetValue())
}

func TestInput_UndoesCtrlU(t *testing.T) {
	input := components.NewInput()

	input.HandleInput("h")
	input.HandleInput("e")
	input.HandleInput("l")
	input.HandleInput("l")
	input.HandleInput("o")
	input.HandleInput(" ")
	input.HandleInput("w")
	input.HandleInput("o")
	input.HandleInput("r")
	input.HandleInput("l")
	input.HandleInput("d")
	input.HandleInput("\x01") // Ctrl+A
	for range 6 {
		input.HandleInput("\x1b[C")
	}

	input.HandleInput("\x15") // Ctrl+U
	assert.Equal(t, "world", input.GetValue())

	input.HandleInput("\x1b[45;5u") // Ctrl+- (undo)
	assert.Equal(t, "hello world", input.GetValue())
}

func TestInput_UndoesYank(t *testing.T) {
	input := components.NewInput()

	input.HandleInput("h")
	input.HandleInput("e")
	input.HandleInput("l")
	input.HandleInput("l")
	input.HandleInput("o")
	input.HandleInput(" ")
	input.HandleInput("\x17") // Ctrl+W - delete "hello "
	input.HandleInput("\x19") // Ctrl+Y - yank
	assert.Equal(t, "hello ", input.GetValue())

	input.HandleInput("\x1b[45;5u") // Ctrl+- (undo)
	assert.Equal(t, "", input.GetValue())
}

func TestInput_UndoesPasteAtomically(t *testing.T) {
	input := components.NewInput()

	input.SetValue("hello world")
	input.HandleInput("\x01") // Ctrl+A
	for range 5 {
		input.HandleInput("\x1b[C")
	}

	// Simulate bracketed paste
	input.HandleInput("\x1b[200~beep boop\x1b[201~")
	assert.Equal(t, "hellobeep boop world", input.GetValue())

	// Single undo should restore entire pre-paste state
	input.HandleInput("\x1b[45;5u") // Ctrl+- (undo)
	assert.Equal(t, "hello world", input.GetValue())
}

func TestInput_UndoesAltD(t *testing.T) {
	input := components.NewInput()

	input.SetValue("hello world")
	input.HandleInput("\x01") // Ctrl+A

	input.HandleInput("\x1bd") // Alt+D - deletes "hello"
	assert.Equal(t, " world", input.GetValue())

	input.HandleInput("\x1b[45;5u") // Ctrl+- (undo)
	assert.Equal(t, "hello world", input.GetValue())
}

func TestInput_CursorMovementStartsNewUndoUnit(t *testing.T) {
	input := components.NewInput()

	input.HandleInput("a")
	input.HandleInput("b")
	input.HandleInput("c")
	input.HandleInput("\x01") // Ctrl+A - movement breaks coalescing
	input.HandleInput("\x05") // Ctrl+E
	input.HandleInput("d")
	input.HandleInput("e")
	assert.Equal(t, "abcde", input.GetValue())

	// Undo removes "de" (typed after movement)
	input.HandleInput("\x1b[45;5u") // Ctrl+- (undo)
	assert.Equal(t, "abc", input.GetValue())

	// Undo removes "abc"
	input.HandleInput("\x1b[45;5u") // Ctrl+- (undo)
	assert.Equal(t, "", input.GetValue())
}
