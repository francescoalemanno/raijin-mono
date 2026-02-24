// Package keys provides keyboard input handling for terminal applications.
//
// Supports both legacy terminal sequences and Kitty keyboard protocol.
// See: https://sw.kovidgoyal.net/kitty/keyboard-protocol/
//
// API:
// - MatchesKey(data, keyId) - Check if input matches a key identifier
// - ParseKey(data) - Parse input and return the key identifier
// - SetKittyProtocolActive(active) - Set global Kitty protocol state
// - IsKittyProtocolActive() - Query global Kitty protocol state
package keys

import (
	"strconv"
	"strings"
)

// =============================================================================
// Global Kitty Protocol State
// =============================================================================

var _kittyProtocolActive = false

// SetKittyProtocolActive sets the global Kitty keyboard protocol state.
// Called by ProcessTerminal after detecting protocol support.
func SetKittyProtocolActive(active bool) {
	_kittyProtocolActive = active
}

// IsKittyProtocolActive queries whether Kitty keyboard protocol is currently active.
func IsKittyProtocolActive() bool {
	return _kittyProtocolActive
}

// =============================================================================
// Constants
// =============================================================================

var symbolKeys = map[rune]bool{
	'`': true, '-': true, '=': true, '[': true, ']': true,
	'\\': true, ';': true, '\'': true, ',': true, '.': true,
	'/': true, '!': true, '@': true, '#': true, '$': true,
	'%': true, '^': true, '&': true, '*': true, '(': true,
	')': true, '_': true, '+': true, '|': true, '~': true,
	'{': true, '}': true, ':': true, '<': true, '>': true,
	'?': true,
}

const (
	modShift = 1
	modAlt   = 2
	modCtrl  = 4
)

const lockMask = 64 + 128 // Caps Lock + Num Lock

const (
	cpEscape  = 27
	cpTab     = 9
	cpEnter   = 13
	cpSpace   = 32
	cpBacksp  = 127
	cpKpEnter = 57414 // Numpad Enter (Kitty protocol)
)

const (
	cpUp    = -1
	cpDown  = -2
	cpRight = -3
	cpLeft  = -4
)

const (
	cpDelete = -10
	cpInsert = -11
	cpPageUp = -12
	cpPgDown = -13
	cpHome   = -14
	cpEnd    = -15
)

var legacyKeySequences = map[string][]string{
	"up":       {"\x1b[A", "\x1bOA"},
	"down":     {"\x1b[B", "\x1bOB"},
	"right":    {"\x1b[C", "\x1bOC"},
	"left":     {"\x1b[D", "\x1bOD"},
	"home":     {"\x1b[H", "\x1bOH", "\x1b[1~", "\x1b[7~"},
	"end":      {"\x1b[F", "\x1bOF", "\x1b[4~", "\x1b[8~"},
	"insert":   {"\x1b[2~"},
	"delete":   {"\x1b[3~"},
	"pageUp":   {"\x1b[5~", "\x1b[[5~"},
	"pageDown": {"\x1b[6~", "\x1b[[6~"},
	"clear":    {"\x1b[E", "\x1bOE"},
	"f1":       {"\x1bOP", "\x1b[11~", "\x1b[[A"},
	"f2":       {"\x1bOQ", "\x1b[12~", "\x1b[[B"},
	"f3":       {"\x1bOR", "\x1b[13~", "\x1b[[C"},
	"f4":       {"\x1bOS", "\x1b[14~", "\x1b[[D"},
	"f5":       {"\x1b[15~", "\x1b[[E"},
	"f6":       {"\x1b[17~"},
	"f7":       {"\x1b[18~"},
	"f8":       {"\x1b[19~"},
	"f9":       {"\x1b[20~"},
	"f10":      {"\x1b[21~"},
	"f11":      {"\x1b[23~"},
	"f12":      {"\x1b[24~"},
}

var legacyShiftSequences = map[string][]string{
	"up":       {"\x1b[a"},
	"down":     {"\x1b[b"},
	"right":    {"\x1b[c"},
	"left":     {"\x1b[d"},
	"clear":    {"\x1b[e"},
	"insert":   {"\x1b[2$"},
	"delete":   {"\x1b[3$"},
	"pageUp":   {"\x1b[5$"},
	"pageDown": {"\x1b[6$"},
	"home":     {"\x1b[7$"},
	"end":      {"\x1b[8$"},
}

var legacyCtrlSequences = map[string][]string{
	"up":       {"\x1bOa"},
	"down":     {"\x1bOb"},
	"right":    {"\x1bOc"},
	"left":     {"\x1bOd"},
	"clear":    {"\x1bOe"},
	"insert":   {"\x1b[2^"},
	"delete":   {"\x1b[3^"},
	"pageUp":   {"\x1b[5^"},
	"pageDown": {"\x1b[6^"},
	"home":     {"\x1b[7^"},
	"end":      {"\x1b[8^"},
}

