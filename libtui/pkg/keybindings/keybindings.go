package keybindings

import (
	"github.com/francescoalemanno/raijin-mono/libtui/pkg/keys"
)

// KeyId represents a keyboard key identifier (e.g., "up", "ctrl+c", "enter").
type KeyId = string

// EditorAction represents all possible editor actions.
type EditorAction string

const (
	// Cursor movement
	ActionCursorUp        EditorAction = "cursorUp"
	ActionCursorDown      EditorAction = "cursorDown"
	ActionCursorLeft      EditorAction = "cursorLeft"
	ActionCursorRight     EditorAction = "cursorRight"
	ActionCursorWordLeft  EditorAction = "cursorWordLeft"
	ActionCursorWordRight EditorAction = "cursorWordRight"
	ActionCursorLineStart EditorAction = "cursorLineStart"
	ActionCursorLineEnd   EditorAction = "cursorLineEnd"
	ActionJumpForward     EditorAction = "jumpForward"
	ActionJumpBackward    EditorAction = "jumpBackward"
	ActionPageUp          EditorAction = "pageUp"
	ActionPageDown        EditorAction = "pageDown"
	// Deletion
	ActionDeleteCharBackward EditorAction = "deleteCharBackward"
	ActionDeleteCharForward  EditorAction = "deleteCharForward"
	ActionDeleteWordBackward EditorAction = "deleteWordBackward"
	ActionDeleteWordForward  EditorAction = "deleteWordForward"
	ActionDeleteToLineStart  EditorAction = "deleteToLineStart"
	ActionDeleteToLineEnd    EditorAction = "deleteToLineEnd"
	// Text input
	ActionNewLine EditorAction = "newLine"
	ActionSubmit  EditorAction = "submit"
	ActionTab     EditorAction = "tab"
	// Selection/autocomplete
	ActionSelectUp       EditorAction = "selectUp"
	ActionSelectDown     EditorAction = "selectDown"
	ActionSelectPageUp   EditorAction = "selectPageUp"
	ActionSelectPageDown EditorAction = "selectPageDown"
	ActionSelectConfirm  EditorAction = "selectConfirm"
	ActionSelectCancel   EditorAction = "selectCancel"
	// Clipboard
	ActionCopy EditorAction = "copy"
	// Kill ring
	ActionYank    EditorAction = "yank"
	ActionYankPop EditorAction = "yankPop"
	// Undo
	ActionUndo EditorAction = "undo"
	// Tool output
	ActionExpandTools EditorAction = "expandTools"
	// Session
	ActionToggleSessionPath        EditorAction = "toggleSessionPath"
	ActionToggleSessionSort        EditorAction = "toggleSessionSort"
	ActionRenameSession            EditorAction = "renameSession"
	ActionDeleteSession            EditorAction = "deleteSession"
	ActionDeleteSessionNoninvasive EditorAction = "deleteSessionNoninvasive"
)

// EditorKeybindingsConfig maps actions to key IDs (single or multiple).
type EditorKeybindingsConfig map[EditorAction][]KeyId

