// Package oneshot implements the first-class non-interactive CLI mode for Raijin.
// It streams events to stderr with styled status lines and writes the final
// assistant response to stdout. Conversational commands require an explicit
// bound REPL or shell context.
package oneshot

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"

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
	"github.com/francescoalemanno/raijin-mono/internal/shellinit"
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

var ralphEphemeralRunMu sync.Mutex

func init() {
	ralph.SetEphemeralPromptRunner(runRalphEphemeralPrompt)
	ralph.SetPlanningQuestionAsker(runPlanningQuestionPrompt)
}

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

	case cmd.name == "max-images":
		return handleMaxImages(opts, strings.TrimSpace(cmd.args))

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
	ctx := context.Background()
	target := strings.TrimSpace(rawArgs)

	resolvedPair, resolvedExplicitly, err := resolvePlanTarget(ctx, target)
	if err != nil {
		return err
	}
	if resolvedExplicitly {
		initialAction := planScopedActionRun
		if _, statErr := os.Stat(resolvedPair.SpecPath); statErr != nil {
			initialAction = planScopedActionRevise
		}
		return handleScopedPlanFlow(ctx, resolvedPair, initialAction)
	}
	if target != "" {
		return errors.New("one-off /plan prompts are no longer supported; use /plan and choose create or revise, or pass a spec slug/path like /plan custom.md")
	}
	return handlePlanRootFlow(ctx)
}

type planRootAction string

const (
	planRootActionContinue planRootAction = "continue"
	planRootActionCreate   planRootAction = "create"
	planRootActionEdit     planRootAction = "edit"
	planRootActionReview   planRootAction = "review"
	planRootActionRevise   planRootAction = "revise"
	planRootActionRun      planRootAction = "run"
)

type planScopedAction string

const (
	planScopedActionRun     planScopedAction = "run"
	planScopedActionEdit    planScopedAction = "edit"
	planScopedActionReview  planScopedAction = "review"
	planScopedActionRevise  planScopedAction = "revise"
	planScopedActionScratch planScopedAction = "scratch"
	planScopedActionClose   planScopedAction = "close"
)

type planSpecPickerPurpose string

const (
	planSpecPickerPurposeContinue planSpecPickerPurpose = "continue"
	planSpecPickerPurposeEdit     planSpecPickerPurpose = "edit"
	planSpecPickerPurposeReview   planSpecPickerPurpose = "review"
	planSpecPickerPurposeRevise   planSpecPickerPurpose = "revise"
	planSpecPickerPurposeRun      planSpecPickerPurpose = "run"
)

type planSpecListEntry struct {
	Pair    ralph.SpecPair
	Status  ralph.PlanningStatus
	SortKey string
	Label   string
	Preview string
}

var (
	runPlanRootPicker         = defaultRunPlanRootPicker
	runPlanScopedActionPicker = defaultRunPlanScopedActionPicker
	runPlanSpecPicker         = defaultRunPlanSpecPicker
	editPlanSpec              = defaultEditPlanSpec
)

func handlePlanRootFlow(ctx context.Context) error {
	pairs, err := ralph.ListSpecPairs(ctx, "")
	if err != nil {
		return err
	}
	action, ok, err := runPlanRootPicker(len(pairs) > 0)
	if err != nil {
		return err
	}
	if !ok {
		return nil
	}
	return executePlanRootAction(ctx, action, "")
}