var legacySequenceKeyIDs = map[string]string{
	"\x1bOA":   "up",
	"\x1bOB":   "down",
	"\x1bOC":   "right",
	"\x1bOD":   "left",
	"\x1bOH":   "home",
	"\x1bOF":   "end",
	"\x1b[E":   "clear",
	"\x1bOE":   "clear",
	"\x1bOe":   "ctrl+clear",
	"\x1b[e":   "shift+clear",
	"\x1b[2~":  "insert",
	"\x1b[2$":  "shift+insert",
	"\x1b[2^":  "ctrl+insert",
	"\x1b[3$":  "shift+delete",
	"\x1b[3^":  "ctrl+delete",
	"\x1b[[5~": "pageUp",
	"\x1b[[6~": "pageDown",
	"\x1b[a":   "shift+up",
	"\x1b[b":   "shift+down",
	"\x1b[c":   "shift+right",
	"\x1b[d":   "shift+left",
	"\x1bOa":   "ctrl+up",
	"\x1bOb":   "ctrl+down",
	"\x1bOc":   "ctrl+right",
	"\x1bOd":   "ctrl+left",
	"\x1b[5$":  "shift+pageUp",
	"\x1b[6$":  "shift+pageDown",
	"\x1b[7$":  "shift+home",
	"\x1b[8$":  "shift+end",
	"\x1b[5^":  "ctrl+pageUp",
	"\x1b[6^":  "ctrl+pageDown",
	"\x1b[7^":  "ctrl+home",
	"\x1b[8^":  "ctrl+end",
	"\x1bOP":   "f1",
	"\x1bOQ":   "f2",
	"\x1bOR":   "f3",
	"\x1bOS":   "f4",
	"\x1b[11~": "f1",
	"\x1b[12~": "f2",
	"\x1b[13~": "f3",
	"\x1b[14~": "f4",
	"\x1b[[A":  "f1",
	"\x1b[[B":  "f2",
	"\x1b[[C":  "f3",
	"\x1b[[D":  "f4",
	"\x1b[[E":  "f5",
	"\x1b[15~": "f5",
	"\x1b[17~": "f6",
	"\x1b[18~": "f7",
	"\x1b[19~": "f8",
	"\x1b[20~": "f9",
	"\x1b[21~": "f10",
	"\x1b[23~": "f11",
	"\x1b[24~": "f12",
	"\x1bb":    "alt+left",
	"\x1bf":    "alt+right",
	"\x1bp":    "alt+up",
	"\x1bn":    "alt+down",
}

// =============================================================================
// Kitty Protocol Parsing
// =============================================================================

// parsedKittySequence represents a parsed Kitty keyboard protocol sequence
type parsedKittySequence struct {
	codepoint     int
	shiftedKey    *int
	baseLayoutKey *int
	modifier      int
	eventType     string // "press", "repeat", "release"
}

// parseEventType parses the event type string from Kitty protocol
func parseEventType(eventTypeStr string) string {
	if eventTypeStr == "" {
		return "press"
	}
	eventType := parseIntSafe(eventTypeStr, 0)
	if eventType == 2 {
		return "repeat"
	}
	if eventType == 3 {
		return "release"
	}
	return "press"
}

// parseKittySequence parses a Kitty keyboard protocol CSI sequence
func parseKittySequence(data string) *parsedKittySequence {
	// CSI u format with alternate keys (flag 4):
	// \x1b[<codepoint>u
	// \x1b[<codepoint>;<mod>u
	// \x1b[<codepoint>;<mod>:<event>u
	// \x1b[<codepoint>:<shifted>;<mod>u
	// \x1b[<codepoint>:<shifted>:<base>;<mod>u
	// \x1b[<codepoint>::<base>;<mod>u (no shifted key, only base)
	//
	// With flag 2, event type is appended after modifier colon: 1=press, 2=repeat, 3=release
	// With flag 4, alternate keys are appended after codepoint with colons

	// Match CSI u sequence
	if csiUMatch := matchKittyCSIu(data); csiUMatch != nil {
		return csiUMatch
	}

	// Arrow keys with modifier: \x1b[1;<mod>A/B/C/D or \x1b[1;<mod>:<event>A/B/C/D
	if arrowMatch := matchKittyArrow(data); arrowMatch != nil {
		return arrowMatch
	}

	// Functional keys: \x1b[<num>~ or \x1b[<num>;<mod>~ or \x1b[<num>;<mod>:<event>~
	if funcMatch := matchKittyFunc(data); funcMatch != nil {
		return funcMatch
	}

	// Home/End with modifier: \x1b[1;<mod>H/F or \x1b[1;<mod>:<event>H/F
	if homeEndMatch := matchKittyHomeEnd(data); homeEndMatch != nil {
		return homeEndMatch
	}

	return nil
}

