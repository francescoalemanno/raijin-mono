package oneshot

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	modelconfig "github.com/francescoalemanno/raijin-mono/internal/config"
	"github.com/francescoalemanno/raijin-mono/internal/persist"
	libagent "github.com/francescoalemanno/raijin-mono/libagent"
)

func TestRunNewPersistsSessionWithoutMessages(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	if err := Run(Options{}, "/new"); err != nil {
		t.Fatalf("Run(/new): %v", err)
	}

	sessionsDir := filepath.Join(home, ".config", "raijin", "sessions")
	matches, err := filepath.Glob(filepath.Join(sessionsDir, "*.jsonl"))
	if err != nil {
		t.Fatalf("Glob sessions: %v", err)
	}
	if len(matches) != 1 {
		entries, _ := os.ReadDir(sessionsDir)
		names := make([]string, 0, len(entries))
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Fatalf("expected exactly one persisted session file, got %d (%v)", len(matches), names)
	}

	data, err := os.ReadFile(matches[0])
	if err != nil {
		t.Fatalf("ReadFile(%s): %v", matches[0], err)
	}
	if !strings.Contains(string(data), `"typ":"session"`) {
		t.Fatalf("expected session header entry in %s, got %q", matches[0], string(data))
	}
}

func TestRunStatusPrintsModelAndContextFill(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	opts := Options{
		ModelCfg: libagent.ModelConfig{
			Provider:      "openai",
			Model:         "gpt-test",
			ThinkingLevel: libagent.ThinkingLevelHigh,
			ContextWindow: 10000,
		},
	}

	out := captureStdout(t, func() {
		if err := Run(opts, "/status"); err != nil {
			t.Fatalf("Run(/status): %v", err)
		}
	})

	if !strings.Contains(out, "Model: openai/gpt-test") {
		t.Fatalf("expected model line in output, got %q", out)
	}
	if !strings.Contains(out, "Reasoning: high") {
		t.Fatalf("expected reasoning line in output, got %q", out)
	}
	if !strings.Contains(out, "Context: 24.0%") {
		t.Fatalf("expected context percentage in output, got %q", out)
	}
}

func TestRunReasoningUpdatesDefaultModelLevel(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	store, err := modelconfig.LoadModelStore()
	if err != nil {
		t.Fatalf("LoadModelStore: %v", err)
	}
	model := libagent.ModelConfig{
		Name:          "openai/gpt-test",
		Provider:      "openai",
		Model:         "gpt-test",
		ThinkingLevel: libagent.ThinkingLevelLow,
	}
	if err := store.Add(model); err != nil {
		t.Fatalf("Add model: %v", err)
	}
	if err := store.SetDefault(model.Name); err != nil {
		t.Fatalf("SetDefault: %v", err)
	}

	opts := Options{Store: store}
	if err := Run(opts, "/reasoning high"); err != nil {
		t.Fatalf("Run(/reasoning high): %v", err)
	}

	reloaded, err := modelconfig.LoadModelStore()
	if err != nil {
		t.Fatalf("Reload model store: %v", err)
	}
	got, ok := reloaded.GetDefault()
	if !ok {
		t.Fatalf("expected default model after reasoning update")
	}
	if got.ThinkingLevel != libagent.ThinkingLevelHigh {
		t.Fatalf("ThinkingLevel = %q, want %q", got.ThinkingLevel, libagent.ThinkingLevelHigh)
	}
}

