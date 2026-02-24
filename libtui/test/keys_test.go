package test

import (
	"testing"

	"github.com/francescoalemanno/raijin-mono/libtui/pkg/keys"
	"github.com/stretchr/testify/assert"
)

// Test matchesKey - Kitty protocol with alternate keys (non-Latin layouts)

func TestMatchesKey_CyrillicCtrlC_WithBaseLayoutKey(t *testing.T) {
	keys.SetKittyProtocolActive(true)
	defer keys.SetKittyProtocolActive(false)

	// Cyrillic 'с' = codepoint 1089, Latin 'c' = codepoint 99
	// Format: CSI 1089::99;5u (codepoint::base;modifier with ctrl=4, +1=5)
	cyrillicCtrlC := "\x1b[1089::99;5u"
	assert.True(t, keys.MatchesKey(cyrillicCtrlC, "ctrl+c"))
}

func TestMatchesKey_CyrillicCtrlD_WithBaseLayoutKey(t *testing.T) {
	keys.SetKittyProtocolActive(true)
	defer keys.SetKittyProtocolActive(false)

	// Cyrillic 'в' = codepoint 1074, Latin 'd' = codepoint 100
	cyrillicCtrlD := "\x1b[1074::100;5u"
	assert.True(t, keys.MatchesKey(cyrillicCtrlD, "ctrl+d"))
}

func TestMatchesKey_CyrillicCtrlZ_WithBaseLayoutKey(t *testing.T) {
	keys.SetKittyProtocolActive(true)
	defer keys.SetKittyProtocolActive(false)

	// Cyrillic 'я' = codepoint 1103, Latin 'z' = codepoint 122
	cyrillicCtrlZ := "\x1b[1103::122;5u"
	assert.True(t, keys.MatchesKey(cyrillicCtrlZ, "ctrl+z"))
}

func TestMatchesKey_CtrlShiftP_WithBaseLayoutKey(t *testing.T) {
	keys.SetKittyProtocolActive(true)
	defer keys.SetKittyProtocolActive(false)

	// Cyrillic 'з' = codepoint 1079, Latin 'p' = codepoint 112
	// ctrl=4, shift=1, +1 = 6
	cyrillicCtrlShiftP := "\x1b[1079::112;6u"
	assert.True(t, keys.MatchesKey(cyrillicCtrlShiftP, "ctrl+shift+p"))
}

func TestMatchesKey_DirectCodepointWhenNoBaseLayout(t *testing.T) {
	keys.SetKittyProtocolActive(true)
	defer keys.SetKittyProtocolActive(false)

	// Latin ctrl+c without base layout key (terminal doesn't support flag 4)
	latinCtrlC := "\x1b[99;5u"
	assert.True(t, keys.MatchesKey(latinCtrlC, "ctrl+c"))
}

func TestMatchesKey_ShiftedKeyInFormat(t *testing.T) {
	keys.SetKittyProtocolActive(true)
	defer keys.SetKittyProtocolActive(false)

	// Format with shifted key: CSI codepoint:shifted:base;modifier u
	// Latin 'c' with shifted 'C' (67) and base 'c' (99)
	shiftedKey := "\x1b[99:67:99;2u" // shift modifier = 1, +1 = 2
	assert.True(t, keys.MatchesKey(shiftedKey, "shift+c"))
}

func TestMatchesKey_EventTypeInFormat(t *testing.T) {
	keys.SetKittyProtocolActive(true)
	defer keys.SetKittyProtocolActive(false)

	// Format with event type: CSI codepoint::base;modifier:event u
	// Cyrillic ctrl+c release event (event type 3)
	releaseEvent := "\x1b[1089::99;5:3u"
	assert.True(t, keys.MatchesKey(releaseEvent, "ctrl+c"))
}

