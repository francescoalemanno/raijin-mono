package ralph

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunPlanCreatesArtifactsWithoutImplicitVerifyDiscovery(t *testing.T) {
	repo := t.TempDir()

	origPrompt := runEphemeralPrompt
	t.Cleanup(func() { runEphemeralPrompt = origPrompt })
	runEphemeralPrompt = func(_ context.Context, repoRoot, prompt string, _, _ io.Writer) (promptResult, error) {
		if repoRoot != repo {
			t.Fatalf("repoRoot = %q, want %q", repoRoot, repo)
		}
		if !strings.Contains(prompt, "revise .raijin/ralph/goal.md and .raijin/ralph/plan.md in place") {
			t.Fatalf("planning prompt missing revision instruction: %q", prompt)
		}
		if !strings.Contains(prompt, "- .raijin/ralph/plan.md") {
			t.Fatalf("planning prompt should read the existing plan: %q", prompt)
		}
		if !strings.Contains(prompt, "If .raijin/ralph/feedback.md exists, decide whether it still matters for planning") {
			t.Fatalf("planning prompt should explain feedback handling: %q", prompt)
		}
		if !strings.Contains(prompt, "Be surgical and technical.") {
			t.Fatalf("planning prompt should demand a technical plan: %q", prompt)
		}
		if !strings.Contains(prompt, "Prefer narrow implementation steps over broad phases.") {
			t.Fatalf("planning prompt should prefer narrow tasks: %q", prompt)
		}
		if !strings.Contains(prompt, "The plan should read like a careful engineer's execution checklist") {
			t.Fatalf("planning prompt should require an engineer-style plan: %q", prompt)
		}
		if !strings.Contains(prompt, "Planning mode must never execute the plan.") {
			t.Fatalf("planning prompt should explicitly forbid execution: %q", prompt)
		}
		if !strings.Contains(prompt, "Do not run verification commands, builds, tests, or migrations in planning mode.") {
			t.Fatalf("planning prompt should explicitly forbid command execution: %q", prompt)
		}
		if !strings.Contains(prompt, "New planning request from /plan:") {
			t.Fatalf("planning prompt should include the /plan request: %q", prompt)
		}
		if strings.Contains(prompt, "Current canonical goal from .raijin/ralph/goal.md:") {
			t.Fatalf("planning prompt should not inject canonical goal content directly: %q", prompt)
		}
		if !strings.Contains(prompt, "Read .raijin/ralph/goal.md yourself if it exists") {
			t.Fatalf("planning prompt should tell the model to read goal.md itself: %q", prompt)
		}
		if !strings.Contains(prompt, "implement ralph") {
			t.Fatalf("planning prompt should include the initial planning request: %q", prompt)
		}
		if err := os.WriteFile(filepath.Join(repo, ".raijin", "ralph", "plan.md"), []byte("- [ ] first task\n"), 0o644); err != nil {
			t.Fatalf("write plan.md: %v", err)
		}
		if err := os.WriteFile(filepath.Join(repo, ".raijin", "ralph", "goal.md"), []byte("implement ralph\n"), 0o644); err != nil {
			t.Fatalf("write goal.md: %v", err)
		}
		return promptResult{Stdout: "planned\n"}, nil
	}

	if err := Run(context.Background(), Options{
		PlanningRequest: "implement ralph",
		Mode:            ModePlan,
		RepoRoot:        repo,
	}); err != nil {
		t.Fatalf("Run(plan): %v", err)
	}

	state, err := loadState(filepath.Join(repo, ".raijin", "ralph", "state.json"))
	if err != nil {
		t.Fatalf("loadState: %v", err)
	}
	if state.LastStatus != "planned" {
		t.Fatalf("LastStatus = %q, want planned", state.LastStatus)
	}
	if state.Goal != "implement ralph" {
		t.Fatalf("Goal = %q, want %q", state.Goal, "implement ralph")
	}
	if goal := readOptionalFile(filepath.Join(repo, ".raijin", "ralph", "goal.md")); strings.TrimSpace(goal) != "implement ralph" {
		t.Fatalf("goal.md = %q", goal)
	}
}

