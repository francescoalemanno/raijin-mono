package ralph

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/francescoalemanno/raijin-mono/internal/shell"
	libagent "github.com/francescoalemanno/raijin-mono/libagent"
)

type Mode string

const (
	ModeAuto Mode = "auto"
	ModePlan Mode = "plan"

	defaultMaxIterations = 25
	promiseDone          = "PROMISE: DONE"
	promiseContinue      = "PROMISE: CONTINUE"
)

var ErrMaxIterationsReached = errors.New("ralph: maximum iterations reached")

type Options struct {
	Goal            string
	PlanningRequest string
	Mode            Mode
	RepoRoot        string
	MaxIterations   int
	ResetPlan       bool
	SpecPath        string
}

type Snapshot struct {
	RepoRoot     string
	SpecPath     string
	ProgressPath string
	Spec         string
	Progress     string
}

type PlanningState string

const (
	PlanningStateEmpty     PlanningState = "empty"
	PlanningStatePlanned   PlanningState = "planned"
	PlanningStateCompleted PlanningState = "completed"
)

type PlanningStatus struct {
	RepoRoot     string
	SpecPath     string
	ProgressPath string
	State        PlanningState
}

type SpecPair struct {
	SpecPath     string
	ProgressPath string
	Slug         string
}

type basePaths struct {
	root    string
	baseDir string
}

type promptResult struct {
	Stdout string
	Stderr string
}

type EphemeralPromptOptions struct {
	Prompt         string
	OnCompleteHook libagent.OnCompleteHook
	ExtraTools     []libagent.Tool
}

type PlanningQuestionOption struct {
	Label       string `json:"label" description:"Short answer option label returned to the planner if selected"`
	Description string `json:"description,omitempty" description:"Brief explanation shown to the user for this answer option"`
}

type PlanningQuestionPrompt struct {
	Question string                   `json:"question" description:"Clarifying question the planner needs answered"`
	Options  []PlanningQuestionOption `json:"options" description:"One to three suggested answer options"`
}

var (
	runEphemeralPrompt  func(ctx context.Context, repoRoot string, opts EphemeralPromptOptions, stdout, stderr io.Writer) (promptResult, error)
	askPlanningQuestion func(context.Context, PlanningQuestionPrompt) (string, error)
	resolveRepoRoot     = defaultResolveRepoRoot
	resolveSpecTarget   = ResolveSpecSelection
	generateSpecSlug    = defaultGenerateSpecSlug
)

func SetEphemeralPromptRunner(fn func(ctx context.Context, repoRoot string, opts EphemeralPromptOptions, stdout, stderr io.Writer) (stdoutText, stderrText string, err error)) {
	if fn == nil {
		runEphemeralPrompt = nil
		return
	}
	runEphemeralPrompt = func(ctx context.Context, repoRoot string, opts EphemeralPromptOptions, stdout, stderr io.Writer) (promptResult, error) {
		stdoutText, stderrText, err := fn(ctx, repoRoot, opts, stdout, stderr)
		return promptResult{Stdout: stdoutText, Stderr: stderrText}, err
	}
}

func SetPlanningQuestionAsker(fn func(context.Context, PlanningQuestionPrompt) (string, error)) {
	askPlanningQuestion = fn
}