func TestMatchesKey_FullFormatWithAllFields(t *testing.T) {
	keys.SetKittyProtocolActive(true)
	defer keys.SetKittyProtocolActive(false)

	// Full format: CSI codepoint:shifted:base;modifier:event u
	// Cyrillic 'С' (shifted) with base 'c', Ctrl+Shift pressed, repeat event
	// Cyrillic 'с' = 1089, Cyrillic 'С' = 1057, Latin 'c' = 99
	// ctrl=4, shift=1, +1 = 6, repeat event = 2
	fullFormat := "\x1b[1089:1057:99;6:2u"
	assert.True(t, keys.MatchesKey(fullFormat, "ctrl+shift+c"))
}

func TestMatchesKey_PreferCodepointForLatinLetters(t *testing.T) {
	keys.SetKittyProtocolActive(true)
	defer keys.SetKittyProtocolActive(false)

	// Dvorak Ctrl+K reports codepoint 'k' (107) and base layout 'v' (118)
	dvorakCtrlK := "\x1b[107::118;5u"
	assert.True(t, keys.MatchesKey(dvorakCtrlK, "ctrl+k"))
	assert.False(t, keys.MatchesKey(dvorakCtrlK, "ctrl+v"))
}

func TestMatchesKey_PreferCodepointForSymbolKeys(t *testing.T) {
	keys.SetKittyProtocolActive(true)
	defer keys.SetKittyProtocolActive(false)

	// Dvorak Ctrl+/ reports codepoint '/' (47) and base layout '[' (91)
	dvorakCtrlSlash := "\x1b[47::91;5u"
	assert.True(t, keys.MatchesKey(dvorakCtrlSlash, "ctrl+/"))
	assert.False(t, keys.MatchesKey(dvorakCtrlSlash, "ctrl+["))
}

func TestMatchesKey_WrongKeyEvenWithBaseLayout(t *testing.T) {
	keys.SetKittyProtocolActive(true)
	defer keys.SetKittyProtocolActive(false)

	// Cyrillic ctrl+с with base 'c' should NOT match ctrl+d
	cyrillicCtrlC := "\x1b[1089::99;5u"
	assert.False(t, keys.MatchesKey(cyrillicCtrlC, "ctrl+d"))
}

func TestMatchesKey_WrongModifiersEvenWithBaseLayout(t *testing.T) {
	keys.SetKittyProtocolActive(true)
	defer keys.SetKittyProtocolActive(false)

	// Cyrillic ctrl+с should NOT match ctrl+shift+c
	cyrillicCtrlC := "\x1b[1089::99;5u"
	assert.False(t, keys.MatchesKey(cyrillicCtrlC, "ctrl+shift+c"))
}

// Test matchesKey - Legacy key matching

func TestMatchesKey_LegacyCtrlC(t *testing.T) {
	keys.SetKittyProtocolActive(false)
	// Ctrl+c sends ASCII 3 (ETX)
	assert.True(t, keys.MatchesKey("\x03", "ctrl+c"))
}

func TestMatchesKey_LegacyCtrlD(t *testing.T) {
	keys.SetKittyProtocolActive(false)
	// Ctrl+d sends ASCII 4 (EOT)
	assert.True(t, keys.MatchesKey("\x04", "ctrl+d"))
}

func TestMatchesKey_EscapeKey(t *testing.T) {
	assert.True(t, keys.MatchesKey("\x1b", "escape"))
}

func TestMatchesKey_LegacyLinefeedAsEnter(t *testing.T) {
	keys.SetKittyProtocolActive(false)
	assert.True(t, keys.MatchesKey("\n", "enter"))
	assert.Equal(t, "enter", keys.ParseKey("\n"))
}

func TestMatchesKey_LinefeedAsShiftEnterWhenKittyActive(t *testing.T) {
	keys.SetKittyProtocolActive(true)
	defer keys.SetKittyProtocolActive(false)

	assert.True(t, keys.MatchesKey("\n", "shift+enter"))
	assert.False(t, keys.MatchesKey("\n", "enter"))
	assert.Equal(t, "shift+enter", keys.ParseKey("\n"))
}