func executePlanRootAction(ctx context.Context, action planRootAction, request string) error {
	pairs, err := ralph.ListSpecPairs(ctx, "")
	if err != nil {
		return err
	}

	switch action {
	case planRootActionCreate:
		return createNewPlanFromRequest(ctx, request)
	case planRootActionContinue:
		pair, ok, err := runPlanSpecPicker(ctx, pairs, planSpecPickerPurposeContinue)
		if err != nil {
			return err
		}
		if !ok {
			return nil
		}
		return handleScopedPlanFlow(ctx, pair, planScopedActionRun)
	case planRootActionEdit:
		pair, ok, err := runPlanSpecPicker(ctx, pairs, planSpecPickerPurposeEdit)
		if err != nil {
			return err
		}
		if !ok {
			return nil
		}
		if err := editPlanSpec(pair.SpecPath); err != nil {
			return err
		}
		if !canUseEmbeddedFZF() {
			return nil
		}
		return handleScopedPlanFlow(ctx, pair, planScopedActionRun)
	case planRootActionReview:
		pair, ok, err := runPlanSpecPicker(ctx, pairs, planSpecPickerPurposeReview)
		if err != nil {
			return err
		}
		if !ok {
			return nil
		}
		return renderCurrentPlan(pair.SpecPath)
	case planRootActionRevise:
		pair, ok, err := runPlanSpecPicker(ctx, pairs, planSpecPickerPurposeRevise)
		if err != nil {
			return err
		}
		if !ok {
			return nil
		}
		revisionRequest := request
		if strings.TrimSpace(revisionRequest) == "" {
			revisionRequest, err = resolvePlanningRequest("")
			if err != nil || revisionRequest == "" {
				return err
			}
		}
		if err := runPlanningRequest(revisionRequest, pair.SpecPath, false); err != nil {
			return err
		}
		if !canUseEmbeddedFZF() {
			return nil
		}
		return handleScopedPlanFlow(ctx, pair, planScopedActionRun)
	case planRootActionRun:
		pair, ok, err := runPlanSpecPicker(ctx, pairs, planSpecPickerPurposeRun)
		if err != nil {
			return err
		}
		if !ok {
			return nil
		}
		return runExistingPlan(pair.SpecPath)
	default:
		return nil
	}
}

func createNewPlanFromRequest(ctx context.Context, request string) error {
	var err error
	request = strings.TrimSpace(request)
	if request == "" {
		request, err = resolvePlanningRequest("")
		if err != nil || request == "" {
			return err
		}
	}
	pair, err := ralph.AllocateNamedSpecPair(ctx, "")
	if err != nil {
		return err
	}
	if err := runPlanningRequest(request, pair.SpecPath, true); err != nil {
		return err
	}
	if !canUseEmbeddedFZF() {
		return nil
	}
	return handleScopedPlanFlow(ctx, pair, planScopedActionRun)
}

func handleScopedPlanFlow(ctx context.Context, pair ralph.SpecPair, initialAction planScopedAction) error {
	status, err := ralph.InspectPlanningState(ctx, "", pair.SpecPath)
	if err != nil {
		return err
	}
	action, ok, err := runPlanScopedActionPicker(pair, status, initialAction)
	if err != nil {
		return err
	}
	if !ok {
		return nil
	}

	switch action {
	case planScopedActionRun:
		return runExistingPlan(pair.SpecPath)
	case planScopedActionEdit:
		if err := editPlanSpec(pair.SpecPath); err != nil {
			return err
		}
		if !canUseEmbeddedFZF() {
			return nil
		}
		return handleScopedPlanFlow(ctx, pair, planScopedActionRun)
	case planScopedActionReview:
		return renderCurrentPlan(pair.SpecPath)
	case planScopedActionRevise:
		request, err := resolvePlanningRequest("")
		if err != nil || request == "" {
			return err
		}
		if err := runPlanningRequest(request, pair.SpecPath, false); err != nil {
			return err
		}
		if !canUseEmbeddedFZF() {
			return nil
		}
		return handleScopedPlanFlow(ctx, pair, planScopedActionRun)
	case planScopedActionScratch:
		request, err := resolvePlanningRequest("")
		if err != nil || request == "" {
			return err
		}
		if err := runPlanningRequest(request, pair.SpecPath, true); err != nil {
			return err
		}
		if !canUseEmbeddedFZF() {
			return nil
		}
		return handleScopedPlanFlow(ctx, pair, planScopedActionRun)
	default:
		return nil
	}
}

func resolvePlanTarget(ctx context.Context, request string) (ralph.SpecPair, bool, error) {
	pair, found, err := ralph.ResolveSpecSelection(ctx, "", request)
	if err != nil {
		return ralph.SpecPair{}, false, err
	}
	if found {
		return pair, true, nil
	}
	return ralph.SpecPair{}, false, nil
}

