package oneshot

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
	"github.com/francescoalemanno/raijin-mono/internal/shellinit"
	libagent "github.com/francescoalemanno/raijin-mono/libagent"
	"golang.org/x/term"
)

const (
	replPrompt             = "raijin❯ "
	replContinuationPrompt = "    ...❯ "
	replPickerModeComplete = "complete"
	replPickerModeMention  = "mention"
)

type replRunStats struct {
	Duration time.Duration
}

type replPickerRequest struct {
	mode       string
	line       string
	tokenStart int
	tokenEnd   int
	query      string
	candidates []string
}

type replStatusMsg struct {
	label string
}

type replRunDoneMsg struct {
	stats replRunStats
	err   error
}

type replPickerDoneMsg struct {
	req      *replPickerRequest
	selected string
	err      error
}

type replStatus struct {
	mu    sync.Mutex
	label string
}

type replModel struct {
	baseArgs []string

	status       string
	statusLoaded bool

	pendingLines []string
	buf          []rune
	cursor       int

	history       []string
	historyIndex  int
	historyDraft  string
	historyActive bool
}

type replPickerExec struct {
	mode       string
	query      string
	candidates []string
	selected   string
}

func RunSubprocessREPL(baseArgs []string) error {
	stdinFD := int(os.Stdin.Fd())
	stdoutFD := int(os.Stdout.Fd())
	if !term.IsTerminal(stdinFD) || !term.IsTerminal(stdoutFD) {
		return errors.New("no prompt provided and repl mode requires a terminal")
	}

	initialStatus := replStatusQuery(baseArgs)
	fmt.Fprintln(os.Stdout, RenderThemedAccent("Raijin REPL")+" "+RenderThemedDim("subprocess mode"))
	fmt.Fprintln(os.Stdout, RenderThemedDim("ctrl+d or /exit to quit · tab autocomplete · up/down history"))
	fmt.Fprintln(os.Stdout, renderPrintedStatusLine(initialStatus))

	model := replModel{
		baseArgs:     append([]string(nil), baseArgs...),
		status:       initialStatus,
		statusLoaded: true,
		historyIndex: -1,
	}
	p := tea.NewProgram(model, tea.WithInput(os.Stdin), tea.WithOutput(os.Stdout))
	if _, err := p.Run(); err != nil {
		return fmt.Errorf("run repl: %w", err)
	}
	fmt.Fprint(os.Stdout, "\n")
	return nil
}

func (m replModel) Init() tea.Cmd {
	return nil
}

func (m replModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case replStatusMsg:
		m.status = msg.label
		m.statusLoaded = true
		return m, tea.Println(renderPrintedStatusLine(m.status))
	case replRunDoneMsg:
		return m, tea.Batch(replFeedbackCmd(msg.stats, msg.err), replStatusCmd(m.baseArgs))
	case replPickerDoneMsg:
		if msg.err != nil {
			return m, tea.Println(RenderThemedErr(libagent.FormatErrorForCLI(msg.err)))
		}
		line, err := replApplyPickerSelection(msg.req, msg.selected)
		if err != nil {
			return m, tea.Println(RenderThemedErr(libagent.FormatErrorForCLI(err)))
		}
		m.setBufferString(line, utf8.RuneCountInString(line))
	case tea.PasteMsg:
		m.insertText(msg.Content)
	case tea.KeyPressMsg:
		switch msg.String() {
		case "ctrl+c":
			m.pendingLines = nil
			m.buf = nil
			m.cursor = 0
			m.resetHistoryNav()
		case "ctrl+d":
			if len(m.pendingLines) == 0 && len(m.buf) == 0 {
				return m, tea.Quit
			}
			m.deleteForward()
		case "tab":
			return m.handleTab()
		case "enter":
			return m.submit()
		case "backspace", "ctrl+h":
			m.deleteBackward()
		case "delete":
			m.deleteForward()
		case "left":
			if m.cursor > 0 {
				m.cursor--
			}
		case "right":
			if m.cursor < len(m.buf) {
				m.cursor++
			}
		case "home", "ctrl+a":
			m.cursor = 0
		case "end", "ctrl+e":
			m.cursor = len(m.buf)
		case "up":
			m.historyPrev()
		case "down":
			m.historyNext()
		default:
			if text := msg.Key().Text; text != "" {
				m.insertText(text)
			}
		}
	}
	return m, nil
}

