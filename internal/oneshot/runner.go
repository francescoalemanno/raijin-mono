// Package oneshot implements the first-class non-interactive CLI mode for Raijin.
// It streams events to stderr with styled status lines and writes the final
// assistant response to stdout. Conversational commands require an explicit
// bound REPL or shell context.
package oneshot

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/francescoalemanno/raijin-mono/internal/compaction"
	libagent "github.com/francescoalemanno/raijin-mono/libagent"
	"golang.org/x/term"

	"github.com/francescoalemanno/raijin-mono/internal/agent"
	"github.com/francescoalemanno/raijin-mono/internal/commands"
	modelconfig "github.com/francescoalemanno/raijin-mono/internal/config"
	"github.com/francescoalemanno/raijin-mono/internal/input"
	"github.com/francescoalemanno/raijin-mono/internal/persist"
	"github.com/francescoalemanno/raijin-mono/internal/prompts"
	"github.com/francescoalemanno/raijin-mono/internal/ralph"
	"github.com/francescoalemanno/raijin-mono/internal/session"
	"github.com/francescoalemanno/raijin-mono/internal/skills"
	"github.com/francescoalemanno/raijin-mono/internal/substitution"
)

func effectiveContextWindow(opts Options) int64 {
	contextWindow := opts.RuntimeModel.EffectiveContextWindow()
	if contextWindow <= 0 {
		contextWindow = opts.ModelCfg.ContextWindow
	}
	return contextWindow
}

// Options configures a one-shot run.
type Options struct {
	RuntimeModel libagent.RuntimeModel
	ModelCfg     libagent.ModelConfig
	Store        *modelconfig.ModelStore
	ForceNew     bool
	Ephemeral    bool
}

const assistantCaptureEnv = "RAIJIN_ASSISTANT_CAPTURE_FILE"

// Run executes a single prompt in non-interactive CLI mode.
// Slash commands are supported: non-interactive ones run inline,
// interactive ones launch a Bubbletea selector.
func Run(opts Options, rawPrompt string) error {
	rawPrompt = strings.TrimSpace(rawPrompt)
	if rawPrompt == "" {
		return errors.New("empty prompt")
	}

	// Check for /new prefix: strip it and force a new session.
	forceNew := opts.ForceNew
	if strings.HasPrefix(rawPrompt, "/new") {
		rest := strings.TrimSpace(strings.TrimPrefix(rawPrompt, "/new"))
		if rest == "" {
			// Bare "/new" — just create a new session and exit.
			return handleNew(opts)
		}
		// "/new <prompt>" — force new session then run the prompt.
		rawPrompt = rest
		forceNew = true
	}

	// Resolve the prompt through the same pipeline as interactive mode.
	resolved, err := resolvePrompt(rawPrompt)
	if err != nil {
		return err
	}

	// Handle builtin commands.
	if resolved.builtin != nil {
		return handleBuiltin(opts, *resolved, forceNew)
	}

	// Regular prompt — run through agent.
	return runPrompt(opts, resolved.promptText, forceNew)
}

// ---------------------------------------------------------------------------
// Prompt resolution (reuses chat pipeline types)
// ---------------------------------------------------------------------------

type builtinCmd struct {
	name   string
	args   string
	fields []string
}

type resolvedPrompt struct {
	promptText string
	builtin    *builtinCmd
	template   string
}

func resolvePrompt(raw string) (*resolvedPrompt, error) {
	text := strings.TrimSpace(raw)
	if text == "" {
		return nil, errors.New("empty prompt")
	}

	if !strings.HasPrefix(text, "/") {
		expanded, _ := substitution.ExpandShellSubstitutions(context.Background(), text)
		return &resolvedPrompt{promptText: expanded}, nil
	}

	fields := strings.Fields(text)
	if len(fields) == 0 {
		return nil, errors.New("empty prompt")
	}

	cmdToken := fields[0]
	if !strings.HasPrefix(cmdToken, "/") {
		expanded, _ := substitution.ExpandShellSubstitutions(context.Background(), text)
		return &resolvedPrompt{promptText: expanded}, nil
	}

	commandName := strings.TrimPrefix(cmdToken, "/")
	args := text[len(cmdToken):]

	// Check if it's a builtin command.
	if commands.IsBuiltin(commandName) {
		return &resolvedPrompt{builtin: &builtinCmd{
			name:   commandName,
			args:   args,
			fields: fields,
		}}, nil
	}

	// Check prompt templates.
	tmpl, found := prompts.Find(commandName)
	if !found {
		return nil, fmt.Errorf("unknown command: %s", commandName)
	}

	args = strings.TrimSpace(args)
	expanded := substitution.ExpandAll(context.Background(), strings.TrimSpace(tmpl.Content), args, substitution.ArgModeList)
	return &resolvedPrompt{
		promptText: expanded,
		template:   tmpl.Name,
	}, nil
}