func Run(ctx context.Context, opts Options) error {
	mode := opts.Mode
	if mode == "" {
		mode = ModeAuto
	}
	if mode != ModeAuto && mode != ModePlan {
		return fmt.Errorf("ralph: unsupported mode %q", mode)
	}

	repoRoot, err := resolveRepoRoot(ctx, strings.TrimSpace(opts.RepoRoot))
	if err != nil {
		return err
	}
	paths := newBasePaths(repoRoot)
	if err := ensureDirs(paths); err != nil {
		return err
	}

	pair, planningRequest, maxIterations, err := prepareRun(repoRoot, paths, Options{
		Goal:            strings.TrimSpace(opts.Goal),
		PlanningRequest: strings.TrimSpace(opts.PlanningRequest),
		Mode:            mode,
		MaxIterations:   opts.MaxIterations,
		ResetPlan:       opts.ResetPlan,
		SpecPath:        strings.TrimSpace(opts.SpecPath),
	})
	if err != nil {
		return err
	}

	if opts.ResetPlan {
		if err := resetPair(pair); err != nil {
			return err
		}
	}

	switch mode {
	case ModePlan:
		return runPlanning(ctx, repoRoot, pair, planningRequest)
	case ModeAuto:
		return runAutomatic(ctx, repoRoot, pair, maxIterations)
	default:
		return fmt.Errorf("ralph: unsupported mode %q", mode)
	}
}

func ReadSnapshot(ctx context.Context, repoRoot, specTarget string) (Snapshot, error) {
	resolvedRoot, err := resolveRepoRoot(ctx, strings.TrimSpace(repoRoot))
	if err != nil {
		return Snapshot{}, err
	}
	pair, err := resolveSnapshotPair(ctx, resolvedRoot, strings.TrimSpace(specTarget))
	if err != nil {
		return Snapshot{}, err
	}

	spec, err := os.ReadFile(pair.SpecPath)
	if err != nil {
		if os.IsNotExist(err) {
			return Snapshot{}, fmt.Errorf("ralph: %s does not exist; run /plan <request> first", relPath(resolvedRoot, pair.SpecPath))
		}
		return Snapshot{}, err
	}
	if strings.TrimSpace(string(spec)) == "" {
		return Snapshot{}, fmt.Errorf("ralph: %s is empty", relPath(resolvedRoot, pair.SpecPath))
	}

	progress := readOptionalFile(pair.ProgressPath)
	return Snapshot{
		RepoRoot:     resolvedRoot,
		SpecPath:     pair.SpecPath,
		ProgressPath: pair.ProgressPath,
		Spec:         string(spec),
		Progress:     progress,
	}, nil
}

func HasPlanningState(ctx context.Context, repoRoot, specTarget string) (bool, error) {
	status, err := InspectPlanningState(ctx, repoRoot, specTarget)
	if err != nil {
		return false, err
	}
	return status.State != PlanningStateEmpty, nil
}

func InspectPlanningState(ctx context.Context, repoRoot, specTarget string) (PlanningStatus, error) {
	resolvedRoot, err := resolveRepoRoot(ctx, strings.TrimSpace(repoRoot))
	if err != nil {
		return PlanningStatus{}, err
	}

	pair, err := resolveSnapshotPair(ctx, resolvedRoot, strings.TrimSpace(specTarget))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) || strings.Contains(err.Error(), "no specs found in") {
			return PlanningStatus{RepoRoot: resolvedRoot, State: PlanningStateEmpty}, nil
		}
		return PlanningStatus{}, err
	}
	spec := strings.TrimSpace(readOptionalFile(pair.SpecPath))
	if spec == "" {
		return PlanningStatus{RepoRoot: resolvedRoot, State: PlanningStateEmpty}, nil
	}

	promise, err := readProgressPromiseForState(pair.ProgressPath)
	if err != nil {
		if os.IsNotExist(err) {
			return PlanningStatus{
				RepoRoot:     resolvedRoot,
				SpecPath:     pair.SpecPath,
				ProgressPath: pair.ProgressPath,
				State:        PlanningStatePlanned,
			}, nil
		}
		return PlanningStatus{}, err
	}
	state := PlanningStatePlanned
	if promise == promiseDone {
		state = PlanningStateCompleted
	}
	return PlanningStatus{
		RepoRoot:     resolvedRoot,
		SpecPath:     pair.SpecPath,
		ProgressPath: pair.ProgressPath,
		State:        state,
	}, nil
}

