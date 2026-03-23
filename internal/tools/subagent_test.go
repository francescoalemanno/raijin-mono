package tools

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/francescoalemanno/raijin-mono/internal/artifacts"
	"github.com/francescoalemanno/raijin-mono/internal/paths"
	libagent "github.com/francescoalemanno/raijin-mono/libagent"
)

type subagentTestRuntime struct {
	model libagent.RuntimeModel
	tools []libagent.Tool
}

func (r *subagentTestRuntime) Model() libagent.RuntimeModel { return r.model }
func (r *subagentTestRuntime) Tools() []libagent.Tool       { return r.tools }

func withSubagentToolCwd(t *testing.T, dir string) {
	t.Helper()
	prev, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(prev)
	})
}

func writeSubagentToolFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestExecuteSubagentReturnsTextAndUsesIsolatedPrompt(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	project := t.TempDir()
	withSubagentToolCwd(t, project)

	writeSubagentToolFile(t, filepath.Join(project, paths.ProjectSubagentsDirRel, "delegate.md"), `---
description: Delegate
---
You are isolated.`)
	if err := artifacts.Reload(); err != nil {
		t.Fatalf("artifacts.Reload: %v", err)
	}

	model := &libagent.StaticTextModel{Response: "nested output"}
	runtime := &subagentTestRuntime{
		model: libagent.RuntimeModel{
			Model: model,
			ModelCfg: libagent.ModelConfig{
				Provider: "mock",
				Model:    "mock",
			},
		},
	}

	got, err := ExecuteSubagent(context.Background(), runtime, "delegate", "hello world", nil)
	if err != nil {
		t.Fatalf("ExecuteSubagent: %v", err)
	}
	if got != "nested output" {
		t.Fatalf("output = %q, want %q", got, "nested output")
	}

	if model.PromptLen != 2 {
		t.Fatalf("prompt len = %d, want 2", model.PromptLen)
	}
	if !strings.Contains(model.PromptJSON, "You are isolated.") {
		t.Fatalf("prompt JSON missing system prompt: %s", model.PromptJSON)
	}
	if !strings.Contains(model.PromptJSON, "hello world") {
		t.Fatalf("prompt JSON missing user message: %s", model.PromptJSON)
	}
}

func TestExecuteSubagentRejectsUnknownWhitelistedTool(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	project := t.TempDir()
	withSubagentToolCwd(t, project)

	writeSubagentToolFile(t, filepath.Join(project, paths.ProjectSubagentsDirRel, "delegate.md"), `---
description: Delegate
tools: [missing]
---
You are isolated.`)
	if err := artifacts.Reload(); err != nil {
		t.Fatalf("artifacts.Reload: %v", err)
	}

	runtime := &subagentTestRuntime{
		model: libagent.RuntimeModel{Model: &libagent.StaticTextModel{Response: "unused"}},
	}
	_, err := ExecuteSubagent(context.Background(), runtime, "delegate", "hello world", nil)
	if err == nil || !strings.Contains(err.Error(), "unknown tools: missing") {
		t.Fatalf("err = %v, want unknown tools error", err)
	}
}

func TestExecuteSubagentRejectsSelfRecursion(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	project := t.TempDir()
	withSubagentToolCwd(t, project)

	writeSubagentToolFile(t, filepath.Join(project, paths.ProjectSubagentsDirRel, "delegate.md"), `---
description: Delegate
tools: [subagent]
---
You are isolated.`)
	if err := artifacts.Reload(); err != nil {
		t.Fatalf("artifacts.Reload: %v", err)
	}

	runtime := &subagentTestRuntime{
		model: libagent.RuntimeModel{Model: &libagent.StaticTextModel{Response: "unused"}},
	}
	_, err := ExecuteSubagent(context.Background(), runtime, "delegate", "hello world", nil)
	if err == nil || !strings.Contains(err.Error(), "cannot whitelist the subagent tool") {
		t.Fatalf("err = %v, want self-recursion error", err)
	}
}

func TestSubagentEmitterEmitsNestedToolLines(t *testing.T) {
	var lines []string
	emitter := newSubagentEmitter(func(line string) {
		lines = append(lines, line)
	}, []libagent.Tool{NewReadTool()})

	emitter.handle(libagent.AgentEvent{
		Type:       libagent.AgentEventTypeToolExecutionStart,
		ToolCallID: "call-1",
		ToolName:   "read",
		ToolArgs:   `{"path":"README.md"}`,
	})
	emitter.handle(libagent.AgentEvent{
		Type:       libagent.AgentEventTypeToolExecutionEnd,
		ToolCallID: "call-1",
		ToolName:   "read",
		ToolArgs:   `{"path":"README.md"}`,
	})

	if got, want := len(lines), 2; got != want {
		t.Fatalf("line count = %d, want %d", got, want)
	}
	if !strings.HasPrefix(lines[0], subagentNestPrefix) {
		t.Fatalf("first line = %q, want nested prefix", lines[0])
	}
	if !strings.Contains(lines[0], "read README.md") {
		t.Fatalf("first line = %q, want rendered read preview", lines[0])
	}
	if !strings.Contains(lines[1], "✓") {
		t.Fatalf("second line = %q, want success marker", lines[1])
	}
}
