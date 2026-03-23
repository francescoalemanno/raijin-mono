package libagent

import (
	"context"
	"encoding/json"
	"iter"

	"charm.land/fantasy"
)

// StaticTextModel is a small test helper that streams a fixed text response and
// captures prompt metadata without requiring callers outside libagent to import fantasy.
type StaticTextModel struct {
	Response   string
	PromptLen  int
	PromptJSON string
}

func (m *StaticTextModel) Stream(_ context.Context, call fantasy.Call) (fantasy.StreamResponse, error) {
	m.PromptLen = len(call.Prompt)
	if raw, err := json.Marshal(call.Prompt); err == nil {
		m.PromptJSON = string(raw)
	} else {
		m.PromptJSON = ""
	}

	return iter.Seq[fantasy.StreamPart](func(yield func(fantasy.StreamPart) bool) {
		if !yield(fantasy.StreamPart{Type: fantasy.StreamPartTypeTextStart, ID: "txt-1"}) {
			return
		}
		if !yield(fantasy.StreamPart{Type: fantasy.StreamPartTypeTextDelta, ID: "txt-1", Delta: m.Response}) {
			return
		}
		if !yield(fantasy.StreamPart{Type: fantasy.StreamPartTypeTextEnd, ID: "txt-1"}) {
			return
		}
		yield(fantasy.StreamPart{Type: fantasy.StreamPartTypeFinish, FinishReason: fantasy.FinishReasonStop})
	}), nil
}

func (m *StaticTextModel) Generate(context.Context, fantasy.Call) (*fantasy.Response, error) {
	return nil, nil
}

func (m *StaticTextModel) GenerateObject(context.Context, fantasy.ObjectCall) (*fantasy.ObjectResponse, error) {
	return nil, nil
}

func (m *StaticTextModel) StreamObject(context.Context, fantasy.ObjectCall) (fantasy.ObjectStreamResponse, error) {
	return nil, nil
}

func (m *StaticTextModel) Provider() string { return "mock" }
func (m *StaticTextModel) Model() string    { return "mock" }
