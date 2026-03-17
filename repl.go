package main

import (
	"errors"
	"strconv"
	"strings"

	"github.com/francescoalemanno/raijin-mono/internal/shellinit"
)

const (
	replPickerModeComplete = "complete"
	replPickerModeMention  = "mention"
)

type replPickerRequest struct {
	mode       string
	line       string
	tokenStart int
	tokenEnd   int
	query      string
	candidates []string
}

type replCompletionPicker func(mode, query string, candidates []string) (string, error)

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

	start := pos
	for start > 0 && !isREPLSpace(byte(line[start-1])) {
		start--
	}
	prefix := string(line[start:pos])
	if strings.HasPrefix(prefix, ":") {
		return nil, 0
	}

	context := string(line[:pos])
	candidates := replCompletionCandidates(context)
	if len(candidates) == 0 {
		return nil, 0
	}

	prefixMatches := candidates[:0]
	for _, candidate := range candidates {
		if strings.HasPrefix(candidate, prefix) {
			prefixMatches = append(prefixMatches, candidate)
		}
	}
	if len(prefixMatches) == 0 {
		return nil, 0
	}

	out := make([][]rune, 0, len(prefixMatches))
	for _, cand := range prefixMatches {
		out = append(out, []rune(strings.TrimPrefix(cand, prefix)))
	}
	return out, len(prefix)
}

func replCompleteLineWithMatches(line string, pos int, key rune, showMatches func([]string)) (string, int, bool) {
	return replCompleteLineWithPicker(line, pos, key, nil, showMatches)
}

func replBuildPickerRequest(line string, pos int, key rune) (string, int, *replPickerRequest, bool) {
	if key != '\t' {
		return "", 0, nil, false
	}
	if pos < 0 {
		pos = 0
	}
	if pos > len(line) {
		pos = len(line)
	}

	tokenStart, tokenEnd := replTokenBounds(line, pos)
	tokenPrefix := line[tokenStart:pos]
	if tokenPrefix == "" {
		return line, pos, nil, true
	}
	if strings.HasPrefix(tokenPrefix, ":") {
		return line, pos, nil, true
	}
	if strings.HasPrefix(tokenPrefix, "@") {
		return line, pos, &replPickerRequest{
			mode:       replPickerModeMention,
			line:       line,
			tokenStart: tokenStart,
			tokenEnd:   tokenEnd,
			query:      replMentionQuery(tokenPrefix),
		}, true
	}

	context := line[:pos]
	candidates := replCompletionCandidates(context)
	if len(candidates) == 0 {
		return line, pos, nil, true
	}
	if len(candidates) == 1 {
		replacement := candidates[0]
		newLine := line[:tokenStart] + replacement + line[tokenEnd:]
		newPos := tokenStart + len(replacement)
		return newLine, newPos, nil, true
	}
	return line, pos, &replPickerRequest{
		mode:       replPickerModeComplete,
		line:       line,
		tokenStart: tokenStart,
		tokenEnd:   tokenEnd,
		query:      replCompletionQuery(context),
		candidates: append([]string(nil), candidates...),
	}, true
}

func replCompleteLineWithPicker(line string, pos int, key rune, pick replCompletionPicker, showMatches func([]string)) (string, int, bool) {
	newLine, newPos, req, ok := replBuildPickerRequest(line, pos, key)
	if !ok {
		return "", 0, false
	}
	if req == nil {
		return newLine, newPos, true
	}
	if req.mode == replPickerModeComplete && showMatches != nil {
		showMatches(req.candidates)
	}
	if pick == nil {
		return line, pos, true
	}
	selected, err := pick(req.mode, req.query, req.candidates)
	if err != nil || selected == "" {
		return line, pos, true
	}
	resolved, err := replApplyPickerSelection(req, selected)
	if err != nil {
		return line, pos, true
	}
	replacement := selected
	if req.mode == replPickerModeMention {
		replacement = replFormatMention(selected)
	}
	return resolved, req.tokenStart + len(replacement), true
}

func replCompletionCandidates(context string) []string {
	return shellinit.Candidates(context)
}

func replApplyPickerSelection(req *replPickerRequest, selected string) (string, error) {
	if req == nil {
		return "", errors.New("picker request is required")
	}
	if selected == "" {
		return req.line, nil
	}
	replacement := selected
	if req.mode == replPickerModeMention {
		replacement = replFormatMention(selected)
	}
	return req.line[:req.tokenStart] + replacement + req.line[req.tokenEnd:], nil
}

func replCompletionQuery(context string) string {
	context = strings.TrimSpace(context)
	if context == "" {
		return ""
	}
	parts := strings.Fields(context)
	if len(parts) == 0 {
		return ""
	}
	token := parts[len(parts)-1]
	switch {
	case strings.HasPrefix(token, "+"):
		return strings.TrimPrefix(token, "+")
	case strings.HasPrefix(token, "/"):
		return strings.TrimPrefix(token, "/")
	default:
		return token
	}
}

func replMentionQuery(token string) string {
	return strings.TrimPrefix(token, "@")
}

func replFormatMention(path string) string {
	if strings.ContainsAny(path, " \t") {
		escaped := strings.ReplaceAll(path, "\\", "\\\\")
		escaped = strings.ReplaceAll(escaped, `"`, `\"`)
		return `@"` + escaped + `"`
	}
	return "@" + path
}

func replTokenBounds(line string, pos int) (start, end int) {
	start = pos
	for start > 0 {
		if isREPLSpace(line[start-1]) {
			break
		}
		start--
	}

	end = pos
	for end < len(line) {
		if isREPLSpace(line[end]) {
			break
		}
		end++
	}
	return start, end
}

func isREPLSpace(b byte) bool {
	switch b {
	case ' ', '\t', '\n', '\r':
		return true
	default:
		return false
	}
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
