package substitution

import (
	"context"
	"reflect"
	"runtime"
	"strings"
	"testing"
)

func TestReplaceNamed(t *testing.T) {
	t.Parallel()

	got := ReplaceNamed(
		`value={{ARGUMENTS}} literal=\{{ARGUMENTS}} dirs={{PROJECT_AGENTS_DIR}}/{{SCRIPTS_DIR}} prompts={{PROJECT_PROMPTS_DIR}}`,
		DefaultNamedValues("demo"),
		BracesStyle(),
	)

	want := "value=demo literal={{ARGUMENTS}} dirs=.agents/scripts prompts=.agents/prompts"
	if got != want {
		t.Fatalf("ReplaceNamed() = %q, want %q", got, want)
	}
}

func TestParseCommandArgs(t *testing.T) {
	t.Parallel()

	got := ParseCommandArgs(`Button "onClick handler" 'disabled support' bare`)
	want := []string{"Button", "onClick handler", "disabled support", "bare"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ParseCommandArgs() = %#v, want %#v", got, want)
	}
}

func TestExpandArgRefsFromList(t *testing.T) {
	t.Parallel()

	got := ExpandArgRefsFromList("$1 | $@ | ${@:2} | ${@:2:2}", []string{"cmd", "arg1", "arg2", "arg3"})
	want := "cmd | cmd arg1 arg2 arg3 | arg1 arg2 arg3 | arg1 arg2"
	if got != want {
		t.Fatalf("ExpandArgRefsFromList() = %q, want %q", got, want)
	}
}

func TestExpandArgRefsFromText(t *testing.T) {
	t.Parallel()

	got := ExpandArgRefsFromText(`$ARGUMENTS | $@ | $1 | ${@:2} | \$@`, "fix all")
	want := "fix all | fix all | $1 | ${@:2} | $@"
	if got != want {
		t.Fatalf("ExpandArgRefsFromText() = %q, want %q", got, want)
	}
}

func TestExpandShellSubstitutions(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("uses unix shell semantics")
	}

	input := "before\n~~ echo hello\nafter"
	got, err := ExpandShellSubstitutions(context.Background(), input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(got, "hello") {
		t.Fatalf("expected 'hello' in output, got %q", got)
	}
	if !strings.Contains(got, "before") || !strings.Contains(got, "after") {
		t.Fatalf("surrounding text missing in output: %q", got)
	}
}

func TestExpandShellSubstitutionsNoMatch(t *testing.T) {
	t.Parallel()

	input := "just plain text\nno substitutions here"
	got, err := ExpandShellSubstitutions(context.Background(), input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != input {
		t.Fatalf("expected unchanged output, got %q", got)
	}
}

func TestExpandShellSubstitutionsStderr(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("uses unix shell semantics")
	}

	input := "~~ echo out && echo err >&2"
	got, err := ExpandShellSubstitutions(context.Background(), input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(got, "out") || !strings.Contains(got, "err") {
		t.Fatalf("expected both stdout and stderr in output, got %q", got)
	}
}
