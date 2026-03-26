package ralph

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	libagent "github.com/francescoalemanno/raijin-mono/libagent"
)

func writeSpecFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(path, []byte(strings.TrimSpace(content)+"\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(%s): %v", path, err)
	}
}

func writeProgressFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile(%s): %v", path, err)
	}
}

func writeNamedSpecPair(t *testing.T, repo, slug, spec, progress string) SpecPair {
	t.Helper()
	pair := SpecPair{
		SpecPath:     filepath.Join(repo, ".raijin", "ralph", "spec-"+slug+".md"),
		ProgressPath: filepath.Join(repo, ".raijin", "ralph", "progress-"+slug+".txt"),
		Slug:         slug,
	}
	writeSpecFile(t, pair.SpecPath, spec)
	if progress != "" {
		writeProgressFile(t, pair.ProgressPath, progress)
	}
	return pair
}

func testSpecContent(title string) string {
	return "# Goal\n\n" + title + "\n\n# User Specification\n\nKeep changes narrow.\n\n# Plan\n\n1. Do the work.\n"
}

func TestReadSnapshotReturnsSpecAndProgress(t *testing.T) {
	repo := t.TempDir()
	pair := writeNamedSpecPair(t, repo, "otter-thread-sage", testSpecContent("Ship the builder"), "working\nPROMISE: CONTINUE\n")

	got, err := ReadSnapshot(context.Background(), repo, pair.SpecPath)
	if err != nil {
		t.Fatalf("ReadSnapshot: %v", err)
	}
	if got.RepoRoot != repo {
		t.Fatalf("RepoRoot = %q, want %q", got.RepoRoot, repo)
	}
	if got.SpecPath != pair.SpecPath {
		t.Fatalf("SpecPath = %q, want %q", got.SpecPath, pair.SpecPath)
	}
	if got.ProgressPath != pair.ProgressPath {
		t.Fatalf("ProgressPath = %q, want %q", got.ProgressPath, pair.ProgressPath)
	}
	if !strings.Contains(got.Spec, "# Goal") {
		t.Fatalf("Spec = %q, want goal section", got.Spec)
	}
	if !strings.Contains(got.Progress, promiseContinue) {
		t.Fatalf("Progress = %q, want promise", got.Progress)
	}
}

func TestResolveSpecSelectionSupportsSlugPathAndCustomSpecPaths(t *testing.T) {
	repo := t.TempDir()
	pair := writeNamedSpecPair(t, repo, "otter-thread-sage", testSpecContent("Named spec"), "")

	got, found, err := ResolveSpecSelection(context.Background(), repo, pair.Slug)
	if err != nil {
		t.Fatalf("ResolveSpecSelection(slug): %v", err)
	}
	if !found || got.SpecPath != pair.SpecPath || got.ProgressPath != pair.ProgressPath {
		t.Fatalf("ResolveSpecSelection(slug) = %#v, %v", got, found)
	}

	got, found, err = ResolveSpecSelection(context.Background(), repo, pair.SpecPath)
	if err != nil {
		t.Fatalf("ResolveSpecSelection(path): %v", err)
	}
	if !found || got.SpecPath != pair.SpecPath {
		t.Fatalf("ResolveSpecSelection(path) = %#v, %v", got, found)
	}

	customSpec := filepath.Join(repo, "docs", "feature-spec.md")
	writeSpecFile(t, customSpec, testSpecContent("External spec"))
	got, found, err = ResolveSpecSelection(context.Background(), repo, customSpec)
	if err != nil {
		t.Fatalf("ResolveSpecSelection(custom path): %v", err)
	}
	if !found {
		t.Fatalf("ResolveSpecSelection(custom path) found = false, want true")
	}
	wantProgress := filepath.Join(repo, "docs", "feature-spec.progress.txt")
	if got.ProgressPath != wantProgress {
		t.Fatalf("ProgressPath = %q, want %q", got.ProgressPath, wantProgress)
	}
}

