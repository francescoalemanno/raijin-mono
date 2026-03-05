package libagent_test

import (
	"errors"
	"strings"
	"testing"

	"charm.land/fantasy"
	"github.com/francescoalemanno/raijin-mono/libagent"
)

func TestFormatErrorForCLI_NonProviderError(t *testing.T) {
	got := libagent.FormatErrorForCLI(errors.New("plain"))
	if got != "plain" {
		t.Fatalf("got %q want %q", got, "plain")
	}
}

func TestFormatErrorForCLI_ProviderErrorIncludesBodyAndRequestID(t *testing.T) {
	err := &fantasy.ProviderError{
		Title:      "bad request",
		Message:    "Provider returned error",
		StatusCode: 400,
		ResponseHeaders: map[string]string{
			"x-request-id": "req_123",
		},
		ResponseBody: []byte("HTTP/1.1 400 Bad Request\r\ncontent-type: application/json\r\n\r\n{\"error\":\"boom\"}"),
	}

	got := libagent.FormatErrorForCLI(err)
	if got == "" {
		t.Fatal("expected non-empty output")
	}
	if !containsAll(got, []string{
		"bad request: Provider returned error",
		"(status 400)",
		"request_id: req_123",
		"provider_response:",
		"{\"error\":\"boom\"}",
	}) {
		t.Fatalf("unexpected output:\n%s", got)
	}
}

func containsAll(s string, parts []string) bool {
	for _, p := range parts {
		if !strings.Contains(s, p) {
			return false
		}
	}
	return true
}
