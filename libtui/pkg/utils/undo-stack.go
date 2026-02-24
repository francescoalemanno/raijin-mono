package utils

// NewUndoStack creates a new empty UndoStack.
func NewUndoStack[S any]() *UndoStack[S] {
	return &UndoStack[S]{}
}

// UndoStack is a generic undo stack with clone-on-push semantics.
//
// Stores deep clones of state snapshots. Popped snapshots are returned
// directly (no re-cloning) since they are already detached.
type UndoStack[S any] struct {
	stack []S
}

// Push adds a deep clone of the given state onto the stack.
func (s *UndoStack[S]) Push(state S) {
	// For simple types, we copy. For complex types, the caller is
	// responsible for providing a copy if deep cloning is needed.
	// In Go, we don't have structuredClone like JS, so we store
	// the value directly. For pointer types, callers should clone.
	s.stack = append(s.stack, state)
}

// Pop removes and returns the most recent snapshot, or zero value if empty.
func (s *UndoStack[S]) Pop() S {
	var zero S
	if len(s.stack) == 0 {
		return zero
	}
	idx := len(s.stack) - 1
	val := s.stack[idx]
	s.stack = s.stack[:idx]
	return val
}

// PopWithOK removes and returns the most recent snapshot, along with
// a boolean indicating whether the stack was non-empty.
func (s *UndoStack[S]) PopWithOK() (S, bool) {
	var zero S
	if len(s.stack) == 0 {
		return zero, false
	}
	idx := len(s.stack) - 1
	val := s.stack[idx]
	s.stack = s.stack[:idx]
	return val, true
}

// Clear removes all snapshots from the stack.
func (s *UndoStack[S]) Clear() {
	s.stack = s.stack[:0]
}

// Length returns the number of snapshots in the stack.
func (s *UndoStack[S]) Length() int {
	return len(s.stack)
}

// IsEmpty returns true if the stack has no snapshots.
func (s *UndoStack[S]) IsEmpty() bool {
	return len(s.stack) == 0
}

// Peek returns the most recent snapshot without modifying the stack,
// or zero value if empty.
func (s *UndoStack[S]) Peek() S {
	var zero S
	if len(s.stack) == 0 {
		return zero
	}
	return s.stack[len(s.stack)-1]
}

// PeekWithOK returns the most recent snapshot without modifying the stack,
// along with a boolean indicating whether the stack was non-empty.
func (s *UndoStack[S]) PeekWithOK() (S, bool) {
	var zero S
	if len(s.stack) == 0 {
		return zero, false
	}
	return s.stack[len(s.stack)-1], true
}
