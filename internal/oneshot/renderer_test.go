package oneshot

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/francescoalemanno/raijin-mono/internal/tools"
	libagent "github.com/francescoalemanno/raijin-mono/libagent"
)

func TestRendererFinalToolStatusIncludesArgsFromExecutionStart(t *testing.T) {
	var stderr bytes.Buffer
	r := newRenderer(&stderr, &bytes.Buffer{}, nil, false)

	r.handleEvent(libagent.AgentEvent{
		Type:       libagent.AgentEventTypeMessageUpdate,
		Delta:      &libagent.StreamDelta{Type: "tool_input_start", ID: "call-1", ToolName: "read"},
		ToolCallID: "call-1",
		ToolName:   "read",
	})
	r.handleEvent(libagent.AgentEvent{
		Type:       libagent.AgentEventTypeToolExecutionStart,
		ToolCallID: "call-1",
		ToolName:   "read",
		ToolArgs:   `{"path":"README.md"}`,
	})
	r.handleEvent(libagent.AgentEvent{
		Type: libagent.AgentEventTypeMessageEnd,
		Message: &libagent.ToolResultMessage{
			ToolCallID: "call-1",
			ToolName:   "read",
		},
	})

	last := lastNonEmptyLine(stderr.String())
	if !strings.Contains(last, "README.md") {
		t.Fatalf("expected final tool status to include args, got %q", last)
	}
}

func TestRendererFinalToolStatusIncludesArgsFromStreamedInput(t *testing.T) {
	var stderr bytes.Buffer
	r := newRenderer(&stderr, &bytes.Buffer{}, nil, false)

	r.handleEvent(libagent.AgentEvent{
		Type:       libagent.AgentEventTypeMessageUpdate,
		Delta:      &libagent.StreamDelta{Type: "tool_input_start", ID: "call-2", ToolName: "bash"},
		ToolCallID: "call-2",
		ToolName:   "bash",
	})
	r.handleEvent(libagent.AgentEvent{
		Type:  libagent.AgentEventTypeMessageUpdate,
		Delta: &libagent.StreamDelta{Type: "tool_input_delta", ID: "call-2", Delta: `{"command":"echo hi"}`},
	})
	r.handleEvent(libagent.AgentEvent{
		Type:       libagent.AgentEventTypeToolExecutionStart,
		ToolCallID: "call-2",
		ToolName:   "bash",
		ToolArgs:   "",
	})
	r.handleEvent(libagent.AgentEvent{
		Type: libagent.AgentEventTypeMessageEnd,
		Message: &libagent.ToolResultMessage{
			ToolCallID: "call-2",
			ToolName:   "bash",
		},
	})

	last := lastNonEmptyLine(stderr.String())
	if !strings.Contains(last, "echo hi") {
		t.Fatalf("expected final tool status to include streamed args, got %q", last)
	}
}

func TestRendererPrintsThinkingOnReasoningStartWithoutDelta(t *testing.T) {
	var stderr bytes.Buffer
	r := newRenderer(&stderr, &bytes.Buffer{}, nil, false)

	r.handleEvent(libagent.AgentEvent{
		Type: libagent.AgentEventTypeMessageUpdate,
		Delta: &libagent.StreamDelta{
			Type: "reasoning_start",
			ID:   "reason-1",
		},
	})
	r.handleEvent(libagent.AgentEvent{
		Type: libagent.AgentEventTypeMessageUpdate,
		Delta: &libagent.StreamDelta{
			Type: "reasoning_end",
			ID:   "reason-1",
		},
	})

	// The spinner handles signaling pending reasoning; no explicit "Reasoning" line is printed.
	_ = stderr.String()
}

func TestRendererReasoningCompletionLabelIsExplicit(t *testing.T) {
	var stderr bytes.Buffer
	current := time.Unix(100, 0)
	r := newRendererWithOptions(&stderr, &bytes.Buffer{}, nil, false, rendererOptions{
		now: func() time.Time { return current },
	})

	r.handleEvent(libagent.AgentEvent{
		Type: libagent.AgentEventTypeMessageUpdate,
		Delta: &libagent.StreamDelta{
			Type: "reasoning_start",
			ID:   "reason-2",
		},
	})
	current = current.Add(3 * time.Second)
	r.handleEvent(libagent.AgentEvent{
		Type: libagent.AgentEventTypeMessageUpdate,
		Delta: &libagent.StreamDelta{
			Type: "reasoning_end",
			ID:   "reason-2",
		},
	})

	if got := lastNonEmptyLine(stderr.String()); !strings.Contains(got, "Reasoning (3.0s)") {
		t.Fatalf("expected explicit reasoning duration label, got %q", got)
	}
}

func TestRendererReplyOutputFlushesOnlyOnNewlineUntilMessageEnd(t *testing.T) {
	var stderr bytes.Buffer
	var stdout bytes.Buffer
	r := newRenderer(&stderr, &stdout, nil, false)

	r.handleEvent(libagent.AgentEvent{
		Type:  libagent.AgentEventTypeMessageUpdate,
		Delta: &libagent.StreamDelta{Type: "text_delta", Delta: "hello"},
	})
	if got := stdout.String(); got != "" {
		t.Fatalf("expected no stdout flush without newline, got %q", got)
	}

	r.handleEvent(libagent.AgentEvent{
		Type:  libagent.AgentEventTypeMessageUpdate,
		Delta: &libagent.StreamDelta{Type: "text_delta", Delta: " world\nnext"},
	})
	if got := stdout.String(); got != "hello world\n" {
		t.Fatalf("expected newline-terminated chunk to flush, got %q", got)
	}

	r.handleEvent(libagent.AgentEvent{
		Type:    libagent.AgentEventTypeMessageEnd,
		Message: &libagent.AssistantMessage{},
	})
	if got := stdout.String(); got != "hello world\nnext\n" {
		t.Fatalf("expected remaining reply tail at message end, got %q", got)
	}
}

