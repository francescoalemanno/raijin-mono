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

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textarea"
	tea "charm.land/bubbletea/v2"
	"github.com/francescoalemanno/raijin-mono/internal/persist"
	"github.com/francescoalemanno/raijin-mono/internal/shellinit"
	libagent "github.com/francescoalemanno/raijin-mono/libagent"
	"github.com/google/uuid"
	"golang.org/x/term"
)

const (
	replPrompt             = "raijin❯ "
	replContinuationPrompt = "   ...❯ "
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
	binding  replBinding

	status       string
	statusLoaded bool

	width int

	editor textarea.Model

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

type replBinding struct {
	key      string
	ownerPID int
}

func newREPLEditor() textarea.Model {
	editor := textarea.New()
	editor.ShowLineNumbers = false
	editor.Prompt = ""
	editor.Placeholder = ""
	editor.EndOfBufferCharacter = ' '
	editor.SetVirtualCursor(true)
	editor.SetHeight(1)
	editor.KeyMap.WordBackward = key.NewBinding(
		key.WithKeys("alt+left", "ctrl+left", "alt+b"),
		key.WithHelp("alt+left", "word backward"),
	)
	editor.KeyMap.WordForward = key.NewBinding(
		key.WithKeys("alt+right", "ctrl+right", "alt+f"),
		key.WithHelp("alt+right", "word forward"),
	)
	editor.KeyMap.InsertNewline = key.NewBinding(
		key.WithKeys("alt+enter", "ctrl+j"),
		key.WithHelp("alt+enter", "newline"),
	)
	editor.Focus()
	return editor
}

func RunSubprocessREPL(baseArgs []string) error {
	stdinFD := int(os.Stdin.Fd())
	stdoutFD := int(os.Stdout.Fd())
	if !term.IsTerminal(stdinFD) || !term.IsTerminal(stdoutFD) {
		return errors.New("no prompt provided and repl mode requires a terminal")
	}

	baseArgs = replSanitizeBaseArgs(baseArgs)
	binding, err := replEnsureBindingEnv()
	if err != nil {
		return err
	}
	initialStatus := replStatusQuery(baseArgs, binding)
	fmt.Fprintln(os.Stdout, RenderThemedAccent("Raijin REPL")+" "+RenderThemedDim("subprocess mode"))
	fmt.Fprintln(os.Stdout, RenderThemedDim("ctrl+d or /exit to quit · tab autocomplete · up/down history"))
	fmt.Fprintln(os.Stdout, renderPrintedStatusLine(initialStatus))

	model := replModel{
		baseArgs:     append([]string(nil), baseArgs...),
		binding:      binding,
		status:       initialStatus,
		statusLoaded: true,
		editor:       newREPLEditor(),
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
	m.ensureEditor()
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		return m, nil
	case replStatusMsg:
		m.status = msg.label
		m.statusLoaded = true
		return m, tea.Println(renderPrintedStatusLine(m.status))
	case replRunDoneMsg:
		return m, tea.Batch(replFeedbackCmd(msg.stats, msg.err), replStatusCmd(m.baseArgs, m.binding))
	case replPickerDoneMsg:
		if msg.err != nil {
			return m, tea.Println(RenderThemedErr(libagent.FormatErrorForCLI(msg.err)))
		}
		line, err := replApplyPickerSelection(msg.req, msg.selected)
		if err != nil {
			return m, tea.Println(RenderThemedErr(libagent.FormatErrorForCLI(err)))
		}
		m.resetHistoryNav()
		m.setEditorState(line, runePosForByteOffset(line, replPickerCursorBytePos(msg.req, msg.selected)))
		return m, nil
	case tea.PasteMsg:
		return m, m.updateEditor(msg)
	case tea.KeyPressMsg:
		switch msg.String() {
		case "ctrl+c":
			m.editor.Reset()
			m.resetHistoryNav()
			return m, nil
		case "ctrl+d":
			if m.editor.Value() == "" {
				return m, tea.Quit
			}
			return m, m.updateEditor(msg)
		case "tab":
			return m.handleTab()
		case "enter":
			return m.submit()
		case "up":
			m.historyPrev()
			return m, nil
		case "down":
			m.historyNext()
			return m, nil
		}
		switch {
		case key.Matches(msg, replInputBeginBinding()):
			m.editor.MoveToBegin()
			return m, nil
		case key.Matches(msg, replInputEndBinding()):
			m.editor.MoveToEnd()
			return m, nil
		}
		return m, m.updateEditor(msg)
	}
	return m, nil
}

func (m replModel) View() tea.View {
	m.ensureEditor()
	var b strings.Builder
	lines := strings.Split(m.editor.Value(), "\n")
	if len(lines) == 0 {
		lines = []string{""}
	}
	cursorLine := m.editor.Line()
	cursorColumn := m.editor.Column()

	promptWidth := utf8.RuneCountInString(replPrompt)
	availableWidth := m.width - promptWidth
	if availableWidth <= 0 {
		availableWidth = 80
	}

	for i, logicalLine := range lines {
		runes := []rune(logicalLine)
		if len(runes) == 0 {
			prompt := replContinuationPrompt
			if i == 0 {
				prompt = replPrompt
			}
			b.WriteString(prompt)
			if i == cursorLine {
				b.WriteString(renderBufferWithCursor(nil, 0))
			}
			if i < len(lines)-1 {
				b.WriteByte('\n')
			}
			continue
		}

		for start := 0; start < len(runes); start += availableWidth {
			end := start + availableWidth
			if end > len(runes) {
				end = len(runes)
			}

			prompt := replContinuationPrompt
			if i == 0 && start == 0 {
				prompt = replPrompt
			}
			b.WriteString(prompt)

			if i == cursorLine && cursorColumn >= start && (cursorColumn < end || (cursorColumn == end && end == len(runes))) {
				b.WriteString(renderBufferWithCursor(runes[start:end], cursorColumn-start))
			} else {
				b.WriteString(string(runes[start:end]))
			}

			if end < len(runes) {
				b.WriteByte('\n')
			}
		}

		if i < len(lines)-1 {
			b.WriteByte('\n')
		}
	}

	return tea.NewView(b.String())
}

func (m *replModel) handleTab() (tea.Model, tea.Cmd) {
	m.ensureEditor()
	line := m.editor.Value()
	bytePos := byteOffsetForRunePos(line, m.editorRunePos())
	newLine, newBytePos, req, ok := replBuildPickerRequest(line, bytePos)
	if !ok {
		return *m, nil
	}
	if req == nil {
		if newLine != line {
			m.resetHistoryNav()
			m.setEditorState(newLine, runePosForByteOffset(newLine, newBytePos))
		}
		return *m, nil
	}
	return *m, replPickerCmd(req)
}

func (m *replModel) submit() (tea.Model, tea.Cmd) {
	m.ensureEditor()
	line := m.editor.Value()
	if continuation, ok := replMultilineContinuation(line); ok {
		next := continuation + "\n"
		m.setEditorState(next, utf8.RuneCountInString(next))
		m.resetHistoryNav()
		return *m, nil
	}

	prompt := strings.TrimSpace(line)
	m.editor.Reset()
	m.resetHistoryNav()

	if prompt == "" {
		return *m, tea.Println(replPrompt)
	}
	if isREPLExitInput(prompt) {
		return *m, tea.Quit
	}

	m.history = append(m.history, prompt)
	cmd, err := replPromptExecCmd(m.baseArgs, m.binding, prompt)
	if err != nil {
		return *m, tea.Println(RenderThemedErr(libagent.FormatErrorForCLI(err)))
	}
	return *m, cmd
}

func (m *replModel) updateEditor(msg tea.Msg) tea.Cmd {
	m.ensureEditor()
	before := m.editor.Value()
	var cmd tea.Cmd
	m.editor, cmd = m.editor.Update(msg)
	if m.editor.Value() != before {
		m.resetHistoryNav()
	}
	return cmd
}

func (m *replModel) historyPrev() {
	m.ensureEditor()
	if len(m.history) == 0 {
		return
	}
	if !m.historyActive {
		m.historyDraft = m.editor.Value()
		m.historyIndex = len(m.history)
		m.historyActive = true
	}
	if m.historyIndex <= 0 {
		return
	}
	m.historyIndex--
	m.setEditorState(m.history[m.historyIndex], utf8.RuneCountInString(m.history[m.historyIndex]))
}

func (m *replModel) historyNext() {
	m.ensureEditor()
	if !m.historyActive {
		return
	}
	if m.historyIndex >= len(m.history)-1 {
		m.historyIndex = -1
		draft := m.historyDraft
		m.historyDraft = ""
		m.historyActive = false
		m.setEditorState(draft, utf8.RuneCountInString(draft))
		return
	}
	m.historyIndex++
	m.setEditorState(m.history[m.historyIndex], utf8.RuneCountInString(m.history[m.historyIndex]))
}

func (m *replModel) resetHistoryNav() {
	if !m.historyActive {
		return
	}
	m.historyIndex = -1
	m.historyDraft = ""
	m.historyActive = false
}

func (m *replModel) setEditorState(line string, cursorRunes int) {
	m.ensureEditor()
	m.editor.SetValue(line)
	m.editor.MoveToBegin()
	targetLine, targetCol := replLineColumnForRunePos(line, cursorRunes)
	for m.editor.Line() < targetLine {
		m.editor.CursorDown()
	}
	m.editor.SetCursorColumn(targetCol)
}

func (m replModel) editorRunePos() int {
	return replRunePosForLineColumn(m.editor.Value(), m.editor.Line(), m.editor.Column())
}

func (m *replModel) ensureEditor() {
	if m.editor.LineCount() > 0 {
		return
	}
	m.editor = newREPLEditor()
}

func replInputBeginBinding() key.Binding {
	return key.NewBinding(key.WithKeys("super+left"))
}

func replInputEndBinding() key.Binding {
	return key.NewBinding(key.WithKeys("super+right"))
}

func replPickerCursorBytePos(req *replPickerRequest, selected string) int {
	if req == nil {
		return 0
	}
	replacement := selected
	if req.mode == replPickerModeMention {
		replacement = replFormatMention(selected)
	}
	return req.tokenStart + len(replacement)
}

func replRunePosForLineColumn(value string, line, col int) int {
	lines := strings.Split(value, "\n")
	if len(lines) == 0 {
		return 0
	}
	line = max(0, min(line, len(lines)-1))
	pos := 0
	for i := 0; i < line; i++ {
		pos += len([]rune(lines[i])) + 1
	}
	return pos + max(0, min(col, len([]rune(lines[line]))))
}

func replLineColumnForRunePos(value string, runePos int) (line, col int) {
	lines := strings.Split(value, "\n")
	if len(lines) == 0 {
		return 0, 0
	}
	remaining := max(0, runePos)
	for i, lineText := range lines {
		lineLen := len([]rune(lineText))
		if remaining <= lineLen {
			return i, remaining
		}
		remaining -= lineLen
		if i < len(lines)-1 {
			if remaining == 0 {
				return i + 1, 0
			}
			remaining--
		}
	}
	last := len(lines) - 1
	return last, len([]rune(lines[last]))
}

func replPromptExecCmd(baseArgs []string, binding replBinding, prompt string) (tea.Cmd, error) {
	started := time.Now()
	exePath, err := os.Executable()
	if err != nil {
		return nil, fmt.Errorf("resolve executable path: %w", err)
	}
	cmd := exec.Command(exePath, replSubprocessArgs(baseArgs, prompt)...)
	cmd.Env = replCommandEnv(binding)
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

func replStatusCmd(baseArgs []string, binding replBinding) tea.Cmd {
	args := append([]string(nil), baseArgs...)
	return func() tea.Msg {
		return replStatusMsg{label: replStatusQuery(args, binding)}
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

func replStatusQuery(baseArgs []string, binding replBinding) string {
	status := &replStatus{}
	status.update(baseArgs, binding)
	return status.rightPrompt()
}

func (s *replStatus) update(baseArgs []string, binding replBinding) {
	label := s.queryStatus(baseArgs, binding)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.label = label
}

func (s *replStatus) rightPrompt() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.label
}

func (s *replStatus) queryStatus(baseArgs []string, binding replBinding) string {
	exePath, err := os.Executable()
	if err != nil {
		return ""
	}
	var buf bytes.Buffer
	cmd := exec.Command(exePath, replSubprocessArgs(baseArgs, "/status")...)
	cmd.Stdout = &buf
	cmd.Stderr = io.Discard
	cmd.Env = replCommandEnv(binding)
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

func replEnsureBindingEnv() (replBinding, error) {
	key := strings.TrimSpace(os.Getenv(persist.SessionBindingKeyEnv))
	if rawOwner := strings.TrimSpace(os.Getenv(persist.SessionBindingOwnerPIDEnv)); key != "" && rawOwner != "" {
		ownerPID, err := strconv.Atoi(rawOwner)
		if err == nil && ownerPID > 0 {
			return replBinding{key: key, ownerPID: ownerPID}, nil
		}
	}

	binding := replBinding{
		key:      "repl-" + uuid.NewString(),
		ownerPID: os.Getpid(),
	}
	if err := os.Setenv(persist.SessionBindingKeyEnv, binding.key); err != nil {
		return replBinding{}, fmt.Errorf("set repl binding key env: %w", err)
	}
	if err := os.Setenv(persist.SessionBindingOwnerPIDEnv, strconv.Itoa(binding.ownerPID)); err != nil {
		return replBinding{}, fmt.Errorf("set repl binding owner env: %w", err)
	}
	return binding, nil
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

func replSanitizeBaseArgs(baseArgs []string) []string {
	out := make([]string, 0, len(baseArgs))
	for _, arg := range baseArgs {
		if arg == "--new" {
			continue
		}
		out = append(out, arg)
	}
	return out
}

func replCommandEnv(binding replBinding) []string {
	env := os.Environ()
	if binding.key == "" || binding.ownerPID <= 0 {
		return env
	}
	env = append(env, persist.SessionBindingKeyEnv+"="+binding.key)
	env = append(env, persist.SessionBindingOwnerPIDEnv+"="+strconv.Itoa(binding.ownerPID))
	return env
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
