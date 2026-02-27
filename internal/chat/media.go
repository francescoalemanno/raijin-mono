package chat

import (
	"encoding/base64"
	"strings"
)

// Payload is a normalized media payload used across chat/runtime/UI boundaries.
type Payload struct {
	MIMEType string
	Data     string // base64-encoded payload
	Source   string
}

// IsSupportedImageMIME reports whether mimeType is one of the image types
// currently supported by the read tool and terminal image renderer.
func IsSupportedImageMIME(mimeType string) bool {
	switch strings.ToLower(strings.TrimSpace(mimeType)) {
	case "image/png", "image/jpeg", "image/gif", "image/webp":
		return true
	default:
		return false
	}
}

// NormalizeImagePayload returns a normalized base64 payload for supported images.
// It accepts standard/URL-safe base64 and also raw byte strings.
func NormalizeImagePayload(mimeType, data string) (string, bool) {
	if !IsSupportedImageMIME(mimeType) {
		return "", false
	}
	trimmed := strings.TrimSpace(data)
	if trimmed == "" {
		return "", false
	}

	if decoded, ok := decodeBase64Flexible(trimmed); ok {
		return base64.StdEncoding.EncodeToString(decoded), true
	}

	// Fallback: treat incoming string bytes as raw binary payload.
	raw := []byte(data)
	if len(raw) == 0 {
		return "", false
	}
	return base64.StdEncoding.EncodeToString(raw), true
}

// IsRenderableImage reports whether the payload is a supported image with usable data.
func IsRenderableImage(mimeType, data string) bool {
	_, ok := NormalizeImagePayload(mimeType, data)
	return ok
}

func decodeBase64Flexible(data string) ([]byte, bool) {
	if decoded, err := base64.StdEncoding.DecodeString(data); err == nil {
		return decoded, true
	}
	if decoded, err := base64.RawStdEncoding.DecodeString(data); err == nil {
		return decoded, true
	}
	if decoded, err := base64.URLEncoding.DecodeString(data); err == nil {
		return decoded, true
	}
	if decoded, err := base64.RawURLEncoding.DecodeString(data); err == nil {
		return decoded, true
	}
	return nil, false
}
