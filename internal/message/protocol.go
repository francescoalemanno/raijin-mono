package message

import "strings"

// SanitizeHistory enforces persisted-message protocol invariants.
// It keeps only structurally valid user/assistant/tool messages and removes
// tool-call/result pairs that are not globally bijective (exactly one call and
// one result per tool call ID).
func SanitizeHistory(msgs []Message) []Message {
	if len(msgs) == 0 {
		return msgs
	}

	callCounts := make(map[string]int)
	resultCounts := make(map[string]int)

	for _, msg := range msgs {
		switch msg.Role {
		case Assistant:
			for _, call := range msg.ToolCalls() {
				id := strings.TrimSpace(call.ID)
				if id == "" {
					continue
				}
				callCounts[id]++
			}
		case Tool:
			for _, result := range msg.ToolResults() {
				id := strings.TrimSpace(result.ToolCallID)
				if id == "" {
					continue
				}
				resultCounts[id]++
			}
		}
	}

	validToolIDs := make(map[string]struct{})
	for id, calls := range callCounts {
		if calls == 1 && resultCounts[id] == 1 {
			validToolIDs[id] = struct{}{}
		}
	}

	out := make([]Message, 0, len(msgs))
	for _, msg := range msgs {
		clone := msg.Clone()
		switch clone.Role {
		case User:
			parts := make([]ContentPart, 0, len(clone.Parts))
			for _, part := range clone.Parts {
				switch p := part.(type) {
				case TextContent:
					if strings.TrimSpace(p.Text) == "" {
						continue
					}
					parts = append(parts, p)
				case BinaryContent:
					parts = append(parts, p)
				}
			}
			if len(parts) == 0 {
				continue
			}
			clone.Parts = parts
			out = append(out, clone)

		case Assistant:
			parts := make([]ContentPart, 0, len(clone.Parts))
			hasTextOrThinking := false
			hasToolCall := false
			hasFinish := false

			for _, part := range clone.Parts {
				switch p := part.(type) {
				case TextContent:
					if strings.TrimSpace(p.Text) == "" {
						continue
					}
					hasTextOrThinking = true
					parts = append(parts, p)
				case ReasoningContent:
					if strings.TrimSpace(p.Thinking) == "" {
						continue
					}
					hasTextOrThinking = true
					parts = append(parts, p)
				case ToolCall:
					id := strings.TrimSpace(p.ID)
					name := strings.TrimSpace(p.Name)
					if id == "" || name == "" {
						continue
					}
					if _, ok := validToolIDs[id]; !ok {
						continue
					}
					hasToolCall = true
					parts = append(parts, p)
				case Finish:
					hasFinish = true
					parts = append(parts, p)
				}
			}

			if !hasFinish {
				continue
			}
			if !hasTextOrThinking && !hasToolCall {
				continue
			}
			clone.Parts = parts
			out = append(out, clone)

		case Tool:
			parts := make([]ContentPart, 0, len(clone.Parts))
			for _, part := range clone.Parts {
				result, ok := part.(ToolResult)
				if !ok {
					continue
				}
				id := strings.TrimSpace(result.ToolCallID)
				name := strings.TrimSpace(result.Name)
				if id == "" || name == "" {
					continue
				}
				if _, ok := validToolIDs[id]; !ok {
					continue
				}
				parts = append(parts, result)
			}
			if len(parts) == 0 {
				continue
			}
			clone.Parts = parts
			out = append(out, clone)
		}
	}

	return out
}
