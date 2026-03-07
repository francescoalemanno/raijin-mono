package oauth

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

var (
	antigravityClientID = mustDecode(
		"MTA3MTAwNjA2MDU5MS10bWhzc2luMmgyMWxjcmUyMzV2dG9sb2poNGc0MDNlcC5hcHBzLmdvb2dsZXVzZXJjb250ZW50LmNvbQ==",
	)
	antigravityClientSecret = mustDecode("R09DU1BYLUs1OEZXUjQ4NkxkTEoxbUxCOHNYQzR6NnFEQWY=")
	antigravityRedirectURI  = "http://localhost:51121/oauth-callback"
	antigravityScopes       = []string{
		"https://www.googleapis.com/auth/cloud-platform",
		"https://www.googleapis.com/auth/userinfo.email",
		"https://www.googleapis.com/auth/userinfo.profile",
		"https://www.googleapis.com/auth/cclog",
		"https://www.googleapis.com/auth/experimentsandconfigs",
	}
	antigravityAuthURL    = "https://accounts.google.com/o/oauth2/v2/auth"
	antigravityTokenURL   = "https://oauth2.googleapis.com/token"
	antigravityAssistURLs = []string{
		"https://cloudcode-pa.googleapis.com",
		"https://daily-cloudcode-pa.sandbox.googleapis.com",
	}
	antigravityDefaultProject = "rising-fact-p41fc"
)

// RefreshAntigravityToken refreshes an Antigravity OAuth token.
func RefreshAntigravityToken(ctx context.Context, creds Credentials) (Credentials, error) {
	projectID := ""
	if creds.Extra != nil {
		projectID = creds.Extra["projectId"]
	}
	if projectID == "" {
		return Credentials{}, fmt.Errorf("antigravity oauth: missing projectId in credentials")
	}

	vals := url.Values{
		"client_id":     {antigravityClientID},
		"client_secret": {antigravityClientSecret},
		"refresh_token": {creds.Refresh},
		"grant_type":    {"refresh_token"},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, antigravityTokenURL, strings.NewReader(vals.Encode()))
	if err != nil {
		return Credentials{}, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return Credentials{}, fmt.Errorf("antigravity oauth: refresh: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return Credentials{}, fmt.Errorf("antigravity oauth: refresh: status %d", resp.StatusCode)
	}

	var data struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int64  `json:"expires_in"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return Credentials{}, fmt.Errorf("antigravity oauth: refresh: decode: %w", err)
	}

	refresh := data.RefreshToken
	if refresh == "" {
		refresh = creds.Refresh
	}
	return Credentials{
		Refresh: refresh,
		Access:  data.AccessToken,
		Expires: time.Now().UnixMilli() + data.ExpiresIn*1000 - 5*60*1000,
		Extra:   map[string]string{"projectId": projectID},
	}, nil
}

// LoginAntigravity runs the Antigravity OAuth flow.
// It starts a local HTTP server on :51121, then discovers the user's project.
func LoginAntigravity(ctx context.Context, cb LoginCallbacks) (Credentials, error) {
	p, err := generatePKCE()
	if err != nil {
		return Credentials{}, fmt.Errorf("antigravity oauth: pkce: %w", err)
	}

	if cb.OnProgress != nil {
		cb.OnProgress("Starting local server for OAuth callback...")
	}
	srv, err := startCallbackServer("127.0.0.1:51121", "/oauth-callback")
	if err != nil {
		return Credentials{}, fmt.Errorf("antigravity oauth: %w", err)
	}
	defer srv.close()

	params := url.Values{
		"client_id":             {antigravityClientID},
		"response_type":         {"code"},
		"redirect_uri":          {antigravityRedirectURI},
		"scope":                 {strings.Join(antigravityScopes, " ")},
		"code_challenge":        {p.Challenge},
		"code_challenge_method": {"S256"},
		"state":                 {p.Verifier},
		"access_type":           {"offline"},
		"prompt":                {"consent"},
	}
	authURL := antigravityAuthURL + "?" + params.Encode()

	cb.OnAuth(AuthInfo{URL: authURL, Instructions: "Complete the sign-in in your browser."})
	if cb.OnProgress != nil {
		cb.OnProgress("Waiting for OAuth callback...")
	}

	code, err := raceManualInput(
		ctx, srv, cb.OnManualCodeInput,
		parseRedirectURL,
		func(state string) error {
			if state != p.Verifier {
				return fmt.Errorf("antigravity oauth: state mismatch — possible CSRF attack")
			}
			return nil
		},
	)
	if err != nil || code == "" {
		if err == nil {
			err = fmt.Errorf("antigravity oauth: no authorization code received")
		}
		return Credentials{}, err
	}

	// Exchange code for tokens.
	if cb.OnProgress != nil {
		cb.OnProgress("Exchanging authorization code for tokens...")
	}
	vals := url.Values{
		"client_id":     {antigravityClientID},
		"client_secret": {antigravityClientSecret},
		"code":          {code},
		"grant_type":    {"authorization_code"},
		"redirect_uri":  {antigravityRedirectURI},
		"code_verifier": {p.Verifier},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, antigravityTokenURL, strings.NewReader(vals.Encode()))
	if err != nil {
		return Credentials{}, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return Credentials{}, fmt.Errorf("antigravity oauth: exchange: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return Credentials{}, fmt.Errorf("antigravity oauth: exchange: status %d", resp.StatusCode)
	}

	var tokenData struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int64  `json:"expires_in"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&tokenData); err != nil {
		return Credentials{}, fmt.Errorf("antigravity oauth: exchange: decode: %w", err)
	}
	if tokenData.RefreshToken == "" {
		return Credentials{}, fmt.Errorf("antigravity oauth: no refresh token received")
	}

	if cb.OnProgress != nil {
		cb.OnProgress("Getting user info and discovering project...")
	}
	projectID := antigravityDiscoverProject(ctx, tokenData.AccessToken, cb.OnProgress)

	return Credentials{
		Refresh: tokenData.RefreshToken,
		Access:  tokenData.AccessToken,
		Expires: time.Now().UnixMilli() + tokenData.ExpiresIn*1000 - 5*60*1000,
		Extra:   map[string]string{"projectId": projectID},
	}, nil
}