func TestInspectPlanningStateUsesProgressPromise(t *testing.T) {
	repo := t.TempDir()

	status, err := InspectPlanningState(context.Background(), repo, "")
	if err != nil {
		t.Fatalf("InspectPlanningState(empty): %v", err)
	}
	if status.State != PlanningStateEmpty {
		t.Fatalf("state = %q, want empty", status.State)
	}

	pair := writeNamedSpecPair(t, repo, "otter-thread-sage", testSpecContent("Implement loop"), "")

	status, err = InspectPlanningState(context.Background(), repo, pair.SpecPath)
	if err != nil {
		t.Fatalf("InspectPlanningState(spec only): %v", err)
	}
	if status.State != PlanningStatePlanned {
		t.Fatalf("state = %q, want planned", status.State)
	}

	writeProgressFile(t, pair.ProgressPath, "still working\nPROMISE: CONTINUE\n")
	status, err = InspectPlanningState(context.Background(), repo, pair.SpecPath)
	if err != nil {
		t.Fatalf("InspectPlanningState(continue): %v", err)
	}
	if status.State != PlanningStatePlanned {
		t.Fatalf("state = %q, want planned", status.State)
	}

	writeProgressFile(t, pair.ProgressPath, "still working with no explicit promise\n")
	status, err = InspectPlanningState(context.Background(), repo, pair.SpecPath)
	if err != nil {
		t.Fatalf("InspectPlanningState(missing promise): %v", err)
	}
	if status.State != PlanningStatePlanned {
		t.Fatalf("state = %q, want planned when promise is missing", status.State)
	}

	writeProgressFile(t, pair.ProgressPath, "done\nPROMISE: DONE\n")
	status, err = InspectPlanningState(context.Background(), repo, pair.SpecPath)
	if err != nil {
		t.Fatalf("InspectPlanningState(done): %v", err)
	}
	if status.State != PlanningStateCompleted {
		t.Fatalf("state = %q, want completed", status.State)
	}

	hasState, err := HasPlanningState(context.Background(), repo, pair.SpecPath)
	if err != nil {
		t.Fatalf("HasPlanningState: %v", err)
	}
	if !hasState {
		t.Fatalf("HasPlanningState = false, want true")
	}
}

func TestReadProgressPromiseAcceptsNonAlphabeticPrefix(t *testing.T) {
	repo := t.TempDir()
	progressPath := filepath.Join(repo, ".raijin", "ralph", "progress-otter-thread-sage.txt")
	writeProgressFile(t, progressPath, "working\n> PROMISE: DONE\n")

	promise, err := readProgressPromise(progressPath)
	if err != nil {
		t.Fatalf("readProgressPromise(prefixed): %v", err)
	}
	if promise != promiseDone {
		t.Fatalf("promise = %q, want %q", promise, promiseDone)
	}
}

func TestClearPromiseLinesRemovesNonAlphabeticPrefixPromise(t *testing.T) {
	repo := t.TempDir()
	progressPath := filepath.Join(repo, ".raijin", "ralph", "progress-otter-thread-sage.txt")
	writeProgressFile(t, progressPath, "keep me\n> PROMISE: CONTINUE\nand me\n")

	if err := clearPromiseLines(progressPath); err != nil {
		t.Fatalf("clearPromiseLines: %v", err)
	}

	got := readOptionalFile(progressPath)
	if strings.Contains(got, "PROMISE:") {
		t.Fatalf("progress still contains promise line: %q", got)
	}
	if !strings.Contains(got, "keep me") || !strings.Contains(got, "and me") {
		t.Fatalf("progress lost non-promise content: %q", got)
	}
}

