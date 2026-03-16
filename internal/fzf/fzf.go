// Package fzf implements an fzf-style fuzzy matching and ranking algorithm.
//
// Queries are split on whitespace into tokens. Every token must match the
// candidate text for it to pass. Tokens support prefix-anchoring (^tok),
// suffix-anchoring (tok$), exact substring ('tok), and negation (!tok).
// Plain tokens use fuzzy subsequence matching with quality-based scoring.
//
// Lower score = better match.
package fzf

import (
	"sort"
	"strings"
)

// Result holds the outcome of matching a single candidate.
type Result struct {
	Matches bool
	Score   int // lower is better; only meaningful when Matches is true
}

// Match checks whether query matches text using fzf-style rules.
// The query is split into whitespace-separated tokens. All tokens must match.
func Match(query, text string) Result {
	q := strings.TrimSpace(query)
	if q == "" {
		return Result{Matches: true, Score: 0}
	}
	tLower := strings.ToLower(text)
	tokens := strings.Fields(strings.ToLower(q))

	total := 0
	for _, tok := range tokens {
		r := matchToken(tok, tLower)
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
		return results[i].score < results[j].score
	})

	out := make([]T, len(results))
	for i, r := range results {
		out[i] = r.item
	}
	return out
}

// ---------------------------------------------------------------------------
// Token matching
// ---------------------------------------------------------------------------

func matchToken(tok, textLower string) Result {
	// Negation: !term — matches when term is NOT found.
	if strings.HasPrefix(tok, "!") {
		inner := tok[1:]
		if inner == "" {
			return Result{Matches: true, Score: 0}
		}
		r := matchToken(inner, textLower)
		if r.Matches {
			return Result{Matches: false}
		}
		return Result{Matches: true, Score: 0}
	}

	// Exact substring: 'term
	if strings.HasPrefix(tok, "'") {
		inner := tok[1:]
		if inner == "" {
			return Result{Matches: true, Score: 0}
		}
		idx := strings.Index(textLower, inner)
		if idx < 0 {
			return Result{Matches: false}
		}
		return Result{Matches: true, Score: scoreSubstring(idx, len(inner), len(textLower))}
	}

	// Prefix anchor: ^term
	if strings.HasPrefix(tok, "^") {
		inner := tok[1:]
		if inner == "" {
			return Result{Matches: true, Score: 0}
		}
		if !strings.HasPrefix(textLower, inner) {
			return Result{Matches: false}
		}
		return Result{Matches: true, Score: scorePrefix(len(inner), len(textLower))}
	}

	// Suffix anchor: term$
	if strings.HasSuffix(tok, "$") {
		inner := tok[:len(tok)-1]
		if inner == "" {
			return Result{Matches: true, Score: 0}
		}
		if !strings.HasSuffix(textLower, inner) {
			return Result{Matches: false}
		}
		return Result{Matches: true, Score: scoreSuffix(len(inner), len(textLower))}
	}

	// Plain fuzzy token.
	return fuzzyMatch(tok, textLower)
}

// ---------------------------------------------------------------------------
// Fuzzy subsequence matching with scoring
// ---------------------------------------------------------------------------

// Bonus/penalty constants (tuned to produce intuitive orderings).
const (
	bonusExact         = -200 // full exact match
	bonusPrefix        = -120 // starts with query
	bonusSuffix        = -100 // ends with query
	bonusWordBoundary  = -80  // match starts at a word boundary
	bonusSubstring     = -40  // contiguous substring anywhere
	bonusSubseqBase    = -10  // ordered subsequence found
	bonusBoundaryStart = -15  // first char of subsequence at boundary
	bonusConsecutive   = -3   // per consecutive matched char in subsequence
	penaltyGap         = 4    // per gap char between matched chars
	penaltyLateStart   = 1    // per char before first match
	penaltyLengthDiff  = 1    // per extra char in text beyond query
)

func fuzzyMatch(query, textLower string) Result {
	if query == textLower {
		return Result{Matches: true, Score: bonusExact}
	}

	best := Result{Matches: false}

	// Prefix match.
	if strings.HasPrefix(textLower, query) {
		s := bonusPrefix + (len(textLower)-len(query))*penaltyLengthDiff
		best = betterResult(best, Result{Matches: true, Score: s})
	}

	// Suffix match.
	if strings.HasSuffix(textLower, query) {
		s := bonusSuffix + (len(textLower)-len(query))*penaltyLengthDiff
		best = betterResult(best, Result{Matches: true, Score: s})
	}

	// Contiguous substring.
	if idx := strings.Index(textLower, query); idx >= 0 {
		s := scoreSubstring(idx, len(query), len(textLower))
		if idx > 0 && isBoundary(textLower[idx-1]) {
			s = bonusWordBoundary + idx*penaltyLateStart + (len(textLower)-len(query))*penaltyLengthDiff
		}
		best = betterResult(best, Result{Matches: true, Score: s})
	}

	// Subsequence match (greedy with scoring).
	if r := subsequenceMatch(query, textLower); r.Matches {
		best = betterResult(best, r)
	}

	return best
}

func subsequenceMatch(query, textLower string) Result {
	qi := 0
	firstPos := -1
	gaps := 0
	consecutive := 0
	totalConsecutive := 0
	lastMatch := -1

	for i := 0; i < len(textLower) && qi < len(query); i++ {
		if textLower[i] == query[qi] {
			if firstPos < 0 {
				firstPos = i
			}
			if lastMatch >= 0 && i == lastMatch+1 {
				consecutive++
			} else {
				totalConsecutive += consecutive
				consecutive = 0
				if lastMatch >= 0 {
					gaps += i - lastMatch - 1
				}
			}
			lastMatch = i
			qi++
		}
	}
	if qi < len(query) {
		return Result{Matches: false}
	}

	totalConsecutive += consecutive

	score := bonusSubseqBase +
		firstPos*penaltyLateStart +
		gaps*penaltyGap +
		totalConsecutive*bonusConsecutive +
		(len(textLower)-len(query))*penaltyLengthDiff

	if firstPos == 0 || (firstPos > 0 && isBoundary(textLower[firstPos-1])) {
		score += bonusBoundaryStart
	}

	return Result{Matches: true, Score: score}
}

// ---------------------------------------------------------------------------
// Scoring helpers
// ---------------------------------------------------------------------------

func scorePrefix(qLen, tLen int) int {
	return bonusPrefix + (tLen-qLen)*penaltyLengthDiff
}

func scoreSuffix(qLen, tLen int) int {
	return bonusSuffix + (tLen-qLen)*penaltyLengthDiff
}

func scoreSubstring(idx, qLen, tLen int) int {
	return bonusSubstring + idx*penaltyLateStart + (tLen-qLen)*penaltyLengthDiff
}

func betterResult(a, b Result) Result {
	if !a.Matches {
		return b
	}
	if !b.Matches {
		return a
	}
	if b.Score < a.Score {
		return b
	}
	return a
}

func isBoundary(c byte) bool {
	switch c {
	case ' ', '\t', '\n', '\r', '-', '_', '.', '/', ':', ',', ';', '(', ')', '[', ']':
		return true
	}
	return false
}
