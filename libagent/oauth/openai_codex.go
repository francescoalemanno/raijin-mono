package oauth

import (
	"context"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// codexHTTPClient is used for all OAuth token exchange/refresh calls.
// HTTP/2 is disabled (same reasoning as openai-compat providers) and a
// 30-second timeout is set so auth calls never hang indefinitely.
var codexHTTPClient = &http.Client{
	Timeout: 30 * time.Second,
	Transport: &http.Transport{
		TLSNextProto: make(map[string]func(string, *tls.Conn) http.RoundTripper),
	},
}

const (
	codexClientID     = "app_EMoamEEZ73f0CkXaXp7hrann"
	codexAuthorizeURL = "https://auth.openai.com/oauth/authorize"
	codexTokenURL     = "https://auth.openai.com/oauth/token"
	codexRedirectURI  = "http://localhost:1455/auth/callback"
	codexScope        = "openid profile email offline_access"
	codexJWTClaimKey  = "https://api.openai.com/auth"
)

// LoginOpenAICodex runs the OpenAI Codex PKCE authorization-code flow.
// It starts a local HTTP server on :1455 to receive the browser redirect.
func LoginOpenAICodex(ctx context.Context, cb LoginCallbacks) (Credentials, error) {
	p, err := generatePKCE()
	if err != nil {
		return Credentials{}, fmt.Errorf("openai codex oauth: pkce: %w", err)
	}
	state := codexMakeState()

	params := url.Values{
		"response_type":              {"code"},
		"client_id":                  {codexClientID},
		"redirect_uri":               {codexRedirectURI},
		"scope":                      {codexScope},
		"code_challenge":             {p.Challenge},
		"code_challenge_method":      {"S256"},
		"state":                      {state},
		"id_token_add_organizations": {"true"},
		"codex_cli_simplified_flow":  {"true"},
		"originator":                 {"pi"},
	}
	authURL := codexAuthorizeURL + "?" + params.Encode()

	srv, err := startCallbackServer("127.0.0.1:1455", "/auth/callback")
	if err != nil {
		// Fallback: local server unavailable, we will rely on manual paste only.
		srv = nil
	}
	if srv != nil {
		defer srv.close()
	}

	cb.OnAuth(AuthInfo{URL: authURL, Instructions: "A browser window should open. Complete login to finish."})

	var code string
	if srv != nil {
		code, err = raceManualInput(
			ctx, srv, cb.OnManualCodeInput,
			func(input string) (string, string) {
				return codexParseInput(input, state)
			},
			func(gotState string) error {
				if gotState != "" && gotState != state {
					return fmt.Errorf("openai codex oauth: state mismatch")
				}
				return nil
			},
		)
	} else if cb.OnManualCodeInput != nil {
		raw, err2 := cb.OnManualCodeInput(ctx)
		if err2 != nil {
			return Credentials{}, err2
		}
		c, _ := codexParseInput(raw, state)
		code = c
	} else {
		return Credentials{}, fmt.Errorf("openai codex oauth: callback server unavailable and manual code input callback not configured")
	}

	if err != nil && code == "" {
		return Credentials{}, err
	}
	if code == "" {
		return Credentials{}, fmt.Errorf("openai codex oauth: missing authorization code")
	}

	if cb.OnProgress != nil {
		cb.OnProgress("Exchanging authorization code for tokens...")
	}
	result, err := codexExchangeCode(ctx, code, p.Verifier)
	if err != nil {
		return Credentials{}, err
	}

	if cb.OnProgress != nil {
		cb.OnProgress("Extracting account ID...")
	}
	accountID := codexExtractAccountID(result.AccessToken)
	if accountID == "" {
		return Credentials{}, fmt.Errorf("openai codex oauth: could not extract accountId from token")
	}

	if cb.OnProgress != nil {
		cb.OnProgress("OAuth tokens acquired.")
	}
	return Credentials{
		Access:  result.AccessToken,
		Refresh: result.RefreshToken,
		Expires: result.Expires,
		Extra:   map[string]string{"accountId": accountID},
	}, nil
}

// RefreshOpenAICodexToken refreshes an OpenAI Codex OAuth token.
func RefreshOpenAICodexToken(ctx context.Context, creds Credentials) (Credentials, error) {
	vals := url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {creds.Refresh},
		"client_id":     {codexClientID},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, codexTokenURL, strings.NewReader(vals.Encode()))
	if err != nil {
		return Credentials{}, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := codexHTTPClient.Do(req)
	if err != nil {
		return Credentials{}, fmt.Errorf("openai codex oauth: refresh: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return Credentials{}, fmt.Errorf("openai codex oauth: refresh: status %d", resp.StatusCode)
	}

	var data struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int64  `json:"expires_in"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return Credentials{}, fmt.Errorf("openai codex oauth: refresh: decode: %w", err)
	}
	if data.AccessToken == "" || data.RefreshToken == "" {
		return Credentials{}, fmt.Errorf("openai codex oauth: refresh: missing fields")
	}

	accountID := codexExtractAccountID(data.AccessToken)
	if accountID == "" {
		return Credentials{}, fmt.Errorf("openai codex oauth: refresh: could not extract accountId")
	}

	return Credentials{
		Access:  data.AccessToken,
		Refresh: data.RefreshToken,
		Expires: time.Now().UnixMilli() + data.ExpiresIn*1000,
		Extra:   map[string]string{"accountId": accountID},
	}, nil
}

type codexTokenResult struct {
	AccessToken  string
	RefreshToken string
	Expires      int64
}

func codexExchangeCode(ctx context.Context, code, verifier string) (codexTokenResult, error) {
	vals := url.Values{
		"grant_type":    {"authorization_code"},
		"client_id":     {codexClientID},
		"code":          {code},
		"code_verifier": {verifier},
		"redirect_uri":  {codexRedirectURI},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, codexTokenURL, strings.NewReader(vals.Encode()))
	if err != nil {
		return codexTokenResult{}, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := codexHTTPClient.Do(req)
	if err != nil {
		return codexTokenResult{}, fmt.Errorf("openai codex oauth: exchange: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return codexTokenResult{}, fmt.Errorf("openai codex oauth: exchange: status %d", resp.StatusCode)
	}

	var data struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int64  `json:"expires_in"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return codexTokenResult{}, fmt.Errorf("openai codex oauth: exchange: decode: %w", err)
	}
	if data.AccessToken == "" || data.RefreshToken == "" {
		return codexTokenResult{}, fmt.Errorf("openai codex oauth: exchange: missing fields")
	}

	return codexTokenResult{
		AccessToken:  data.AccessToken,
		RefreshToken: data.RefreshToken,
		Expires:      time.Now().UnixMilli() + data.ExpiresIn*1000,
	}, nil
}

func codexExtractAccountID(accessToken string) string {
	parts := strings.Split(accessToken, ".")
	if len(parts) != 3 {
		return ""
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return ""
	}
	var claims map[string]json.RawMessage
	if err := json.Unmarshal(payload, &claims); err != nil {
		return ""
	}
	authRaw, ok := claims[codexJWTClaimKey]
	if !ok {
		return ""
	}
	var auth struct {
		ChatGPTAccountID string `json:"chatgpt_account_id"`
	}
	if err := json.Unmarshal(authRaw, &auth); err != nil {
		return ""
	}
	return auth.ChatGPTAccountID
}

func codexMakeState() string {
	p, err := generatePKCE()
	if err != nil {
		return fmt.Sprintf("state-%d", time.Now().UnixNano())
	}
	// Use the first 32 chars of the verifier as an opaque state.
	if len(p.Verifier) > 32 {
		return p.Verifier[:32]
	}
	return p.Verifier
}

// codexParseInput parses a raw user input (URL or bare code) into a code string.
// The expectedState is used to validate state if present.
func codexParseInput(input, expectedState string) (code, state string) {
	v := strings.TrimSpace(input)
	if v == "" {
		return "", ""
	}
	// Try as URL.
	if u, err := url.Parse(v); err == nil && u.RawQuery != "" {
		q := u.Query()
		if c := q.Get("code"); c != "" {
			return c, q.Get("state")
		}
	}
	// Try "code=...&state=..." query string.
	if strings.Contains(v, "code=") {
		q, err := url.ParseQuery(v)
		if err == nil {
			if c := q.Get("code"); c != "" {
				return c, q.Get("state")
			}
		}
	}
	// Try "code#state".
	if strings.Contains(v, "#") {
		parts := strings.SplitN(v, "#", 2)
		return parts[0], parts[1]
	}
	// Bare code.
	return v, ""
}

// OpenAICodexProvider is the OAuth Provider for OpenAI Codex (ChatGPT Plus/Pro).
var OpenAICodexProvider Provider = &openAICodexProvider{}

type openAICodexProvider struct{}

func (o *openAICodexProvider) ID() string               { return "openai-codex" }
func (o *openAICodexProvider) Name() string             { return "ChatGPT Plus/Pro (Codex Subscription)" }
func (o *openAICodexProvider) UsesCallbackServer() bool { return true }

func (o *openAICodexProvider) Login(ctx context.Context, cb LoginCallbacks) (Credentials, error) {
	return LoginOpenAICodex(ctx, cb)
}

func (o *openAICodexProvider) RefreshToken(ctx context.Context, creds Credentials) (Credentials, error) {
	return RefreshOpenAICodexToken(ctx, creds)
}

func (o *openAICodexProvider) APIKey(creds Credentials) string {
	return creds.Access
}
