package llm

import "testing"

func TestNormalizeThinkingLevel_KnownLevel(t *testing.T) {
	t.Parallel()

	got := NormalizeThinkingLevel(ThinkingLevelHigh)
	if got != ThinkingLevelHigh {
		t.Fatalf("NormalizeThinkingLevel = %q, want %q", got, ThinkingLevelHigh)
	}
}

func TestNormalizeThinkingLevel_AliasXHigh(t *testing.T) {
	t.Parallel()

	got := NormalizeThinkingLevel("xhigh")
	if got != ThinkingLevelMax {
		t.Fatalf("NormalizeThinkingLevel = %q, want %q", got, ThinkingLevelMax)
	}
}

func TestNormalizeThinkingLevel_EmptyIsOff(t *testing.T) {
	t.Parallel()

	got := NormalizeThinkingLevel("")
	if got != ThinkingLevelOff {
		t.Fatalf("NormalizeThinkingLevel = %q, want %q", got, ThinkingLevelOff)
	}
}