// ---------------------------------------------------------------------------
// Builtin command dispatch
// ---------------------------------------------------------------------------

func handleBuiltin(opts Options, resolved resolvedPrompt, forceNew bool) error {
	cmd := resolved.builtin
	switch {
	case cmd.name == "help":
		return handleHelp()

	case cmd.name == "exit":
		return nil

	case cmd.name == "new":
		return handleNew(opts)

	case cmd.name == "status":
		return handleStatus(opts, forceNew)

	case cmd.name == "reasoning":
		return handleReasoning(opts, strings.TrimSpace(cmd.args))

	case cmd.name == "edit":
		return handleEdit(opts, cmd.args, forceNew)

	case cmd.name == "compact":
		instructions := strings.TrimSpace(cmd.args)
		return handleCompact(opts, instructions, forceNew)

	case cmd.name == "plan":
		return handlePlan(strings.TrimSpace(cmd.args))

	case cmd.name == "sessions":
		return handleSessions(opts)

	case cmd.name == "tree":
		return handleTree(opts)

	case cmd.name == "history":
		return handleHistory(opts, forceNew)

	case cmd.name == "retry":
		return handleRetry(opts)

	case cmd.name == "models" && len(cmd.fields) == 1:
		return handleModels(opts)

	case cmd.name == "add-model":
		return handleModelsAdd(opts)

	case cmd.name == "setup":
		return handleSetup(opts, cmd.args)

	default:
		return fmt.Errorf("unknown command: %s", cmd.name)
	}
}

type editorCommand struct {
	path string
	args []string
}

func handleEdit(opts Options, initialContent string, forceNew bool) error {
	return handleEditWithRunner(opts, initialContent, forceNew, runPrompt)
}

func handleEditWithRunner(opts Options, initialContent string, forceNew bool, runner func(Options, string, bool) error) error {
	if runner == nil {
		return errors.New("prompt runner is required")
	}

	prompt, err := capturePromptFromEditor(strings.TrimLeft(initialContent, " \t"))
	if err != nil {
		return err
	}
	if strings.TrimSpace(prompt) == "" {
		return errors.New("editor buffer is empty")
	}
	return runner(opts, prompt, forceNew)
}

func capturePromptFromEditor(initialContent string) (string, error) {
	tempFile, err := os.CreateTemp("", "raijin-edit-*.md")
	if err != nil {
		return "", fmt.Errorf("create temp file for /edit: %w", err)
	}
	tempPath := tempFile.Name()
	defer func() { _ = os.Remove(tempPath) }()

	if initialContent != "" {
		if _, err := tempFile.WriteString(initialContent); err != nil {
			_ = tempFile.Close()
			return "", fmt.Errorf("write initial /edit content: %w", err)
		}
	}
	if err := tempFile.Close(); err != nil {
		return "", fmt.Errorf("close temp file for /edit: %w", err)
	}

	editor, err := resolveEditorCommand(os.Getenv, exec.LookPath)
	if err != nil {
		return "", err
	}

	args := append(append([]string{}, editor.args...), tempPath)
	cmd := exec.Command(editor.path, args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = os.Environ()
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("run editor command: %w", err)
	}

	content, err := os.ReadFile(tempPath)
	if err != nil {
		return "", fmt.Errorf("read temp file from /edit: %w", err)
	}
	return string(content), nil
}

func resolveEditorCommand(getenv func(string) string, lookPath func(string) (string, error)) (editorCommand, error) {
	if getenv == nil {
		getenv = os.Getenv
	}
	if lookPath == nil {
		lookPath = exec.LookPath
	}

	if editorSpec := strings.TrimSpace(getenv("EDITOR")); editorSpec != "" {
		parts := substitution.ParseCommandArgs(editorSpec)
		if len(parts) == 0 {
			return editorCommand{}, errors.New("EDITOR is set but empty after parsing")
		}
		path, err := lookPath(parts[0])
		if err != nil {
			return editorCommand{}, fmt.Errorf("EDITOR command %q not found: %w", parts[0], err)
		}
		return editorCommand{
			path: path,
			args: parts[1:],
		}, nil
	}

	fallback := []string{"micro", "nano", "nvim", "vim", "vi"}
	for _, name := range fallback {
		path, err := lookPath(name)
		if err == nil {
			return editorCommand{path: path}, nil
		}
	}
	return editorCommand{}, errors.New("no editor found; set EDITOR or install one of: micro, nano, nvim, vim, vi")
}

