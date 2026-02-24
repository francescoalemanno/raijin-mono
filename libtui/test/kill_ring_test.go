package test

import (
	"testing"

	"github.com/francescoalemanno/raijin-mono/libtui/pkg/utils"
	"github.com/stretchr/testify/assert"
)

func TestKillRing_BasicOperations(t *testing.T) {
	k := utils.NewKillRing()

	// Initially empty
	assert.True(t, k.IsEmpty())
	assert.Equal(t, 0, k.Length())

	// Push first entry
	k.Push("hello", utils.PushOptions{})
	assert.False(t, k.IsEmpty())
	assert.Equal(t, 1, k.Length())
	assert.Equal(t, "hello", k.Peek())

	// Push second entry
	k.Push("world", utils.PushOptions{})
	assert.Equal(t, 2, k.Length())
	assert.Equal(t, "world", k.Peek())
}

func TestKillRing_PeekWithOK(t *testing.T) {
	k := utils.NewKillRing()

	// Empty peek returns ok=false
	val, ok := k.PeekWithOK()
	assert.False(t, ok)
	assert.Equal(t, "", val)

	k.Push("test", utils.PushOptions{})
	val, ok = k.PeekWithOK()
	assert.True(t, ok)
	assert.Equal(t, "test", val)
}

func TestKillRing_Clear(t *testing.T) {
	k := utils.NewKillRing()

	k.Push("first", utils.PushOptions{})
	k.Push("second", utils.PushOptions{})
	assert.Equal(t, 2, k.Length())

	k.Clear()
	assert.Equal(t, 0, k.Length())
	assert.True(t, k.IsEmpty())
	assert.Equal(t, "", k.Peek())
}

func TestKillRing_Rotate(t *testing.T) {
	k := utils.NewKillRing()

	// Populate ring
	k.Push("first", utils.PushOptions{})
	k.Push("second", utils.PushOptions{})
	k.Push("third", utils.PushOptions{})

	assert.Equal(t, 3, k.Length())
	assert.Equal(t, "third", k.Peek())

	// Rotate: last moves to front
	k.Rotate()
	assert.Equal(t, 3, k.Length())
	assert.Equal(t, "second", k.Peek())

	// Check order via GetAll
	entries := k.GetAll()
	assert.Equal(t, []string{"third", "first", "second"}, entries)

	// Another rotate
	k.Rotate()
	assert.Equal(t, "first", k.Peek())
	entries = k.GetAll()
	assert.Equal(t, []string{"second", "third", "first"}, entries)

	// Another rotation completes the cycle
	k.Rotate()
	assert.Equal(t, "third", k.Peek())
	entries = k.GetAll()
	assert.Equal(t, []string{"first", "second", "third"}, entries)
}

func TestKillRing_RotateSingleEntry(t *testing.T) {
	k := utils.NewKillRing()

	k.Push("single", utils.PushOptions{})
	assert.Equal(t, 1, k.Length())

	// Rotate with single entry should do nothing
	k.Rotate()
	assert.Equal(t, 1, k.Length())
	assert.Equal(t, "single", k.Peek())
}

func TestKillRing_RotateEmpty(t *testing.T) {
	k := utils.NewKillRing()

	// Rotate empty should do nothing
	k.Rotate()
	assert.True(t, k.IsEmpty())
	assert.Equal(t, 0, k.Length())
}

func TestKillRing_AccumulateAppend(t *testing.T) {
	k := utils.NewKillRing()

	// First kill
	k.Push("hello ", utils.PushOptions{Accumulate: false})
	assert.Equal(t, 1, k.Length())
	assert.Equal(t, "hello ", k.Peek())

	// Accumulate second kill (append - forward deletion)
	k.Push("world", utils.PushOptions{Accumulate: true, Prepend: false})
	assert.Equal(t, 1, k.Length()) // Still 1 entry
	assert.Equal(t, "hello world", k.Peek())

	// New non-accumulated kill creates new entry
	k.Push("new text", utils.PushOptions{Accumulate: false})
	assert.Equal(t, 2, k.Length())
	assert.Equal(t, "new text", k.Peek())
}