func TestMatchesKey_CtrlSpace(t *testing.T) {
	keys.SetKittyProtocolActive(false)
	assert.True(t, keys.MatchesKey("\x00", "ctrl+space"))
	assert.Equal(t, "ctrl+space", keys.ParseKey("\x00"))
}

func TestMatchesKey_LegacyCtrlSymbol(t *testing.T) {
	keys.SetKittyProtocolActive(false)
	// Ctrl+\ sends ASCII 28 (File Separator) in legacy terminals
	assert.True(t, keys.MatchesKey("\x1c", "ctrl+\\"))
	assert.Equal(t, "ctrl+\\", keys.ParseKey("\x1c"))
	// Ctrl+] sends ASCII 29 (Group Separator) in legacy terminals
	assert.True(t, keys.MatchesKey("\x1d", "ctrl+]"))
	assert.Equal(t, "ctrl+]", keys.ParseKey("\x1d"))
	// Ctrl+_ sends ASCII 31 (Unit Separator) in legacy terminals
	// Ctrl+- is on the same physical key on US keyboards
	assert.True(t, keys.MatchesKey("\x1f", "ctrl+_"))
	assert.True(t, keys.MatchesKey("\x1f", "ctrl+-"))
	assert.Equal(t, "ctrl+-", keys.ParseKey("\x1f"))
}

func TestMatchesKey_LegacyCtrlAltSymbol(t *testing.T) {
	keys.SetKittyProtocolActive(false)
	// Ctrl+Alt+[ sends ESC followed by ESC (Ctrl+[ = ESC)
	assert.True(t, keys.MatchesKey("\x1b\x1b", "ctrl+alt+["))
	assert.Equal(t, "ctrl+alt+[", keys.ParseKey("\x1b\x1b"))
	// Ctrl+Alt+\ sends ESC followed by ASCII 28
	assert.True(t, keys.MatchesKey("\x1b\x1c", "ctrl+alt+\\"))
	assert.Equal(t, "ctrl+alt+\\", keys.ParseKey("\x1b\x1c"))
	// Ctrl+Alt+] sends ESC followed by ASCII 29
	assert.True(t, keys.MatchesKey("\x1b\x1d", "ctrl+alt+]"))
	assert.Equal(t, "ctrl+alt+]", keys.ParseKey("\x1b\x1d"))
	// Ctrl+Alt+- sends ESC followed by ASCII 31
	assert.True(t, keys.MatchesKey("\x1b\x1f", "ctrl+alt+_"))
	assert.True(t, keys.MatchesKey("\x1b\x1f", "ctrl+alt+-"))
	assert.Equal(t, "ctrl+alt+-", keys.ParseKey("\x1b\x1f"))
}

