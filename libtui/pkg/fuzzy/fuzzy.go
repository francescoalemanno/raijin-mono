package fuzzy

import (
	"sort"
	"strings"
	"unicode"
)

// FuzzyMatch represents the result of a fuzzy match
type FuzzyMatch struct {
	Matches bool
	Score   float64
}

type scoreWeights struct {
	scoreExactFull           float64
	scoreExactStart          float64
	scoreWordBoundaryHit     float64
	scoreSubstringHit        float64
	scoreWeakSubseqBase      float64
	bonusSubseqFirstBoundary float64
	penaltyLatePosFactor     float64
	penaltyGapFactor         float64
	penaltyPrefixTailFactor  float64
	penaltySwappedToken      float64
}

var defaultScoreWeights = scoreWeights{
	scoreExactFull:           -100.0,
	scoreExactStart:          -60.0,
	scoreWordBoundaryHit:     -40.0,
	scoreSubstringHit:        -3.0,
	scoreWeakSubseqBase:      -5.0,
	bonusSubseqFirstBoundary: -8.0,
	penaltyLatePosFactor:     1.0,
	penaltyGapFactor:         2.0,
	penaltyPrefixTailFactor:  0.5,
	penaltySwappedToken:      15.0,
}

// Match checks if query matches text using additive heuristic weights.
// Stronger evidence gets lower (better) scores, while later and gappier
// matches incur penalties. Lower score = better match.
func Match(query, text string) FuzzyMatch {
	return matchWithWeights(query, text, defaultScoreWeights)
}

func matchWithWeights(query, text string, w scoreWeights) FuzzyMatch {
	q := strings.ToLower(strings.TrimSpace(query))
	t := strings.ToLower(text)
	if q == "" {
		return FuzzyMatch{Matches: true, Score: 0}
	}
	if t == "" {
		return FuzzyMatch{Matches: false, Score: 0}
	}

	primary := matchCoreWithWeights(q, t, w)
	if primary.Matches {
		return primary
	}

	// Try swapping alphanumeric tokens (e.g., "codex52" -> "52codex").
	swapped, ok := swappedAlphaNumericToken(q)
	if !ok {
		return primary
	}
	swappedMatch := matchCoreWithWeights(swapped, t, w)
	if !swappedMatch.Matches {
		return primary
	}
	// Swapped-token evidence is weaker than direct evidence.
	return FuzzyMatch{Matches: true, Score: swappedMatch.Score + w.penaltySwappedToken}
}

func matchCoreWithWeights(q, t string, w scoreWeights) FuzzyMatch {
	score := 0.0
	matches := false

	if t == q {
		score += w.scoreExactFull
		matches = true
	}

	if strings.HasPrefix(t, q) {
		tail := len(t) - len(q)
		score += w.scoreExactStart + float64(tail)*w.penaltyPrefixTailFactor
		matches = true
	}

	if idx := strings.Index(t, q); idx >= 0 {
		if idx == 0 || isWordBoundaryChar(rune(t[idx-1])) {
			score += w.scoreWordBoundaryHit + float64(idx)*w.penaltyLatePosFactor
		} else {
			score += w.scoreSubstringHit + float64(idx)*w.penaltyLatePosFactor
		}
		matches = true
	}

	// Weakest evidence: ordered subsequence match with penalties.
	subseqMatch, firstPos, gaps, firstBoundary := subsequenceEvidence(q, t)
	if subseqMatch {
		score += w.scoreWeakSubseqBase + float64(firstPos)*w.penaltyLatePosFactor + float64(gaps)*w.penaltyGapFactor
		if firstBoundary {
			score += w.bonusSubseqFirstBoundary
		}
		matches = true
	}

	if !matches {
		return FuzzyMatch{Matches: false, Score: 0}
	}

	return FuzzyMatch{Matches: true, Score: score}
}

func subsequenceEvidence(q, t string) (bool, int, int, bool) {
	qi := 0
	last := -1
	gaps := 0
	firstPos := -1
	firstBoundary := false
	for i := 0; i < len(t) && qi < len(q); i++ {
		if t[i] != q[qi] {
			continue
		}
		if firstPos < 0 {
			firstPos = i
			firstBoundary = i == 0 || isWordBoundaryChar(rune(t[i-1]))
		}
		if last >= 0 && i > last+1 {
			gaps += i - last - 1
		}
		last = i
		qi++
	}
	if qi < len(q) {
		return false, -1, 0, false
	}
	return true, firstPos, gaps, firstBoundary
}

func swappedAlphaNumericToken(q string) (string, bool) {
	if q == "" {
		return "", false
	}
	isLetter := func(b byte) bool { return b >= 'a' && b <= 'z' }
	isDigit := func(b byte) bool { return b >= '0' && b <= '9' }

	// letters + digits
	i := 0
	for i < len(q) && isLetter(q[i]) {
		i++
	}
	if i > 0 {
		j := i
		for j < len(q) && isDigit(q[j]) {
			j++
		}
		if j == len(q) && i < len(q) {
			return q[i:] + q[:i], true
		}
	}

	// digits + letters
	i = 0
	for i < len(q) && isDigit(q[i]) {
		i++
	}
	if i > 0 {
		j := i
		for j < len(q) && isLetter(q[j]) {
			j++
		}
		if j == len(q) && i < len(q) {
			return q[i:] + q[:i], true
		}
	}

	return "", false
}

// isWordBoundaryChar checks if a character is a word boundary
func isWordBoundaryChar(r rune) bool {
	return strings.ContainsRune(" \t\n\r-_./:", r)
}

// FuzzyFilter filters and sorts items by fuzzy match quality
// Supports space-separated tokens: all tokens must match
func FuzzyFilter[T any](items []T, query string, getText func(T) string) []T {
	query = strings.TrimSpace(query)
	if query == "" {
		return items
	}

	tokens := strings.Fields(query)
	if len(tokens) == 0 {
		return items
	}

	type result struct {
		item       T
		totalScore float64
	}

	results := []result{}

	for _, item := range items {
		text := getText(item)
		totalScore := 0.0
		allMatch := true

		for _, token := range tokens {
			match := Match(token, text)
			if match.Matches {
				totalScore += match.Score
			} else {
				allMatch = false
				break
			}
		}

		if allMatch {
			results = append(results, result{item: item, totalScore: totalScore})
		}
	}

	// Sort by score (lower is better)
	sort.Slice(results, func(i, j int) bool {
		return results[i].totalScore < results[j].totalScore
	})

	// Extract items
	resultItems := make([]T, len(results))
	for i, r := range results {
		resultItems[i] = r.item
	}
	return resultItems
}

// IsSpace checks if a rune is whitespace
func IsSpace(r rune) bool {
	return unicode.IsSpace(r)
}