func handleNew(opts Options) error {
	if _, err := openSession(opts, true, true); err != nil {
		return err
	}
	fmt.Fprintln(os.Stderr, renderStatusSuccess("✓")+" New session created")
	return nil
}

func handleHelp() error {
	var b strings.Builder
	b.WriteString(commands.HelpText())
	b.WriteString(renderTemplates())
	b.WriteString(renderSkills())
	fmt.Print(b.String())
	return nil
}

func renderTemplates() string {
	templates := prompts.GetTemplates()
	if len(templates) == 0 {
		return "No prompt templates loaded\n"
	}
	var b strings.Builder
	b.WriteString("Prompt templates:\n")
	for _, tmpl := range templates {
		desc := strings.TrimSpace(tmpl.Description)
		if desc == "" {
			desc = "(no description)"
		}
		fmt.Fprintf(&b, "  /%-18s %s [%s]\n", tmpl.Name, desc, tmpl.Source)
	}
	return b.String()
}

func renderSkills() string {
	skillsList := skills.GetSkills()
	if len(skillsList) == 0 {
		return "No skills loaded\n"
	}
	var b strings.Builder
	b.WriteString("\nSkills:\n")
	for _, skill := range skillsList {
		desc := strings.TrimSpace(skill.Description)
		if desc == "" {
			desc = "(no description)"
		}
		fmt.Fprintf(&b, "  +%-18s %s [%s]\n", skill.Name, desc, skill.Source)
	}
	return b.String()
}

func handleCompact(opts Options, instructions string, forceNew bool) error {
	sess, err := openSession(opts, forceNew, false)
	if err != nil {
		return err
	}

	if sess.Agent() == nil || sess.ID() == "" {
		return errors.New("no model configured")
	}

	_, _, err = compactSession(context.Background(), sess, opts.RuntimeModel, instructions, func(ev libagent.ContextCompactionEvent) {
		icon, text, ok := contextCompactionStatusParts(ev)
		if !ok {
			return
		}
		fmt.Fprintln(os.Stderr, formatStatusLine(icon, text))
	})
	if err != nil {
		return err
	}
	return nil
}

func handlePlan(rawArgs string) error {
	status, err := ralph.InspectPlanningState(context.Background(), "")
	if err != nil {
		return err
	}

	request := strings.TrimSpace(rawArgs)
	if status.State == ralph.PlanningStateEmpty {
		request, err = resolvePlanningRequest(request)
		if err != nil || request == "" {
			return err
		}
		if err := runPlanningRequest(request, true); err != nil {
			return err
		}
		return handlePostPlanFlow()
	}

	action, ok, err := runPlanActionPicker(status.State, request != "")
	if err != nil {
		return err
	}
	if !ok {
		return nil
	}

	switch action {
	case planActionReview:
		return renderCurrentPlan()
	case planActionRevise:
		request, err = resolvePlanningRequest(request)
		if err != nil || request == "" {
			return err
		}
		if err := runPlanningRequest(request, false); err != nil {
			return err
		}
		return handlePostPlanFlow()
	case planActionRun:
		return runExistingPlan()
	case planActionScratch:
		request, err = resolvePlanningRequest(request)
		if err != nil || request == "" {
			return err
		}
		if err := runPlanningRequest(request, true); err != nil {
			return err
		}
		return handlePostPlanFlow()
	default:
		return nil
	}
}

type planAction string

const (
	planActionReview  planAction = "review"
	planActionRevise  planAction = "revise"
	planActionRun     planAction = "run"
	planActionScratch planAction = "scratch"
	planActionCancel  planAction = "cancel"
)

type postPlanAction string

const (
	postPlanActionRun    postPlanAction = "run"
	postPlanActionRevise postPlanAction = "revise"
	postPlanActionClose  postPlanAction = "close"
)

var (
	runPlanActionPicker     = defaultRunPlanActionPicker
	runPostPlanActionPicker = defaultRunPostPlanActionPicker
)