func TestRunPlanCreatesNamedSpecWithoutProgress(t *testing.T) {
	repo := t.TempDir()

	origPrompt := runEphemeralPrompt
	origSlug := generateSpecSlug
	origAsk := askPlanningQuestion
	t.Cleanup(func() {
		runEphemeralPrompt = origPrompt
		generateSpecSlug = origSlug
		askPlanningQuestion = origAsk
	})

	generateSpecSlug = func() string { return "otter-thread-sage" }
	askPlanningQuestion = func(context.Context, PlanningQuestionPrompt) (string, error) {
		return "answer", nil
	}
	runEphemeralPrompt = func(_ context.Context, repoRoot string, opts EphemeralPromptOptions, _, _ io.Writer) (promptResult, error) {
		prompt := opts.Prompt
		if repoRoot != repo {
			t.Fatalf("repoRoot = %q, want %q", repoRoot, repo)
		}
		if opts.OnCompleteHook == nil {
			t.Fatalf("planning prompt should set onCompleteHook")
		}
		if len(opts.ExtraTools) != 1 {
			t.Fatalf("planning prompt should inject exactly one extra tool, got %d", len(opts.ExtraTools))
		}
		if opts.ExtraTools[0].Info().Name != "question" {
			t.Fatalf("planning extra tool = %q, want question", opts.ExtraTools[0].Info().Name)
		}
		if !strings.Contains(prompt, ".raijin/ralph/spec-otter-thread-sage.md") {
			t.Fatalf("planning prompt missing spec path: %q", prompt)
		}
		if !strings.Contains(prompt, ".raijin/ralph/progress-otter-thread-sage.txt") {
			t.Fatalf("planning prompt missing progress path: %q", prompt)
		}
		if !strings.Contains(prompt, "must not create or modify .raijin/ralph/progress-otter-thread-sage.txt") {
			t.Fatalf("planning prompt should forbid progress writes: %q", prompt)
		}
		if !strings.Contains(prompt, "ask clarifying questions instead of guessing") {
			t.Fatalf("planning prompt should require clarifications: %q", prompt)
		}
		if !strings.Contains(prompt, "Ask only 1-3 focused high-leverage questions") {
			t.Fatalf("planning prompt should limit question batches: %q", prompt)
		}
		if !strings.Contains(prompt, "do not write interview transcript") {
			t.Fatalf("planning prompt should forbid interview transcript state: %q", prompt)
		}
		if strings.Contains(prompt, "plan.md") {
			t.Fatalf("planning prompt should not reference legacy plan.md: %q", prompt)
		}
		writeSpecFile(t, filepath.Join(repo, ".raijin", "ralph", "spec-otter-thread-sage.md"), testSpecContent("Design the loop"))
		inject, ok, err := opts.OnCompleteHook(context.Background(), libagent.NewAssistantMessage("done", "", nil, time.Now()), nil)
		if err != nil {
			t.Fatalf("planning onCompleteHook: %v", err)
		}
		if !ok || inject != "" {
			t.Fatalf("planning onCompleteHook = %q, %v, want accept", inject, ok)
		}
		return promptResult{Stdout: "planned\n"}, nil
	}

	if err := Run(context.Background(), Options{
		PlanningRequest: "design the loop",
		Mode:            ModePlan,
		RepoRoot:        repo,
	}); err != nil {
		t.Fatalf("Run(plan): %v", err)
	}

	if !fileExists(filepath.Join(repo, ".raijin", "ralph", "spec-otter-thread-sage.md")) {
		t.Fatalf("expected spec file to be created")
	}
	if fileExists(filepath.Join(repo, ".raijin", "ralph", "progress-otter-thread-sage.txt")) {
		t.Fatalf("progress file should not be created during planning")
	}
}

