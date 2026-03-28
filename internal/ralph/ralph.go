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

	defaultMaxIterations         = 25
	defaultPlanningMaxIterations = 8
	promiseDone                  = "<promise>DONE</promise>"
	promiseContinue              = "<promise>CONTINUE</promise>"
	planningPromiseDone          = "<plan-promise>DONE</plan-promise>"
	planningPromiseContinue      = "<plan-promise>CONTINUE</plan-promise>"
)

var ErrMaxIterationsReached = errors.New("ralph: maximum iterations reached")

var (
	builderPromiseMarkers    = []string{promiseDone, promiseContinue}
	builderPromiseFragments  = []string{"<promise>", "</promise>"}
	planningPromiseMarkers   = []string{planningPromiseDone, planningPromiseContinue}
	planningPromiseFragments = []string{"<plan-promise>", "</plan-promise>"}
	allPromiseMarkers        = []string{promiseDone, promiseContinue, planningPromiseDone, planningPromiseContinue}
	allPromiseFragments      = []string{"<promise>", "</promise>", "<plan-promise>", "</plan-promise>"}
)

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
		return runPlanning(ctx, repoRoot, pair, planningRequest, maxIterations)
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

	promise, err := readPersistedProgressPromise(pair.ProgressPath)
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
		specPath := target
		if !filepath.IsAbs(specPath) {
			specPath = filepath.Join(resolvedRoot, specPath)
		}
		specPath, err := filepath.Abs(specPath)
		if err != nil {
			return SpecPair{}, false, err
		}
		return newSpecPair(specPath), true, nil
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
	renderer := newLoopRenderer(os.Stderr, repoRoot, pair)

	for iteration := 1; ; iteration++ {
		if err := ctx.Err(); err != nil {
			renderer.interrupted("builder")
			return nil
		}
		if iteration > maxIterations {
			return ErrMaxIterationsReached
		}
		renderer.iteration(iteration, maxIterations)
		if err := clearProgressPromiseMarkers(pair.ProgressPath); err != nil {
			return err
		}

		var acceptedPromise string
		onCompleteHook := finalResponsePromiseHook(pair.ProgressPath, &acceptedPromise)
		result, runErr := runEphemeralPrompt(ctx, repoRoot, EphemeralPromptOptions{
			Prompt:         buildImplementationPrompt(repoRoot, pair),
			OnCompleteHook: onCompleteHook,
		}, os.Stdout, os.Stderr)
		if runErr != nil {
			renderer.retry(iteration, summarizePromptFailure("implementation iteration", result, runErr))
			if noteErr := writeControllerNote(pair.ProgressPath, summarizePromptFailure("implementation iteration", result, runErr)); noteErr != nil {
				return noteErr
			}
			continue
		}

		switch acceptedPromise {
		case promiseDone:
			if err := appendProgressDonePromise(pair.ProgressPath); err != nil {
				return err
			}
			renderer.completed(iteration)
			return nil
		case promiseContinue:
			renderer.continuing(iteration)
		default:
			reason := fmt.Sprintf("Controller could not validate the final builder promise after iteration %d.", iteration)
			renderer.retry(iteration, reason)
			if noteErr := writeControllerNote(pair.ProgressPath, fmt.Sprintf("Controller note: Ralph could not validate the final builder promise after iteration %d.", iteration)); noteErr != nil {
				return noteErr
			}
		}
	}
}