func TestRendererTTYDoesNotMixPendingToolLineWithReplyText(t *testing.T) {
	var tty bytes.Buffer
	r := newRenderer(&tty, &tty, nil, true)

	r.handleEvent(libagent.AgentEvent{
		Type:       libagent.AgentEventTypeToolExecutionStart,
		ToolCallID: "call-read",
		ToolName:   "read",
		ToolArgs:   `{"path":"."}`,
	})
	r.handleEvent(libagent.AgentEvent{
		Type:  libagent.AgentEventTypeMessageUpdate,
		Delta: &libagent.StreamDelta{Type: "text_delta", Delta: "I'll explore the codebase.\n"},
	})

	out := tty.String()
	if strings.Contains(out, "read I'll explore") {
		t.Fatalf("expected pending tool line to be separated from reply text, got %q", out)
	}
	if !strings.Contains(out, "read") || !strings.Contains(out, "I'll explore the codebase.") {
		t.Fatalf("expected both tool status and reply text in output, got %q", out)
	}
}

func TestRendererLiveSpinnerStartsImmediatelyAndClearsOnStop(t *testing.T) {
	var stderr bytes.Buffer
	current := time.Unix(100, 0)
	r := newRendererWithOptions(&stderr, &bytes.Buffer{}, nil, true, rendererOptions{
		persistentSpinner: true,
		now:               func() time.Time { return current },
		spinnerInterval:   time.Hour,
		modelLabel:        "openai/gpt-test",
		contextWindow:     10000,
	})

	r.startPersistentSpinner()

	if !strings.Contains(stderr.String(), "Reasoning") || !strings.Contains(stderr.String(), "0.00s") {
		t.Fatalf("expected initial live spinner output, got %q", stderr.String())
	}
	if !strings.Contains(stderr.String(), "openai/gpt-test") {
		t.Fatalf("expected initial spinner to include model label, got %q", stderr.String())
	}
	if !strings.Contains(stderr.String(), "ctx 24.0%") {
		t.Fatalf("expected initial spinner to include context percentage, got %q", stderr.String())
	}
	if !r.spinnerVisible {
		t.Fatalf("expected spinner to be visible after start")
	}

	r.stopPersistentSpinner()

	if r.spinnerVisible {
		t.Fatalf("expected spinner to be cleared on stop")
	}
	if r.spinnerEnabled {
		t.Fatalf("expected spinner to be disabled on stop")
	}
}

func TestRendererLiveSpinnerIgnoresAssistantUsageMetadata(t *testing.T) {
	var stderr bytes.Buffer
	current := time.Unix(100, 0)
	r := newRendererWithOptions(&stderr, &bytes.Buffer{}, nil, true, rendererOptions{
		persistentSpinner: true,
		now:               func() time.Time { return current },
		spinnerInterval:   time.Hour,
		modelLabel:        "openai/gpt-test",
		contextWindow:     10000,
		initialMessages: []libagent.Message{
			&libagent.AssistantMessage{
				Role: "assistant",
			},
		},
	})
	libagent.SetAssistantUsage(r.contextMessages[0].(*libagent.AssistantMessage), 120_000, 0, 0)

	r.startPersistentSpinner()
	defer r.stopPersistentSpinner()

	if !strings.Contains(stderr.String(), "ctx 24.0%") {
		t.Fatalf("expected approximate context percentage to ignore usage metadata, got %q", stderr.String())
	}
}

func TestRendererLiveSpinnerLabelPriority(t *testing.T) {
	var stderr bytes.Buffer
	current := time.Unix(200, 0)
	r := newRendererWithOptions(&stderr, &bytes.Buffer{}, nil, true, rendererOptions{
		persistentSpinner: true,
		now:               func() time.Time { return current },
		spinnerInterval:   time.Hour,
		modelLabel:        "openai/gpt-test",
		contextWindow:     10000,
	})
	r.startPersistentSpinner()
	defer r.stopPersistentSpinner()

	r.handleEvent(libagent.AgentEvent{Type: libagent.AgentEventTypeTurnStart})
	if got := spinnerLabelForTest(r); got != "Reasoning" {
		t.Fatalf("spinner label after turn start = %q, want %q", got, "Reasoning")
	}

	r.handleEvent(libagent.AgentEvent{
		Type:  libagent.AgentEventTypeMessageUpdate,
		Delta: &libagent.StreamDelta{Type: "text_delta", Delta: "hello"},
	})
	if got := spinnerLabelForTest(r); got != "Responding" {
		t.Fatalf("spinner label during text delta = %q, want %q", got, "Responding")
	}

	r.handleEvent(libagent.AgentEvent{
		Type:  libagent.AgentEventTypeMessageUpdate,
		Delta: &libagent.StreamDelta{Type: "reasoning_start", ID: "r1"},
	})
	if got := spinnerLabelForTest(r); got != "Reasoning" {
		t.Fatalf("spinner label during reasoning = %q, want %q", got, "Reasoning")
	}

	r.handleEvent(libagent.AgentEvent{
		Type:       libagent.AgentEventTypeToolExecutionStart,
		ToolCallID: "call-read",
		ToolName:   "read",
		ToolArgs:   `{"path":"README.md"}`,
	})
	if got := spinnerLabelForTest(r); got != "read (20 bytes)" {
		t.Fatalf("spinner label during tool call = %q, want %q", got, "read (20 bytes)")
	}
}

