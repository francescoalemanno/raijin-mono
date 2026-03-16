package main

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/chzyer/readline"
	"github.com/francescoalemanno/raijin-mono/internal/oneshot"
	"github.com/francescoalemanno/raijin-mono/internal/paths"
	"github.com/francescoalemanno/raijin-mono/internal/shellinit"
	libagent "github.com/francescoalemanno/raijin-mono/libagent"
	"golang.org/x/term"
)

const (
	replPrompt             = "raijin❯ "
	replContinuationPrompt = "    ...❯ "
)

type replRunStats struct {
	Duration time.Duration
}

func runSubprocessREPL(baseArgs []string) error {
	stdinFD := int(os.Stdin.Fd())
	stdoutFD := int(os.Stdout.Fd())
	if !term.IsTerminal(stdinFD) || !term.IsTerminal(stdoutFD) {
		return errors.New("no prompt provided and repl mode requires a terminal")
	}

	status := &replStatus{}
	status.update(baseArgs)

	historyPath := replHistoryPath()
	rl, err := readline.NewEx(&readline.Config{
		Prompt:            replPrompt,
		HistoryFile:       historyPath,
		InterruptPrompt:   "^C",
		EOFPrompt:         "exit",
		AutoComplete:      newREPLCompleter(),
		HistorySearchFold: true,
		FuncFilterInputRune: func(r rune) (rune, bool) {
			if r == readline.CharCtrlZ {
				return r, false
			}
			return r, true
		},
	})
	if err != nil {
		return fmt.Errorf("initialize readline: %w", err)
	}
	defer rl.Close()

	printREPLWelcome(status)
	printStatusLine(status)

	var multilines []string
	for {
		line, err := rl.Readline()
		if err != nil {
			if errors.Is(err, readline.ErrInterrupt) {
				multilines = nil
				rl.SetPrompt(replPrompt)
				continue
			}
			if errors.Is(err, io.EOF) {
				fmt.Fprint(os.Stdout, "\n")
				return nil
			}
			return fmt.Errorf("read repl input: %w", err)
		}

		if continuation, ok := replMultilineContinuation(line); ok {
			multilines = append(multilines, continuation)
			rl.SetPrompt(replContinuationPrompt)
			continue
		}

		multilines = append(multilines, line)
		prompt := strings.TrimSpace(strings.Join(multilines, "\n"))
		multilines = nil
		rl.SetPrompt(replPrompt)

		if prompt == "" {
			continue
		}
		if isREPLExitInput(prompt) {
			return nil
		}

		stats, runErr := runREPLSubprocess(baseArgs, prompt)
		if runErr != nil {
			fmt.Fprintln(os.Stderr, libagent.FormatErrorForCLI(runErr))
		}
		printRunFeedback(stats, runErr)
		status.update(baseArgs)
		printStatusLine(status)
	}
}

func printREPLWelcome(status *replStatus) {
	fmt.Fprintln(os.Stdout)
	title := oneshot.RenderThemedAccent("Raijin REPL")
	mode := oneshot.RenderThemedDim("subprocess mode")
	fmt.Fprintf(os.Stdout, "%s %s\n", title, mode)
	fmt.Fprintln(os.Stdout, oneshot.RenderThemedDim("ctrl+d or /exit to quit · tab autocomplete"))
	if status.rightPrompt() == "" {
		fmt.Fprintln(os.Stdout, oneshot.RenderThemedWarn("No model configured: use /add-model"))
	}
}

func printRunFeedback(stats replRunStats, runErr error) {
	if stats.Duration <= 0 {
		return
	}

	icon := oneshot.RenderThemedOK("✓")
	if runErr != nil {
		icon = oneshot.RenderThemedErr("✗")
	}

	fmt.Fprintf(os.Stdout, "%s %s\n", icon, oneshot.RenderThemedDim(formatDurationCompact(stats.Duration)))
}

func printStatusLine(status *replStatus) {
	if info := status.rightPrompt(); info != "" {
		fmt.Fprintf(os.Stdout, "\n%s %s\n", oneshot.RenderThemedInfo("◉"), renderStyledStatusLine(info))
	} else {
		fmt.Fprintln(os.Stdout)
	}
}

func renderStyledStatusLine(label string) string {
	parts := strings.Split(label, " · ")
	if len(parts) == 0 {
		return oneshot.RenderThemedDim(label)
	}

	styled := make([]string, 0, len(parts))
	for i, part := range parts {
		trimmed := strings.TrimSpace(part)
		switch {
		case strings.HasPrefix(trimmed, "ctx:"):
			styled = append(styled, styleContextToken(trimmed))
		case isReasoningLabel(trimmed):
			styled = append(styled, oneshot.RenderThemedWarn(trimmed))
		case i == 0:
			styled = append(styled, oneshot.RenderThemedModel(trimmed))
		case looksLikePath(trimmed):
			styled = append(styled, oneshot.RenderThemedDim(trimmed))
		default:
			styled = append(styled, oneshot.RenderThemedDim(trimmed))
		}
	}

	return strings.Join(styled, oneshot.RenderThemedDim(" · "))
}

