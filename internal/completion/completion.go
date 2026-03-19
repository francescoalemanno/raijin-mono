// Package completion provides a unified mechanism for REPL and shell completion.
// It handles token parsing, candidate generation, and fzf-based selection.
package completion

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/francescoalemanno/raijin-mono/internal/commands"
	"github.com/francescoalemanno/raijin-mono/internal/fsutil"
	fzfmatch "github.com/francescoalemanno/raijin-mono/internal/fzf"
	"github.com/francescoalemanno/raijin-mono/internal/prompts"
	"github.com/francescoalemanno/raijin-mono/internal/skills"
	jfzf "github.com/junegunn/fzf/src"
)

// TokenType identifies the category of completion being requested.
type TokenType int

const (
	TokenUnknown   TokenType = iota
	TokenFiles               // @ prefix - file paths
	TokenCommands            // / prefix - builtin commands and templates
	TokenSkills              // + prefix - skills
	TokenUniversal           // no prefix - everything combined
)

// Token represents the token being completed.
type Token struct {
	Type      TokenType
	Raw       string // Full token including prefix (e.g., "@file", "+skill")
	Query     string // Query text without prefix (e.g., "file", "skill")
	Start     int    // Start position in original line
	End       int    // End position in original line
	HasPrefix bool   // Whether the token had an explicit prefix
}

// Candidate is a single completion option.
type Candidate struct {
	Value     string // Full value with prefix (e.g., "@path/to/file")
	Display   string // What to show in fzf (e.g., "path/to/file")
	QueryText string // Text to use for fuzzy matching
}

// Source provides candidates for a specific token type.
type Source interface {
	Candidates() []Candidate
}

// Parse extracts the token being completed at the given position.
// Returns TokenUnknown with empty Raw if no completion should occur.
func Parse(line string, pos int) Token {
	if pos < 0 {
		pos = 0
	}
	if pos > len(line) {
		pos = len(line)
	}

	// Find token boundaries
	start, end := tokenBounds(line, pos)
	if start >= end {
		return Token{Type: TokenUnknown}
	}

	raw := line[start:end]
	if raw == "" {
		return Token{Type: TokenUnknown}
	}

	// Determine token type and extract query
	token := Token{
		Raw:   raw,
		Start: start,
		End:   end,
	}

	switch {
	case strings.HasPrefix(raw, "@"):
		token.Type = TokenFiles
		token.Query = strings.TrimPrefix(raw, "@")
		token.HasPrefix = true
	case strings.HasPrefix(raw, "/"):
		token.Type = TokenCommands
		token.Query = strings.TrimPrefix(raw, "/")
		token.HasPrefix = true
	case strings.HasPrefix(raw, "+"):
		token.Type = TokenSkills
		token.Query = strings.TrimPrefix(raw, "+")
		token.HasPrefix = true
	case strings.HasPrefix(raw, ":"):
		// Colon prefix: ":token" completes to ": /token" (or appropriate prefix)
		token.HasPrefix = true
		afterColon := strings.TrimPrefix(raw, ":")
		switch {
		case strings.HasPrefix(afterColon, "@"):
			token.Type = TokenFiles
			token.Query = strings.TrimPrefix(afterColon, "@")
		case strings.HasPrefix(afterColon, "/"):
			token.Type = TokenCommands
			token.Query = strings.TrimPrefix(afterColon, "/")
		case strings.HasPrefix(afterColon, "+"):
			token.Type = TokenSkills
			token.Query = strings.TrimPrefix(afterColon, "+")
		default:
			// ":cmd" -> look up in universal (commands/templates/skills)
			token.Type = TokenUniversal
			token.Query = afterColon
		}
	default:
		// Bare tokens without prefix autocomplete among all candidates
		token.Type = TokenUniversal
		token.Query = raw
		token.HasPrefix = false
	}

	return token
}