// matchKittyCSIu matches CSI u format sequences
func matchKittyCSIu(data string) *parsedKittySequence {
	// Check format: \x1b[<codepoint>u or \x1b[<codepoint>;<mod>u or \x1b[<codepoint>:<shifted>:<base>;<mod>u, etc.
	if !strings.HasPrefix(data, "\x1b[") || !strings.HasSuffix(data, "u") {
		return nil
	}

	// Expected format: ESC [ <codepoint> [ : <shifted> ] [ : <base> ] ; <mod> [ : <event> ] u
	remaining := data[2:]                    // Skip ESC [
	remaining = remaining[:len(remaining)-1] // Remove trailing 'u'

	// Split by semicolon to get modifier part
	semicolons := strings.Split(remaining, ";")
	if len(semicolons) < 1 || len(semicolons) > 2 {
		return nil
	}

	var codepointPart, modifierPart string
	if len(semicolons) == 1 {
		codepointPart = semicolons[0]
		modifierPart = "1" // Default
	} else if len(semicolons) == 2 {
		codepointPart = semicolons[0]
		modifierPart = semicolons[1]
	}

	// Parse codepoint and its optional shifted/base parts
	colons := strings.Split(codepointPart, ":")
	if len(colons) < 1 || len(colons) > 3 {
		return nil
	}

	codepoint := parseIntSafe(colons[0], 0)
	var shiftedKey *int
	var baseLayoutKey *int

	if len(colons) > 1 {
		if colons[1] != "" {
			// Shifted key is present: codepoint:shifted or codepoint:shifted:base
			val := parseIntSafe(colons[1], 0)
			shiftedKey = &val
		}
	}
	if len(colons) > 2 && colons[2] != "" {
		// Base layout key is present: codepoint::base or codepoint:shifted:base
		val := parseIntSafe(colons[2], 0)
		baseLayoutKey = &val
	}

	// Parse modifier and optional event type
	modParts := strings.Split(modifierPart, ":")
	modValue := parseIntSafe(modParts[0], 1)
	eventType := parseEventType("")
	if len(modParts) > 1 {
		eventType = parseEventType(modParts[1])
	}

	return &parsedKittySequence{
		codepoint:     codepoint,
		shiftedKey:    shiftedKey,
		baseLayoutKey: baseLayoutKey,
		modifier:      modValue - 1,
		eventType:     eventType,
	}
}

// matchKittyArrow matches arrow key sequences with modifiers
func matchKittyArrow(data string) *parsedKittySequence {
	// Pattern: ^\x1b\[1;(\d+)(?::(\d+))?([ABCD])$
	if !strings.HasPrefix(data, "\x1b[1;") || len(data) < 5 {
		return nil
	}

	remaining := data[4:] // Skip ESC [1;

	// Find the last character (A, B, C, or D)
	lastChar := rune(data[len(data)-1])
	if lastChar != 'A' && lastChar != 'B' && lastChar != 'C' && lastChar != 'D' {
		return nil
	}

	// Parse modifier and optional event
	inner := remaining[:len(remaining)-1] // Remove the arrow character
	modParts := strings.Split(inner, ":")

	var eventType string
	var modValue int

	if len(modParts) == 1 {
		modValue = parseIntSafe(modParts[0], 0)
		eventType = parseEventType("")
	} else if len(modParts) == 2 {
		modValue = parseIntSafe(modParts[0], 0)
		eventType = parseEventType(modParts[1])
	} else {
		return nil
	}

	arrowCodes := map[rune]int{
		'A': cpUp,
		'B': cpDown,
		'C': cpRight,
		'D': cpLeft,
	}

	return &parsedKittySequence{
		codepoint: arrowCodes[lastChar],
		modifier:  modValue - 1,
		eventType: eventType,
	}
}

