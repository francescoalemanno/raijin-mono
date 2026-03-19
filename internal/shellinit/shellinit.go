// Package shellinit provides shell integration scripts and completion data
// for the `:` prefix shortcut.
package shellinit

import (
	"bytes"
	"embed"
	"fmt"
	"io"
	"strings"
	"text/template"

	"github.com/francescoalemanno/raijin-mono/internal/completion"
	jfzf "github.com/junegunn/fzf/src"
)

//go:embed scripts/*
var scriptFS embed.FS

// SupportedShells returns the list of shells that have init scripts.
func SupportedShells() []string {
	return []string{"zsh", "bash", "fish"}
}

// Init returns the shell integration script for the given shell.
func Init(shell string) (string, error) {
	var filename string
	switch shell {
	case "zsh":
		filename = "scripts/raijin.zsh"
	case "bash":
		filename = "scripts/raijin.bash"
	case "fish":
		filename = "scripts/raijin.fish"
	default:
		return "", fmt.Errorf("unsupported shell %q; supported: %s", shell, strings.Join(SupportedShells(), ", "))
	}
	data, err := scriptFS.ReadFile(filename)
	if err != nil {
		return "", fmt.Errorf("reading init script for %s: %w", shell, err)
	}
	// Execute template (kept for future extensibility, no data needed currently)
	tmpl, err := template.New(filename).Parse(string(data))
	if err != nil {
		return "", fmt.Errorf("parsing init script template for %s: %w", shell, err)
	}
	var rendered bytes.Buffer
	if err := tmpl.Execute(&rendered, nil); err != nil {
		return "", fmt.Errorf("rendering init script for %s: %w", shell, err)
	}
	return strings.TrimRight(rendered.String(), "\n") + "\n", nil
}

// Completions returns all completable entries shown in /help, one per line.
// Slash commands and templates are returned without the leading "/".
// Skills are returned with the leading "+".
// This is meant to be called by shell completion functions via
// `raijin --completions`.
func Completions() string {
	return completion.Completions()
}

// Complete resolves shell completions and returns one candidate per line.
// It accepts either a token or the full current input line.
//
// Deprecated: Use completion.FilterCandidates directly for new code.
func Complete(current string) string {
	token := completion.ParseLastToken(current)
	if token.Type == completion.TokenUnknown {
		return ""
	}

	candidates := completion.GetCandidates(token)
	filtered := completion.FilterCandidates(candidates, token)

	var out strings.Builder
	for _, c := range filtered {
		out.WriteString(c.Value)
		out.WriteByte('\n')
	}
	return out.String()
}

var (
	runFZFForComplete          = RunFZF
	completionMatchesForSelect = defaultCompletionMatches
)

// defaultCompletionMatches returns filtered candidates for the given input.
func defaultCompletionMatches(current string) []string {
	token := completion.ParseLastToken(current)
	if token.Type == completion.TokenUnknown {
		return nil
	}

	candidates := completion.GetCandidates(token)
	filtered := completion.FilterCandidates(candidates, token)

	out := make([]string, len(filtered))
	for i, c := range filtered {
		out[i] = c.Value
	}
	return out
}

// CompleteSelection resolves completion to a single candidate.
//
// Behavior:
//   - one match: return that completion directly
//   - multiple matches: open embedded fzf to let the user choose
//   - no match or canceled picker: return the original input as-is
func CompleteSelection(current string) string {
	token := completion.ParseLastToken(current)
	if token.Type == completion.TokenUnknown {
		return current
	}

	matches := completionMatchesForSelect(current)
	if len(matches) == 0 {
		return current
	}
	if len(matches) == 1 {
		return applyCompletion(current, matches[0])
	}

	if chosen, ok := pickCompletionWithFZF(matches, token.Query); ok {
		return applyCompletion(current, chosen)
	}
	return current
}

func applyCompletion(current, selected string) string {
	start, end, ok := completion.CompletionTokenBounds(current)
	if !ok {
		return selected
	}
	// Handle colon prefix: ":" -> ": /selected"
	if strings.HasPrefix(current[start:end], ":") {
		selected = ": " + selected
	}
	return current[:start] + selected + current[end:]
}