func TestPlanningSpecChangedHookReinjectsWhenSpecDidNotChange(t *testing.T) {
	repo := t.TempDir()
	specPath := filepath.Join(repo, ".raijin", "ralph", "spec-otter-thread-sage.md")
	initialSpec := testSpecContent("Initial spec")
	writeSpecFile(t, specPath, initialSpec)

	hook := planningSpecChangedHook(repo, specPath, readOptionalFile(specPath))
	inject, ok, err := hook(context.Background(), libagent.NewAssistantMessage("done", "", nil, time.Now()), nil)
	if err != nil {
		t.Fatalf("hook unchanged: %v", err)
	}
	if ok {
		t.Fatalf("hook unchanged ok = true, want false")
	}
	if !strings.Contains(inject, "bidirectionally create or revise the durable Ralph specification with the user") {
		t.Fatalf("inject = %q, want bidirectional reminder", inject)
	}
	if !strings.Contains(inject, ".raijin/ralph/spec-otter-thread-sage.md") {
		t.Fatalf("inject = %q, want spec path", inject)
	}

	writeSpecFile(t, specPath, testSpecContent("Updated spec"))
	inject, ok, err = hook(context.Background(), libagent.NewAssistantMessage("done", "", nil, time.Now()), nil)
	if err != nil {
		t.Fatalf("hook changed: %v", err)
	}
	if !ok || inject != "" {
		t.Fatalf("hook changed = %q, %v, want accept", inject, ok)
	}
}

func TestPlanningQuestionToolValidatesAndReturnsAnswer(t *testing.T) {
	origAsk := askPlanningQuestion
	t.Cleanup(func() { askPlanningQuestion = origAsk })

	askPlanningQuestion = func(_ context.Context, prompt PlanningQuestionPrompt) (string, error) {
		if prompt.Question != "Which baseline?" {
			t.Fatalf("Question = %q", prompt.Question)
		}
		if len(prompt.Options) != 2 {
			t.Fatalf("len(Options) = %d, want 2", len(prompt.Options))
		}
		if prompt.Options[0].Label != "CLI-first" || prompt.Options[1].Label != "Library-first" {
			t.Fatalf("unexpected options = %#v", prompt.Options)
		}
		return "CLI-first", nil
	}

	tool, err := newPlanningQuestionTool()
	if err != nil {
		t.Fatalf("newPlanningQuestionTool: %v", err)
	}

	resp, err := tool.Run(context.Background(), libagent.ToolCall{
		Name:  "question",
		Input: `{"question":"Which baseline?","options":[{"label":"CLI-first","description":"Keep the CLI as the main path."},{"label":"Library-first","description":"Optimize for embedded use."}]}`,
	})
	if err != nil {
		t.Fatalf("tool.Run(valid): %v", err)
	}
	if resp.IsError {
		t.Fatalf("tool response unexpectedly errored: %q", resp.Content)
	}
	if strings.TrimSpace(resp.Content) != "CLI-first" {
		t.Fatalf("resp.Content = %q, want %q", resp.Content, "CLI-first")
	}

	for _, tc := range []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "empty question",
			input: `{"question":"   ","options":[{"label":"A","description":"alpha"}]}`,
			want:  "question is required",
		},
		{
			name:  "no options",
			input: `{"question":"Which?","options":[]}`,
			want:  "at least one option is required",
		},
		{
			name:  "too many options",
			input: `{"question":"Which?","options":[{"label":"A"},{"label":"B"},{"label":"C"},{"label":"D"}]}`,
			want:  "at most three options are allowed",
		},
		{
			name:  "empty label",
			input: `{"question":"Which?","options":[{"label":" ","description":"alpha"}]}`,
			want:  "option labels must not be empty",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			resp, err := tool.Run(context.Background(), libagent.ToolCall{Name: "question", Input: tc.input})
			if err != nil {
				t.Fatalf("tool.Run(%s): %v", tc.name, err)
			}
			if !resp.IsError {
				t.Fatalf("tool.Run(%s) IsError = false, want true", tc.name)
			}
			if !strings.Contains(resp.Content, tc.want) {
				t.Fatalf("tool.Run(%s) Content = %q, want substring %q", tc.name, resp.Content, tc.want)
			}
		})
	}
}