func defaultRunPlanRootPicker(hasSpecs bool) (planRootAction, bool, error) {
	if !canUseEmbeddedFZFForTest() {
		return "", false, errors.New("interactive Ralph dashboard requires a TTY")
	}

	items := []fzfPickerItem{
		{
			key:     string(planRootActionCreate),
			label:   "✦ Create new spec",
			preview: "Create a brand new Ralph spec from a planning request.\n\nThis allocates a new spec file, runs planning mode, and then returns to the scoped Ralph view for that new spec.",
		},
	}
	if hasSpecs {
		items = append([]fzfPickerItem{
			{
				key:     string(planRootActionContinue),
				label:   "↻ Continue spec",
				preview: "Select an existing spec, inspect its structured spec/progress preview, and then choose the next action from the scoped Ralph menu.\n\nThis is the primary entrypoint for ongoing multi-plan work.",
			},
			{
				key:     string(planRootActionEdit),
				label:   "✎ Edit spec",
				preview: "Select an existing spec and open it directly in your editor.\n\nThis is the manual alternative to AI-guided revise, and returns to a scoped Ralph menu for the edited spec.",
			},
			{
				key:     string(planRootActionReview),
				label:   "◱ Review spec",
				preview: "Select an existing spec and render its current spec and progress files without running Ralph.",
			},
			{
				key:     string(planRootActionRevise),
				label:   "⟳ Revise spec",
				preview: "Select an existing spec, provide a planning request, and revise that spec in place.",
			},
			{
				key:     string(planRootActionRun),
				label:   "▶ Run spec",
				preview: "Select an existing spec and immediately run Ralph builder mode for it.",
			},
		}, items...)
	}
	chosen, ok, err := pickPlanFZFKey(items, string(planRootActionContinue), "Ralph Dashboard")
	return planRootAction(chosen), ok, err
}

func defaultRunPlanScopedActionPicker(pair ralph.SpecPair, status ralph.PlanningStatus, initialAction planScopedAction) (planScopedAction, bool, error) {
	if !canUseEmbeddedFZFForTest() {
		return "", false, errors.New("interactive Ralph action menu requires a TTY")
	}

	preview := buildPlanSpecPreview(pair, status, "")
	items := []fzfPickerItem{
		{
			key:     string(planScopedActionRun),
			label:   "▶ Run spec",
			preview: "Run Ralph builder mode for this spec.\n\n" + preview,
		},
		{
			key:     string(planScopedActionEdit),
			label:   "✎ Edit spec",
			preview: "Open this spec directly in your editor for manual changes.\n\n" + preview,
		},
		{
			key:     string(planScopedActionReview),
			label:   "◱ Review spec",
			preview: "Render the current spec and progress without modifying anything.\n\n" + preview,
		},
		{
			key:     string(planScopedActionRevise),
			label:   "⟳ Revise spec",
			preview: "Open a planning iteration and revise this spec in place.\n\n" + preview,
		},
		{
			key:     string(planScopedActionScratch),
			label:   "↺ Replan from scratch",
			preview: "Reset this spec/progress pair and rebuild the spec from a new planning request.\n\n" + preview,
		},
		{
			key:     string(planScopedActionClose),
			label:   "✕ Close",
			preview: "Exit the scoped Ralph menu without taking action.\n\n" + preview,
		},
	}
	chosen, ok, err := pickPlanFZFKey(items, string(initialAction), buildPlanScopedMenuHeader(pair))
	return planScopedAction(chosen), ok, err
}

func defaultRunPlanSpecPicker(ctx context.Context, pairs []ralph.SpecPair, purpose planSpecPickerPurpose) (ralph.SpecPair, bool, error) {
	if !canUseEmbeddedFZFForTest() {
		return ralph.SpecPair{}, false, errors.New("interactive Ralph spec selection requires a TTY")
	}

	items, keyToPair, err := buildPlanSpecPickerItems(ctx, pairs)
	if err != nil {
		return ralph.SpecPair{}, false, err
	}
	if len(items) == 0 {
		return ralph.SpecPair{}, false, nil
	}

	chosenKey, action, err := pickWithEmbeddedFZFConfig(items, "", false, true, "", "default", shellinit.RunFZFOptions{
		Header:        buildPlanSpecPickerHeader(purpose),
		UseFullscreen: true,
	})
	if err != nil {
		return ralph.SpecPair{}, false, err
	}
	if action != fzfPickerActionSelect {
		return ralph.SpecPair{}, false, nil
	}
	pair, ok := keyToPair[chosenKey]
	if !ok {
		return ralph.SpecPair{}, false, fmt.Errorf("unknown Ralph spec selection: %s", chosenKey)
	}
	_ = purpose
	return pair, true, nil
}

func pickPlanFZFKey(items []fzfPickerItem, initialKey, header string) (string, bool, error) {
	chosenKey, action, err := pickWithEmbeddedFZFConfig(items, "", false, true, initialKey, "default", shellinit.RunFZFOptions{
		Header:        header,
		UseFullscreen: true,
	})
	if err != nil {
		return "", false, err
	}
	if action != fzfPickerActionSelect {
		return "", false, nil
	}
	return chosenKey, true, nil
}

