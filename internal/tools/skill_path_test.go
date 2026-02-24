package tools

import (
	"testing"
)

func TestRegisterSkillScriptsPath(t *testing.T) {
	t.Parallel()

	paths := NewPathRegistry()
	RegisterSkillScriptsPath(paths, " /tmp/skill-scripts ")
	RegisterSkillScriptsPath(paths, "/tmp/skill-scripts")
	RegisterSkillScriptsPath(paths, "")

	got := paths.Paths()
	if len(got) != 1 {
		t.Fatalf("paths length = %d, want 1", len(got))
	}
	if got[0] != "/tmp/skill-scripts" {
		t.Fatalf("first path = %q, want %q", got[0], "/tmp/skill-scripts")
	}
}

func TestRegisterSkillScriptsPathNilRegistry(t *testing.T) {
	t.Parallel()
	RegisterSkillScriptsPath(nil, "/tmp/skill-scripts")
}
