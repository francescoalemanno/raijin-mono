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

func assertContainsAll(t *testing.T, got string, want ...string) {
	t.Helper()
	for _, needle := range want {
		if !strings.Contains(got, needle) {
			t.Fatalf("expected %q to contain %q", got, needle)
		}
	}
}

func TestReadSnapshotReturnsSpecAndProgress(t *testing.T) {
	repo := t.TempDir()
	pair := writeNamedSpecPair(t, repo, "otter-thread-sage", testSpecContent("Ship the builder"), "working\n"+promiseDone+"\n")

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
	if !strings.Contains(got.Progress, promiseDone) {
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

	got, found, err = ResolveSpecSelection(context.Background(), repo, "custom.md")
	if err != nil {
		t.Fatalf("ResolveSpecSelection(missing custom path): %v", err)
	}
	if !found {
		t.Fatalf("ResolveSpecSelection(missing custom path) found = false, want true")
	}
	if got.SpecPath != filepath.Join(repo, "custom.md") {
		t.Fatalf("SpecPath = %q, want %q", got.SpecPath, filepath.Join(repo, "custom.md"))
	}
	if got.ProgressPath != filepath.Join(repo, "custom.progress.txt") {
		t.Fatalf("ProgressPath = %q, want %q", got.ProgressPath, filepath.Join(repo, "custom.progress.txt"))
	}
}

func TestInspectPlanningStateUsesProgressPromiseMarker(t *testing.T) {
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

	writeProgressFile(t, pair.ProgressPath, "still working\n"+promiseContinue+"\n")
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

	writeProgressFile(t, pair.ProgressPath, "done\n"+promiseDone+"\n")
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

func TestReadRequiredPromiseMarkerRequiresFinalExactLine(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    string
		err     string
	}{
		{
			name:    "done",
			content: "work complete\n" + promiseDone + "\n",
			want:    promiseDone,
		},
		{
			name:    "continue",
			content: "more work\n" + promiseContinue + "\n",
			want:    promiseContinue,
		},
		{
			name:    "missing",
			content: "more work\n",
			err:     "missing promise marker",
		},
		{
			name:    "invalid inline",
			content: "more work <promise>DONE</promise>\n",
			err:     "invalid promise marker",
		},
		{
			name:    "not final line",
			content: promiseDone + "\nextra\n",
			err:     "final non-empty line",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			promise, err := readRequiredPromiseMarker(tc.content, "final builder response")
			if tc.err != "" {
				if err == nil || !strings.Contains(err.Error(), tc.err) {
					t.Fatalf("err = %v, want substring %q", err, tc.err)
				}
				return
			}
			if err != nil {
				t.Fatalf("readRequiredPromiseMarker: %v", err)
			}
			if promise != tc.want {
				t.Fatalf("promise = %q, want %q", promise, tc.want)
			}
		})
	}
}

func TestClearProgressPromiseMarkersRemovesExactMarkers(t *testing.T) {
	repo := t.TempDir()
	progressPath := filepath.Join(repo, ".raijin", "ralph", "progress-otter-thread-sage.txt")
	writeProgressFile(t, progressPath, "keep me\n"+promiseContinue+"\nand me\n"+planningPromiseContinue+"\n"+promiseDone+"\n")

	if err := clearProgressPromiseMarkers(progressPath); err != nil {
		t.Fatalf("clearProgressPromiseMarkers: %v", err)
	}

	got := readOptionalFile(progressPath)
	if strings.Contains(got, "<promise>") || strings.Contains(got, "<plan-promise>") {
		t.Fatalf("progress still contains promise marker: %q", got)
	}
	if !strings.Contains(got, "keep me") || !strings.Contains(got, "and me") {
		t.Fatalf("progress lost non-promise content: %q", got)
	}
}

