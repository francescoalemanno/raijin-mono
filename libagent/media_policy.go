package libagent

import (
	"context"
	"strings"
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

func runtimeMediaTransform(support MediaSupport) TransformContextFn {
	if !support.Known || support.Enabled {
		return nil
	}
	return func(_ context.Context, messages []Message) ([]Message, error) {
		return stripUnsupportedMedia(messages), nil
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
				if strings.HasPrefix(f.MediaType, "image/") || strings.HasPrefix(f.MediaType, "video/") {
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
