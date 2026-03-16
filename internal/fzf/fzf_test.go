package fzf

import "testing"

func TestExactMatchBest(t *testing.T) {
	t.Parallel()
	r := Match("hello", "hello")
	if !r.Matches {
		t.Fatal("expected match")
	}
	if r.Score != bonusExact {
		t.Fatalf("expected exact bonus %d, got %d", bonusExact, r.Score)
	}
}

func TestEvidenceRanking(t *testing.T) {
	t.Parallel()
	exact := Match("abc", "abc")
	prefix := Match("abc", "abcdef")
	boundary := Match("abc", "xx-abc-yy")
	substring := Match("abc", "xxabczz")
	subseq := Match("abc", "a---b---c")

	scores := []struct {
		name string
		r    Result
	}{
		{"exact", exact},
		{"prefix", prefix},
		{"boundary", boundary},
		{"substring", substring},
		{"subseq", subseq},
	}
	for _, s := range scores {
		if !s.r.Matches {
			t.Fatalf("%s: expected match", s.name)
		}
	}
	for i := 0; i < len(scores)-1; i++ {
		if scores[i].r.Score >= scores[i+1].r.Score {
			t.Fatalf("%s (score=%d) should rank better than %s (score=%d)",
				scores[i].name, scores[i].r.Score,
				scores[i+1].name, scores[i+1].r.Score)
		}
	}
}

func TestNoMatch(t *testing.T) {
	t.Parallel()
	r := Match("xyz", "hello world")
	if r.Matches {
		t.Fatal("expected no match")
	}
}

func TestEmptyQuery(t *testing.T) {
	t.Parallel()
	r := Match("", "anything")
	if !r.Matches {
		t.Fatal("empty query should match everything")
	}
}

func TestCaseInsensitive(t *testing.T) {
	t.Parallel()
	r := Match("ABC", "abcdef")
	if !r.Matches {
		t.Fatal("expected case-insensitive match")
	}
}

func TestMultiToken(t *testing.T) {
	t.Parallel()
	r := Match("foo bar", "foobar baz")
	if !r.Matches {
		t.Fatal("both tokens should match")
	}

	r = Match("foo xyz", "foobar baz")
	if r.Matches {
		t.Fatal("xyz should not match")
	}
}

func TestNegation(t *testing.T) {
	t.Parallel()
	r := Match("foo !bar", "foobar baz")
	if r.Matches {
		t.Fatal("!bar should exclude this")
	}

	r = Match("foo !xyz", "foobar baz")
	if !r.Matches {
		t.Fatal("!xyz should allow this")
	}
}

func TestPrefixAnchor(t *testing.T) {
	t.Parallel()
	r := Match("^foo", "foobar")
	if !r.Matches {
		t.Fatal("^foo should match foobar")
	}

	r = Match("^foo", "barfoo")
	if r.Matches {
		t.Fatal("^foo should not match barfoo")
	}
}

func TestSuffixAnchor(t *testing.T) {
	t.Parallel()
	r := Match("bar$", "foobar")
	if !r.Matches {
		t.Fatal("bar$ should match foobar")
	}

	r = Match("bar$", "barbaz")
	if r.Matches {
		t.Fatal("bar$ should not match barbaz")
	}
}

func TestExactSubstring(t *testing.T) {
	t.Parallel()
	r := Match("'oba", "foobar")
	if !r.Matches {
		t.Fatal("'oba should match as exact substring")
	}

	r = Match("'xyz", "foobar")
	if r.Matches {
		t.Fatal("'xyz should not match")
	}
}

func TestRankOrdering(t *testing.T) {
	t.Parallel()
	items := []string{
		"a---l---p---h---a",
		"xx alpha yy",
		"alpha beta",
		"zzalphazz",
	}
	out := Rank(items, "alpha", func(s string) string { return s })
	if len(out) != len(items) {
		t.Fatalf("expected %d results, got %d", len(items), len(out))
	}
	// "alpha beta" should be first (word-boundary / prefix-ish).
	if out[0] != "alpha beta" {
		t.Fatalf("first=%q, want 'alpha beta'", out[0])
	}
	// The weak subsequence should be last.
	if out[len(out)-1] != "a---l---p---h---a" {
		t.Fatalf("last=%q, want 'a---l---p---h---a'", out[len(out)-1])
	}
}

func TestRankStableForEqualScores(t *testing.T) {
	t.Parallel()
	items := []string{"aaa", "bbb", "ccc"}
	out := Rank(items, "", func(s string) string { return s })
	for i, s := range items {
		if out[i] != s {
			t.Fatalf("position %d: got %q, want %q", i, out[i], s)
		}
	}
}

func TestSubsequenceGapPenalty(t *testing.T) {
	t.Parallel()
	tight := Match("abc", "a_b_c__")
	loose := Match("abc", "a____b____c")
	if !tight.Matches || !loose.Matches {
		t.Fatal("both should match")
	}
	if tight.Score >= loose.Score {
		t.Fatalf("tighter gaps should score better: tight=%d loose=%d", tight.Score, loose.Score)
	}
}

func TestBoundaryStartBonus(t *testing.T) {
	t.Parallel()
	atBoundary := Match("mod", "select-model")
	midWord := Match("mod", "remodel")
	if !atBoundary.Matches || !midWord.Matches {
		t.Fatal("both should match")
	}
	if atBoundary.Score >= midWord.Score {
		t.Fatalf("boundary start should score better: boundary=%d mid=%d", atBoundary.Score, midWord.Score)
	}
}