func defaultRunPlanActionPicker(state ralph.PlanningState, hasRequest bool) (planAction, bool, error) {
	if !term.IsTerminal(int(os.Stdin.Fd())) || !term.IsTerminal(int(os.Stdout.Fd())) {
		return "", false, fmt.Errorf("interactive picker requires a TTY")
	}

	items := []planActionItem{
		{action: planActionReview, label: "Review current goal and plan", desc: "Render the saved goal and checklist"},
		{action: planActionRevise, label: "Revise current plan", desc: "Adjust the current goal and plan in place"},
		{action: planActionRun, label: "Run current plan", desc: "Start Ralph execution from the saved plan"},
		{action: planActionScratch, label: "Start a new plan from scratch", desc: "Discard saved Ralph state and replan"},
		{action: planActionCancel, label: "Cancel", desc: "Leave Ralph unchanged"},
	}

	initialAction := planActionRun
	switch {
	case hasRequest:
		initialAction = planActionRevise
	case state == ralph.PlanningStateCompleted:
		initialAction = planActionReview
	}

	cursor := 0
	for i, item := range items {
		if item.action == initialAction {
			cursor = i
			break
		}
	}

	fl := newFilterList(
		"RALPH ACTIONS",
		items,
		cursor,
		0,
		func(item planActionItem) string {
			return string(item.action) + " " + item.label + " " + item.desc
		},
		func(item planActionItem, selected bool) string {
			label := item.label
			if item.desc != "" {
				label += " — " + item.desc
			}
			pointer := "  "
			if selected {
				pointer = "→ "
				return oneshotAccentStyle.Bold(true).Render(pointer + label)
			}
			if item.action == planActionCancel {
				return oneshotMutedStyle.Render(pointer + label)
			}
			return oneshotNormalStyle.Render(pointer + label)
		},
	)

	final, err := tea.NewProgram(fl, tea.WithAltScreen()).Run()
	if err != nil {
		return "", false, err
	}
	result := final.(filterList[planActionItem])
	if result.quitting || result.chosen == nil || result.chosen.action == planActionCancel {
		return "", false, nil
	}
	return result.chosen.action, true, nil
}

func defaultRunPostPlanActionPicker() (postPlanAction, bool, error) {
	if !term.IsTerminal(int(os.Stdin.Fd())) || !term.IsTerminal(int(os.Stdout.Fd())) {
		return "", false, fmt.Errorf("interactive picker requires a TTY")
	}

	items := []postPlanActionItem{
		{action: postPlanActionRun, label: "Run now", desc: "Start the freshly reviewed plan"},
		{action: postPlanActionRevise, label: "Revise again", desc: "Open another planning iteration"},
		{action: postPlanActionClose, label: "Close", desc: "Leave the current plan as is"},
	}

	fl := newFilterList(
		"NEXT STEP",
		items,
		0,
		0,
		func(item postPlanActionItem) string {
			return string(item.action) + " " + item.label + " " + item.desc
		},
		func(item postPlanActionItem, selected bool) string {
			label := item.label
			if item.desc != "" {
				label += " — " + item.desc
			}
			pointer := "  "
			if selected {
				pointer = "→ "
				return oneshotAccentStyle.Bold(true).Render(pointer + label)
			}
			if item.action == postPlanActionClose {
				return oneshotMutedStyle.Render(pointer + label)
			}
			return oneshotNormalStyle.Render(pointer + label)
		},
	)

	final, err := tea.NewProgram(fl, tea.WithAltScreen()).Run()
	if err != nil {
		return "", false, err
	}
	result := final.(filterList[postPlanActionItem])
	if result.quitting || result.chosen == nil || result.chosen.action == postPlanActionClose {
		return "", false, nil
	}
	return result.chosen.action, true, nil
}

type planActionItem struct {
	action planAction
	label  string
	desc   string
}

type postPlanActionItem struct {
	action postPlanAction
	label  string
	desc   string
}

func resolvePlanningRequest(request string) (string, error) {
	request = strings.TrimSpace(request)
	if request != "" {
		return request, nil
	}

	prompt, err := capturePromptFromEditor("")
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(prompt) == "" {
		fmt.Fprintln(os.Stderr, renderStatusWarning("●")+" Plan prompt was empty; nothing changed")
		return "", nil
	}
	return strings.TrimSpace(prompt), nil
}

