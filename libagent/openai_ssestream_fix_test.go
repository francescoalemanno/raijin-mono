package libagent

import (
	"bytes"
	"io"
	"net/http"
	"testing"

	openai "github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/packages/ssestream"
	"github.com/tidwall/gjson"
)

func TestOpenAICompatibleSSEDecoderSkipsCommentOnlyEvents(t *testing.T) {
	t.Parallel()

	res := &http.Response{
		Header: http.Header{
			"Content-Type": []string{"text/event-stream"},
		},
		Body: io.NopCloser(bytes.NewBufferString(
			": OPENROUTER PROCESSING\n\n" +
				"data: " +
				`{"id":"chunk-1","object":"chat.completion.chunk","created":1,"model":"kimi-k2.5","choices":[{"index":0,"delta":{"content":"","role":"assistant","tool_calls":[{"index":0,"id":"weather:0","type":"function","function":{"name":"weather","arguments":"{\"location\":\"Florence\"}"}}]},"finish_reason":"tool_calls"}]}` +
				"\n\n" +
				"data: [DONE]\n\n" +
				`data: {"choices":[],"cost":"0"}` + "\n\n",
		)),
	}

	stream := ssestream.NewStream[openai.ChatCompletionChunk](ssestream.NewDecoder(res), nil)

	if !stream.Next() {
		t.Fatalf("expected first chunk, got err=%v", stream.Err())
	}

	chunk := stream.Current()
	if chunk.ID != "chunk-1" {
		t.Fatalf("unexpected chunk id %q", chunk.ID)
	}
	if got := len(chunk.Choices); got != 1 {
		t.Fatalf("unexpected choice count %d", got)
	}
	if got := len(chunk.Choices[0].Delta.ToolCalls); got != 1 {
		t.Fatalf("unexpected tool call count %d", got)
	}
	if got := chunk.Choices[0].Delta.ToolCalls[0].Function.Name; got != "weather" {
		t.Fatalf("unexpected tool call name %q", got)
	}

	if stream.Next() {
		t.Fatal("expected stream to stop after [DONE]")
	}
	if err := stream.Err(); err != nil {
		t.Fatalf("unexpected stream err: %v", err)
	}
}

func TestOpenAICompatibleSSEDecoderSupportsCharsetContentType(t *testing.T) {
	t.Parallel()

	res := &http.Response{
		Header: http.Header{
			"Content-Type": []string{"text/event-stream; charset=utf-8"},
		},
		Body: io.NopCloser(bytes.NewBufferString(
			": keepalive\n\n" +
				`data: {"id":"chunk-2","object":"chat.completion.chunk","created":1,"model":"kimi-k2.5","choices":[]}` + "\n\n",
		)),
	}

	stream := ssestream.NewStream[openai.ChatCompletionChunk](ssestream.NewDecoder(res), nil)

	if !stream.Next() {
		t.Fatalf("expected first chunk, got err=%v", stream.Err())
	}
	if got := stream.Current().ID; got != "chunk-2" {
		t.Fatalf("unexpected chunk id %q", got)
	}
}

func TestNormalizeOpenAICompatibleReasoningFields_UsesReasoningWhenReasoningContentMissing(t *testing.T) {
	t.Parallel()

	raw := []byte(`{"choices":[{"delta":{"reasoning":"The user is asking"}}]}`)
	got := normalizeOpenAICompatibleReasoningFields(raw)

	if reasoning := gjson.GetBytes(got, "choices.0.delta.reasoning_content").String(); reasoning != "The user is asking" {
		t.Fatalf("reasoning_content=%q want %q", reasoning, "The user is asking")
	}
}

func TestNormalizeOpenAICompatibleReasoningFields_PreservesExistingReasoningContent(t *testing.T) {
	t.Parallel()

	raw := []byte(`{"choices":[{"delta":{"reasoning_content":"canonical","reasoning":"duplicate"}}]}`)
	got := normalizeOpenAICompatibleReasoningFields(raw)

	if reasoning := gjson.GetBytes(got, "choices.0.delta.reasoning_content").String(); reasoning != "canonical" {
		t.Fatalf("reasoning_content=%q want %q", reasoning, "canonical")
	}
}

func TestNormalizeOpenAICompatibleReasoningFields_SupportsReasoningText(t *testing.T) {
	t.Parallel()

	raw := []byte(`{"choices":[{"delta":{"reasoning_text":"Alternative field"}}]}`)
	got := normalizeOpenAICompatibleReasoningFields(raw)

	if reasoning := gjson.GetBytes(got, "choices.0.delta.reasoning_content").String(); reasoning != "Alternative field" {
		t.Fatalf("reasoning_content=%q want %q", reasoning, "Alternative field")
	}
}