func antigravityDiscoverProject(ctx context.Context, accessToken string, onProgress func(string)) string {
	if onProgress != nil {
		onProgress("Checking for existing project...")
	}

	headers := map[string]string{
		"Authorization":     "Bearer " + accessToken,
		"Content-Type":      "application/json",
		"User-Agent":        "google-api-nodejs-client/9.15.1",
		"X-Goog-Api-Client": "google-cloud-sdk vscode_cloudshelleditor/0.1",
		"Client-Metadata":   `{"ideType":"IDE_UNSPECIFIED","platform":"PLATFORM_UNSPECIFIED","pluginType":"GEMINI"}`,
	}

	loadBody := `{"metadata":{"ideType":"IDE_UNSPECIFIED","platform":"PLATFORM_UNSPECIFIED","pluginType":"GEMINI"}}`

	for _, endpoint := range antigravityAssistURLs {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost,
			endpoint+"/v1internal:loadCodeAssist", strings.NewReader(loadBody))
		if err != nil {
			continue
		}
		for k, v := range headers {
			req.Header.Set(k, v)
		}

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			continue
		}

		if resp.StatusCode == http.StatusOK {
			var payload struct {
				CloudaicompanionProject any `json:"cloudaicompanionProject"`
			}
			if err := json.NewDecoder(resp.Body).Decode(&payload); err == nil {
				resp.Body.Close()
				switch v := payload.CloudaicompanionProject.(type) {
				case string:
					if v != "" {
						return v
					}
				case map[string]any:
					if id, ok := v["id"].(string); ok && id != "" {
						return id
					}
				}
			} else {
				resp.Body.Close()
			}
		} else {
			resp.Body.Close()
		}
	}

	if onProgress != nil {
		onProgress("Using default project...")
	}
	return antigravityDefaultProject
}

// AntigravityProvider is the OAuth Provider for Antigravity (Gemini 3, Claude, GPT-OSS).
var AntigravityProvider Provider = &antigravityProvider{}

type antigravityProvider struct{}

func (a *antigravityProvider) ID() string               { return "google-antigravity" }
func (a *antigravityProvider) Name() string             { return "Antigravity (Gemini 3, Claude, GPT-OSS)" }
func (a *antigravityProvider) UsesCallbackServer() bool { return true }

func (a *antigravityProvider) Login(ctx context.Context, cb LoginCallbacks) (Credentials, error) {
	return LoginAntigravity(ctx, cb)
}

func (a *antigravityProvider) RefreshToken(ctx context.Context, creds Credentials) (Credentials, error) {
	return RefreshAntigravityToken(ctx, creds)
}

func (a *antigravityProvider) APIKey(creds Credentials) string {
	projectID := ""
	if creds.Extra != nil {
		projectID = creds.Extra["projectId"]
	}
	b, _ := json.Marshal(map[string]string{"token": creds.Access, "projectId": projectID})
	return string(b)
}
