package chat

import (
	"encoding/base64"
	"testing"
)

func TestIsRenderableImage(t *testing.T) {
	png := []byte{0x89, 0x50, 0x4e, 0x47}
	encoded := base64.StdEncoding.EncodeToString(png)

	if !IsRenderableImage("image/png", encoded) {
		t.Fatalf("expected renderable png payload")
	}
	if IsRenderableImage("image/svg+xml", encoded) {
		t.Fatalf("expected unsupported image mime to be non-renderable")
	}
}

func TestNormalizeImagePayload(t *testing.T) {
	raw := []byte{0x89, 0x50, 0x4e, 0x47}
	std := base64.StdEncoding.EncodeToString(raw)

	normalized, ok := NormalizeImagePayload("image/png", std)
	if !ok || normalized != std {
		t.Fatalf("expected normalized std base64 payload")
	}

	rawInput := string(raw)
	normalizedRaw, ok := NormalizeImagePayload("image/png", rawInput)
	if !ok {
		t.Fatalf("expected raw binary string payload to normalize")
	}
	decoded, err := base64.StdEncoding.DecodeString(normalizedRaw)
	if err != nil {
		t.Fatalf("decode normalized raw payload: %v", err)
	}
	if string(decoded) != rawInput {
		t.Fatalf("normalized raw payload mismatch")
	}
}
