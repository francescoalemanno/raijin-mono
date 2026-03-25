package shellinit

import (
	"bytes"
	"strings"
	"testing"
)

func TestFZFArgsForREPLCompletionUseFullscreen(t *testing.T) {
	args := strings.Join(fzfArgs("repl-complete", "sts", RunFZFOptions{}), " ")
	if strings.Contains(args, "--height=80%") {
		t.Fatalf("repl-complete args should not force light renderer height, got %q", args)
	}
	if !strings.Contains(args, "--prompt=Raijin > ") {
		t.Fatalf("repl-complete args missing prompt, got %q", args)
	}
	if !strings.Contains(args, "--bind=tab:accept") {
		t.Fatalf("repl-complete args should allow tab to accept selection, got %q", args)
	}
	if !strings.Contains(args, "--query=sts") {
		t.Fatalf("repl-complete args missing query, got %q", args)
	}
}

func TestFZFArgsForShellCompletionKeepDialogHeight(t *testing.T) {
	args := strings.Join(fzfArgs("complete", "", RunFZFOptions{}), " ")
	if !strings.Contains(args, "--height=80%") {
		t.Fatalf("complete args should keep dialog height, got %q", args)
	}
	if !strings.Contains(args, "--bind=tab:accept") {
		t.Fatalf("complete args should allow tab to accept selection, got %q", args)
	}
}

func TestFZFArgsForPathsUseFullscreen(t *testing.T) {
	args := strings.Join(fzfArgs("paths", "todo", RunFZFOptions{}), " ")
	if strings.Contains(args, "--height=80%") {
		t.Fatalf("paths args should not force light renderer height, got %q", args)
	}
	if !strings.Contains(args, "--scheme=path") {
		t.Fatalf("paths args missing path scheme, got %q", args)
	}
	if !strings.Contains(args, "--prompt=@ ") {
		t.Fatalf("paths args missing prompt, got %q", args)
	}
	if !strings.Contains(args, "--bind=tab:accept") {
		t.Fatalf("paths args should allow tab to accept selection, got %q", args)
	}
}

func TestFZFArgsIncludeExpectAndBind(t *testing.T) {
	args := strings.Join(fzfArgs("default", "", RunFZFOptions{
		ExpectKeys:  []string{"ctrl-x"},
		Bindings:    []string{"ctrl-x:accept"},
		DisableSort: true,
		Header:      ">>> ENTER = SELECT | CTRL+X = DELETE <<<",
	}), " ")
	if !strings.Contains(args, "--expect=ctrl-x") {
		t.Fatalf("args missing expect key, got %q", args)
	}
	if !strings.Contains(args, "--bind=ctrl-x:accept") {
		t.Fatalf("args missing bind, got %q", args)
	}
	if !strings.Contains(args, "--no-sort") {
		t.Fatalf("args missing no-sort, got %q", args)
	}
	if !strings.Contains(args, "--header=>>> ENTER = SELECT | CTRL+X = DELETE <<<") {
		t.Fatalf("args missing header, got %q", args)
	}
}

func TestFZFArgsIncludeInitialPositionBinding(t *testing.T) {
	args := strings.Join(fzfArgs("default", "", RunFZFOptions{InitialPosition: 7}), " ")
	if !strings.Contains(args, "--bind=load:pos(7)") {
		t.Fatalf("args missing initial position bind, got %q", args)
	}
}

func TestFZFArgsIncludePreviewConfiguration(t *testing.T) {
	args := strings.Join(fzfArgs("default", "", RunFZFOptions{
		Delimiter:      "\t",
		WithNth:        "1",
		PreviewCommand: "printf '%b' {2}",
		PreviewWindow:  "right:55%,wrap",
		PreviewLabel:   "Docs",
	}), " ")
	if !strings.Contains(args, "--delimiter=\t") {
		t.Fatalf("args missing delimiter, got %q", args)
	}
	if !strings.Contains(args, "--with-nth=1") {
		t.Fatalf("args missing with-nth, got %q", args)
	}
	if !strings.Contains(args, "--preview=printf '%b' {2}") {
		t.Fatalf("args missing preview command, got %q", args)
	}
	if !strings.Contains(args, "--preview-window=right:55%,wrap") {
		t.Fatalf("args missing preview window, got %q", args)
	}
	if !strings.Contains(args, "--preview-label=Docs") {
		t.Fatalf("args missing preview label, got %q", args)
	}
}

func TestSplitExpectOutputEnterWithEmptyFirstLine(t *testing.T) {
	key, selected := splitExpectOutput([]string{"", "model-a"}, []string{"ctrl-x"})
	if key != "" {
		t.Fatalf("key = %q, want empty", key)
	}
	if len(selected) != 1 || selected[0] != "model-a" {
		t.Fatalf("selected = %#v, want [model-a]", selected)
	}
}

func TestSplitExpectOutputCtrlX(t *testing.T) {
	key, selected := splitExpectOutput([]string{"ctrl-x", "model-a"}, []string{"ctrl-x"})
	if key != "ctrl-x" {
		t.Fatalf("key = %q, want ctrl-x", key)
	}
	if len(selected) != 1 || selected[0] != "model-a" {
		t.Fatalf("selected = %#v, want [model-a]", selected)
	}
}

func TestSplitExpectOutputEnterWithoutKeyLine(t *testing.T) {
	key, selected := splitExpectOutput([]string{"model-a"}, []string{"ctrl-x"})
	if key != "" {
		t.Fatalf("key = %q, want empty", key)
	}
	if len(selected) != 1 || selected[0] != "model-a" {
		t.Fatalf("selected = %#v, want [model-a]", selected)
	}
}

func TestReadStdinItemsPreservesLeadingWhitespace(t *testing.T) {
	in := bytes.NewBufferString("   one\n\t two\n\n")
	items, err := readStdinItems(in)
	if err != nil {
		t.Fatalf("readStdinItems: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("len(items) = %d, want 2", len(items))
	}
	if got, want := items[0], "   one"; got != want {
		t.Fatalf("items[0] = %q, want %q", got, want)
	}
	if got, want := items[1], "\t two"; got != want {
		t.Fatalf("items[1] = %q, want %q", got, want)
	}
}