func TestAppendProgressDonePromiseIsIdempotent(t *testing.T) {
	repo := t.TempDir()
	progressPath := filepath.Join(repo, ".raijin", "ralph", "progress-otter-thread-sage.txt")
	writeProgressFile(t, progressPath, "final validation passed\n")

	if err := appendProgressDonePromise(progressPath); err != nil {
		t.Fatalf("appendProgressDonePromise(first): %v", err)
	}
	if err := appendProgressDonePromise(progressPath); err != nil {
		t.Fatalf("appendProgressDonePromise(second): %v", err)
	}

	got := readOptionalFile(progressPath)
	if strings.Count(got, promiseDone) != 1 {
		t.Fatalf("progress = %q, want exactly one done marker", got)
	}
}

func TestRunPlanCreatesNamedSpecAndInitializesProgress(t *testing.T) {
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
		assertContainsAll(t, prompt,
			"# Goal",
			"# User Specification",
			"# Plan",
			"builder-facing mutable state",
			"one highest-leverage planning task",
			"question tool",
			"ask 1-3 focused clarifying questions",
			"Planning mode only",
			planningPromiseDone,
			planningPromiseContinue,
		)
		if strings.Contains(prompt, "plan.md") {
			t.Fatalf("planning prompt should not reference legacy plan.md: %q", prompt)
		}
		writeSpecFile(t, filepath.Join(repo, ".raijin", "ralph", "spec-otter-thread-sage.md"), testSpecContent("Design the loop"))
		writeProgressFile(t, filepath.Join(repo, ".raijin", "ralph", "progress-otter-thread-sage.txt"), "Initial task breakdown\n- Implement the loop scaffold\n")
		inject, ok, err := opts.OnCompleteHook(context.Background(), libagent.NewAssistantMessage("done", "", nil, time.Now()), nil)
		if err != nil {
			t.Fatalf("planning onCompleteHook: %v", err)
		}
		if ok {
			t.Fatalf("planning onCompleteHook without promise ok = true, want false")
		}
		if !strings.Contains(inject, planningPromiseDone) || !strings.Contains(inject, planningPromiseContinue) {
			t.Fatalf("planning inject = %q, want planning promise reminder", inject)
		}
		inject, ok, err = opts.OnCompleteHook(context.Background(), libagent.NewAssistantMessage("done\n"+planningPromiseDone, "", nil, time.Now()), nil)
		if err != nil {
			t.Fatalf("planning onCompleteHook corrected: %v", err)
		}
		if !ok || inject != "" {
			t.Fatalf("planning onCompleteHook corrected = %q, %v, want accept", inject, ok)
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
	if !fileExists(filepath.Join(repo, ".raijin", "ralph", "progress-otter-thread-sage.txt")) {
		t.Fatalf("progress file should be initialized during planning")
	}
}

func TestRunPlanContinuesUntilPlanningPromiseDone(t *testing.T) {
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

	callCount := 0
	runEphemeralPrompt = func(_ context.Context, repoRoot string, opts EphemeralPromptOptions, _, _ io.Writer) (promptResult, error) {
		callCount++
		if repoRoot != repo {
			t.Fatalf("repoRoot = %q, want %q", repoRoot, repo)
		}
		if opts.OnCompleteHook == nil {
			t.Fatalf("planning prompt should set onCompleteHook")
		}
		if len(opts.ExtraTools) != 1 || opts.ExtraTools[0].Info().Name != "question" {
			t.Fatalf("expected planning question tool, got %#v", opts.ExtraTools)
		}

		writeSpecFile(t, filepath.Join(repo, ".raijin", "ralph", "spec-otter-thread-sage.md"), testSpecContent("Design the loop"))
		switch callCount {
		case 1:
			writeProgressFile(t, filepath.Join(repo, ".raijin", "ralph", "progress-otter-thread-sage.txt"), "Initial task breakdown\n- Audit command surface\n")
			inject, ok, err := opts.OnCompleteHook(context.Background(), libagent.NewAssistantMessage("iteration one\n"+planningPromiseContinue, "", nil, time.Now()), nil)
			if err != nil {
				t.Fatalf("planning onCompleteHook(iteration one): %v", err)
			}
			if !ok || inject != "" {
				t.Fatalf("planning onCompleteHook(iteration one) = %q, %v, want accept", inject, ok)
			}
		case 2:
			writeProgressFile(t, filepath.Join(repo, ".raijin", "ralph", "progress-otter-thread-sage.txt"), "Builder-ready task breakdown\n- Implement command surface\n- Verify help output\n")
			inject, ok, err := opts.OnCompleteHook(context.Background(), libagent.NewAssistantMessage("iteration two\n"+planningPromiseDone, "", nil, time.Now()), nil)
			if err != nil {
				t.Fatalf("planning onCompleteHook(iteration two): %v", err)
			}
			if !ok || inject != "" {
				t.Fatalf("planning onCompleteHook(iteration two) = %q, %v, want accept", inject, ok)
			}
		default:
			t.Fatalf("unexpected planning iteration %d", callCount)
		}
		return promptResult{Stdout: "planned\n"}, nil
	}

	if err := Run(context.Background(), Options{
		PlanningRequest: "design the loop",
		Mode:            ModePlan,
		RepoRoot:        repo,
	}); err != nil {
		t.Fatalf("Run(plan loop): %v", err)
	}

	if callCount != 2 {
		t.Fatalf("callCount = %d, want 2", callCount)
	}
}

func TestPlanningArtifactsChangedHookReinjectsWhenSpecOrProgressDidNotChange(t *testing.T) {
	repo := t.TempDir()
	specPath := filepath.Join(repo, ".raijin", "ralph", "spec-otter-thread-sage.md")
	progressPath := filepath.Join(repo, ".raijin", "ralph", "progress-otter-thread-sage.txt")
	initialSpec := testSpecContent("Initial spec")
	writeSpecFile(t, specPath, initialSpec)
	initialProgress := "Initial tasks\n- First step\n"
	writeProgressFile(t, progressPath, initialProgress)

	hook := planningArtifactsChangedHook(repo, specPath, progressPath, readOptionalFile(specPath), readOptionalFile(progressPath))
	inject, ok, err := hook(context.Background(), libagent.NewAssistantMessage("done", "", nil, time.Now()), nil)
	if err != nil {
		t.Fatalf("hook unchanged: %v", err)
	}
	if ok {
		t.Fatalf("hook unchanged ok = true, want false")
	}
	if !strings.Contains(inject, "did not leave any durable change behind") {
		t.Fatalf("inject = %q, want durable-change reminder", inject)
	}
	if !strings.Contains(inject, ".raijin/ralph/spec-otter-thread-sage.md") {
		t.Fatalf("inject = %q, want spec path", inject)
	}

	writeSpecFile(t, specPath, testSpecContent("Updated spec"))
	inject, ok, err = hook(context.Background(), libagent.NewAssistantMessage("done", "", nil, time.Now()), nil)
	if err != nil {
		t.Fatalf("hook changed spec: %v", err)
	}
	if !ok || inject != "" {
		t.Fatalf("hook changed spec = %q, %v, want accept", inject, ok)
	}

	writeProgressFile(t, progressPath, "Initialized builder tasks\n- First implementation slice\n")
	inject, ok, err = hook(context.Background(), libagent.NewAssistantMessage("done", "", nil, time.Now()), nil)
	if err != nil {
		t.Fatalf("hook changed: %v", err)
	}
	if !ok || inject != "" {
		t.Fatalf("hook changed = %q, %v, want accept", inject, ok)
	}
}

func TestPlanningPromiseHookRequiresPlanningPromiseMarker(t *testing.T) {
	repo := t.TempDir()
	specPath := filepath.Join(repo, ".raijin", "ralph", "spec-otter-thread-sage.md")
	progressPath := filepath.Join(repo, ".raijin", "ralph", "progress-otter-thread-sage.txt")
	writeSpecFile(t, specPath, testSpecContent("Initial spec"))
	writeProgressFile(t, progressPath, "Initial tasks\n- First step\n")

	initialSpec := readOptionalFile(specPath)
	initialProgress := readOptionalFile(progressPath)
	writeSpecFile(t, specPath, testSpecContent("Updated spec"))
	writeProgressFile(t, progressPath, "Builder-ready tasks\n- Implement it\n")

	accepted := ""
	hook := planningPromiseHook(repo, specPath, progressPath, initialSpec, initialProgress, &accepted)
	inject, ok, err := hook(context.Background(), libagent.NewAssistantMessage("done", "", nil, time.Now()), nil)
	if err != nil {
		t.Fatalf("hook missing promise: %v", err)
	}
	if ok {
		t.Fatalf("hook missing promise ok = true, want false")
	}
	if !strings.Contains(inject, planningPromiseDone) || !strings.Contains(inject, planningPromiseContinue) {
		t.Fatalf("inject = %q, want planning promise reminder", inject)
	}

	inject, ok, err = hook(context.Background(), libagent.NewAssistantMessage("done\n"+planningPromiseContinue, "", nil, time.Now()), nil)
	if err != nil {
		t.Fatalf("hook planning continue: %v", err)
	}
	if !ok || inject != "" {
		t.Fatalf("hook planning continue = %q, %v, want accept", inject, ok)
	}
	if accepted != planningPromiseContinue {
		t.Fatalf("accepted = %q, want %q", accepted, planningPromiseContinue)
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
		writeProgressFile(t, filepath.Join(repo, ".raijin", "ralph", "progress-otter-thread-sage.txt"), "Partial task breakdown\n- Clarify deployment target before implementation\n")
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
	progressPath := filepath.Join(repo, ".raijin", "ralph", "progress-otter-thread-sage.txt")
	if !fileExists(progressPath) {
		t.Fatalf("expected partial progress draft to remain on disk")
	}
	if !strings.Contains(readOptionalFile(progressPath), "Partial task breakdown") {
		t.Fatalf("partial progress draft was not preserved")
	}
}

func TestRunAutoUsesFinalResponsePromiseAndPersistsDoneFooter(t *testing.T) {
	repo := t.TempDir()
	pair := writeNamedSpecPair(t, repo, "otter-thread-sage", testSpecContent("Ship the refactor"), "stale note\n"+promiseDone+"\n")

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
		if strings.Contains(progressBefore, "<promise>") {
			t.Fatalf("promise marker should be cleared before iteration %d, got %q", callCount, progressBefore)
		}
		if !strings.Contains(prompt, promiseDone) || !strings.Contains(prompt, promiseContinue) {
			t.Fatalf("builder prompt missing promise instructions: %q", prompt)
		}
		assertContainsAll(t, prompt,
			"read-only durable input",
			"mutable working state",
			"one concrete highest-leverage open task",
			"Do only that one task this iteration",
			"Run the relevant checks",
			"Update .raijin/ralph/progress-otter-thread-sage.txt to reflect what changed",
			"End your final response with exactly one whole-line marker",
			"full specification is complete and verified",
		)
		if strings.Contains(prompt, "%!s(MISSING)") {
			t.Fatalf("builder prompt has missing interpolation: %q", prompt)
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
			if !strings.Contains(inject, "End your final response with exactly one whole-line promise marker") {
				t.Fatalf("inject = %q, want final-response promise reminder", inject)
			}
			if !strings.Contains(inject, "Finishing only the current task is not enough") {
				t.Fatalf("inject = %q, want conservative DONE reminder", inject)
			}
			inject, ok, err = onCompleteHook(context.Background(), libagent.NewAssistantMessage("iteration one\n"+promiseContinue, "", nil, time.Now()), nil)
			if err != nil {
				t.Fatalf("onCompleteHook(iteration one corrected): %v", err)
			}
			if !ok || inject != "" {
				t.Fatalf("onCompleteHook(iteration one corrected) = %q, %v", inject, ok)
			}
		case 2:
			writeProgressFile(t, pair.ProgressPath, "final validation passed\n")
			inject, ok, err := onCompleteHook(context.Background(), libagent.NewAssistantMessage("iteration two\n"+promiseDone, "", nil, time.Now()), nil)
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
	finalProgress := readOptionalFile(pair.ProgressPath)
	if strings.Count(finalProgress, promiseDone) != 1 {
		t.Fatalf("final progress = %q, want exactly one persisted done marker", finalProgress)
	}
	if strings.Contains(finalProgress, promiseContinue) {
		t.Fatalf("final progress = %q, should not persist continue marker", finalProgress)
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
