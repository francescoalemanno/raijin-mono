package test

import (
	"testing"

	"github.com/francescoalemanno/raijin-mono/libtui/pkg/utils"
	"github.com/stretchr/testify/assert"
)

func TestUndoStack_BasicOperations(t *testing.T) {
	s := utils.NewUndoStack[string]()

	// Initially empty
	assert.True(t, s.IsEmpty())
	assert.Equal(t, 0, s.Length())

	// Push and peek
	s.Push("first")
	assert.False(t, s.IsEmpty())
	assert.Equal(t, 1, s.Length())
	assert.Equal(t, "first", s.Peek())
	val, ok := s.PeekWithOK()
	assert.True(t, ok)
	assert.Equal(t, "first", val)

	// Push more
	s.Push("second")
	s.Push("third")
	assert.Equal(t, 3, s.Length())
	assert.Equal(t, "third", s.Peek())

	// Pop returns most recent
	popped := s.Pop()
	assert.Equal(t, "third", popped)
	assert.Equal(t, 2, s.Length())
	assert.Equal(t, "second", s.Peek())

	// Pop all remaining
	s.Pop()
	assert.Equal(t, 1, s.Length())
	s.Pop()
	assert.Equal(t, 0, s.Length())
	assert.True(t, s.IsEmpty())

	// Pop from empty returns zero value
	empty := s.Pop()
	assert.Equal(t, "", empty)
	empty, ok = s.PopWithOK()
	assert.Equal(t, "", empty)
	assert.False(t, ok)
}

func TestUndoStack_Clear(t *testing.T) {
	s := utils.NewUndoStack[string]()

	s.Push("first")
	s.Push("second")
	s.Push("third")
	assert.Equal(t, 3, s.Length())

	s.Clear()
	assert.Equal(t, 0, s.Length())
	assert.True(t, s.IsEmpty())

	// Pop after clear returns zero value
	empty := s.Pop()
	assert.Equal(t, "", empty)
}

func TestUndoStack_WithTypeAlias(t *testing.T) {
	type State struct {
		Value int
		Text  string
	}

	s := utils.NewUndoStack[State]()

	s.Push(State{Value: 1, Text: "one"})
	s.Push(State{Value: 2, Text: "two"})

	assert.Equal(t, 2, s.Length())

	top := s.Peek()
	assert.Equal(t, 2, top.Value)
	assert.Equal(t, "two", top.Text)

	popped := s.Pop()
	assert.Equal(t, 2, popped.Value)
	assert.Equal(t, 1, s.Length())
}

func TestUndoStack_IntegerStack(t *testing.T) {
	s := utils.NewUndoStack[int]()

	s.Push(10)
	s.Push(20)
	s.Push(30)

	assert.Equal(t, 30, s.Pop())
	assert.Equal(t, 20, s.Pop())
	assert.Equal(t, 10, s.Peek())
	assert.Equal(t, 10, s.Pop())

	// Empty pops return 0
	assert.Equal(t, 0, s.Pop())
	assert.True(t, s.IsEmpty())
}

func TestUndoStack_PeekWithOK(t *testing.T) {
	s := utils.NewUndoStack[string]()

	// Empty peek returns ok=false
	val, ok := s.PeekWithOK()
	assert.False(t, ok)
	assert.Equal(t, "", val)

	s.Push("test")
	val, ok = s.PeekWithOK()
	assert.True(t, ok)
	assert.Equal(t, "test", val)

	// Peek after pop
	_ = s.Pop()
	_, ok = s.PeekWithOK()
	assert.False(t, ok)
}

func TestUndoStack_PopWithOK(t *testing.T) {
	s := utils.NewUndoStack[int]()

	// Empty pop returns ok=false
	val, ok := s.PopWithOK()
	assert.False(t, ok)
	assert.Equal(t, 0, val)

	s.Push(42)
	val, ok = s.PopWithOK()
	assert.True(t, ok)
	assert.Equal(t, 42, val)
	assert.Equal(t, 0, s.Length())

	// Second pop after emptying
	_, ok = s.PopWithOK()
	assert.False(t, ok)
}