func styleContextToken(token string) string {
	pct, ok := parseContextPercent(token)
	if !ok {
		return oneshot.RenderThemedDim(token)
	}
	switch {
	case pct >= 85:
		return oneshot.RenderThemedErr(token)
	case pct >= 60:
		return oneshot.RenderThemedWarn(token)
	default:
		return oneshot.RenderThemedOK(token)
	}
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

func isReasoningLabel(label string) bool {
	switch strings.ToLower(strings.TrimSpace(label)) {
	case "low", "medium", "high", "max", "(not configured)":
		return true
	default:
		return false
	}
}

func looksLikePath(v string) bool {
	return strings.HasPrefix(v, "~") || strings.HasPrefix(v, "/") || strings.HasPrefix(v, ".")
}

func replHistoryPath() string {
	historyPath := paths.RaijinPath("repl", "history.jsonl")
	if historyPath == "" {
		return ""
	}
	_ = os.MkdirAll(filepath.Dir(historyPath), 0o755)
	return historyPath
}

// newREPLCompleter builds a readline.AutoCompleter backed by shellinit.Complete.
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

	// Find the token the cursor is on.
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

	// chzyer/readline expects the suffix to append (after the shared prefix).
	out := make([][]rune, 0, len(candidates))
	for _, cand := range candidates {
		suffix := strings.TrimPrefix(cand, prefix)
		out = append(out, []rune(suffix))
	}
	return out, len(prefix)
}

// replStatus holds the dynamic status, updated after each subprocess run
// by invoking `/status` as a subprocess and parsing its output.
type replStatus struct {
	mu    sync.Mutex
	label string
}

func (s *replStatus) update(baseArgs []string) {
	label := s.queryStatus(baseArgs)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.label = label
}

func (s *replStatus) rightPrompt() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.label
}

// queryStatus runs `/status` as a silent subprocess and parses its stdout
// to build a compact status string (e.g. "anthropic/claude · ctx:12%").
func (s *replStatus) queryStatus(baseArgs []string) string {
	exePath, err := os.Executable()
	if err != nil {
		return ""
	}
	args := replSubprocessArgs(baseArgs, "/status")
	var buf bytes.Buffer
	cmd := exec.Command(exePath, args...)
	cmd.Stdout = &buf
	cmd.Stderr = io.Discard
	cmd.Env = os.Environ()
	if err := cmd.Run(); err != nil {
		return ""
	}
	label := parseStatusOutput(buf.String())
	if cwd := compactCwd(); cwd != "" {
		if label != "" {
			label += " · " + cwd
		} else {
			label = cwd
		}
	}
	return label
}

// parseStatusOutput extracts a compact label from `/status` output lines
// like "Model: x", "Context: 12.3% (24k/200k)".
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
			// Extract just the percentage, e.g. "12.3%" from "12.3% (24k/200k)"
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

func compactCwd() string {
	cwd, err := os.Getwd()
	if err != nil {
		return ""
	}
	home, _ := os.UserHomeDir()
	if home != "" && strings.HasPrefix(cwd, home) {
		cwd = "~" + cwd[len(home):]
	}
	return cwd
}

func runREPLSubprocess(baseArgs []string, prompt string) (replRunStats, error) {
	started := time.Now()
	stats := replRunStats{}

	exePath, err := os.Executable()
	if err != nil {
		return stats, fmt.Errorf("resolve executable path: %w", err)
	}
	args := replSubprocessArgs(baseArgs, prompt)

	cmd := exec.Command(exePath, args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = os.Environ()

	waitErr := runCommandIgnoringInterrupt(cmd)
	stats.Duration = time.Since(started)
	if waitErr != nil {
		var exitErr *exec.ExitError
		if errors.As(waitErr, &exitErr) {
			return stats, fmt.Errorf("subprocess exited with code %d", exitErr.ExitCode())
		}
		return stats, fmt.Errorf("run subprocess: %w", waitErr)
	}

	return stats, nil
}

func runCommandIgnoringInterrupt(cmd *exec.Cmd) error {
	interrupts := make(chan os.Signal, 1)
	signal.Notify(interrupts, os.Interrupt)
	defer signal.Stop(interrupts)
	return cmd.Run()
}

func formatDurationCompact(d time.Duration) string {
	if d < time.Millisecond {
		return "<1ms"
	}
	if d < time.Second {
		return fmt.Sprintf("%dms", d.Milliseconds())
	}
	if d < time.Minute {
		return fmt.Sprintf("%.2fs", d.Seconds())
	}
	return d.Round(100 * time.Millisecond).String()
}

func replSubprocessArgs(baseArgs []string, prompt string) []string {
	args := make([]string, 0, len(baseArgs)+1)
	args = append(args, baseArgs...)
	args = append(args, prompt)
	return args
}

// replMultilineContinuation checks if a line ends with a trailing backslash,
// indicating the user wants to continue input on the next line.
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

func replCompleteLine(line string, pos int, key rune) (string, int, bool) {
	return replCompleteLineWithMatches(line, pos, key, nil)
}

func replCompleteLineWithMatches(line string, pos int, key rune, showMatches func([]string)) (string, int, bool) {
	if key != '\t' {
		return "", 0, false
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
		return line, pos, true
	}
	if strings.HasPrefix(tokenPrefix, ":") {
		return line, pos, true
	}

	context := line[:pos]
	candidates := replCompletionCandidates(context)
	if len(candidates) == 0 {
		return line, pos, true
	}
	if len(candidates) > 1 && showMatches != nil {
		showMatches(candidates)
	}

	replacement := candidates[0]
	if len(candidates) > 1 {
		common := longestCommonPrefix(candidates)
		if len(common) <= len(tokenPrefix) {
			return line, pos, true
		}
		replacement = common
	}

	newLine := line[:tokenStart] + replacement + line[tokenEnd:]
	newPos := tokenStart + len(replacement)
	return newLine, newPos, true
}

func replCompletionCandidates(context string) []string {
	raw := strings.TrimSpace(shellinit.Complete(context))
	return splitNonEmptyLines(raw)
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

func splitNonEmptyLines(s string) []string {
	lines := strings.Split(s, "\n")
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		out = append(out, line)
	}
	return out
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