func TestMatchesKey_LegacyAltPrefixed(t *testing.T) {
	keys.SetKittyProtocolActive(false)
	assert.True(t, keys.MatchesKey("\x1b ", "alt+space"))
	assert.Equal(t, "alt+space", keys.ParseKey("\x1b "))
	assert.True(t, keys.MatchesKey("\x1b\b", "alt+backspace"))
	assert.Equal(t, "alt+backspace", keys.ParseKey("\x1b\b"))
	assert.True(t, keys.MatchesKey("\x1b\x03", "ctrl+alt+c"))
	assert.Equal(t, "ctrl+alt+c", keys.ParseKey("\x1b\x03"))
	assert.True(t, keys.MatchesKey("\x1bB", "alt+left"))
	assert.Equal(t, "alt+left", keys.ParseKey("\x1bB"))
	assert.True(t, keys.MatchesKey("\x1bF", "alt+right"))
	assert.Equal(t, "alt+right", keys.ParseKey("\x1bF"))
	assert.True(t, keys.MatchesKey("\x1ba", "alt+a"))
	assert.Equal(t, "alt+a", keys.ParseKey("\x1ba"))
	assert.True(t, keys.MatchesKey("\x1by", "alt+y"))
	assert.Equal(t, "alt+y", keys.ParseKey("\x1by"))
	assert.True(t, keys.MatchesKey("\x1bz", "alt+z"))
	assert.Equal(t, "alt+z", keys.ParseKey("\x1bz"))

	keys.SetKittyProtocolActive(true)
	assert.False(t, keys.MatchesKey("\x1b ", "alt+space"))
	assert.Equal(t, "", keys.ParseKey("\x1b "))
	assert.True(t, keys.MatchesKey("\x1b\b", "alt+backspace"))
	assert.Equal(t, "alt+backspace", keys.ParseKey("\x1b\b"))
	assert.False(t, keys.MatchesKey("\x1b\x03", "ctrl+alt+c"))
	assert.Equal(t, "", keys.ParseKey("\x1b\x03"))
	assert.False(t, keys.MatchesKey("\x1bB", "alt+left"))
	assert.Equal(t, "", keys.ParseKey("\x1bB"))
	assert.False(t, keys.MatchesKey("\x1bF", "alt+right"))
	assert.Equal(t, "", keys.ParseKey("\x1bF"))
	assert.False(t, keys.MatchesKey("\x1ba", "alt+a"))
	assert.Equal(t, "", keys.ParseKey("\x1ba"))
	assert.False(t, keys.MatchesKey("\x1by", "alt+y"))
	assert.Equal(t, "", keys.ParseKey("\x1by"))

	keys.SetKittyProtocolActive(false)
}

func TestMatchesKey_ArrowKeys(t *testing.T) {
	assert.True(t, keys.MatchesKey("\x1b[A", "up"))
	assert.True(t, keys.MatchesKey("\x1b[B", "down"))
	assert.True(t, keys.MatchesKey("\x1b[C", "right"))
	assert.True(t, keys.MatchesKey("\x1b[D", "left"))
}

func TestMatchesKey_SS3ArrowsAndHomeEnd(t *testing.T) {
	assert.True(t, keys.MatchesKey("\x1bOA", "up"))
	assert.True(t, keys.MatchesKey("\x1bOB", "down"))
	assert.True(t, keys.MatchesKey("\x1bOC", "right"))
	assert.True(t, keys.MatchesKey("\x1bOD", "left"))
	assert.True(t, keys.MatchesKey("\x1bOH", "home"))
	assert.True(t, keys.MatchesKey("\x1bOF", "end"))
}

func TestMatchesKey_LegacyFunctionKeysAndClear(t *testing.T) {
	assert.True(t, keys.MatchesKey("\x1bOP", "f1"))
	assert.True(t, keys.MatchesKey("\x1b[24~", "f12"))
	assert.True(t, keys.MatchesKey("\x1b[E", "clear"))
}

func TestMatchesKey_AltArrows(t *testing.T) {
	assert.True(t, keys.MatchesKey("\x1bp", "alt+up"))
	assert.False(t, keys.MatchesKey("\x1bp", "up"))
}

func TestMatchesKey_RxvtModifierSequences(t *testing.T) {
	assert.True(t, keys.MatchesKey("\x1b[a", "shift+up"))
	assert.True(t, keys.MatchesKey("\x1bOa", "ctrl+up"))
	assert.True(t, keys.MatchesKey("\x1b[2$", "shift+insert"))
	assert.True(t, keys.MatchesKey("\x1b[2^", "ctrl+insert"))
	assert.True(t, keys.MatchesKey("\x1b[7$", "shift+home"))
}

// Test parseKey - Kitty protocol with alternate keys

func TestParseKey_BaseLayoutKeyIsPresent(t *testing.T) {
	keys.SetKittyProtocolActive(true)
	defer keys.SetKittyProtocolActive(false)

	// Cyrillic ctrl+с with base layout 'c'
	cyrillicCtrlC := "\x1b[1089::99;5u"
	assert.Equal(t, "ctrl+c", keys.ParseKey(cyrillicCtrlC))
}

