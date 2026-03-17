package tools

import (
	"context"
	"strings"
	"testing"

	"github.com/francescoalemanno/raijin-mono/libagent"
)

func TestWrapToolProvidesRenderHooks(t *testing.T) {
	base := libagent.NewParallelTypedTool("read", "test", func(context.Context, map[string]any, libagent.ToolCall) (libagent.ToolResponse, error) {
		return libagent.NewTextResponse("ok"), nil
	})
	wrapped := WrapTool(base, RenderReadSingleLinePreview, nil)

	if _, ok := wrapped.(WrappedTool); !ok {
		t.Fatalf("wrapped tool should implement WrappedTool")
	}
}

func TestSingleLinePreviewReadIsCompact(t *testing.T) {
	preview := RenderReadSingleLinePreview(`{"path":"README.md","offset":11,"limit":40}`)
	if !strings.Contains(preview, "read README.md") {
		t.Fatalf("expected path in preview, got %q", preview)
	}
	if !strings.Contains(preview, "offset=11") || !strings.Contains(preview, "limit=40") {
		t.Fatalf("expected offset/limit in preview, got %q", preview)
	}
}

func TestSingleLinePreviewRepairsPartialJSONByClosingDelimiters(t *testing.T) {
	preview := RenderReadSingleLinePreview(`{"path":"README.md","offset":11`)
	if !strings.Contains(preview, "read README.md") {
		t.Fatalf("expected repaired preview to include path, got %q", preview)
	}
	if !strings.Contains(preview, "offset=11") {
		t.Fatalf("expected repaired preview to include offset, got %q", preview)
	}
}

func TestSingleLinePreviewRepairsTrailingCommaInPartialJSON(t *testing.T) {
	preview := RenderReadSingleLinePreview(`{"path":"README.md",`)
	if !strings.Contains(preview, "read README.md") {
		t.Fatalf("expected repaired preview to include path, got %q", preview)
	}
}

func TestFinalRenderDefaultsToSingleLineWhenNoSpecialMetadata(t *testing.T) {
	preview := RenderGlobSingleLinePreview(`{"pattern":"*.go","path":"internal"}`)
	final := WrapTool(
		libagent.NewParallelTypedTool("glob", "test", func(context.Context, map[string]any, libagent.ToolCall) (libagent.ToolResponse, error) {
			return libagent.NewTextResponse("ok"), nil
		}),
		RenderGlobSingleLinePreview,
		nil,
	).(WrappedTool).FinalRender(`{"pattern":"*.go","path":"internal"}`, "result", "")
	if final != preview {
		t.Fatalf("expected final render to fallback to single-line preview, got preview=%q final=%q", preview, final)
	}
}

func TestFinalRenderShowsDiffForEditAndWrite(t *testing.T) {
	final := RenderEditFinalRender(`{"path":"main.go"}`, "ok", `{"diff":"- 2 | old line\n+ 2 | new line"}`)
	if !strings.Contains(final, "edit main.go") {
		t.Fatalf("expected edit preview in final render, got %q", final)
	}
	if !strings.Contains(final, "old line") || !strings.Contains(final, "new line") {
		t.Fatalf("expected diff lines in final render, got %q", final)
	}
}
