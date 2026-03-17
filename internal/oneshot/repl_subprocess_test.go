package oneshot

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func TestReplStatusRefreshPrintsEvenWhenUnchanged(t *testing.T) {
	m := replModel{
		statusLoaded: true,
		status:       "test-model · medium",
		historyIndex: -1,
	}

	updated, cmd := m.Update(replStatusMsg{label: "test-model · medium"})
	if cmd == nil {
		t.Fatalf("status refresh returned nil command")
	}

	next, ok := updated.(replModel)
	if !ok {
		t.Fatalf("updated model type = %T, want replModel", updated)
	}
	if next.status != "test-model · medium" {
		t.Fatalf("status = %q, want unchanged status", next.status)
	}
}

func TestReplEmptySubmitRepeatsPrompt(t *testing.T) {
	m := replModel{
		statusLoaded: true,
		status:       "test-model · medium",
		historyIndex: -1,
	}

	view := m.View().Content
	if got := strings.Count(view, replPrompt); got != 1 {
		t.Fatalf("initial prompt count = %d, want 1", got)
	}
	if strings.Contains(view, "Raijin REPL") {
		t.Fatalf("view unexpectedly contains static banner: %q", view)
	}

	updated, cmd := m.submit()
	if cmd == nil {
		t.Fatalf("submit returned nil command for empty prompt")
	}

	next, ok := updated.(replModel)
	if !ok {
		t.Fatalf("updated model type = %T, want replModel", updated)
	}
	if len(next.history) != 0 {
		t.Fatalf("history len = %d, want 0", len(next.history))
	}
	if got := strings.Count(next.View().Content, replPrompt); got != 1 {
		t.Fatalf("prompt count after empty submit = %d, want 1", got)
	}
}

func TestReplEnterSubmitsAndClearsEditor(t *testing.T) {
	m := replModel{historyIndex: -1}
	m.setEditorState("hello world", len([]rune("hello world")))

	updated, cmd := m.submit()
	if cmd == nil {
		t.Fatal("submit returned nil command for non-empty prompt")
	}

	next := updated.(replModel)
	if next.editor.Value() != "" {
		t.Fatalf("editor value = %q, want empty after submit", next.editor.Value())
	}
	if len(next.history) != 1 || next.history[0] != "hello world" {
		t.Fatalf("history = %#v, want submitted prompt", next.history)
	}
}

func TestReplAltEnterInsertsNewline(t *testing.T) {
	m := replModel{historyIndex: -1}
	m.setEditorState("hello", len([]rune("hello")))

	updated, _ := m.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter, Mod: tea.ModAlt}))
	next := updated.(replModel)

	if next.editor.Value() != "hello\n" {
		t.Fatalf("editor value = %q, want %q", next.editor.Value(), "hello\n")
	}
	if !strings.Contains(next.View().Content, "\n"+replContinuationPrompt) {
		t.Fatalf("view = %q, want continuation prompt on second line", next.View().Content)
	}
}

func TestReplCtrlJInsertsNewline(t *testing.T) {
	m := replModel{historyIndex: -1}
	m.setEditorState("hello", len([]rune("hello")))

	updated, _ := m.Update(tea.KeyPressMsg(tea.Key{Code: 'j', Mod: tea.ModCtrl}))
	next := updated.(replModel)

	if next.editor.Value() != "hello\n" {
		t.Fatalf("editor value = %q, want %q", next.editor.Value(), "hello\n")
	}
}

func TestReplHistoryNavigationUsesUpDown(t *testing.T) {
	m := replModel{
		history:      []string{"first", "second"},
		historyIndex: -1,
	}
	m.setEditorState("draft", len([]rune("draft")))

	updated, _ := m.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyUp}))
	next := updated.(replModel)
	if next.editor.Value() != "second" {
		t.Fatalf("after first up value = %q, want %q", next.editor.Value(), "second")
	}

	updated, _ = next.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyUp}))
	next = updated.(replModel)
	if next.editor.Value() != "first" {
		t.Fatalf("after second up value = %q, want %q", next.editor.Value(), "first")
	}

	updated, _ = next.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyDown}))
	next = updated.(replModel)
	if next.editor.Value() != "second" {
		t.Fatalf("after down value = %q, want %q", next.editor.Value(), "second")
	}

	updated, _ = next.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyDown}))
	next = updated.(replModel)
	if next.editor.Value() != "draft" {
		t.Fatalf("after final down value = %q, want %q", next.editor.Value(), "draft")
	}
}

func TestReplSuperArrowsMoveToInputBounds(t *testing.T) {
	m := replModel{historyIndex: -1}
	value := "first line\nsecond line"
	m.setEditorState(value, len([]rune(value)))

	updated, _ := m.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyLeft, Mod: tea.ModSuper}))
	next := updated.(replModel)
	if next.editor.Line() != 0 || next.editor.Column() != 0 {
		t.Fatalf("after super+left cursor = (%d,%d), want (0,0)", next.editor.Line(), next.editor.Column())
	}

	updated, _ = next.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyRight, Mod: tea.ModSuper}))
	next = updated.(replModel)
	if got := replRunePosForLineColumn(next.editor.Value(), next.editor.Line(), next.editor.Column()); got != len([]rune(value)) {
		t.Fatalf("after super+right rune pos = %d, want %d", got, len([]rune(value)))
	}
}