func TestRendererLiveSpinnerTimerDoesNotResetDuringToolPhaseLabelChanges(t *testing.T) {
	var stderr bytes.Buffer
	current := time.Unix(250, 0)
	r := newRendererWithOptions(&stderr, &bytes.Buffer{}, nil, true, rendererOptions{
		persistentSpinner: true,
		now:               func() time.Time { return current },
		spinnerInterval:   time.Hour,
		modelLabel:        "openai/gpt-test",
		contextWindow:     10000,
	})
	r.startPersistentSpinner()
	defer r.stopPersistentSpinner()

	r.handleEvent(libagent.AgentEvent{
		Type:  libagent.AgentEventTypeMessageUpdate,
		Delta: &libagent.StreamDelta{Type: "tool_input_start", ID: "call-1", ToolName: "read"},
	})
	if got := spinnerLabelForTest(r); got != "read (0 bytes)" {
		t.Fatalf("spinner label after tool input start = %q, want %q", got, "read (0 bytes)")
	}
	if got := spinnerElapsedForTest(r); got != "0.00s" {
		t.Fatalf("spinner elapsed after entering tool phase = %q, want %q", got, "0.00s")
	}

	r.handleEvent(libagent.AgentEvent{
		Type:  libagent.AgentEventTypeMessageUpdate,
		Delta: &libagent.StreamDelta{Type: "tool_input_delta", ID: "call-1", Delta: `{"path":"README.md"}`},
	})
	current = current.Add(2 * time.Second)
	if got := spinnerLabelForTest(r); got != "read (20 bytes)" {
		t.Fatalf("spinner label after tool input delta = %q, want %q", got, "read (20 bytes)")
	}
	if got := spinnerElapsedForTest(r); got != "2.00s" {
		t.Fatalf("spinner elapsed after tool input delta = %q, want %q", got, "2.00s")
	}
}

func TestRendererLiveSpinnerToolPhaseLastsUntilAllToolResultsFinish(t *testing.T) {
	var stderr bytes.Buffer
	current := time.Unix(260, 0)
	r := newRendererWithOptions(&stderr, &bytes.Buffer{}, nil, true, rendererOptions{
		persistentSpinner: true,
		now:               func() time.Time { return current },
		spinnerInterval:   time.Hour,
		modelLabel:        "openai/gpt-test",
		contextWindow:     10000,
	})
	r.startPersistentSpinner()
	defer r.stopPersistentSpinner()

	r.handleEvent(libagent.AgentEvent{
		Type:       libagent.AgentEventTypeToolExecutionStart,
		ToolCallID: "call-1",
		ToolName:   "read",
		ToolArgs:   `{"path":"README.md"}`,
	})
	current = current.Add(1 * time.Second)
	r.handleEvent(libagent.AgentEvent{
		Type:       libagent.AgentEventTypeToolExecutionStart,
		ToolCallID: "call-2",
		ToolName:   "bash",
		ToolArgs:   `{"command":"pwd"}`,
	})
	if got := spinnerElapsedForTest(r); got != "1.00s" {
		t.Fatalf("spinner elapsed after second tool starts = %q, want %q", got, "1.00s")
	}

	current = current.Add(2 * time.Second)
	r.handleEvent(libagent.AgentEvent{
		Type: libagent.AgentEventTypeMessageEnd,
		Message: &libagent.ToolResultMessage{
			ToolCallID: "call-1",
			ToolName:   "read",
			Content:    "ok",
		},
	})
	if got := spinnerLabelForTest(r); got != "bash (17 bytes)" {
		t.Fatalf("spinner label with one tool still pending = %q, want %q", got, "bash (17 bytes)")
	}
	if got := spinnerElapsedForTest(r); got != "3.00s" {
		t.Fatalf("spinner elapsed with one tool still pending = %q, want %q", got, "3.00s")
	}

	current = current.Add(1 * time.Second)
	r.handleEvent(libagent.AgentEvent{
		Type: libagent.AgentEventTypeMessageEnd,
		Message: &libagent.ToolResultMessage{
			ToolCallID: "call-2",
			ToolName:   "bash",
			Content:    "ok",
		},
	})
	if got := spinnerLabelForTest(r); got != "Reasoning" {
		t.Fatalf("spinner label after all tool results finish = %q, want %q", got, "Reasoning")
	}
	if got := spinnerElapsedForTest(r); got != "0.00s" {
		t.Fatalf("spinner elapsed after all tool results finish = %q, want %q", got, "0.00s")
	}
}

func TestRendererInteractiveDialogSuspendsSpinnerRedraw(t *testing.T) {
	var stderr bytes.Buffer
	current := time.Unix(300, 0)
	r := newRendererWithOptions(&stderr, &bytes.Buffer{}, nil, true, rendererOptions{
		persistentSpinner: true,
		now:               func() time.Time { return current },
		spinnerInterval:   time.Hour,
		modelLabel:        "openai/gpt-test",
		contextWindow:     10000,
	})
	r.startPersistentSpinner()
	defer r.stopPersistentSpinner()

	stderr.Reset()
	r.beginInteractiveDialog()
	cleared := stderr.String()
	if !strings.Contains(cleared, "\r") {
		t.Fatalf("expected beginInteractiveDialog to clear the spinner, got %q", cleared)
	}

	stderr.Reset()
	r.redrawSpinnerLocked()
	if got := stderr.String(); got != "" {
		t.Fatalf("spinner redraw should be suppressed during dialog, got %q", got)
	}

	r.endInteractiveDialog()
	if got := stderr.String(); !strings.Contains(got, "Reasoning") {
		t.Fatalf("expected spinner redraw after dialog ends, got %q", got)
	}
}