func ListSpecPairs(ctx context.Context, repoRoot string) ([]SpecPair, error) {
	resolvedRoot, err := resolveRepoRoot(ctx, strings.TrimSpace(repoRoot))
	if err != nil {
		return nil, err
	}
	baseDir := newBasePaths(resolvedRoot).baseDir
	entries, err := os.ReadDir(baseDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	pairs := make([]SpecPair, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		slug, ok := specSlugFromName(name)
		if !ok {
			continue
		}
		specPath := filepath.Join(baseDir, name)
		pairs = append(pairs, SpecPair{
			SpecPath:     specPath,
			ProgressPath: deriveProgressPath(specPath),
			Slug:         slug,
		})
	}
	sort.Slice(pairs, func(i, j int) bool {
		return pairs[i].SpecPath < pairs[j].SpecPath
	})
	return pairs, nil
}

func AllocateNamedSpecPair(ctx context.Context, repoRoot string) (SpecPair, error) {
	resolvedRoot, err := resolveRepoRoot(ctx, strings.TrimSpace(repoRoot))
	if err != nil {
		return SpecPair{}, err
	}
	paths := newBasePaths(resolvedRoot)
	if err := ensureDirs(paths); err != nil {
		return SpecPair{}, err
	}
	return newNamedSpecPair(paths)
}

func ResolveSpecSelection(ctx context.Context, repoRoot, target string) (SpecPair, bool, error) {
	resolvedRoot, err := resolveRepoRoot(ctx, strings.TrimSpace(repoRoot))
	if err != nil {
		return SpecPair{}, false, err
	}

	target = strings.TrimSpace(target)
	if target == "" {
		return SpecPair{}, false, nil
	}

	if looksLikePath(target) {
		specPath, err := filepath.Abs(target)
		if err != nil {
			return SpecPair{}, false, err
		}
		if fileExists(specPath) {
			return newSpecPair(specPath), true, nil
		}
		return SpecPair{}, false, nil
	}

	pairs, err := ListSpecPairs(ctx, resolvedRoot)
	if err != nil {
		return SpecPair{}, false, err
	}
	for _, pair := range pairs {
		if pair.Slug == target {
			return pair, true, nil
		}
	}
	return SpecPair{}, false, nil
}

func runAutomatic(ctx context.Context, repoRoot string, pair SpecPair, maxIterations int) error {
	if !fileExists(pair.SpecPath) {
		return fmt.Errorf("ralph: %s does not exist; run /plan <request> first", relPath(repoRoot, pair.SpecPath))
	}
	if strings.TrimSpace(readOptionalFile(pair.SpecPath)) == "" {
		return fmt.Errorf("ralph: %s is empty", relPath(repoRoot, pair.SpecPath))
	}
	if runEphemeralPrompt == nil {
		return errors.New("ralph: no ephemeral runner registered")
	}

	for iteration := 1; ; iteration++ {
		if err := ctx.Err(); err != nil {
			return nil
		}
		if iteration > maxIterations {
			return ErrMaxIterationsReached
		}
		if err := clearPromiseLines(pair.ProgressPath); err != nil {
			return err
		}

		onCompleteHook := progressPromiseHook(pair.ProgressPath)
		result, runErr := runEphemeralPrompt(ctx, repoRoot, EphemeralPromptOptions{
			Prompt:         buildImplementationPrompt(repoRoot, pair),
			OnCompleteHook: onCompleteHook,
		}, os.Stdout, os.Stderr)
		if runErr != nil {
			if noteErr := writeControllerNote(pair.ProgressPath, summarizePromptFailure("implementation iteration", result, runErr)); noteErr != nil {
				return noteErr
			}
			continue
		}

		promise, err := readProgressPromise(pair.ProgressPath)
		if err != nil {
			if noteErr := writeControllerNote(pair.ProgressPath, fmt.Sprintf("Controller note: Ralph could not validate %s after iteration %d: %v", relPath(repoRoot, pair.ProgressPath), iteration, err)); noteErr != nil {
				return noteErr
			}
			continue
		}
		if promise == promiseDone {
			fmt.Fprintln(os.Stderr, "Ralph completed successfully")
			return nil
		}
	}
}

func runPlanning(ctx context.Context, repoRoot string, pair SpecPair, planningRequest string) error {
	if strings.TrimSpace(planningRequest) == "" {
		return errors.New("ralph: goal is required; use /plan <goal> first")
	}
	if err := os.MkdirAll(filepath.Dir(pair.SpecPath), 0o755); err != nil {
		return err
	}
	if runEphemeralPrompt == nil {
		return errors.New("ralph: no ephemeral runner registered")
	}
	questionTool, err := newPlanningQuestionTool()
	if err != nil {
		return err
	}
	initialSpec := readOptionalFile(pair.SpecPath)

	result, runErr := runEphemeralPrompt(ctx, repoRoot, EphemeralPromptOptions{
		Prompt:         buildPlanningPrompt(repoRoot, pair, planningRequest),
		OnCompleteHook: planningSpecChangedHook(repoRoot, pair.SpecPath, initialSpec),
		ExtraTools:     []libagent.Tool{questionTool},
	}, os.Stdout, os.Stderr)
	if runErr != nil {
		if errors.Is(runErr, context.Canceled) {
			fmt.Fprintln(os.Stderr, "Ralph planning interrupted")
			return nil
		}
		return runErr
	}
	spec := strings.TrimSpace(readOptionalFile(pair.SpecPath))
	if spec == "" {
		return fmt.Errorf("ralph planning did not create %s", relPath(repoRoot, pair.SpecPath))
	}
	_ = result
	return nil
}

func prepareRun(repoRoot string, paths basePaths, opts Options) (SpecPair, string, int, error) {
	maxIterations := firstPositive(opts.MaxIterations, defaultMaxIterations)

	switch opts.Mode {
	case ModePlan:
		request := strings.TrimSpace(opts.PlanningRequest)
		if request == "" {
			request = strings.TrimSpace(opts.Goal)
		}
		pair, err := resolvePlanningPair(repoRoot, paths, strings.TrimSpace(opts.SpecPath))
		if err != nil {
			return SpecPair{}, "", 0, err
		}
		return pair, request, maxIterations, nil
	case ModeAuto:
		pair, err := resolveAutoPair(context.Background(), repoRoot, strings.TrimSpace(opts.SpecPath))
		if err != nil {
			return SpecPair{}, "", 0, err
		}
		return pair, "", maxIterations, nil
	default:
		return SpecPair{}, "", 0, fmt.Errorf("ralph: unsupported mode %q", opts.Mode)
	}
}

func resolvePlanningPair(repoRoot string, paths basePaths, specTarget string) (SpecPair, error) {
	if specTarget != "" {
		resolved, found, err := resolveSpecTarget(context.Background(), repoRoot, specTarget)
		if err != nil {
			return SpecPair{}, err
		}
		if found {
			return resolved, nil
		}
		specPath, err := filepath.Abs(specTarget)
		if err != nil {
			return SpecPair{}, err
		}
		return newSpecPair(specPath), nil
	}
	return newNamedSpecPair(paths)
}

func resolveAutoPair(ctx context.Context, repoRoot, specTarget string) (SpecPair, error) {
	if specTarget != "" {
		pair, found, err := resolveSpecTarget(ctx, repoRoot, specTarget)
		if err != nil {
			return SpecPair{}, err
		}
		if found {
			return pair, nil
		}
		return SpecPair{}, fmt.Errorf("ralph: spec not found: %s", specTarget)
	}

	pairs, err := ListSpecPairs(ctx, repoRoot)
	if err != nil {
		return SpecPair{}, err
	}
	switch len(pairs) {
	case 0:
		return SpecPair{}, fmt.Errorf("ralph: no specs found in %s; run /plan <request> first", relPath(repoRoot, newBasePaths(repoRoot).baseDir))
	case 1:
		return pairs[0], nil
	default:
		return SpecPair{}, errors.New("ralph: multiple specs exist; select one explicitly by path or slug")
	}
}

func resolveSnapshotPair(ctx context.Context, repoRoot, specTarget string) (SpecPair, error) {
	if strings.TrimSpace(specTarget) != "" {
		pair, found, err := resolveSpecTarget(ctx, repoRoot, specTarget)
		if err != nil {
			return SpecPair{}, err
		}
		if found {
			return pair, nil
		}
		return SpecPair{}, fmt.Errorf("ralph: spec not found: %s", specTarget)
	}
	return resolveAutoPair(ctx, repoRoot, "")
}

func buildPlanningPrompt(repoRoot string, pair SpecPair, planningRequest string) string {
	return fmt.Sprintf(strings.TrimSpace(`
You are running inside a Ralph planning iteration for this repository.

This is a fresh ephemeral run. The durable user/planner-owned specification lives on disk.

Read these files first if they exist:
- %s
- %s
- AGENTS.md
- specs/
- README.md
- existing implementation files relevant to the request

Your task is to create or revise %s in place. This file is the durable user/planner specification. It must stay builder-readable and progress-free.

Keep %s as a single Markdown document with these sections in order:

# Goal
<durable project goal>

# User Specification
<durable user requirements, constraints, and exclusions>

# Plan
<durable implementation plan or checklist>

Requirements:
1. Treat %s as planner-owned durable state. Do not use it for progress tracking, completion tracking, or builder handoff notes.
2. If %s already exists, treat it as canonical and revise it surgically against the new planning request instead of replacing it wholesale.
3. If %s does not exist yet, create it from the planning request.
4. Keep the result technical, concise, stable across builder iterations, and limited to durable facts that are already established.
5. If uncertainty would materially change the goal, scope boundaries, constraints, acceptance criteria, or sequencing, ask clarifying questions instead of guessing.
6. Use the question tool for those clarifications. Ask only 1-3 focused high-leverage questions before waiting for answers.
7. Free-form answers are always allowed. After answers arrive, continue planning in the same session.
8. You may write a partial draft of %s as you learn durable facts, but do not write interview transcript, open questions, temporary notes, or planner progress tracking into it.
9. Planning mode must never execute the plan, must not create or modify %s, and must not edit implementation files.
10. Do not run verification commands, builds, tests, or migrations in planning mode.
11. The builder Ralph will treat %s as read-only and will manage its own progress in %s.

New planning request from /plan:
%s
`),
		relPath(repoRoot, pair.SpecPath),
		relPath(repoRoot, pair.ProgressPath),
		relPath(repoRoot, pair.SpecPath),
		relPath(repoRoot, pair.SpecPath),
		relPath(repoRoot, pair.SpecPath),
		relPath(repoRoot, pair.SpecPath),
		relPath(repoRoot, pair.SpecPath),
		relPath(repoRoot, pair.SpecPath),
		relPath(repoRoot, pair.ProgressPath),
		relPath(repoRoot, pair.SpecPath),
		relPath(repoRoot, pair.ProgressPath),
		renderPromptBlock(planningRequest),
	)
}

type planningQuestionToolInput struct {
	Question string                   `json:"question" description:"Clarifying question the planner needs answered"`
	Options  []PlanningQuestionOption `json:"options" description:"One to three suggested answer options"`
}

func newPlanningQuestionTool() (libagent.Tool, error) {
	if askPlanningQuestion == nil {
		return nil, errors.New("ralph: no planning question asker registered")
	}
	return libagent.NewTypedTool("question", "Ask the user a structured clarifying question during Ralph planning. Supports one to three suggested answer options and always allows a free-form answer.", func(ctx context.Context, input planningQuestionToolInput, _ libagent.ToolCall) (libagent.ToolResponse, error) {
		question := strings.TrimSpace(input.Question)
		if question == "" {
			return libagent.NewTextErrorResponse("question is required"), nil
		}
		if len(input.Options) == 0 {
			return libagent.NewTextErrorResponse("at least one option is required"), nil
		}
		if len(input.Options) > 3 {
			return libagent.NewTextErrorResponse("at most three options are allowed"), nil
		}
		options := make([]PlanningQuestionOption, 0, len(input.Options))
		for _, option := range input.Options {
			label := strings.TrimSpace(option.Label)
			if label == "" {
				return libagent.NewTextErrorResponse("option labels must not be empty"), nil
			}
			options = append(options, PlanningQuestionOption{
				Label:       label,
				Description: strings.TrimSpace(option.Description),
			})
		}
		answer, err := askPlanningQuestion(ctx, PlanningQuestionPrompt{
			Question: question,
			Options:  options,
		})
		if err != nil {
			return libagent.ToolResponse{}, err
		}
		return libagent.NewTextResponse(strings.TrimSpace(answer)), nil
	}), nil
}

func planningSpecChangedHook(repoRoot, specPath, initialSpec string) libagent.OnCompleteHook {
	return func(context.Context, *libagent.AssistantMessage, []libagent.Message) (string, bool, error) {
		currentSpec := readOptionalFile(specPath)
		if currentSpec != initialSpec {
			return "", true, nil
		}
		return fmt.Sprintf(
			"Your task is to bidirectionally create or revise the durable Ralph specification with the user and save it in %s. The file still matches what existed at the start of this planning session. Continue the planning interview if needed, ask clarifying questions when in doubt, and persist the updated spec to %s before ending.",
			relPath(repoRoot, specPath),
			relPath(repoRoot, specPath),
		), false, nil
	}
}

func buildImplementationPrompt(repoRoot string, pair SpecPair) string {
	return fmt.Sprintf(strings.TrimSpace(`
You are running inside a Ralph builder iteration for this repository.

This is a fresh ephemeral run. The durable planner specification and your mutable progress file live on disk.

Read these files first if they exist:
- %s
- %s
- AGENTS.md
- relevant source files, tests, and specs

Rules:
1. Treat %s as read-only durable input. Do not modify it.
2. Use %s as your mutable current-view progress file. Update it surgically to track your task breakdown, current progress, controller notes that still matter, and next-iteration notes.
3. Preserve still-relevant progress content, remaining work, and controller notes in %s. Do not wipe or drastically shrink the file unless the old content is obsolete and you replace it with an equally complete current view.
4. If %s does not exist yet, create it before finishing.
5. Keep exactly one whole-line canonical promise in %s before the iteration ends:
   - %s
   - %s
6. You must choose exactly one of those two promise lines and persist it in %s before the iteration is allowed to end.
7. Write %s only if the entire current specification is complete, verified, and there is no important remaining work. Otherwise write %s.
8. Finishing your single chosen task is not enough for %s unless that task also completes the entire current specification.
9. Do not rely on %s for progress tracking or completion tracking.
10. Run relevant checks inside the repo before finishing.
11. Re-read the files you need before making decisions.
12. At the start of the iteration, choose exactly one single most high-leverage task from %s to work on.
13. Prefer the most important foundational item that unlocks or de-risks later work, while still keeping the chosen task concrete and finishable within one iteration.
14. Do that single chosen task, update %s to reflect the result, and then stop.
15. Do not try to push progress across the whole specification in one iteration.
`),
		relPath(repoRoot, pair.SpecPath),
		relPath(repoRoot, pair.ProgressPath),
		relPath(repoRoot, pair.SpecPath),
		relPath(repoRoot, pair.ProgressPath),
		relPath(repoRoot, pair.ProgressPath),
		relPath(repoRoot, pair.ProgressPath),
		relPath(repoRoot, pair.ProgressPath),
		promiseDone,
		promiseContinue,
		relPath(repoRoot, pair.ProgressPath),
		promiseDone,
		promiseContinue,
		promiseDone,
		relPath(repoRoot, pair.SpecPath),
		relPath(repoRoot, pair.ProgressPath),
		relPath(repoRoot, pair.ProgressPath),
	)
}

func progressPromiseHook(progressPath string) libagent.OnCompleteHook {
	return func(_ context.Context, _ *libagent.AssistantMessage, _ []libagent.Message) (string, bool, error) {
		promise, err := readProgressPromise(progressPath)
		if err == nil && (promise == promiseDone || promise == promiseContinue) {
			return "", true, nil
		}
		if err != nil && !os.IsNotExist(err) && !looksLikePromiseValidationError(err) {
			return "", false, err
		}
		return fmt.Sprintf(
			"Before ending this builder iteration, reopen %s and update it coherently with the builder rules. Preserve still-relevant progress, remaining work, and controller notes. Add exactly one whole-line promise: %s or %s. Write %s only if the entire current specification is complete, verified, and there is no important remaining work. Finishing only the current task is not enough for %s. Then respond again.",
			progressPath,
			promiseDone,
			promiseContinue,
			promiseDone,
			promiseDone,
		), false, nil
	}
}

func looksLikePromiseValidationError(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "promise line")
}

