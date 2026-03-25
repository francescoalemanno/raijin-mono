package ralph

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/francescoalemanno/raijin-mono/internal/shell"
	"golang.org/x/term"
)

type Mode string

const (
	ModeAuto Mode = "auto"
	ModePlan Mode = "plan"

	defaultMaxIterations = 25
	forceANSIEnv         = "RAIJIN_FORCE_ANSI"
	assistantCaptureEnv  = "RAIJIN_ASSISTANT_CAPTURE_FILE"
)

var ErrMaxIterationsReached = errors.New("ralph: maximum iterations reached")

type Options struct {
	Goal            string
	PlanningRequest string
	Mode            Mode
	RepoRoot        string
	MaxIterations   int
	ResetPlan       bool
}

type State struct {
	Goal          string `json:"goal"`
	RepoRoot      string `json:"repo_root"`
	Iteration     int    `json:"iteration"`
	MaxIterations int    `json:"max_iterations"`
	LastStatus    string `json:"last_status,omitempty"`
	LastPromise   string `json:"last_promise,omitempty"`
	LastError     string `json:"last_error,omitempty"`
	UpdatedAt     string `json:"updated_at"`
}

type Snapshot struct {
	RepoRoot string
	Goal     string
	Plan     string
}

type PlanningState string

const (
	PlanningStateEmpty     PlanningState = "empty"
	PlanningStatePlanned   PlanningState = "planned"
	PlanningStateCompleted PlanningState = "completed"
)

type PlanningStatus struct {
	RepoRoot string
	State    PlanningState
}

type loopPaths struct {
	root            string
	baseDir         string
	logsDir         string
	goal            string
	plan            string
	state           string
	feedback        string
	harnessFeedback string
}

type promptResult struct {
	Stdout string
	Stderr string
}

var (
	runEphemeralPrompt = defaultRunEphemeralPrompt
	resolveRepoRoot    = defaultResolveRepoRoot
)

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

	paths := newLoopPaths(repoRoot)
	if err := ensureDirs(paths); err != nil {
		return err
	}
	if mode == ModePlan && opts.ResetPlan {
		if err := resetPlanningWorkspace(paths); err != nil {
			return err
		}
	}

	planningRequest := strings.TrimSpace(opts.PlanningRequest)
	if mode == ModePlan && planningRequest == "" {
		planningRequest = strings.TrimSpace(opts.Goal)
	}
	canonicalGoal := strings.TrimSpace(opts.Goal)
	if mode == ModePlan {
		canonicalGoal = ""
	}

	state, reset, err := prepareState(repoRoot, paths, Options{
		Goal:            canonicalGoal,
		PlanningRequest: planningRequest,
		Mode:            mode,
		MaxIterations:   opts.MaxIterations,
	})
	if err != nil {
		return err
	}
	if reset {
		if err := resetWorkspace(paths); err != nil {
			return err
		}
		state.Iteration = 0
		state.LastStatus = ""
		state.LastPromise = ""
		state.LastError = ""
	}

	if mode != ModePlan {
		if err := writeGoal(paths.goal, state.Goal); err != nil {
			return err
		}
	}

	switch mode {
	case ModePlan:
		if err := runPlanning(ctx, paths, &state, planningRequest); err != nil {
			if errors.Is(err, context.Canceled) {
				state.LastStatus = "interrupted"
				state.LastError = ""
				_ = saveState(paths.state, state)
			}
			return err
		}
		return nil
	case ModeAuto:
		return runAutomatic(ctx, paths, &state)
	default:
		return fmt.Errorf("ralph: unsupported mode %q", mode)
	}
}

func ReadSnapshot(ctx context.Context, repoRoot string) (Snapshot, error) {
	resolvedRoot, err := resolveRepoRoot(ctx, strings.TrimSpace(repoRoot))
	if err != nil {
		return Snapshot{}, err
	}

	paths := newLoopPaths(resolvedRoot)
	goal := strings.TrimSpace(readOptionalFile(paths.goal))
	plan := strings.TrimSpace(readOptionalFile(paths.plan))
	if goal == "" && plan == "" {
		return Snapshot{}, fmt.Errorf("ralph: no goal or plan found in %s; run /plan <goal> first", relPath(resolvedRoot, paths.baseDir))
	}
	if goal == "" {
		return Snapshot{}, fmt.Errorf("ralph: %s does not exist or is empty", relPath(resolvedRoot, paths.goal))
	}
	if plan == "" {
		return Snapshot{}, fmt.Errorf("ralph: %s does not exist or is empty", relPath(resolvedRoot, paths.plan))
	}

	return Snapshot{
		RepoRoot: resolvedRoot,
		Goal:     goal,
		Plan:     plan,
	}, nil
}