func runPlanning(ctx context.Context, repoRoot string, pair SpecPair, planningRequest string, maxIterations int) error {
	if strings.TrimSpace(planningRequest) == "" {
		return errors.New("ralph: goal is required; use /plan <goal> first")
	}
	if err := os.MkdirAll(filepath.Dir(pair.SpecPath), 0o755); err != nil {
		return err
	}
	if runEphemeralPrompt == nil {
		return errors.New("ralph: no ephemeral runner registered")
	}
	renderer := newLoopRenderer(os.Stderr, repoRoot, pair)
	questionTool, err := newPlanningQuestionTool()
	if err != nil {
		return err
	}

	for iteration := 1; ; iteration++ {
		if err := ctx.Err(); err != nil {
			renderer.interrupted("planning")
			return nil
		}
		if iteration > maxIterations {
			return ErrMaxIterationsReached
		}
		renderer.planningIteration(planningRequest, iteration, maxIterations)
		if err := clearProgressPromiseMarkers(pair.ProgressPath); err != nil {
			return err
		}
		initialSpec := readOptionalFile(pair.SpecPath)
		initialProgress := readOptionalFile(pair.ProgressPath)

		var acceptedPromise string
		result, runErr := runEphemeralPrompt(ctx, repoRoot, EphemeralPromptOptions{
			Prompt: buildPlanningPrompt(repoRoot, pair, planningRequest),
			OnCompleteHook: planningPromiseHook(
				repoRoot,
				pair.SpecPath,
				pair.ProgressPath,
				initialSpec,
				initialProgress,
				&acceptedPromise,
			),
			ExtraTools: []libagent.Tool{questionTool},
		}, os.Stdout, os.Stderr)
		if runErr != nil {
			if errors.Is(runErr, context.Canceled) {
				renderer.interrupted("planning")
				return nil
			}
			reason := summarizePromptFailure("planning iteration", result, runErr)
			renderer.planningRetry(iteration, reason)
			if noteErr := writeControllerNote(pair.ProgressPath, reason); noteErr != nil {
				return noteErr
			}
			continue
		}

		switch acceptedPromise {
		case planningPromiseDone:
			renderer.planningCompleted(iteration)
			return nil
		case planningPromiseContinue:
			renderer.planningContinuing(iteration)
		default:
			reason := fmt.Sprintf("Controller could not validate the final planning promise after iteration %d.", iteration)
			renderer.planningRetry(iteration, reason)
			if noteErr := writeControllerNote(pair.ProgressPath, reason); noteErr != nil {
				return noteErr
			}
		}
	}
}

