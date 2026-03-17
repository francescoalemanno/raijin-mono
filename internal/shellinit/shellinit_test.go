package shellinit

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
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
	out := Complete(":/add")
	lines := strings.Split(strings.TrimSpace(out), "\n")
	found := false
	for _, line := range lines {
		if line == ":/add-model" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected :/add-model in completions, got %q", lines)
	}
}

func TestCompleteBareCommandToken(t *testing.T) {
	out := Complete(":add")
	lines := strings.Split(strings.TrimSpace(out), "\n")
	found := false
	for _, line := range lines {
		if line == ":/add-model" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected :/add-model in bare-token completions, got %q", lines)
	}
}

func TestCompleteSkillsPrefix(t *testing.T) {
	out := Complete(":+")
	lines := strings.Split(strings.TrimSpace(out), "\n")
	found := false
	for _, line := range lines {
		if strings.HasPrefix(line, ":+") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected at least one skill completion, got %q", lines)
	}
}

func TestCompleteColonShowsSkillsAndCommands(t *testing.T) {
	out := Complete(":")
	lines := strings.Split(strings.TrimSpace(out), "\n")
	foundSkill := false
	foundCommand := false
	for _, line := range lines {
		if strings.HasPrefix(line, ":+") {
			foundSkill = true
		}
		if strings.HasPrefix(line, ":/") {
			foundCommand = true
		}
	}
	if !foundSkill || !foundCommand {
		t.Fatalf("expected both skill and command completions, got %q", lines)
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
	out := Complete(":please run /add")
	lines := strings.Split(strings.TrimSpace(out), "\n")
	found := false
	for _, line := range lines {
		if line == "/add-model" || line == ":/add-model" {
			found = true
			break
		}
	}
	if found {
		t.Fatalf("expected no /command completion for mid-sentence token, got %q", lines)
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

func TestBashInitGeneratesColonShortcuts(t *testing.T) {
	script, err := Init("bash")
	if err != nil {
		t.Fatalf("Init(bash) failed: %v", err)
	}
	if !strings.Contains(script, "alias :='raijin'") {
		t.Fatalf("bash init missing : alias")
	}
	if !strings.Contains(script, "alias :status='raijin /status'") {
		t.Fatalf("bash init missing generated :status alias")
	}
	if !strings.Contains(script, "alias :+") {
		t.Fatalf("bash init missing generated :+skill aliases")
	}
	if strings.Contains(script, ":() {") {
		t.Fatalf("bash init should not emit function shortcuts")
	}
	if strings.Contains(script, "bind -x") || strings.Contains(script, "complete -D") {
		t.Fatalf("bash init should not use keybinding or completion interception")
	}
	if !strings.Contains(script, "complete -F _raijin_colon_complete :") {
		t.Fatalf("bash init missing completion for : alias")
	}
	if !strings.Contains(script, `raijin --complete "$line"`) {
		t.Fatalf("bash init completion should delegate to raijin --complete")
	}
}

func TestZshInitGeneratesColonShortcuts(t *testing.T) {
	script, err := Init("zsh")
	if err != nil {
		t.Fatalf("Init(zsh) failed: %v", err)
	}
	if !strings.Contains(script, "alias :='raijin'") {
		t.Fatalf("zsh init missing : alias")
	}
	if !strings.Contains(script, "alias :status='raijin /status'") {
		t.Fatalf("zsh init missing generated :status alias")
	}
	if !strings.Contains(script, "alias :+") {
		t.Fatalf("zsh init missing generated :+skill aliases")
	}
	if strings.Contains(script, ":() {") {
		t.Fatalf("zsh init should not emit function shortcuts")
	}
	if !strings.Contains(script, `"$_RAIJIN_BIN" --complete "$line"`) {
		t.Fatalf("zsh init completion should delegate to raijin --complete")
	}
	if !strings.Contains(script, `"$_RAIJIN_BIN" --fzf paths --fzf-query "$query"`) {
		t.Fatalf("zsh init missing embedded path picker")
	}
	if !strings.Contains(script, `| "$_RAIJIN_BIN" --fzf complete --fzf-query "$current_word" 2>/dev/null`) {
		t.Fatalf("zsh init missing embedded completion picker")
	}
	if strings.Contains(script, "command find .") || strings.Contains(script, " fzf ") || strings.Contains(script, "fdfind ") || strings.Contains(script, "fd --type") {
		t.Fatalf("zsh init should not depend on external path/fzf tools")
	}
	if !strings.Contains(script, "zle -N raijin-completion-widget _raijin_completion_widget") {
		t.Fatalf("zsh init missing custom tab completion widget")
	}
	if !strings.Contains(script, "bindkey -M emacs '^I' raijin-completion-widget") || !strings.Contains(script, "bindkey -M viins '^I' raijin-completion-widget") {
		t.Fatalf("zsh init missing tab keybinding for custom completion widget")
	}
	if !strings.Contains(script, "zle -N raijin-accept-line _raijin_accept_line") {
		t.Fatalf("zsh init missing custom accept-line widget")
	}
	if !strings.Contains(script, "bindkey -M emacs '^M' raijin-accept-line") || !strings.Contains(script, "bindkey -M viins '^M' raijin-accept-line") || !strings.Contains(script, "bindkey -M vicmd '^M' raijin-accept-line") {
		t.Fatalf("zsh init missing enter keybinding for accept-line widget")
	}
	if !strings.Contains(script, "zle -N bracketed-paste _raijin_bracketed_paste") {
		t.Fatalf("zsh init missing bracketed-paste refresh widget")
	}
	if !strings.Contains(script, "_raijin_enable_syntax_highlighting") {
		t.Fatalf("zsh init missing syntax-highlighting hook")
	}
	if !strings.Contains(script, "compdef _raijin_colon_complete :") {
		t.Fatalf("zsh init missing completion wiring for : alias")
	}
	if !strings.Contains(script, "_raijin_register_colon_completion_precmd") {
		t.Fatalf("zsh init missing deferred completion registration hook")
	}
	if !strings.Contains(script, "_raijin_colon_completer") {
		t.Fatalf("zsh init missing global colon completer fallback")
	}
}

func TestFishInitGeneratesColonShortcuts(t *testing.T) {
	script, err := Init("fish")
	if err != nil {
		t.Fatalf("Init(fish) failed: %v", err)
	}
	if !strings.Contains(script, `alias : "raijin"`) {
		t.Fatalf("fish init missing : alias")
	}
	if !strings.Contains(script, `alias :status "raijin /status"`) {
		t.Fatalf("fish init missing generated :status alias")
	}
	if !strings.Contains(script, "alias :+") {
		t.Fatalf("fish init missing generated :+skill aliases")
	}
	if strings.Contains(script, "function :") {
		t.Fatalf("fish init should not emit function shortcuts")
	}
	if !strings.Contains(script, "__raijin_colon_complete") {
		t.Fatalf("fish init missing completion helper")
	}
	if !strings.Contains(script, "raijin --complete (commandline) 2>/dev/null") {
		t.Fatalf("fish init completion should delegate to raijin --complete")
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

	paths, err := mentionPaths(root)
	if err != nil {
		t.Fatalf("mentionPaths failed: %v", err)
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