func runPlanningRequest(request string, reset bool) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	err := runRalph(ctx, ralph.Options{
		PlanningRequest: request,
		Mode:            ralph.ModePlan,
		ResetPlan:       reset,
	})
	if errors.Is(err, context.Canceled) {
		fmt.Fprintln(os.Stderr, "Ralph interrupted")
		return nil
	}
	return err
}

func runExistingPlan() error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	err := runRalph(ctx, ralph.Options{
		Mode: ralph.ModeAuto,
	})
	if errors.Is(err, context.Canceled) {
		fmt.Fprintln(os.Stderr, "Ralph interrupted")
		return nil
	}
	return err
}

func renderCurrentPlan() error {
	snapshot, err := ralph.ReadSnapshot(context.Background(), "")
	if err != nil {
		return err
	}

	var doc strings.Builder
	doc.WriteString("# Goal\n\n")
	doc.WriteString(snapshot.Goal)
	doc.WriteString("\n\n# Plan\n\n")
	doc.WriteString(snapshot.Plan)
	doc.WriteString("\n")

	renderMarkdownDocument(os.Stdout, doc.String())
	return nil
}

func handlePostPlanFlow() error {
	for {
		if err := renderCurrentPlan(); err != nil {
			return err
		}
		action, ok, err := runPostPlanActionPicker()
		if err != nil {
			return err
		}
		if !ok {
			return nil
		}
		switch action {
		case postPlanActionRun:
			return runExistingPlan()
		case postPlanActionRevise:
			request, err := resolvePlanningRequest("")
			if err != nil || request == "" {
				return err
			}
			if err := runPlanningRequest(request, false); err != nil {
				return err
			}
		default:
			return nil
		}
	}
}

func handleSessions(opts Options) error {
	sess, err := openSession(opts, false, false)
	if err != nil {
		return err
	}
	summaries := sess.ListSessionSummaries()
	if len(summaries) == 0 {
		fmt.Println("No previous sessions found")
		return nil
	}

	return runSessionSelector(summaries, sess)
}

func handleTree(opts Options) error {
	sess, err := openSession(opts, false, false)
	if err != nil {
		return err
	}
	entries := sess.GetTree()
	if len(entries) == 0 {
		fmt.Println("No history to navigate")
		return nil
	}

	return runTreeSelector(entries, sess)
}

func handleHistory(opts Options, forceNew bool) error {
	sess, err := openSession(opts, forceNew, false)
	if err != nil {
		return err
	}

	items, err := sess.ListReplayItems()
	if err != nil {
		return err
	}

	isTTY := term.IsTerminal(int(os.Stderr.Fd()))
	r := newRenderer(os.Stderr, os.Stdout, sess.Tools(), isTTY)
	if replayed := replaySessionEvents(r, items); replayed == 0 {
		fmt.Println("No session output yet")
	}
	return nil
}

func handleRetry(opts Options) error {
	sess, err := openSession(opts, false, false)
	if err != nil {
		return err
	}
	if sess.Agent() == nil {
		return errors.New("no model configured; use /add-model to set up")
	}
	if sess.ID() == "" {
		return errors.New("no active session to retry")
	}

	msgs, err := sess.ListMessages(context.Background())
	if err != nil {
		return err
	}
	if len(msgs) == 0 {
		return errors.New("no session state to retry")
	}
	if last := msgs[len(msgs)-1]; last.GetRole() == "assistant" {
		retryFromID := ""
		for i := len(msgs) - 2; i >= 0; i-- {
			if msgs[i].GetRole() == "assistant" {
				continue
			}
			retryFromID = strings.TrimSpace(libagent.MessageID(msgs[i]))
			if retryFromID != "" {
				break
			}
		}
		if retryFromID == "" {
			return errors.New("no retryable state before the final assistant response")
		}
		if err := sess.SetLeaf(retryFromID); err != nil {
			return err
		}
		msgs, err = sess.ListMessages(context.Background())
		if err != nil {
			return err
		}
		if len(msgs) == 0 {
			return errors.New("no session state to retry")
		}
	}

	msgs, err = sess.ListMessages(context.Background())
	if err != nil {
		return err
	}

	isTTY := term.IsTerminal(int(os.Stderr.Fd()))
	r := newRendererWithOptions(os.Stderr, os.Stdout, sess.Tools(), isTTY, rendererOptions{
		persistentSpinner: true,
		modelLabel:        statusModelLabel(opts),
		contextWindow:     opts.RuntimeModel.EffectiveContextWindow(),
		initialMessages:   msgs,
	})
	if r.contextWindow <= 0 {
		r.contextWindow = opts.ModelCfg.ContextWindow
	}
	r.startPersistentSpinner()
	defer r.stopPersistentSpinner()

	maxTokens := opts.ModelCfg.MaxTokens
	if maxTokens <= 0 {
		maxTokens = libagent.DefaultMaxTokens
	}

	sess.SetEventCallback(func(event libagent.AgentEvent) {
		r.handleEvent(event)
	})

	runErr := sess.Agent().Continue(context.Background(), agent.SessionAgentCall{
		SessionID:       sess.ID(),
		MaxOutputTokens: maxTokens,
	})
	_ = sess.EnsurePersisted()
	return runErr
}