// ParseLastToken parses from the end of the line (for shell integration).
func ParseLastToken(line string) Token {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" {
		return Token{Type: TokenUnknown}
	}

	// If line ends with space, there's no active token
	if len(line) > 0 && unicode.IsSpace(rune(line[len(line)-1])) {
		return Token{Type: TokenUnknown}
	}

	// Get last field
	parts := strings.Fields(trimmed)
	if len(parts) == 0 {
		return Token{Type: TokenUnknown}
	}

	last := parts[len(parts)-1]

	// Calculate position for the last token
	start := strings.LastIndex(line, last)
	if start < 0 {
		start = len(line) - len(last)
	}

	return Parse(line, start+len(last))
}

// GetCandidates returns all candidates for the given token.
func GetCandidates(token Token) []Candidate {
	switch token.Type {
	case TokenFiles:
		return fileCandidates()
	case TokenCommands:
		return commandCandidates()
	case TokenSkills:
		return skillCandidates()
	case TokenUniversal:
		return universalCandidates()
	default:
		return nil
	}
}

// FilterCandidates filters candidates by the token's query using fuzzy matching.
func FilterCandidates(candidates []Candidate, token Token) []Candidate {
	if token.Query == "" {
		return candidates
	}

	type ranked struct {
		candidate Candidate
		score     int
	}

	rankedItems := make([]ranked, 0, len(candidates))
	for _, c := range candidates {
		rankedItems = append(rankedItems, ranked{candidate: c, score: 0})
	}

	matches := fzfmatch.Rank(rankedItems, token.Query, func(r ranked) string { return r.candidate.QueryText })

	out := make([]Candidate, len(matches))
	for i, m := range matches {
		out[i] = m.candidate
	}
	return out
}

// Apply replaces the token in the line with the selected candidate.
func Apply(line string, token Token, selected string) string {
	if token.Start >= token.End || selected == "" {
		return line
	}
	// Handle colon prefix: ":token" -> ": /completion"
	if strings.HasPrefix(token.Raw, ":") {
		selected = ": " + selected
	}
	return line[:token.Start] + selected + line[token.End:]
}

// Complete performs full completion: parse, get candidates, optionally pick with fzf.
// If picker is nil, returns the best match or all matches if ambiguous.
func Complete(line string, pos int, picker func(candidates []Candidate, token Token) (string, error)) (newLine string, newPos int, done bool) {
	token := Parse(line, pos)
	if token.Type == TokenUnknown {
		return line, pos, true
	}

	candidates := GetCandidates(token)
	if len(candidates) == 0 {
		return line, pos, true
	}

	// Filter by query
	filtered := FilterCandidates(candidates, token)
	if len(filtered) == 0 {
		return line, pos, true
	}

	// Single match - apply directly
	if len(filtered) == 1 {
		result := Apply(line, token, filtered[0].Value)
		return result, token.Start + len(filtered[0].Value), true
	}

	// Multiple matches - use picker if available
	if picker == nil {
		return line, pos, false
	}

	selected, err := picker(filtered, token)
	if err != nil || selected == "" {
		return line, pos, true
	}

	result := Apply(line, token, selected)
	return result, token.Start + len(selected), true
}

// FZFPicker creates a picker function that uses embedded fzf.
type FZFPicker struct {
	// UseFullscreen disables the --height flag for fullscreen mode
	UseFullscreen bool
	// Prompt is the fzf prompt string
	Prompt string
}