// matchKittyFunc matches functional key sequences
func matchKittyFunc(data string) *parsedKittySequence {
	// Pattern: ^\x1b\[(\d+)(?:;(\d+))?(?::(\d+))?~$
	if !strings.HasPrefix(data, "\x1b[") || !strings.HasSuffix(data, "~") {
		return nil
	}

	remaining := data[2 : len(data)-1] // Skip ESC [ and trailing ~

	// Split by semicolon
	parts := strings.Split(remaining, ";")
	if len(parts) > 2 {
		return nil
	}

	keyNum := parseIntSafe(parts[0], 0)
	modValue := 1
	eventType := ""

	if len(parts) > 1 {
		modParts := strings.Split(parts[1], ":")
		modValue = parseIntSafe(modParts[0], 1)
		if len(modParts) > 1 {
			eventType = modParts[1]
		}
	}

	funcCodes := map[int]int{
		2: cpInsert,
		3: cpDelete,
		5: cpPageUp,
		6: cpPgDown,
		7: cpHome,
		8: cpEnd,
	}

	codepoint, ok := funcCodes[keyNum]
	if !ok {
		return nil
	}

	return &parsedKittySequence{
		codepoint: codepoint,
		modifier:  modValue - 1,
		eventType: parseEventType(eventType),
	}
}

// matchKittyHomeEnd matches home/end key sequences with modifiers
func matchKittyHomeEnd(data string) *parsedKittySequence {
	// Pattern: ^\x1b\[1;(\d+)(?::(\d+))?([HF])$
	if !strings.HasPrefix(data, "\x1b[1;") || len(data) < 5 {
		return nil
	}

	remaining := data[4:] // Skip ESC [1;

	lastChar := rune(data[len(data)-1])
	if lastChar != 'H' && lastChar != 'F' {
		return nil
	}

	// Parse modifier and optional event
	inner := remaining[:len(remaining)-1]
	modParts := strings.Split(inner, ":")

	var eventType string
	var modValue int

	if len(modParts) == 1 {
		modValue = parseIntSafe(modParts[0], 0)
		eventType = parseEventType("")
	} else if len(modParts) == 2 {
		modValue = parseIntSafe(modParts[0], 0)
		eventType = parseEventType(modParts[1])
	} else {
		return nil
	}

	var codepoint int
	if lastChar == 'H' {
		codepoint = cpHome
	} else {
		codepoint = cpEnd
	}

	return &parsedKittySequence{
		codepoint: codepoint,
		modifier:  modValue - 1,
		eventType: eventType,
	}
}

// matchesKittySequence checks if data matches expected codepoint and modifier
func matchesKittySequence(data string, expectedCodepoint int, expectedModifier int) bool {
	parsed := parseKittySequence(data)
	if parsed == nil {
		return false
	}

	actualMod := parsed.modifier & ^lockMask
	expectedMod := expectedModifier & ^lockMask

	// Check if modifiers match
	if actualMod != expectedMod {
		return false
	}

	// Primary match: codepoint matches directly
	if parsed.codepoint == expectedCodepoint {
		return true
	}

	// Alternate match: use base layout key for non-Latin keyboard layouts.
	// Only fall back to base layout key when codepoint is NOT already a
	// recognized Latin letter (a-z) or symbol.
	if parsed.baseLayoutKey != nil && *parsed.baseLayoutKey == expectedCodepoint {
		cp := parsed.codepoint
		isLatinLetter := cp >= 97 && cp <= 122 // a-z
		_, isKnownSymbol := symbolKeys[rune(cp)]
		if !isLatinLetter && !isKnownSymbol {
			return true
		}
	}

	return false
}

// matchesModifyOtherKeys matches xterm modifyOtherKeys format
func matchesModifyOtherKeys(data string, expectedKeycode int, expectedModifier int) bool {
	// Pattern: ^\x1b\[27;(\d+);(\d+)~$
	if !strings.HasPrefix(data, "\x1b[27;") || !strings.HasSuffix(data, "~") {
		return false
	}

	remaining := data[6 : len(data)-1] // Skip ESC [27; and trailing ~
	parts := strings.Split(remaining, ";")
	if len(parts) != 2 {
		return false
	}

	modValue := parseIntSafe(parts[0], 0)
	keycode := parseIntSafe(parts[1], 0)

	actualMod := modValue - 1
	return keycode == expectedKeycode && actualMod == expectedModifier
}

// =============================================================================
// Helper Functions
// =============================================================================

// rawCtrlChar gets the control character for a key
// Uses universal formula: code & 0x1f (mask to lower 5 bits)
func rawCtrlChar(key string) string {
	if len(key) == 0 {
		return ""
	}

	char := strings.ToLower(key)
	if len(char) > 1 {
		return ""
	}

	r := rune(char[0])
	if (r >= 'a' && r <= 'z') || r == '[' || r == '\\' || r == ']' || r == '_' {
		return string(r & 0x1f)
	}

	// Handle - as _ (same physical key on US keyboards)
	if r == '-' {
		return "\x1f" // Same as Ctrl+_
	}

	return ""
}

