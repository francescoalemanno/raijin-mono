package libagent

import (
	"encoding/json"

	"charm.land/fantasy"
)

// MarshalAssistantContent serializes assistant response content in a format
// that can be safely decoded back into concrete fantasy content types.
func MarshalAssistantContent(content fantasy.ResponseContent) json.RawMessage {
	if len(content) == 0 {
		return nil
	}
	data, err := json.Marshal(fantasy.Response{Content: content})
	if err != nil {
		return nil
	}
	return data
}

// UnmarshalAssistantContent decodes assistant response content previously
// serialized by MarshalAssistantContent.
func UnmarshalAssistantContent(raw json.RawMessage) fantasy.ResponseContent {
	if len(raw) == 0 {
		return nil
	}
	var resp fantasy.Response
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil
	}
	if len(resp.Content) == 0 {
		return nil
	}
	return resp.Content
}