// Pick runs fzf to select from candidates.
func (p *FZFPicker) Pick(candidates []Candidate, token Token) (string, error) {
	if len(candidates) == 0 {
		return "", nil
	}
	if len(candidates) == 1 {
		return candidates[0].Value, nil
	}

	// Build fzf input
	var stdin bytes.Buffer
	for _, c := range candidates {
		// Use Display for fzf list, we'll map back to Value after
		stdin.WriteString(c.Display)
		stdin.WriteByte('\n')
	}

	// Build args
	args := fzfArgs(token, p)
	options, err := jfzf.ParseOptions(true, args)
	if err != nil {
		return "", fmt.Errorf("parse fzf options: %w", err)
	}

	// Feed items
	inputChan := make(chan string)
	go func() {
		defer close(inputChan)
		for _, c := range candidates {
			inputChan <- c.Display
		}
	}()

	// Collect output
	outputChan := make(chan string)
	resultChan := make(chan string, 1)
	go func() {
		var selected string
		for item := range outputChan {
			selected = item
		}
		resultChan <- selected
	}()

	options.Input = inputChan
	options.Output = outputChan

	code, err := jfzf.Run(options)
	close(outputChan)
	selectedDisplay := <-resultChan

	if err != nil || code != 0 || selectedDisplay == "" {
		return "", nil
	}

	// Map display back to value
	for _, c := range candidates {
		if c.Display == selectedDisplay {
			return c.Value, nil
		}
	}
	return "", nil
}

// Helper functions

func tokenBounds(line string, pos int) (start, end int) {
	start = pos
	for start > 0 && !isSpace(line[start-1]) {
		start--
	}

	end = pos
	for end < len(line) && !isSpace(line[end]) {
		end++
	}
	return start, end
}

func isSpace(b byte) bool {
	return b == ' ' || b == '\t' || b == '\n' || b == '\r'
}

func fileCandidates() []Candidate {
	paths, err := collectPaths(".")
	if err != nil {
		return nil
	}

	candidates := make([]Candidate, len(paths))
	for i, p := range paths {
		candidates[i] = Candidate{
			Value:     "@" + p,
			Display:   p,
			QueryText: p,
		}
	}
	return candidates
}

func commandCandidates() []Candidate {
	names := commandAndTemplateNames()
	candidates := make([]Candidate, len(names))
	for i, name := range names {
		candidates[i] = Candidate{
			Value:     "/" + name,
			Display:   name,
			QueryText: name,
		}
	}
	return candidates
}

func skillCandidates() []Candidate {
	names := skillNames()
	candidates := make([]Candidate, len(names))
	for i, name := range names {
		candidates[i] = Candidate{
			Value:     "+" + name,
			Display:   name,
			QueryText: name,
		}
	}
	return candidates
}

func universalCandidates() []Candidate {
	// Combine commands, templates, and skills
	var candidates []Candidate

	// Add commands and templates (as /name)
	for _, name := range commandAndTemplateNames() {
		candidates = append(candidates, Candidate{
			Value:     "/" + name,
			Display:   "/" + name,
			QueryText: name,
		})
	}

	// Add skills (as +name)
	for _, name := range skillNames() {
		candidates = append(candidates, Candidate{
			Value:     "+" + name,
			Display:   "+" + name,
			QueryText: name,
		})
	}

	return candidates
}

func commandAndTemplateNames() []string {
	seen := make(map[string]struct{})
	var lines []string

	for _, cmd := range commands.BuiltinCommands {
		name := strings.TrimPrefix(cmd.Command, "/")
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		lines = append(lines, name)
	}

	reserved := make(map[string]struct{})
	for _, cmd := range commands.BuiltinCommands {
		reserved[strings.TrimPrefix(strings.Fields(cmd.Command)[0], "/")] = struct{}{}
	}

	for _, tmpl := range prompts.GetTemplates() {
		if _, ok := reserved[tmpl.Name]; ok {
			continue
		}
		if _, ok := seen[tmpl.Name]; ok {
			continue
		}
		seen[tmpl.Name] = struct{}{}
		lines = append(lines, tmpl.Name)
	}

	return lines
}

