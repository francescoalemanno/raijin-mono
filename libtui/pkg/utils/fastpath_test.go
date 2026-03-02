package utils

import "testing"

func TestVisibleWidthASCIIWithANSIAndTab(t *testing.T) {
	t.Parallel()

	input := "\x1b[31mhello\tworld\x1b[0m"
	if got, want := VisibleWidth(input), 13; got != want {
		t.Fatalf("VisibleWidth(%q) = %d, want %d", input, got, want)
	}
}

func TestVisibleWidthNonASCIIFallback(t *testing.T) {
	t.Parallel()

	input := "caf\u00e9"
	if got, want := VisibleWidth(input), 4; got != want {
		t.Fatalf("VisibleWidth(%q) = %d, want %d", input, got, want)
	}
}

func TestWrapTextWithAnsiASCIIWordBreak(t *testing.T) {
	t.Parallel()

	input := "\x1b[32msupercalifragilisticexpialidocious\x1b[0m tail"
	lines := WrapTextWithAnsi(input, 8)
	if len(lines) < 2 {
		t.Fatalf("expected wrapped output, got %v", lines)
	}
	for _, line := range lines {
		if got := VisibleWidth(line); got > 8 {
			t.Fatalf("line width %d exceeds limit in %q", got, line)
		}
	}
}