// DefaultEditorKeybindings is the default keybindings configuration.
var DefaultEditorKeybindings = EditorKeybindingsConfig{
	ActionCursorUp:        {"up"},
	ActionCursorDown:      {"down"},
	ActionCursorLeft:      {"left", "ctrl+b"},
	ActionCursorRight:     {"right", "ctrl+f"},
	ActionCursorWordLeft:  {"alt+left", "ctrl+left", "alt+b"},
	ActionCursorWordRight: {"alt+right", "ctrl+right", "alt+f"},
	ActionCursorLineStart: {"home", "ctrl+a"},
	ActionCursorLineEnd:   {"end", "ctrl+e"},
	ActionJumpForward:     {"ctrl+]"},
	ActionJumpBackward:    {"ctrl+alt+]"},
	ActionPageUp:          {"pageUp"},
	ActionPageDown:        {"pageDown"},
	// Deletion
	ActionDeleteCharBackward: {"backspace"},
	ActionDeleteCharForward:  {"delete", "ctrl+d"},
	ActionDeleteWordBackward: {"ctrl+w", "alt+backspace", "ctrl+backspace"},
	ActionDeleteWordForward:  {"alt+d", "alt+delete", "ctrl+delete"},
	ActionDeleteToLineStart:  {"ctrl+u"},
	ActionDeleteToLineEnd:    {"ctrl+k"},
	// Text input
	ActionNewLine: {"shift+enter", "ctrl+enter"},
	ActionSubmit:  {"enter"},
	ActionTab:     {"tab"},
	// Selection/autocomplete
	ActionSelectUp:       {"up"},
	ActionSelectDown:     {"down"},
	ActionSelectPageUp:   {"pageUp"},
	ActionSelectPageDown: {"pageDown"},
	ActionSelectConfirm:  {"enter"},
	ActionSelectCancel:   {"escape", "ctrl+c"},
	// Clipboard
	ActionCopy: {"ctrl+c"},
	// Kill ring
	ActionYank:    {"ctrl+y"},
	ActionYankPop: {"alt+y"},
	// Undo
	ActionUndo: {"ctrl+-", "ctrl+z"},
	// Tool output
	ActionExpandTools: {"ctrl+o"},
	// Session
	ActionToggleSessionPath:        {"ctrl+p"},
	ActionToggleSessionSort:        {"ctrl+s"},
	ActionRenameSession:            {"ctrl+r"},
	ActionDeleteSession:            {"ctrl+d"},
	ActionDeleteSessionNoninvasive: {"ctrl+backspace"},
}

// EditorKeybindingsManager manages keybindings for the editor.
type EditorKeybindingsManager struct {
	actionToKeys map[EditorAction][]KeyId
}

// NewEditorKeybindingsManager creates a new keybindings manager with optional config.
func NewEditorKeybindingsManager(config EditorKeybindingsConfig) *EditorKeybindingsManager {
	m := &EditorKeybindingsManager{
		actionToKeys: make(map[EditorAction][]KeyId),
	}
	m.buildMaps(config)
	return m
}

// buildMaps builds the action-to-keys mapping.
func (m *EditorKeybindingsManager) buildMaps(config EditorKeybindingsConfig) {
	// Clear existing maps
	for k := range m.actionToKeys {
		delete(m.actionToKeys, k)
	}

	// Start with defaults
	for action, keyArray := range DefaultEditorKeybindings {
		m.actionToKeys[action] = append([]KeyId{}, keyArray...)
	}

	// Override with user config
	for action, keyArray := range config {
		m.actionToKeys[action] = append([]KeyId{}, keyArray...)
	}
}

// Matches checks if input matches a specific action.
func (m *EditorKeybindingsManager) Matches(data string, action EditorAction) bool {
	keyArray, ok := m.actionToKeys[action]
	if !ok {
		return false
	}
	for _, key := range keyArray {
		if keys.MatchesKey(data, key) {
			return true
		}
	}
	return false
}

// GetKeys returns the keys bound to an action.
func (m *EditorKeybindingsManager) GetKeys(action EditorAction) []KeyId {
	keyArray, ok := m.actionToKeys[action]
	if !ok {
		return []KeyId{}
	}
	result := make([]KeyId, len(keyArray))
	copy(result, keyArray)
	return result
}

// SetConfig updates the configuration.
func (m *EditorKeybindingsManager) SetConfig(config EditorKeybindingsConfig) {
	m.buildMaps(config)
}

// Global instance
var globalEditorKeybindings *EditorKeybindingsManager

// GetEditorKeybindings returns the global keybindings manager.
func GetEditorKeybindings() *EditorKeybindingsManager {
	if globalEditorKeybindings == nil {
		globalEditorKeybindings = NewEditorKeybindingsManager(nil)
	}
	return globalEditorKeybindings
}

// SetEditorKeybindings sets the global keybindings manager.
func SetEditorKeybindings(manager *EditorKeybindingsManager) {
	globalEditorKeybindings = manager
}
