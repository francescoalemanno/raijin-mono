package oneshot

import (
	"bytes"
	"context"
	"strings"
	"testing"

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

	out := stderr.String()
	if !strings.Contains(out, "Thinking") {
		t.Fatalf("expected thinking status to be printed, got %q", out)
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

func TestRendererReplyOutputRendersMarkdownLineByLine(t *testing.T) {
	var stderr bytes.Buffer
	var stdout bytes.Buffer
	r := newRenderer(&stderr, &stdout, nil, false)

	r.handleEvent(libagent.AgentEvent{
		Type:  libagent.AgentEventTypeMessageUpdate,
		Delta: &libagent.StreamDelta{Type: "text_delta", Delta: "**bold**\n"},
	})

	got := stdout.String()
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
	expectedFull := expectedFirst + thinkingMutedStyle.Render("tail") + "\n"
	if got := stdout.String(); got != expectedFull {
		t.Fatalf("expected buffered muted thinking tail on reasoning_end %q, got %q", expectedFull, got)
	}
}

func TestRendererThinkingOutputTrimsWeirdSpacing(t *testing.T) {
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

	expected := thinkingMutedStyle.Render("first line") + "\n" +
		thinkingMutedStyle.Render("second line") + "\n"
	if got := stdout.String(); got != expected {
		t.Fatalf("expected trimmed muted thinking output %q, got %q", expected, got)
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

	want := renderUserPrefix() + "hello\n"
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
	r.handleEvent(libagent.AgentEvent{Type: libagent.AgentEventTypeAgentEnd})

	if got := stdout.String(); !strings.Contains(got, "hello") {
		t.Fatalf("expected response text in stdout, got %q", got)
	}
	if got := stderr.String(); !strings.Contains(got, "read") {
		t.Fatalf("expected tool status in stderr, got %q", got)
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