func HasPlanningState(ctx context.Context, repoRoot string) (bool, error) {
	status, err := InspectPlanningState(ctx, repoRoot)
	if err != nil {
		return false, err
	}
	return status.State != PlanningStateEmpty, nil
}

func InspectPlanningState(ctx context.Context, repoRoot string) (PlanningStatus, error) {
	resolvedRoot, err := resolveRepoRoot(ctx, strings.TrimSpace(repoRoot))
	if err != nil {
		return PlanningStatus{}, err
	}

	paths := newLoopPaths(resolvedRoot)
	goal := strings.TrimSpace(readOptionalFile(paths.goal))
	plan := strings.TrimSpace(readOptionalFile(paths.plan))
	if goal == "" || plan == "" {
		return PlanningStatus{RepoRoot: resolvedRoot, State: PlanningStateEmpty}, nil
	}

	state, err := loadState(paths.state)
	if err == nil && state.LastStatus == "completed" {
		return PlanningStatus{RepoRoot: resolvedRoot, State: PlanningStateCompleted}, nil
	}
	return PlanningStatus{RepoRoot: resolvedRoot, State: PlanningStatePlanned}, nil
}

func runAutomatic(ctx context.Context, paths loopPaths, state *State) error {
	if !fileExists(paths.plan) {
		return fmt.Errorf("ralph: %s does not exist; run /plan <goal> first", relPath(paths.root, paths.plan))
	}

	for {
		if err := ctx.Err(); err != nil {
			state.LastStatus = "interrupted"
			state.LastError = ""
			if saveErr := saveState(paths.state, *state); saveErr != nil {
				return saveErr
			}
			return nil
		}
		if state.Iteration >= state.MaxIterations {
			state.LastStatus = "max_iterations"
			state.LastError = ErrMaxIterationsReached.Error()
			if err := saveState(paths.state, *state); err != nil {
				return err
			}
			return ErrMaxIterationsReached
		}

		state.Iteration++
		state.LastStatus = "iterating"
		state.LastError = ""
		if err := saveState(paths.state, *state); err != nil {
			return err
		}

		fmt.Fprintf(os.Stderr, "Ralph iteration %d/%d\n", state.Iteration, state.MaxIterations)
		harnessFeedback := strings.TrimSpace(readOptionalFile(paths.harnessFeedback))
		prompt := buildImplementationPrompt(*state, harnessFeedback)
		if harnessFeedback != "" {
			if err := clearHarnessFeedback(paths.harnessFeedback); err != nil {
				return err
			}
		}
		result, runErr := runEphemeralPrompt(ctx, state.RepoRoot, prompt, os.Stdout, os.Stderr)
		if err := writePromptLog(filepath.Join(paths.logsDir, fmt.Sprintf("iter-%d.txt", state.Iteration)), result, runErr); err != nil {
			return err
		}

		promise := detectPromise(result.Stdout)
		state.LastPromise = promise

		if runErr != nil {
			state.LastStatus = "iteration_failed"
			state.LastError = summarizePromptFailure("implementation iteration", result, runErr)
			if err := writeHarnessFeedback(paths.harnessFeedback, state.LastError); err != nil {
				return err
			}
			if err := saveState(paths.state, *state); err != nil {
				return err
			}
			continue
		}

		hasOpenTasks, err := planHasUncheckedTasks(paths.plan)
		if err != nil {
			state.LastStatus = "iteration_failed"
			state.LastError = fmt.Sprintf("Ralph could not read %s after iteration %d: %v", relPath(paths.root, paths.plan), state.Iteration, err)
			if writeErr := writeHarnessFeedback(paths.harnessFeedback, state.LastError); writeErr != nil {
				return writeErr
			}
			if saveErr := saveState(paths.state, *state); saveErr != nil {
				return saveErr
			}
			continue
		}

		if promise == "DONE" && !hasOpenTasks {
			state.LastStatus = "completed"
			state.LastError = ""
			if err := saveState(paths.state, *state); err != nil {
				return err
			}
			fmt.Fprintln(os.Stderr, "Ralph completed successfully")
			return nil
		}

		state.LastStatus = "continue"
		switch {
		case promise == "DONE" && hasOpenTasks:
			state.LastError = fmt.Sprintf("The agent emitted <promise>DONE</promise>, but %s still contains unchecked tasks. Pick the highest-priority unchecked task, complete it, update the plan, and only emit DONE when no unchecked tasks remain.", relPath(paths.root, paths.plan))
		case promise != "DONE" && !hasOpenTasks:
			state.LastError = fmt.Sprintf("%s has no unchecked tasks, but the agent did not emit <promise>DONE</promise>. Re-read the plan, confirm the work is complete, and emit DONE if no tasks remain.", relPath(paths.root, paths.plan))
		case promise == "":
			state.LastError = "The agent did not emit a required completion marker. Emit exactly one of <promise>CONTINUE</promise> or <promise>DONE</promise> at the end of the response."
		default:
			state.LastError = ""
		}
		if state.LastError != "" {
			if err := writeHarnessFeedback(paths.harnessFeedback, state.LastError); err != nil {
				return err
			}
		}
		if err := saveState(paths.state, *state); err != nil {
			return err
		}
	}
}

