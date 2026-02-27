package oauth

import (
	"context"
	"net/url"
	"strings"
)

// urlEncode percent-encodes a string for use in query parameters.
func urlEncode(s string) string {
	return url.QueryEscape(s)
}

// raceManualInput waits for a local callback server first, then optionally
// falls back to manual-paste input if no callback code is received.
//
//   - srv is the local callback server already listening.
//   - verifier is the PKCE verifier / expected state value (provider-dependent).
//   - onManualCodeInput (may be nil) returns the raw user input.
//   - parseInput converts raw user input to a {code, state} pair.
//
// Returns the authorization code, or an error if neither path succeeds.
func raceManualInput(
	ctx context.Context,
	srv *callbackServer,
	onManualCodeInput func(context.Context) (string, error),
	parseInput func(string) (code, state string),
	validateState func(state string) error,
) (string, error) {
	// Prefer browser callback path. This avoids concurrent stdin readers that
	// can interfere with active TUIs if the callback arrives first.
	result, ok := srv.waitForCode(ctx)
	if ok && result.Code != "" {
		if err := validateState(result.State); err != nil {
			return "", err
		}
		return result.Code, nil
	}

	if onManualCodeInput == nil {
		return "", context.Canceled
	}

	input, err := onManualCodeInput(ctx)
	if err != nil {
		return "", err
	}
	code, state := parseInput(input)
	if state != "" {
		if err := validateState(state); err != nil {
			return "", err
		}
	}
	if code != "" {
		return code, nil
	}
	return "", context.Canceled
}

// parseRedirectURL extracts code and state from a redirect URL or returns empty strings.
func parseRedirectURL(input string) (code, state string) {
	v := strings.TrimSpace(input)
	if v == "" {
		return "", ""
	}
	u, err := url.Parse(v)
	if err != nil {
		return "", ""
	}
	return u.Query().Get("code"), u.Query().Get("state")
}