func prepareRun(repoRoot string, paths basePaths, opts Options) (SpecPair, string, int, error) {
	maxIterations := opts.MaxIterations
	if maxIterations <= 0 {
		maxIterations = defaultMaxIterations
		if opts.Mode == ModePlan {
			maxIterations = defaultPlanningMaxIterations
		}
	}

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
	specPath := relPath(repoRoot, pair.SpecPath)
	progressPath := relPath(repoRoot, pair.ProgressPath)
	request := renderPromptBlock(planningRequest)

	return strings.TrimSpace(
		"\nYou are running inside a Ralph planning iteration for this repository.\n\n" +
			"This is a fresh ephemeral run. The durable user/planner-owned specification and the planner-initialized progress file live on disk.\n\n" +
			"Read these files first if they exist:\n" +
			"- " + specPath + "\n" +
			"- " + progressPath + "\n" +
			"- AGENTS.md\n" +
			"- specs/\n" +
			"- README.md\n" +
			"- existing implementation files relevant to the request\n\n" +
			"Create or revise " + specPath + " in place and create or revise " + progressPath + " in place.\n\n" +
			"Keep " + specPath + " as the planner-owned durable spec with these sections:\n\n" +
			"# Goal\n" +
			"<durable project goal>\n\n" +
			"# User Specification\n" +
			"<durable user requirements, constraints, and exclusions>\n\n" +
			"# Plan\n" +
			"<durable implementation plan or checklist>\n\n" +
			"Requirements:\n" +
			"1. Treat " + specPath + " as planner-owned durable state. Keep it progress-free and revise it surgically when it already exists.\n" +
			"2. Treat " + progressPath + " as builder-facing mutable state. Keep it limited to concrete next tasks, ordering, remaining work, and short notes that still matter.\n" +
			"3. At the start of this iteration, choose one highest-leverage planning task that would most improve builder success.\n" +
			"4. That one planning task may refine scope, tighten acceptance criteria, inspect relevant code for feasibility, reorder implementation work, or de-risk the next builder slice.\n" +
			"5. Keep both files technical, concise, and limited to durable facts that are already established.\n" +
			"6. If uncertainty would materially change the goal, scope, constraints, acceptance criteria, or sequencing, use the question tool and ask 1-3 focused clarifying questions before proceeding. Free-form answers are allowed.\n" +
			"7. Planning mode only. You may inspect relevant implementation files for context, but do not edit implementation files or run builds, tests, migrations, or other verification commands.\n" +
			"8. Update " + specPath + " and " + progressPath + " surgically to reflect only the planning work completed in this iteration.\n" +
			"9. End your final response with exactly one whole-line planning promise marker: " + planningPromiseDone + " only if the spec and progress are builder-ready with no material planning ambiguity remaining, otherwise " + planningPromiseContinue + ".\n" +
			"10. The builder Ralph will treat " + specPath + " as read-only durable input and continue execution from " + progressPath + ".\n\n" +
			"New planning request from /plan:\n" +
			request + "\n",
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

func planningArtifactsChangedHook(repoRoot, specPath, progressPath, initialSpec, initialProgress string) libagent.OnCompleteHook {
	return func(context.Context, *libagent.AssistantMessage, []libagent.Message) (string, bool, error) {
		currentSpec := readOptionalFile(specPath)
		currentProgress := readOptionalFile(progressPath)
		if strings.TrimSpace(currentSpec) == "" {
			return fmt.Sprintf(
				"Your task is to bidirectionally create or revise the durable Ralph specification with the user and save it in %s. The spec file still matches what existed at the start of this planning session. Continue the planning interview if needed, ask clarifying questions when in doubt, and persist the updated spec to %s before ending.",
				relPath(repoRoot, specPath),
				relPath(repoRoot, specPath),
			), false, nil
		}
		if strings.TrimSpace(currentProgress) == "" {
			return fmt.Sprintf(
				"Before ending planning, initialize or revise %s so the builder Ralph can pick up concrete tasks from it. Keep only durable builder-facing task breakdown and next-step context, and save the updated progress file to %s.",
				relPath(repoRoot, progressPath),
				relPath(repoRoot, progressPath),
			), false, nil
		}
		if currentSpec == initialSpec && currentProgress == initialProgress {
			return fmt.Sprintf(
				"This planning iteration did not leave any durable change behind. Reopen %s or %s, make at least one meaningful planning update, and then respond again.",
				relPath(repoRoot, specPath),
				relPath(repoRoot, progressPath),
			), false, nil
		}
		return "", true, nil
	}
}

func planningPromiseHook(repoRoot, specPath, progressPath, initialSpec, initialProgress string, acceptedPromise *string) libagent.OnCompleteHook {
	artifactsHook := planningArtifactsChangedHook(repoRoot, specPath, progressPath, initialSpec, initialProgress)
	return func(ctx context.Context, final *libagent.AssistantMessage, messages []libagent.Message) (string, bool, error) {
		inject, ok, err := artifactsHook(ctx, final, messages)
		if err != nil {
			return "", false, err
		}
		if !ok {
			if acceptedPromise != nil {
				*acceptedPromise = ""
			}
			return inject, false, nil
		}

		promise, err := readRequiredPlanningPromiseMarker(libagent.AssistantText(final), "final planning response")
		if err == nil {
			if acceptedPromise != nil {
				*acceptedPromise = promise
			}
			return "", true, nil
		}

		if acceptedPromise != nil {
			*acceptedPromise = ""
		}
		return "Before ending this planning iteration, reopen " + relPath(repoRoot, specPath) + " and " + relPath(repoRoot, progressPath) +
			" and update them coherently with the planning rules. Preserve durable scope in the spec and only still-relevant builder-facing work in progress. " +
			"End your final response with exactly one whole-line planning promise marker: " + planningPromiseDone +
			" only if the spec and progress are builder-ready with no material planning ambiguity remaining; otherwise use " + planningPromiseContinue + ". Then respond again.", false, nil
	}
}

func buildImplementationPrompt(repoRoot string, pair SpecPair) string {
	specPath := relPath(repoRoot, pair.SpecPath)
	progressPath := relPath(repoRoot, pair.ProgressPath)

	return strings.TrimSpace(
		"\nYou are running inside a Ralph builder iteration for this repository.\n\n" +
			"This is a fresh ephemeral run. The durable planner specification and your mutable progress file live on disk.\n\n" +
			"Read these files first if they exist:\n" +
			"- " + specPath + "\n" +
			"- " + progressPath + "\n" +
			"- AGENTS.md\n" +
			"- relevant source files, tests, and specs\n\n" +
			"Rules:\n" +
			"1. Treat " + specPath + " as read-only durable input. Do not modify it.\n" +
			"2. Use " + progressPath + " as your mutable working state. Preserve still-relevant task breakdown, remaining work, and controller notes. If it does not exist yet, create it before finishing.\n" +
			"3. At the start of the iteration, choose one concrete highest-leverage open task from " + progressPath + ".\n" +
			"4. Do only that one task this iteration. Prefer foundational work that unlocks or de-risks later work.\n" +
			"5. Run the relevant checks for that work before finishing.\n" +
			"6. Update " + progressPath + " to reflect what changed, what remains, and any short notes that still matter.\n" +
			"7. End your final response with exactly one whole-line marker: " + promiseDone + " if the full specification is complete and verified, otherwise " + promiseContinue + ".\n",
	)
}

func finalResponsePromiseHook(progressPath string, acceptedPromise *string) libagent.OnCompleteHook {
	return func(_ context.Context, final *libagent.AssistantMessage, _ []libagent.Message) (string, bool, error) {
		promise, err := readRequiredPromiseMarker(libagent.AssistantText(final), "final builder response")
		if err == nil {
			if acceptedPromise != nil {
				*acceptedPromise = promise
			}
			return "", true, nil
		}
		if acceptedPromise != nil {
			*acceptedPromise = ""
		}
		return "Before ending this builder iteration, reopen " + progressPath +
			" and update it coherently with the builder rules. Preserve still-relevant progress, remaining work, and controller notes. " +
			"End your final response with exactly one whole-line promise marker: " + promiseDone + " if the entire current specification is complete, verified, and there is no important remaining work; otherwise " + promiseContinue + ". " +
			"Finishing only the current task is not enough for " + promiseDone + ". Then respond again.", false, nil
	}
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

func readPersistedProgressPromise(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return readPersistedPromiseMarker(string(data)), nil
}

func readRequiredPromiseMarker(content, location string) (string, error) {
	promise, found, err := readOptionalPromiseMarkerSet(content, location, builderPromiseMarkers, builderPromiseFragments)
	if err != nil {
		return "", err
	}
	if !found {
		return "", fmt.Errorf("missing promise marker in %s", location)
	}
	return promise, nil
}

func readRequiredPlanningPromiseMarker(content, location string) (string, error) {
	promise, found, err := readOptionalPromiseMarkerSet(content, location, planningPromiseMarkers, planningPromiseFragments)
	if err != nil {
		return "", err
	}
	if !found {
		return "", fmt.Errorf("missing promise marker in %s", location)
	}
	return promise, nil
}

func readOptionalPromiseMarkerSet(content, location string, markers, fragments []string) (promise string, found bool, err error) {
	promises, invalid := collectPromiseMarkers(content, markers, fragments)
	if invalid != "" {
		return "", false, fmt.Errorf("invalid promise marker in %s: %q", location, invalid)
	}
	if len(promises) == 0 {
		return "", false, nil
	}
	if len(promises) > 1 {
		return "", false, fmt.Errorf("multiple promise markers in %s", location)
	}
	if last := lastNonEmptyTrimmedLine(content); last != promises[0] {
		return "", false, fmt.Errorf("promise marker must be the final non-empty line in %s", location)
	}
	return promises[0], true, nil
}

func collectPromiseMarkers(content string, markers, fragments []string) (found []string, invalid string) {
	lines := strings.Split(strings.ReplaceAll(content, "\r\n", "\n"), "\n")
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		promise, promiseLike := extractPromiseMarker(trimmed, markers, fragments)
		if promise != "" {
			found = append(found, promise)
			continue
		}
		if promiseLike {
			return nil, trimmed
		}
	}
	return found, ""
}

func clearProgressPromiseMarkers(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	content := stripPersistedPromiseMarkersSet(string(data), allPromiseMarkers, allPromiseFragments)
	if content == "" {
		return os.WriteFile(path, nil, 0o644)
	}
	return os.WriteFile(path, []byte(content+"\n"), 0o644)
}

func appendProgressDonePromise(path string) error {
	content := stripPersistedPromiseMarkersSet(readOptionalFile(path), allPromiseMarkers, allPromiseFragments)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	if content == "" {
		return os.WriteFile(path, []byte(promiseDone+"\n"), 0o644)
	}
	return os.WriteFile(path, []byte(content+"\n"+promiseDone+"\n"), 0o644)
}

func readPersistedPromiseMarker(content string) string {
	last := lastNonEmptyTrimmedLine(content)
	for _, marker := range allPromiseMarkers {
		if last == marker {
			return marker
		}
	}
	return ""
}

func stripPersistedPromiseMarkersSet(content string, markers, fragments []string) string {
	lines := strings.Split(strings.ReplaceAll(content, "\r\n", "\n"), "\n")
	filtered := make([]string, 0, len(lines))
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		promise, promiseLike := extractPromiseMarker(trimmed, markers, fragments)
		if promise != "" || promiseLike {
			continue
		}
		filtered = append(filtered, line)
	}
	return strings.TrimRight(strings.Join(filtered, "\n"), "\n")
}

func extractPromiseMarker(trimmed string, markers, fragments []string) (promise string, promiseLike bool) {
	trimmed = strings.TrimSpace(trimmed)
	if trimmed == "" {
		return "", false
	}
	for _, marker := range markers {
		if trimmed == marker {
			return trimmed, true
		}
	}
	for _, fragment := range fragments {
		if strings.Contains(trimmed, fragment) {
			return "", true
		}
	}
	return "", false
}

func lastNonEmptyTrimmedLine(content string) string {
	lines := strings.Split(strings.ReplaceAll(content, "\r\n", "\n"), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		trimmed := strings.TrimSpace(lines[i])
		if trimmed != "" {
			return trimmed
		}
	}
	return ""
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