func skillNames() []string {
	seen := make(map[string]struct{})
	var names []string
	for _, s := range skills.GetSkills() {
		name := strings.TrimSpace(s.Name)
		if name == "" {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func collectPaths(root string) ([]string, error) {
	cwd := root
	if cwd == "" {
		cwd = "."
	}
	absRoot, err := filepath.Abs(cwd)
	if err != nil {
		return nil, fmt.Errorf("resolve root: %w", err)
	}

	var items []string
	err = filepath.WalkDir(absRoot, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil
		}
		if path == absRoot {
			return nil
		}

		name := d.Name()
		if d.IsDir() && fsutil.ShouldSkipMentionDir(name) {
			return filepath.SkipDir
		}

		rel, err := filepath.Rel(absRoot, path)
		if err != nil {
			return nil
		}
		rel = fsutil.NormalizePath(rel)
		if rel == "." || rel == "" {
			return nil
		}
		items = append(items, rel)
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(items)
	return items, nil
}

func fzfArgs(token Token, picker *FZFPicker) []string {
	args := []string{"--reverse", "--border", "--no-scrollbar", "--exit-0", "--select-1"}

	if !picker.UseFullscreen {
		args = append(args, "--height=80%")
	}

	// Configure based on token type
	switch token.Type {
	case TokenFiles:
		args = append(args, "--scheme=path")
		args = append(args, "--prompt=@ ")
	case TokenCommands:
		args = append(args, "--prompt=/ ")
	case TokenSkills:
		args = append(args, "--prompt=+ ")
	case TokenUniversal:
		args = append(args, "--prompt=> ")
	default:
		args = append(args, "--prompt=> ")
	}

	if picker.Prompt != "" {
		args = append(args, "--prompt="+picker.Prompt)
	}

	if token.Query != "" {
		args = append(args, "--query="+token.Query)
	}

	args = append(args, "--bind=tab:accept")

	return args
}

// Legacy compatibility helpers

// CompletionTokenBounds returns the start/end positions of the token being completed.
// Used for backward compatibility with shellinit.
func CompletionTokenBounds(current string) (start, end int, ok bool) {
	if strings.TrimSpace(current) == "" {
		return 0, 0, false
	}
	end = len(current)
	for end > 0 {
		r, size := utf8.DecodeLastRuneInString(current[:end])
		if !unicode.IsSpace(r) {
			break
		}
		end -= size
	}
	if end == 0 {
		return 0, 0, false
	}
	start = end
	for start > 0 {
		r, size := utf8.DecodeLastRuneInString(current[:start])
		if unicode.IsSpace(r) {
			break
		}
		start -= size
	}
	return start, end, true
}

// Shell helpers for backward compatibility

// ShellComplete performs shell completion (from shell integration scripts).
func ShellComplete(current string) string {
	token := ParseLastToken(current)
	if token.Type == TokenUnknown {
		return current
	}

	candidates := GetCandidates(token)
	filtered := FilterCandidates(candidates, token)

	if len(filtered) == 0 {
		return current
	}
	if len(filtered) == 1 {
		return applyShellCompletion(current, filtered[0].Value)
	}

	// Multiple matches - use fzf picker
	picker := &FZFPicker{UseFullscreen: false, Prompt: "Raijin > "}
	selected, err := picker.Pick(filtered, token)
	if err != nil || selected == "" {
		return current
	}

	return applyShellCompletion(current, selected)
}

func applyShellCompletion(current, selected string) string {
	start, end, ok := CompletionTokenBounds(current)
	if !ok {
		return selected
	}
	return current[:start] + selected + current[end:]
}

// Completions returns all completable entries as newline-separated string.
// Used for shell completion listing.
func Completions() string {
	// Return bare command/template names (no / prefix)
	// and skills with + prefix
	var entries []string
	entries = append(entries, commandAndTemplateNames()...)
	for _, name := range skillNames() {
		entries = append(entries, "+"+name)
	}
	return strings.Join(entries, "\n")
}

// MentionEscape formats a path for use as an @-mention.
// If the path contains spaces, it escapes it with quotes.
func MentionEscape(path string) string {
	if strings.ContainsAny(path, " \t") {
		escaped := strings.ReplaceAll(path, "\\", "\\\\")
		escaped = strings.ReplaceAll(escaped, `"`, `\"`)
		return `@"` + escaped + `"`
	}
	return "@" + path
}

// SetRunFZF allows tests to stub the fzf runner.
var SetRunFZF func(mode, query string, stdin io.Reader, stdout io.Writer) (int, error)
