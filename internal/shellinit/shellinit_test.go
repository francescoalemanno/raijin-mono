package shellinit

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/francescoalemanno/raijin-mono/internal/completion"
)

func TestCompletionsIncludeOneShotStatus(t *testing.T) {
	lines := strings.Split(Completions(), "\n")
	foundStatus := false
	foundReasoning := false
	foundEdit := false
	for _, line := range lines {
		if line == "status" {
			foundStatus = true
		}
		if line == "reasoning" {
			foundReasoning = true
		}
		if line == "edit" {
			foundEdit = true
		}
	}
	if !foundStatus || !foundReasoning || !foundEdit {
		t.Fatalf("expected completions to include status, reasoning, and edit, got %q", lines)
	}
}

func TestCompletionsIncludeSkills(t *testing.T) {
	lines := strings.Split(Completions(), "\n")
	foundSkill := false
	for _, line := range lines {
		if strings.HasPrefix(line, "+") {
			foundSkill = true
			break
		}
	}
	if !foundSkill {
		t.Fatalf("expected --completions output to include +skill entries, got %q", lines)
	}
}

func TestCompleteSlashCommand(t *testing.T) {
	out := Complete("/add")
	lines := strings.Split(strings.TrimSpace(out), "\n")
	found := false
	for _, line := range lines {
		if line == "/add-model" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected /add-model in completions, got %q", lines)
	}
}

func TestCompleteBareCommandToken(t *testing.T) {
	// Bare tokens (without /, +, or @ prefix) now trigger universal completion
	// They autocomplete among all candidates (skills + builtins + templates)
	out := Complete("add")
	lines := strings.Split(strings.TrimSpace(out), "\n")
	// Should find "/add-model" among the completions
	found := false
	for _, line := range lines {
		if line == "/add-model" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected universal completion to include '/add-model' for 'add', got %q", lines)
	}
}

func TestCompleteUsesFuzzyMatchingForCommands(t *testing.T) {
	out := Complete("/rs")
	lines := strings.Split(strings.TrimSpace(out), "\n")
	found := false
	for _, line := range lines {
		if line == "/reasoning" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected fuzzy completion /reasoning for /rs, got %q", lines)
	}
}

func TestCompleteSkillsPrefix(t *testing.T) {
	out := Complete("+")
	lines := strings.Split(strings.TrimSpace(out), "\n")
	found := false
	for _, line := range lines {
		if strings.HasPrefix(line, "+") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected at least one skill completion, got %q", lines)
	}
}

func TestCompleteSlashShowsSkillsAndCommands(t *testing.T) {
	// "/" alone doesn't trigger completions - need a prefix or trigger char
	// This test verifies that "/" with some context works
	out := Complete("/")
	lines := strings.Split(strings.TrimSpace(out), "\n")
	foundCommand := false
	for _, line := range lines {
		if strings.HasPrefix(line, "/") {
			foundCommand = true
			break
		}
	}
	if !foundCommand {
		t.Fatalf("expected command completions for /, got %q", lines)
	}
}

func TestCompleteMidSentenceSkillToken(t *testing.T) {
	out := Complete(":please use +")
	lines := strings.Split(strings.TrimSpace(out), "\n")
	foundSkill := false
	for _, line := range lines {
		if strings.HasPrefix(line, "+") {
			foundSkill = true
			break
		}
	}
	if !foundSkill {
		t.Fatalf("expected skill completion for mid-sentence token, got %q", lines)
	}
}

func TestCompleteMidSentenceSlashToken(t *testing.T) {
	out := Complete("please run /add")
	lines := strings.Split(strings.TrimSpace(out), "\n")
	found := false
	for _, line := range lines {
		if line == "/add-model" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected /command completion for mid-sentence token, got %q", lines)
	}
}


