package oneshot

import "testing"

func TestBuildFZFPickerLinesMakesDuplicateLabelsUnique(t *testing.T) {
	t.Parallel()

	lines, lineToKey, previewEnabled := buildFZFPickerLines([]fzfPickerItem{
		{key: "a", label: "same"},
		{key: "b", label: "same"},
		{key: "c", label: "same"},
	})

	if len(lines) != 3 {
		t.Fatalf("len(lines) = %d, want 3", len(lines))
	}
	if len(lineToKey) != 3 {
		t.Fatalf("len(lineToKey) = %d, want 3", len(lineToKey))
	}

	seen := make(map[string]bool, len(lines))
	for _, line := range lines {
		if seen[line] {
			t.Fatalf("line %q appears more than once", line)
		}
		seen[line] = true
		if _, ok := lineToKey[line]; !ok {
			t.Fatalf("line %q missing key mapping", line)
		}
	}
	if previewEnabled {
		t.Fatalf("previewEnabled = true, want false")
	}
}

func TestBuildFZFPickerLinesPreservesLeadingWhitespace(t *testing.T) {
	t.Parallel()

	lines, _, _ := buildFZFPickerLines([]fzfPickerItem{
		{key: "a", label: "   └─ + child"},
	})

	if len(lines) != 1 {
		t.Fatalf("len(lines) = %d, want 1", len(lines))
	}
	if got, want := lines[0], "   └─ + child"; got != want {
		t.Fatalf("line = %q, want %q", got, want)
	}
}

func TestPickerLinePositionUsesResolvedLines(t *testing.T) {
	t.Parallel()

	items := []fzfPickerItem{
		{key: "a", label: "same"},
		{key: "b", label: "same"},
		{key: "c", label: "other"},
	}
	lines, lineToKey, _ := buildFZFPickerLines(items)

	if got, want := pickerLinePosition(lines, lineToKey, "b"), 2; got != want {
		t.Fatalf("pickerLinePosition(...) = %d, want %d", got, want)
	}
}

func TestBuildFZFPickerLinesIncludesEncodedPreview(t *testing.T) {
	t.Parallel()

	lines, lineToKey, previewEnabled := buildFZFPickerLines([]fzfPickerItem{
		{key: "help", label: "/help", preview: "/help\n\nShow help"},
	})

	if !previewEnabled {
		t.Fatalf("previewEnabled = false, want true")
	}
	if len(lines) != 1 {
		t.Fatalf("len(lines) = %d, want 1", len(lines))
	}
	if got, want := lines[0], "/help\t/help\\n\\nShow help"; got != want {
		t.Fatalf("line = %q, want %q", got, want)
	}
	if got, want := lineToKey[lines[0]], "help"; got != want {
		t.Fatalf("lineToKey[...] = %q, want %q", got, want)
	}
}