func replaySessionEvents(r *renderer, items []persist.ReplayItem) int {
	if r == nil {
		return 0
	}

	type toolCallKey struct {
		id   string
		name string
	}

	pendingCalls := make(map[toolCallKey][]libagent.ToolCallItem)

	replayed := 0
	r.handleEvent(libagent.AgentEvent{Type: libagent.AgentEventTypeAgentStart})

	for _, item := range items {
		if item.ContextCompaction != nil {
			r.handleEvent(libagent.AgentEvent{
				Type:              libagent.AgentEventTypeContextCompaction,
				ContextCompaction: item.ContextCompaction,
			})
			continue
		}
		msg := item.Message
		if msg == nil {
			continue
		}
		switch m := msg.(type) {
		case *libagent.UserMessage:
			if strings.TrimSpace(m.Content) == "" && len(m.Files) == 0 {
				continue
			}
			replayed++
			r.handleEvent(libagent.AgentEvent{
				Type:    libagent.AgentEventTypeMessageStart,
				Message: m,
			})
			r.handleEvent(libagent.AgentEvent{
				Type:    libagent.AgentEventTypeMessageEnd,
				Message: m,
			})

		case *libagent.AssistantMessage:
			reasoning := libagent.AssistantReasoning(m)
			text := libagent.AssistantText(m)
			toolCalls := libagent.AssistantToolCalls(m)

			msgID := libagent.MessageID(m)
			if strings.TrimSpace(reasoning) != "" || strings.TrimSpace(text) != "" {
				replayed++
			}

			// Emit turn start/end around each assistant message for proper state reset.
			r.handleEvent(libagent.AgentEvent{Type: libagent.AgentEventTypeTurnStart})

			r.handleEvent(libagent.AgentEvent{
				Type:    libagent.AgentEventTypeMessageStart,
				Message: m,
			})

			if strings.TrimSpace(reasoning) != "" {
				r.handleEvent(libagent.AgentEvent{
					Type:    libagent.AgentEventTypeMessageUpdate,
					Message: m,
					Delta: &libagent.StreamDelta{
						Type: "reasoning_start",
						ID:   msgID + "-reasoning",
					},
				})
				r.handleEvent(libagent.AgentEvent{
					Type:    libagent.AgentEventTypeMessageUpdate,
					Message: m,
					Delta: &libagent.StreamDelta{
						Type:  "reasoning_delta",
						ID:    msgID + "-reasoning",
						Delta: reasoning,
					},
				})
				r.handleEvent(libagent.AgentEvent{
					Type:    libagent.AgentEventTypeMessageUpdate,
					Message: m,
					Delta: &libagent.StreamDelta{
						Type: "reasoning_end",
						ID:   msgID + "-reasoning",
					},
				})
			}

			if strings.TrimSpace(text) != "" {
				r.handleEvent(libagent.AgentEvent{
					Type:    libagent.AgentEventTypeMessageUpdate,
					Message: m,
					Delta: &libagent.StreamDelta{
						Type: "text_start",
						ID:   msgID + "-text",
					},
				})
				r.handleEvent(libagent.AgentEvent{
					Type:    libagent.AgentEventTypeMessageUpdate,
					Message: m,
					Delta: &libagent.StreamDelta{
						Type:  "text_delta",
						ID:    msgID + "-text",
						Delta: text,
					},
				})
				r.handleEvent(libagent.AgentEvent{
					Type:    libagent.AgentEventTypeMessageUpdate,
					Message: m,
					Delta: &libagent.StreamDelta{
						Type: "text_end",
						ID:   msgID + "-text",
					},
				})
			}

			for _, call := range toolCalls {
				key := toolCallKey{
					id:   strings.TrimSpace(call.ID),
					name: strings.TrimSpace(call.Name),
				}
				pendingCalls[key] = append(pendingCalls[key], call)
			}

			r.handleEvent(libagent.AgentEvent{
				Type:    libagent.AgentEventTypeMessageEnd,
				Message: m,
			})

			r.handleEvent(libagent.AgentEvent{Type: libagent.AgentEventTypeTurnEnd})

		case *libagent.ToolResultMessage:
			replayed++
			key := toolCallKey{
				id:   strings.TrimSpace(m.ToolCallID),
				name: strings.TrimSpace(m.ToolName),
			}

			toolArgs := ""
			if queued := pendingCalls[key]; len(queued) > 0 {
				toolArgs = queued[0].Input
				if len(queued) == 1 {
					delete(pendingCalls, key)
				} else {
					pendingCalls[key] = queued[1:]
				}
			}

			r.handleEvent(libagent.AgentEvent{
				Type:       libagent.AgentEventTypeToolExecutionStart,
				ToolCallID: m.ToolCallID,
				ToolName:   m.ToolName,
				ToolArgs:   toolArgs,
			})
			r.handleEvent(libagent.AgentEvent{
				Type:        libagent.AgentEventTypeToolExecutionEnd,
				ToolCallID:  m.ToolCallID,
				ToolName:    m.ToolName,
				ToolArgs:    toolArgs,
				ToolResult:  m.Content,
				ToolIsError: m.IsError,
			})
			r.handleEvent(libagent.AgentEvent{
				Type:    libagent.AgentEventTypeMessageEnd,
				Message: m,
			})
		}
	}

	r.handleEvent(libagent.AgentEvent{Type: libagent.AgentEventTypeTurnEnd})
	r.handleEvent(libagent.AgentEvent{Type: libagent.AgentEventTypeAgentEnd})

	return replayed
}

