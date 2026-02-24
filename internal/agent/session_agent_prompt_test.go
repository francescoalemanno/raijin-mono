package agent

import (
	"strings"
	"testing"

	"github.com/francescoalemanno/raijin-mono/internal/core"
	"github.com/francescoalemanno/raijin-mono/llmbridge/pkg/codec"
)

func TestPrepareUserRequest_NoAllowedToolsNotice(t *testing.T) {
	t.Parallel()

	got := codec.PrepareUserRequest(codec.UserRequest{
		Prompt: "Review this file.",
	}).Prompt
	if strings.Contains(got, "only tools that are allowed are") {
		t.Fatalf("unexpected tool-allowlist notice in prompt: %q", got)
	}
}

func TestPrepareUserRequest_AppendsAllowedToolsNotice(t *testing.T) {
	t.Parallel()

	got := codec.PrepareUserRequest(codec.UserRequest{
		Prompt:       "Review this file.",
		AllowedTools: core.DedupeSorted([]string{" read ", "GREP", "read"}),
	}).Prompt
	want := "<system_info>For this specific user request the only tools that are allowed are: grep, read.</system_info>"
	if !strings.Contains(got, want) {
		t.Fatalf("prompt missing notice.\nGot: %q\nWant to contain: %q", got, want)
	}
}

func TestBuildSystemPrompt_IncludesToolPreferencesSection(t *testing.T) {
	t.Parallel()

	got := BuildSystemPrompt()
	if !strings.Contains(got, "<tool-preferences>") {
		t.Fatalf("system prompt missing tool-preferences section")
	}
	if !strings.Contains(got, "</tool-preferences>") {
		t.Fatalf("system prompt missing tool-preferences closing tag")
	}
	if !strings.Contains(got, "Use the read tool instead of shelling out with cat/sed/head/tail/ls") {
		t.Fatalf("system prompt missing read preference")
	}
	if !strings.Contains(got, "Use the webfetch tool instead of curl/wget in bash") {
		t.Fatalf("system prompt missing webfetch preference")
	}
}

func TestToolPreferenceFor_BuiltinAndPluginDefaults(t *testing.T) {
	t.Parallel()

	if got := toolPreferenceFor("grep"); !strings.Contains(got, "instead of running grep/ripgrep in bash") {
		t.Fatalf("grep preference mismatch: %q", got)
	}
	if got := toolPreferenceFor("myplugin"); got != "Use the myplugin tool instead of using bash or shell scripts as equivalents for that task." {
		t.Fatalf("plugin preference mismatch: %q", got)
	}
}
