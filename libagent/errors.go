package libagent

import (
	"errors"
	"fmt"
	"strings"

	"charm.land/fantasy"
)

const maxProviderErrorBodyLen = 4096

// FormatErrorForCLI returns a user-facing error string.
// For provider errors it includes HTTP status, request id (when available),
// and the raw provider response body for easier debugging.
func FormatErrorForCLI(err error) string {
	if err == nil {
		return ""
	}

	var providerErr *fantasy.ProviderError
	if !errors.As(err, &providerErr) {
		return err.Error()
	}

	var b strings.Builder
	b.WriteString(err.Error())

	if providerErr.StatusCode != 0 {
		b.WriteString(fmt.Sprintf(" (status %d)", providerErr.StatusCode))
	}

	if requestID := providerRequestID(providerErr.ResponseHeaders); requestID != "" {
		b.WriteString("\nrequest_id: ")
		b.WriteString(requestID)
	}

	body := normalizeProviderResponseBody(providerErr.ResponseBody)
	if body != "" {
		b.WriteString("\nprovider_response:\n")
		b.WriteString(body)
	}

	return b.String()
}

func providerRequestID(headers map[string]string) string {
	if len(headers) == 0 {
		return ""
	}

	keys := [...]string{
		"x-request-id",
		"request-id",
		"openai-request-id",
	}
	for _, k := range keys {
		if v := strings.TrimSpace(headers[k]); v != "" {
			return v
		}
	}
	return ""
}

func normalizeProviderResponseBody(raw []byte) string {
	if len(raw) == 0 {
		return ""
	}

	s := strings.TrimSpace(string(raw))
	if s == "" {
		return ""
	}

	// openai-go DumpResponse(true) includes status line + headers + body.
	if idx := strings.Index(s, "\r\n\r\n"); idx >= 0 {
		s = strings.TrimSpace(s[idx+4:])
	} else if idx := strings.Index(s, "\n\n"); idx >= 0 {
		s = strings.TrimSpace(s[idx+2:])
	}

	if len(s) > maxProviderErrorBodyLen {
		s = s[:maxProviderErrorBodyLen] + "...(truncated)"
	}
	return s
}