func handleModels(opts Options) error {
	if opts.Store == nil {
		return errors.New("no model store available")
	}
	models := opts.Store.List()
	if len(models) == 0 {
		fmt.Println("No models configured")
		return nil
	}
	return runModelSelector(opts.Store)
}

func handleStatus(opts Options, forceNew bool) error {
	sess, err := openSession(opts, forceNew, false)
	if err != nil {
		return err
	}

	msgs, err := sess.ListMessages(context.Background())
	if err != nil {
		return err
	}

	usedTokens := compaction.ApproximateConversationUsageTokens(msgs)

	contextWindow := effectiveContextWindow(opts)

	fmt.Printf("Model: %s\n", statusModelLabel(opts))
	fmt.Printf("Reasoning: %s\n", statusReasoningLabel(opts))
	if contextWindow > 0 {
		pct := float64(usedTokens) / float64(contextWindow) * 100
		fmt.Printf("Context: %.1f%% (%s/%s)\n", pct, formatStatusTokenCount(usedTokens), formatStatusTokenCount(contextWindow))
	} else {
		fmt.Printf("Context: unknown (%s used)\n", formatStatusTokenCount(usedTokens))
	}

	return nil
}

func renderMarkdownDocument(w io.Writer, doc string) {
	if w == nil {
		return
	}

	r := newLineMarkdownRenderer()
	for _, line := range strings.Split(strings.ReplaceAll(doc, "\r\n", "\n"), "\n") {
		rendered := r.RenderLine(line)
		if rendered == "" {
			if strings.TrimSpace(line) == "" {
				fmt.Fprintln(w)
			}
			continue
		}
		fmt.Fprintln(w, rendered)
	}
	if tail := r.FlushTable(); tail != "" {
		fmt.Fprintln(w, tail)
	}
}

func statusModelLabel(opts Options) string {
	provider := strings.TrimSpace(opts.ModelCfg.Provider)
	model := strings.TrimSpace(opts.ModelCfg.Model)
	if provider == "" {
		provider = strings.TrimSpace(opts.RuntimeModel.ModelInfo.ProviderID)
	}
	if model == "" {
		model = strings.TrimSpace(opts.RuntimeModel.ModelInfo.ModelID)
	}

	switch {
	case provider == "" && model == "":
		return "(not configured)"
	case provider == "":
		return model
	case model == "":
		return provider
	default:
		return provider + "/" + model
	}
}