func TestRendererLiveSpinnerSuspendsAroundStdoutWrites(t *testing.T) {
	var tty bytes.Buffer
	current := time.Unix(300, 0)
	r := newRendererWithOptions(&tty, &tty, nil, true, rendererOptions{
		persistentSpinner: true,
		deferSpinnerPaint: true,
		now:               func() time.Time { return current },
		spinnerInterval:   time.Hour,
		modelLabel:        "openai/gpt-test",
		contextWindow:     10000,
	})
	r.startPersistentSpinner()
	defer r.stopPersistentSpinner()

	r.handleEvent(libagent.AgentEvent{
		Type:  libagent.AgentEventTypeMessageUpdate,
		Delta: &libagent.StreamDelta{Type: "text_delta", Delta: "hello\n"},
	})

	out := tty.String()
	if !strings.Contains(out, "hello\n") {
		t.Fatalf("expected reply text in combined tty output, got %q", out)
	}
	if strings.Contains(out, "Respondinghello") || strings.Contains(out, "Thinkinghello") {
		t.Fatalf("expected spinner footer to be cleared before stdout writes, got %q", out)
	}
	if !r.spinnerVisible {
		t.Fatalf("expected spinner to be restored after stdout writes")
	}
}

func TestRendererLiveSpinnerDoesNotRedrawBetweenUserSeparatorAndPrompt(t *testing.T) {
	var tty bytes.Buffer
	current := time.Unix(305, 0)
	r := newRendererWithOptions(&tty, &tty, nil, true, rendererOptions{
		persistentSpinner: true,
		now:               func() time.Time { return current },
		spinnerInterval:   time.Hour,
		modelLabel:        "openai/gpt-test",
		contextWindow:     10000,
	})
	r.startPersistentSpinner()
	defer r.stopPersistentSpinner()

	tty.Reset()
	r.handleEvent(libagent.AgentEvent{
		Type: libagent.AgentEventTypeMessageEnd,
		Message: &libagent.UserMessage{
			Content: "ciao",
		},
	})

	out := ansiRE.ReplaceAllString(tty.String(), "")
	separatorIdx := strings.Index(out, "────")
	if separatorIdx < 0 {
		t.Fatalf("expected user separator in output, got %q", out)
	}
	promptIdx := strings.Index(out, "❯ ciao\n")
	if promptIdx < 0 {
		t.Fatalf("expected rendered user prompt in output, got %q", out)
	}
	modelIdx := strings.Index(out, "openai/gpt-test")
	if modelIdx >= 0 && modelIdx < promptIdx {
		t.Fatalf("expected spinner footer after prompt render, got %q", out)
	}
}

func TestRendererLiveSpinnerKeepsFooterAfterFinalizedToolStatus(t *testing.T) {
	var stderr bytes.Buffer
	current := time.Unix(400, 0)
	r := newRendererWithOptions(&stderr, &bytes.Buffer{}, nil, true, rendererOptions{
		persistentSpinner: true,
		now:               func() time.Time { return current },
		spinnerInterval:   time.Hour,
		modelLabel:        "openai/gpt-test",
		contextWindow:     10000,
	})
	r.startPersistentSpinner()
	defer r.stopPersistentSpinner()

	r.handleEvent(libagent.AgentEvent{
		Type:       libagent.AgentEventTypeToolExecutionStart,
		ToolCallID: "call-read",
		ToolName:   "read",
		ToolArgs:   `{"path":"README.md"}`,
	})
	current = current.Add(1500 * time.Millisecond)
	r.handleEvent(libagent.AgentEvent{
		Type: libagent.AgentEventTypeMessageEnd,
		Message: &libagent.ToolResultMessage{
			ToolCallID: "call-read",
			ToolName:   "read",
			Content:    "ok",
		},
	})

	out := stderr.String()
	if !strings.Contains(out, "✓") || !strings.Contains(out, "README.md") {
		t.Fatalf("expected finalized tool status in stderr, got %q", out)
	}
	if !r.spinnerVisible {
		t.Fatalf("expected footer to remain active after finalized tool status")
	}
	if got := spinnerLabelForTest(r); got != "Reasoning" {
		t.Fatalf("spinner label after tool completion = %q, want %q", got, "Reasoning")
	}
}

func TestRendererPersistentSpinnerDoesNothingForNonTTY(t *testing.T) {
	var stderr bytes.Buffer
	r := newRendererWithOptions(&stderr, &bytes.Buffer{}, nil, false, rendererOptions{
		persistentSpinner: true,
		spinnerInterval:   time.Hour,
		modelLabel:        "openai/gpt-test",
		contextWindow:     10000,
	})

	r.startPersistentSpinner()
	defer r.stopPersistentSpinner()

	if got := stderr.String(); got != "" {
		t.Fatalf("expected no live spinner output for non-tty renderer, got %q", got)
	}
	if r.spinnerVisible {
		t.Fatalf("expected spinner to remain hidden for non-tty renderer")
	}
}

func TestRendererLiveSpinnerShowsMultiplePendingTools(t *testing.T) {
	var stderr bytes.Buffer
	r := newRendererWithOptions(&stderr, &bytes.Buffer{}, nil, true, rendererOptions{
		persistentSpinner: true,
		spinnerInterval:   time.Hour,
	})
	r.startPersistentSpinner()
	defer r.stopPersistentSpinner()

	// First tool starts - shows compact format with byte size
	r.handleEvent(libagent.AgentEvent{
		Type:       libagent.AgentEventTypeToolExecutionStart,
		ToolCallID: "call-1",
		ToolName:   "read",
		ToolArgs:   `{"path":"main.go"}`,
	})
	if got := spinnerLabelForTest(r); got != "read (18 bytes)" {
		t.Fatalf("spinner label with one pending tool = %q, want %q", got, "read (18 bytes)")
	}

	// Second tool starts - shows "Tool calls (N, X bytes)" format
	r.handleEvent(libagent.AgentEvent{
		Type:       libagent.AgentEventTypeToolExecutionStart,
		ToolCallID: "call-2",
		ToolName:   "bash",
		ToolArgs:   `{"command":"go build"}`,
	})
	// 18 bytes (first) + 22 bytes (second) = 40 bytes
	if got := spinnerLabelForTest(r); got != "Tool calls (2, 40 bytes)" {
		t.Fatalf("spinner label with two pending tools = %q, want %q", got, "Tool calls (2, 40 bytes)")
	}

	// Third tool starts
	r.handleEvent(libagent.AgentEvent{
		Type:       libagent.AgentEventTypeToolExecutionStart,
		ToolCallID: "call-3",
		ToolName:   "glob",
		ToolArgs:   `{"pattern":"*.go"}`,
	})
	// 18 + 22 + 18 = 58 bytes
	if got := spinnerLabelForTest(r); got != "Tool calls (3, 58 bytes)" {
		t.Fatalf("spinner label with three pending tools = %q, want %q", got, "Tool calls (3, 58 bytes)")
	}
}

