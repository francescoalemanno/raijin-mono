package ansi

import (
	"strconv"
	"strings"
)

// AnsiCodeTracker tracks active ANSI SGR codes to preserve styling across line breaks.
type AnsiCodeTracker struct {
	bold          bool
	dim           bool
	italic        bool
	underline     bool
	blink         bool
	inverse       bool
	hidden        bool
	strikethrough bool
	fgColor       string
	bgColor       string
}

// NewAnsiCodeTracker creates a new ANSI code tracker
func NewAnsiCodeTracker() *AnsiCodeTracker {
	return &AnsiCodeTracker{}
}

// Process processes an ANSI code and updates the tracker state
func (t *AnsiCodeTracker) Process(ansiCode string) {
	if !strings.HasSuffix(ansiCode, "m") {
		return
	}

	// Extract the parameters between \x1b[ and m
	if !strings.HasPrefix(ansiCode, "\x1b[") {
		return
	}

	// Skip "\x1b[" prefix and trailing "m"
	params := ansiCode[2 : len(ansiCode)-1]
	if params == "" || params == "0" {
		t.Reset()
		return
	}

	// Parse parameters (semicolon-separated)
	parts := strings.Split(params, ";")
	for i := 0; i < len(parts); {
		code, err := strconv.Atoi(parts[i])
		if err != nil {
			i++
			continue
		}

		// Handle 256-color and RGB codes which consume multiple parameters
		if code == 38 || code == 48 {
			if i+2 < len(parts) && parts[i+1] == "5" {
				// 256 color: 38;5;N or 48;5;N
				colorCode := parts[i] + ";" + parts[i+1] + ";" + parts[i+2]
				if code == 38 {
					t.fgColor = colorCode
				} else {
					t.bgColor = colorCode
				}
				i += 3
				continue
			}
			if i+4 < len(parts) && parts[i+1] == "2" {
				// RGB color: 38;2;R;G;B or 48;2;R;G;B
				colorCode := parts[i] + ";" + parts[i+1] + ";" + parts[i+2] + ";" + parts[i+3] + ";" + parts[i+4]
				if code == 38 {
					t.fgColor = colorCode
				} else {
					t.bgColor = colorCode
				}
				i += 5
				continue
			}
		}

		t.processCode(code)
		i++
	}
}

// processCode processes a single SGR code
func (t *AnsiCodeTracker) processCode(code int) {
	switch code {
	case 0:
		t.Reset()
	case 1:
		t.bold = true
	case 2:
		t.dim = true
	case 3:
		t.italic = true
	case 4:
		t.underline = true
	case 5:
		t.blink = true
	case 7:
		t.inverse = true
	case 8:
		t.hidden = true
	case 9:
		t.strikethrough = true
	case 21:
		t.bold = false
	case 22:
		t.bold = false
		t.dim = false
	case 23:
		t.italic = false
	case 24:
		t.underline = false
	case 25:
		t.blink = false
	case 27:
		t.inverse = false
	case 28:
		t.hidden = false
	case 29:
		t.strikethrough = false
	case 39:
		t.fgColor = "" // Default fg
	case 49:
		t.bgColor = "" // Default bg
	default:
		// Standard foreground colors 30-37, 90-97
		if (code >= 30 && code <= 37) || (code >= 90 && code <= 97) {
			t.fgColor = strconv.Itoa(code)
		}
		// Standard background colors 40-47, 100-107
		if (code >= 40 && code <= 47) || (code >= 100 && code <= 107) {
			t.bgColor = strconv.Itoa(code)
		}
	}
}

// Reset clears all state
func (t *AnsiCodeTracker) Reset() {
	t.bold = false
	t.dim = false
	t.italic = false
	t.underline = false
	t.blink = false
	t.inverse = false
	t.hidden = false
	t.strikethrough = false
	t.fgColor = ""
	t.bgColor = ""
}

// Clear clears all state (alias for Reset)
func (t *AnsiCodeTracker) Clear() {
	t.Reset()
}

// GetActiveCodes returns the active ANSI codes as a string
func (t *AnsiCodeTracker) GetActiveCodes() string {
	if !t.HasActiveCodes() {
		return ""
	}

	var codes []string
	if t.bold {
		codes = append(codes, "1")
	}
	if t.dim {
		codes = append(codes, "2")
	}
	if t.italic {
		codes = append(codes, "3")
	}
	if t.underline {
		codes = append(codes, "4")
	}
	if t.blink {
		codes = append(codes, "5")
	}
	if t.inverse {
		codes = append(codes, "7")
	}
	if t.hidden {
		codes = append(codes, "8")
	}
	if t.strikethrough {
		codes = append(codes, "9")
	}
	if t.fgColor != "" {
		codes = append(codes, t.fgColor)
	}
	if t.bgColor != "" {
		codes = append(codes, t.bgColor)
	}

	return "\x1b[" + strings.Join(codes, ";") + "m"
}

// HasActiveCodes checks if any codes are active
func (t *AnsiCodeTracker) HasActiveCodes() bool {
	return t.bold || t.dim || t.italic || t.underline ||
		t.blink || t.inverse || t.hidden || t.strikethrough ||
		t.fgColor != "" || t.bgColor != ""
}

// GetLineEndReset returns reset codes for attributes that bleed to padding
func (t *AnsiCodeTracker) GetLineEndReset() string {
	// Only underline causes visual bleeding into padding
	if t.underline {
		return "\x1b[24m"
	}
	return ""
}
