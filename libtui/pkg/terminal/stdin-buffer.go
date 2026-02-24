package terminal

import (
	"sync"
	"time"
)

const (
	ESC                   = "\x1b"
	BRACKETED_PASTE_START = "\x1b[200~"
	BRACKETED_PASTE_END   = "\x1b[201~"
)

// StdinBufferOptions configures a StdinBuffer
type StdinBufferOptions struct {
	// Timeout is the maximum time to wait for sequence completion (default: 10ms)
	Timeout int
}

// StdinBuffer buffers stdin input and emits complete sequences.
// Handles partial escape sequences that arrive across multiple chunks.
type StdinBuffer struct {
	mu          sync.Mutex
	buffer      string
	timeout     *time.Timer
	timeoutSeq  uint64
	timeoutMs   int
	onData      func(string)
	onPaste     func(string)
	pasteMode   bool
	pasteBuffer string
}

// NewStdinBuffer creates a new StdinBuffer with the given options
func NewStdinBuffer(opts StdinBufferOptions) *StdinBuffer {
	timeoutMs := opts.Timeout
	if timeoutMs == 0 {
		timeoutMs = 10
	}

	return &StdinBuffer{
		timeoutMs: timeoutMs,
	}
}

// SetOnData sets the callback for when complete sequences are emitted
func (sb *StdinBuffer) SetOnData(callback func(string)) {
	sb.mu.Lock()
	defer sb.mu.Unlock()
	sb.onData = callback
}

// SetOnPaste sets the callback for when paste content is emitted
func (sb *StdinBuffer) SetOnPaste(callback func(string)) {
	sb.mu.Lock()
	defer sb.mu.Unlock()
	sb.onPaste = callback
}

// Process feeds input data into the buffer
func (sb *StdinBuffer) Process(data interface{}) {
	// Convert data to string
	var str string
	if bytes, ok := data.([]byte); ok {
		if len(bytes) == 1 && bytes[0] > 127 {
			// High-byte conversion for compatibility
			byteVal := bytes[0] - 128
			str = ESC + string(rune(byteVal))
		} else {
			str = string(bytes)
		}
	} else if s, ok := data.(string); ok {
		str = s
	} else {
		return
	}

	var dataEvents []string
	var pasteEvents []string
	var onData func(string)
	var onPaste func(string)
	var remaining string

	sb.mu.Lock()
	sb.cancelTimeoutLocked()

	if str == "" && sb.buffer == "" {
		onData = sb.onData
		sb.mu.Unlock()
		if onData != nil {
			onData("")
		}
		return
	}

	sb.buffer += str

	// Handle paste mode
	if sb.pasteMode {
		sb.pasteBuffer += sb.buffer
		sb.buffer = ""

		endIndex := findSubstring(sb.pasteBuffer, BRACKETED_PASTE_END)
		if endIndex != -1 {
			pastedContent := sb.pasteBuffer[:endIndex]
			remaining = sb.pasteBuffer[endIndex+len(BRACKETED_PASTE_END):]

			sb.pasteMode = false
			sb.pasteBuffer = ""
			pasteEvents = append(pasteEvents, pastedContent)
		}

		onPaste = sb.onPaste
		sb.mu.Unlock()

		for _, paste := range pasteEvents {
			if onPaste != nil {
				onPaste(paste)
			}
		}
		if len(remaining) > 0 {
			sb.Process(remaining)
		}
		return
	}

	// Check for paste start
	startIndex := findSubstring(sb.buffer, BRACKETED_PASTE_START)
	if startIndex != -1 {
		if startIndex > 0 {
			beforePaste := sb.buffer[:startIndex]
			result := extractCompleteSequences(beforePaste)
			dataEvents = append(dataEvents, result.sequences...)
		}

		sb.buffer = sb.buffer[startIndex+len(BRACKETED_PASTE_START):]
		sb.pasteMode = true
		sb.pasteBuffer = sb.buffer
		sb.buffer = ""

		endIndex := findSubstring(sb.pasteBuffer, BRACKETED_PASTE_END)
		if endIndex != -1 {
			pastedContent := sb.pasteBuffer[:endIndex]
			remaining = sb.pasteBuffer[endIndex+len(BRACKETED_PASTE_END):]

			sb.pasteMode = false
			sb.pasteBuffer = ""
			pasteEvents = append(pasteEvents, pastedContent)
		}

		onData = sb.onData
		onPaste = sb.onPaste
		sb.mu.Unlock()

		for _, sequence := range dataEvents {
			if onData != nil {
				onData(sequence)
			}
		}
		for _, paste := range pasteEvents {
			if onPaste != nil {
				onPaste(paste)
			}
		}
		if len(remaining) > 0 {
			sb.Process(remaining)
		}
		return
	}

	// Extract complete sequences from buffer
	result := extractCompleteSequences(sb.buffer)
	sb.buffer = result.remainder
	dataEvents = append(dataEvents, result.sequences...)

	// Set timeout for incomplete sequences
	if len(sb.buffer) > 0 {
		sb.timeoutSeq++
		seq := sb.timeoutSeq
		sb.timeout = time.AfterFunc(time.Duration(sb.timeoutMs)*time.Millisecond, func() {
			sb.flushFromTimeout(seq)
		})
	}

	onData = sb.onData
	sb.mu.Unlock()

	for _, sequence := range dataEvents {
		if onData != nil {
			onData(sequence)
		}
	}
}

