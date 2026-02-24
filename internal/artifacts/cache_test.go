package artifacts

import (
	"os"
	"testing"
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

func TestManagerLoadReloadAndGetAll(t *testing.T) {
	m := NewManager()
	loadCount := 0

	m.Register(KindSkill, func() ([]Item, error) {
		loadCount++
		return []Item{{Name: "commit", Value: "skill:commit"}}, nil
	})

	if err := m.Load(); err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if loadCount != 1 {
		t.Fatalf("load count = %d, want 1", loadCount)
	}

	items := m.GetAll(KindSkill)
	if len(items) != 1 {
		t.Fatalf("GetAll(skill) len = %d, want 1", len(items))
	}
	if items[0].Name != "commit" {
		t.Fatalf("item name = %q, want %q", items[0].Name, "commit")
	}

	// Returned slices are copies.
	items[0].Name = "changed"
	again := m.GetAll(KindSkill)
	if again[0].Name != "commit" {
		t.Fatalf("cache mutated through returned slice: got %q", again[0].Name)
	}

	if err := m.Reload(); err != nil {
		t.Fatalf("Reload() error: %v", err)
	}
	if loadCount != 2 {
		t.Fatalf("load count = %d, want 2", loadCount)
	}
}

func TestManagerLoadDoesNotAutoReloadOnContextChange(t *testing.T) {
	m := NewManager()
	loadCount := 0

	m.Register(KindSkill, func() ([]Item, error) {
		loadCount++
		return []Item{{Name: "x", Value: loadCount}}, nil
	})

	withCwd(t, t.TempDir())
	_ = m.GetAll(KindSkill)
	if loadCount != 1 {
		t.Fatalf("initial load count = %d, want 1", loadCount)
	}

	if err := os.Chdir(t.TempDir()); err != nil {
		t.Fatalf("chdir: %v", err)
	}

	_ = m.GetAll(KindSkill)
	if loadCount != 1 {
		t.Fatalf("load count after cwd change = %d, want 1", loadCount)
	}

	if err := m.Reload(); err != nil {
		t.Fatalf("Reload() error: %v", err)
	}
	if loadCount != 2 {
		t.Fatalf("load count after explicit reload = %d, want 2", loadCount)
	}
}