func TestCompleteMidSentenceSkillWithPrefixStillCompletes(t *testing.T) {
	out := Complete(":please use +tm")
	lines := strings.Split(strings.TrimSpace(out), "\n")
	found := false
	for _, line := range lines {
		if strings.HasPrefix(line, "+tm") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected +skill completion for mid-sentence token, got %q", lines)
	}
}

func TestCompleteSelectionReturnsOriginalOnNoMatch(t *testing.T) {
	prevMatches := completionMatchesForSelect
	t.Cleanup(func() {
		completionMatchesForSelect = prevMatches
	})
	completionMatchesForSelect = func(current string) []string {
		return nil
	}

	input := "see @rea"
	if got := CompleteSelection(input); got != input {
		t.Fatalf("CompleteSelection(%q) = %q, want %q", input, got, input)
	}
}

func TestCompleteSelectionReturnsSingleMatchWithoutFZF(t *testing.T) {
	prevMatches := completionMatchesForSelect
	t.Cleanup(func() {
		completionMatchesForSelect = prevMatches
	})
	completionMatchesForSelect = func(current string) []string {
		return []string{"@readme.md"}
	}

	prev := runFZFForComplete
	t.Cleanup(func() {
		runFZFForComplete = prev
	})
	runFZFForComplete = func(mode, query string, stdin io.Reader, stdout io.Writer) (int, error) {
		t.Fatalf("runFZFForComplete should not be called for a single match")
		return 1, nil
	}

	input := "see @rea"
	want := "see @readme.md"
	if got := CompleteSelection(input); got != want {
		t.Fatalf("CompleteSelection(%q) = %q, want %q", input, got, want)
	}
}

func TestCompleteSelectionUsesFZFWhenMultipleMatches(t *testing.T) {
	prevMatches := completionMatchesForSelect
	t.Cleanup(func() {
		completionMatchesForSelect = prevMatches
	})
	completionMatchesForSelect = func(current string) []string {
		return []string{"@readme.md", "@real.txt"}
	}

	prev := runFZFForComplete
	t.Cleanup(func() {
		runFZFForComplete = prev
	})

	var expected string
	runFZFForComplete = func(mode, query string, stdin io.Reader, stdout io.Writer) (int, error) {
		if mode != "complete" {
			t.Fatalf("mode = %q, want complete", mode)
		}
		if query != "re" {
			t.Fatalf("query = %q, want re", query)
		}
		raw, err := io.ReadAll(stdin)
		if err != nil {
			t.Fatalf("read stdin: %v", err)
		}
		lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
		if len(lines) < 2 {
			t.Fatalf("expected at least 2 completion candidates, got %q", lines)
		}
		expected = lines[1]
		if _, err := io.WriteString(stdout, expected+"\n"); err != nil {
			t.Fatalf("write stdout: %v", err)
		}
		return 0, nil
	}

	input := "see @re"
	got := CompleteSelection(input)
	if expected == "" {
		t.Fatalf("expected stub picker to provide a selected candidate")
	}
	want := "see " + expected
	if got != want {
		t.Fatalf("CompleteSelection(%q) = %q, want %q", input, got, want)
	}
}

func TestCompleteSelectionReturnsOriginalWhenPickerCancelled(t *testing.T) {
	prevMatches := completionMatchesForSelect
	t.Cleanup(func() {
		completionMatchesForSelect = prevMatches
	})
	completionMatchesForSelect = func(current string) []string {
		return []string{"@readme.md", "@real.txt"}
	}

	prev := runFZFForComplete
	t.Cleanup(func() {
		runFZFForComplete = prev
	})
	runFZFForComplete = func(mode, query string, stdin io.Reader, stdout io.Writer) (int, error) {
		return 130, nil
	}

	input := "see @re"
	if got := CompleteSelection(input); got != input {
		t.Fatalf("CompleteSelection(%q) = %q, want %q on cancel", input, got, input)
	}
}