func defaultResolveRepoRoot(ctx context.Context, requested string) (string, error) {
	if strings.TrimSpace(requested) != "" {
		return filepath.Abs(strings.TrimSpace(requested))
	}

	var out bytes.Buffer
	err := shell.Run(ctx, shell.ExecSpec{
		Path: "git",
		Args: []string{"rev-parse", "--show-toplevel"},
		Env:  os.Environ(),
	}, &out, io.Discard)
	if err == nil {
		if root := strings.TrimSpace(out.String()); root != "" {
			return filepath.Abs(root)
		}
	}

	cwd, cwdErr := os.Getwd()
	if cwdErr != nil {
		return "", cwdErr
	}
	return filepath.Abs(cwd)
}

func newBasePaths(repoRoot string) basePaths {
	return basePaths{
		root:    repoRoot,
		baseDir: filepath.Join(repoRoot, ".raijin", "ralph"),
	}
}

func ensureDirs(paths basePaths) error {
	return os.MkdirAll(paths.baseDir, 0o755)
}

func newNamedSpecPair(paths basePaths) (SpecPair, error) {
	for i := 0; i < 64; i++ {
		slug := generateSpecSlug()
		specPath := filepath.Join(paths.baseDir, "spec-"+slug+".md")
		if fileExists(specPath) {
			continue
		}
		return SpecPair{
			SpecPath:     specPath,
			ProgressPath: deriveProgressPath(specPath),
			Slug:         slug,
		}, nil
	}
	return SpecPair{}, errors.New("ralph: could not allocate a unique spec name")
}

