package components

import "github.com/francescoalemanno/raijin-mono/libtui/pkg/autocomplete"

// EditorComponent is the interface for custom editor components.
//
// This allows extensions to provide their own editor implementation
// (e.g., vim mode, emacs mode, custom keybindings) while maintaining
// compatibility with the core application.
type EditorComponent interface {
	// Render returns an array of strings, one per line.
	Render(width int) []string

	// HandleInput handles raw terminal input (key presses, paste sequences, etc.)
	HandleInput(data string)

	// Invalidate clears any cached render state.
	Invalidate()

	// GetText returns the current text content.
	GetText() string

	// SetText sets the text content.
	SetText(text string)

	// SetOnSubmit sets the callback for when user submits (e.g., Enter key).
	SetOnSubmit(fn func(string))

	// SetOnChange sets the callback for when text changes.
	SetOnChange(fn func(string))
}

// EditorWithHistory is optionally implemented by editors that support history.
type EditorWithHistory interface {
	AddToHistory(text string)
}

// EditorWithCursorInsert is optionally implemented by editors that support cursor insertion.
type EditorWithCursorInsert interface {
	InsertTextAtCursor(text string)
}

// EditorWithExpandedText is optionally implemented by editors that expand markers.
type EditorWithExpandedText interface {
	GetExpandedText() string
}

// EditorWithAutocomplete is optionally implemented by editors with autocomplete support.
type EditorWithAutocomplete interface {
	SetAutocompleteProvider(provider autocomplete.AutocompleteProvider)
}

// EditorWithBorderColor is optionally implemented by editors that support border color.
type EditorWithBorderColor interface {
	SetBorderColor(fn func(string) string)
}

// EditorWithPadding is optionally implemented by editors that support padding.
type EditorWithPadding interface {
	SetPaddingX(padding int)
}

// EditorWithAutocompleteMaxVisible is optionally implemented by editors that limit autocomplete items.
type EditorWithAutocompleteMaxVisible interface {
	SetAutocompleteMaxVisible(maxVisible int)
}