func TestRunReasoningRejectsInvalidLevel(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	store, err := modelconfig.LoadModelStore()
	if err != nil {
		t.Fatalf("LoadModelStore: %v", err)
	}
	model := libagent.ModelConfig{
		Name:          "openai/gpt-test",
		Provider:      "openai",
		Model:         "gpt-test",
		ThinkingLevel: libagent.ThinkingLevelMedium,
	}
	if err := store.Add(model); err != nil {
		t.Fatalf("Add model: %v", err)
	}
	if err := store.SetDefault(model.Name); err != nil {
		t.Fatalf("SetDefault: %v", err)
	}

	err = Run(Options{Store: store}, "/reasoning turbo")
	if err == nil {
		t.Fatalf("expected invalid reasoning level error")
	}
	if !strings.Contains(err.Error(), "invalid reasoning level") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunHistoryNoOutputYet(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	if err := Run(Options{}, "/new"); err != nil {
		t.Fatalf("Run(/new): %v", err)
	}

	out := captureStdout(t, func() {
		if err := Run(Options{}, "/history"); err != nil {
			t.Fatalf("Run(/history): %v", err)
		}
	})

	if got, want := out, "No session output yet\n"; got != want {
		t.Fatalf("history output = %q, want %q", got, want)
	}
}

func TestRunHistoryReplaysUserOnlyOutput(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	store, err := persist.OpenStore()
	if err != nil {
		t.Fatalf("OpenStore: %v", err)
	}
	sess, err := store.CreateEphemeral()
	if err != nil {
		t.Fatalf("CreateEphemeral: %v", err)
	}

	msgs := store.Messages()
	if _, err := msgs.Create(context.Background(), sess.ID, &libagent.UserMessage{
		Role:    "user",
		Content: "hello",
	}); err != nil {
		t.Fatalf("create user message: %v", err)
	}

	out := captureStdout(t, func() {
		if err := Run(Options{}, "/history"); err != nil {
			t.Fatalf("Run(/history): %v", err)
		}
	})

	want := renderUserPrefix() + "hello\n"
	if got := out; got != want {
		t.Fatalf("history output = %q, want %q", got, want)
	}
}

func TestRunHistoryReplaysAssistantOutput(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	store, err := persist.OpenStore()
	if err != nil {
		t.Fatalf("OpenStore: %v", err)
	}
	sess, err := store.CreateEphemeral()
	if err != nil {
		t.Fatalf("CreateEphemeral: %v", err)
	}

	msgs := store.Messages()
	if _, err := msgs.Create(context.Background(), sess.ID, &libagent.UserMessage{
		Role:    "user",
		Content: "hello",
	}); err != nil {
		t.Fatalf("create user message: %v", err)
	}
	first := libagent.NewAssistantMessage("first answer", "", nil, time.Now())
	first.Completed = true
	if _, err := msgs.Create(context.Background(), sess.ID, first); err != nil {
		t.Fatalf("create first assistant message: %v", err)
	}
	second := libagent.NewAssistantMessage("second answer", "thinking...", nil, time.Now())
	second.Completed = true
	if _, err := msgs.Create(context.Background(), sess.ID, second); err != nil {
		t.Fatalf("create second assistant message: %v", err)
	}

	out := captureStdout(t, func() {
		if err := Run(Options{}, "/history"); err != nil {
			t.Fatalf("Run(/history): %v", err)
		}
	})

	want := renderUserPrefix() + "hello\n" +
		"first answer\n" + thinkingMutedStyle.Render("thinking...") + "\nsecond answer\n"
	if got := out; got != want {
		t.Fatalf("history output = %q, want %q", got, want)
	}
}

func TestRunHistoryUsesStandardRendererMarkdownPath(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	store, err := persist.OpenStore()
	if err != nil {
		t.Fatalf("OpenStore: %v", err)
	}
	sess, err := store.CreateEphemeral()
	if err != nil {
		t.Fatalf("CreateEphemeral: %v", err)
	}

	msgs := store.Messages()
	if _, err := msgs.Create(context.Background(), sess.ID, &libagent.UserMessage{
		Role:    "user",
		Content: "hello",
	}); err != nil {
		t.Fatalf("create user message: %v", err)
	}
	reply := libagent.NewAssistantMessage("**bold**", "", nil, time.Now())
	reply.Completed = true
	if _, err := msgs.Create(context.Background(), sess.ID, reply); err != nil {
		t.Fatalf("create assistant message: %v", err)
	}

	out := captureStdout(t, func() {
		if err := Run(Options{}, "/history"); err != nil {
			t.Fatalf("Run(/history): %v", err)
		}
	})

	if !strings.Contains(out, "bold\n") {
		t.Fatalf("expected rendered markdown content, got %q", out)
	}
	if strings.Contains(out, "**bold**") {
		t.Fatalf("expected markdown markers to be rendered, got %q", out)
	}
}

func TestResolveEditorCommandPrefersEDITOR(t *testing.T) {
	seen := []string{}
	cmd, err := resolveEditorCommand(
		func(key string) string {
			if key == "EDITOR" {
				return `nvim -u NONE`
			}
			return ""
		},
		func(file string) (string, error) {
			seen = append(seen, file)
			if file == "nvim" {
				return "/usr/bin/nvim", nil
			}
			return "", os.ErrNotExist
		},
	)
	if err != nil {
		t.Fatalf("resolveEditorCommand: %v", err)
	}
	if cmd.path != "/usr/bin/nvim" {
		t.Fatalf("editor path = %q, want %q", cmd.path, "/usr/bin/nvim")
	}
	if !reflect.DeepEqual(cmd.args, []string{"-u", "NONE"}) {
		t.Fatalf("editor args = %#v, want %#v", cmd.args, []string{"-u", "NONE"})
	}
	if !reflect.DeepEqual(seen, []string{"nvim"}) {
		t.Fatalf("lookPath calls = %#v, want %#v", seen, []string{"nvim"})
	}
}

func TestResolveEditorCommandFallbackOrder(t *testing.T) {
	seen := []string{}
	cmd, err := resolveEditorCommand(
		func(string) string { return "" },
		func(file string) (string, error) {
			seen = append(seen, file)
			if file == "nvim" {
				return "/usr/bin/nvim", nil
			}
			return "", os.ErrNotExist
		},
	)
	if err != nil {
		t.Fatalf("resolveEditorCommand: %v", err)
	}
	if cmd.path != "/usr/bin/nvim" {
		t.Fatalf("editor path = %q, want %q", cmd.path, "/usr/bin/nvim")
	}
	if !reflect.DeepEqual(seen, []string{"micro", "nano", "nvim"}) {
		t.Fatalf("fallback search = %#v, want %#v", seen, []string{"micro", "nano", "nvim"})
	}
}

func TestHandleEditWithRunnerSendsSavedContent(t *testing.T) {
	dir := t.TempDir()
	editorPath := filepath.Join(dir, "fake-editor.sh")
	editorScript := "#!/bin/sh\ncat <<'EOF' > \"$1\"\nhello from editor\nEOF\n"
	if err := os.WriteFile(editorPath, []byte(editorScript), 0o755); err != nil {
		t.Fatalf("write fake editor: %v", err)
	}

	t.Setenv("EDITOR", editorPath)

	var (
		capturedPrompt string
		capturedForce  bool
	)
	err := handleEditWithRunner(Options{}, "", true, func(_ Options, prompt string, forceNew bool) error {
		capturedPrompt = prompt
		capturedForce = forceNew
		return nil
	})
	if err != nil {
		t.Fatalf("handleEditWithRunner: %v", err)
	}
	if capturedPrompt != "hello from editor\n" {
		t.Fatalf("captured prompt = %q, want %q", capturedPrompt, "hello from editor\n")
	}
	if !capturedForce {
		t.Fatalf("forceNew = false, want true")
	}
}

func TestHandleEditWithRunnerRejectsEmptyBuffer(t *testing.T) {
	dir := t.TempDir()
	editorPath := filepath.Join(dir, "fake-editor-empty.sh")
	editorScript := "#!/bin/sh\n: > \"$1\"\n"
	if err := os.WriteFile(editorPath, []byte(editorScript), 0o755); err != nil {
		t.Fatalf("write fake editor: %v", err)
	}

	t.Setenv("EDITOR", editorPath)

	err := handleEditWithRunner(Options{}, "", false, func(_ Options, _ string, _ bool) error {
		t.Fatalf("runner should not be called for empty content")
		return nil
	})
	if err == nil {
		t.Fatalf("expected error for empty editor buffer")
	}
	if !strings.Contains(err.Error(), "editor buffer is empty") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func captureStdout(t *testing.T, fn func()) string {
	t.Helper()

	orig := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	os.Stdout = w
	t.Cleanup(func() {
		os.Stdout = orig
		_ = r.Close()
	})

	fn()

	if err := w.Close(); err != nil {
		t.Fatalf("close write pipe: %v", err)
	}
	out, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("read output: %v", err)
	}
	return string(out)
}