func TestBashInitProvidesColonAlias(t *testing.T) {
	script, err := Init("bash", "raijin")
	if err != nil {
		t.Fatalf("Init(bash) failed: %v", err)
	}
	if !strings.Contains(script, `_RAIJIN_BINDING_KEY="${RAIJIN_SESSION_BINDING_KEY:-shell-bash-$$-$RANDOM}"`) {
		t.Fatalf("bash init missing binding key export")
	}
	if !strings.Contains(script, `_raijin_main() {`) {
		t.Fatalf("bash init missing wrapper function")
	}
	if !strings.Contains(script, "alias :='_raijin_main'") {
		t.Fatalf("bash init missing : alias")
	}
	if strings.Contains(script, "alias :status") {
		t.Fatalf("bash init should not generate :status shorthand alias")
	}
	if strings.Contains(script, "alias :+") {
		t.Fatalf("bash init should not generate :+skill shorthand aliases")
	}
	if strings.Contains(script, "bind -x") || strings.Contains(script, "complete -D") {
		t.Fatalf("bash init should not use keybinding or completion interception")
	}
	if !strings.Contains(script, "complete -F _raijin_colon_complete :") {
		t.Fatalf("bash init missing completion for : alias")
	}
	if !strings.Contains(script, `"raijin" -complete "$line"`) {
		t.Fatalf("bash init completion should delegate to raijin -complete")
	}
}

func TestZshInitProvidesColonAlias(t *testing.T) {
	script, err := Init("zsh", "raijin")
	if err != nil {
		t.Fatalf("Init(zsh) failed: %v", err)
	}
	if !strings.Contains(script, `typeset -h _RAIJIN_BINDING_KEY="${RAIJIN_SESSION_BINDING_KEY:-shell-zsh-$$-$RANDOM}"`) {
		t.Fatalf("zsh init missing binding key setup")
	}
	if !strings.Contains(script, "_raijin_main() {") {
		t.Fatalf("zsh init missing _raijin_main wrapper function")
	}
	if !strings.Contains(script, "alias :='noglob _raijin_main'") {
		t.Fatalf("zsh init missing : alias with noglob")
	}
	if strings.Contains(script, "alias :status") {
		t.Fatalf("zsh init should not generate :status shorthand alias")
	}
	if strings.Contains(script, "alias :+") {
		t.Fatalf("zsh init should not generate :+skill shorthand aliases")
	}
	if !strings.Contains(script, "_raijin_exec()") {
		t.Fatalf("zsh init missing execution helper")
	}
	if !strings.Contains(script, `-complete "$LBUFFER"`) {
		t.Fatalf("zsh init completion should delegate to raijin -complete")
	}
	if !strings.Contains(script, "zle -I") {
		t.Fatalf("zsh init missing zle -I for interactive completion")
	}
	if !strings.Contains(script, "[[ ! \"$LBUFFER\" =~ '[@:+/]' ]]") {
		t.Fatalf("zsh init missing trigger check optimization")
	}
	if !strings.Contains(script, "zle -N raijin-completion-widget _raijin_completion_widget") {
		t.Fatalf("zsh init missing tab completion widget")
	}

	if !strings.Contains(script, "bindkey -M main '^I' raijin-completion-widget") || !strings.Contains(script, "bindkey -M emacs '^I' raijin-completion-widget") || !strings.Contains(script, "bindkey -M viins '^I' raijin-completion-widget") || !strings.Contains(script, "bindkey -M vicmd '^I' raijin-completion-widget") {
		t.Fatalf("zsh init missing tab keybindings")
	}
	if !strings.Contains(script, "add-zle-hook-widget line-init _raijin_bind_tab") || !strings.Contains(script, "add-zle-hook-widget keymap-select _raijin_bind_tab") {
		t.Fatalf("zsh init missing tab rebind hooks")
	}
	if !strings.Contains(script, "zle .expand-or-complete") {
		t.Fatalf("zsh init should use builtin expand-or-complete as fallback")
	}

	removed := []string{
		"--fzf",
		"--fzf-query",
		"_raijin_should_use_command_picker",
		"_raijin_register_widgets_precmd",
		"_raijin_register_widget_hooks",
		"_raijin_enable_syntax_highlighting",
		"_raijin_accept_line",
		"_raijin_colon_completer",
		"compdef _raijin_colon_complete :",
		"zle -N bracketed-paste",
	}
	for _, token := range removed {
		if strings.Contains(script, token) {
			t.Fatalf("zsh init should not include legacy logic token %q", token)
		}
	}
}

