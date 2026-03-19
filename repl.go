package main

import (
	"strconv"
	"strings"

	"github.com/francescoalemanno/raijin-mono/internal/completion"
)

type replCompletionPicker func(query string, candidates []completion.Candidate, tokenType completion.TokenType) (string, error)

func replSubprocessArgs(baseArgs []string, prompt string) []string {
	args := make([]string, 0, len(baseArgs)+1)
	args = append(args, baseArgs...)
	args = append(args, prompt)
	return args
}

func replMultilineContinuation(line string) (string, bool) {
	trimmed := strings.TrimRight(line, " \t")
	if strings.HasSuffix(trimmed, "\\") {
		return strings.TrimSuffix(trimmed, "\\"), true
	}
	return "", false
}

func isREPLExitInput(input string) bool {
	switch strings.ToLower(strings.TrimSpace(input)) {
	case "/exit", "exit", "quit":
		return true
	default:
		return false
	}
}

func parseStatusOutput(output string) string {
	var model, reasoning, ctx string
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if after, ok := strings.CutPrefix(line, "Model: "); ok {
			model = strings.TrimSpace(after)
		}
		if after, ok := strings.CutPrefix(line, "Reasoning: "); ok {
			reasoning = strings.TrimSpace(after)
		}
		if after, ok := strings.CutPrefix(line, "Context: "); ok {
			if pct, _, found := strings.Cut(after, " "); found {
				ctx = "ctx:" + pct
			} else {
				ctx = "ctx:" + strings.TrimSpace(after)
			}
		}
	}
	parts := make([]string, 0, 4)
	if model != "" {
		parts = append(parts, model)
	}
	if reasoning != "" {
		parts = append(parts, reasoning)
	}
	if ctx != "" {
		parts = append(parts, ctx)
	}
	return strings.Join(parts, " · ")
}

func parseContextPercent(token string) (float64, bool) {
	raw := strings.TrimSpace(strings.TrimPrefix(token, "ctx:"))
	raw = strings.TrimSuffix(raw, "%")
	if raw == "" {
		return 0, false
	}
	v, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return 0, false
	}
	return v, true
}

func replCompleteLine(line string, pos int, key rune) (string, int, bool) {
	return replCompleteLineWithMatches(line, pos, key, nil)
}

func newREPLCompleter() *replAutoCompleter {
	return &replAutoCompleter{}
}

type replAutoCompleter struct{}

func (c *replAutoCompleter) Do(line []rune, pos int) ([][]rune, int) {
	if pos < 0 {
		pos = 0
	}
	if pos > len(line) {
		pos = len(line)
	}

	token := completion.Parse(string(line), pos)
	if token.Type == completion.TokenUnknown {
		return nil, 0
	}

	candidates := completion.GetCandidates(token)
	filtered := completion.FilterCandidates(candidates, token)

	if len(filtered) == 0 {
		return nil, 0
	}

	// For colon-prefixed or bare (universal) tokens, return all filtered candidates
	// The full token will be replaced
	if strings.HasPrefix(token.Raw, ":") || token.Type == completion.TokenUniversal {
		out := make([][]rune, 0, len(filtered))
		for _, c := range filtered {
			// Return the full value including prefix (e.g., "/add-model")
			out = append(out, []rune(c.Value))
		}
		return out, len(token.Raw)
	}

	// Build prefix from current text after token start
	prefix := string(line[token.Start:pos])

	// Find prefix matches
	var matches []completion.Candidate
	for _, c := range filtered {
		if strings.HasPrefix(c.Value, prefix) {
			matches = append(matches, c)
		}
	}
	if len(matches) == 0 {
		return nil, 0
	}

	out := make([][]rune, 0, len(matches))
	for _, m := range matches {
		out = append(out, []rune(strings.TrimPrefix(m.Value, prefix)))
	}
	return out, len(prefix)
}

func replCompleteLineWithMatches(line string, pos int, key rune, showMatches func([]string)) (string, int, bool) {
	return replCompleteLineWithPicker(line, pos, key, nil, showMatches)
}

func replCompleteLineWithPicker(line string, pos int, key rune, pick replCompletionPicker, showMatches func([]string)) (string, int, bool) {
	if key != '\t' {
		return "", 0, false
	}

	token := completion.Parse(line, pos)
	if token.Type == completion.TokenUnknown {
		return line, pos, true
	}

	candidates := completion.GetCandidates(token)
	filtered := completion.FilterCandidates(candidates, token)

	if len(filtered) == 0 {
		return line, pos, true
	}

	// Single match - apply directly
	if len(filtered) == 1 {
		result := completion.Apply(line, token, filtered[0].Value)
		newPos := token.Start + len(filtered[0].Value)
		// Apply adds ": " prefix for colon-prefixed tokens
		if strings.HasPrefix(token.Raw, ":") {
			newPos += 2 // for ": "
		}
		return result, newPos, true
	}

	// Show matches if callback provided
	if showMatches != nil {
		display := make([]string, len(filtered))
		for i, c := range filtered {
			display[i] = c.Display
		}
		showMatches(display)
	}

	// No picker - stay at current state
	if pick == nil {
		return line, pos, true
	}

	// Use picker
	selected, err := pick(token.Query, filtered, token.Type)
	if err != nil || selected == "" {
		return line, pos, true
	}

	// Find the candidate with this display value to get the full value
	for _, c := range filtered {
		if c.Display == selected {
			result := completion.Apply(line, token, c.Value)
			newPos := token.Start + len(c.Value)
			// Apply adds ": " prefix for colon-prefixed tokens
			if strings.HasPrefix(token.Raw, ":") {
				newPos += 2 // for ": "
			}
			return result, newPos, true
		}
	}

	return line, pos, true
}

func longestCommonPrefix(items []string) string {
	if len(items) == 0 {
		return ""
	}
	prefix := items[0]
	for _, item := range items[1:] {
		for !strings.HasPrefix(item, prefix) && prefix != "" {
			prefix = prefix[:len(prefix)-1]
		}
		if prefix == "" {
			return ""
		}
	}
	return prefix
}