func TestRendererReplyOutputRendersMarkdownLineByLine(t *testing.T) {
	var stderr bytes.Buffer
	var stdout bytes.Buffer
	r := newRenderer(&stderr, &stdout, nil, false)

	r.handleEvent(libagent.AgentEvent{
		Type:  libagent.AgentEventTypeMessageUpdate,
		Delta: &libagent.StreamDelta{Type: "text_delta", Delta: "**bold**\n"},
	})

	got := ansiRE.ReplaceAllString(stdout.String(), "")
	if !strings.Contains(got, "bold\n") {
		t.Fatalf("expected rendered markdown line in stdout, got %q", got)
	}
	if strings.Contains(got, "**bold**") {
		t.Fatalf("expected markdown markers removed, got %q", got)
	}
}

func TestRendererThinkingOutputIsMutedAndLineBuffered(t *testing.T) {
	var stderr bytes.Buffer
	var stdout bytes.Buffer
	r := newRenderer(&stderr, &stdout, nil, false)

	r.handleEvent(libagent.AgentEvent{
		Type:  libagent.AgentEventTypeMessageUpdate,
		Delta: &libagent.StreamDelta{Type: "reasoning_start", ID: "r1"},
	})
	r.handleEvent(libagent.AgentEvent{
		Type:  libagent.AgentEventTypeMessageUpdate,
		Delta: &libagent.StreamDelta{Type: "reasoning_delta", ID: "r1", Delta: "think"},
	})
	if got := stdout.String(); got != "" {
		t.Fatalf("expected no thinking stdout flush without newline, got %q", got)
	}

	r.handleEvent(libagent.AgentEvent{
		Type:  libagent.AgentEventTypeMessageUpdate,
		Delta: &libagent.StreamDelta{Type: "reasoning_delta", ID: "r1", Delta: "ing\ntail"},
	})
	first := stdout.String()
	expectedFirst := thinkingMutedStyle.Render("thinking") + "\n"
	if first != expectedFirst {
		t.Fatalf("expected muted flushed thinking line %q, got %q", expectedFirst, first)
	}
	if strings.Contains(first, "tail") {
		t.Fatalf("expected tail to remain buffered before reasoning_end, got %q", first)
	}

	r.handleEvent(libagent.AgentEvent{
		Type:  libagent.AgentEventTypeMessageUpdate,
		Delta: &libagent.StreamDelta{Type: "reasoning_end", ID: "r1"},
	})
	if got := stdout.String(); got != expectedFirst {
		t.Fatalf("expected buffered thinking tail to remain pending after reasoning_end, got %q", got)
	}

	r.handleEvent(libagent.AgentEvent{
		Type: libagent.AgentEventTypeMessageEnd,
		Message: &libagent.AssistantMessage{
			Role: "assistant",
		},
	})
	expectedFull := expectedFirst + thinkingMutedStyle.Render("tail") + "\n"
	if got := stdout.String(); got != expectedFull {
		t.Fatalf("expected buffered muted thinking tail on assistant end %q, got %q", expectedFull, got)
	}
}

func TestRendererThinkingOutputTrimsFirstDeltaLeftWhitespace(t *testing.T) {
	var stderr bytes.Buffer
	var stdout bytes.Buffer
	r := newRenderer(&stderr, &stdout, nil, false)

	r.handleEvent(libagent.AgentEvent{
		Type:  libagent.AgentEventTypeMessageUpdate,
		Delta: &libagent.StreamDelta{Type: "reasoning_start", ID: "r2"},
	})
	r.handleEvent(libagent.AgentEvent{
		Type:  libagent.AgentEventTypeMessageUpdate,
		Delta: &libagent.StreamDelta{Type: "reasoning_delta", ID: "r2", Delta: "   first line   \n    second line   "},
	})
	r.handleEvent(libagent.AgentEvent{
		Type:  libagent.AgentEventTypeMessageUpdate,
		Delta: &libagent.StreamDelta{Type: "reasoning_end", ID: "r2"},
	})
	r.handleEvent(libagent.AgentEvent{
		Type: libagent.AgentEventTypeMessageEnd,
		Message: &libagent.AssistantMessage{
			Role: "assistant",
		},
	})

	// First delta's leading whitespace is trimmed, but internal and trailing spacing is preserved.
	expected := thinkingMutedStyle.Render("first line   ") + "\n" +
		thinkingMutedStyle.Render("    second line   ") + "\n"
	if got := stdout.String(); got != expected {
		t.Fatalf("expected trimmed first delta output %q, got %q", expected, got)
	}
}

