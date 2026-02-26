package fuzzy

import "testing"

func TestMatchEvidenceRanking(t *testing.T) {
	t.Parallel()

	exact := Match("abc", "abc")
	prefix := Match("abc", "abcdef")
	boundary := Match("abc", "xx abc yy")
	substring := Match("abc", "xxzabczz")
	weak := Match("abc", "a---b---c")

	if !exact.Matches || !prefix.Matches || !boundary.Matches || !substring.Matches || !weak.Matches {
		t.Fatalf("expected all forms to match")
	}
	if !(exact.Score < prefix.Score) {
		t.Fatalf("exact should outrank prefix: %v >= %v", exact.Score, prefix.Score)
	}
	if !(prefix.Score < boundary.Score) {
		t.Fatalf("prefix should outrank boundary: %v >= %v", prefix.Score, boundary.Score)
	}
	if !(boundary.Score < substring.Score) {
		t.Fatalf("boundary should outrank substring: %v >= %v", boundary.Score, substring.Score)
	}
	if !(substring.Score < weak.Score) {
		t.Fatalf("substring should outrank weak subsequence: %v >= %v", substring.Score, weak.Score)
	}
}

func TestSwappedAlphaNumericSupportedButWeakerThanDirect(t *testing.T) {
	t.Parallel()
	direct := Match("codex52", "codex52")
	swapped := Match("codex52", "gpt-5.2-codex")
	if !direct.Matches || !swapped.Matches {
		t.Fatalf("both direct and swapped should match")
	}
	if !(direct.Score < swapped.Score) {
		t.Fatalf("direct should outrank swapped evidence: %v >= %v", direct.Score, swapped.Score)
	}
}

func TestFuzzyFilterStrictLikeResultsComeFirst(t *testing.T) {
	t.Parallel()
	items := []string{"alpha beta", "xx alpha yy", "zzalphazz", "a----l----p----h----a"}
	out := FuzzyFilter(items, "alpha", func(s string) string { return s })
	if len(out) != len(items) {
		t.Fatalf("len(out)=%d, want %d", len(out), len(items))
	}
	if out[0] != "alpha beta" {
		t.Fatalf("first=%q, want alpha beta", out[0])
	}
	if out[len(out)-1] != "a----l----p----h----a" {
		t.Fatalf("last=%q, want weakest subsequence", out[len(out)-1])
	}
}
