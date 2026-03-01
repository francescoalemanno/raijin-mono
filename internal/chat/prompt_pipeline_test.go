package chat

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestResolvePromptSubmissionRejectsBuiltinInOneShot(t *testing.T) {
	t.Parallel()

	_, err := resolvePromptSubmission(context.Background(), "/help", promptModeOneShot)
	if err == nil {
		t.Fatal("expected error for /help in one-shot mode")
	}
}

func TestResolvePromptSubmissionReturnsBuiltinInInteractive(t *testing.T) {
	t.Parallel()

	resolved, err := resolvePromptSubmission(context.Background(), "/models add", promptModeInteractive)
	if err != nil {
		t.Fatalf("resolvePromptSubmission returned error: %v", err)
	}
	if resolved.builtin == nil {
		t.Fatal("expected builtin command")
	}
	if resolved.builtin.name != "models" {
		t.Fatalf("builtin name = %q, want %q", resolved.builtin.name, "models")
	}
	if len(resolved.builtin.fields) != 2 || resolved.builtin.fields[1] != "add" {
		t.Fatalf("builtin fields = %#v, want [models add]", resolved.builtin.fields)
	}
}

func TestResolvePromptSubmissionExpandsTemplate(t *testing.T) {
	t.Parallel()

	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	tmp := t.TempDir()
	if err := os.Chdir(tmp); err != nil {
		t.Fatalf("chdir temp dir: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(cwd)
	})

	projectPromptsDir := filepath.Join(".agents", "prompts")
	if err := os.MkdirAll(projectPromptsDir, 0o755); err != nil {
		t.Fatalf("mkdir prompts dir: %v", err)
	}
	content := "---\ndescription: test template\n---\nHello $1"
	if err := os.WriteFile(filepath.Join(projectPromptsDir, "hello.md"), []byte(content), 0o644); err != nil {
		t.Fatalf("write template: %v", err)
	}

	resolved, err := resolvePromptSubmission(context.Background(), "/hello world", promptModeOneShot)
	if err != nil {
		t.Fatalf("resolvePromptSubmission returned error: %v", err)
	}
	if resolved.builtin != nil {
		t.Fatal("expected non-builtin prompt")
	}
	if resolved.opts.TemplateName != "hello" {
		t.Fatalf("template name = %q, want %q", resolved.opts.TemplateName, "hello")
	}
	if resolved.promptText != "Hello world" {
		t.Fatalf("resolved prompt text = %q, want %q", resolved.promptText, "Hello world")
	}
}

func TestResolvePromptSubmissionRejectsUnknownSlashCommand(t *testing.T) {
	t.Parallel()

	_, err := resolvePromptSubmission(context.Background(), "/definitely-unknown-command", promptModeOneShot)
	if err == nil {
		t.Fatal("expected error for unknown slash command")
	}
}
