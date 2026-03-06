package agent

import (
	"strings"
	"testing"
)

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

func TestBuildSystemPrompt_IncludesToolParameterDetails(t *testing.T) {
	t.Parallel()

	got := BuildSystemPrompt()
	if !strings.Contains(got, "Parameters:") {
		t.Fatalf("system prompt missing tool parameters section")
	}
	if !strings.Contains(got, "- `path` (string, required): Path to the file or directory to read (relative or absolute)") {
		t.Fatalf("system prompt missing required path parameter details")
	}
	if !strings.Contains(got, "- `offset` (integer, optional): Line number to start reading from (1-indexed)") {
		t.Fatalf("system prompt missing optional offset parameter details")
	}
}
