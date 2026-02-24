package skills

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/francescoalemanno/raijin-mono/internal/paths"
	"github.com/francescoalemanno/raijin-mono/internal/substitution"
)

func TestParseSkillMarkdown_LLMDescription(t *testing.T) {
	t.Parallel()

	content := `---
description: User-visible description
LLMDescription: Model-visible description
---

# Heading
Body`

	desc, llmDesc, hideFromLLM, body := parseSkillMarkdown(content)
	if desc != "User-visible description" {
		t.Fatalf("description = %q, want %q", desc, "User-visible description")
	}
	if llmDesc != "Model-visible description" {
		t.Fatalf("llmDescription = %q, want %q", llmDesc, "Model-visible description")
	}
	if hideFromLLM {
		t.Fatalf("hideFromLLM = true, want false")
	}
	if body != "# Heading\nBody" {
		t.Fatalf("body = %q, want %q", body, "# Heading\nBody")
	}
}

func TestParseSkillMarkdown_HideFromLLMTrue(t *testing.T) {
	t.Parallel()

	content := `---
description: hidden
hide-from-llm: true
---
body`

	_, _, hideFromLLM, _ := parseSkillMarkdown(content)
	if !hideFromLLM {
		t.Fatalf("hideFromLLM = false, want true")
	}
}

func TestSkillPromptDescription(t *testing.T) {
	t.Parallel()

	withLLMDescription := Skill{
		Description:    "User-visible description",
		LLMDescription: "Model-visible description",
	}
	if got := withLLMDescription.PromptDescription(); got != "Model-visible description" {
		t.Fatalf("PromptDescription() = %q, want %q", got, "Model-visible description")
	}

	withoutLLMDescription := Skill{
		Description: "User-visible description",
	}
	if got := withoutLLMDescription.PromptDescription(); got != "User-visible description" {
		t.Fatalf("PromptDescription() = %q, want %q", got, "User-visible description")
	}
}

func TestExpandAll_SkillArgModeText(t *testing.T) {
	t.Parallel()

	got := substitution.ExpandAll(context.Background(), "named={{ARGUMENTS}} dollar=$ARGUMENTS at=$@ idx=$1 slice=${@:2} escaped=\\$@ literal=\\{{ARGUMENTS}}", "commit all changes", substitution.ArgModeText)
	want := "named=commit all changes dollar=commit all changes at=commit all changes idx=$1 slice=${@:2} escaped=$@ literal={{ARGUMENTS}}"
	if got != want {
		t.Fatalf("ExpandAll(ArgModeText) = %q, want %q", got, want)
	}
}

func TestSkillShouldAdvertiseToLLM(t *testing.T) {
	t.Parallel()

	defaultSkill := Skill{}
	if !defaultSkill.ShouldAdvertiseToLLM() {
		t.Fatalf("default ShouldAdvertiseToLLM() = false, want true")
	}

	hidden := Skill{HideFromLLM: true}
	if hidden.ShouldAdvertiseToLLM() {
		t.Fatalf("hidden ShouldAdvertiseToLLM() = true, want false")
	}

	explicitShown := Skill{HideFromLLM: false}
	if !explicitShown.ShouldAdvertiseToLLM() {
		t.Fatalf("explicit shown ShouldAdvertiseToLLM() = false, want true")
	}
}

func withSkillCwd(t *testing.T, dir string) {
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

func writeSkillFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func mergedSkillsForTest() map[string]Skill {
	m := make(map[string]Skill)
	for _, s := range loadEmbeddedSkills() {
		m[s.Name] = s
	}
	for _, s := range loadExternalSkillsMerged() {
		m[s.Name] = s
	}
	return m
}

func TestSkillPrecedence_ProjectOverUserOverEmbedded(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	project := t.TempDir()
	withSkillCwd(t, project)

	writeSkillFile(t, filepath.Join(paths.UserSkillsDir(), "commit", externalSkillFile),
		"---\ndescription: user commit\n---\nuser body")
	writeSkillFile(t, filepath.Join(project, paths.ProjectSkillsDirRel, "commit", externalSkillFile),
		"---\ndescription: project commit\n---\nproject body")

	merged := mergedSkillsForTest()
	got, ok := merged["commit"]
	if !ok {
		t.Fatalf("expected commit skill")
	}
	if !strings.Contains(got.Content, "project body") {
		t.Fatalf("expected project content to win, got %q", got.Content)
	}
}

func TestSkillPrecedence_UserOverEmbedded(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	project := t.TempDir()
	withSkillCwd(t, project)

	writeSkillFile(t, filepath.Join(paths.UserSkillsDir(), "init", externalSkillFile),
		"---\ndescription: user init\n---\nuser init body")

	merged := mergedSkillsForTest()
	got, ok := merged["init"]
	if !ok {
		t.Fatalf("expected init skill")
	}
	if !strings.Contains(got.Content, "user init body") {
		t.Fatalf("expected user content to win over embedded, got %q", got.Content)
	}
}