func pickCompletionWithFZF(candidates []string, query string) (string, bool) {
	var stdin bytes.Buffer
	for _, candidate := range candidates {
		if strings.TrimSpace(candidate) == "" {
			continue
		}
		stdin.WriteString(candidate)
		stdin.WriteByte('\n')
	}
	if stdin.Len() == 0 {
		return "", false
	}

	var stdout bytes.Buffer
	code, err := runFZFForComplete("complete", strings.TrimSpace(query), &stdin, &stdout)
	if err != nil || code != 0 {
		return "", false
	}

	selected := firstNonEmptyLine(&stdout)
	if selected == "" {
		return "", false
	}
	return selected, true
}

func firstNonEmptyLine(r io.Reader) string {
	b, err := io.ReadAll(r)
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(strings.ReplaceAll(string(b), "\r\n", "\n"), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		return strings.TrimRight(line, "\r")
	}
	return ""
}

// Candidates returns the full eligible completion set for the active token.
// Unlike Complete, it does not filter by the token text, which makes it
// suitable for fuzzy ranking and fzf-driven selection.
// Returns bare completions (+skill, /command, @path) without any shell prefix.
//
// Deprecated: Use completion.GetCandidates and completion.ParseLastToken directly.
func Candidates(current string) []string {
	token := completion.ParseLastToken(current)
	if token.Type == completion.TokenUnknown {
		return nil
	}

	candidates := completion.GetCandidates(token)
	out := make([]string, len(candidates))
	for i, c := range candidates {
		out[i] = c.Value
	}
	return out
}

// RunFZFOptions configures fzf behavior.
type RunFZFOptions struct {
	ExpectKeys              []string
	Bindings                []string
	DisableSingleItemBypass bool
	DisableSelectOne        bool
	DisableSort             bool
	Header                  string
}

// RunFZFResult holds the outcome of running fzf.
type RunFZFResult struct {
	Code     int
	Selected []string
	Key      string
}

// RunFZF launches the embedded fzf picker.
//
// Modes:
//   - default / complete / repl-complete: read candidates from stdin
//   - paths: walk the current workspace and feed @-mention paths
func RunFZF(mode, query string, stdin io.Reader, stdout io.Writer) (int, error) {
	result, err := RunFZFWithOptions(mode, query, stdin, RunFZFOptions{})
	if err != nil {
		return result.Code, err
	}
	for _, item := range result.Selected {
		if _, writeErr := fmt.Fprintln(stdout, item); writeErr != nil {
			return jfzf.ExitError, writeErr
		}
	}
	return result.Code, nil
}

// RunFZFWithOptions runs fzf with additional configuration options.
func RunFZFWithOptions(mode, query string, stdin io.Reader, cfg RunFZFOptions) (RunFZFResult, error) {
	mode = strings.TrimSpace(strings.ToLower(mode))
	if mode == "" {
		mode = "default"
	}

	items, err := fzfItems(mode, stdin)
	if err != nil {
		return RunFZFResult{Code: jfzf.ExitError}, err
	}
	if len(items) == 0 {
		return RunFZFResult{Code: 0}, nil
	}
	if len(items) == 1 && !cfg.DisableSingleItemBypass {
		return RunFZFResult{Code: 0, Selected: []string{items[0]}}, nil
	}

	args := fzfArgs(mode, query, cfg)
	options, err := jfzf.ParseOptions(true, args)
	if err != nil {
		return RunFZFResult{Code: jfzf.ExitError}, err
	}

	inputChan := make(chan string)
	go func() {
		defer close(inputChan)
		for _, item := range items {
			inputChan <- item
		}
	}()

	outputChan := make(chan string)
	resultChan := make(chan []string, 1)
	go func() {
		var selected []string
		for item := range outputChan {
			selected = append(selected, item)
		}
		resultChan <- selected
	}()

	options.Input = inputChan
	options.Output = outputChan

	code, err := jfzf.Run(options)
	close(outputChan)
	result := RunFZFResult{
		Code:     code,
		Selected: <-resultChan,
	}
	if len(cfg.ExpectKeys) == 0 || len(result.Selected) == 0 {
		return result, err
	}
	result.Key, result.Selected = splitExpectOutput(result.Selected, cfg.ExpectKeys)
	return result, err
}