func TestParseKey_PreferCodepointForLatinLetters(t *testing.T) {
	keys.SetKittyProtocolActive(true)
	defer keys.SetKittyProtocolActive(false)

	// Dvorak Ctrl+K reports codepoint 'k' (107) and base layout 'v' (118)
	dvorakCtrlK := "\x1b[107::118;5u"
	assert.Equal(t, "ctrl+k", keys.ParseKey(dvorakCtrlK))
}

func TestParseKey_PreferCodepointForSymbolKeys(t *testing.T) {
	keys.SetKittyProtocolActive(true)
	defer keys.SetKittyProtocolActive(false)

	// Dvorak Ctrl+/ reports codepoint '/' (47) and base layout '[' (91)
	dvorakCtrlSlash := "\x1b[47::91;5u"
	assert.Equal(t, "ctrl+/", keys.ParseKey(dvorakCtrlSlash))
}

func TestParseKey_FromCodepointWhenNoBaseLayout(t *testing.T) {
	keys.SetKittyProtocolActive(true)
	defer keys.SetKittyProtocolActive(false)

	latinCtrlC := "\x1b[99;5u"
	assert.Equal(t, "ctrl+c", keys.ParseKey(latinCtrlC))
}

// Test parseKey - Legacy key parsing

func TestParseKey_LegacyCtrlLetter(t *testing.T) {
	keys.SetKittyProtocolActive(false)
	assert.Equal(t, "ctrl+c", keys.ParseKey("\x03"))
	assert.Equal(t, "ctrl+d", keys.ParseKey("\x04"))
}

func TestParseKey_SpecialKeys(t *testing.T) {
	assert.Equal(t, "escape", keys.ParseKey("\x1b"))
	assert.Equal(t, "tab", keys.ParseKey("\t"))
	assert.Equal(t, "enter", keys.ParseKey("\r"))
	assert.Equal(t, "enter", keys.ParseKey("\n"))
	assert.Equal(t, "ctrl+space", keys.ParseKey("\x00"))
	assert.Equal(t, "space", keys.ParseKey(" "))
}

func TestParseKey_ArrowKeys(t *testing.T) {
	assert.Equal(t, "up", keys.ParseKey("\x1b[A"))
	assert.Equal(t, "down", keys.ParseKey("\x1b[B"))
	assert.Equal(t, "right", keys.ParseKey("\x1b[C"))
	assert.Equal(t, "left", keys.ParseKey("\x1b[D"))
}

func TestParseKey_SS3ArrowsAndHomeEnd(t *testing.T) {
	assert.Equal(t, "up", keys.ParseKey("\x1bOA"))
	assert.Equal(t, "down", keys.ParseKey("\x1bOB"))
	assert.Equal(t, "right", keys.ParseKey("\x1bOC"))
	assert.Equal(t, "left", keys.ParseKey("\x1bOD"))
	assert.Equal(t, "home", keys.ParseKey("\x1bOH"))
	assert.Equal(t, "end", keys.ParseKey("\x1bOF"))
}

func TestParseKey_LegacyFunctionAndModifierSequences(t *testing.T) {
	assert.Equal(t, "f1", keys.ParseKey("\x1bOP"))
	assert.Equal(t, "f12", keys.ParseKey("\x1b[24~"))
	assert.Equal(t, "clear", keys.ParseKey("\x1b[E"))
	assert.Equal(t, "ctrl+insert", keys.ParseKey("\x1b[2^"))
	assert.Equal(t, "alt+up", keys.ParseKey("\x1bp"))
}

func TestParseKey_DoubleBracketPageUp(t *testing.T) {
	assert.Equal(t, "pageUp", keys.ParseKey("\x1b[[5~"))
}