func TestKillRing_AccumulatePrepend(t *testing.T) {
	k := utils.NewKillRing()

	// First kill
	k.Push("world", utils.PushOptions{Accumulate: false})
	assert.Equal(t, 1, k.Length())
	assert.Equal(t, "world", k.Peek())

	// Accumulate second kill (prepend - backward deletion like Ctrl+U)
	k.Push("hello ", utils.PushOptions{Accumulate: true, Prepend: true})
	assert.Equal(t, 1, k.Length())
	assert.Equal(t, "hello world", k.Peek())
}

func TestKillRing_AccumulateMultiple(t *testing.T) {
	k := utils.NewKillRing()

	// Multiple accumulated kills
	k.Push("a", utils.PushOptions{Accumulate: false})
	k.Push("b", utils.PushOptions{Accumulate: true, Prepend: false})
	k.Push("c", utils.PushOptions{Accumulate: true, Prepend: false})
	k.Push("d", utils.PushOptions{Accumulate: true, Prepend: false})

	assert.Equal(t, 1, k.Length())
	assert.Equal(t, "abcd", k.Peek())

	// Now prepend
	k.Push("0", utils.PushOptions{Accumulate: true, Prepend: true})
	k.Push("-", utils.PushOptions{Accumulate: true, Prepend: true})

	assert.Equal(t, 1, k.Length())
	assert.Equal(t, "-0abcd", k.Peek())
}

func TestKillRing_PushEmptyString(t *testing.T) {
	k := utils.NewKillRing()

	// Pushing empty string should do nothing
	k.Push("", utils.PushOptions{})
	assert.Equal(t, 0, k.Length())
	assert.True(t, k.IsEmpty())

	// Also with accumulate
	k.Push("", utils.PushOptions{Accumulate: true})
	assert.Equal(t, 0, k.Length())
}

func TestKillRing_AccumulateEmpty(t *testing.T) {
	k := utils.NewKillRing()

	// First entry
	k.Push("first", utils.PushOptions{Accumulate: false})
	assert.Equal(t, 1, k.Length())

	// Accumulate empty should... do nothing (empty check is at start)
	k.Push("", utils.PushOptions{Accumulate: true})
	assert.Equal(t, 1, k.Length())
	assert.Equal(t, "first", k.Peek())
}

func TestKillRing_YankPopScenario(t *testing.T) {
	// Simulate a yank-pop (Alt+Y) workflow
	k := utils.NewKillRing()

	// Kill multiple entries (e.g., Ctrl+W multiple times)
	k.Push("first", utils.PushOptions{})
	k.Push("second", utils.PushOptions{})
	k.Push("third", utils.PushOptions{})

	// Yank (peek) - get most recent
	yanked := k.Peek()
	assert.Equal(t, "third", yanked)

	// Yank-pop (rotate) - cycle to previous
	k.Rotate()
	yanked = k.Peek()
	assert.Equal(t, "second", yanked)

	// Another yank-pop
	k.Rotate()
	yanked = k.Peek()
	assert.Equal(t, "first", yanked)

	// Another yank-pop wraps around
	k.Rotate()
	yanked = k.Peek()
	assert.Equal(t, "third", yanked)
}

func TestKillRing_GetAll(t *testing.T) {
	k := utils.NewKillRing()

	k.Push("first", utils.PushOptions{})
	k.Push("second", utils.PushOptions{})
	k.Push("third", utils.PushOptions{})

	entries := k.GetAll()
	assert.Equal(t, []string{"first", "second", "third"}, entries)

	// Modifying returned slice should not affect internal state
	entries[0] = "modified"

	// Get all again to verify original is unchanged
	entries2 := k.GetAll()
	assert.Equal(t, []string{"first", "second", "third"}, entries2)
}

func TestKillRing_Pop(t *testing.T) {
	k := utils.NewKillRing()

	k.Push("first", utils.PushOptions{})
	k.Push("second", utils.PushOptions{})

	popped := k.Pop()
	assert.Equal(t, "second", popped)
	assert.Equal(t, 1, k.Length())

	popped = k.Pop()
	assert.Equal(t, "first", popped)
	assert.Equal(t, 0, k.Length())

	// Pop empty returns empty string
	popped = k.Pop()
	assert.Equal(t, "", popped)
}
