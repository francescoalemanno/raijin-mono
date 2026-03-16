package tools

import (
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"

	"github.com/francescoalemanno/raijin-mono/internal/fsutil"
)

func TestEditHelpers_DetectLineEnding(t *testing.T) {
	t.Parallel()

	if got := detectLineEnding("a\r\nb\r\n"); got != "\r\n" {
		t.Fatalf("detectLineEnding(CRLF) = %q, want CRLF", got)
	}
	if got := detectLineEnding("a\nb\n"); got != "\n" {
		t.Fatalf("detectLineEnding(LF) = %q, want LF", got)
	}
	if got := detectLineEnding("no-newline"); got != "\n" {
		t.Fatalf("detectLineEnding(no newline) = %q, want LF", got)
	}
}

func TestEditHelpers_NormalizeAndRestoreLineEndings(t *testing.T) {
	t.Parallel()

	normalized := normalizeToLF("a\r\nb\rc\n")
	if normalized != "a\nb\nc\n" {
		t.Fatalf("normalizeToLF = %q", normalized)
	}
	if got := restoreLineEndings("a\nb\n", "\r\n"); got != "a\r\nb\r\n" {
		t.Fatalf("restoreLineEndings(CRLF) = %q", got)
	}
	if got := restoreLineEndings("a\nb\n", "\n"); got != "a\nb\n" {
		t.Fatalf("restoreLineEndings(LF) = %q", got)
	}
}

func TestEditHelpers_NormalizeForFuzzyMatch(t *testing.T) {
	t.Parallel()

	input := "line one   \n“quoted” and ‘single’ and 1–5 and hello\u00A0world\n"
	got := normalizeForFuzzyMatch(input)
	want := "line one\n\"quoted\" and 'single' and 1-5 and hello world\n"
	if got != want {
		t.Fatalf("normalizeForFuzzyMatch = %q, want %q", got, want)
	}
}

func TestEditHelpers_FuzzyFindText_PrefersExact(t *testing.T) {
	t.Parallel()

	content := "const x = 'exact';\n"
	oldText := "const x = 'exact';"
	res := fuzzyFindText(content, oldText)
	if !res.Found {
		t.Fatalf("expected match")
	}
	if res.UsedFuzzyMatch {
		t.Fatalf("expected exact match, got fuzzy")
	}
	if res.Index != 0 {
		t.Fatalf("index = %d, want 0", res.Index)
	}
	if res.ContentForReplacement != content {
		t.Fatalf("contentForReplacement mismatch")
	}
}

func TestEditHelpers_FuzzyFindText_FallsBackToFuzzy(t *testing.T) {
	t.Parallel()

	content := "console.log(‘hello’);\n"
	oldText := "console.log('hello');"
	res := fuzzyFindText(content, oldText)
	if !res.Found {
		t.Fatalf("expected fuzzy match")
	}
	if !res.UsedFuzzyMatch {
		t.Fatalf("expected fuzzy match flag")
	}
	if !strings.Contains(res.ContentForReplacement, "console.log('hello');") {
		t.Fatalf("unexpected contentForReplacement: %q", res.ContentForReplacement)
	}
}

func TestEditHelpers_StripBOM(t *testing.T) {
	t.Parallel()

	bom, text := stripBom("\uFEFFhello")
	if bom != "\uFEFF" || text != "hello" {
		t.Fatalf("stripBom with BOM = (%q, %q)", bom, text)
	}
	bom, text = stripBom("hello")
	if bom != "" || text != "hello" {
		t.Fatalf("stripBom without BOM = (%q, %q)", bom, text)
	}
}

func TestEditHelpers_GenerateDiffString(t *testing.T) {
	t.Parallel()

	oldContent := "one\ntwo\nthree\n"
	newContent := "one\nTWO\nthree\n"
	res := generateDiffString(oldContent, newContent, 4)
	if res.FirstChangedLine == nil || *res.FirstChangedLine != 2 {
		t.Fatalf("firstChangedLine = %#v, want 2", res.FirstChangedLine)
	}
	if !regexp.MustCompile(`(?m)^-\s+\d+\s+\|\s+two$`).MatchString(res.Diff) {
		t.Fatalf("diff missing removed line: %q", res.Diff)
	}
	if !regexp.MustCompile(`(?m)^\+\s+\d+\s+\|\s+TWO$`).MatchString(res.Diff) {
		t.Fatalf("diff missing added line: %q", res.Diff)
	}
}

func TestEditHelpers_ResolveToCwd(t *testing.T) {
	t.Parallel()

	cwd := filepath.Join("/tmp", "project")
	abs := filepath.Join("/var", "data", "file.txt")
	if runtime.GOOS == "windows" {
		cwd = `C:\tmp\project`
		abs = `C:\var\data\file.txt`
	}

	if got := fsutil.ResolveToCwd(abs, cwd); got != abs {
		t.Fatalf("fsutil.ResolveToCwd(abs) = %q, want %q", got, abs)
	}

	rel := "dir/file.txt"
	want := filepath.Join(cwd, rel)
	if got := fsutil.ResolveToCwd(rel, cwd); got != want {
		t.Fatalf("fsutil.ResolveToCwd(rel) = %q, want %q", got, want)
	}

	if got := fsutil.ExpandPath("@dir/file.txt"); got != "dir/file.txt" {
		t.Fatalf("fsutil.ExpandPath(@...) = %q", got)
	}
	if got := fsutil.ExpandPath("dir\u00A0name/file.txt"); got != "dir name/file.txt" {
		t.Fatalf("fsutil.ExpandPath(unicode space) = %q", got)
	}
}
