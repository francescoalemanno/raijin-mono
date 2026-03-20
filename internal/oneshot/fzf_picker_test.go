package oneshot

import "testing"

func TestBuildFZFPickerLinesMakesDuplicateLabelsUnique(t *testing.T) {
	t.Parallel()

	lines, lineToKey := buildFZFPickerLines([]fzfPickerItem{
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
}

func TestBuildFZFPickerLinesPreservesLeadingWhitespace(t *testing.T) {
	t.Parallel()

	lines, _ := buildFZFPickerLines([]fzfPickerItem{
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
	lines, lineToKey := buildFZFPickerLines(items)

	if got, want := pickerLinePosition(lines, lineToKey, "b"), 2; got != want {
		t.Fatalf("pickerLinePosition(...) = %d, want %d", got, want)
	}
}
