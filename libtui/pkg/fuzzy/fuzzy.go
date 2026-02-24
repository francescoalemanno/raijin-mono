package fuzzy

import (
	"regexp"
	"sort"
	"strings"
	"unicode"
)

// FuzzyMatch represents the result of a fuzzy match
type FuzzyMatch struct {
	Matches bool
	Score   float64
}

// Match checks if query matches text using fuzzy matching
// Characters must appear in order, not necessarily consecutively
// Lower score = better match
func Match(query, text string) FuzzyMatch {
	queryLower := strings.ToLower(query)
	textLower := strings.ToLower(text)

	matchQuery := func(normalizedQuery string) FuzzyMatch {
		if normalizedQuery == "" {
			return FuzzyMatch{Matches: true, Score: 0}
		}

		if len(normalizedQuery) > len(textLower) {
			return FuzzyMatch{Matches: false, Score: 0}
		}

		queryIndex := 0
		score := 0.0
		lastMatchIndex := -1
		consecutiveMatches := 0

		for i := 0; i < len(textLower) && queryIndex < len(normalizedQuery); i++ {
			if textLower[i] == normalizedQuery[queryIndex] {
				isWordBoundary := i == 0 || isWordBoundaryChar(rune(textLower[i-1]))

				// Reward consecutive matches
				if lastMatchIndex == i-1 {
					consecutiveMatches++
					score -= float64(consecutiveMatches * 5)
				} else {
					consecutiveMatches = 0
					// Penalize gaps
					if lastMatchIndex >= 0 {
						score += float64((i - lastMatchIndex - 1) * 2)
					}
				}

				// Reward word boundary matches
				if isWordBoundary {
					score -= 10
				}

				// Slight penalty for later matches
				score += float64(i) * 0.1

				lastMatchIndex = i
				queryIndex++
			}
		}

		if queryIndex < len(normalizedQuery) {
			return FuzzyMatch{Matches: false, Score: 0}
		}

		return FuzzyMatch{Matches: true, Score: score}
	}

	primaryMatch := matchQuery(queryLower)
	if primaryMatch.Matches {
		return primaryMatch
	}

	// Try swapping alphanumeric tokens (e.g., "codex52" -> "52codex")
	swappedQuery := ""
	alphaNumericRe := regexp.MustCompile(`^([a-z]+)([0-9]+)$`)
	numericAlphaRe := regexp.MustCompile(`^([0-9]+)([a-z]+)$`)

	if matches := alphaNumericRe.FindStringSubmatch(queryLower); matches != nil {
		swappedQuery = matches[2] + matches[1]
	} else if matches := numericAlphaRe.FindStringSubmatch(queryLower); matches != nil {
		swappedQuery = matches[2] + matches[1]
	}

	if swappedQuery == "" {
		return primaryMatch
	}

	swappedMatch := matchQuery(swappedQuery)
	if !swappedMatch.Matches {
		return primaryMatch
	}

	return FuzzyMatch{Matches: true, Score: swappedMatch.Score + 5}
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
