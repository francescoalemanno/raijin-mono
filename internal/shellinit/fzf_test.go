package shellinit

import (
	"strings"
	"testing"
)

func TestFZFArgsForREPLCompletionUseFullscreen(t *testing.T) {
	args := strings.Join(fzfArgs("repl-complete", "sts"), " ")
	if strings.Contains(args, "--height=80%") {
		t.Fatalf("repl-complete args should not force light renderer height, got %q", args)
	}
	if !strings.Contains(args, "--prompt=Raijin > ") {
		t.Fatalf("repl-complete args missing prompt, got %q", args)
	}
	if !strings.Contains(args, "--query=sts") {
		t.Fatalf("repl-complete args missing query, got %q", args)
	}
}

func TestFZFArgsForShellCompletionKeepDialogHeight(t *testing.T) {
	args := strings.Join(fzfArgs("complete", ""), " ")
	if !strings.Contains(args, "--height=80%") {
		t.Fatalf("complete args should keep dialog height, got %q", args)
	}
}

func TestFZFArgsForPathsUseFullscreen(t *testing.T) {
	args := strings.Join(fzfArgs("paths", "todo"), " ")
	if strings.Contains(args, "--height=80%") {
		t.Fatalf("paths args should not force light renderer height, got %q", args)
	}
	if !strings.Contains(args, "--scheme=path") {
		t.Fatalf("paths args missing path scheme, got %q", args)
	}
	if !strings.Contains(args, "--prompt=@ ") {
		t.Fatalf("paths args missing prompt, got %q", args)
	}
}
