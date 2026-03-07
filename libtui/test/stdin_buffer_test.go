package test

import (
	"sync"
	"testing"
	"time"

	terminalpkg "github.com/francescoalemanno/raijin-mono/libtui/pkg/terminal"
)

// Helper to collect emitted sequences
type testBufferCollector struct {
	mu     sync.Mutex
	data   []string
	pastes []string
}

func (c *testBufferCollector) OnData(seq string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.data = append(c.data, seq)
}

func (c *testBufferCollector) OnPaste(content string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.pastes = append(c.pastes, content)
}

func (c *testBufferCollector) GetData() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.data
}

func (c *testBufferCollector) GetPastes() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.pastes
}

func (c *testBufferCollector) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.data = nil
	c.pastes = nil
}

// Helper to wait for async operations
func wait(t *testing.T, ms int) {
	time.Sleep(time.Duration(ms) * time.Millisecond)
}

func newTestBuffer() (*terminalpkg.StdinBuffer, *testBufferCollector) {
	collector := &testBufferCollector{}
	buffer := terminalpkg.NewStdinBuffer(terminalpkg.StdinBufferOptions{Timeout: 10})
	buffer.SetOnData(collector.OnData)
	buffer.SetOnPaste(collector.OnPaste)
	return buffer, collector
}

// ------------------------------------------------------------
// Regular Characters
// ------------------------------------------------------------
func TestStdinBuffer_RegularChars(t *testing.T) {
	t.Run("should pass through regular characters immediately", func(t *testing.T) {
		buffer, collector := newTestBuffer()
		buffer.Process("a")
		expected := []string{"a"}
		got := collector.GetData()
		assertSlicesEqual(t, expected, got)
	})

	t.Run("should pass through multiple regular characters", func(t *testing.T) {
		buffer, collector := newTestBuffer()
		buffer.Process("abc")
		expected := []string{"a", "b", "c"}
		got := collector.GetData()
		assertSlicesEqual(t, expected, got)
	})

	t.Run("should handle unicode characters", func(t *testing.T) {
		buffer, collector := newTestBuffer()
		buffer.Process("hello 世界")
		expected := []string{"h", "e", "l", "l", "o", " ", "世", "界"}
		got := collector.GetData()
		assertSlicesEqual(t, expected, got)
	})
}

// ------------------------------------------------------------
// Complete Escape Sequences
// ------------------------------------------------------------
func TestStdinBuffer_CompleteEscapeSequences(t *testing.T) {
	t.Run("should pass through complete mouse SGR sequences", func(t *testing.T) {
		buffer, collector := newTestBuffer()
		mouseSeq := "\x1b[<35;20;5m"
		buffer.Process(mouseSeq)
		expected := []string{mouseSeq}
		got := collector.GetData()
		assertSlicesEqual(t, expected, got)
	})

	t.Run("should pass through complete arrow key sequences", func(t *testing.T) {
		buffer, collector := newTestBuffer()
		upArrow := "\x1b[A"
		buffer.Process(upArrow)
		expected := []string{upArrow}
		got := collector.GetData()
		assertSlicesEqual(t, expected, got)
	})

	t.Run("should pass through complete function key sequences", func(t *testing.T) {
		buffer, collector := newTestBuffer()
		f1 := "\x1b[11~"
		buffer.Process(f1)
		expected := []string{f1}
		got := collector.GetData()
		assertSlicesEqual(t, expected, got)
	})

	t.Run("should pass through meta key sequences", func(t *testing.T) {
		buffer, collector := newTestBuffer()
		metaA := "\x1ba"
		buffer.Process(metaA)
		expected := []string{metaA}
		got := collector.GetData()
		assertSlicesEqual(t, expected, got)
	})

	t.Run("should pass through SS3 sequences", func(t *testing.T) {
		buffer, collector := newTestBuffer()
		ss3 := "\x1bOA"
		buffer.Process(ss3)
		expected := []string{ss3}
		got := collector.GetData()
		assertSlicesEqual(t, expected, got)
	})
}