func buildPlanScopedMenuHeader(pair ralph.SpecPair) string {
	name := pair.Slug
	if strings.TrimSpace(name) == "" {
		name = filepath.Base(pair.SpecPath)
	}
	return "Ralph · " + name
}

func buildPlanSpecPickerHeader(purpose planSpecPickerPurpose) string {
	switch purpose {
	case planSpecPickerPurposeContinue:
		return "Continue a Ralph spec"
	case planSpecPickerPurposeEdit:
		return "Edit a Ralph spec"
	case planSpecPickerPurposeReview:
		return "Review a Ralph spec"
	case planSpecPickerPurposeRevise:
		return "Revise a Ralph spec"
	case planSpecPickerPurposeRun:
		return "Run a Ralph spec"
	default:
		return "Select a Ralph spec"
	}
}

func buildPlanSpecPickerItems(ctx context.Context, pairs []ralph.SpecPair) ([]fzfPickerItem, map[string]ralph.SpecPair, error) {
	entries := make([]planSpecListEntry, 0, len(pairs))
	for _, pair := range pairs {
		status, err := ralph.InspectPlanningState(ctx, "", pair.SpecPath)
		if err != nil {
			return nil, nil, err
		}
		entries = append(entries, planSpecListEntry{
			Pair:    pair,
			Status:  status,
			SortKey: buildPlanSpecSortKey(pair),
			Label:   buildPlanSpecLabel(pair, status),
			Preview: buildPlanSpecPreview(pair, status, readOptionalPlanFile(pair.ProgressPath)),
		})
	}

	sort.Slice(entries, func(i, j int) bool {
		iCompleted := entries[i].Status.State == ralph.PlanningStateCompleted
		jCompleted := entries[j].Status.State == ralph.PlanningStateCompleted
		if iCompleted != jCompleted {
			return !iCompleted
		}
		return entries[i].SortKey < entries[j].SortKey
	})

	items := make([]fzfPickerItem, 0, len(entries))
	keyToPair := make(map[string]ralph.SpecPair, len(entries))
	for _, entry := range entries {
		key := entry.Pair.SpecPath
		items = append(items, fzfPickerItem{
			key:     key,
			label:   entry.Label,
			preview: entry.Preview,
		})
		keyToPair[key] = entry.Pair
	}
	return items, keyToPair, nil
}

func buildPlanSpecLabel(pair ralph.SpecPair, status ralph.PlanningStatus) string {
	stateIcon := "◉"
	stateLabel := "active"
	if status.State == ralph.PlanningStateCompleted {
		stateIcon = "✓"
		stateLabel = "completed"
	}
	name := pair.Slug
	if strings.TrimSpace(name) == "" {
		name = filepath.Base(pair.SpecPath)
	}
	return fmt.Sprintf("%s %s  ·  %s  ·  %s", stateIcon, name, stateLabel, relativePlanPath(status.RepoRoot, pair.SpecPath))
}

