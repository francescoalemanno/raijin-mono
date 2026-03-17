// Package fzf implements fuzzy matching and ranking using junegunn/fzf's
// scoring algorithms while preserving the small matcher surface used by Raijin.
package fzf

import (
	"sort"
	"strings"

	"github.com/junegunn/fzf/src/algo"
	"github.com/junegunn/fzf/src/util"
)

func init() {
	_ = algo.Init("default")
}

// Result holds the outcome of matching a single candidate.
type Result struct {
	Matches bool
	Score   int // higher is better; only meaningful when Matches is true
}

func Match(query, text string) Result {
	q := strings.TrimSpace(query)
	if q == "" {
		return Result{Matches: true, Score: 0}
	}
	tokens := strings.Fields(q)

	total := 0
	for _, tok := range tokens {
		r := matchToken(tok, text)
		if !r.Matches {
			return Result{Matches: false}
		}
		total += r.Score
	}
	return Result{Matches: true, Score: total}
}

// Rank filters and sorts items by fuzzy match quality (best first).
func Rank[T any](items []T, query string, getText func(T) string) []T {
	q := strings.TrimSpace(query)
	if q == "" {
		return items
	}

	type scored struct {
		item  T
		score int
	}
	var results []scored
	for _, item := range items {
		r := Match(q, getText(item))
		if r.Matches {
			results = append(results, scored{item: item, score: r.Score})
		}
	}

	sort.SliceStable(results, func(i, j int) bool {
		return results[i].score > results[j].score
	})

	out := make([]T, len(results))
	for i, r := range results {
		out[i] = r.item
	}
	return out
}

func matchToken(tok, text string) Result {
	if strings.HasPrefix(tok, "!") {
		inner := tok[1:]
		if inner == "" {
			return Result{Matches: true, Score: 0}
		}
		r := matchToken(inner, text)
		if r.Matches {
			return Result{Matches: false}
		}
		return Result{Matches: true, Score: 0}
	}

	caseSensitive := false
	normalized := normalizeToken(tok)
	chars := util.RunesToChars([]rune(text))

	if strings.HasPrefix(tok, "'") {
		inner := tok[1:]
		if inner == "" {
			return Result{Matches: true, Score: 0}
		}
		return runAlgo(algo.ExactMatchNaive, caseSensitive, chars, normalizeToken(inner))
	}

	if strings.HasPrefix(tok, "^") {
		inner := tok[1:]
		if inner == "" {
			return Result{Matches: true, Score: 0}
		}
		return runAlgo(algo.PrefixMatch, caseSensitive, chars, normalizeToken(inner))
	}

	if strings.HasSuffix(tok, "$") {
		inner := tok[:len(tok)-1]
		if inner == "" {
			return Result{Matches: true, Score: 0}
		}
		return runAlgo(algo.SuffixMatch, caseSensitive, chars, normalizeToken(inner))
	}

	return runAlgo(algo.FuzzyMatchV2, caseSensitive, chars, normalized)
}

func runAlgo(fn algo.Algo, caseSensitive bool, chars util.Chars, pattern []rune) Result {
	res, _ := fn(caseSensitive, true, true, &chars, pattern, false, nil)
	if res.Start < 0 {
		return Result{Matches: false}
	}
	score := res.Score*1_000_000 - res.Start*1_000 - len(chars.ToRunes())
	return Result{Matches: true, Score: score}
}

func normalizeToken(tok string) []rune {
	lower := strings.ToLower(tok)
	return algo.NormalizeRunes([]rune(lower))
}