func (m replModel) View() tea.View {
	var b strings.Builder

	for _, line := range m.pendingLines {
		b.WriteString(replContinuationPrompt)
		b.WriteString(line)
		b.WriteByte('\n')
	}

	prompt := replPrompt
	if len(m.pendingLines) > 0 {
		prompt = replContinuationPrompt
	}
	b.WriteString(prompt)
	b.WriteString(renderBufferWithCursor(m.buf, m.cursor))

	return tea.NewView(b.String())
}

func (m *replModel) handleTab() (tea.Model, tea.Cmd) {
	line := string(m.buf)
	bytePos := byteOffsetForRunePos(line, m.cursor)
	newLine, newBytePos, req, ok := replBuildPickerRequest(line, bytePos)
	if !ok {
		return *m, nil
	}
	if req == nil {
		if newLine != line {
			m.setBufferString(newLine, runePosForByteOffset(newLine, newBytePos))
		}
		return *m, nil
	}
	return *m, replPickerCmd(req)
}

func (m *replModel) submit() (tea.Model, tea.Cmd) {
	line := string(m.buf)
	if continuation, ok := replMultilineContinuation(line); ok {
		m.pendingLines = append(m.pendingLines, continuation)
		m.buf = nil
		m.cursor = 0
		m.resetHistoryNav()
		return *m, nil
	}

	lines := append(append([]string(nil), m.pendingLines...), line)
	prompt := strings.TrimSpace(strings.Join(lines, "\n"))
	m.pendingLines = nil
	m.buf = nil
	m.cursor = 0
	m.resetHistoryNav()

	if prompt == "" {
		return *m, tea.Println(replPrompt)
	}
	if isREPLExitInput(prompt) {
		return *m, tea.Quit
	}

	m.history = append(m.history, prompt)
	cmd, err := replPromptExecCmd(m.baseArgs, prompt)
	if err != nil {
		return *m, tea.Println(RenderThemedErr(libagent.FormatErrorForCLI(err)))
	}
	return *m, cmd
}

func (m *replModel) insertText(s string) {
	if s == "" {
		return
	}
	runes := []rune(s)
	buf := make([]rune, 0, len(m.buf)+len(runes))
	buf = append(buf, m.buf[:m.cursor]...)
	buf = append(buf, runes...)
	buf = append(buf, m.buf[m.cursor:]...)
	m.buf = buf
	m.cursor += len(runes)
	m.resetHistoryNav()
}

func (m *replModel) deleteBackward() {
	if m.cursor == 0 {
		return
	}
	m.buf = append(m.buf[:m.cursor-1], m.buf[m.cursor:]...)
	m.cursor--
	m.resetHistoryNav()
}

func (m *replModel) deleteForward() {
	if m.cursor >= len(m.buf) {
		return
	}
	m.buf = append(m.buf[:m.cursor], m.buf[m.cursor+1:]...)
	m.resetHistoryNav()
}

func (m *replModel) historyPrev() {
	if len(m.history) == 0 {
		return
	}
	if !m.historyActive {
		m.historyDraft = string(m.buf)
		m.historyIndex = len(m.history)
		m.historyActive = true
	}
	if m.historyIndex <= 0 {
		return
	}
	m.historyIndex--
	m.setBufferString(m.history[m.historyIndex], utf8.RuneCountInString(m.history[m.historyIndex]))
}

func (m *replModel) historyNext() {
	if !m.historyActive {
		return
	}
	if m.historyIndex >= len(m.history)-1 {
		m.historyIndex = -1
		draft := m.historyDraft
		m.historyDraft = ""
		m.historyActive = false
		m.setBufferString(draft, utf8.RuneCountInString(draft))
		return
	}
	m.historyIndex++
	m.setBufferString(m.history[m.historyIndex], utf8.RuneCountInString(m.history[m.historyIndex]))
}

func (m *replModel) resetHistoryNav() {
	if !m.historyActive {
		return
	}
	m.historyIndex = -1
	m.historyDraft = ""
	m.historyActive = false
}