// ------------------------------------------------------------
// Partial Escape Sequences
// ------------------------------------------------------------
func TestStdinBuffer_PartialEscapeSequences(t *testing.T) {
	t.Run("should buffer incomplete mouse SGR sequence", func(t *testing.T) {
		buffer, collector := newTestBuffer()
		buffer.Process("\x1b")
		assertSlicesEqual(t, []string{}, collector.GetData())
		if got := buffer.GetBuffer(); got != "\x1b" {
			t.Errorf("Expected buffer to be '\\x1b', got %q", got)
		}

		buffer.Process("[<35")
		assertSlicesEqual(t, []string{}, collector.GetData())
		if got := buffer.GetBuffer(); got != "\x1b[<35" {
			t.Errorf("Expected buffer to be '\\x1b[<35', got %q", got)
		}

		buffer.Process(";20;5m")
		expected := []string{"\x1b[<35;20;5m"}
		got := collector.GetData()
		assertSlicesEqual(t, expected, got)
		if got := buffer.GetBuffer(); got != "" {
			t.Errorf("Expected buffer to be empty, got %q", got)
		}
	})

	t.Run("should buffer incomplete CSI sequence", func(t *testing.T) {
		buffer, collector := newTestBuffer()
		buffer.Process("\x1b[")
		assertSlicesEqual(t, []string{}, collector.GetData())

		buffer.Process("1;")
		assertSlicesEqual(t, []string{}, collector.GetData())

		buffer.Process("5H")
		expected := []string{"\x1b[1;5H"}
		got := collector.GetData()
		assertSlicesEqual(t, expected, got)
	})

	t.Run("should buffer split across many chunks", func(t *testing.T) {
		buffer, collector := newTestBuffer()
		buffer.Process("\x1b")
		buffer.Process("[")
		buffer.Process("<")
		buffer.Process("3")
		buffer.Process("5")
		buffer.Process(";")
		buffer.Process("2")
		buffer.Process("0")
		buffer.Process(";")
		buffer.Process("5")
		buffer.Process("m")

		expected := []string{"\x1b[<35;20;5m"}
		got := collector.GetData()
		assertSlicesEqual(t, expected, got)
	})

	t.Run("should flush incomplete sequence after timeout", func(t *testing.T) {
		buffer, collector := newTestBuffer()
		buffer.Process("\x1b[<35")
		assertSlicesEqual(t, []string{}, collector.GetData())

		// Wait for timeout
		wait(t, 15)

		expected := []string{"\x1b[<35"}
		got := collector.GetData()
		assertSlicesEqual(t, expected, got)
	})
}

// ------------------------------------------------------------
// Mixed Content
// ------------------------------------------------------------
func TestStdinBuffer_MixedContent(t *testing.T) {
	t.Run("should handle characters followed by escape sequence", func(t *testing.T) {
		buffer, collector := newTestBuffer()
		buffer.Process("abc\x1b[A")
		expected := []string{"a", "b", "c", "\x1b[A"}
		got := collector.GetData()
		assertSlicesEqual(t, expected, got)
	})

	t.Run("should handle escape sequence followed by characters", func(t *testing.T) {
		buffer, collector := newTestBuffer()
		buffer.Process("\x1b[Aabc")
		expected := []string{"\x1b[A", "a", "b", "c"}
		got := collector.GetData()
		assertSlicesEqual(t, expected, got)
	})

	t.Run("should handle multiple complete sequences", func(t *testing.T) {
		buffer, collector := newTestBuffer()
		buffer.Process("\x1b[A\x1b[B\x1b[C")
		expected := []string{"\x1b[A", "\x1b[B", "\x1b[C"}
		got := collector.GetData()
		assertSlicesEqual(t, expected, got)
	})

	t.Run("should handle partial sequence with preceding characters", func(t *testing.T) {
		buffer, collector := newTestBuffer()
		buffer.Process("abc\x1b[<35")
		expected := []string{"a", "b", "c"}
		got := collector.GetData()
		assertSlicesEqual(t, expected, got)
		if got := buffer.GetBuffer(); got != "\x1b[<35" {
			t.Errorf("Expected buffer to be '\\x1b[<35', got %q", got)
		}

		buffer.Process(";20;5m")
		expected = []string{"a", "b", "c", "\x1b[<35;20;5m"}
		got = collector.GetData()
		assertSlicesEqual(t, expected, got)
	})
}