// Flush flushes the buffer and returns any remaining content
func (sb *StdinBuffer) Flush() []string {
	sb.mu.Lock()
	defer sb.mu.Unlock()
	sb.cancelTimeoutLocked()

	if sb.buffer == "" {
		return []string{}
	}

	sequences := []string{sb.buffer}
	sb.buffer = ""
	return sequences
}

// Clear clears the buffer and reset state
func (sb *StdinBuffer) Clear() {
	sb.mu.Lock()
	defer sb.mu.Unlock()
	sb.cancelTimeoutLocked()
	sb.buffer = ""
	sb.pasteMode = false
	sb.pasteBuffer = ""
}

// GetBuffer returns the current buffer content
func (sb *StdinBuffer) GetBuffer() string {
	sb.mu.Lock()
	defer sb.mu.Unlock()
	return sb.buffer
}

// Destroy cleans up resources
func (sb *StdinBuffer) Destroy() {
	sb.Clear()
}

func (sb *StdinBuffer) cancelTimeoutLocked() {
	sb.timeoutSeq++
	if sb.timeout != nil {
		sb.timeout.Stop()
		sb.timeout = nil
	}
}

func (sb *StdinBuffer) flushFromTimeout(seq uint64) {
	var flushed string
	var onData func(string)

	sb.mu.Lock()
	if seq != sb.timeoutSeq {
		sb.mu.Unlock()
		return
	}

	sb.timeout = nil
	if sb.buffer != "" {
		flushed = sb.buffer
		sb.buffer = ""
		onData = sb.onData
	}
	sb.mu.Unlock()

	if flushed != "" && onData != nil {
		onData(flushed)
	}
}

// Helper: find substring (strings.Index alternative)
func findSubstring(s, substr string) int {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}

// Helper: escape sequence completion status
type sequenceStatus int

const (
	statusComplete sequenceStatus = iota
	statusIncomplete
	statusNotEscape
)

// isCompleteSequence checks if a string is a complete escape sequence
func isCompleteSequence(data string) sequenceStatus {
	if len(data) == 0 || data[0] != ESC[0] {
		return statusNotEscape
	}

	if len(data) == 1 {
		return statusIncomplete
	}

	afterEsc := data[1:]

	// CSI sequences: ESC [
	if len(afterEsc) > 0 && afterEsc[0] == '[' {
		// Check for old-style mouse: ESC[M + 3 bytes
		if len(afterEsc) >= 2 && afterEsc[1] == 'M' {
			// Old-style mouse needs ESC[M + 3 bytes = 6 total
			if len(data) >= 6 {
				return statusComplete
			}
			return statusIncomplete
		}
		return isCompleteCsiSequence(data)
	}

	// OSC sequences: ESC ]
	if len(afterEsc) > 0 && afterEsc[0] == ']' {
		return isCompleteOscSequence(data)
	}

	// DCS sequences: ESC P
	if len(afterEsc) > 0 && afterEsc[0] == 'P' {
		return isCompleteDcsSequence(data)
	}

	// APC sequences: ESC _
	if len(afterEsc) > 0 && afterEsc[0] == '_' {
		return isCompleteApcSequence(data)
	}

	// SS3 sequences: ESC O
	if len(afterEsc) > 0 && afterEsc[0] == 'O' {
		// ESC O followed by a single character
		if len(afterEsc) >= 2 {
			return statusComplete
		}
		return statusIncomplete
	}

	// Meta key sequences: ESC followed by a single character
	if len(afterEsc) == 1 {
		return statusComplete
	}

	return statusComplete
}