// parseKeyId parses a key identifier string
func parseKeyId(keyId string) (key string, ctrl, shift, alt bool) {
	if keyId == "" {
		return
	}

	parts := strings.Split(strings.ToLower(keyId), "+")
	key = parts[len(parts)-1]

	for i := 0; i < len(parts)-1; i++ {
		switch parts[i] {
		case "ctrl":
			ctrl = true
		case "shift":
			shift = true
		case "alt":
			alt = true
		}
	}

	return
}

// parseIntSafe parses an int from a string, returning default on error
func parseIntSafe(s string, defaultVal int) int {
	val, err := strconv.Atoi(s)
	if err != nil {
		return defaultVal
	}
	return val
}

// matchesLegacySequence checks if data matches any of the given sequences
func matchesLegacySequence(data string, sequences []string) bool {
	for _, seq := range sequences {
		if data == seq {
			return true
		}
	}
	return false
}

// =============================================================================
// Public API: MatchesKey
// =============================================================================

// MatchesKey checks if input data matches a key identifier string.
//
// Supported key identifiers:
// - Single keys: "escape", "tab", "enter", "backspace", "delete", "home", "end", "space"
// - Arrow keys: "up", "down", "left", "right"
// - Ctrl combinations: "ctrl+c", "ctrl+z", etc.
// - Shift combinations: "shift+tab", "shift+enter"
// - Alt combinations: "alt+enter", "alt+backspace"
// - Combined modifiers: "shift+ctrl+p", "ctrl+alt+x"
func MatchesKey(data, keyId string) bool {
	key, ctrl, shift, alt := parseKeyId(keyId)

	modifier := 0
	if shift {
		modifier |= modShift
	}
	if alt {
		modifier |= modAlt
	}
	if ctrl {
		modifier |= modCtrl
	}

	switch key {
	case "escape", "esc":
		if modifier != 0 {
			return false
		}
		return data == "\x1b" || matchesKittySequence(data, cpEscape, 0)

	case "space":
		if !_kittyProtocolActive {
			if ctrl && !alt && !shift && data == "\x00" {
				return true
			}
			if alt && !ctrl && !shift && data == "\x1b " {
				return true
			}
		}
		if modifier == 0 {
			return data == " " || matchesKittySequence(data, cpSpace, 0)
		}
		return matchesKittySequence(data, cpSpace, modifier)

	case "tab":
		if shift && !ctrl && !alt {
			return data == "\x1b[Z" || matchesKittySequence(data, cpTab, modShift)
		}
		if modifier == 0 {
			return data == "\t" || matchesKittySequence(data, cpTab, 0)
		}
		return matchesKittySequence(data, cpTab, modifier)

	case "enter", "return":
		if shift && !ctrl && !alt {
			if matchesKittySequence(data, cpEnter, modShift) ||
				matchesKittySequence(data, cpKpEnter, modShift) ||
				matchesModifyOtherKeys(data, cpEnter, modShift) {
				return true
			}
			if _kittyProtocolActive {
				return data == "\x1b\r" || data == "\n"
			}
			return false
		}
		if alt && !ctrl && !shift {
			if matchesKittySequence(data, cpEnter, modAlt) ||
				matchesKittySequence(data, cpKpEnter, modAlt) ||
				matchesModifyOtherKeys(data, cpEnter, modAlt) {
				return true
			}
			if !_kittyProtocolActive {
				return data == "\x1b\r"
			}
			return false
		}
		if modifier == 0 {
			return data == "\r" ||
				(!_kittyProtocolActive && data == "\n") ||
				data == "\x1bOM" ||
				matchesKittySequence(data, cpEnter, 0) ||
				matchesKittySequence(data, cpKpEnter, 0)
		}
		return matchesKittySequence(data, cpEnter, modifier) ||
			matchesKittySequence(data, cpKpEnter, modifier)

	case "backspace":
		if alt && !ctrl && !shift {
			if data == "\x1b\x7f" || data == "\x1b\b" {
				return true
			}
			return matchesKittySequence(data, cpBacksp, modAlt)
		}
		if modifier == 0 {
			return data == "\x7f" || data == "\x08" || matchesKittySequence(data, cpBacksp, 0)
		}
		return matchesKittySequence(data, cpBacksp, modifier)

	case "insert":
		if modifier == 0 {
			return matchesLegacySequence(data, legacyKeySequences["insert"]) ||
				matchesKittySequence(data, cpInsert, 0)
		}
		if matchesLegacySequence(data, legacyShiftSequences["insert"]) ||
			matchesLegacySequence(data, legacyCtrlSequences["insert"]) {
			return true
		}
		return matchesKittySequence(data, cpInsert, modifier)

	case "delete":
		if modifier == 0 {
			return matchesLegacySequence(data, legacyKeySequences["delete"]) ||
				matchesKittySequence(data, cpDelete, 0)
		}
		if matchesLegacySequence(data, legacyShiftSequences["delete"]) ||
			matchesLegacySequence(data, legacyCtrlSequences["delete"]) {
			return true
		}
		return matchesKittySequence(data, cpDelete, modifier)

	case "clear":
		if modifier == 0 {
			return matchesLegacySequence(data, legacyKeySequences["clear"])
		}
		return matchesLegacySequence(data, legacyShiftSequences["clear"]) ||
			matchesLegacySequence(data, legacyCtrlSequences["clear"])

	case "home":
		if modifier == 0 {
			return matchesLegacySequence(data, legacyKeySequences["home"]) ||
				matchesKittySequence(data, cpHome, 0)
		}
		if matchesLegacySequence(data, legacyShiftSequences["home"]) ||
			matchesLegacySequence(data, legacyCtrlSequences["home"]) {
			return true
		}
		return matchesKittySequence(data, cpHome, modifier)

	case "end":
		if modifier == 0 {
			return matchesLegacySequence(data, legacyKeySequences["end"]) ||
				matchesKittySequence(data, cpEnd, 0)
		}
		if matchesLegacySequence(data, legacyShiftSequences["end"]) ||
			matchesLegacySequence(data, legacyCtrlSequences["end"]) {
			return true
		}
		return matchesKittySequence(data, cpEnd, modifier)

	case "pageup":
		if modifier == 0 {
			return matchesLegacySequence(data, legacyKeySequences["pageUp"]) ||
				matchesKittySequence(data, cpPageUp, 0)
		}
		if matchesLegacySequence(data, legacyShiftSequences["pageUp"]) ||
			matchesLegacySequence(data, legacyCtrlSequences["pageUp"]) {
			return true
		}
		return matchesKittySequence(data, cpPageUp, modifier)

	case "pagedown":
		if modifier == 0 {
			return matchesLegacySequence(data, legacyKeySequences["pageDown"]) ||
				matchesKittySequence(data, cpPgDown, 0)
		}
		if matchesLegacySequence(data, legacyShiftSequences["pageDown"]) ||
			matchesLegacySequence(data, legacyCtrlSequences["pageDown"]) {
			return true
		}
		return matchesKittySequence(data, cpPgDown, modifier)

	case "up":
		if alt && !ctrl && !shift {
			return data == "\x1bp" || matchesKittySequence(data, cpUp, modAlt)
		}
		if modifier == 0 {
			return matchesLegacySequence(data, legacyKeySequences["up"]) ||
				matchesKittySequence(data, cpUp, 0)
		}
		if matchesLegacySequence(data, legacyShiftSequences["up"]) ||
			matchesLegacySequence(data, legacyCtrlSequences["up"]) {
			return true
		}
		return matchesKittySequence(data, cpUp, modifier)

	case "down":
		if alt && !ctrl && !shift {
			return data == "\x1bn" || matchesKittySequence(data, cpDown, modAlt)
		}
		if modifier == 0 {
			return matchesLegacySequence(data, legacyKeySequences["down"]) ||
				matchesKittySequence(data, cpDown, 0)
		}
		if matchesLegacySequence(data, legacyShiftSequences["down"]) ||
			matchesLegacySequence(data, legacyCtrlSequences["down"]) {
			return true
		}
		return matchesKittySequence(data, cpDown, modifier)

	case "left":
		if alt && !ctrl && !shift {
			return data == "\x1b[1;3D" ||
				(!_kittyProtocolActive && data == "\x1bB") ||
				data == "\x1bb" ||
				matchesKittySequence(data, cpLeft, modAlt)
		}
		if ctrl && !alt && !shift {
			return data == "\x1b[1;5D" ||
				matchesLegacySequence(data, legacyCtrlSequences["left"]) ||
				matchesKittySequence(data, cpLeft, modCtrl)
		}
		if modifier == 0 {
			return matchesLegacySequence(data, legacyKeySequences["left"]) ||
				matchesKittySequence(data, cpLeft, 0)
		}
		if matchesLegacySequence(data, legacyShiftSequences["left"]) ||
			matchesLegacySequence(data, legacyCtrlSequences["left"]) {
			return true
		}
		return matchesKittySequence(data, cpLeft, modifier)

	case "right":
		if alt && !ctrl && !shift {
			return data == "\x1b[1;3C" ||
				(!_kittyProtocolActive && data == "\x1bF") ||
				data == "\x1bf" ||
				matchesKittySequence(data, cpRight, modAlt)
		}
		if ctrl && !alt && !shift {
			return data == "\x1b[1;5C" ||
				matchesLegacySequence(data, legacyCtrlSequences["right"]) ||
				matchesKittySequence(data, cpRight, modCtrl)
		}
		if modifier == 0 {
			return matchesLegacySequence(data, legacyKeySequences["right"]) ||
				matchesKittySequence(data, cpRight, 0)
		}
		if matchesLegacySequence(data, legacyShiftSequences["right"]) ||
			matchesLegacySequence(data, legacyCtrlSequences["right"]) {
			return true
		}
		return matchesKittySequence(data, cpRight, modifier)

	case "f1", "f2", "f3", "f4", "f5", "f6", "f7", "f8", "f9", "f10", "f11", "f12":
		if modifier != 0 {
			return false
		}
		sequences, ok := legacyKeySequences[key]
		return ok && matchesLegacySequence(data, sequences)
	}

	// Handle single letter keys (a-z) and some symbols
	if len(key) == 1 && ((key >= "a" && key <= "z") || symbolKeys[rune(key[0])]) {
		codepoint := int(key[0])
		rawCtrl := rawCtrlChar(key)

		if ctrl && alt && !shift && !_kittyProtocolActive && rawCtrl != "" {
			// Legacy: ctrl+alt+key is ESC followed by control character
			return data == "\x1b"+rawCtrl
		}

		if alt && !ctrl && !shift && !_kittyProtocolActive && key >= "a" && key <= "z" {
			// Legacy: alt+letter is ESC followed by letter
			if data == "\x1b"+key {
				return true
			}
		}

		if ctrl && !shift && !alt {
			// Legacy: ctrl+key sends control character
			if rawCtrl != "" && data == rawCtrl {
				return true
			}
			return matchesKittySequence(data, codepoint, modCtrl)
		}

		if ctrl && shift && !alt {
			return matchesKittySequence(data, codepoint, modShift+modCtrl)
		}

		if shift && !ctrl && !alt {
			// Legacy: shift+letter produces uppercase
			if data == strings.ToUpper(key) {
				return true
			}
			return matchesKittySequence(data, codepoint, modShift)
		}

		if modifier != 0 {
			return matchesKittySequence(data, codepoint, modifier)
		}

		// Check both raw char and Kitty sequence
		return data == key || matchesKittySequence(data, codepoint, 0)
	}

	return false
}