func TestReadSnapshotReturnsGoalAndPlan(t *testing.T) {
	repo := t.TempDir()
	if err := os.MkdirAll(filepath.Join(repo, ".raijin", "ralph"), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(repo, ".raijin", "ralph", "goal.md"), []byte("ship the tui refresh\n"), 0o644); err != nil {
		t.Fatalf("write goal: %v", err)
	}
	if err := os.WriteFile(filepath.Join(repo, ".raijin", "ralph", "plan.md"), []byte("- [ ] tighten spacing\n"), 0o644); err != nil {
		t.Fatalf("write plan: %v", err)
	}

	got, err := ReadSnapshot(context.Background(), repo)
	if err != nil {
		t.Fatalf("ReadSnapshot: %v", err)
	}
	if got.RepoRoot != repo {
		t.Fatalf("RepoRoot = %q, want %q", got.RepoRoot, repo)
	}
	if got.Goal != "ship the tui refresh" {
		t.Fatalf("Goal = %q", got.Goal)
	}
	if got.Plan != "- [ ] tighten spacing" {
		t.Fatalf("Plan = %q", got.Plan)
	}
}

func TestHasPlanningState(t *testing.T) {
	repo := t.TempDir()
	if err := os.MkdirAll(filepath.Join(repo, ".raijin", "ralph"), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	got, err := HasPlanningState(context.Background(), repo)
	if err != nil {
		t.Fatalf("HasPlanningState(empty): %v", err)
	}
	if got {
		t.Fatalf("HasPlanningState(empty) = true, want false")
	}

	if err := os.WriteFile(filepath.Join(repo, ".raijin", "ralph", "goal.md"), []byte("goal\n"), 0o644); err != nil {
		t.Fatalf("write goal: %v", err)
	}
	got, err = HasPlanningState(context.Background(), repo)
	if err != nil {
		t.Fatalf("HasPlanningState(goal only): %v", err)
	}
	if got {
		t.Fatalf("HasPlanningState(goal only) = true, want false")
	}

	if err := os.WriteFile(filepath.Join(repo, ".raijin", "ralph", "plan.md"), []byte("- [ ] task\n"), 0o644); err != nil {
		t.Fatalf("write plan: %v", err)
	}
	got, err = HasPlanningState(context.Background(), repo)
	if err != nil {
		t.Fatalf("HasPlanningState(goal+plan): %v", err)
	}
	if !got {
		t.Fatalf("HasPlanningState(goal+plan) = false, want true")
	}
}

func TestInspectPlanningStateClassifiesStatuses(t *testing.T) {
	repo := t.TempDir()
	if err := os.MkdirAll(filepath.Join(repo, ".raijin", "ralph"), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	status, err := InspectPlanningState(context.Background(), repo)
	if err != nil {
		t.Fatalf("InspectPlanningState(empty): %v", err)
	}
	if status.State != PlanningStateEmpty {
		t.Fatalf("state = %q, want empty", status.State)
	}

	if err := os.WriteFile(filepath.Join(repo, ".raijin", "ralph", "goal.md"), []byte("goal\n"), 0o644); err != nil {
		t.Fatalf("write goal: %v", err)
	}
	if err := os.WriteFile(filepath.Join(repo, ".raijin", "ralph", "plan.md"), []byte("- [ ] task\n"), 0o644); err != nil {
		t.Fatalf("write plan: %v", err)
	}

	status, err = InspectPlanningState(context.Background(), repo)
	if err != nil {
		t.Fatalf("InspectPlanningState(planned): %v", err)
	}
	if status.State != PlanningStatePlanned {
		t.Fatalf("state = %q, want planned", status.State)
	}

	if err := saveState(filepath.Join(repo, ".raijin", "ralph", "state.json"), State{
		Goal:          "goal",
		RepoRoot:      repo,
		MaxIterations: defaultMaxIterations,
		LastStatus:    "completed",
	}); err != nil {
		t.Fatalf("saveState: %v", err)
	}

	status, err = InspectPlanningState(context.Background(), repo)
	if err != nil {
		t.Fatalf("InspectPlanningState(completed): %v", err)
	}
	if status.State != PlanningStateCompleted {
		t.Fatalf("state = %q, want completed", status.State)
	}
}

func TestRunPlanDoesNotDeleteExistingPlanOnGoalRevision(t *testing.T) {
	repo := t.TempDir()
	if err := os.MkdirAll(filepath.Join(repo, ".raijin", "ralph"), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	existingGoal := "make TUI awesome by improving layout, typography, and navigation"
	revisionRequest := "revise item 3: do not add progress bars"
	existingPlan := "- [x] foundation complete\n- [ ] next step\n"
	if err := os.WriteFile(filepath.Join(repo, ".raijin", "ralph", "plan.md"), []byte(existingPlan), 0o644); err != nil {
		t.Fatalf("write existing plan: %v", err)
	}
	if err := os.WriteFile(filepath.Join(repo, ".raijin", "ralph", "goal.md"), []byte(existingGoal+"\n"), 0o644); err != nil {
		t.Fatalf("write existing goal: %v", err)
	}
	if err := saveState(filepath.Join(repo, ".raijin", "ralph", "state.json"), State{
		Goal:          existingGoal,
		RepoRoot:      repo,
		Iteration:     17,
		MaxIterations: defaultMaxIterations,
		LastStatus:    "max_iterations",
		LastPromise:   "CONTINUE",
		LastError:     "previous failure",
	}); err != nil {
		t.Fatalf("saveState: %v", err)
	}

	origPrompt := runEphemeralPrompt
	t.Cleanup(func() { runEphemeralPrompt = origPrompt })
	runEphemeralPrompt = func(_ context.Context, repoRoot, prompt string, _, _ io.Writer) (promptResult, error) {
		if repoRoot != repo {
			t.Fatalf("repoRoot = %q, want %q", repoRoot, repo)
		}
		currentGoal, err := os.ReadFile(filepath.Join(repoRoot, ".raijin", "ralph", "goal.md"))
		if err != nil {
			t.Fatalf("ReadFile(goal): %v", err)
		}
		if strings.TrimSpace(string(currentGoal)) != existingGoal {
			t.Fatalf("existing goal was modified before planning run: %q", string(currentGoal))
		}
		currentPlan, err := os.ReadFile(filepath.Join(repoRoot, ".raijin", "ralph", "plan.md"))
		if err != nil {
			t.Fatalf("ReadFile(plan): %v", err)
		}
		if string(currentPlan) != existingPlan {
			t.Fatalf("existing plan was modified before planning run: %q", string(currentPlan))
		}
		if !strings.Contains(prompt, "treat it as canonical and treat the new /plan request as revision instructions against it") {
			t.Fatalf("planning prompt missing goal-revision guidance: %q", prompt)
		}
		if !strings.Contains(prompt, "Do not blindly replace .raijin/ralph/goal.md with the raw /plan request text.") {
			t.Fatalf("planning prompt missing no-blind-replacement guidance: %q", prompt)
		}
		if strings.Contains(prompt, existingGoal) {
			t.Fatalf("planning prompt should not inject the existing goal content directly: %q", prompt)
		}
		if !strings.Contains(prompt, revisionRequest) {
			t.Fatalf("planning prompt should include the revision request: %q", prompt)
		}
		if !strings.Contains(prompt, "Read .raijin/ralph/goal.md yourself if it exists") {
			t.Fatalf("planning prompt should tell the model to read goal.md itself: %q", prompt)
		}
		if !strings.Contains(prompt, "Remove completed items or completed phases") {
			t.Fatalf("planning prompt missing completed-item pruning guidance: %q", prompt)
		}
		if !strings.Contains(prompt, "Avoid roadmap-style items such as \"foundation\", \"architecture\", \"polish\", or \"improve quality\"") {
			t.Fatalf("planning prompt missing anti-roadmap guidance: %q", prompt)
		}
		if !strings.Contains(prompt, "If it is obsolete after the revision, delete it.") {
			t.Fatalf("planning prompt missing obsolete-feedback deletion guidance: %q", prompt)
		}
		if err := os.WriteFile(filepath.Join(repoRoot, ".raijin", "ralph", "plan.md"), []byte("- [ ] revised next step\n"), 0o644); err != nil {
			t.Fatalf("write revised plan: %v", err)
		}
		if err := os.WriteFile(filepath.Join(repoRoot, ".raijin", "ralph", "goal.md"), []byte(existingGoal+" without progress bars\n"), 0o644); err != nil {
			t.Fatalf("write revised goal: %v", err)
		}
		return promptResult{Stdout: "planned\n"}, nil
	}

	if err := Run(context.Background(), Options{
		PlanningRequest: revisionRequest,
		Mode:            ModePlan,
		RepoRoot:        repo,
	}); err != nil {
		t.Fatalf("Run(plan): %v", err)
	}

	gotPlan, err := os.ReadFile(filepath.Join(repo, ".raijin", "ralph", "plan.md"))
	if err != nil {
		t.Fatalf("ReadFile(revised plan): %v", err)
	}
	if string(gotPlan) != "- [ ] revised next step\n" {
		t.Fatalf("plan after revision = %q", string(gotPlan))
	}
	gotGoal, err := os.ReadFile(filepath.Join(repo, ".raijin", "ralph", "goal.md"))
	if err != nil {
		t.Fatalf("ReadFile(revised goal): %v", err)
	}
	if strings.TrimSpace(string(gotGoal)) != existingGoal+" without progress bars" {
		t.Fatalf("goal after revision = %q", string(gotGoal))
	}

	state, err := loadState(filepath.Join(repo, ".raijin", "ralph", "state.json"))
	if err != nil {
		t.Fatalf("loadState(revised): %v", err)
	}
	if state.Goal != existingGoal+" without progress bars" {
		t.Fatalf("Goal after planning = %q, want revised goal", state.Goal)
	}
	if state.Iteration != 0 {
		t.Fatalf("Iteration after planning = %d, want 0", state.Iteration)
	}
	if state.LastStatus != "planned" {
		t.Fatalf("LastStatus after planning = %q, want planned", state.LastStatus)
	}
	if state.LastPromise != "" {
		t.Fatalf("LastPromise after planning = %q, want empty", state.LastPromise)
	}
	if state.LastError != "" {
		t.Fatalf("LastError after planning = %q, want empty", state.LastError)
	}
}

func TestRunPlanFromScratchClearsExistingGoalAndPlanBeforePlanning(t *testing.T) {
	repo := t.TempDir()
	if err := os.MkdirAll(filepath.Join(repo, ".raijin", "ralph"), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(repo, ".raijin", "ralph", "plan.md"), []byte("- [ ] stale task\n"), 0o644); err != nil {
		t.Fatalf("write existing plan: %v", err)
	}
	if err := os.WriteFile(filepath.Join(repo, ".raijin", "ralph", "goal.md"), []byte("old goal\n"), 0o644); err != nil {
		t.Fatalf("write existing goal: %v", err)
	}
	if err := saveState(filepath.Join(repo, ".raijin", "ralph", "state.json"), State{
		Goal:          "old goal",
		RepoRoot:      repo,
		Iteration:     9,
		MaxIterations: defaultMaxIterations,
		LastStatus:    "continue",
		LastPromise:   "CONTINUE",
		LastError:     "old failure",
	}); err != nil {
		t.Fatalf("saveState: %v", err)
	}

	origPrompt := runEphemeralPrompt
	t.Cleanup(func() { runEphemeralPrompt = origPrompt })
	runEphemeralPrompt = func(_ context.Context, repoRoot, prompt string, _, _ io.Writer) (promptResult, error) {
		if repoRoot != repo {
			t.Fatalf("repoRoot = %q, want %q", repoRoot, repo)
		}
		if fileExists(filepath.Join(repoRoot, ".raijin", "ralph", "goal.md")) {
			t.Fatalf("goal.md should be cleared before scratch planning")
		}
		if fileExists(filepath.Join(repoRoot, ".raijin", "ralph", "plan.md")) {
			t.Fatalf("plan.md should be cleared before scratch planning")
		}
		if err := os.WriteFile(filepath.Join(repoRoot, ".raijin", "ralph", "plan.md"), []byte("- [ ] brand new task\n"), 0o644); err != nil {
			t.Fatalf("write new plan: %v", err)
		}
		if err := os.WriteFile(filepath.Join(repoRoot, ".raijin", "ralph", "goal.md"), []byte("brand new goal\n"), 0o644); err != nil {
			t.Fatalf("write new goal: %v", err)
		}
		return promptResult{Stdout: "planned\n"}, nil
	}

	if err := Run(context.Background(), Options{
		PlanningRequest: "start over completely",
		Mode:            ModePlan,
		RepoRoot:        repo,
		ResetPlan:       true,
	}); err != nil {
		t.Fatalf("Run(plan scratch): %v", err)
	}

	state, err := loadState(filepath.Join(repo, ".raijin", "ralph", "state.json"))
	if err != nil {
		t.Fatalf("loadState: %v", err)
	}
	if state.Goal != "brand new goal" {
		t.Fatalf("Goal = %q, want brand new goal", state.Goal)
	}
	if state.Iteration != 0 {
		t.Fatalf("Iteration = %d, want 0", state.Iteration)
	}
}

func TestRunAutoRetriesAfterHarnessFeedbackAndCompletes(t *testing.T) {
	repo := t.TempDir()
	if err := os.MkdirAll(filepath.Join(repo, ".raijin", "ralph"), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(repo, ".raijin", "ralph", "plan.md"), []byte("- [ ] task one\n- [ ] task two\n"), 0o644); err != nil {
		t.Fatalf("write initial plan: %v", err)
	}

	origPrompt := runEphemeralPrompt
	t.Cleanup(func() {
		runEphemeralPrompt = origPrompt
	})

	call := 0
	runEphemeralPrompt = func(_ context.Context, repoRoot, prompt string, _, _ io.Writer) (promptResult, error) {
		call++
		switch call {
		case 1:
			if fileExists(filepath.Join(repoRoot, ".raijin", "ralph", "feedback.md")) {
				t.Fatalf("feedback should not exist before first build iteration")
			}
			if fileExists(filepath.Join(repoRoot, ".raijin", "ralph", "harness_feedback.md")) {
				t.Fatalf("harness feedback should not exist before first build iteration")
			}
			if err := os.WriteFile(filepath.Join(repoRoot, ".raijin", "ralph", "plan.md"), []byte("- [x] task one\n- [ ] task two\n"), 0o644); err != nil {
				t.Fatalf("write plan after iteration 1: %v", err)
			}
			if err := os.WriteFile(filepath.Join(repoRoot, ".raijin", "ralph", "feedback.md"), []byte("Agent note: wiring between parser and CLI still needs final call-site hookup.\n"), 0o644); err != nil {
				t.Fatalf("write agent feedback note: %v", err)
			}
			return promptResult{Stdout: "implemented first task\n"}, nil
		case 2:
			feedback := readOptionalFile(filepath.Join(repoRoot, ".raijin", "ralph", "feedback.md"))
			if !strings.Contains(feedback, "Agent note: wiring between parser and CLI still needs final call-site hookup.") {
				t.Fatalf("expected agent handoff note to be preserved, got %q", feedback)
			}
			if fileExists(filepath.Join(repoRoot, ".raijin", "ralph", "harness_feedback.md")) {
				t.Fatalf("harness_feedback.md should be removed before the next iteration runs")
			}
			if !strings.Contains(prompt, "Harness feedback from the previous iteration:") {
				t.Fatalf("expected interpolated harness feedback section, got %q", prompt)
			}
			if !strings.Contains(prompt, "The agent did not emit a required completion marker.") {
				t.Fatalf("expected harness feedback in prompt, got %q", prompt)
			}
			if err := os.WriteFile(filepath.Join(repoRoot, ".raijin", "ralph", "plan.md"), []byte("- [x] task one\n- [x] task two\n"), 0o644); err != nil {
				t.Fatalf("write plan after iteration 2: %v", err)
			}
			return promptResult{Stdout: "implemented second task\n<promise>DONE</promise>\n"}, nil
		default:
			t.Fatalf("unexpected prompt invocation %d", call)
			return promptResult{}, nil
		}
	}

	if err := Run(context.Background(), Options{
		Goal:          "ship two tasks",
		Mode:          ModeAuto,
		RepoRoot:      repo,
		MaxIterations: 3,
	}); err != nil {
		t.Fatalf("Run(auto): %v", err)
	}

	state, err := loadState(filepath.Join(repo, ".raijin", "ralph", "state.json"))
	if err != nil {
		t.Fatalf("loadState: %v", err)
	}
	if state.Iteration != 2 {
		t.Fatalf("Iteration = %d, want 2", state.Iteration)
	}
	if state.LastStatus != "completed" {
		t.Fatalf("LastStatus = %q, want completed", state.LastStatus)
	}
	if state.LastPromise != "DONE" {
		t.Fatalf("LastPromise = %q, want DONE", state.LastPromise)
	}
	feedbackAfterSuccess := readOptionalFile(filepath.Join(repo, ".raijin", "ralph", "feedback.md"))
	if !strings.Contains(feedbackAfterSuccess, "Agent note: wiring between parser and CLI still needs final call-site hookup.") {
		t.Fatalf("expected manual feedback to survive successful verification, got %q", feedbackAfterSuccess)
	}
	if fileExists(filepath.Join(repo, ".raijin", "ralph", "harness_feedback.md")) {
		t.Fatalf("harness_feedback.md should be removed after the next iteration consumes it")
	}
	if !fileExists(filepath.Join(repo, ".raijin", "ralph", "logs", "iter-1.txt")) || !fileExists(filepath.Join(repo, ".raijin", "ralph", "logs", "iter-2.txt")) {
		t.Fatalf("expected iteration logs to exist")
	}
}

func TestRunAutoStopsAtMaxIterationsAndPreservesState(t *testing.T) {
	repo := t.TempDir()
	if err := os.MkdirAll(filepath.Join(repo, ".raijin", "ralph"), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(repo, ".raijin", "ralph", "plan.md"), []byte("- [ ] still open\n"), 0o644); err != nil {
		t.Fatalf("write initial plan: %v", err)
	}

	origPrompt := runEphemeralPrompt
	t.Cleanup(func() {
		runEphemeralPrompt = origPrompt
	})

	runEphemeralPrompt = func(_ context.Context, repoRoot, prompt string, _, _ io.Writer) (promptResult, error) {
		if err := os.WriteFile(filepath.Join(repoRoot, ".raijin", "ralph", "plan.md"), []byte("- [ ] still open\n"), 0o644); err != nil {
			t.Fatalf("keep plan open: %v", err)
		}
		return promptResult{Stdout: "not done\n<promise>CONTINUE</promise>\n"}, nil
	}
	err := Run(context.Background(), Options{
		Goal:          "keep looping",
		Mode:          ModeAuto,
		RepoRoot:      repo,
		MaxIterations: 1,
	})
	if !errors.Is(err, ErrMaxIterationsReached) {
		t.Fatalf("err = %v, want ErrMaxIterationsReached", err)
	}

	state, err := loadState(filepath.Join(repo, ".raijin", "ralph", "state.json"))
	if err != nil {
		t.Fatalf("loadState: %v", err)
	}
	if state.LastStatus != "max_iterations" {
		t.Fatalf("LastStatus = %q, want max_iterations", state.LastStatus)
	}
	if state.Iteration != 1 {
		t.Fatalf("Iteration = %d, want 1", state.Iteration)
	}
	if !fileExists(filepath.Join(repo, ".raijin", "ralph", "plan.md")) {
		t.Fatalf("plan.md should be preserved")
	}
}

func TestUpsertEnvReplacesExistingEntry(t *testing.T) {
	t.Parallel()

	env := []string{"A=1", "RAIJIN_FORCE_ANSI=0"}
	got := upsertEnv(env, "RAIJIN_FORCE_ANSI", "1")
	if len(got) != 2 {
		t.Fatalf("len(got) = %d, want 2", len(got))
	}
	if got[1] != "RAIJIN_FORCE_ANSI=1" {
		t.Fatalf("got[1] = %q, want %q", got[1], "RAIJIN_FORCE_ANSI=1")
	}
}

func TestBuildImplementationPromptExplainsFeedbackLifecycle(t *testing.T) {
	t.Parallel()

	prompt := buildImplementationPrompt(State{
		Goal: "fix the loop",
	}, "Harness verification failed:\ngo test ./...")

	if !strings.Contains(prompt, "If .raijin/ralph/feedback.md exists, read it before choosing work.") {
		t.Fatalf("implementation prompt missing feedback-read guidance: %q", prompt)
	}
	if !strings.Contains(prompt, "Harness feedback from the previous iteration:") {
		t.Fatalf("implementation prompt missing harness-feedback section: %q", prompt)
	}
	if !strings.Contains(prompt, "Harness verification failed:\ngo test ./...") {
		t.Fatalf("implementation prompt missing interpolated harness feedback: %q", prompt)
	}
	if !strings.Contains(prompt, "Do not opportunistically start a second unchecked task") {
		t.Fatalf("implementation prompt missing single-task guardrail: %q", prompt)
	}
	if !strings.Contains(prompt, "If you leave unfinished wiring, partial integration, follow-up edge cases, or any other technical handoff") {
		t.Fatalf("implementation prompt missing unfinished-wiring guidance: %q", prompt)
	}
	if !strings.Contains(prompt, "If it is still useful for the next fresh iteration, rewrite it as a short actionable note.") {
		t.Fatalf("implementation prompt missing feedback-rewrite guidance: %q", prompt)
	}
	if !strings.Contains(prompt, "If no feedback is needed, do not keep the file around.") {
		t.Fatalf("implementation prompt missing feedback cleanup guidance: %q", prompt)
	}
}

func TestHarnessFeedbackLifecycleUsesSeparateFile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	feedbackPath := filepath.Join(dir, "feedback.md")
	harnessPath := filepath.Join(dir, "harness_feedback.md")
	manual := "Agent note: unfinished wiring in internal/oneshot still needs final integration.\n"
	if err := os.WriteFile(feedbackPath, []byte(manual), 0o644); err != nil {
		t.Fatalf("write manual feedback: %v", err)
	}

	if err := writeHarnessFeedback(harnessPath, "Harness verification failed:\ngo test ./..."); err != nil {
		t.Fatalf("writeHarnessFeedback: %v", err)
	}

	if got := readOptionalFile(feedbackPath); strings.TrimSpace(got) != strings.TrimSpace(manual) {
		t.Fatalf("manual feedback should be untouched, got %q want %q", got, manual)
	}
	if got := readOptionalFile(harnessPath); !strings.Contains(got, "Harness verification failed:\ngo test ./...") {
		t.Fatalf("expected harness feedback content, got %q", got)
	}

	if err := clearHarnessFeedback(harnessPath); err != nil {
		t.Fatalf("clearHarnessFeedback: %v", err)
	}

	if !fileExists(feedbackPath) {
		t.Fatalf("manual feedback file should remain")
	}
	if fileExists(harnessPath) {
		t.Fatalf("harness feedback file should be removed")
	}
}

func TestRalphPromptsDescribeFreshEphemeralRunsWithoutHistoryWarnings(t *testing.T) {
	t.Parallel()

	planningPrompt := buildPlanningPrompt(State{
		Goal: "plan the work",
	}, "revise item 3")
	implementationPrompt := buildImplementationPrompt(State{
		Goal: "implement the work",
	}, "")

	if !strings.Contains(planningPrompt, "This is a fresh ephemeral run.") {
		t.Fatalf("planning prompt missing ephemeral guidance: %q", planningPrompt)
	}
	if !strings.Contains(planningPrompt, "small enough that one implementation iteration can plausibly complete it") {
		t.Fatalf("planning prompt missing iteration-sized task guidance: %q", planningPrompt)
	}
	if strings.Contains(planningPrompt, "conversation history") {
		t.Fatalf("planning prompt should not mention conversation history: %q", planningPrompt)
	}

	if !strings.Contains(implementationPrompt, "This is a fresh ephemeral run.") {
		t.Fatalf("implementation prompt missing ephemeral guidance: %q", implementationPrompt)
	}
	if !strings.Contains(implementationPrompt, "Harness feedback from the previous iteration:\n(none)") {
		t.Fatalf("implementation prompt should render an empty harness section: %q", implementationPrompt)
	}
	if strings.Contains(implementationPrompt, "conversation state") || strings.Contains(implementationPrompt, "chat history") {
		t.Fatalf("implementation prompt should not mention prior chat state: %q", implementationPrompt)
	}
}
