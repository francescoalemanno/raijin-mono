package oneshot

import (
	"bytes"
	"context"
	"io"
	"iter"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"charm.land/fantasy"
	"github.com/francescoalemanno/raijin-mono/internal/agent"
	"github.com/francescoalemanno/raijin-mono/internal/artifacts"
	modelconfig "github.com/francescoalemanno/raijin-mono/internal/config"
	"github.com/francescoalemanno/raijin-mono/internal/paths"
	"github.com/francescoalemanno/raijin-mono/internal/persist"
	"github.com/francescoalemanno/raijin-mono/internal/ralph"
	"github.com/francescoalemanno/raijin-mono/internal/session"
	"github.com/francescoalemanno/raijin-mono/internal/shellinit"
	libagent "github.com/francescoalemanno/raijin-mono/libagent"
)

func writeRalphSpecPair(t *testing.T, specPath, spec, progress string) ralph.SpecPair {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(specPath), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	spec = strings.TrimSpace(spec) + "\n"
	if err := os.WriteFile(specPath, []byte(spec), 0o644); err != nil {
		t.Fatalf("write spec doc: %v", err)
	}
	progressPath := strings.TrimSuffix(specPath, filepath.Ext(specPath)) + ".progress.txt"
	if strings.HasPrefix(filepath.Base(specPath), "spec-") && strings.HasSuffix(filepath.Base(specPath), ".md") {
		slug := strings.TrimSuffix(strings.TrimPrefix(filepath.Base(specPath), "spec-"), ".md")
		progressPath = filepath.Join(filepath.Dir(specPath), "progress-"+slug+".txt")
	}
	if progress != "" {
		if err := os.WriteFile(progressPath, []byte(progress), 0o644); err != nil {
			t.Fatalf("write progress doc: %v", err)
		}
	}
	return ralph.SpecPair{
		SpecPath:     specPath,
		ProgressPath: progressPath,
		Slug:         strings.TrimSuffix(strings.TrimPrefix(filepath.Base(specPath), "spec-"), ".md"),
	}
}

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

type scriptedStreamModel struct {
	calls   int
	respFns []func(call int) fantasy.StreamResponse
}

func newScriptedStreamModel(fns ...func(call int) fantasy.StreamResponse) *scriptedStreamModel {
	return &scriptedStreamModel{respFns: fns}
}

func (m *scriptedStreamModel) Stream(_ context.Context, _ fantasy.Call) (fantasy.StreamResponse, error) {
	idx := m.calls
	m.calls++
	fn := m.respFns[min(idx, len(m.respFns)-1)]
	return fn(idx), nil
}

func (m *scriptedStreamModel) Generate(context.Context, fantasy.Call) (*fantasy.Response, error) {
	return nil, nil
}

func (m *scriptedStreamModel) GenerateObject(context.Context, fantasy.ObjectCall) (*fantasy.ObjectResponse, error) {
	return nil, nil
}

func (m *scriptedStreamModel) StreamObject(context.Context, fantasy.ObjectCall) (fantasy.ObjectStreamResponse, error) {
	return nil, nil
}

func (m *scriptedStreamModel) Provider() string { return "mock" }
func (m *scriptedStreamModel) Model() string    { return "mock" }

func scriptedTextResponse(text string) func(int) fantasy.StreamResponse {
	return func(_ int) fantasy.StreamResponse {
		return iter.Seq[fantasy.StreamPart](func(yield func(fantasy.StreamPart) bool) {
			if !yield(fantasy.StreamPart{Type: fantasy.StreamPartTypeTextStart, ID: "t1"}) {
				return
			}
			if !yield(fantasy.StreamPart{Type: fantasy.StreamPartTypeTextDelta, ID: "t1", Delta: text}) {
				return
			}
			if !yield(fantasy.StreamPart{Type: fantasy.StreamPartTypeTextEnd, ID: "t1"}) {
				return
			}
			yield(fantasy.StreamPart{Type: fantasy.StreamPartTypeFinish, FinishReason: fantasy.FinishReasonStop})
		})
	}
}