// ------------------------------------------------------------
// Kitty Keyboard Protocol
// ------------------------------------------------------------
func TestStdinBuffer_KittyKeyboardProtocol(t *testing.T) {
	t.Run("should handle Kitty CSI u press events", func(t *testing.T) {
		buffer, collector := newTestBuffer()
		// Press 'a' in Kitty protocol
		buffer.Process("\x1b[97u")
		expected := []string{"\x1b[97u"}
		got := collector.GetData()
		assertSlicesEqual(t, expected, got)
	})

	t.Run("should handle Kitty CSI u release events", func(t *testing.T) {
		buffer, collector := newTestBuffer()
		// Release 'a' in Kitty protocol
		buffer.Process("\x1b[97;1:3u")
		expected := []string{"\x1b[97;1:3u"}
		got := collector.GetData()
		assertSlicesEqual(t, expected, got)
	})

	t.Run("should handle batched Kitty press and release", func(t *testing.T) {
		buffer, collector := newTestBuffer()
		// Press 'a', release 'a' batched together (common over SSH)
		buffer.Process("\x1b[97u\x1b[97;1:3u")
		expected := []string{"\x1b[97u", "\x1b[97;1:3u"}
		got := collector.GetData()
		assertSlicesEqual(t, expected, got)
	})

	t.Run("should handle multiple batched Kitty events", func(t *testing.T) {
		buffer, collector := newTestBuffer()
		// Press 'a', release 'a', press 'b', release 'b'
		buffer.Process("\x1b[97u\x1b[97;1:3u\x1b[98u\x1b[98;1:3u")
		expected := []string{"\x1b[97u", "\x1b[97;1:3u", "\x1b[98u", "\x1b[98;1:3u"}
		got := collector.GetData()
		assertSlicesEqual(t, expected, got)
	})

	t.Run("should handle Kitty arrow keys with event type", func(t *testing.T) {
		buffer, collector := newTestBuffer()
		// Up arrow press with event type
		buffer.Process("\x1b[1;1:1A")
		expected := []string{"\x1b[1;1:1A"}
		got := collector.GetData()
		assertSlicesEqual(t, expected, got)
	})

	t.Run("should handle Kitty functional keys with event type", func(t *testing.T) {
		buffer, collector := newTestBuffer()
		// Delete key release
		buffer.Process("\x1b[3;1:3~")
		expected := []string{"\x1b[3;1:3~"}
		got := collector.GetData()
		assertSlicesEqual(t, expected, got)
	})

	t.Run("should handle plain characters mixed with Kitty sequences", func(t *testing.T) {
		buffer, collector := newTestBuffer()
		// Plain 'a' followed by Kitty release
		buffer.Process("a\x1b[97;1:3u")
		expected := []string{"a", "\x1b[97;1:3u"}
		got := collector.GetData()
		assertSlicesEqual(t, expected, got)
	})

	t.Run("should handle Kitty sequence followed by plain characters", func(t *testing.T) {
		buffer, collector := newTestBuffer()
		buffer.Process("\x1b[97ua")
		expected := []string{"\x1b[97u", "a"}
		got := collector.GetData()
		assertSlicesEqual(t, expected, got)
	})

	t.Run("should handle rapid typing simulation with Kitty protocol", func(t *testing.T) {
		buffer, collector := newTestBuffer()
		// Simulates typing "hi" quickly with releases interleaved
		buffer.Process("\x1b[104u\x1b[104;1:3u\x1b[105u\x1b[105;1:3u")
		expected := []string{"\x1b[104u", "\x1b[104;1:3u", "\x1b[105u", "\x1b[105;1:3u"}
		got := collector.GetData()
		assertSlicesEqual(t, expected, got)
	})
}