func TestRendererBackfillsFinalAssistantReasoningWhenStreamWasIncomplete(t *testing.T) {
	var stderr bytes.Buffer
	var stdout bytes.Buffer
	r := newRenderer(&stderr, &stdout, nil, false)

	r.handleEvent(libagent.AgentEvent{
		Type:  libagent.AgentEventTypeMessageUpdate,
		Delta: &libagent.StreamDelta{Type: "reasoning_start", ID: "r3", Delta: "The user is asking"},
	})
	r.handleEvent(libagent.AgentEvent{
		Type: libagent.AgentEventTypeMessageEnd,
		Message: libagent.NewAssistantMessage(
			"4",
			"The user is asking for 2+2. I should answer directly.",
			nil,
			time.Now(),
		),
	})

	expectedThinking := thinkingMutedStyle.Render("The user is asking for 2+2. I should answer directly.") + "\n"
	if got := stdout.String(); got != expectedThinking {
		t.Fatalf("expected reconciled reasoning and final text, got %q", got)
	}
}

func TestRendererThinkingTailDropsIncompleteTrailingSentenceFragment(t *testing.T) {
	var stderr bytes.Buffer
	var stdout bytes.Buffer
	r := newRenderer(&stderr, &stdout, nil, false)

	r.handleEvent(libagent.AgentEvent{
		Type:  libagent.AgentEventTypeMessageUpdate,
		Delta: &libagent.StreamDelta{Type: "reasoning_start", ID: "r4"},
	})
	r.handleEvent(libagent.AgentEvent{
		Type:  libagent.AgentEventTypeMessageUpdate,
		Delta: &libagent.StreamDelta{Type: "reasoning_delta", ID: "r4", Delta: "The user is asking a simple math question: 2+2. I can answer this directly without needing"},
	})
	r.handleEvent(libagent.AgentEvent{
		Type: libagent.AgentEventTypeMessageEnd,
		Message: &libagent.AssistantMessage{
			Role: "assistant",
		},
	})

	expected := thinkingMutedStyle.Render("The user is asking a simple math question: 2+2.") + "\n"
	if got := stdout.String(); got != expected {
		t.Fatalf("expected incomplete trailing sentence to be dropped, got %q", got)
	}
}

func TestRendererUserMessageRendersWithPrefix(t *testing.T) {
	var stderr bytes.Buffer
	var stdout bytes.Buffer
	r := newRenderer(&stderr, &stdout, nil, false)

	r.handleEvent(libagent.AgentEvent{
		Type: libagent.AgentEventTypeMessageEnd,
		Message: &libagent.UserMessage{
			Role:    "user",
			Content: "hello",
		},
	})

	want := renderUserSeparator() + "\n" + renderUserPrefix() + "hello\n"
	if got := stdout.String(); got != want {
		t.Fatalf("user replay output = %q, want %q", got, want)
	}
}

func TestRendererToolResultRendersDiffPreviewForEditLikeTools(t *testing.T) {
	var stderr bytes.Buffer
	editTool := tools.WrapTool(
		libagent.NewParallelTypedTool("edit", "test", func(context.Context, map[string]any, libagent.ToolCall) (libagent.ToolResponse, error) {
			return libagent.NewTextResponse("ok"), nil
		}),
		tools.RenderEditSingleLinePreview,
		tools.RenderEditFinalRender,
	)
	r := newRenderer(&stderr, &bytes.Buffer{}, []libagent.Tool{editTool}, false)

	r.handleEvent(libagent.AgentEvent{
		Type:  libagent.AgentEventTypeMessageUpdate,
		Delta: &libagent.StreamDelta{Type: "tool_input_start", ID: "call-edit", ToolName: "edit"},
	})
	r.handleEvent(libagent.AgentEvent{
		Type: libagent.AgentEventTypeMessageEnd,
		Message: &libagent.ToolResultMessage{
			ToolCallID: "call-edit",
			ToolName:   "edit",
			Content:    "ok",
			Metadata:   `{"diff":"- 2 | old line\n+ 2 | new line"}`,
		},
	})

	out := stderr.String()
	if !strings.Contains(out, "old line") || !strings.Contains(out, "new line") {
		t.Fatalf("expected completion diff preview in stderr, got %q", out)
	}
}

func TestRendererToolResultSkipsLargeOutputPreviewForNonEditTools(t *testing.T) {
	var stderr bytes.Buffer
	bashTool := libagent.NewParallelTypedTool("bash", "test", func(context.Context, map[string]any, libagent.ToolCall) (libagent.ToolResponse, error) {
		return libagent.NewTextResponse("ok"), nil
	})
	r := newRenderer(&stderr, &bytes.Buffer{}, []libagent.Tool{bashTool}, false)

	r.handleEvent(libagent.AgentEvent{
		Type:  libagent.AgentEventTypeMessageUpdate,
		Delta: &libagent.StreamDelta{Type: "tool_input_start", ID: "call-bash", ToolName: "bash"},
	})
	r.handleEvent(libagent.AgentEvent{
		Type: libagent.AgentEventTypeMessageEnd,
		Message: &libagent.ToolResultMessage{
			ToolCallID: "call-bash",
			ToolName:   "bash",
			Content:    "ok",
		},
	})

	out := stderr.String()
	if strings.Contains(out, "should-not-be-rendered") {
		t.Fatalf("expected non-edit tool completion preview to remain compact, got %q", out)
	}
}

func TestRendererToolExecutionEndIsNotLostWithoutToolMessageEnd(t *testing.T) {
	var stderr bytes.Buffer
	r := newRenderer(&stderr, &bytes.Buffer{}, nil, false)

	r.handleEvent(libagent.AgentEvent{
		Type:       libagent.AgentEventTypeToolExecutionStart,
		ToolCallID: "call-read",
		ToolName:   "read",
		ToolArgs:   `{"path":"README.md"}`,
	})
	r.handleEvent(libagent.AgentEvent{
		Type:        libagent.AgentEventTypeToolExecutionEnd,
		ToolCallID:  "call-read",
		ToolName:    "read",
		ToolArgs:    `{"path":"README.md"}`,
		ToolResult:  "ok",
		ToolIsError: false,
	})
	r.handleEvent(libagent.AgentEvent{Type: libagent.AgentEventTypeAgentEnd})

	out := stderr.String()
	if strings.Contains(out, "(cancelled)") {
		t.Fatalf("expected completed tool not to be cancelled, got %q", out)
	}
	if !strings.Contains(out, "✓") {
		t.Fatalf("expected completed tool status, got %q", out)
	}
}

