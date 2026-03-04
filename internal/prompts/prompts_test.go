package prompts

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

func mergedTemplatesForTest() map[string]Template {
	all := artifacts.Merge(
		func(t Template) string { return t.Name },
		loadTemplatesFromPath("embedded://templates", artifacts.SourceEmbedded),
		loadTemplatesFromPath(paths.UserPromptsDir(), artifacts.SourceUser),
		loadTemplatesFromPath(filepath.Join(".", paths.ProjectPromptsDirRel), artifacts.SourceProject),
	)
	m := make(map[string]Template, len(all))
	for _, t := range all {
		m[t.Name] = t
	}
	return m
}

func TestLoadPrecedenceProjectUserEmbedded(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	project := t.TempDir()
	withCwd(t, project)

	writeFile(t,
		filepath.Join(paths.UserPromptsDir(), "review.md"),
		"---\ndescription: User review\n---\nuser body",
	)
	writeFile(t,
		filepath.Join(project, paths.ProjectPromptsDirRel, "review.md"),
		"---\ndescription: Project review\n---\nproject body",
	)

	merged := mergedTemplatesForTest()
	review, ok := merged["review"]
	if !ok {
		t.Fatalf("expected review template")
	}
	if review.Source != artifacts.SourceProject {
		t.Fatalf("review source = %q, want %q", review.Source, artifacts.SourceProject)
	}
	if !strings.Contains(review.Content, "project body") {
		t.Fatalf("review content = %q, want project content", review.Content)
	}

	init, ok := merged["init"]
	if !ok {
		t.Fatalf("expected embedded init template")
	}
	if init.Source != artifacts.SourceEmbedded {
		t.Fatalf("init source = %q, want %q", init.Source, artifacts.SourceEmbedded)
	}
}

func TestLoadPrecedenceUserOverEmbedded(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	project := t.TempDir()
	withCwd(t, project)

	writeFile(t,
		filepath.Join(paths.UserPromptsDir(), "init.md"),
		"---\ndescription: User init\n---\nuser init body",
	)

	merged := mergedTemplatesForTest()
	init, ok := merged["init"]
	if !ok {
		t.Fatalf("expected init template")
	}
	if init.Source != artifacts.SourceUser {
		t.Fatalf("init source = %q, want %q", init.Source, artifacts.SourceUser)
	}
	if !strings.Contains(init.Content, "user init body") {
		t.Fatalf("init content = %q, want user content", init.Content)
	}
}