// isCompleteCsiSequence checks if CSI sequence is complete
func isCompleteCsiSequence(data string) sequenceStatus {
	if len(data) < 2 {
		return statusIncomplete
	}

	if data[:2] != ESC+"[" {
		return statusComplete
	}

	// Need at least ESC [ and one more character
	if len(data) < 3 {
		return statusIncomplete
	}

	payload := data[2:]

	// CSI sequences end with a byte in 0x40-0x7E (@-~)
	if len(payload) == 0 {
		return statusIncomplete
	}

	lastChar := payload[len(payload)-1]
	lastByte := lastChar

	if lastByte >= 0x40 && lastByte <= 0x7e {
		// Special handling for SGR mouse sequences
		// Format: ESC[<B;X;Ym or ESC[<B;X;YM
		if len(payload) > 0 && payload[0] == '<' {
			mouseMatch := isMouseSGRSequence(payload)
			if mouseMatch {
				return statusComplete
			}
			if lastChar == 'M' || lastChar == 'm' {
				// Check if we have the right structure
				if len(payload) > 1 {
					parts := splitString(payload[1:len(payload)-1], ";")
					if len(parts) == 3 && isAllDigits(parts) {
						return statusComplete
					}
				}
			}
			return statusIncomplete
		}
		return statusComplete
	}

	return statusIncomplete
}

// isMouseSGRSequence checks if the payload matches mouse SGR format
func isMouseSGRSequence(payload string) bool {
	if len(payload) < 2 {
		return false
	}
	if payload[0] != '<' {
		return false
	}
	lastChar := payload[len(payload)-1]
	if lastChar != 'M' && lastChar != 'm' {
		return false
	}
	payloadWithoutBrackets := payload[1 : len(payload)-1]
	parts := splitString(payloadWithoutBrackets, ";")
	if len(parts) != 3 {
		return false
	}
	return isAllDigits(parts)
}

// isCompleteOscSequence checks if OSC sequence is complete
func isCompleteOscSequence(data string) sequenceStatus {
	if len(data) < 2 {
		return statusIncomplete
	}

	if data[:2] != ESC+"]" {
		return statusComplete
	}

	// OSC sequences end with ST (ESC \) or BEL (\x07)
	if hasSuffix(data, ESC+"\\") || hasSuffix(data, "\x07") {
		return statusComplete
	}

	return statusIncomplete
}

// isCompleteDcsSequence checks if DCS sequence is complete
func isCompleteDcsSequence(data string) sequenceStatus {
	if len(data) < 2 {
		return statusIncomplete
	}

	if data[:2] != ESC+"P" {
		return statusComplete
	}

	// DCS sequences end with ST (ESC \)
	if hasSuffix(data, ESC+"\\") {
		return statusComplete
	}

	return statusIncomplete
}

// isCompleteApcSequence checks if APC sequence is complete
func isCompleteApcSequence(data string) sequenceStatus {
	if len(data) < 2 {
		return statusIncomplete
	}

	if data[:2] != ESC+"_" {
		return statusComplete
	}

	// APC sequences end with ST (ESC \)
	if hasSuffix(data, ESC+"\\") {
		return statusComplete
	}

	return statusIncomplete
}

// extractCompleteSequencesResult represents the result of extracting sequences
type extractCompleteSequencesResult struct {
	sequences []string
	remainder string
}

// extractCompleteSequences splits accumulated buffer into complete sequences
func extractCompleteSequences(buffer string) extractCompleteSequencesResult {
	var sequences []string
	runeStr := []rune(buffer)
	pos := 0

	for pos < len(runeStr) {
		remaining := string(runeStr[pos:])

		// Try to extract a sequence starting at this position
		if len(remaining) > 0 && remaining[0] == ESC[0] {
			// Find the end of this escape sequence
			seqEnd := 1
			for seqEnd <= len(remaining) {
				candidate := remaining[:seqEnd]
				status := isCompleteSequence(candidate)

				if status == statusComplete {
					sequences = append(sequences, candidate)
					pos += len([]rune(candidate))
					break
				} else if status == statusIncomplete {
					seqEnd++
				} else {
					// Should not happen when starting with ESC
					sequences = append(sequences, candidate)
					pos += len([]rune(candidate))
					break
				}
			}

			if seqEnd > len(remaining) {
				return extractCompleteSequencesResult{
					sequences: sequences,
					remainder: remaining,
				}
			}
		} else {
			// Not an escape sequence - take a single rune
			if len(remaining) > 0 {
				sequences = append(sequences, string(runeStr[pos:pos+1]))
			}
			pos++
		}
	}

	return extractCompleteSequencesResult{
		sequences: sequences,
		remainder: "",
	}
}

// Helper: split string by separator
func splitString(s, sep string) []string {
	if s == "" {
		return []string{}
	}

	var parts []string
	start := 0
	for {
		idx := findSubstring(s[start:], sep)
		if idx == -1 {
			parts = append(parts, s[start:])
			break
		}
		parts = append(parts, s[start:start+idx])
		start += idx + len(sep)
	}
	return parts
}

// Helper: check if all strings are digits
func isAllDigits(items []string) bool {
	for _, item := range items {
		for _, ch := range item {
			if ch < '0' || ch > '9' {
				return false
			}
		}
	}
	return true
}

// Helper: check if string has suffix
func hasSuffix(s, suffix string) bool {
	if len(s) < len(suffix) {
		return false
	}
	return s[len(s)-len(suffix):] == suffix
}