// ------------------------------------------------------------
// Mouse Events
// ------------------------------------------------------------
func TestStdinBuffer_MouseEvents(t *testing.T) {
	t.Run("should handle mouse press event", func(t *testing.T) {
		buffer, collector := newTestBuffer()
		buffer.Process("\x1b[<0;10;5M")
		expected := []string{"\x1b[<0;10;5M"}
		got := collector.GetData()
		assertSlicesEqual(t, expected, got)
	})

	t.Run("should handle mouse release event", func(t *testing.T) {
		buffer, collector := newTestBuffer()
		buffer.Process("\x1b[<0;10;5m")
		expected := []string{"\x1b[<0;10;5m"}
		got := collector.GetData()
		assertSlicesEqual(t, expected, got)
	})

	t.Run("should handle mouse move event", func(t *testing.T) {
		buffer, collector := newTestBuffer()
		buffer.Process("\x1b[<35;20;5m")
		expected := []string{"\x1b[<35;20;5m"}
		got := collector.GetData()
		assertSlicesEqual(t, expected, got)
	})

	t.Run("should handle split mouse events", func(t *testing.T) {
		buffer, collector := newTestBuffer()
		buffer.Process("\x1b[<3")
		buffer.Process("5;1")
		buffer.Process("5;")
		buffer.Process("10m")
		expected := []string{"\x1b[<35;15;10m"}
		got := collector.GetData()
		assertSlicesEqual(t, expected, got)
	})

	t.Run("should handle multiple mouse events", func(t *testing.T) {
		buffer, collector := newTestBuffer()
		buffer.Process("\x1b[<35;1;1m\x1b[<35;2;2m\x1b[<35;3;3m")
		expected := []string{"\x1b[<35;1;1m", "\x1b[<35;2;2m", "\x1b[<35;3;3m"}
		got := collector.GetData()
		assertSlicesEqual(t, expected, got)
	})

	t.Run("should handle old-style mouse sequence", func(t *testing.T) {
		buffer, collector := newTestBuffer()
		buffer.Process("\x1b[M abc")
		expected := []string{"\x1b[M ab", "c"}
		got := collector.GetData()
		assertSlicesEqual(t, expected, got)
	})

	t.Run("should buffer incomplete old-style mouse sequence", func(t *testing.T) {
		buffer, collector := newTestBuffer()
		buffer.Process("\x1b[M")
		if got := buffer.GetBuffer(); got != "\x1b[M" {
			t.Errorf("Expected buffer to be '\\x1b[M', got %q", got)
		}

		buffer.Process(" a")
		if got := buffer.GetBuffer(); got != "\x1b[M a" {
			t.Errorf("Expected buffer to be '\\x1b[M a', got %q", got)
		}

		buffer.Process("b")
		expected := []string{"\x1b[M ab"}
		got := collector.GetData()
		assertSlicesEqual(t, expected, got)
	})
}

// ------------------------------------------------------------
// Edge Cases
// ------------------------------------------------------------
func TestStdinBuffer_EdgeCases(t *testing.T) {
	t.Run("should handle empty input", func(t *testing.T) {
		buffer, collector := newTestBuffer()
		buffer.Process("")
		// Empty string emits an empty data event
		expected := []string{""}
		got := collector.GetData()
		assertSlicesEqual(t, expected, got)
	})

	t.Run("should handle lone escape character with timeout", func(t *testing.T) {
		buffer, collector := newTestBuffer()
		buffer.Process("\x1b")
		assertSlicesEqual(t, []string{}, collector.GetData())

		// After timeout, should emit
		wait(t, 15)
		expected := []string{"\x1b"}
		got := collector.GetData()
		assertSlicesEqual(t, expected, got)
	})

	t.Run("should handle lone escape character with explicit flush", func(t *testing.T) {
		buffer, collector := newTestBuffer()
		buffer.Process("\x1b")
		assertSlicesEqual(t, []string{}, collector.GetData())

		flushed := buffer.Flush()
		expected := []string{"\x1b"}
		assertSlicesEqual(t, expected, flushed)
	})

	t.Run("should handle buffer input", func(t *testing.T) {
		buffer, collector := newTestBuffer()
		buffer.Process([]byte("\x1b[A"))
		expected := []string{"\x1b[A"}
		got := collector.GetData()
		assertSlicesEqual(t, expected, got)
	})

	t.Run("should handle very long sequences", func(t *testing.T) {
		buffer, collector := newTestBuffer()
		longSeq := "\x1b["
		for range 50 {
			longSeq += "1;"
		}
		longSeq += "H"
		buffer.Process(longSeq)
		expected := []string{longSeq}
		got := collector.GetData()
		assertSlicesEqual(t, expected, got)
	})
}

