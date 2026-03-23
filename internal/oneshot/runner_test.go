package oneshot

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/francescoalemanno/raijin-mono/internal/artifacts"
	modelconfig "github.com/francescoalemanno/raijin-mono/internal/config"
	"github.com/francescoalemanno/raijin-mono/internal/paths"
	"github.com/francescoalemanno/raijin-mono/internal/persist"
	libagent "github.com/francescoalemanno/raijin-mono/libagent"
)

func bindTestContext(t *testing.T) string {
	t.Helper()
	key := strings.ToLower(strings.ReplaceAll(t.Name(), "/", "-"))
	t.Setenv(persist.SessionBindingKeyEnv, key)
	t.Setenv(persist.SessionBindingOwnerPIDEnv, "4242")
	return key
}

func bindSession(t *testing.T, key string, store *persist.Store, sess persist.Session) {
	t.Helper()
	if err := store.SaveBinding(persist.Binding{
		Key:              key,
		SessionID:        sess.ID,
		OwnerPID:         4242,
		SessionCreatedAt: sess.CreatedAt,
		SessionUpdatedAt: sess.UpdatedAt,
	}); err != nil {
		t.Fatalf("SaveBinding: %v", err)
	}
}

func TestRunNewCreatesEphemeralBoundSessionWithoutPersistingMessages(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	key := bindTestContext(t)

	if err := Run(Options{}, "/new"); err != nil {
		t.Fatalf("Run(/new): %v", err)
	}

	sessionsDir := filepath.Join(home, ".config", "raijin", "sessions")
	matches, err := filepath.Glob(filepath.Join(sessionsDir, "*.jsonl"))
	if err != nil {
		t.Fatalf("Glob sessions: %v", err)
	}
	if len(matches) != 0 {
		entries, _ := os.ReadDir(sessionsDir)
		names := make([]string, 0, len(entries))
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Fatalf("expected no persisted session files, got %d (%v)", len(matches), names)
	}
	store, err := persist.OpenStore()
	if err != nil {
		t.Fatalf("OpenStore: %v", err)
	}
	binding, err := store.LoadBinding(key)
	if err != nil {
		t.Fatalf("LoadBinding: %v", err)
	}
	if binding.SessionID == "" {
		t.Fatalf("binding should have a session ID after /new: %#v", binding)
	}
}