// =============================================================================
// Public API: ParseKey
// =============================================================================

// ParseKey parses input data and returns the key identifier if recognized.
// Returns empty string if not recognized.
func ParseKey(data string) string {
	kitty := parseKittySequence(data)
	if kitty != nil {
		codepoint := kitty.codepoint
		baseLayoutKey := kitty.baseLayoutKey
		modifier := kitty.modifier

		var mods []string
		effectiveMod := modifier & ^lockMask
		if effectiveMod&modShift != 0 {
			mods = append(mods, "shift")
		}
		if effectiveMod&modCtrl != 0 {
			mods = append(mods, "ctrl")
		}
		if effectiveMod&modAlt != 0 {
			mods = append(mods, "alt")
		}

		// Use base layout key only when codepoint is not a recognized Latin
		// letter (a-z) or symbol.
		isLatinLetter := codepoint >= 97 && codepoint <= 122
		_, isKnownSymbol := symbolKeys[rune(codepoint)]
		effectiveCodepoint := codepoint
		if baseLayoutKey != nil && !isLatinLetter && !isKnownSymbol {
			effectiveCodepoint = *baseLayoutKey
		}

		var keyName string
		switch effectiveCodepoint {
		case cpEscape:
			keyName = "escape"
		case cpTab:
			keyName = "tab"
		case cpEnter, cpKpEnter:
			keyName = "enter"
		case cpSpace:
			keyName = "space"
		case cpBacksp:
			keyName = "backspace"
		case cpDelete:
			keyName = "delete"
		case cpInsert:
			keyName = "insert"
		case cpHome:
			keyName = "home"
		case cpEnd:
			keyName = "end"
		case cpPageUp:
			keyName = "pageUp"
		case cpPgDown:
			keyName = "pageDown"
		case cpUp:
			keyName = "up"
		case cpDown:
			keyName = "down"
		case cpLeft:
			keyName = "left"
		case cpRight:
			keyName = "right"
		default:
			if effectiveCodepoint >= 97 && effectiveCodepoint <= 122 {
				keyName = string(rune(effectiveCodepoint))
			} else if symbolKeys[rune(effectiveCodepoint)] {
				keyName = string(rune(effectiveCodepoint))
			}
		}

		if keyName != "" {
			if len(mods) > 0 {
				return strings.Join(mods, "+") + "+" + keyName
			}
			return keyName
		}
	}

	// Mode-aware legacy sequences
	if _kittyProtocolActive {
		if data == "\x1b\r" || data == "\n" {
			return "shift+enter"
		}
	}

	if keyId, ok := legacySequenceKeyIDs[data]; ok {
		return keyId
	}

	// Legacy sequences (used when Kitty protocol is not active)
	if data == "\x1b" {
		return "escape"
	}
	if data == "\x1c" {
		return "ctrl+\\"
	}
	if data == "\x1d" {
		return "ctrl+]"
	}
	if data == "\x1f" {
		return "ctrl+-"
	}
	if data == "\x1b\x1b" {
		return "ctrl+alt+["
	}
	if data == "\x1b\x1c" {
		return "ctrl+alt+\\"
	}
	if data == "\x1b\x1d" {
		return "ctrl+alt+]"
	}
	if data == "\x1b\x1f" {
		return "ctrl+alt+-"
	}
	if data == "\t" {
		return "tab"
	}
	if data == "\r" || (!_kittyProtocolActive && data == "\n") || data == "\x1bOM" {
		return "enter"
	}
	if data == "\x00" {
		return "ctrl+space"
	}
	if data == " " {
		return "space"
	}
	if data == "\x7f" || data == "\x08" {
		return "backspace"
	}
	if data == "\x1b[Z" {
		return "shift+tab"
	}
	if !_kittyProtocolActive {
		if data == "\x1b\r" {
			return "alt+enter"
		}
		if data == "\x1b " {
			return "alt+space"
		}
		if data == "\x1b[1;3D" {
			return "alt+left"
		}
		if data == "\x1b[1;3C" {
			return "alt+right"
		}
	}
	if data == "\x1b\x7f" || data == "\x1b\b" {
		return "alt+backspace"
	}
	if !_kittyProtocolActive && data == "\x1bB" {
		return "alt+left"
	}
	if !_kittyProtocolActive && data == "\x1bF" {
		return "alt+right"
	}
	if !_kittyProtocolActive && len(data) == 2 && data[0] == '\x1b' {
		code := int(data[1])
		if code >= 1 && code <= 26 {
			return "ctrl+alt+" + string(rune(code+96))
		}
		if code >= 97 && code <= 122 {
			return "alt+" + string(rune(code))
		}
	}
	if data == "\x1b[A" {
		return "up"
	}
	if data == "\x1b[B" {
		return "down"
	}
	if data == "\x1b[C" {
		return "right"
	}
	if data == "\x1b[D" {
		return "left"
	}
	if data == "\x1b[H" || data == "\x1bOH" {
		return "home"
	}
	if data == "\x1b[F" || data == "\x1bOF" {
		return "end"
	}
	if data == "\x1b[3~" {
		return "delete"
	}
	if data == "\x1b[5~" {
		return "pageUp"
	}
	if data == "\x1b[6~" {
		return "pageDown"
	}

	// Raw Ctrl+letter
	if len(data) == 1 {
		code := int(data[0])
		if code >= 1 && code <= 26 {
			return "ctrl+" + string(rune(code+96))
		}
		if code >= 32 && code <= 126 {
			return data
		}
	}

	return ""
}

