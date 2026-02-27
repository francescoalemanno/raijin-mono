package oauth

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

var (
	anthropicClientID     = mustDecode("OWQxYzI1MGEtZTYxYi00NGQ5LTg4ZWQtNTk0NGQxOTYyZjVl")
	anthropicAuthorizeURL = "https://claude.ai/oauth/authorize"
	anthropicTokenURL     = "https://console.anthropic.com/v1/oauth/token"
	anthropicRedirectURI  = "https://console.anthropic.com/oauth/code/callback"
	anthropicScopes       = "org:create_api_key user:profile user:inference"
)

// LoginAnthropic runs the Anthropic PKCE authorization-code flow.
// It calls onAuth with the URL to open, then calls OnPrompt asking the user
// to paste the authorization code (format "code#state") from the redirect.
func LoginAnthropic(ctx context.Context, cb LoginCallbacks) (Credentials, error) {
	p, err := generatePKCE()
	if err != nil {
		return Credentials{}, fmt.Errorf("anthropic oauth: generate pkce: %w", err)
	}

	params := fmt.Sprintf(
		"code=true&client_id=%s&response_type=code&redirect_uri=%s&scope=%s&code_challenge=%s&code_challenge_method=S256&state=%s",
		anthropicClientID, urlEncode(anthropicRedirectURI), urlEncode(anthropicScopes),
		p.Challenge, p.Verifier,
	)
	authURL := anthropicAuthorizeURL + "?" + params

	cb.OnAuth(AuthInfo{URL: authURL})

	raw, err := cb.OnPrompt(ctx, Prompt{Message: "Paste the authorization code (format: code#state):"})
	if err != nil {
		return Credentials{}, fmt.Errorf("anthropic oauth: prompt: %w", err)
	}

	parts := strings.SplitN(strings.TrimSpace(raw), "#", 2)
	code := parts[0]
	state := ""
	if len(parts) == 2 {
		state = parts[1]
	}
	_ = state // state is embedded in the verifier; server validates it

	creds, err := anthropicExchangeCode(ctx, code, p.Verifier)
	if err != nil {
		return Credentials{}, err
	}
	return creds, nil
}

// RefreshAnthropicToken exchanges a refresh token for fresh credentials.
func RefreshAnthropicToken(ctx context.Context, creds Credentials) (Credentials, error) {
	body := fmt.Sprintf(`{"grant_type":"refresh_token","client_id":"%s","refresh_token":"%s"}`,
		anthropicClientID, creds.Refresh)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, anthropicTokenURL, strings.NewReader(body))
	if err != nil {
		return Credentials{}, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return Credentials{}, fmt.Errorf("anthropic oauth: refresh: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return Credentials{}, fmt.Errorf("anthropic oauth: refresh: status %d", resp.StatusCode)
	}

	var data struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int64  `json:"expires_in"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return Credentials{}, fmt.Errorf("anthropic oauth: refresh: decode: %w", err)
	}

	return Credentials{
		Refresh: data.RefreshToken,
		Access:  data.AccessToken,
		Expires: time.Now().UnixMilli() + data.ExpiresIn*1000 - 5*60*1000,
	}, nil
}

func anthropicExchangeCode(ctx context.Context, code, verifier string) (Credentials, error) {
	body := fmt.Sprintf(
		`{"grant_type":"authorization_code","client_id":"%s","code":"%s","redirect_uri":"%s","code_verifier":"%s"}`,
		anthropicClientID, code, anthropicRedirectURI, verifier,
	)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, anthropicTokenURL, strings.NewReader(body))
	if err != nil {
		return Credentials{}, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return Credentials{}, fmt.Errorf("anthropic oauth: exchange: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return Credentials{}, fmt.Errorf("anthropic oauth: exchange: status %d", resp.StatusCode)
	}

	var data struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int64  `json:"expires_in"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return Credentials{}, fmt.Errorf("anthropic oauth: exchange: decode: %w", err)
	}

	return Credentials{
		Refresh: data.RefreshToken,
		Access:  data.AccessToken,
		Expires: time.Now().UnixMilli() + data.ExpiresIn*1000 - 5*60*1000,
	}, nil
}

// AnthropicProvider is the OAuthProvider for Anthropic (Claude Pro/Max).
var AnthropicProvider Provider = &anthropicProvider{}

type anthropicProvider struct{}

func (a *anthropicProvider) ID() string               { return "anthropic" }
func (a *anthropicProvider) Name() string             { return "Anthropic (Claude Pro/Max)" }
func (a *anthropicProvider) UsesCallbackServer() bool { return false }

func (a *anthropicProvider) Login(ctx context.Context, cb LoginCallbacks) (Credentials, error) {
	return LoginAnthropic(ctx, cb)
}

func (a *anthropicProvider) RefreshToken(ctx context.Context, creds Credentials) (Credentials, error) {
	return RefreshAnthropicToken(ctx, creds)
}

func (a *anthropicProvider) APIKey(creds Credentials) string {
	return creds.Access
}

// mustDecode base64-decodes a string at init time; panics on invalid input.
func mustDecode(s string) string {
	b, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		panic("oauth: invalid base64 constant: " + err.Error())
	}
	return string(b)
}
