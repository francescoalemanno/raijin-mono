package subagents

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/francescoalemanno/raijin-mono/internal/artifacts"
	"github.com/francescoalemanno/raijin-mono/internal/paths"
)

func withCwd(t *testing.T, dir string) {
	t.Helper()
	prev, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir %s: %v", dir, err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(prev)
	})
}

func writeFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func mergedSubagentsForTest() map[string]Subagent {
	all := artifacts.Merge(
		func(s Subagent) string { return s.Name },
		loadSubagentsFromPath("embedded://subagents", artifacts.SourceEmbedded),
		loadSubagentsFromPath(paths.UserSubagentsDir(), artifacts.SourceUser),
		loadSubagentsFromPath(filepath.Join(".", paths.ProjectSubagentsDirRel), artifacts.SourceProject),
	)
	m := make(map[string]Subagent, len(all))
	for _, s := range all {
		m[s.Name] = s
	}
	return m
}

func TestLoadPrecedenceProjectUserEmbedded(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	project := t.TempDir()
	withCwd(t, project)

	writeFile(t,
		filepath.Join(paths.UserSubagentsDir(), "delegate.md"),
		"---\ndescription: User delegate\ntools: [read]\n---\nuser body",
	)
	writeFile(t,
		filepath.Join(project, paths.ProjectSubagentsDirRel, "delegate.md"),
		"---\ndescription: Project delegate\ntools: [grep]\n---\nproject body",
	)

	merged := mergedSubagentsForTest()
	delegate, ok := merged["delegate"]
	if !ok {
		t.Fatalf("expected delegate subagent")
	}
	if delegate.Source != artifacts.SourceProject {
		t.Fatalf("delegate source = %q, want %q", delegate.Source, artifacts.SourceProject)
	}
	if !strings.Contains(delegate.Prompt, "project body") {
		t.Fatalf("delegate prompt = %q, want project content", delegate.Prompt)
	}
	if len(delegate.Tools) != 1 || delegate.Tools[0] != "grep" {
		t.Fatalf("delegate tools = %v, want grep", delegate.Tools)
	}
}

func TestParseSubagentFrontmatter(t *testing.T) {
	t.Parallel()

	parsed := parseSubagentFile("review.md", "/tmp/review.md", `---
description: "Review code"
tools:
  - read
  - grep
  - read
---
You are a reviewer.`, artifacts.SourceUser)

	if parsed.Name != "review" {
		t.Fatalf("name = %q, want review", parsed.Name)
	}
	if parsed.Description != "Review code" {
		t.Fatalf("description = %q, want Review code", parsed.Description)
	}
	if parsed.Prompt != "You are a reviewer." {
		t.Fatalf("prompt = %q", parsed.Prompt)
	}
	if got, want := strings.Join(parsed.Tools, ","), "read,grep"; got != want {
		t.Fatalf("tools = %q, want %q", got, want)
	}
}

func TestEmbeddedSubagentsIncludeOracle(t *testing.T) {
	t.Parallel()

	merged := mergedSubagentsForTest()
	oracle, ok := merged["oracle"]
	if !ok {
		t.Fatalf("expected oracle subagent")
	}
	if oracle.Source != artifacts.SourceEmbedded {
		t.Fatalf("oracle source = %q, want %q", oracle.Source, artifacts.SourceEmbedded)
	}
	if !strings.Contains(strings.ToLower(oracle.Description), "strategic technical advisor") {
		t.Fatalf("oracle description = %q", oracle.Description)
	}
	if got, want := strings.Join(oracle.Tools, ","), "glob,grep,read"; got != want {
		t.Fatalf("oracle tools = %q, want %q", got, want)
	}
}