func TestRunPlanKeepsPartialDraftWhenClarificationIsCanceled(t *testing.T) {
	repo := t.TempDir()

	origPrompt := runEphemeralPrompt
	origSlug := generateSpecSlug
	origAsk := askPlanningQuestion
	t.Cleanup(func() {
		runEphemeralPrompt = origPrompt
		generateSpecSlug = origSlug
		askPlanningQuestion = origAsk
	})

	generateSpecSlug = func() string { return "otter-thread-sage" }
	askPlanningQuestion = func(context.Context, PlanningQuestionPrompt) (string, error) {
		return "", context.Canceled
	}
	runEphemeralPrompt = func(_ context.Context, repoRoot string, opts EphemeralPromptOptions, _, _ io.Writer) (promptResult, error) {
		if repoRoot != repo {
			t.Fatalf("repoRoot = %q, want %q", repoRoot, repo)
		}
		if len(opts.ExtraTools) != 1 || opts.ExtraTools[0].Info().Name != "question" {
			t.Fatalf("expected planning question tool, got %#v", opts.ExtraTools)
		}
		writeSpecFile(t, filepath.Join(repo, ".raijin", "ralph", "spec-otter-thread-sage.md"), "# Goal\n\nPartial draft\n\n# User Specification\n\nKnown facts only.\n\n# Plan\n\n1. Initial draft.\n")
		_, err := opts.ExtraTools[0].Run(context.Background(), libagent.ToolCall{
			Name:  "question",
			Input: `{"question":"Which deployment target matters most?","options":[{"label":"CLI","description":"Interactive users first."}]}`,
		})
		return promptResult{}, err
	}

	if err := Run(context.Background(), Options{
		PlanningRequest: "design the loop",
		Mode:            ModePlan,
		RepoRoot:        repo,
	}); err != nil {
		t.Fatalf("Run(plan cancel): %v", err)
	}

	specPath := filepath.Join(repo, ".raijin", "ralph", "spec-otter-thread-sage.md")
	if !fileExists(specPath) {
		t.Fatalf("expected partial spec draft to remain on disk")
	}
	if !strings.Contains(readOptionalFile(specPath), "Partial draft") {
		t.Fatalf("partial spec draft was not preserved")
	}
	if fileExists(filepath.Join(repo, ".raijin", "ralph", "progress-otter-thread-sage.txt")) {
		t.Fatalf("progress file should not be created during interrupted planning")
	}
}

