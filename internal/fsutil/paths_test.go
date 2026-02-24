package fsutil

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExpandPath_StripsAtPrefixAndNormalizesUnicodeSpaces(t *testing.T) {
	t.Parallel()

	if got := ExpandPath("@dir/file.txt"); got != "dir/file.txt" {
		t.Fatalf("ExpandPath(@...) = %q", got)
	}
	if got := ExpandPath("dir\u00A0name/file.txt"); got != "dir name/file.txt" {
		t.Fatalf("ExpandPath(unicode space) = %q", got)
	}
}

func TestExpandPath_ExpandsHome(t *testing.T) {
	t.Parallel()

	home, err := os.UserHomeDir()
	if err != nil {
		t.Skipf("user home unavailable: %v", err)
	}

	if got := ExpandPath("~"); got != home {
		t.Fatalf("ExpandPath(~) = %q, want %q", got, home)
	}

	got := ExpandPath("~/tmp")
	if !strings.HasPrefix(got, home) {
		t.Fatalf("ExpandPath(~/tmp) = %q, expected prefix %q", got, home)
	}
	if !strings.Contains(got, "tmp") {
		t.Fatalf("ExpandPath(~/tmp) = %q, expected tmp suffix", got)
	}
}

func TestResolveToCwd_RelativeAndAbsolute(t *testing.T) {
	t.Parallel()

	cwd := filepath.Join("/tmp", "project")
	abs := filepath.Join("/var", "data", "file.txt")
	if got := ResolveToCwd(abs, cwd); got != abs {
		t.Fatalf("ResolveToCwd(abs) = %q, want %q", got, abs)
	}

	rel := "dir/file.txt"
	want := filepath.Join(cwd, rel)
	if got := ResolveToCwd(rel, cwd); got != want {
		t.Fatalf("ResolveToCwd(rel) = %q, want %q", got, want)
	}
}