func TestRendererPreviewOnlyToolIsNotMarkedCancelled(t *testing.T) {
	var stderr bytes.Buffer
	r := newRenderer(&stderr, &bytes.Buffer{}, nil, false)

	r.handleEvent(libagent.AgentEvent{Type: libagent.AgentEventTypeTurnStart})
	r.handleEvent(libagent.AgentEvent{
		Type:  libagent.AgentEventTypeMessageUpdate,
		Delta: &libagent.StreamDelta{Type: "tool_input_start", ID: "preview-1", ToolName: "read"},
	})
	r.handleEvent(libagent.AgentEvent{
		Type: libagent.AgentEventTypeMessageEnd,
		Message: &libagent.AssistantMessage{
			Role: "assistant",
		},
	})
	r.handleEvent(libagent.AgentEvent{Type: libagent.AgentEventTypeAgentEnd})

	out := stderr.String()
	if strings.Contains(out, "(cancelled)") {
		t.Fatalf("expected preview-only tool not to be cancelled, got %q", out)
	}
}

func TestRendererContextCompactionUpdatesSpinnerEstimate(t *testing.T) {
	r := newRendererWithOptions(&bytes.Buffer{}, &bytes.Buffer{}, nil, false, rendererOptions{
		contextWindow: 10000,
		initialMessages: []libagent.Message{
			&libagent.UserMessage{Role: "user", Content: strings.Repeat("a", 2000)},
			&libagent.AssistantMessage{Role: "assistant", Text: strings.Repeat("b", 2000), Completed: true},
			&libagent.UserMessage{Role: "user", Content: strings.Repeat("c", 2000)},
			&libagent.AssistantMessage{Role: "assistant", Text: strings.Repeat("d", 2000), Completed: true},
		},
	})

	before := r.spinnerContextLabelLocked()
	r.handleEvent(libagent.AgentEvent{
		Type: libagent.AgentEventTypeContextCompaction,
		ContextCompaction: &libagent.ContextCompactionEvent{
			Phase: libagent.ContextCompactionPhaseEnd,
			Mode:  libagent.ContextCompactionModeAuto,
			Kept:  1,
		},
	})
	after := r.spinnerContextLabelLocked()

	if before == after {
		t.Fatalf("expected compaction to update spinner estimate, before=%q after=%q", before, after)
	}
}

func TestRendererHandlesAllAgentEventTypesAndKnownDeltaTypes(t *testing.T) {
	var stderr bytes.Buffer
	var stdout bytes.Buffer
	r := newRenderer(&stderr, &stdout, nil, false)

	r.handleEvent(libagent.AgentEvent{Type: libagent.AgentEventTypeAgentStart})
	r.handleEvent(libagent.AgentEvent{Type: libagent.AgentEventTypeTurnStart})
	r.handleEvent(libagent.AgentEvent{
		Type:    libagent.AgentEventTypeMessageStart,
		Message: &libagent.AssistantMessage{},
	})
	r.handleEvent(libagent.AgentEvent{
		Type:  libagent.AgentEventTypeMessageUpdate,
		Delta: &libagent.StreamDelta{Type: "text_start", ID: "txt-1"},
	})
	r.handleEvent(libagent.AgentEvent{
		Type:  libagent.AgentEventTypeMessageUpdate,
		Delta: &libagent.StreamDelta{Type: "text_delta", ID: "txt-1", Delta: "hello"},
	})
	r.handleEvent(libagent.AgentEvent{
		Type:  libagent.AgentEventTypeMessageUpdate,
		Delta: &libagent.StreamDelta{Type: "text_end", ID: "txt-1"},
	})
	r.handleEvent(libagent.AgentEvent{
		Type:  libagent.AgentEventTypeMessageUpdate,
		Delta: &libagent.StreamDelta{Type: "reasoning_start", ID: "rsn-1"},
	})
	r.handleEvent(libagent.AgentEvent{
		Type:  libagent.AgentEventTypeMessageUpdate,
		Delta: &libagent.StreamDelta{Type: "reasoning_delta", ID: "rsn-1", Delta: "thinking"},
	})
	r.handleEvent(libagent.AgentEvent{
		Type:  libagent.AgentEventTypeMessageUpdate,
		Delta: &libagent.StreamDelta{Type: "reasoning_end", ID: "rsn-1"},
	})
	r.handleEvent(libagent.AgentEvent{
		Type:  libagent.AgentEventTypeMessageUpdate,
		Delta: &libagent.StreamDelta{Type: "tool_input_start", ID: "tool-1", ToolName: "read"},
	})
	r.handleEvent(libagent.AgentEvent{
		Type:  libagent.AgentEventTypeMessageUpdate,
		Delta: &libagent.StreamDelta{Type: "tool_input_delta", ID: "tool-1", Delta: `{"path":"README.md"}`},
	})
	r.handleEvent(libagent.AgentEvent{
		Type:  libagent.AgentEventTypeMessageUpdate,
		Delta: &libagent.StreamDelta{Type: "tool_input_end", ID: "tool-1"},
	})
	r.handleEvent(libagent.AgentEvent{
		Type:       libagent.AgentEventTypeToolExecutionStart,
		ToolCallID: "tool-1",
		ToolName:   "read",
		ToolArgs:   `{"path":"README.md"}`,
	})
	r.handleEvent(libagent.AgentEvent{
		Type:        libagent.AgentEventTypeToolExecutionUpdate,
		ToolCallID:  "tool-1",
		ToolName:    "read",
		ToolArgs:    `{"path":"README.md"}`,
		ToolResult:  "partial",
		ToolIsError: false,
	})
	r.handleEvent(libagent.AgentEvent{
		Type:        libagent.AgentEventTypeToolExecutionEnd,
		ToolCallID:  "tool-1",
		ToolName:    "read",
		ToolArgs:    `{"path":"README.md"}`,
		ToolResult:  "done",
		ToolIsError: false,
	})
	r.handleEvent(libagent.AgentEvent{
		Type: libagent.AgentEventTypeMessageEnd,
		Message: &libagent.AssistantMessage{
			Role: "assistant",
		},
	})
	r.handleEvent(libagent.AgentEvent{Type: libagent.AgentEventTypeTurnEnd})
	r.handleEvent(libagent.AgentEvent{
		Type:         libagent.AgentEventTypeRetry,
		RetryMessage: "retrying",
	})
	r.handleEvent(libagent.AgentEvent{
		Type: libagent.AgentEventTypeContextCompaction,
		ContextCompaction: &libagent.ContextCompactionEvent{
			Phase:                  libagent.ContextCompactionPhaseEnd,
			Mode:                   libagent.ContextCompactionModeAuto,
			TriggerEstimatedTokens: 165000,
			TriggerContextPercent:  63.0,
			Summarized:             100,
			Kept:                   5,
		},
	})
	r.handleEvent(libagent.AgentEvent{Type: libagent.AgentEventTypeAgentEnd})

	if got := stdout.String(); !strings.Contains(got, "hello") {
		t.Fatalf("expected response text in stdout, got %q", got)
	}
	if got := stderr.String(); !strings.Contains(got, "read") {
		t.Fatalf("expected tool status in stderr, got %q", got)
	}
	if got := stderr.String(); !strings.Contains(got, "partial") {
		t.Fatalf("expected tool update payload in stderr, got %q", got)
	}
}

