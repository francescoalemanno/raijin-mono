package prompts

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

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
		filepath.Join(project, projectPromptsDirRel, "review.md"),
		"---\ndescription: Project review\n---\nproject body",
	)

	res := Load()
	review, ok := res.Find("review")
	if !ok {
		t.Fatalf("expected review template")
	}
	if review.Source != SourceProject {
		t.Fatalf("review source = %q, want %q", review.Source, SourceProject)
	}
	if !strings.Contains(review.Content, "project body") {
		t.Fatalf("review content = %q, want project content", review.Content)
	}

	plan, ok := res.Find("plan")
	if !ok {
		t.Fatalf("expected embedded plan template")
	}
	if plan.Source != SourceEmbedded {
		t.Fatalf("plan source = %q, want %q", plan.Source, SourceEmbedded)
	}
}

func TestLoadPrecedenceUserOverEmbedded(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	project := t.TempDir()
	withCwd(t, project)

	writeFile(t,
		filepath.Join(paths.UserPromptsDir(), "plan.md"),
		"---\ndescription: User plan\n---\nuser plan body",
	)

	res := Load()
	plan, ok := res.Find("plan")
	if !ok {
		t.Fatalf("expected plan template")
	}
	if plan.Source != SourceUser {
		t.Fatalf("plan source = %q, want %q", plan.Source, SourceUser)
	}
	if !strings.Contains(plan.Content, "user plan body") {
		t.Fatalf("plan content = %q, want user content", plan.Content)
	}
}

func TestAllowedToolsFrontmatterParsing(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	project := t.TempDir()
	withCwd(t, project)

	writeFile(t,
		filepath.Join(project, projectPromptsDirRel, "gated.md"),
		"---\ndescription: gated\nallowed-tools:\n  - read\n  - GREP\n  - read\n---\nbody",
	)

	res := Load()
	tmpl, ok := res.Find("gated")
	if !ok {
		t.Fatalf("expected gated template")
	}
	want := []string{"read", "grep"}
	if !reflect.DeepEqual(tmpl.AllowedTools, want) {
		t.Fatalf("allowed_tools = %#v, want %#v", tmpl.AllowedTools, want)
	}
}

func TestAllowedToolsFrontmatterSpaceDelimited(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	project := t.TempDir()
	withCwd(t, project)

	writeFile(t,
		filepath.Join(project, projectPromptsDirRel, "space.md"),
		"---\ndescription: spaced\nallowed-tools: read GREP webfetch\n---\nbody",
	)

	res := Load()
	tmpl, ok := res.Find("space")
	if !ok {
		t.Fatalf("expected space template")
	}
	want := []string{"read", "grep", "webfetch"}
	if !reflect.DeepEqual(tmpl.AllowedTools, want) {
		t.Fatalf("allowed_tools = %#v, want %#v", tmpl.AllowedTools, want)
	}
}