func splitExpectOutput(lines []string, expectKeys []string) (string, []string) {
	if len(lines) == 0 {
		return "", nil
	}

	first := strings.TrimSpace(lines[0])
	if first == "" {
		if len(lines) == 1 {
			return "", nil
		}
		// Some fzf builds emit an empty first line for Enter when --expect is used.
		return "", lines[1:]
	}

	expectSet := make(map[string]struct{}, len(expectKeys))
	for _, key := range expectKeys {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		expectSet[key] = struct{}{}
	}
	if _, ok := expectSet[first]; ok {
		return first, lines[1:]
	}

	// Other builds don't emit a key line when Enter is pressed; keep full selection.
	return "", lines
}

func fzfArgs(mode, query string, cfg RunFZFOptions) []string {
	args := []string{"--reverse", "--border", "--no-scrollbar", "--exit-0"}
	if !cfg.DisableSelectOne {
		args = append(args, "--select-1")
	}
	if cfg.DisableSort {
		args = append(args, "--no-sort")
	}
	switch mode {
	case "paths":
		args = append(args, "--scheme=path", "--prompt=@ ")
		args = append(args, "--bind=tab:accept")
	case "complete":
		args = append(args, "--height=80%")
		args = append(args, "--prompt=Raijin > ")
		args = append(args, "--bind=tab:accept")
	case "repl-complete":
		args = append(args, "--prompt=Raijin > ")
		args = append(args, "--bind=tab:accept")
	default:
		args = append(args, "--height=80%")
		args = append(args, "--prompt=> ")
	}
	if query != "" {
		args = append(args, "--query="+query)
	}
	if header := strings.TrimSpace(cfg.Header); header != "" {
		args = append(args, "--header="+header)
	}
	if len(cfg.ExpectKeys) > 0 {
		keys := make([]string, 0, len(cfg.ExpectKeys))
		for _, key := range cfg.ExpectKeys {
			key = strings.TrimSpace(key)
			if key == "" {
				continue
			}
			keys = append(keys, key)
		}
		if len(keys) > 0 {
			args = append(args, "--expect="+strings.Join(keys, ","))
		}
	}
	for _, binding := range cfg.Bindings {
		binding = strings.TrimSpace(binding)
		if binding == "" {
			continue
		}
		args = append(args, "--bind="+binding)
	}
	return args
}

func fzfItems(mode string, stdin io.Reader) ([]string, error) {
	switch mode {
	case "default", "complete", "repl-complete":
		return readStdinItems(stdin)
	case "paths":
		// Use completion package for path collection
		candidates := completion.GetCandidates(completion.Token{Type: completion.TokenFiles})
		items := make([]string, len(candidates))
		for i, c := range candidates {
			items[i] = c.Display
		}
		return items, nil
	default:
		return nil, fmt.Errorf("unsupported fzf mode %q", mode)
	}
}

func readStdinItems(stdin io.Reader) ([]string, error) {
	// Use bufio.Scanner for efficient line reading with proper whitespace handling
	// Import locally to avoid adding to package imports for this compatibility function
	scanner := &lineScanner{r: stdin}
	const maxTokenSize = 1024 * 1024
	scanner.buffer = make([]byte, 0, 64*1024)
	scanner.maxTokenSize = maxTokenSize

	var items []string
	for scanner.scan() {
		line := scanner.text()
		line = strings.TrimRight(line, "\r")
		// Only skip truly empty lines, preserve whitespace-only lines
		if line == "" {
			continue
		}
		items = append(items, line)
	}
	if err := scanner.err(); err != nil {
		return nil, fmt.Errorf("read fzf input: %w", err)
	}
	return items, nil
}

// lineScanner is a simple line scanner that preserves leading whitespace.
type lineScanner struct {
	r            io.Reader
	buffer       []byte
	maxTokenSize int
	lastErr      error
}

func (s *lineScanner) scan() bool {
	if s.lastErr != nil {
		return false
	}
	s.buffer = s.buffer[:0]
	for {
		if len(s.buffer) >= s.maxTokenSize {
			return true
		}
		var b [1]byte
		n, err := s.r.Read(b[:])
		if err != nil {
			if err == io.EOF && len(s.buffer) > 0 {
				return true
			}
			s.lastErr = err
			return false
		}
		if n == 0 {
			continue
		}
		if b[0] == '\n' {
			return true
		}
		s.buffer = append(s.buffer, b[0])
	}
}

func (s *lineScanner) text() string {
	return string(s.buffer)
}

func (s *lineScanner) err() error {
	if s.lastErr == io.EOF {
		return nil
	}
	return s.lastErr
}