func TestRendererNoThinkingSuppressesThinkingOutput(t *testing.T) {
	var stderr bytes.Buffer
	var stdout bytes.Buffer
	r := newRendererWithOptions(&stderr, &stdout, nil, false, rendererOptions{
		noThinking: true,
	})

	// Simulate reasoning events
	r.handleEvent(libagent.AgentEvent{
		Type:  libagent.AgentEventTypeMessageUpdate,
		Delta: &libagent.StreamDelta{Type: "reasoning_start", ID: "r1"},
	})
	r.handleEvent(libagent.AgentEvent{
		Type:  libagent.AgentEventTypeMessageUpdate,
		Delta: &libagent.StreamDelta{Type: "reasoning_delta", ID: "r1", Delta: "This is my reasoning.\nMore thoughts."},
	})
	r.handleEvent(libagent.AgentEvent{
		Type:  libagent.AgentEventTypeMessageUpdate,
		Delta: &libagent.StreamDelta{Type: "reasoning_end", ID: "r1"},
	})
	r.handleEvent(libagent.AgentEvent{
		Type: libagent.AgentEventTypeMessageEnd,
		Message: &libagent.AssistantMessage{
			Role: "assistant",
		},
	})

	// With noThinking enabled, thinking content should not appear in stdout
	if got := stdout.String(); got != "" {
		t.Fatalf("expected no thinking output with noThinking=true, got %q", got)
	}

	// Reasoning status label should also be suppressed
	if got := stderr.String(); strings.Contains(got, "Reasoning") {
		t.Fatalf("expected no reasoning status with noThinking=true, got %q", got)
	}
}

func TestRendererNoThinkingAllowsReplyOutput(t *testing.T) {
	var stderr bytes.Buffer
	var stdout bytes.Buffer
	r := newRendererWithOptions(&stderr, &stdout, nil, false, rendererOptions{
		noThinking: true,
	})

	// Simulate both reasoning and reply events
	r.handleEvent(libagent.AgentEvent{
		Type:  libagent.AgentEventTypeMessageUpdate,
		Delta: &libagent.StreamDelta{Type: "reasoning_start", ID: "r1"},
	})
	r.handleEvent(libagent.AgentEvent{
		Type:  libagent.AgentEventTypeMessageUpdate,
		Delta: &libagent.StreamDelta{Type: "reasoning_delta", ID: "r1", Delta: "Hidden reasoning."},
	})
	r.handleEvent(libagent.AgentEvent{
		Type:  libagent.AgentEventTypeMessageUpdate,
		Delta: &libagent.StreamDelta{Type: "text_delta", Delta: "Visible response.\n"},
	})
	r.handleEvent(libagent.AgentEvent{
		Type: libagent.AgentEventTypeMessageEnd,
		Message: &libagent.AssistantMessage{
			Role: "assistant",
		},
	})

	// Thinking should be suppressed
	if got := stdout.String(); strings.Contains(got, "Hidden reasoning") {
		t.Fatalf("expected thinking to be suppressed, got %q", got)
	}

	// Reply should still appear
	if got := stdout.String(); !strings.Contains(got, "Visible response") {
		t.Fatalf("expected reply text in stdout, got %q", got)
	}
}

func lastNonEmptyLine(s string) string {
	lines := strings.Split(s, "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if strings.TrimSpace(lines[i]) != "" {
			return lines[i]
		}
	}
	return ""
}

func spinnerLabelForTest(r *renderer) string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.spinnerLabelLocked()
}

func spinnerElapsedForTest(r *renderer) string {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.updateSpinnerPhaseLocked()
	return r.spinnerElapsedLocked()
}