func TestFishInitProvidesColonAlias(t *testing.T) {
	script, err := Init("fish", "raijin")
	if err != nil {
		t.Fatalf("Init(fish) failed: %v", err)
	}
	if !strings.Contains(script, `function __raijin_main`) {
		t.Fatalf("fish init missing wrapper function")
	}
	if !strings.Contains(script, `set -g __raijin_binding_key "$RAIJIN_SESSION_BINDING_KEY"`) {
		t.Fatalf("fish init missing binding key setup")
	}
	if !strings.Contains(script, `alias : "__raijin_main"`) {
		t.Fatalf("fish init missing : alias")
	}
	if strings.Contains(script, `alias :status`) {
		t.Fatalf("fish init should not generate :status shorthand alias")
	}
	if strings.Contains(script, "alias :+") {
		t.Fatalf("fish init should not generate :+skill shorthand aliases")
	}
	if !strings.Contains(script, "__raijin_colon_complete") {
		t.Fatalf("fish init missing completion helper")
	}
	if !strings.Contains(script, `"raijin" -complete (commandline) 2>/dev/null`) {
		t.Fatalf("fish init completion should delegate to raijin -complete")
	}
}

func TestInitTemplatesCustomBinaryPath(t *testing.T) {
	customPath := "/usr/local/bin/my-raijin"
	for _, shell := range []string{"bash", "zsh", "fish"} {
		script, err := Init(shell, customPath)
		if err != nil {
			t.Fatalf("Init(%s) failed: %v", shell, err)
		}
		if !strings.Contains(script, customPath) {
			t.Fatalf("Init(%s) should template custom binary path %q, got:\n%s", shell, customPath, script)
		}
		// Ensure it doesn't contain the default "raijin" command (unless the custom path happens to end with it)
		if shell == "bash" && strings.Contains(script, "command raijin") {
			t.Fatalf("Init(%s) should not contain hardcoded 'raijin' command", shell)
		}
	}
}

func TestMentionPathsSkipsExcludedDirs(t *testing.T) {
	root := t.TempDir()
	mustWrite := func(rel string) {
		path := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", rel, err)
		}
		if err := os.WriteFile(path, []byte(rel), 0o644); err != nil {
			t.Fatalf("write %s: %v", rel, err)
		}
	}

	mustWrite("visible/file.txt")
	mustWrite(".env")
	mustWrite(".hidden-dir/secret.txt")
	mustWrite("node_modules/pkg/index.js")
	mustWrite("vendor/pkg/file.go")
	mustWrite("build/out.txt")
	mustWrite("_external-cache/generated.txt")

	// Change to the temp directory to test path collection
	originalWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(root); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	defer os.Chdir(originalWD)

	// Use completion package to get paths
	candidates := completion.GetCandidates(completion.Token{Type: completion.TokenFiles})
	var paths []string
	for _, c := range candidates {
		paths = append(paths, c.Display)
	}

	joined := strings.Join(paths, "\n")
	if !strings.Contains(joined, ".env") || !strings.Contains(joined, "visible/file.txt") {
		t.Fatalf("expected visible files in mention paths, got %q", paths)
	}
	for _, excluded := range []string{
		".hidden-dir/secret.txt",
		"node_modules/pkg/index.js",
		"vendor/pkg/file.go",
		"build/out.txt",
		"_external-cache/generated.txt",
	} {
		if strings.Contains(joined, excluded) {
			t.Fatalf("did not expect excluded path %q in %q", excluded, paths)
		}
	}
}
