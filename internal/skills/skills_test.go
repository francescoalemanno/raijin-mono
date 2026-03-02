package skills

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/francescoalemanno/raijin-mono/internal/paths"
)

func TestParseSkillHeader_LLMDescription(t *testing.T) {
	t.Parallel()

	content := `---
description: User-visible description
LLMDescription: Model-visible description
---

# Heading
Body`

	desc, llmDesc := parseSkillHeader(content)
	if desc != "User-visible description" {
		t.Fatalf("description = %q, want %q", desc, "User-visible description")
	}
	if llmDesc != "Model-visible description" {
		t.Fatalf("llmDescription = %q, want %q", llmDesc, "Model-visible description")
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

	writeSkillFile(t, filepath.Join(paths.UserSkillsDir(), "commit", paths.SkillFileName),
		"---\ndescription: user commit\n---\nuser body")
	writeSkillFile(t, filepath.Join(project, paths.ProjectSkillsDirRel, "commit", paths.SkillFileName),
		"---\ndescription: project commit\n---\nproject body")

	merged := mergedSkillsForTest()
	got, ok := merged["commit"]
	if !ok {
		t.Fatalf("expected commit skill")
	}
	if got.Source != SourceProject {
		t.Fatalf("expected project source to win, got %q", got.Source)
	}
	if got.Description != "project commit" {
		t.Fatalf("expected project description, got %q", got.Description)
	}
}

func TestSkillPrecedence_UserOverEmbedded(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	project := t.TempDir()
	withSkillCwd(t, project)

	writeSkillFile(t, filepath.Join(paths.UserSkillsDir(), "init", paths.SkillFileName),
		"---\ndescription: user init\n---\nuser init body")

	merged := mergedSkillsForTest()
	got, ok := merged["init"]
	if !ok {
		t.Fatalf("expected init skill")
	}
	if got.Source != SourceUser {
		t.Fatalf("expected user source to win over embedded, got %q", got.Source)
	}
	if got.Description != "user init" {
		t.Fatalf("expected user description, got %q", got.Description)
	}
}