func newSpecPair(specPath string) SpecPair {
	specPath, _ = filepath.Abs(specPath)
	return SpecPair{
		SpecPath:     specPath,
		ProgressPath: deriveProgressPath(specPath),
		Slug:         slugFromSpecPath(specPath),
	}
}

func deriveProgressPath(specPath string) string {
	dir := filepath.Dir(specPath)
	base := filepath.Base(specPath)
	if slug, ok := specSlugFromName(base); ok {
		return filepath.Join(dir, "progress-"+slug+".txt")
	}
	ext := filepath.Ext(base)
	stem := strings.TrimSuffix(base, ext)
	return filepath.Join(dir, stem+".progress.txt")
}

func specSlugFromName(name string) (string, bool) {
	if !strings.HasPrefix(name, "spec-") || !strings.HasSuffix(name, ".md") {
		return "", false
	}
	slug := strings.TrimSuffix(strings.TrimPrefix(name, "spec-"), ".md")
	if strings.TrimSpace(slug) == "" {
		return "", false
	}
	return slug, true
}

func slugFromSpecPath(specPath string) string {
	slug, _ := specSlugFromName(filepath.Base(specPath))
	return slug
}

func resetPair(pair SpecPair) error {
	for _, path := range []string{pair.SpecPath, pair.ProgressPath} {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	return nil
}

func readProgressPromise(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	lines := strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n")
	var found []string
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		switch trimmed {
		case "":
			continue
		case promiseDone, promiseContinue:
			found = append(found, trimmed)
		default:
			if strings.HasPrefix(trimmed, "PROMISE:") {
				return "", fmt.Errorf("invalid promise line in %s: %q", path, trimmed)
			}
		}
	}
	if len(found) == 0 {
		return "", fmt.Errorf("missing promise line in %s", path)
	}
	if len(found) > 1 {
		return "", fmt.Errorf("multiple promise lines in %s", path)
	}
	return found[0], nil
}