func buildPlanSpecPreview(pair ralph.SpecPair, status ralph.PlanningStatus, progress string) string {
	if progress == "" {
		progress = readOptionalPlanFile(pair.ProgressPath)
	}
	repoRoot := status.RepoRoot
	if strings.TrimSpace(repoRoot) == "" {
		repoRoot, _ = os.Getwd()
	}
	stateLabel := "Active"
	stateIcon := "◉"
	if status.State == ralph.PlanningStateCompleted {
		stateLabel = "Completed"
		stateIcon = "✓"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%s Ralph snapshot\n\n", stateIcon)
	fmt.Fprintf(&b, "State     %s\n", stateLabel)
	fmt.Fprintf(&b, "Spec      %s\n", relativePlanPath(repoRoot, pair.SpecPath))
	fmt.Fprintf(&b, "Progress  %s\n\n", relativePlanPath(repoRoot, pair.ProgressPath))
	b.WriteString("── Spec\n")
	spec := strings.TrimRight(readOptionalPlanFile(pair.SpecPath), "\n")
	if spec == "" {
		b.WriteString("(empty)\n")
	} else {
		b.WriteString(spec)
		b.WriteString("\n")
	}
	b.WriteString("\n── Progress\n")
	progress = strings.TrimRight(progress, "\n")
	if progress == "" {
		b.WriteString("(no progress yet)\n")
	} else {
		b.WriteString(progress)
		b.WriteString("\n")
	}
	return b.String()
}

func buildPlanSpecSortKey(pair ralph.SpecPair) string {
	name := pair.Slug
	if strings.TrimSpace(name) == "" {
		name = filepath.Base(pair.SpecPath)
	}
	return strings.ToLower(name + " " + pair.SpecPath)
}

func readOptionalPlanFile(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return string(data)
}

func defaultEditPlanSpec(path string) error {
	editor, err := resolveEditorCommand(os.Getenv, exec.LookPath)
	if err != nil {
		return err
	}

	args := append(append([]string{}, editor.args...), path)
	cmd := exec.Command(editor.path, args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = os.Environ()
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("run editor command: %w", err)
	}
	return nil
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

func runPlanningRequest(request, specPath string, reset bool) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	err := runRalph(ctx, ralph.Options{
		PlanningRequest: request,
		Mode:            ralph.ModePlan,
		ResetPlan:       reset,
		SpecPath:        specPath,
	})
	if errors.Is(err, context.Canceled) {
		fmt.Fprintln(os.Stderr, renderStatusWarning("●")+" Ralph interrupted")
		return nil
	}
	return err
}

func runExistingPlan(specPath string) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	err := runRalph(ctx, ralph.Options{
		Mode:     ralph.ModeAuto,
		SpecPath: specPath,
	})
	if errors.Is(err, context.Canceled) {
		fmt.Fprintln(os.Stderr, renderStatusWarning("●")+" Ralph interrupted")
		return nil
	}
	return err
}

func renderCurrentPlan(specPath string) error {
	snapshot, err := ralph.ReadSnapshot(context.Background(), "", specPath)
	if err != nil {
		return err
	}

	var doc strings.Builder
	doc.WriteString("# Ralph Spec\n\n")
	doc.WriteString("Path: `")
	doc.WriteString(relativePlanPath(snapshot.RepoRoot, snapshot.SpecPath))
	doc.WriteString("`\n\n")
	doc.WriteString(strings.TrimSpace(snapshot.Spec))
	doc.WriteString("\n\n# Ralph Progress\n\n")
	doc.WriteString("Path: `")
	doc.WriteString(relativePlanPath(snapshot.RepoRoot, snapshot.ProgressPath))
	doc.WriteString("`\n\n")
	if strings.TrimSpace(snapshot.Progress) == "" {
		doc.WriteString("_No progress yet._\n")
	} else {
		doc.WriteString("```text\n")
		doc.WriteString(strings.TrimRight(snapshot.Progress, "\n"))
		doc.WriteString("\n```\n")
	}

	renderMarkdownDocument(os.Stdout, doc.String())
	return nil
}