func runPlanning(ctx context.Context, paths loopPaths, state *State, planningRequest string) error {
	state.Iteration = 0
	state.LastStatus = "planning"
	state.LastPromise = ""
	state.LastError = ""
	if err := saveState(paths.state, *state); err != nil {
		return err
	}

	fmt.Fprintln(os.Stderr, "Ralph planning...")
	result, runErr := runEphemeralPrompt(ctx, state.RepoRoot, buildPlanningPrompt(*state, planningRequest), os.Stdout, os.Stderr)
	if err := writePromptLog(filepath.Join(paths.logsDir, "plan.txt"), result, runErr); err != nil {
		return err
	}
	if runErr != nil {
		state.LastStatus = "planning_failed"
		state.LastError = summarizePromptFailure("planning iteration", result, runErr)
		if err := saveState(paths.state, *state); err != nil {
			return err
		}
		return runErr
	}
	if !fileExists(paths.plan) {
		state.LastStatus = "planning_failed"
		state.LastError = fmt.Sprintf("Ralph planning did not create %s", relPath(paths.root, paths.plan))
		if err := saveState(paths.state, *state); err != nil {
			return err
		}
		return errors.New(state.LastError)
	}
	if !fileExists(paths.goal) {
		state.LastStatus = "planning_failed"
		state.LastError = fmt.Sprintf("Ralph planning did not create %s", relPath(paths.root, paths.goal))
		if err := saveState(paths.state, *state); err != nil {
			return err
		}
		return errors.New(state.LastError)
	}

	revisedGoal := strings.TrimSpace(readOptionalFile(paths.goal))
	if revisedGoal == "" {
		state.LastStatus = "planning_failed"
		state.LastError = fmt.Sprintf("Ralph planning left %s empty", relPath(paths.root, paths.goal))
		if err := saveState(paths.state, *state); err != nil {
			return err
		}
		return errors.New(state.LastError)
	}

	state.Goal = revisedGoal
	state.LastStatus = "planned"
	state.LastError = ""
	return saveState(paths.state, *state)
}