func TestRunAutoClearsPromisesAndCompletesFromProgressFile(t *testing.T) {
	repo := t.TempDir()
	pair := writeNamedSpecPair(t, repo, "otter-thread-sage", testSpecContent("Ship the refactor"), "stale note\nPROMISE: CONTINUE\n")

	origPrompt := runEphemeralPrompt
	t.Cleanup(func() { runEphemeralPrompt = origPrompt })

	callCount := 0
	runEphemeralPrompt = func(_ context.Context, repoRoot string, opts EphemeralPromptOptions, _, _ io.Writer) (promptResult, error) {
		prompt := opts.Prompt
		onCompleteHook := opts.OnCompleteHook
		callCount++
		if repoRoot != repo {
			t.Fatalf("repoRoot = %q, want %q", repoRoot, repo)
		}
		if onCompleteHook == nil {
			t.Fatalf("builder run should set onCompleteHook")
		}
		if len(opts.ExtraTools) != 0 {
			t.Fatalf("builder run should not inject planning tools, got %d extra tools", len(opts.ExtraTools))
		}
		progressBefore := readOptionalFile(pair.ProgressPath)
		if strings.Contains(progressBefore, "PROMISE:") {
			t.Fatalf("promise line should be cleared before iteration %d, got %q", callCount, progressBefore)
		}
		if !strings.Contains(prompt, promiseDone) || !strings.Contains(prompt, promiseContinue) {
			t.Fatalf("builder prompt missing promise instructions: %q", prompt)
		}
		if !strings.Contains(prompt, "Update it surgically") {
			t.Fatalf("builder prompt missing surgical progress instruction: %q", prompt)
		}
		if !strings.Contains(prompt, "Do not wipe or drastically shrink the file") {
			t.Fatalf("builder prompt missing anti-destructive progress instruction: %q", prompt)
		}
		if !strings.Contains(prompt, "choose exactly one single most high-leverage task") {
			t.Fatalf("builder prompt missing high-leverage task instruction: %q", prompt)
		}
		if !strings.Contains(prompt, "Prefer the most important foundational item") {
			t.Fatalf("builder prompt missing foundational-priority instruction: %q", prompt)
		}
		if !strings.Contains(prompt, "Do that single chosen task") {
			t.Fatalf("builder prompt missing stop-after-one-task instruction: %q", prompt)
		}
		if strings.Contains(prompt, "%!s(MISSING)") {
			t.Fatalf("builder prompt has missing interpolation: %q", prompt)
		}
		if !strings.Contains(prompt, "update .raijin/ralph/progress-otter-thread-sage.txt to reflect the result") {
			t.Fatalf("builder prompt missing interpolated progress path in stop instruction: %q", prompt)
		}
		if !strings.Contains(prompt, "Finishing your single chosen task is not enough for PROMISE: DONE") {
			t.Fatalf("builder prompt missing conservative DONE rule: %q", prompt)
		}

		switch callCount {
		case 1:
			writeProgressFile(t, pair.ProgressPath, "working through task breakdown\n")
			inject, ok, err := onCompleteHook(context.Background(), libagent.NewAssistantMessage("iteration one", "", nil, time.Now()), nil)
			if err != nil {
				t.Fatalf("onCompleteHook(iteration one): %v", err)
			}
			if ok {
				t.Fatalf("onCompleteHook(iteration one) ok = true, want false")
			}
			if !strings.Contains(inject, promiseDone) || !strings.Contains(inject, promiseContinue) {
				t.Fatalf("inject = %q, want promise reminder", inject)
			}
			if !strings.Contains(inject, "Preserve still-relevant progress") {
				t.Fatalf("inject = %q, want progress-preservation reminder", inject)
			}
			if !strings.Contains(inject, "Finishing only the current task is not enough") {
				t.Fatalf("inject = %q, want conservative DONE reminder", inject)
			}
			writeProgressFile(t, pair.ProgressPath, "working through task breakdown\nPROMISE: CONTINUE\n")
		case 2:
			writeProgressFile(t, pair.ProgressPath, "final validation passed\nPROMISE: DONE\n")
			inject, ok, err := onCompleteHook(context.Background(), libagent.NewAssistantMessage("iteration two", "", nil, time.Now()), nil)
			if err != nil {
				t.Fatalf("onCompleteHook(iteration two): %v", err)
			}
			if !ok || inject != "" {
				t.Fatalf("onCompleteHook(iteration two) = %q, %v", inject, ok)
			}
		default:
			t.Fatalf("unexpected iteration %d", callCount)
		}
		return promptResult{}, nil
	}

	if err := Run(context.Background(), Options{
		Mode:          ModeAuto,
		RepoRoot:      repo,
		SpecPath:      pair.SpecPath,
		MaxIterations: 4,
	}); err != nil {
		t.Fatalf("Run(auto): %v", err)
	}

	if callCount != 2 {
		t.Fatalf("callCount = %d, want 2", callCount)
	}
	status, err := InspectPlanningState(context.Background(), repo, pair.SpecPath)
	if err != nil {
		t.Fatalf("InspectPlanningState(final): %v", err)
	}
	if status.State != PlanningStateCompleted {
		t.Fatalf("state = %q, want completed", status.State)
	}
	if fileExists(filepath.Join(repo, ".raijin", "ralph", "feedback.md")) {
		t.Fatalf("feedback.md should not exist")
	}
	if fileExists(filepath.Join(repo, ".raijin", "ralph", "harness_feedback.md")) {
		t.Fatalf("harness_feedback.md should not exist")
	}
	if fileExists(filepath.Join(repo, ".raijin", "ralph", "logs")) {
		t.Fatalf("logs directory should not exist")
	}
}
