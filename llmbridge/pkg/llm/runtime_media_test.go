package llm

import (
	"encoding/base64"
	"testing"

	"charm.land/fantasy"
)

func TestDefaultProviderMediaStrategy_AdaptMessages_UnsupportedProvider(t *testing.T) {
	t.Parallel()

	data := base64.StdEncoding.EncodeToString([]byte{0x01, 0x02})
	messages := []Message{{
		Role: RoleTool,
		Content: []Part{ToolResultPart{
			ToolCallID: "call-1",
			Output: ToolResultOutput{
				Type:      ToolResultOutputMedia,
				Data:      data,
				MediaType: "image/png",
			},
		}},
	}}

	adapted := newProviderMediaStrategy(ProviderOpenAI).AdaptMessages(messages)
	if len(adapted) != 2 {
		t.Fatalf("adapted message count = %d, want 2", len(adapted))
	}
	if adapted[0].Role != RoleTool || adapted[1].Role != RoleUser {
		t.Fatalf("unexpected role sequence: %q, %q", adapted[0].Role, adapted[1].Role)
	}

	tr, ok := adapted[0].Content[0].(ToolResultPart)
	if !ok {
		t.Fatalf("expected first adapted part to be ToolResultPart, got %T", adapted[0].Content[0])
	}
	if tr.Output.Type != ToolResultOutputText {
		t.Fatalf("tool result output type = %q, want text", tr.Output.Type)
	}
	if tr.Output.Text == "" {
		t.Fatalf("tool result placeholder text should not be empty")
	}
	if tr.Metadata != "" {
		t.Fatalf("metadata = %q, want empty", tr.Metadata)
	}

	if _, ok := adapted[1].Content[len(adapted[1].Content)-1].(FilePart); !ok {
		t.Fatalf("expected user media relay message to include FilePart")
	}
}

func TestDefaultProviderMediaStrategy_AdaptMessages_SupportedProvider(t *testing.T) {
	t.Parallel()

	messages := []Message{{
		Role: RoleTool,
		Content: []Part{ToolResultPart{
			ToolCallID: "call-1",
			Output:     ToolResultOutput{Type: ToolResultOutputMedia, Data: "ZmFrZQ==", MediaType: "image/png"},
		}},
	}}

	adapted := newProviderMediaStrategy(ProviderAnthropic).AdaptMessages(messages)
	if len(adapted) != 1 {
		t.Fatalf("adapted message count = %d, want 1", len(adapted))
	}
	if adapted[0].Role != RoleTool {
		t.Fatalf("adapted role = %q, want tool", adapted[0].Role)
	}
}

func TestFromFantasyToolResult_PreservesClientMetadata(t *testing.T) {
	t.Parallel()

	part := fromFantasyToolResult(fantasy.ToolResultContent{
		ToolCallID:       "call-1",
		ToolName:         "bash",
		Result:           fantasy.ToolResultOutputContentText{Text: "ok"},
		ClientMetadata:   `{"k":"v"}`,
		ProviderExecuted: false,
	})

	if part.Metadata != `{"k":"v"}` {
		t.Fatalf("metadata = %q, want preserved client metadata", part.Metadata)
	}
}

func TestToFantasyToolResponseType(t *testing.T) {
	t.Parallel()

	if got := toFantasyToolResponseType(ToolResponseTypeText); got != "text" {
		t.Fatalf("text mapping = %q, want %q", got, "text")
	}
	if got := toFantasyToolResponseType(ToolResponseTypeMedia); got != "image" {
		t.Fatalf("media mapping = %q, want %q", got, "image")
	}
	if got := toFantasyToolResponseType(ToolResponseType("custom")); got != "custom" {
		t.Fatalf("custom mapping = %q, want %q", got, "custom")
	}
}
