package chat

import (
	"testing"

	"github.com/francescoalemanno/raijin-mono/libtui/pkg/tui"

	libagent "github.com/francescoalemanno/raijin-mono/libagent"
)

func TestAppendStoredMessage_AssistantReasoningRendersThinkingBlock(t *testing.T) {
	app := &ChatApp{
		history:      &tui.Container{},
		pendingTools: make(map[string]*ToolExecutionComponent),
	}

	app.appendStoredMessage(&libagent.AssistantMessage{
		Role:      "assistant",
		Reasoning: "step one\nstep two",
		Completed: true,
	})

	if len(app.items) != 2 {
		t.Fatalf("history entries = %d, want 2 (spacer + thinking)", len(app.items))
	}
	if _, ok := app.items[1].component.(*ThinkingComponent); !ok {
		t.Fatalf("history component type = %T, want *ThinkingComponent", app.items[1].component)
	}
}

func TestAppendStoredMessage_AssistantUnfinishedToolCallRehydratesAsCancelledOnFinalize(t *testing.T) {
	app := &ChatApp{
		history:      &tui.Container{},
		pendingTools: make(map[string]*ToolExecutionComponent),
	}

	app.appendStoredMessage(&libagent.AssistantMessage{
		Role:      "assistant",
		Completed: true,
		ToolCalls: []libagent.ToolCallItem{{
			ID:    "tool-1",
			Name:  "read",
			Input: `{"path":"README.md"}`,
		}},
	})

	comp, ok := app.pendingTools["tool-1"]
	if !ok {
		t.Fatal("expected unfinished tool call to remain pending before finalization")
	}
	if !comp.IsPending() {
		t.Fatal("expected unfinished tool call component to be pending before finalization")
	}

	app.finalizeReplayedToolStates()

	if len(app.pendingTools) != 0 {
		t.Fatalf("pending tools = %d, want 0 after finalization", len(app.pendingTools))
	}
	if comp.IsPending() {
		t.Fatal("expected unfinished tool call to be marked cancelled after finalization")
	}
}

func TestAppendStoredMessage_ToolResultWithImageRehydratesMediaMetadata(t *testing.T) {
	pngData := []byte{
		0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a,
		0x00, 0x00, 0x00, 0x0d, 0x49, 0x48, 0x44, 0x52,
		0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01,
		0x08, 0x06, 0x00, 0x00, 0x00, 0x1f, 0x15, 0xc4,
		0x89, 0x00, 0x00, 0x00, 0x0a, 0x49, 0x44, 0x41,
		0x54, 0x78, 0x9c, 0x63, 0x00, 0x01, 0x00, 0x00,
		0x05, 0x00, 0x01, 0x0d, 0x0a, 0x2d, 0xb4, 0x00,
		0x00, 0x00, 0x00, 0x49, 0x45, 0x4e, 0x44, 0xae,
		0x42, 0x60, 0x82,
	}

	app := &ChatApp{
		history:      &tui.Container{},
		pendingTools: make(map[string]*ToolExecutionComponent),
	}

	app.appendStoredMessage(&libagent.ToolResultMessage{
		Role:       "toolResult",
		ToolCallID: "tool-1",
		ToolName:   "read",
		Content:    "Loaded image/png content",
		Data:       pngData,
		MIMEType:   "image/png",
	})

	if len(app.items) != 1 {
		t.Fatalf("history entries = %d, want 1 tool component", len(app.items))
	}
	comp, ok := app.items[0].component.(*ToolExecutionComponent)
	if !ok {
		t.Fatalf("history component type = %T, want *ToolExecutionComponent", app.items[0].component)
	}
	if comp.result == nil || comp.result.media == nil {
		t.Fatalf("expected tool component to carry replayed media")
	}

	statusOnly := comp.status.Render(80)
	full := comp.Render(80)
	if len(full) != len(statusOnly) {
		t.Fatalf("expected no additional inline image lines after image-render removal")
	}
}