// ------------------------------------------------------------
// Flush
// ------------------------------------------------------------
func TestStdinBuffer_Flush(t *testing.T) {
	t.Run("should flush incomplete sequences", func(t *testing.T) {
		buffer, _ := newTestBuffer()
		buffer.Process("\x1b[<35")
		flushed := buffer.Flush()
		expectedFlush := []string{"\x1b[<35"}
		assertSlicesEqual(t, expectedFlush, flushed)
		if got := buffer.GetBuffer(); got != "" {
			t.Errorf("Expected buffer to be empty, got %q", got)
		}
	})

	t.Run("should return empty array if nothing to flush", func(t *testing.T) {
		buffer, _ := newTestBuffer()
		flushed := buffer.Flush()
		expected := []string{}
		assertSlicesEqual(t, expected, flushed)
	})

	t.Run("should emit flushed data via timeout", func(t *testing.T) {
		buffer, collector := newTestBuffer()
		buffer.Process("\x1b[<35")
		assertSlicesEqual(t, []string{}, collector.GetData())

		// Wait for timeout to flush
		wait(t, 15)

		expected := []string{"\x1b[<35"}
		got := collector.GetData()
		assertSlicesEqual(t, expected, got)
	})
}

// ------------------------------------------------------------
// Clear
// ------------------------------------------------------------
func TestStdinBuffer_Clear(t *testing.T) {
	t.Run("should clear buffered content without emitting", func(t *testing.T) {
		buffer, collector := newTestBuffer()
		buffer.Process("\x1b[<35")
		if got := buffer.GetBuffer(); got != "\x1b[<35" {
			t.Errorf("Expected buffer to be '\\x1b[<35', got %q", got)
		}

		buffer.Clear()
		if got := buffer.GetBuffer(); got != "" {
			t.Errorf("Expected buffer to be empty, got %q", got)
		}
		assertSlicesEqual(t, []string{}, collector.GetData())
	})
}

