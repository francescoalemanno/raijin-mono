package oneshot

import (
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestFilterListDeleteRequiresTwoCtrlXPresses(t *testing.T) {
	m := newFilterList(
		"TEST",
		[]int{42},
		0,
		5,
		func(item int) string { return fmt.Sprint(item) },
		func(item int, selected bool) string { return fmt.Sprint(item) },
	)
	m.deletableFn = func(item int) bool { return true }

	model, _ := m.Update(tea.KeyMsg{Type: tea.KeyCtrlX})
	afterFirst := model.(filterList[int])
	if !afterFirst.pendingDelete {
		t.Fatalf("expected pending delete after first ctrl+x")
	}
	if afterFirst.deleted != nil {
		t.Fatalf("did not expect deleted item after first ctrl+x")
	}

	model, _ = afterFirst.Update(tea.KeyMsg{Type: tea.KeyCtrlX})
	afterSecond := model.(filterList[int])
	if afterSecond.deleted == nil || *afterSecond.deleted != 42 {
		t.Fatalf("expected deleted item=42 after second ctrl+x, got %#v", afterSecond.deleted)
	}
	if afterSecond.pendingDelete {
		t.Fatalf("expected pending delete to reset after confirmation")
	}
}

func TestFilterListDeleteSkippedWhenItemIsNotDeletable(t *testing.T) {
	m := newFilterList(
		"TEST",
		[]int{7},
		0,
		5,
		func(item int) string { return fmt.Sprint(item) },
		func(item int, selected bool) string { return fmt.Sprint(item) },
	)
	m.deletableFn = func(item int) bool { return false }

	model, _ := m.Update(tea.KeyMsg{Type: tea.KeyCtrlX})
	after := model.(filterList[int])
	if after.pendingDelete {
		t.Fatalf("did not expect pending delete when deletableFn=false")
	}
	if after.deleted != nil {
		t.Fatalf("did not expect deleted item when deletableFn=false")
	}
}

func TestFilterListNonCtrlXClearsDeleteConfirmation(t *testing.T) {
	m := newFilterList(
		"TEST",
		[]int{1},
		0,
		5,
		func(item int) string { return fmt.Sprint(item) },
		func(item int, selected bool) string { return fmt.Sprint(item) },
	)
	m.deletableFn = func(item int) bool { return true }

	model, _ := m.Update(tea.KeyMsg{Type: tea.KeyCtrlX})
	pending := model.(filterList[int])
	if !pending.pendingDelete {
		t.Fatalf("expected pending delete before reset")
	}

	model, _ = pending.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
	reset := model.(filterList[int])
	if reset.pendingDelete {
		t.Fatalf("expected pending delete to clear on non-ctrl+x key")
	}
}

func TestFilterListFullPageUsesViewportHeight(t *testing.T) {
	items := make([]int, 30)
	for i := range items {
		items[i] = i
	}
	m := newFilterList(
		"TEST",
		items,
		0,
		0, // full-page mode
		func(item int) string { return fmt.Sprint(item) },
		func(item int, selected bool) string {
			prefix := "  "
			if selected {
				prefix = "→ "
			}
			return prefix + fmt.Sprint(item)
		},
	)

	model, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 12})
	updated := model.(filterList[int])
	view := updated.View()

	// title + filter + 8 items + count + footer = 12 lines
	lineCount := len(strings.Split(strings.TrimRight(view, "\n"), "\n"))
	if lineCount != 12 {
		t.Fatalf("line count=%d, want 12; view:\n%s", lineCount, view)
	}
	if !strings.Contains(view, "8/30 shown") {
		t.Fatalf("expected viewport-sized list with count line, got:\n%s", view)
	}
}