func prepareState(repoRoot string, paths loopPaths, opts Options) (State, bool, error) {
	existing, _ := loadState(paths.state)
	goalFromFile := strings.TrimSpace(readOptionalFile(paths.goal))

	state := existing
	state.RepoRoot = repoRoot
	state.MaxIterations = firstPositive(opts.MaxIterations, existing.MaxIterations, defaultMaxIterations)

	explicitGoal := strings.TrimSpace(opts.Goal)
	planningRequest := strings.TrimSpace(opts.PlanningRequest)
	currentGoal := strings.TrimSpace(existing.Goal)
	if currentGoal == "" {
		currentGoal = goalFromFile
	}

	reset := opts.Mode == ModeAuto && explicitGoal != "" && currentGoal != "" && explicitGoal != currentGoal

	if opts.Mode == ModePlan {
		switch {
		case planningRequest != "":
			state.Goal = ""
		default:
			return State{}, false, errors.New("ralph: goal is required; use /plan <goal> first")
		}
	} else {
		switch {
		case explicitGoal != "":
			state.Goal = explicitGoal
		case strings.TrimSpace(state.Goal) != "":
			state.Goal = strings.TrimSpace(state.Goal)
		case goalFromFile != "":
			state.Goal = goalFromFile
		default:
			return State{}, false, errors.New("ralph: goal is required; use /plan <goal> first")
		}
	}

	return state, reset, nil
}

func buildPlanningPrompt(state State, planningRequest string) string {
	return fmt.Sprintf(strings.TrimSpace(`
You are running inside a Ralph planning iteration for this repository.

This is a fresh ephemeral run. The durable memory for this loop is on disk.

Read these files first if they exist:
- .raijin/ralph/goal.md
- .raijin/ralph/plan.md
- AGENTS.md
- specs/
- README.md
- existing implementation files relevant to the goal
- .raijin/ralph/feedback.md

Your task is to revise .raijin/ralph/goal.md and .raijin/ralph/plan.md in place. Do not throw the existing plan away unless it is clearly obsolete for the current goal.

New planning request from /plan:
%s

Requirements:
1. Read .raijin/ralph/goal.md yourself if it exists, and compare the repository against that current goal plus the new planning request from /plan.
2. If .raijin/ralph/goal.md already exists, treat it as canonical and treat the new /plan request as revision instructions against it, not as an automatic replacement. Revise .raijin/ralph/goal.md if the new planning request changes intent, scope, constraints, or exclusions. If .raijin/ralph/goal.md does not exist yet, create it from the planning request. Do not blindly replace .raijin/ralph/goal.md with the raw /plan request text.
3. If .raijin/ralph/plan.md already exists, treat it as the starting point and revise it to match the revised goal. Keep the result as a prioritized Markdown checklist in .raijin/ralph/plan.md, preserve still-relevant unchecked tasks instead of replacing them wholesale, and reorder, merge, split, or rewrite tasks only when that improves correctness for the current goal.
4. If .raijin/ralph/feedback.md exists, decide whether it still matters for planning. If it is still useful, incorporate it into the revised plan. If it is obsolete after the revision, delete it.
5. Remove completed items or completed phases that no longer need tracking, especially when the plan is already completed and needs a clean next-step view.
6. Be surgical and technical. Base tasks on the actual codebase structure, existing abstractions, and likely edit points you inspected. Prefer narrow implementation steps over broad phases. A good task usually names a package, file, subsystem, or concrete behavior to change.
7. Avoid roadmap-style items such as "foundation", "architecture", "polish", or "improve quality" unless they are rewritten into specific engineering work.
8. Each task must be actionable, concrete, testable, and small enough that one implementation iteration can plausibly complete it. When useful, include specifics such as target files, APIs, invariants, edge cases, or verification expectations directly in the task text. Include acceptance criteria directly in task wording where useful.
9. Use Markdown checkbox items in priority order:
   - [ ] for remaining work
   - [x] only for work that is still useful to keep visible in the revised plan
10. The plan should read like a careful engineer's execution checklist, not a product roadmap or status document.
11. Planning mode must never execute the plan. Do not edit implementation files, do not run project changes, and do not perform the tasks in the plan. Planning mode is read-only except for revising .raijin/ralph/goal.md, revising .raijin/ralph/plan.md, and optionally deleting .raijin/ralph/feedback.md if it is obsolete.
12. Do not run verification commands, builds, tests, or migrations in planning mode. Keep the plan concise. Avoid prose-heavy essays. Do not emit <promise>DONE</promise> in planning mode.
`), renderPromptBlock(planningRequest))
}