// IsKeyRelease checks if the last parsed key event was a key release.
// Only meaningful when Kitty keyboard protocol with flag 2 is active.
func IsKeyRelease(data string) bool {
	// Don't treat bracketed paste content as key release, even if it contains
	// patterns like ":3F" (e.g., bluetooth MAC addresses like "90:62:3F:A5").
	// Terminal re-wraps paste content with bracketed paste markers before
	// passing to TUI, so pasted data will always contain \x1b[200~.
	if strings.Contains(data, "\x1b[200~") {
		return false
	}

	// Quick check: release events with flag 2 contain ":3"
	if !strings.Contains(data, ":3") {
		return false
	}

	// Must be a CSI sequence
	if !strings.HasPrefix(data, "\x1b[") {
		return false
	}

	// Check for Kitty CSI-u sequences: \x1b[<codepoint>;<modifier>:3u
	if strings.HasSuffix(data, "u") {
		inner := data[2 : len(data)-1]
		parts := strings.Split(inner, ";")
		if len(parts) >= 2 {
			modifierPart := parts[len(parts)-1]
			return strings.HasSuffix(modifierPart, ":3")
		}
	}

	// Check for Kitty arrow/home/end sequences: \x1b[<num>;<modifier>:3[A/B/C/D/H/F]
	if len(data) >= 4 {
		lastChar := data[len(data)-1]
		if lastChar == 'A' || lastChar == 'B' || lastChar == 'C' || lastChar == 'D' || lastChar == 'H' || lastChar == 'F' {
			// Look for :3 before the final character
			inner := data[2 : len(data)-1]
			return strings.HasSuffix(inner, ":3") || strings.Contains(inner, ":3;")
		}
	}

	return false
}
