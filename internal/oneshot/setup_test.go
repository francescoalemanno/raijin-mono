package oneshot

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	modelconfig "github.com/francescoalemanno/raijin-mono/internal/config"
	libagent "github.com/francescoalemanno/raijin-mono/libagent"
)

func TestRunSetupZshIsIdempotent(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("SHELL", "/bin/zsh")

	store := setupTestStoreWithDefaultModel(t)
	opts := Options{Store: store}

	if err := Run(opts, "/setup zsh"); err != nil {
		t.Fatalf("Run(/setup zsh): %v", err)
	}
	if err := Run(opts, "/setup zsh"); err != nil {
		t.Fatalf("Run(/setup zsh) second run: %v", err)
	}

	path := filepath.Join(home, ".zshrc")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%s): %v", path, err)
	}
	text := string(data)
	line := `eval "$(raijin --init zsh)"`
	if got := strings.Count(text, line); got != 1 {
		t.Fatalf("expected setup line exactly once, got %d in %q", got, text)
	}
}

func TestRunSetupAutoDetectsFishShell(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("SHELL", "/opt/homebrew/bin/fish")

	store := setupTestStoreWithDefaultModel(t)
	opts := Options{Store: store}

	if err := Run(opts, "/setup"); err != nil {
		t.Fatalf("Run(/setup): %v", err)
	}

	path := filepath.Join(home, ".config", "fish", "config.fish")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%s): %v", path, err)
	}
	text := string(data)
	line := "raijin --init fish | source"
	if !strings.Contains(text, line) {
		t.Fatalf("expected %q in fish config, got %q", line, text)
	}
}

func TestRunSetupRejectsUnsupportedShell(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("SHELL", "/bin/zsh")

	store := setupTestStoreWithDefaultModel(t)
	err := Run(Options{Store: store}, "/setup tcsh")
	if err == nil {
		t.Fatalf("expected error for unsupported shell")
	}
	if !strings.Contains(err.Error(), "unsupported shell") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func setupTestStoreWithDefaultModel(t *testing.T) *modelconfig.ModelStore {
	t.Helper()

	store, err := modelconfig.LoadModelStore()
	if err != nil {
		t.Fatalf("LoadModelStore: %v", err)
	}

	model := libagent.ModelConfig{
		Name:     "openai/gpt-test",
		Provider: "openai",
		Model:    "gpt-test",
	}
	if err := store.Add(model); err != nil {
		t.Fatalf("Add model: %v", err)
	}
	if err := store.SetDefault(model.Name); err != nil {
		t.Fatalf("SetDefault: %v", err)
	}

	return store
}