func buildImplementationPrompt(state State, harnessFeedback string) string {
	return fmt.Sprintf(strings.TrimSpace(`
You are running inside a Ralph implementation iteration for this repository.

This is a fresh ephemeral run. The repository files are the memory between iterations.

Read these files first if they exist:
- .raijin/ralph/goal.md
- .raijin/ralph/plan.md
- .raijin/ralph/feedback.md
- AGENTS.md
- relevant source files, tests, and specs

Harness feedback from the previous iteration:
%s

Rules:
1. Pick exactly one highest-priority unchecked task from .raijin/ralph/plan.md.
2. Implement only that task in this iteration. Do not opportunistically start a second unchecked task, even if the first task finishes early.
3. Keep the iteration scoped to one coherent slice of work. If the task is larger than expected, stop after one technically coherent increment and leave the remaining work for a later iteration.
4. Update .raijin/ralph/plan.md to reflect progress before you finish. Do not mark a task complete if the remaining wiring, edge cases, or verification work means it is still not actually done.
5. If .raijin/ralph/feedback.md exists, read it before choosing work. Treat it as agent-authored handoff context from the previous iteration.
6. Treat the harness feedback section above as harness-authored blocker, regression, or verification feedback that may be higher priority than the next unchecked task.
7. Judge whether .raijin/ralph/feedback.md is still needed after your changes:
   - If it is still useful for the next fresh iteration, rewrite it as a short actionable note.
   - If you fully addressed it or it no longer helps, delete it.
   - If no feedback is needed, do not keep the file around.
8. If you leave unfinished wiring, partial integration, follow-up edge cases, or any other technical handoff for the next iteration, write that note into .raijin/ralph/feedback.md explicitly. Be concrete about what is incomplete, where it lives, and what must happen next.
9. Run relevant checks inside the repo before finishing.
10. Re-read the files you need before making decisions.
11. Do not emit <promise>DONE</promise> unless .raijin/ralph/plan.md has no unchecked tasks remaining.
12. Emit exactly one terminal marker at the end of your response:
   - <promise>CONTINUE</promise> if more tasks remain
   - <promise>DONE</promise> only if all tasks are complete

Current goal:
%s
`), renderPromptBlock(harnessFeedback), state.Goal)
}

func defaultRunEphemeralPrompt(ctx context.Context, repoRoot, prompt string, stdout, stderr io.Writer) (promptResult, error) {
	exePath, err := os.Executable()
	if err != nil {
		return promptResult{}, fmt.Errorf("ralph: resolve executable path: %w", err)
	}

	if term.IsTerminal(int(os.Stdout.Fd())) && term.IsTerminal(int(os.Stderr.Fd())) {
		captureFile, tempErr := os.CreateTemp("", "raijin-ralph-assistant-*.txt")
		if tempErr == nil {
			capturePath := captureFile.Name()
			_ = captureFile.Close()
			defer func() { _ = os.Remove(capturePath) }()

			runErr := shell.Run(ctx, shell.ExecSpec{
				Path:  exePath,
				Args:  []string{"--ephemeral", prompt},
				Dir:   repoRoot,
				Env:   upsertEnv(os.Environ(), assistantCaptureEnv, capturePath),
				Stdin: os.Stdin,
			}, stdout, stderr)

			captured, readErr := os.ReadFile(capturePath)
			if readErr != nil && !os.IsNotExist(readErr) {
				return promptResult{}, fmt.Errorf("ralph: read assistant capture: %w", readErr)
			}
			return promptResult{Stdout: string(captured)}, runErr
		}
	}

	var stdoutBuf bytes.Buffer
	var stderrBuf bytes.Buffer
	stdoutWriter := io.MultiWriter(stdout, &stdoutBuf)
	stderrWriter := io.MultiWriter(stderr, &stderrBuf)

	err = shell.Run(ctx, shell.ExecSpec{
		Path: exePath,
		Args: []string{"--ephemeral", prompt},
		Dir:  repoRoot,
		Env:  ralphEphemeralEnv(),
	}, stdoutWriter, stderrWriter)

	return promptResult{
		Stdout: stdoutBuf.String(),
		Stderr: stderrBuf.String(),
	}, err
}