func statusReasoningLabel(opts Options) string {
	provider := strings.TrimSpace(opts.ModelCfg.Provider)
	model := strings.TrimSpace(opts.ModelCfg.Model)
	if provider == "" {
		provider = strings.TrimSpace(opts.RuntimeModel.ModelInfo.ProviderID)
	}
	if model == "" {
		model = strings.TrimSpace(opts.RuntimeModel.ModelInfo.ModelID)
	}
	if provider == "" && model == "" {
		return "(not configured)"
	}

	level := opts.ModelCfg.ThinkingLevel
	if strings.TrimSpace(string(level)) == "" {
		level = opts.RuntimeModel.ModelCfg.ThinkingLevel
	}
	return string(libagent.NormalizeThinkingLevel(level))
}

func formatStatusTokenCount(tokens int64) string {
	if tokens >= 1000 {
		return fmt.Sprintf("%dk", tokens/1000)
	}
	return fmt.Sprintf("%d", tokens)
}

// ---------------------------------------------------------------------------
// Session management
// ---------------------------------------------------------------------------

func openSession(opts Options, forceNew, createIfMissing bool) (*session.Session, error) {
	if opts.Ephemeral {
		sess, err := session.NewEphemeral(opts.RuntimeModel)
		if err != nil && sess == nil {
			return nil, err
		}
		if sess == nil {
			return nil, errors.New("failed to create session")
		}
		if forceNew || createIfMissing {
			if err := sess.StartEphemeral(context.Background()); err != nil {
				return nil, err
			}
		}
		return sess, nil
	}

	sess, err := session.New(opts.RuntimeModel)
	if err != nil && sess == nil {
		return nil, err
	}
	if sess == nil {
		return nil, errors.New("failed to create session")
	}
	if err := sess.Bind(context.Background(), forceNew, createIfMissing); err != nil {
		return nil, err
	}
	return sess, nil
}

// ---------------------------------------------------------------------------
// Prompt execution
// ---------------------------------------------------------------------------

func runPrompt(opts Options, promptText string, forceNew bool) error {
	sess, err := openSession(opts, forceNew, true)
	if err != nil {
		return err
	}
	return runPromptWithSession(opts, sess, promptText)
}

func runPromptWithSession(opts Options, sess *session.Session, promptText string) error {
	if sess.Agent() == nil || sess.ID() == "" {
		return errors.New("no model configured; use /add-model to set up")
	}

	msgs, err := sess.ListMessages(context.Background())
	if err != nil {
		return err
	}

	isTTY := term.IsTerminal(int(os.Stderr.Fd()))
	r := newRendererWithOptions(os.Stderr, os.Stdout, sess.Tools(), isTTY, rendererOptions{
		persistentSpinner: true,
		modelLabel:        statusModelLabel(opts),
		contextWindow:     opts.RuntimeModel.EffectiveContextWindow(),
		initialMessages:   msgs,
	})
	if r.contextWindow <= 0 {
		r.contextWindow = opts.ModelCfg.ContextWindow
	}
	r.startPersistentSpinner()
	defer r.stopPersistentSpinner()

	// Parse file attachments from the prompt.
	text, files, err := input.ParseAndLoadResources(promptText)
	if err != nil {
		return err
	}
	attachments := make([]libagent.FilePart, 0, len(files))
	for _, f := range files {
		attachments = append(attachments, libagent.FilePart{
			Filename:  f.Path,
			MediaType: f.MediaType,
			Data:      f.Data,
		})
	}

	maxTokens := opts.ModelCfg.MaxTokens
	if maxTokens <= 0 {
		maxTokens = libagent.DefaultMaxTokens
	}

	sess.SetEventCallback(func(event libagent.AgentEvent) {
		r.handleEvent(event)
	})

	runErr := sess.Agent().Run(context.Background(), agent.SessionAgentCall{
		SessionID:       sess.ID(),
		Prompt:          text,
		Attachments:     attachments,
		MaxOutputTokens: maxTokens,
	})
	if capturePath := strings.TrimSpace(os.Getenv(assistantCaptureEnv)); capturePath != "" {
		_ = os.WriteFile(capturePath, []byte(r.FinalText()), 0o644)
	}
	// Always sync the binding even when the run is interrupted (e.g. Ctrl+C).
	if !opts.Ephemeral {
		_ = sess.EnsurePersisted()
	}
	return runErr
}

// stderrWriter is a helper to write status messages to stderr.
var stderrWriter io.Writer = os.Stderr

var runRalph = ralph.Run