func TestRunStatusPrintsModelAndContextFill(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	bindTestContext(t)

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

func TestRunStatusIgnoresAssistantUsageMetadata(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	key := bindTestContext(t)

	store, err := persist.OpenStore()
	if err != nil {
		t.Fatalf("OpenStore: %v", err)
	}
	sess, err := store.CreateEphemeral()
	if err != nil {
		t.Fatalf("CreateEphemeral: %v", err)
	}
	bindSession(t, key, store, sess)

	msg := &libagent.AssistantMessage{
		Role:      "assistant",
		Timestamp: time.Now(),
	}
	libagent.SetAssistantUsage(msg, 95_000, 15_000, 10_000)
	if _, err := store.Messages().Create(context.Background(), sess.ID, msg); err != nil {
		t.Fatalf("Create assistant: %v", err)
	}

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

	if !strings.Contains(out, "Context: 24.0%") {
		t.Fatalf("expected approximate context percentage to ignore usage metadata, got %q", out)
	}
}

func TestRunStatusDoesNotCreateEmptyBoundSession(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	bindTestContext(t)

	opts := Options{
		ModelCfg: libagent.ModelConfig{
			Provider:      "openai",
			Model:         "gpt-test",
			ThinkingLevel: libagent.ThinkingLevelHigh,
			ContextWindow: 10000,
		},
	}
	if err := Run(opts, "/status"); err != nil {
		t.Fatalf("Run(/status): %v", err)
	}

	sessionsDir := filepath.Join(home, ".config", "raijin", "sessions")
	sessionMatches, err := filepath.Glob(filepath.Join(sessionsDir, "*.jsonl"))
	if err != nil {
		t.Fatalf("Glob sessions: %v", err)
	}
	if len(sessionMatches) != 0 {
		t.Fatalf("expected no persisted sessions, got %v", sessionMatches)
	}

	bindingsDir := filepath.Join(home, ".config", "raijin", "bindings")
	bindingMatches, err := filepath.Glob(filepath.Join(bindingsDir, "*.json"))
	if err != nil {
		t.Fatalf("Glob bindings: %v", err)
	}
	if len(bindingMatches) != 0 {
		t.Fatalf("expected no persisted bindings, got %v", bindingMatches)
	}
}

func TestRunHelpIncludesPromptTemplates(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	if err := artifacts.Reload(); err != nil {
		t.Fatalf("artifacts.Reload: %v", err)
	}

	out := captureStdout(t, func() {
		if err := Run(Options{}, "/help"); err != nil {
			t.Fatalf("Run(/help): %v", err)
		}
	})

	if !strings.Contains(out, "Commands:\n") {
		t.Fatalf("expected commands section in /help output, got %q", out)
	}
	if !strings.Contains(out, "/retry") {
		t.Fatalf("expected /retry in /help output, got %q", out)
	}
	if !strings.Contains(out, "Prompt templates:\n") {
		t.Fatalf("expected templates section in /help output, got %q", out)
	}
	if !strings.Contains(out, "/init") {
		t.Fatalf("expected embedded /init template in /help output, got %q", out)
	}
	if !strings.Contains(out, "Skills:\n") {
		t.Fatalf("expected skills section in /help output, got %q", out)
	}
	if !strings.Contains(out, "+commit") {
		t.Fatalf("expected embedded +commit skill in /help output, got %q", out)
	}
	if !strings.Contains(out, "Subagents:\n") {
		t.Fatalf("expected subagents section in /help output, got %q", out)
	}
	if !strings.Contains(out, "%explorer") {
		t.Fatalf("expected embedded %%explorer subagent in /help output, got %q", out)
	}
	if !strings.Contains(out, "%oracle") {
		t.Fatalf("expected embedded %%oracle subagent in /help output, got %q", out)
	}
}

func TestRunPromptRequiresBoundContext(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv(persist.SessionBindingKeyEnv, "")

	err := Run(Options{}, "hello")
	if err == nil {
		t.Fatalf("expected unbound prompt to fail")
	}
	if !strings.Contains(err.Error(), "bound context") {
		t.Fatalf("unexpected error: %v", err)
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
	bindTestContext(t)

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
	key := bindTestContext(t)

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
	bindSession(t, key, store, sess)

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
	key := bindTestContext(t)

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
	bindSession(t, key, store, sess)

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
	key := bindTestContext(t)

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
	bindSession(t, key, store, sess)

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

func TestReplaySessionEventsDoesNotEnablePersistentSpinner(t *testing.T) {
	var stderr bytes.Buffer
	var stdout bytes.Buffer
	r := newRenderer(&stderr, &stdout, nil, true)

	msgs := []libagent.Message{
		&libagent.UserMessage{Role: "user", Content: "hello"},
		libagent.NewAssistantMessage("world", "", nil, time.Now()),
	}

	replayed := replaySessionEvents(r, msgs)
	if replayed != 2 {
		t.Fatalf("replayed messages = %d, want %d", replayed, 2)
	}
	if r.spinnerEnabled {
		t.Fatalf("expected replay renderer to keep persistent spinner disabled")
	}
	if got := stderr.String(); got != "" {
		t.Fatalf("expected no persistent spinner stderr output during history replay, got %q", got)
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

func retryTestAssistant(calls []libagent.ToolCallItem) *libagent.AssistantMessage {
	am := libagent.NewAssistantMessage("", "", calls, time.UnixMilli(1))
	am.Completed = true
	return am
}

func TestRunRetryContinuesFromSanitizedSessionState(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	key := bindTestContext(t)

	store, err := persist.OpenStore()
	if err != nil {
		t.Fatalf("OpenStore: %v", err)
	}
	sess, err := store.CreateEphemeral()
	if err != nil {
		t.Fatalf("CreateEphemeral: %v", err)
	}

	ctx := context.Background()
	msgs := store.Messages()
	if _, err := msgs.Create(ctx, sess.ID, &libagent.UserMessage{
		Role:    "user",
		Content: "start",
	}); err != nil {
		t.Fatalf("create user message: %v", err)
	}
	if _, err := msgs.Create(ctx, sess.ID, retryTestAssistant([]libagent.ToolCallItem{{
		ID:    "call-1",
		Name:  "read",
		Input: `{"path":"a.txt"}`,
	}})); err != nil {
		t.Fatalf("create assistant tool call: %v", err)
	}
	if _, err := msgs.Create(ctx, sess.ID, &libagent.ToolResultMessage{
		Role:       "toolResult",
		ToolCallID: "call-1",
		ToolName:   "read",
		Content:    "file contents",
	}); err != nil {
		t.Fatalf("create tool result: %v", err)
	}
	dangling, err := msgs.Create(ctx, sess.ID, retryTestAssistant([]libagent.ToolCallItem{{
		ID:    "call-2",
		Name:  "bash",
		Input: `{"command":"pwd"}`,
	}}))
	if err != nil {
		t.Fatalf("create dangling assistant tool call: %v", err)
	}
	bindSession(t, key, store, sess)

	opts := Options{
		RuntimeModel: libagent.RuntimeModel{
			Model: &libagent.StaticTextModel{Response: "done"},
			ModelCfg: libagent.ModelConfig{
				Provider: "mock",
				Model:    "mock",
			},
		},
		ModelCfg: libagent.ModelConfig{
			Provider: "mock",
			Model:    "mock",
		},
	}

	out := captureStdout(t, func() {
		if err := Run(opts, "/retry"); err != nil {
			t.Fatalf("Run(/retry): %v", err)
		}
	})
	if !strings.Contains(out, "done") {
		t.Fatalf("expected retry output, got %q", out)
	}

	reloaded, err := persist.OpenStore()
	if err != nil {
		t.Fatalf("OpenStore reload: %v", err)
	}
	if err := reloaded.OpenSession(sess.ID); err != nil {
		t.Fatalf("OpenSession reload: %v", err)
	}
	got, err := reloaded.Messages().List(ctx, sess.ID)
	if err != nil {
		t.Fatalf("List reload: %v", err)
	}
	if len(got) != 4 {
		t.Fatalf("messages after retry = %d, want 4", len(got))
	}
	if got[0].GetRole() != "user" || got[1].GetRole() != "assistant" || got[2].GetRole() != "toolResult" || got[3].GetRole() != "assistant" {
		t.Fatalf("unexpected role sequence after retry: %q, %q, %q, %q", got[0].GetRole(), got[1].GetRole(), got[2].GetRole(), got[3].GetRole())
	}
	if text := libagent.AssistantText(got[3].(*libagent.AssistantMessage)); text != "done" {
		t.Fatalf("final assistant text = %q, want %q", text, "done")
	}
	for _, msg := range got {
		if libagent.MessageID(msg) == libagent.MessageID(dangling) {
			t.Fatalf("dangling assistant tool-call should have been sanitized before retry")
		}
	}
}

func TestResolvePrompt_TemplateBeatsSubagentSlashName(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	project := t.TempDir()
	prev, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(project); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(prev)
	})

	if err := os.MkdirAll(filepath.Join(project, paths.ProjectPromptsDirRel), 0o755); err != nil {
		t.Fatalf("mkdir prompts: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(project, paths.ProjectSubagentsDirRel), 0o755); err != nil {
		t.Fatalf("mkdir subagents: %v", err)
	}
	if err := os.WriteFile(filepath.Join(project, paths.ProjectPromptsDirRel, "delegate.md"), []byte("template body"), 0o644); err != nil {
		t.Fatalf("write prompt: %v", err)
	}
	if err := os.WriteFile(filepath.Join(project, paths.ProjectSubagentsDirRel, "delegate.md"), []byte("---\ndescription: Delegate\n---\nsubagent body"), 0o644); err != nil {
		t.Fatalf("write subagent: %v", err)
	}
	if err := artifacts.Reload(); err != nil {
		t.Fatalf("artifacts.Reload: %v", err)
	}

	resolved, err := resolvePrompt("/delegate hello")
	if err != nil {
		t.Fatalf("resolvePrompt: %v", err)
	}
	if resolved.template != "delegate" {
		t.Fatalf("expected template resolution, got %#v", resolved)
	}
	if !strings.Contains(resolved.promptText, "template body") {
		t.Fatalf("expected template expansion, got %#v", resolved)
	}
}

func TestResolvePrompt_SubagentSyntaxPassesThroughAsPromptText(t *testing.T) {
	resolved, err := resolvePrompt("%explorer study read.go")
	if err != nil {
		t.Fatalf("resolvePrompt: %v", err)
	}
	if resolved.builtin != nil {
		t.Fatalf("did not expect builtin resolution, got %#v", resolved.builtin)
	}
	if resolved.template != "" {
		t.Fatalf("did not expect template resolution, got %q", resolved.template)
	}
	if resolved.promptText != "%explorer study read.go" {
		t.Fatalf("promptText = %q, want %%explorer syntax preserved", resolved.promptText)
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
