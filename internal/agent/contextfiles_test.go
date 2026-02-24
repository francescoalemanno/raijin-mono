package agent

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/francescoalemanno/raijin-mono/internal/artifacts"
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

func TestLoadNearestAgentsFile(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	root := t.TempDir()
	project := filepath.Join(root, "proj")
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", project, err)
	}
	withCwd(t, project)

	writeFile(t, filepath.Join(root, "AGENT.md"), "root agent")
	writeFile(t, filepath.Join(project, "AGENTS.md"), "project agents")

	file, ok := loadNearestAgentsFile()
	if !ok {
		t.Fatalf("expected AGENTS file")
	}
	if file.Name != "AGENTS.md" {
		t.Fatalf("file name = %q, want %q", file.Name, "AGENTS.md")
	}
	if file.Content != "project agents" {
		t.Fatalf("content = %q, want %q", file.Content, "project agents")
	}
	if !SameDir(file.Dir, project) {
		t.Fatalf("dir = %q, want same dir as %q", file.Dir, project)
	}
}

func TestGetAgentsFileFromCache(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	project := t.TempDir()
	withCwd(t, project)
	writeFile(t, filepath.Join(project, "AGENTS.md"), "cached agents")

	if err := artifacts.Reload(); err != nil {
		t.Fatalf("artifacts.Reload() error: %v", err)
	}

	file, ok := GetAgentsFile()
	if !ok {
		t.Fatalf("expected AGENTS file")
	}
	if file.Content != "cached agents" {
		t.Fatalf("content = %q, want %q", file.Content, "cached agents")
	}
}