func (m *replModel) setBufferString(line string, cursorRunes int) {
	m.buf = []rune(line)
	m.cursor = max(0, min(cursorRunes, len(m.buf)))
}

func replPromptExecCmd(baseArgs []string, prompt string) (tea.Cmd, error) {
	started := time.Now()
	exePath, err := os.Executable()
	if err != nil {
		return nil, fmt.Errorf("resolve executable path: %w", err)
	}
	cmd := exec.Command(exePath, replSubprocessArgs(baseArgs, prompt)...)
	cmd.Env = os.Environ()
	return tea.ExecProcess(cmd, func(err error) tea.Msg {
		return replRunDoneMsg{
			stats: replRunStats{Duration: time.Since(started)},
			err:   replNormalizeExecError(err),
		}
	}), nil
}

func replPickerCmd(req *replPickerRequest) tea.Cmd {
	if req == nil {
		return nil
	}
	picker := &replPickerExec{
		mode:       req.mode,
		query:      req.query,
		candidates: append([]string(nil), req.candidates...),
	}
	return tea.Exec(picker, func(err error) tea.Msg {
		return replPickerDoneMsg{
			req:      req,
			selected: picker.selected,
			err:      err,
		}
	})
}

func (c *replPickerExec) Run() error {
	switch c.mode {
	case replPickerModeMention:
		var stdout bytes.Buffer
		code, err := shellinit.RunFZF("paths", c.query, bytes.NewReader(nil), &stdout)
		if err != nil {
			return err
		}
		if code == 0 {
			c.selected = strings.TrimSpace(stdout.String())
		}
		return nil
	default:
		var stdin bytes.Buffer
		for _, candidate := range c.candidates {
			if strings.TrimSpace(candidate) == "" {
				continue
			}
			stdin.WriteString(candidate)
			stdin.WriteByte('\n')
		}
		var stdout bytes.Buffer
		code, err := shellinit.RunFZF("repl-complete", c.query, &stdin, &stdout)
		if err != nil {
			return err
		}
		if code == 0 {
			c.selected = strings.TrimSpace(stdout.String())
		}
		return nil
	}
}

func (c *replPickerExec) SetStdin(io.Reader)  {}
func (c *replPickerExec) SetStdout(io.Writer) {}
func (c *replPickerExec) SetStderr(io.Writer) {}

func replStatusCmd(baseArgs []string) tea.Cmd {
	args := append([]string(nil), baseArgs...)
	return func() tea.Msg {
		return replStatusMsg{label: replStatusQuery(args)}
	}
}

func replFeedbackCmd(stats replRunStats, runErr error) tea.Cmd {
	feedback := renderRunFeedback(stats, runErr)
	if feedback == "" {
		return nil
	}
	return tea.Println(feedback)
}

func renderBufferWithCursor(buf []rune, cursor int) string {
	if cursor < 0 {
		cursor = 0
	}
	if cursor > len(buf) {
		cursor = len(buf)
	}
	if cursor == len(buf) {
		return string(buf) + renderReverse(" ")
	}
	return string(buf[:cursor]) + renderReverse(string(buf[cursor])) + string(buf[cursor+1:])
}

func renderReverse(s string) string {
	return "\x1b[7m" + s + "\x1b[0m"
}

func renderRunFeedback(stats replRunStats, runErr error) string {
	if stats.Duration <= 0 {
		return ""
	}
	icon := RenderThemedOK("✓")
	if runErr != nil {
		icon = RenderThemedErr("✗")
	}
	return fmt.Sprintf("%s %s", icon, RenderThemedDim(formatDurationCompact(stats.Duration)))
}

func renderStyledStatusLine(label string) string {
	parts := strings.Split(label, " · ")
	if len(parts) == 0 {
		return RenderThemedDim(label)
	}
	styled := make([]string, 0, len(parts))
	for i, part := range parts {
		trimmed := strings.TrimSpace(part)
		switch {
		case strings.HasPrefix(trimmed, "ctx:"):
			styled = append(styled, styleContextToken(trimmed))
		case isReasoningLabel(trimmed):
			styled = append(styled, RenderThemedWarn(trimmed))
		case i == 0:
			styled = append(styled, RenderThemedModel(trimmed))
		case looksLikePath(trimmed):
			styled = append(styled, RenderThemedDim(trimmed))
		default:
			styled = append(styled, RenderThemedDim(trimmed))
		}
	}
	return strings.Join(styled, RenderThemedDim(" · "))
}