func ralphEphemeralEnv() []string {
	env := append([]string(nil), os.Environ()...)
	if !term.IsTerminal(int(os.Stdout.Fd())) && !term.IsTerminal(int(os.Stderr.Fd())) {
		return env
	}
	return upsertEnv(env, forceANSIEnv, "1")
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

func newLoopPaths(repoRoot string) loopPaths {
	baseDir := filepath.Join(repoRoot, ".raijin", "ralph")
	return loopPaths{
		root:            repoRoot,
		baseDir:         baseDir,
		logsDir:         filepath.Join(baseDir, "logs"),
		goal:            filepath.Join(baseDir, "goal.md"),
		plan:            filepath.Join(baseDir, "plan.md"),
		state:           filepath.Join(baseDir, "state.json"),
		feedback:        filepath.Join(baseDir, "feedback.md"),
		harnessFeedback: filepath.Join(baseDir, "harness_feedback.md"),
	}
}

func ensureDirs(paths loopPaths) error {
	if err := os.MkdirAll(paths.logsDir, 0o755); err != nil {
		return fmt.Errorf("ralph: create logs dir: %w", err)
	}
	return nil
}

func resetWorkspace(paths loopPaths) error {
	if err := os.RemoveAll(paths.logsDir); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("ralph: reset logs dir: %w", err)
	}
	if err := os.MkdirAll(paths.logsDir, 0o755); err != nil {
		return fmt.Errorf("ralph: recreate logs dir: %w", err)
	}
	for _, path := range []string{paths.plan, paths.feedback, paths.harnessFeedback} {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	return nil
}

func resetPlanningWorkspace(paths loopPaths) error {
	if err := resetWorkspace(paths); err != nil {
		return err
	}
	for _, path := range []string{paths.goal, paths.state} {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	return nil
}

func saveState(path string, state State) error {
	state.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("ralph: marshal state: %w", err)
	}
	data = append(data, '\n')
	return os.WriteFile(path, data, 0o644)
}

func loadState(path string) (State, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return State{}, err
	}
	var state State
	if err := json.Unmarshal(data, &state); err != nil {
		return State{}, fmt.Errorf("ralph: decode state: %w", err)
	}
	return state, nil
}

func writeGoal(path, goal string) error {
	goal = strings.TrimSpace(goal)
	if goal == "" {
		return errors.New("ralph: goal cannot be empty")
	}
	return os.WriteFile(path, []byte(goal+"\n"), 0o644)
}

func writeHarnessFeedback(path, content string) error {
	content = strings.TrimSpace(content)
	if content == "" {
		return clearHarnessFeedback(path)
	}
	return os.WriteFile(path, []byte(content+"\n"), 0o644)
}

func clearHarnessFeedback(path string) error {
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func renderPromptBlock(content string) string {
	content = strings.TrimSpace(content)
	if content == "" {
		return "(none)"
	}
	return content
}

func writePromptLog(path string, result promptResult, runErr error) error {
	var b strings.Builder
	b.WriteString("STDOUT\n")
	b.WriteString(result.Stdout)
	if !strings.HasSuffix(result.Stdout, "\n") && strings.TrimSpace(result.Stdout) != "" {
		b.WriteByte('\n')
	}
	b.WriteString("\nSTDERR\n")
	b.WriteString(result.Stderr)
	if !strings.HasSuffix(result.Stderr, "\n") && strings.TrimSpace(result.Stderr) != "" {
		b.WriteByte('\n')
	}
	if runErr != nil {
		fmt.Fprintf(&b, "\nERROR\n%s\n", runErr.Error())
	}
	return os.WriteFile(path, []byte(b.String()), 0o644)
}

func planHasUncheckedTasks(path string) (bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return false, err
	}
	for _, line := range strings.Split(string(data), "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "- [ ]") || strings.HasPrefix(trimmed, "* [ ]") {
			return true, nil
		}
	}
	return false, nil
}

func detectPromise(output string) string {
	switch {
	case strings.Contains(output, "<promise>DONE</promise>"):
		return "DONE"
	case strings.Contains(output, "<promise>CONTINUE</promise>"):
		return "CONTINUE"
	default:
		return ""
	}
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

func upsertEnv(env []string, key, value string) []string {
	prefix := key + "="
	out := append([]string(nil), env...)
	for i, entry := range out {
		if strings.HasPrefix(entry, prefix) {
			out[i] = prefix + value
			return out
		}
	}
	return append(out, prefix+value)
}
