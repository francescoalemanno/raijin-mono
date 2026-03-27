package libagent

import (
	"context"
	"strings"
)

const (
	excisedAttachmentText = "[ATTACHMENT EXCISED, if needed re-read it to see it again]"
)

func mediaSupportFromModelInfo(info ModelInfo) MediaSupport {
	known := len(info.Capabilities) > 0 || info.SupportsImages
	if !known {
		known = strings.TrimSpace(info.ProviderID) != "" && strings.TrimSpace(info.ModelID) != ""
	}
	return MediaSupport{
		Known:   known,
		Enabled: info.HasCapability(ModelCapabilityImage) || info.SupportsImages,
	}
}

func runtimeMediaTransform(support MediaSupport, maxImages int) TransformContextFn {
	return func(_ context.Context, messages []Message) ([]Message, error) {
		out := limitImageAttachments(messages, maxImages)
		if support.Known && !support.Enabled {
			out = stripUnsupportedMedia(out)
		}
		return out, nil
	}
}

func composeTransformContext(base, extra TransformContextFn) TransformContextFn {
	if base == nil {
		return extra
	}
	if extra == nil {
		return base
	}
	return func(ctx context.Context, messages []Message) ([]Message, error) {
		out, err := base(ctx, messages)
		if err != nil {
			return nil, err
		}
		return extra(ctx, out)
	}
}

func stripUnsupportedMedia(messages []Message) []Message {
	out := make([]Message, 0, len(messages))
	for _, msg := range messages {
		switch m := msg.(type) {
		case *UserMessage:
			filtered := make([]FilePart, 0, len(m.Files))
			for _, f := range m.Files {
				mediaType := strings.ToLower(strings.TrimSpace(f.MediaType))
				if strings.HasPrefix(mediaType, "image/") || strings.HasPrefix(mediaType, "video/") {
					continue
				}
				filtered = append(filtered, f)
			}
			out = append(out, &UserMessage{
				Role:      m.Role,
				Content:   m.Content,
				Files:     filtered,
				Timestamp: m.Timestamp,
			})
		case *ToolResultMessage:
			if len(m.Data) > 0 {
				out = append(out, &ToolResultMessage{
					Role:       m.Role,
					ToolCallID: m.ToolCallID,
					ToolName:   m.ToolName,
					Content:    "[Image/media tool result omitted: selected model does not support image/media inputs]",
					IsError:    m.IsError,
					Timestamp:  m.Timestamp,
				})
			} else {
				out = append(out, m)
			}
		default:
			out = append(out, msg)
		}
	}
	return out
}

func limitImageAttachments(messages []Message, maxImages int) []Message {
	if maxImages < 0 || len(messages) == 0 {
		return messages
	}

	remaining := maxImages
	out := make([]Message, len(messages))
	changed := false

	for msgIdx := len(messages) - 1; msgIdx >= 0; msgIdx-- {
		msg := messages[msgIdx]
		um, ok := msg.(*UserMessage)
		if !ok || len(um.Files) == 0 {
			out[msgIdx] = msg
			continue
		}

		keep := make([]bool, len(um.Files))
		excised := 0
		for fileIdx := len(um.Files) - 1; fileIdx >= 0; fileIdx-- {
			f := um.Files[fileIdx]
			if !strings.HasPrefix(strings.ToLower(strings.TrimSpace(f.MediaType)), "image/") {
				keep[fileIdx] = true
				continue
			}
			if remaining > 0 {
				keep[fileIdx] = true
				remaining--
				continue
			}
			excised++
		}
		if excised == 0 {
			out[msgIdx] = msg
			continue
		}

		clone := CloneMessage(um).(*UserMessage)
		filtered := make([]FilePart, 0, len(clone.Files)-excised)
		for fileIdx, f := range clone.Files {
			if keep[fileIdx] {
				filtered = append(filtered, f)
			}
		}
		clone.Files = filtered
		excisedText := strings.TrimRight(strings.Repeat(excisedAttachmentText+"\n", excised), "\n")
		if strings.TrimSpace(clone.Content) == "" {
			clone.Content = excisedText
		} else {
			clone.Content = strings.TrimRight(clone.Content, "\n") + "\n" + excisedText
		}
		out[msgIdx] = clone
		changed = true
	}

	if !changed {
		return messages
	}
	return out
}
