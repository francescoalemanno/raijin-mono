package chat

import (
	"strings"
	"testing"

	"github.com/francescoalemanno/raijin-mono/libtui/pkg/utils"
)


func TestInfoBar_PriorityOrderWarningModelCtxCwd(t *testing.T) {
	t.Parallel()

	bar := newInfoBar()
	bar.SetParts([]string{
		"warning-msg",
		"(provider) model • thinking",
		"ctx%",
		"cwd",
	})

	// With enough width, all parts in order
	lines := bar.Render(80)
	if len(lines) != 1 {
		t.Fatalf("expected one line, got %d", len(lines))
	}
	line := lines[0]

	plain := utils.StripAnsiCodes(line)
	// Verify order: warning comes first, model before cwd
	warningIdx := strings.Index(plain, "warning-msg")
	modelIdx := strings.Index(plain, "model")
	cwdIdx := strings.Index(plain, "cwd")
	ctxIdx := strings.Index(plain, "ctx%")

	if warningIdx < 0 || modelIdx < 0 || cwdIdx < 0 || ctxIdx < 0 {
		t.Fatalf("expected all parts, got: %q", plain)
	}
	if warningIdx >= modelIdx {
		t.Fatalf("warning should come before model, got indices: warning=%d, model=%d", warningIdx, modelIdx)
	}
	if modelIdx >= cwdIdx {
		t.Fatalf("model should come before cwd, got indices: model=%d, cwd=%d", modelIdx, cwdIdx)
	}
}

func TestInfoBar_DropsRightmostPartsWhenOverflowing(t *testing.T) {
	t.Parallel()

	bar := newInfoBar()
	bar.SetParts([]string{"cwd", "usage", "warning", "model"})

	lines := bar.Render(18)
	if len(lines) != 1 {
		t.Fatalf("expected one line, got %d", len(lines))
	}
	line := lines[0]
	if w := utils.VisibleWidth(line); w > 18 {
		t.Fatalf("line width = %d, want <= 18; line=%q", w, line)
	}
	plain := utils.StripAnsiCodes(line)
	if !strings.Contains(plain, "cwd") || !strings.Contains(plain, "usage") {
		t.Fatalf("expected left parts to remain, got %q", plain)
	}
	if strings.Contains(plain, "model") {
		t.Fatalf("expected rightmost overflow part to be dropped, got %q", plain)
	}
}

func TestInfoBar_SpreadsPartsAcrossWidth(t *testing.T) {
	t.Parallel()

	bar := newInfoBar()
	bar.SetParts([]string{"a", "b", "c"})

	lines := bar.Render(12)
	if len(lines) != 1 {
		t.Fatalf("expected one line, got %d", len(lines))
	}
	line := lines[0]
	if w := utils.VisibleWidth(line); w != 12 {
		t.Fatalf("line width = %d, want 12; line=%q", w, line)
	}
	if !strings.Contains(line, "a") || !strings.Contains(line, "b") || !strings.Contains(line, "c") {
		t.Fatalf("expected all parts in line, got %q", line)
	}
	plain := utils.StripAnsiCodes(line)
	if !strings.Contains(plain, "a") || !strings.Contains(plain, "b") || !strings.Contains(plain, "c") {
		t.Fatalf("expected all parts in plain line, got %q", plain)
	}
	if strings.Count(plain, " ") < 4 {
		t.Fatalf("expected expanded spacing between parts, got %q", plain)
	}
}

func TestInfoBar_SinglePartTruncatesToWidth(t *testing.T) {
	t.Parallel()

	bar := newInfoBar()
	bar.SetParts([]string{"very-long-part"})

	lines := bar.Render(8)
	if len(lines) != 1 {
		t.Fatalf("expected one line, got %d", len(lines))
	}
	line := lines[0]
	if w := utils.VisibleWidth(line); w != 8 {
		t.Fatalf("line width = %d, want 8; line=%q", w, line)
	}
	if !strings.HasPrefix(line, "very-lon") {
		t.Fatalf("line = %q, want prefix %q", line, "very-lon")
	}
}

func TestInfoBar_SetInfoBackCompat(t *testing.T) {
	t.Parallel()

	bar := newInfoBar()
	bar.SetInfo("left", "right")

	lines := bar.Render(20)
	if len(lines) != 1 {
		t.Fatalf("expected one line, got %d", len(lines))
	}
	line := lines[0]
	if !strings.Contains(line, "left") || !strings.Contains(line, "right") {
		t.Fatalf("expected both left and right parts, got %q", line)
	}
}
