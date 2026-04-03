package oneshot

import (
	"os"
	"strconv"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/francescoalemanno/raijin-mono/internal/persist"
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

func TestReplStartupPromptSubmitsOnce(t *testing.T) {
	m := replModel{
		historyIndex:  -1,
		startupPrompt: "hello world",
	}

	if cmd := m.Init(); cmd != nil {
		t.Fatal("Init() should not submit startup prompt immediately")
	}

	updated, cmd := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	if cmd == nil {
		t.Fatal("WindowSizeMsg should queue startup submit")
	}

	queued := updated.(replModel)
	if !queued.startupQueued {
		t.Fatal("startupQueued should be set after initial window size")
	}

	updated, cmd = queued.Update(replStartupMsg{})
	if cmd == nil {
		t.Fatal("startup submit returned nil command")
	}

	next := updated.(replModel)
	if len(next.history) != 1 || next.history[0] != "hello world" {
		t.Fatalf("history = %#v, want startup prompt", next.history)
	}
	if next.startupPrompt != "" {
		t.Fatalf("startupPrompt = %q, want cleared", next.startupPrompt)
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

func TestReplSoftWrapping(t *testing.T) {
	promptWidth := 8
	m := replModel{
		width:        promptWidth + 5, // 5 characters available for text
		historyIndex: -1,
	}
	m.setEditorState("1234567890", 0)

	view := m.View().Content
	lines := strings.Split(view, "\n")

	// Expected lines:
	// "raijin❯ <cursor>12345"
	// "   ...❯ 67890"

	if len(lines) != 2 {
		t.Fatalf("expected 2 lines, got %d:\n%q", len(lines), view)
	}
	if !strings.HasPrefix(lines[0], replPrompt) {
		t.Fatalf("line 0 should have replPrompt, got %q", lines[0])
	}
	if !strings.HasPrefix(lines[1], replContinuationPrompt) {
		t.Fatalf("line 1 should have continuationPrompt, got %q", lines[1])
	}
	if !strings.Contains(lines[0], "\x1b[7m") { // cursor should be in first line
		t.Fatalf("line 0 should contain cursor, but didn't:\n%q", lines[0])
	}

	// Now move cursor to second visual line
	m.setEditorState("1234567890", 6)
	view = m.View().Content
	lines = strings.Split(view, "\n")
	if !strings.Contains(lines[1], "\x1b[7m") {
		t.Fatalf("line 1 should contain cursor at position 6, but didn't:\n%q", lines[1])
	}
}

func TestReplSanitizeBaseArgsRemovesNewFlag(t *testing.T) {
	got := replSanitizeBaseArgs([]string{"--new", "--profile-dir", "profiles"})
	if len(got) != 2 || got[0] != "--profile-dir" || got[1] != "profiles" {
		t.Fatalf("replSanitizeBaseArgs() = %#v", got)
	}
}

func TestReplSanitizeBaseArgsRemovesNewFlagValueForms(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want []string
	}{
		{
			name: "equals form",
			args: []string{"--new=hello world", "--profile-dir", "profiles"},
			want: []string{"--profile-dir", "profiles"},
		},
		{
			name: "separate prompt value",
			args: []string{"-new", "hello world", "--profile-dir", "profiles"},
			want: []string{"--profile-dir", "profiles"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := replSanitizeBaseArgs(tc.args)
			if strings.Join(got, "\x00") != strings.Join(tc.want, "\x00") {
				t.Fatalf("replSanitizeBaseArgs() = %#v, want %#v", got, tc.want)
			}
		})
	}
}

func TestReplCommandEnvIncludesBinding(t *testing.T) {
	env := replCommandEnv(replBinding{key: "repl-test", ownerPID: 1234})
	joined := strings.Join(env, "\n")
	if !strings.Contains(joined, persist.SessionBindingKeyEnv+"=repl-test") {
		t.Fatalf("expected binding key in env, got %q", joined)
	}
	if !strings.Contains(joined, persist.SessionBindingOwnerPIDEnv+"=1234") {
		t.Fatalf("expected binding owner pid in env, got %q", joined)
	}
}

func TestReplEnsureBindingEnvUsesExistingShellStyleVars(t *testing.T) {
	t.Setenv(persist.SessionBindingKeyEnv, "shell-zsh-42-7")
	t.Setenv(persist.SessionBindingOwnerPIDEnv, "42")

	binding, err := replEnsureBindingEnv()
	if err != nil {
		t.Fatalf("replEnsureBindingEnv: %v", err)
	}
	if binding.key != "shell-zsh-42-7" || binding.ownerPID != 42 {
		t.Fatalf("binding = %#v", binding)
	}
}

func TestReplEnsureBindingEnvSetsProcessEnvWhenMissing(t *testing.T) {
	t.Setenv(persist.SessionBindingKeyEnv, "")
	t.Setenv(persist.SessionBindingOwnerPIDEnv, "")

	binding, err := replEnsureBindingEnv()
	if err != nil {
		t.Fatalf("replEnsureBindingEnv: %v", err)
	}
	if binding.key == "" || binding.ownerPID <= 0 {
		t.Fatalf("binding = %#v", binding)
	}
	if got := strings.TrimSpace(os.Getenv(persist.SessionBindingKeyEnv)); got != binding.key {
		t.Fatalf("binding key env = %q, want %q", got, binding.key)
	}
	if got := strings.TrimSpace(os.Getenv(persist.SessionBindingOwnerPIDEnv)); got != strconv.Itoa(binding.ownerPID) {
		t.Fatalf("binding owner env = %q, want %q", got, strconv.Itoa(binding.ownerPID))
	}
}