func relativePlanPath(repoRoot, path string) string {
	if rel, err := filepath.Rel(repoRoot, path); err == nil {
		return rel
	}
	return path
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
	fmt.Printf("Max images: %s\n", statusMaxImagesLabel(opts))
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

func statusMaxImagesLabel(opts Options) string {
	cfg := opts.ModelCfg.Normalize()
	if strings.TrimSpace(cfg.Provider) == "" && strings.TrimSpace(cfg.Model) == "" {
		cfg = opts.RuntimeModel.ModelCfg.Normalize()
	}
	if cfg.MaxImages == nil {
		return fmt.Sprintf("%d (default)", cfg.EffectiveMaxImages())
	}
	return strconv.Itoa(cfg.EffectiveMaxImages())
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

	finalText, runErr := runSessionAgentCallWithRenderer(context.Background(), opts, sess, agent.SessionAgentCall{
		SessionID:       sess.ID(),
		Prompt:          text,
		Attachments:     attachments,
		MaxOutputTokens: maxTokens,
	}, os.Stdout, os.Stderr)
	if capturePath := strings.TrimSpace(os.Getenv(assistantCaptureEnv)); capturePath != "" {
		_ = os.WriteFile(capturePath, []byte(finalText), 0o644)
	}
	// Always sync the binding even when the run is interrupted (e.g. Ctrl+C).
	if !opts.Ephemeral {
		_ = sess.EnsurePersisted()
	}
	return runErr
}

func runSessionAgentCallWithRenderer(ctx context.Context, opts Options, sess *session.Session, call agent.SessionAgentCall, stdout, stderr io.Writer) (string, error) {
	msgs, err := sess.ListMessages(ctx)
	if err != nil {
		return "", err
	}

	isTTY := term.IsTerminal(int(os.Stderr.Fd()))
	r := newRendererWithOptions(stderr, stdout, sess.Tools(), isTTY, rendererOptions{
		persistentSpinner: true,
		modelLabel:        statusModelLabel(opts),
		contextWindow:     opts.RuntimeModel.EffectiveContextWindow(),
		initialMessages:   msgs,
	})
	if r.contextWindow <= 0 {
		r.contextWindow = opts.ModelCfg.ContextWindow
	}
	popRenderer := pushActiveRenderer(r)
	defer popRenderer()
	r.startPersistentSpinner()
	defer r.stopPersistentSpinner()

	sess.SetEventCallback(func(event libagent.AgentEvent) {
		r.handleEvent(event)
	})

	if call.SessionID == "" {
		call.SessionID = sess.ID()
	}
	runErr := sess.Agent().Run(ctx, call)
	return r.FinalText(), runErr
}

func runRalphEphemeralPrompt(ctx context.Context, repoRoot string, opts ralph.EphemeralPromptOptions, stdout, stderr io.Writer) (string, string, error) {
	runtimeModel, err := loadDefaultRuntimeModelFromStore(ctx)
	if err != nil {
		return "", "", err
	}

	ralphEphemeralRunMu.Lock()
	defer ralphEphemeralRunMu.Unlock()

	origWD, err := os.Getwd()
	if err != nil {
		return "", "", err
	}
	if err := os.Chdir(repoRoot); err != nil {
		return "", "", err
	}
	defer func() { _ = os.Chdir(origWD) }()

	sess, err := session.NewEphemeral(runtimeModel)
	if err != nil {
		return "", "", err
	}
	if err := sess.StartEphemeral(ctx); err != nil {
		return "", "", err
	}

	var stdoutBuf, stderrBuf bytes.Buffer
	outWriter := io.Writer(&stdoutBuf)
	errWriter := io.Writer(&stderrBuf)
	if stdout != nil {
		outWriter = io.MultiWriter(stdout, &stdoutBuf)
	}
	if stderr != nil {
		errWriter = io.MultiWriter(stderr, &stderrBuf)
	}

	_, runErr := runSessionAgentCallWithRenderer(ctx, Options{
		Ephemeral:    true,
		RuntimeModel: runtimeModel,
		ModelCfg:     runtimeModel.ModelCfg,
	}, sess, agent.SessionAgentCall{
		SessionID:      sess.ID(),
		Prompt:         opts.Prompt,
		OnCompleteHook: opts.OnCompleteHook,
		ExtraTools:     append([]libagent.Tool(nil), opts.ExtraTools...),
	}, outWriter, errWriter)
	return stdoutBuf.String(), stderrBuf.String(), runErr
}

func loadDefaultRuntimeModelFromStore(ctx context.Context) (libagent.RuntimeModel, error) {
	store, err := modelconfig.LoadModelStore()
	if err != nil {
		return libagent.RuntimeModel{}, err
	}
	if store == nil {
		return libagent.RuntimeModel{}, errors.New("no model store available")
	}
	cfg, ok := store.GetDefault()
	if !ok {
		return libagent.RuntimeModel{}, errors.New("no default model configured")
	}
	cfg = cfg.Normalize()
	apiKey := cfg.APIKey
	if after, ok := strings.CutPrefix(apiKey, "$"); ok {
		apiKey = os.Getenv(after)
	}
	cat := libagent.DefaultCatalog()
	model, err := cat.NewModel(ctx, cfg.Provider, cfg.Model, apiKey)
	if err != nil {
		return libagent.RuntimeModel{}, fmt.Errorf("build default runtime model %s/%s: %w", cfg.Provider, cfg.Model, err)
	}
	info, _, _ := cat.FindModel(cfg.Provider, cfg.Model)
	providerType, catalogOpts := cat.FindModelOptions(cfg.Provider, cfg.Model)
	return libagent.RuntimeModel{
		Model:                  model,
		ModelInfo:              info,
		ModelCfg:               cfg,
		ProviderType:           providerType,
		CatalogProviderOptions: catalogOpts,
	}, nil
}

// stderrWriter is a helper to write status messages to stderr.
var stderrWriter io.Writer = os.Stderr

var runRalph = ralph.Run