// ------------------------------------------------------------
// Bracketed Paste
// ------------------------------------------------------------
func TestStdinBuffer_BracketedPaste(t *testing.T) {
	t.Run("should emit paste event for complete bracketed paste", func(t *testing.T) {
		buffer, collector := newTestBuffer()
		pasteStart := "\x1b[200~"
		pasteEnd := "\x1b[201~"
		content := "hello world"

		buffer.Process(pasteStart + content + pasteEnd)

		expectedPaste := []string{"hello world"}
		gotPaste := collector.GetPastes()
		assertSlicesEqual(t, expectedPaste, gotPaste)
		assertSlicesEqual(t, []string{}, collector.GetData()) // No data events during paste
	})

	t.Run("should handle paste arriving in chunks", func(t *testing.T) {
		buffer, collector := newTestBuffer()
		buffer.Process("\x1b[200~")
		assertSlicesEqual(t, []string{}, collector.GetPastes())

		buffer.Process("hello ")
		assertSlicesEqual(t, []string{}, collector.GetPastes())

		buffer.Process("world\x1b[201~")
		expectedPaste := []string{"hello world"}
		gotPaste := collector.GetPastes()
		assertSlicesEqual(t, expectedPaste, gotPaste)
		assertSlicesEqual(t, []string{}, collector.GetData())
	})

	t.Run("should handle slow paste chunks beyond timeout", func(t *testing.T) {
		buffer, collector := newTestBuffer()
		buffer.Process("\x1b[200~")
		wait(t, 15)

		buffer.Process("hello ")
		wait(t, 15)
		assertSlicesEqual(t, []string{}, collector.GetPastes())
		assertSlicesEqual(t, []string{}, collector.GetData())

		buffer.Process("world")
		wait(t, 15)
		assertSlicesEqual(t, []string{}, collector.GetPastes())
		assertSlicesEqual(t, []string{}, collector.GetData())

		buffer.Process("\x1b[201~")
		expectedPaste := []string{"hello world"}
		gotPaste := collector.GetPastes()
		assertSlicesEqual(t, expectedPaste, gotPaste)
		assertSlicesEqual(t, []string{}, collector.GetData())
	})

	t.Run("should handle paste with input before and after", func(t *testing.T) {
		buffer, collector := newTestBuffer()
		buffer.Process("a")
		buffer.Process("\x1b[200~pasted\x1b[201~")
		buffer.Process("b")

		expectedData := []string{"a", "b"}
		gotData := collector.GetData()
		assertSlicesEqual(t, expectedData, gotData)
		expectedPaste := []string{"pasted"}
		gotPaste := collector.GetPastes()
		assertSlicesEqual(t, expectedPaste, gotPaste)
	})

	t.Run("should handle paste with newlines", func(t *testing.T) {
		buffer, collector := newTestBuffer()
		buffer.Process("\x1b[200~line1\nline2\nline3\x1b[201~")

		expectedPaste := []string{"line1\nline2\nline3"}
		gotPaste := collector.GetPastes()
		assertSlicesEqual(t, expectedPaste, gotPaste)
		assertSlicesEqual(t, []string{}, collector.GetData())
	})

	t.Run("should handle paste with unicode", func(t *testing.T) {
		buffer, collector := newTestBuffer()
		buffer.Process("\x1b[200~Hello 世界 🎉\x1b[201~")

		expectedPaste := []string{"Hello 世界 🎉"}
		gotPaste := collector.GetPastes()
		assertSlicesEqual(t, expectedPaste, gotPaste)
		assertSlicesEqual(t, []string{}, collector.GetData())
	})
}

// ------------------------------------------------------------
// Destroy
// ------------------------------------------------------------
func TestStdinBuffer_Destroy(t *testing.T) {
	t.Run("should clear buffer on destroy", func(t *testing.T) {
		buffer, _ := newTestBuffer()
		buffer.Process("\x1b[<35")
		if got := buffer.GetBuffer(); got != "\x1b[<35" {
			t.Errorf("Expected buffer to be '\\x1b[<35', got %q", got)
		}

		buffer.Destroy()
		if got := buffer.GetBuffer(); got != "" {
			t.Errorf("Expected buffer to be empty, got %q", got)
		}
	})

	t.Run("should clear pending timeouts on destroy", func(t *testing.T) {
		buffer, collector := newTestBuffer()
		buffer.Process("\x1b[<35")
		buffer.Destroy()

		// Wait longer than timeout
		wait(t, 15)

		// Should not have emitted anything
		assertSlicesEqual(t, []string{}, collector.GetData())
	})
}

// ------------------------------------------------------------
// Helper
// ------------------------------------------------------------
func assertSlicesEqual(t *testing.T, expected, got []string) {
	t.Helper()
	if len(expected) != len(got) {
		t.Errorf("Length mismatch: expected %d items, got %d", len(expected), len(got))
		t.Errorf("Expected: %v", expected)
		t.Errorf("Got: %v", got)
		return
	}
	for i, exp := range expected {
		if i >= len(got) {
			t.Errorf("Missing item at index %d: expected %q", i, exp)
			break
		}
		if got[i] != exp {
			t.Errorf("Item %d mismatch: expected %q, got %q", i, exp, got[i])
		}
	}
}
