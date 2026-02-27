// Package oauth provides OAuth login and token-refresh flows for AI providers
// that require OAuth rather than plain API keys:
//   - Anthropic (Claude Pro/Max)
//   - GitHub Copilot
//   - Google Gemini CLI (Cloud Code Assist)
//   - Google Antigravity (Gemini 3, Claude, GPT-OSS via Google Cloud)
//   - OpenAI Codex (ChatGPT Plus/Pro)
package oauth

import "context"

// Credentials holds a persisted OAuth token pair plus expiry and any
// provider-specific extra fields (e.g. projectId for Google providers).
type Credentials struct {
	// Refresh is the long-lived refresh token.
	Refresh string `json:"refresh"`
	// Access is the short-lived access token.
	Access string `json:"access"`
	// Expires is the Unix-millisecond timestamp after which Access is stale.
	Expires int64 `json:"expires"`
	// Extra holds provider-specific fields (e.g. "projectId", "enterpriseUrl", "accountId").
	Extra map[string]string `json:"extra,omitempty"`
}

// AuthInfo is passed to LoginCallbacks.OnAuth to tell the caller which URL to open.
type AuthInfo struct {
	// URL is the authorization URL the user must visit.
	URL string
	// Instructions is an optional human-readable hint (e.g. "Enter code: XXXX-YYYY").
	Instructions string
}

// Prompt describes an input the login flow needs from the user.
type Prompt struct {
	// Message is the question/label shown to the user.
	Message string
	// Placeholder is an optional example value.
	Placeholder string
	// AllowEmpty allows the user to submit an empty response.
	AllowEmpty bool
}

// LoginCallbacks is the set of UI hooks the login flows call during authentication.
// The caller provides implementations that render prompts and progress in their UI.
type LoginCallbacks struct {
	// OnAuth is called when the flow has a URL for the user to open.
	OnAuth func(AuthInfo)
	// OnPrompt is called when the flow needs text input from the user.
	OnPrompt func(context.Context, Prompt) (string, error)
	// OnProgress is called with status messages during the flow (optional).
	OnProgress func(string)
	// OnManualCodeInput, if non-nil, races with any local callback server.
	// It should return the redirect URL (or bare code) that the user pastes.
	// Whichever wins (server callback or manual paste) is used.
	OnManualCodeInput func(context.Context) (string, error)
}

// Provider is the interface every OAuth provider implements.
type Provider interface {
	// ID is the stable provider identifier string (e.g. "github-copilot").
	ID() string
	// Name is the human-readable provider name.
	Name() string
	// UsesCallbackServer reports whether the login flow starts a local HTTP server.
	// When true, callers may want to show a manual-paste fallback.
	UsesCallbackServer() bool
	// Login runs the full login flow and returns credentials to persist.
	Login(ctx context.Context, cb LoginCallbacks) (Credentials, error)
	// RefreshToken exchanges stale credentials for fresh ones.
	RefreshToken(ctx context.Context, creds Credentials) (Credentials, error)
	// APIKey converts stored credentials to the API key string the fantasy
	// provider expects (e.g. a raw token, or JSON-encoded token+projectId).
	APIKey(creds Credentials) string
}