func readProgressPromiseForState(path string) (string, error) {
	promise, err := readProgressPromise(path)
	if err != nil && strings.Contains(err.Error(), "missing promise line") {
		return promiseContinue, nil
	}
	return promise, err
}

func clearPromiseLines(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	lines := strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n")
	filtered := make([]string, 0, len(lines))
	for _, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), "PROMISE:") {
			continue
		}
		filtered = append(filtered, line)
	}
	content := strings.TrimRight(strings.Join(filtered, "\n"), "\n")
	if content == "" {
		return os.WriteFile(path, nil, 0o644)
	}
	return os.WriteFile(path, []byte(content+"\n"), 0o644)
}

func writeControllerNote(path, note string) error {
	note = strings.TrimSpace(note)
	if note == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	content := strings.TrimSpace(readOptionalFile(path))
	block := "Controller note:\n" + note
	if content == "" {
		return os.WriteFile(path, []byte(block+"\n"), 0o644)
	}
	return os.WriteFile(path, []byte(content+"\n\n"+block+"\n"), 0o644)
}

func renderPromptBlock(content string) string {
	content = strings.TrimSpace(content)
	if content == "" {
		return "(none)"
	}
	return content
}

func summarizePromptFailure(stage string, result promptResult, runErr error) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Ralph %s failed: %v", stage, runErr)
	if trimmed := tailText(result.Stdout, 2000); trimmed != "" {
		fmt.Fprintf(&b, "\n\nAssistant output tail:\n%s", trimmed)
	}
	if trimmed := tailText(result.Stderr, 2000); trimmed != "" {
		fmt.Fprintf(&b, "\n\nStderr tail:\n%s", trimmed)
	}
	return b.String()
}

func readOptionalFile(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return string(data)
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func firstPositive(values ...int) int {
	for _, v := range values {
		if v > 0 {
			return v
		}
	}
	return 0
}

func tailText(s string, limit int) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	if limit <= 0 || len(s) <= limit {
		return s
	}
	return s[len(s)-limit:]
}

func relPath(root, path string) string {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return path
	}
	return rel
}

func looksLikePath(target string) bool {
	return strings.Contains(target, string(os.PathSeparator)) || strings.HasPrefix(target, ".") || strings.HasSuffix(target, ".md")
}

func defaultGenerateSpecSlug() string {
	r := rand.New(rand.NewSource(time.Now().UnixNano()))
	return specAnimals[r.Intn(len(specAnimals))] + "-" + specActions[r.Intn(len(specActions))] + "-" + specPlants[r.Intn(len(specPlants))]
}

var specAnimals = []string{
	"otter", "falcon", "badger", "lynx", "heron", "beaver", "wren", "fox",
}

var specActions = []string{
	"refactor", "stabilize", "thread", "shape", "tune", "align", "harden", "simplify",
}

var specPlants = []string{
	"mint", "cedar", "fern", "olive", "clover", "maple", "thistle", "sage",
}
