package utils

// PushOptions defines options for adding text to the kill ring.
type PushOptions struct {
	// Prepend - If accumulating, prepend (backward deletion) or append (forward deletion)
	Prepend bool
	// Accumulate - Merge with the most recent entry instead of creating a new one
	Accumulate bool
}

// NewKillRing creates a new empty KillRing.
func NewKillRing() *KillRing {
	return &KillRing{}
}

// KillRing is a ring buffer for Emacs-style kill/yank operations.
//
// Tracks killed (deleted) text entries. Consecutive kills can accumulate
// into a single entry. Supports yank (paste most recent) and yank-pop
// (cycle through older entries).
type KillRing struct {
	ring []string
}

// Push adds text to the kill ring.
//
// If accumulate is true and the ring is not empty, the text is merged
// with the most recent entry. Otherwise, a new entry is created.
func (k *KillRing) Push(text string, opts PushOptions) {
	if text == "" {
		return
	}

	if opts.Accumulate && len(k.ring) > 0 {
		// Merge with the most recent entry
		last := k.ring[len(k.ring)-1]
		if opts.Prepend {
			// Backward deletion: prepend to existing text
			k.ring[len(k.ring)-1] = text + last
		} else {
			// Forward deletion: append to existing text
			k.ring[len(k.ring)-1] = last + text
		}
	} else {
		// Create a new entry
		k.ring = append(k.ring, text)
	}
}

// Peek returns the most recent entry without modifying the ring,
// or empty string if empty.
func (k *KillRing) Peek() string {
	if len(k.ring) == 0 {
		return ""
	}
	return k.ring[len(k.ring)-1]
}

// PeekWithOK returns the most recent entry without modifying the ring,
// along with a boolean indicating whether the ring was non-empty.
func (k *KillRing) PeekWithOK() (string, bool) {
	if len(k.ring) == 0 {
		return "", false
	}
	return k.ring[len(k.ring)-1], true
}

// Rotate moves the last entry to the front (for yank-pop cycling).
func (k *KillRing) Rotate() {
	if len(k.ring) > 1 {
		// Remove last element
		last := k.ring[len(k.ring)-1]
		// Shrink slice
		k.ring = k.ring[:len(k.ring)-1]
		// Prepend to front
		k.ring = append([]string{last}, k.ring...)
	}
}

// Clear removes all entries from the kill ring.
func (k *KillRing) Clear() {
	k.ring = k.ring[:0]
}

// Length returns the number of entries in the kill ring.
func (k *KillRing) Length() int {
	return len(k.ring)
}

// IsEmpty returns true if the kill ring has no entries.
func (k *KillRing) IsEmpty() bool {
	return len(k.ring) == 0
}

// GetAll returns all entries in the ring, from newest to oldest.
func (k *KillRing) GetAll() []string {
	// Return a copy to prevent modification of internal state
	result := make([]string, len(k.ring))
	copy(result, k.ring)
	return result
}

// Pop removes and returns the most recent entry, or empty string if empty.
// Note: This is not typically used in Emacs-style kill/yank (peek + rotate is the pattern).
func (k *KillRing) Pop() string {
	if len(k.ring) == 0 {
		return ""
	}
	idx := len(k.ring) - 1
	val := k.ring[idx]
	k.ring = k.ring[:idx]
	return val
}

// Yank returns the most recent entry (for yank operation)
func (k *KillRing) Yank() string {
	return k.Peek()
}

// YankPop cycles to the next entry and returns it (for yank-pop operation)
func (k *KillRing) YankPop() string {
	k.Rotate()
	return k.Peek()
}
