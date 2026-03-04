package vfs

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestResolveOSAndEmbedded(t *testing.T) {
	t.Parallel()

	cwd := t.TempDir()
	v := New(cwd)

	osResolved, err := v.Resolve("a/b.txt")
	if err != nil {
		t.Fatalf("resolve os: %v", err)
	}
	if osResolved.Backend != BackendOS {
		t.Fatalf("backend = %q, want %q", osResolved.Backend, BackendOS)
	}
	if osResolved.Path != filepath.Join(cwd, "a", "b.txt") {
		t.Fatalf("path = %q", osResolved.Path)
	}

	embResolved, err := v.Resolve("embedded://skills/commit/SKILL.md")
	if err != nil {
		t.Fatalf("resolve embedded: %v", err)
	}
	if embResolved.Backend != BackendEmbedded {
		t.Fatalf("backend = %q, want %q", embResolved.Backend, BackendEmbedded)
	}
	if embResolved.Path != "skills/commit/SKILL.md" {
		t.Fatalf("path = %q", embResolved.Path)
	}
}

func TestReadEmbeddedAndOS(t *testing.T) {
	t.Parallel()

	cwd := t.TempDir()
	v := New(cwd)

	if err := os.MkdirAll(filepath.Join(cwd, "x"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cwd, "x", "sample.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}

	osData, err := v.ReadFile("x/sample.txt")
	if err != nil {
		t.Fatalf("read os: %v", err)
	}
	if string(osData) != "hello" {
		t.Fatalf("content = %q", string(osData))
	}

	embData, err := v.ReadFile("embedded://skills/commit/SKILL.md")
	if err != nil {
		t.Fatalf("read embedded: %v", err)
	}
	if len(embData) == 0 {
		t.Fatalf("expected embedded content")
	}
}

func TestEmbeddedIsReadOnly(t *testing.T) {
	t.Parallel()

	v := New(t.TempDir())
	if err := v.WriteFile("embedded://skills/new.md", []byte("x"), 0o644); !errors.Is(err, ErrReadOnly) {
		t.Fatalf("err = %v, want ErrReadOnly", err)
	}
	if err := v.MkdirAll("embedded://skills/new", 0o755); !errors.Is(err, ErrReadOnly) {
		t.Fatalf("err = %v, want ErrReadOnly", err)
	}
}

func TestInvalidEmbeddedTraversal(t *testing.T) {
	t.Parallel()

	v := New(t.TempDir())
	_, err := v.Resolve("embedded://skills/../templates/init.md")
	if !errors.Is(err, ErrInvalidPath) {
		t.Fatalf("err = %v, want ErrInvalidPath", err)
	}
}