func styleContextToken(token string) string {
	pct, ok := parseContextPercent(token)
	if !ok {
		return RenderThemedDim(token)
	}
	switch {
	case pct >= 85:
		return RenderThemedErr(token)
	case pct >= 60:
		return RenderThemedWarn(token)
	default:
		return RenderThemedOK(token)
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

func replStatusQuery(baseArgs []string) string {
	status := &replStatus{}
	status.update(baseArgs)
	return status.rightPrompt()
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

func (s *replStatus) queryStatus(baseArgs []string) string {
	exePath, err := os.Executable()
	if err != nil {
		return ""
	}
	var buf bytes.Buffer
	cmd := exec.Command(exePath, replSubprocessArgs(baseArgs, "/status")...)
	cmd.Stdout = &buf
	cmd.Stderr = io.Discard
	cmd.Env = os.Environ()
	if err := cmd.Run(); err != nil {
		return ""
	}
	label := parseStatusOutput(buf.String())
	if cwd, err := os.Getwd(); err == nil && cwd != "" {
		home, _ := os.UserHomeDir()
		if home != "" && strings.HasPrefix(cwd, home) {
			cwd = "~" + cwd[len(home):]
		}
		if label != "" {
			label += " · " + cwd
		} else {
			label = cwd
		}
	}
	return label
}

func renderPrintedStatusLine(label string) string {
	if label == "" {
		return RenderThemedWarn("No model configured: use /add-model")
	}
	return RenderThemedInfo("◉") + " " + renderStyledStatusLine(label)
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

func replNormalizeExecError(err error) error {
	if err == nil {
		return nil
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return fmt.Errorf("subprocess exited with code %d", exitErr.ExitCode())
	}
	return fmt.Errorf("run subprocess: %w", err)
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

func replMultilineContinuation(line string) (string, bool) {
	trimmed := strings.TrimRight(line, " \t")
	return strings.TrimSuffix(trimmed, "\\"), strings.HasSuffix(trimmed, "\\")
}

func isREPLExitInput(input string) bool {
	switch strings.ToLower(strings.TrimSpace(input)) {
	case "/exit", "exit", "quit":
		return true
	default:
		return false
	}
}

func replBuildPickerRequest(line string, pos int) (string, int, *replPickerRequest, bool) {
	tokenStart, tokenEnd := replTokenBounds(line, pos)
	tokenPrefix := line[tokenStart:pos]
	if tokenPrefix == "" || strings.HasPrefix(tokenPrefix, ":") {
		return line, pos, nil, true
	}
	if strings.HasPrefix(tokenPrefix, "@") {
		return line, pos, &replPickerRequest{
			mode:       replPickerModeMention,
			line:       line,
			tokenStart: tokenStart,
			tokenEnd:   tokenEnd,
			query:      strings.TrimPrefix(tokenPrefix, "@"),
		}, true
	}
	context := line[:pos]
	candidates := shellinit.Candidates(context)
	if len(candidates) == 0 {
		return line, pos, nil, true
	}
	if len(candidates) == 1 {
		replacement := candidates[0]
		return line[:tokenStart] + replacement + line[tokenEnd:], tokenStart + len(replacement), nil, true
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
	for start > 0 && !isREPLSpace(line[start-1]) {
		start--
	}
	end = pos
	for end < len(line) && !isREPLSpace(line[end]) {
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

func byteOffsetForRunePos(s string, runePos int) int {
	if runePos <= 0 {
		return 0
	}
	if runePos >= utf8.RuneCountInString(s) {
		return len(s)
	}
	offset := 0
	for i := 0; i < runePos && offset < len(s); i++ {
		_, size := utf8.DecodeRuneInString(s[offset:])
		offset += size
	}
	return offset
}

func runePosForByteOffset(s string, bytePos int) int {
	if bytePos <= 0 {
		return 0
	}
	if bytePos >= len(s) {
		return utf8.RuneCountInString(s)
	}
	return utf8.RuneCountInString(s[:bytePos])
}