func scriptedToolCallResponse(toolCallID, toolName, input string) func(int) fantasy.StreamResponse {
	return func(_ int) fantasy.StreamResponse {
		return iter.Seq[fantasy.StreamPart](func(yield func(fantasy.StreamPart) bool) {
			if !yield(fantasy.StreamPart{
				Type:          fantasy.StreamPartTypeToolCall,
				ID:            toolCallID,
				ToolCallName:  toolName,
				ToolCallInput: input,
			}) {
				return
			}
			yield(fantasy.StreamPart{Type: fantasy.StreamPartTypeFinish, FinishReason: fantasy.FinishReasonToolCalls})
		})
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
	if !strings.Contains(out, "Max images: 20 (default)") {
		t.Fatalf("expected max-images line in output, got %q", out)
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
	if !strings.Contains(out, "/plan") {
		t.Fatalf("expected /plan in /help output, got %q", out)
	}
	if strings.Contains(out, "/start-plan") || strings.Contains(out, "/read-plan") {
		t.Fatalf("expected old Ralph commands to be absent from /help output, got %q", out)
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

func TestRunPlanBuiltinDoesNotRequireBoundContext(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv(persist.SessionBindingKeyEnv, "")
	t.Setenv(persist.SessionBindingOwnerPIDEnv, "")
	repo := t.TempDir()
	t.Chdir(repo)
	editorPath := filepath.Join(repo, "fake-editor-plan.sh")
	editorScript := "#!/bin/sh\ncat <<'EOF' > \"$1\"\ndesign the loop\nEOF\n"
	if err := os.WriteFile(editorPath, []byte(editorScript), 0o755); err != nil {
		t.Fatalf("write fake editor: %v", err)
	}
	t.Setenv("EDITOR", editorPath)

	origRunRalph := runRalph
	origRootPicker := runPlanRootPicker
	t.Cleanup(func() {
		runRalph = origRunRalph
		runPlanRootPicker = origRootPicker
	})

	runPlanRootPicker = func(bool) (planRootAction, bool, error) {
		return planRootActionCreate, true, nil
	}

	called := false
	runRalph = func(_ context.Context, opts ralph.Options) error {
		called = true
		if opts.Mode != ralph.ModePlan {
			t.Fatalf("Mode = %q, want %q", opts.Mode, ralph.ModePlan)
		}
		if opts.PlanningRequest != "design the loop" {
			t.Fatalf("PlanningRequest = %q, want %q", opts.PlanningRequest, "design the loop")
		}
		if !opts.ResetPlan {
			t.Fatalf("ResetPlan = false, want true for fresh spec path")
		}
		if opts.SpecPath == "" {
			t.Fatalf("SpecPath = empty, want allocated spec path")
		}
		writeRalphSpecPair(t, opts.SpecPath, "# Goal\n\ndesign the loop\n\n# User Specification\n\nKeep it clear.\n\n# Plan\n\n1. First step.\n", "")
		return nil
	}

	if err := Run(Options{}, "/plan"); err != nil {
		t.Fatalf("Run(/plan): %v", err)
	}
	if !called {
		t.Fatalf("expected Ralph runner to be called")
	}
}

func TestHandlePlanUsesEditorWhenRequestMissing(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	editorPath := filepath.Join(dir, "fake-editor-plan.sh")
	editorScript := "#!/bin/sh\ncat <<'EOF' > \"$1\"\ndesign the loop from editor\nEOF\n"
	if err := os.WriteFile(editorPath, []byte(editorScript), 0o755); err != nil {
		t.Fatalf("write fake editor: %v", err)
	}
	t.Setenv("EDITOR", editorPath)

	origRunRalph := runRalph
	origRootPicker := runPlanRootPicker
	t.Cleanup(func() {
		runRalph = origRunRalph
		runPlanRootPicker = origRootPicker
	})

	runPlanRootPicker = func(hasSpecs bool) (planRootAction, bool, error) {
		if hasSpecs {
			t.Fatalf("hasSpecs = true, want false")
		}
		return planRootActionCreate, true, nil
	}

	called := false
	runRalph = func(_ context.Context, opts ralph.Options) error {
		called = true
		if opts.PlanningRequest != "design the loop from editor" {
			t.Fatalf("PlanningRequest = %q", opts.PlanningRequest)
		}
		writeRalphSpecPair(t, opts.SpecPath, "# Goal\n\ndesign the loop from editor\n\n# User Specification\n\nKeep it clear.\n\n# Plan\n\n1. First step.\n", "")
		return nil
	}

	if err := handlePlan(""); err != nil {
		t.Fatalf("handlePlan(\"\"): %v", err)
	}
	if !called {
		t.Fatalf("expected Ralph runner to be called")
	}
}

func TestHandlePlanEmptyEditorPromptIsNoOp(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	editorPath := filepath.Join(dir, "fake-editor-empty-plan.sh")
	editorScript := "#!/bin/sh\n: > \"$1\"\n"
	if err := os.WriteFile(editorPath, []byte(editorScript), 0o755); err != nil {
		t.Fatalf("write fake editor: %v", err)
	}
	t.Setenv("EDITOR", editorPath)

	origRunRalph := runRalph
	origRootPicker := runPlanRootPicker
	t.Cleanup(func() {
		runRalph = origRunRalph
		runPlanRootPicker = origRootPicker
	})
	runPlanRootPicker = func(bool) (planRootAction, bool, error) {
		return planRootActionCreate, true, nil
	}
	runRalph = func(_ context.Context, _ ralph.Options) error {
		t.Fatalf("runRalph should not be called for empty editor prompt")
		return nil
	}

	stderr := captureStderr(t, func() {
		if err := handlePlan(""); err != nil {
			t.Fatalf("handlePlan(\"\"): %v", err)
		}
	})

	if !strings.Contains(stderr, "Plan prompt was empty; nothing changed") {
		t.Fatalf("expected empty-plan warning, got %q", stderr)
	}
}

func TestHandlePlanRootContinueOpensScopedMenu(t *testing.T) {
	repo := t.TempDir()
	t.Chdir(repo)
	first := writeRalphSpecPair(t, filepath.Join(repo, ".raijin", "ralph", "spec-otter-thread-sage.md"), "# Goal\n\nfirst goal\n\n# User Specification\n\nA.\n\n# Plan\n\n1. First.\n", "")
	second := writeRalphSpecPair(t, filepath.Join(repo, ".raijin", "ralph", "spec-fox-align-cedar.md"), "# Goal\n\nsecond goal\n\n# User Specification\n\nB.\n\n# Plan\n\n1. Second.\n", "picked progress\n")

	origRunRalph := runRalph
	origRootPicker := runPlanRootPicker
	origScopedPicker := runPlanScopedActionPicker
	origSpecPicker := runPlanSpecPicker
	t.Cleanup(func() {
		runRalph = origRunRalph
		runPlanRootPicker = origRootPicker
		runPlanScopedActionPicker = origScopedPicker
		runPlanSpecPicker = origSpecPicker
	})

	runPlanRootPicker = func(hasSpecs bool) (planRootAction, bool, error) {
		if !hasSpecs {
			t.Fatalf("hasSpecs = false, want true")
		}
		return planRootActionContinue, true, nil
	}
	runPlanSpecPicker = func(_ context.Context, pairs []ralph.SpecPair, purpose planSpecPickerPurpose) (ralph.SpecPair, bool, error) {
		if len(pairs) != 2 {
			t.Fatalf("pairs len = %d, want 2", len(pairs))
		}
		if purpose != planSpecPickerPurposeContinue {
			t.Fatalf("purpose = %q, want continue", purpose)
		}
		return second, true, nil
	}
	runPlanScopedActionPicker = func(pair ralph.SpecPair, status ralph.PlanningStatus, initialAction planScopedAction) (planScopedAction, bool, error) {
		if pair.SpecPath != second.SpecPath {
			t.Fatalf("SpecPath = %q, want %q", pair.SpecPath, second.SpecPath)
		}
		if initialAction != planScopedActionRun {
			t.Fatalf("initialAction = %q, want run", initialAction)
		}
		if status.State != ralph.PlanningStatePlanned {
			t.Fatalf("state = %q, want planned", status.State)
		}
		return planScopedActionReview, true, nil
	}
	runRalph = func(_ context.Context, _ ralph.Options) error {
		t.Fatalf("runRalph should not be called for review action")
		return nil
	}

	out := captureStdout(t, func() {
		if err := handlePlan(""); err != nil {
			t.Fatalf("handlePlan(root continue): %v", err)
		}
	})
	if !strings.Contains(out, "second goal") || !strings.Contains(out, "picked progress") {
		t.Fatalf("expected selected spec review output, got %q", out)
	}
	if strings.Contains(out, "first goal") || first.SpecPath == second.SpecPath {
		t.Fatalf("expected second spec to be selected, got %q", out)
	}
}

func TestHandlePlanRootEditOpensEditorAndReturnsToScopedMenu(t *testing.T) {
	repo := t.TempDir()
	t.Chdir(repo)
	pair := writeRalphSpecPair(t, filepath.Join(repo, ".raijin", "ralph", "spec-fox-align-cedar.md"), "# Goal\n\nsecond goal\n\n# User Specification\n\nB.\n\n# Plan\n\n1. Second.\n", "")

	origRunRalph := runRalph
	origRootPicker := runPlanRootPicker
	origScopedPicker := runPlanScopedActionPicker
	origSpecPicker := runPlanSpecPicker
	origEditPlanSpec := editPlanSpec
	t.Cleanup(func() {
		runRalph = origRunRalph
		runPlanRootPicker = origRootPicker
		runPlanScopedActionPicker = origScopedPicker
		runPlanSpecPicker = origSpecPicker
		editPlanSpec = origEditPlanSpec
	})

	runPlanRootPicker = func(hasSpecs bool) (planRootAction, bool, error) {
		if !hasSpecs {
			t.Fatalf("hasSpecs = false, want true")
		}
		return planRootActionEdit, true, nil
	}
	runPlanSpecPicker = func(_ context.Context, pairs []ralph.SpecPair, purpose planSpecPickerPurpose) (ralph.SpecPair, bool, error) {
		if len(pairs) != 1 {
			t.Fatalf("pairs len = %d, want 1", len(pairs))
		}
		if purpose != planSpecPickerPurposeEdit {
			t.Fatalf("purpose = %q, want edit", purpose)
		}
		return pair, true, nil
	}
	edited := false
	editPlanSpec = func(path string) error {
		edited = true
		if path != pair.SpecPath {
			t.Fatalf("path = %q, want %q", path, pair.SpecPath)
		}
		return nil
	}
	runPlanScopedActionPicker = func(got ralph.SpecPair, _ ralph.PlanningStatus, initialAction planScopedAction) (planScopedAction, bool, error) {
		if got.SpecPath != pair.SpecPath {
			t.Fatalf("SpecPath = %q, want %q", got.SpecPath, pair.SpecPath)
		}
		if initialAction != planScopedActionRun {
			t.Fatalf("initialAction = %q, want run", initialAction)
		}
		return planScopedActionClose, true, nil
	}
	runRalph = func(_ context.Context, _ ralph.Options) error {
		t.Fatalf("runRalph should not be called for manual edit flow")
		return nil
	}

	if err := handlePlan(""); err != nil {
		t.Fatalf("handlePlan(root edit): %v", err)
	}
	if !edited {
		t.Fatalf("expected manual edit to be invoked")
	}
}

func TestHandlePlanExplicitSlugScopesToSpec(t *testing.T) {
	repo := t.TempDir()
	t.Chdir(repo)
	writeRalphSpecPair(t, filepath.Join(repo, ".raijin", "ralph", "spec-otter-thread-sage.md"), "# Goal\n\nfirst goal\n\n# User Specification\n\nA.\n\n# Plan\n\n1. First.\n", "")
	second := writeRalphSpecPair(t, filepath.Join(repo, ".raijin", "ralph", "spec-fox-align-cedar.md"), "# Goal\n\nsecond goal\n\n# User Specification\n\nB.\n\n# Plan\n\n1. Second.\n", "picked progress\n")

	origRunRalph := runRalph
	origRootPicker := runPlanRootPicker
	origSpecPicker := runPlanSpecPicker
	origScopedPicker := runPlanScopedActionPicker
	t.Cleanup(func() {
		runRalph = origRunRalph
		runPlanRootPicker = origRootPicker
		runPlanSpecPicker = origSpecPicker
		runPlanScopedActionPicker = origScopedPicker
	})

	runPlanRootPicker = func(bool) (planRootAction, bool, error) {
		t.Fatalf("root picker should not be called for explicit slug")
		return "", false, nil
	}
	runPlanSpecPicker = func(_ context.Context, _ []ralph.SpecPair, _ planSpecPickerPurpose) (ralph.SpecPair, bool, error) {
		t.Fatalf("spec picker should not be called for explicit slug")
		return ralph.SpecPair{}, false, nil
	}
	runPlanScopedActionPicker = func(pair ralph.SpecPair, _ ralph.PlanningStatus, initialAction planScopedAction) (planScopedAction, bool, error) {
		if pair.SpecPath != second.SpecPath {
			t.Fatalf("SpecPath = %q, want %q", pair.SpecPath, second.SpecPath)
		}
		if initialAction != planScopedActionRun {
			t.Fatalf("initialAction = %q, want run", initialAction)
		}
		return planScopedActionRun, true, nil
	}

	called := false
	runRalph = func(_ context.Context, opts ralph.Options) error {
		called = true
		if opts.Mode != ralph.ModeAuto {
			t.Fatalf("Mode = %q, want auto", opts.Mode)
		}
		if opts.SpecPath != second.SpecPath {
			t.Fatalf("SpecPath = %q, want %q", opts.SpecPath, second.SpecPath)
		}
		return nil
	}

	if err := handlePlan(second.Slug); err != nil {
		t.Fatalf("handlePlan(explicit slug): %v", err)
	}
	if !called {
		t.Fatalf("expected explicit slug to run the scoped spec")
	}
}

func TestHandlePlanExplicitSlugAllowsScopedEdit(t *testing.T) {
	repo := t.TempDir()
	t.Chdir(repo)
	pair := writeRalphSpecPair(t, filepath.Join(repo, ".raijin", "ralph", "spec-fox-align-cedar.md"), "# Goal\n\nsecond goal\n\n# User Specification\n\nB.\n\n# Plan\n\n1. Second.\n", "")

	origRunRalph := runRalph
	origScopedPicker := runPlanScopedActionPicker
	origEditPlanSpec := editPlanSpec
	t.Cleanup(func() {
		runRalph = origRunRalph
		runPlanScopedActionPicker = origScopedPicker
		editPlanSpec = origEditPlanSpec
	})

	pickerCalls := 0
	runPlanScopedActionPicker = func(got ralph.SpecPair, _ ralph.PlanningStatus, initialAction planScopedAction) (planScopedAction, bool, error) {
		if got.SpecPath != pair.SpecPath {
			t.Fatalf("SpecPath = %q, want %q", got.SpecPath, pair.SpecPath)
		}
		if initialAction != planScopedActionRun {
			t.Fatalf("initialAction = %q, want run", initialAction)
		}
		pickerCalls++
		if pickerCalls == 1 {
			return planScopedActionEdit, true, nil
		}
		return planScopedActionClose, true, nil
	}

	edited := false
	editPlanSpec = func(path string) error {
		edited = true
		if path != pair.SpecPath {
			t.Fatalf("path = %q, want %q", path, pair.SpecPath)
		}
		return nil
	}
	runRalph = func(_ context.Context, _ ralph.Options) error {
		t.Fatalf("runRalph should not be called for scoped edit flow")
		return nil
	}

	if err := handlePlan(pair.Slug); err != nil {
		t.Fatalf("handlePlan(explicit slug edit): %v", err)
	}
	if !edited {
		t.Fatalf("expected explicit slug flow to invoke manual edit")
	}
	if pickerCalls != 1 {
		t.Fatalf("pickerCalls = %d, want 1", pickerCalls)
	}
}

func TestHandlePlanExplicitPathScopesToExactSpec(t *testing.T) {
	repo := t.TempDir()
	t.Chdir(repo)
	pair := writeRalphSpecPair(t, filepath.Join(repo, ".raijin", "ralph", "spec-fox-align-cedar.md"), "# Goal\n\nsecond goal\n\n# User Specification\n\nB.\n\n# Plan\n\n1. Second.\n", "")

	origRunRalph := runRalph
	origScopedPicker := runPlanScopedActionPicker
	t.Cleanup(func() {
		runRalph = origRunRalph
		runPlanScopedActionPicker = origScopedPicker
	})

	runPlanScopedActionPicker = func(got ralph.SpecPair, _ ralph.PlanningStatus, _ planScopedAction) (planScopedAction, bool, error) {
		if got.SpecPath != pair.SpecPath {
			t.Fatalf("SpecPath = %q, want %q", got.SpecPath, pair.SpecPath)
		}
		return planScopedActionRun, true, nil
	}

	called := false
	runRalph = func(_ context.Context, opts ralph.Options) error {
		called = true
		if opts.SpecPath != pair.SpecPath {
			t.Fatalf("SpecPath = %q, want %q", opts.SpecPath, pair.SpecPath)
		}
		return nil
	}

	if err := handlePlan(pair.SpecPath); err != nil {
		t.Fatalf("handlePlan(explicit path): %v", err)
	}
	if !called {
		t.Fatalf("expected explicit path to run the scoped spec")
	}
}

func TestHandlePlanFreeTextIsRejected(t *testing.T) {
	repo := t.TempDir()
	t.Chdir(repo)

	err := handlePlan("revise this plan")
	if err == nil {
		t.Fatalf("expected one-off /plan prompt to be rejected")
	}
	if !strings.Contains(err.Error(), "one-off /plan prompts are no longer supported") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestHandlePlanCustomMarkdownPathWithoutDotSlashOpensScopedFlow(t *testing.T) {
	repo := t.TempDir()
	t.Chdir(repo)
	customSpec := filepath.Join(repo, "custom.md")

	origRunRalph := runRalph
	origScopedPicker := runPlanScopedActionPicker
	t.Cleanup(func() {
		runRalph = origRunRalph
		runPlanScopedActionPicker = origScopedPicker
	})

	runPlanScopedActionPicker = func(got ralph.SpecPair, _ ralph.PlanningStatus, initialAction planScopedAction) (planScopedAction, bool, error) {
		if got.SpecPath != customSpec {
			t.Fatalf("SpecPath = %q, want %q", got.SpecPath, customSpec)
		}
		if initialAction != planScopedActionRevise {
			t.Fatalf("initialAction = %q, want %q", initialAction, planScopedActionRevise)
		}
		return planScopedActionClose, true, nil
	}
	runRalph = func(_ context.Context, _ ralph.Options) error {
		t.Fatalf("runRalph should not be called when scoped menu closes immediately")
		return nil
	}

	if err := handlePlan("custom.md"); err != nil {
		t.Fatalf("handlePlan(custom.md): %v", err)
	}
}

func TestRunPlanningQuestionPromptSelectsOptionLabel(t *testing.T) {
	origPick := pickPlanningQuestionChoice
	origRead := readPlanningInlineAnswer
	t.Cleanup(func() {
		pickPlanningQuestionChoice = origPick
		readPlanningInlineAnswer = origRead
	})

	pickPlanningQuestionChoice = func(question string, items []fzfPickerItem) (string, error) {
		if question != "Which baseline matters?" {
			t.Fatalf("question = %q", question)
		}
		if len(items) != 2 {
			t.Fatalf("len(items) = %d, want 2", len(items))
		}
		if items[0].label != "CLI-first" || items[1].label != "Library-first" {
			t.Fatalf("unexpected items = %#v", items)
		}
		return items[1].key, nil
	}
	readPlanningInlineAnswer = func(string) (string, error) {
		t.Fatalf("inline answer should not be used for preset option")
		return "", nil
	}

	answer, err := runPlanningQuestionPrompt(context.Background(), ralph.PlanningQuestionPrompt{
		Question: "Which baseline matters?",
		Options: []ralph.PlanningQuestionOption{
			{Label: "CLI-first", Description: "Prefer the CLI."},
			{Label: "Library-first", Description: "Prefer embedding."},
		},
	})
	if err != nil {
		t.Fatalf("runPlanningQuestionPrompt(select): %v", err)
	}
	if answer != "Library-first" {
		t.Fatalf("answer = %q, want %q", answer, "Library-first")
	}
}

func TestRunPlanningQuestionPromptOtherUsesInlineInput(t *testing.T) {
	origPick := pickPlanningQuestionChoice
	origRead := readPlanningInlineAnswer
	t.Cleanup(func() {
		pickPlanningQuestionChoice = origPick
		readPlanningInlineAnswer = origRead
	})

	pickPlanningQuestionChoice = func(question string, items []fzfPickerItem) (string, error) {
		if question != "What environment matters most?" {
			t.Fatalf("question = %q", question)
		}
		if len(items) != 1 || items[0].label != "CLI" {
			t.Fatalf("unexpected items = %#v", items)
		}
		return planningQuestionOtherKey, nil
	}
	readPlanningInlineAnswer = func(question string) (string, error) {
		if question != "What environment matters most?" {
			t.Fatalf("inline question = %q", question)
		}
		return "Air-gapped on-prem", nil
	}

	answer, err := runPlanningQuestionPrompt(context.Background(), ralph.PlanningQuestionPrompt{
		Question: "What environment matters most?",
		Options: []ralph.PlanningQuestionOption{
			{Label: "CLI", Description: "Command-line users first."},
		},
	})
	if err != nil {
		t.Fatalf("runPlanningQuestionPrompt(other): %v", err)
	}
	if answer != "Air-gapped on-prem" {
		t.Fatalf("answer = %q, want %q", answer, "Air-gapped on-prem")
	}
}

func TestDefaultPickPlanningQuestionChoiceRequiresTTY(t *testing.T) {
	_, err := defaultPickPlanningQuestionChoice("Which baseline matters?", []fzfPickerItem{{
		key:     "option-0",
		label:   "CLI-first",
		preview: "Prefer the CLI.",
	}})
	if err == nil {
		t.Fatalf("expected TTY requirement error")
	}
	if !strings.Contains(err.Error(), "interactive clarification required") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestDefaultPickPlanningQuestionChoiceUsesQuestionHeaderAndFullscreen(t *testing.T) {
	origRunner := runEmbeddedFZFWithOptions
	origCanUse := canUseEmbeddedFZFForTest
	t.Cleanup(func() {
		runEmbeddedFZFWithOptions = origRunner
		canUseEmbeddedFZFForTest = origCanUse
	})

	runEmbeddedFZFWithOptions = func(mode, query string, stdin io.Reader, cfg shellinit.RunFZFOptions) (shellinit.RunFZFResult, error) {
		if mode != "default" {
			t.Fatalf("mode = %q, want default", mode)
		}
		if query != "" {
			t.Fatalf("query = %q, want empty", query)
		}
		if !cfg.UseFullscreen {
			t.Fatalf("expected fullscreen question picker")
		}
		if cfg.Header != "Which baseline matters?" {
			t.Fatalf("cfg.Header = %q", cfg.Header)
		}
		data, err := io.ReadAll(stdin)
		if err != nil {
			t.Fatalf("ReadAll(stdin): %v", err)
		}
		text := string(data)
		if !strings.Contains(text, "CLI-first") || !strings.Contains(text, "Other") {
			t.Fatalf("stdin missing picker items: %q", text)
		}
		return shellinit.RunFZFResult{Code: 0, Selected: []string{"CLI-first\tPrefer the CLI."}}, nil
	}
	canUseEmbeddedFZFForTest = func() bool { return true }

	chosen, err := defaultPickPlanningQuestionChoice("Which baseline matters?", []fzfPickerItem{{
		key:     "option-0",
		label:   "CLI-first",
		preview: "Prefer the CLI.",
	}})
	if err != nil {
		t.Fatalf("defaultPickPlanningQuestionChoice: %v", err)
	}
	if chosen != "option-0" {
		t.Fatalf("chosen = %q, want option-0", chosen)
	}
}

func TestDefaultRunPlanRootPickerUsesFullscreenFZF(t *testing.T) {
	origRunner := runEmbeddedFZFWithOptions
	origCanUse := canUseEmbeddedFZFForTest
	t.Cleanup(func() {
		runEmbeddedFZFWithOptions = origRunner
		canUseEmbeddedFZFForTest = origCanUse
	})

	runEmbeddedFZFWithOptions = func(mode, query string, stdin io.Reader, cfg shellinit.RunFZFOptions) (shellinit.RunFZFResult, error) {
		if mode != "default" {
			t.Fatalf("mode = %q, want default", mode)
		}
		if !cfg.UseFullscreen {
			t.Fatalf("expected Ralph root picker to use fullscreen FZF")
		}
		if cfg.Header != "Ralph Dashboard" {
			t.Fatalf("cfg.Header = %q, want Ralph Dashboard", cfg.Header)
		}
		data, err := io.ReadAll(stdin)
		if err != nil {
			t.Fatalf("ReadAll(stdin): %v", err)
		}
		text := string(data)
		if !strings.Contains(text, "Create new spec") {
			t.Fatalf("stdin missing Ralph root menu items: %q", text)
		}
		selected := ""
		for _, line := range strings.Split(text, "\n") {
			line = strings.TrimSpace(line)
			if strings.Contains(line, "Create new spec") {
				selected = line
				break
			}
		}
		if selected == "" {
			t.Fatalf("could not find create menu item in %q", text)
		}
		return shellinit.RunFZFResult{Code: 0, Selected: []string{selected}}, nil
	}
	canUseEmbeddedFZFForTest = func() bool { return true }

	action, ok, err := defaultRunPlanRootPicker(true)
	if err != nil {
		t.Fatalf("defaultRunPlanRootPicker: %v", err)
	}
	if !ok {
		t.Fatalf("expected selection")
	}
	if action != planRootActionCreate {
		t.Fatalf("action = %q, want %q", action, planRootActionCreate)
	}
}

func TestDefaultRunPlanSpecPickerUsesFullscreenFZF(t *testing.T) {
	repo := t.TempDir()
	t.Chdir(repo)
	pair := writeRalphSpecPair(t, filepath.Join(repo, ".raijin", "ralph", "spec-fox-align-cedar.md"), "# Goal\n\nsecond goal\n\n# User Specification\n\nB.\n\n# Plan\n\n1. Second.\n", "picked progress\n")

	origRunner := runEmbeddedFZFWithOptions
	origCanUse := canUseEmbeddedFZFForTest
	t.Cleanup(func() {
		runEmbeddedFZFWithOptions = origRunner
		canUseEmbeddedFZFForTest = origCanUse
	})

	runEmbeddedFZFWithOptions = func(mode, query string, stdin io.Reader, cfg shellinit.RunFZFOptions) (shellinit.RunFZFResult, error) {
		if mode != "default" {
			t.Fatalf("mode = %q, want default", mode)
		}
		if !cfg.UseFullscreen {
			t.Fatalf("expected Ralph spec picker to use fullscreen FZF")
		}
		if cfg.Header != "Run a Ralph spec" {
			t.Fatalf("cfg.Header = %q, want %q", cfg.Header, "Run a Ralph spec")
		}
		data, err := io.ReadAll(stdin)
		if err != nil {
			t.Fatalf("ReadAll(stdin): %v", err)
		}
		text := string(data)
		if !strings.Contains(text, "fox-align-cedar") || !strings.Contains(text, "active") {
			t.Fatalf("stdin missing Ralph spec picker item: %q", text)
		}
		selected := strings.TrimSpace(strings.Split(text, "\n")[0])
		return shellinit.RunFZFResult{Code: 0, Selected: []string{selected}}, nil
	}
	canUseEmbeddedFZFForTest = func() bool { return true }

	chosen, ok, err := defaultRunPlanSpecPicker(context.Background(), []ralph.SpecPair{pair}, planSpecPickerPurposeRun)
	if err != nil {
		t.Fatalf("defaultRunPlanSpecPicker: %v", err)
	}
	if !ok {
		t.Fatalf("expected selection")
	}
	if chosen.SpecPath != pair.SpecPath {
		t.Fatalf("SpecPath = %q, want %q", chosen.SpecPath, pair.SpecPath)
	}
}

func TestRunSessionAgentCallWithRendererSupportsPlanningQuestionExtraTool(t *testing.T) {
	repo := t.TempDir()
	t.Chdir(repo)

	specPath := filepath.Join(repo, ".raijin", "ralph", "spec-otter-thread-sage.md")
	model := newScriptedStreamModel(
		scriptedToolCallResponse("q1", "question", `{"question":"Which baseline matters?","options":[{"label":"CLI-first","description":"Prefer the CLI."}]}`),
		scriptedToolCallResponse("w1", "write", `{"path":".raijin/ralph/spec-otter-thread-sage.md","content":"# Goal\n\nInterviewed spec\n\n# User Specification\n\nCLI-first.\n\n# Plan\n\n1. Ship it.\n"}`),
		scriptedTextResponse("planning complete"),
	)
	runtimeModel := libagent.RuntimeModel{
		Model: model,
		ModelCfg: libagent.ModelConfig{
			Provider: "mock",
			Model:    "mock",
		},
	}

	sess, err := session.NewEphemeral(runtimeModel)
	if err != nil {
		t.Fatalf("NewEphemeral: %v", err)
	}
	if err := sess.StartEphemeral(context.Background()); err != nil {
		t.Fatalf("StartEphemeral: %v", err)
	}

	questionTool := libagent.NewTypedTool("question", "planner question", func(_ context.Context, input struct {
		Question string `json:"question"`
		Options  []struct {
			Label string `json:"label"`
		} `json:"options"`
	}, _ libagent.ToolCall,
	) (libagent.ToolResponse, error) {
		if input.Question != "Which baseline matters?" {
			t.Fatalf("input.Question = %q", input.Question)
		}
		if len(input.Options) != 1 || input.Options[0].Label != "CLI-first" {
			t.Fatalf("unexpected question options = %#v", input.Options)
		}
		return libagent.NewTextResponse("CLI-first"), nil
	})

	var stdout, stderr bytes.Buffer
	_, err = runSessionAgentCallWithRenderer(context.Background(), Options{
		Ephemeral:    true,
		RuntimeModel: runtimeModel,
		ModelCfg:     runtimeModel.ModelCfg,
	}, sess, agent.SessionAgentCall{
		SessionID:  sess.ID(),
		Prompt:     "plan this feature",
		ExtraTools: []libagent.Tool{questionTool},
	}, &stdout, &stderr)
	if err != nil {
		t.Fatalf("runSessionAgentCallWithRenderer: %v", err)
	}

	data, err := os.ReadFile(specPath)
	if err != nil {
		t.Fatalf("ReadFile(spec): %v", err)
	}
	if !strings.Contains(string(data), "Interviewed spec") || !strings.Contains(string(data), "CLI-first") {
		t.Fatalf("spec content = %q", string(data))
	}
	if !strings.Contains(stdout.String(), "planning complete") {
		t.Fatalf("stdout = %q, want final assistant text", stdout.String())
	}
}

func TestHandlePlanRootReviewActionRendersSpec(t *testing.T) {
	repo := t.TempDir()
	t.Chdir(repo)
	pair := writeRalphSpecPair(t, filepath.Join(repo, ".raijin", "ralph", "spec-otter-thread-sage.md"), "# Goal\n\nexisting goal\n\n# User Specification\n\nKeep it clear.\n\n# Plan\n\n1. Existing step.\n", "current slice\n")

	origRunRalph := runRalph
	origRootPicker := runPlanRootPicker
	origSpecPicker := runPlanSpecPicker
	t.Cleanup(func() {
		runRalph = origRunRalph
		runPlanRootPicker = origRootPicker
		runPlanSpecPicker = origSpecPicker
	})

	runPlanRootPicker = func(bool) (planRootAction, bool, error) {
		return planRootActionReview, true, nil
	}
	runPlanSpecPicker = func(_ context.Context, pairs []ralph.SpecPair, purpose planSpecPickerPurpose) (ralph.SpecPair, bool, error) {
		if len(pairs) != 1 {
			t.Fatalf("pairs len = %d, want 1", len(pairs))
		}
		if purpose != planSpecPickerPurposeReview {
			t.Fatalf("purpose = %q, want review", purpose)
		}
		return pair, true, nil
	}
	runRalph = func(_ context.Context, _ ralph.Options) error {
		t.Fatalf("runRalph should not be called for review action")
		return nil
	}

	out := captureStdout(t, func() {
		if err := handlePlan(""); err != nil {
			t.Fatalf("handlePlan(root review): %v", err)
		}
	})
	if !strings.Contains(out, "existing goal") || !strings.Contains(out, "current slice") || !strings.Contains(out, "Ralph Progress") {
		t.Fatalf("expected rendered spec/progress review output, got %q", out)
	}
}

func TestBuildPlanSpecPickerItemsSortsActiveBeforeCompletedAndIncludesRawPreview(t *testing.T) {
	repo := t.TempDir()
	t.Chdir(repo)
	active := writeRalphSpecPair(t, filepath.Join(repo, ".raijin", "ralph", "spec-otter-thread-sage.md"), "# Goal\n\nactive goal\n\n# User Specification\n\nA.\n\n# Plan\n\n1. First.\n", "active progress\n")
	completed := writeRalphSpecPair(t, filepath.Join(repo, ".raijin", "ralph", "spec-fox-align-cedar.md"), "# Goal\n\ncompleted goal\n\n# User Specification\n\nB.\n\n# Plan\n\n1. Second.\n", "done\n<promise>DONE</promise>\n")

	items, keyToPair, err := buildPlanSpecPickerItems(context.Background(), []ralph.SpecPair{completed, active})
	if err != nil {
		t.Fatalf("buildPlanSpecPickerItems: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("len(items) = %d, want 2", len(items))
	}
	if got := keyToPair[items[0].key].SpecPath; got != active.SpecPath {
		t.Fatalf("first item spec = %q, want active spec %q", got, active.SpecPath)
	}
	if !strings.Contains(items[0].label, "active") || !strings.Contains(items[0].label, "◉") {
		t.Fatalf("first label = %q, want active label", items[0].label)
	}
	if !strings.Contains(items[1].label, "completed") || !strings.Contains(items[1].label, "✓") {
		t.Fatalf("second label = %q, want completed label", items[1].label)
	}
	if !strings.Contains(items[0].preview, "◉ Ralph snapshot") || !strings.Contains(items[0].preview, "# Goal\n\nactive goal") || !strings.Contains(items[0].preview, "active progress") {
		t.Fatalf("active preview missing raw contents: %q", items[0].preview)
	}
	if !strings.Contains(items[1].preview, "✓ Ralph snapshot") || !strings.Contains(items[1].preview, "# Goal\n\ncompleted goal") || !strings.Contains(items[1].preview, "done\n<promise>DONE</promise>") {
		t.Fatalf("completed preview missing raw contents: %q", items[1].preview)
	}
}

func TestHandlePlanMultipleSpecsWithoutTTYFailsWithRootMenuError(t *testing.T) {
	repo := t.TempDir()
	t.Chdir(repo)
	writeRalphSpecPair(t, filepath.Join(repo, ".raijin", "ralph", "spec-otter-thread-sage.md"), "# Goal\n\nfirst goal\n\n# User Specification\n\nA.\n\n# Plan\n\n1. First.\n", "")
	writeRalphSpecPair(t, filepath.Join(repo, ".raijin", "ralph", "spec-fox-align-cedar.md"), "# Goal\n\nsecond goal\n\n# User Specification\n\nB.\n\n# Plan\n\n1. Second.\n", "")

	origRootPicker := runPlanRootPicker
	t.Cleanup(func() { runPlanRootPicker = origRootPicker })
	runPlanRootPicker = defaultRunPlanRootPicker

	err := handlePlan("")
	if err == nil {
		t.Fatalf("expected TTY error")
	}
	if !strings.Contains(err.Error(), "interactive Ralph dashboard requires a TTY") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunPromptWritesAssistantCaptureFile(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv(persist.SessionBindingKeyEnv, "")
	t.Setenv(persist.SessionBindingOwnerPIDEnv, "")

	capturePath := filepath.Join(t.TempDir(), "assistant.txt")
	t.Setenv(assistantCaptureEnv, capturePath)

	opts := Options{
		Ephemeral: true,
		RuntimeModel: libagent.RuntimeModel{
			Model: &libagent.StaticTextModel{Response: "captured output\n<promise>DONE</promise>"},
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

	if err := Run(opts, "hello"); err != nil {
		t.Fatalf("Run(ephemeral prompt): %v", err)
	}

	data, err := os.ReadFile(capturePath)
	if err != nil {
		t.Fatalf("ReadFile(capture): %v", err)
	}
	got := string(data)
	if !strings.Contains(got, "<promise>DONE</promise>") {
		t.Fatalf("capture = %q, want promise marker", got)
	}
}

func TestRunPromptEphemeralDoesNotRequireBoundContextOrPersistState(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv(persist.SessionBindingKeyEnv, "")
	t.Setenv(persist.SessionBindingOwnerPIDEnv, "")

	opts := Options{
		Ephemeral: true,
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

	stdout := captureStdout(t, func() {
		stderr := captureStderr(t, func() {
			if err := Run(opts, "hello"); err != nil {
				t.Fatalf("Run(ephemeral prompt): %v", err)
			}
		})
		if strings.Contains(stderr, "bound context") {
			t.Fatalf("unexpected bound-context error on stderr: %q", stderr)
		}
	})

	if !strings.Contains(stdout, "done") {
		t.Fatalf("expected assistant output, got %q", stdout)
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

func TestRunMaxImagesUpdatesDefaultModelLimit(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	store, err := modelconfig.LoadModelStore()
	if err != nil {
		t.Fatalf("LoadModelStore: %v", err)
	}
	model := libagent.ModelConfig{
		Name:     "moonshot/kimi-k2.5",
		Provider: "moonshot",
		Model:    "kimi-k2.5",
	}
	if err := store.Add(model); err != nil {
		t.Fatalf("Add model: %v", err)
	}
	if err := store.SetDefault(model.Name); err != nil {
		t.Fatalf("SetDefault: %v", err)
	}

	opts := Options{Store: store}
	if err := Run(opts, "/max-images 8"); err != nil {
		t.Fatalf("Run(/max-images 8): %v", err)
	}

	reloaded, err := modelconfig.LoadModelStore()
	if err != nil {
		t.Fatalf("Reload model store: %v", err)
	}
	got, ok := reloaded.GetDefault()
	if !ok {
		t.Fatalf("expected default model after max-images update")
	}
	if got.MaxImages == nil || *got.MaxImages != 8 {
		t.Fatalf("MaxImages = %v, want 8", got.MaxImages)
	}
}

func TestRunMaxImagesRejectsInvalidValue(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	store, err := modelconfig.LoadModelStore()
	if err != nil {
		t.Fatalf("LoadModelStore: %v", err)
	}
	model := libagent.ModelConfig{
		Name:     "moonshot/kimi-k2.5",
		Provider: "moonshot",
		Model:    "kimi-k2.5",
	}
	if err := store.Add(model); err != nil {
		t.Fatalf("Add model: %v", err)
	}
	if err := store.SetDefault(model.Name); err != nil {
		t.Fatalf("SetDefault: %v", err)
	}

	err = Run(Options{Store: store}, "/max-images nope")
	if err == nil {
		t.Fatalf("expected invalid max-images error")
	}
	if !strings.Contains(err.Error(), "invalid max-images value") {
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

	want := renderUserSeparator() + "\n" + renderUserPrefix() + "hello\n"
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

	want := renderUserSeparator() + "\n" + renderUserPrefix() + "hello\n" +
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

func TestRunHistoryReplaysPersistedCompactionEvents(t *testing.T) {
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

	ctx := context.Background()
	for i := 0; i < 6; i++ {
		if _, err := store.Messages().Create(ctx, sess.ID, &libagent.UserMessage{
			Role:      "user",
			Content:   strings.Repeat("u", 1200),
			Timestamp: time.Now(),
		}); err != nil {
			t.Fatalf("create user %d: %v", i, err)
		}
		msg := libagent.NewAssistantMessage(strings.Repeat("a", 1200), "", nil, time.Now())
		msg.Completed = true
		if _, err := store.Messages().Create(ctx, sess.ID, msg); err != nil {
			t.Fatalf("create assistant %d: %v", i, err)
		}
	}

	opts := Options{
		RuntimeModel: libagent.RuntimeModel{
			Model: &libagent.StaticTextModel{Response: "checkpoint"},
			ModelCfg: libagent.ModelConfig{
				Provider:      "mock",
				Model:         "mock",
				ContextWindow: 6000,
			},
		},
		ModelCfg: libagent.ModelConfig{
			Provider:      "mock",
			Model:         "mock",
			ContextWindow: 6000,
		},
	}

	compactStderr := captureStderr(t, func() {
		if err := Run(opts, "/compact"); err != nil {
			t.Fatalf("Run(/compact): %v", err)
		}
	})
	if !strings.Contains(compactStderr, "Compacting session") {
		t.Fatalf("expected compact start status, got %q", compactStderr)
	}
	if !strings.Contains(compactStderr, "Context compacted") {
		t.Fatalf("expected compact end status, got %q", compactStderr)
	}

	var historyStdout string
	historyStderr := captureStderr(t, func() {
		historyStdout = captureStdout(t, func() {
			if err := Run(opts, "/history"); err != nil {
				t.Fatalf("Run(/history): %v", err)
			}
		})
	})
	if !strings.Contains(historyStderr, "Compacting session") {
		t.Fatalf("expected replayed compact start status, got %q", historyStderr)
	}
	if !strings.Contains(historyStderr, "Context compacted") {
		t.Fatalf("expected replayed compact end status, got %q", historyStderr)
	}
	if !strings.Contains(historyStdout, "[Context checkpoint created by /compact]") {
		t.Fatalf("expected checkpoint summary in history output, got %q", historyStdout)
	}
}

func TestReplaySessionEventsDoesNotEnablePersistentSpinner(t *testing.T) {
	var stderr bytes.Buffer
	var stdout bytes.Buffer
	r := newRenderer(&stderr, &stdout, nil, true)

	items := []persist.ReplayItem{
		{Message: &libagent.UserMessage{Role: "user", Content: "hello"}},
		{Message: libagent.NewAssistantMessage("world", "", nil, time.Now())},
	}

	replayed := replaySessionEvents(r, items)
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

func TestRunRetryRewindsCompletedAssistantTurn(t *testing.T) {
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
	user, err := msgs.Create(ctx, sess.ID, &libagent.UserMessage{
		Role:    "user",
		Content: "start",
	})
	if err != nil {
		t.Fatalf("create user message: %v", err)
	}
	finalAssistant := libagent.NewAssistantMessage("done", "", nil, time.UnixMilli(1))
	finalAssistant.Completed = true
	if _, err := msgs.Create(ctx, sess.ID, finalAssistant); err != nil {
		t.Fatalf("create final assistant: %v", err)
	}
	bindSession(t, key, store, sess)

	opts := Options{
		RuntimeModel: libagent.RuntimeModel{
			Model: &libagent.StaticTextModel{Response: "again"},
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
	if !strings.Contains(out, "again") {
		t.Fatalf("expected retried output, got %q", out)
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
	if len(got) != 2 {
		t.Fatalf("messages after retry = %d, want 2", len(got))
	}
	if libagent.MessageID(got[0]) != libagent.MessageID(user) {
		t.Fatalf("current branch user = %q, want %q", libagent.MessageID(got[0]), libagent.MessageID(user))
	}
	if got[1].GetRole() != "assistant" {
		t.Fatalf("final role = %q, want assistant", got[1].GetRole())
	}
	if text := libagent.AssistantText(got[1].(*libagent.AssistantMessage)); text != "again" {
		t.Fatalf("final assistant text = %q, want %q", text, "again")
	}
}

func TestResolvePrompt_TemplateSlashName(t *testing.T) {
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
	if err := os.WriteFile(filepath.Join(project, paths.ProjectPromptsDirRel, "delegate.md"), []byte("template body"), 0o644); err != nil {
		t.Fatalf("write prompt: %v", err)
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

func TestResolvePrompt_PercentSyntaxPassesThroughAsPromptText(t *testing.T) {
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
		t.Fatalf("promptText = %q, want literal percent syntax preserved", resolved.promptText)
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

func captureStderr(t *testing.T, fn func()) string {
	t.Helper()

	orig := os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	os.Stderr = w
	t.Cleanup(func() {
		os.Stderr = orig
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
