package chat

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"testing"

	libagent "github.com/francescoalemanno/raijin-mono/libagent"
)

func TestToolExecutionUpdateResultWithMedia_PreservesMediaMetadataWithoutInlineImageRender(t *testing.T) {
	ui := newVirtualTestUI(t)

	tool := libagent.NewTypedTool("read", "test", func(ctx context.Context, input map[string]any, call libagent.ToolCall) (libagent.ToolResponse, error) {
		return libagent.NewTextResponse("ok"), nil
	})

	var comp *ToolExecutionComponent
	runOnUI(t, ui, func() {
		comp = NewToolExecution("read", json.RawMessage(`{"path":"a.png"}`), tool, ui)
	})

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
	runOnUI(t, ui, func() {
		comp.UpdateResultWithMedia("Loaded image/png content", false, "image/png", base64.StdEncoding.EncodeToString(pngData))
	})

	var hasMedia bool
	var statusOnly []string
	var full []string
	runOnUI(t, ui, func() {
		hasMedia = comp.result != nil && comp.result.media != nil
		statusOnly = append([]string(nil), comp.status.Render(80)...)
		full = append([]string(nil), comp.Render(80)...)
	})

	if !hasMedia {
		t.Fatalf("expected media payload to be stored on tool result")
	}

	if len(full) != len(statusOnly) {
		t.Fatalf("expected no additional inline image lines after image-render removal")
	}
}
