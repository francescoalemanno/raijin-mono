package oauth

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

var (
	copilotClientID = mustDecode("SXYxLmI1MDdhMDhjODdlY2ZlOTg=")

	copilotHeaders = map[string]string{
		"User-Agent":             "GitHubCopilotChat/0.35.0",
		"Editor-Version":         "vscode/1.107.0",
		"Editor-Plugin-Version":  "copilot-chat/0.35.0",
		"Copilot-Integration-Id": "vscode-chat",
	}
)

func copilotURLs(domain string) (deviceCode, accessToken, copilotToken string) {
	deviceCode = "https://" + domain + "/login/device/code"
	accessToken = "https://" + domain + "/login/oauth/access_token"
	copilotToken = "https://api." + domain + "/copilot_internal/v2/token"
	return
}

// NormalizeDomain cleans an enterprise URL/domain string to a bare hostname,
// or returns "" if the input is blank.
func NormalizeDomain(input string) (string, error) {
	trimmed := strings.TrimSpace(input)
	if trimmed == "" {
		return "", nil
	}
	raw := trimmed
	if !strings.Contains(raw, "://") {
		raw = "https://" + raw
	}
	u, err := parseURL(raw)
	if err != nil {
		return "", fmt.Errorf("invalid GitHub Enterprise URL/domain: %w", err)
	}
	return u, nil
}

func parseURL(raw string) (string, error) {
	req, err := http.NewRequest(http.MethodGet, raw, nil)
	if err != nil {
		return "", err
	}
	return req.URL.Host, nil
}

// GetGitHubCopilotBaseURL returns the Copilot API base URL derived from the
// access token's proxy-ep field, falling back to the enterprise or default URL.
func GetGitHubCopilotBaseURL(token, enterpriseDomain string) string {
	if token != "" {
		// Token format: tid=...;exp=...;proxy-ep=proxy.individual.githubcopilot.com;...
		for part := range strings.SplitSeq(token, ";") {
			kv := strings.SplitN(part, "=", 2)
			if len(kv) == 2 && kv[0] == "proxy-ep" {
				apiHost := strings.Replace(kv[1], "proxy.", "api.", 1)
				return "https://" + apiHost
			}
		}
	}
	if enterpriseDomain != "" {
		return "https://copilot-api." + enterpriseDomain
	}
	return "https://api.individual.githubcopilot.com"
}

// RefreshGitHubCopilotToken exchanges a GitHub OAuth access token for a
// short-lived Copilot API token.
func RefreshGitHubCopilotToken(ctx context.Context, refreshToken, enterpriseDomain string) (Credentials, error) {
	domain := "github.com"
	if enterpriseDomain != "" {
		domain = enterpriseDomain
	}
	_, _, copilotTokenURL := copilotURLs(domain)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, copilotTokenURL, nil)
	if err != nil {
		return Credentials{}, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+refreshToken)
	for k, v := range copilotHeaders {
		req.Header.Set(k, v)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return Credentials{}, fmt.Errorf("copilot oauth: refresh: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return Credentials{}, fmt.Errorf("copilot oauth: refresh: status %d", resp.StatusCode)
	}

	var data struct {
		Token     string `json:"token"`
		ExpiresAt int64  `json:"expires_at"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return Credentials{}, fmt.Errorf("copilot oauth: refresh: decode: %w", err)
	}
	if data.Token == "" {
		return Credentials{}, fmt.Errorf("copilot oauth: refresh: empty token")
	}

	extra := map[string]string{}
	if enterpriseDomain != "" {
		extra["enterpriseUrl"] = enterpriseDomain
	}
	return Credentials{
		Refresh: refreshToken,
		Access:  data.Token,
		// expiresAt is in seconds; subtract 5 min buffer
		Expires: data.ExpiresAt*1000 - 5*60*1000,
		Extra:   extra,
	}, nil
}

// LoginGitHubCopilot runs the GitHub device-code OAuth flow and returns
// Copilot credentials ready to persist.
func LoginGitHubCopilot(ctx context.Context, cb LoginCallbacks) (Credentials, error) {
	input, err := cb.OnPrompt(ctx, Prompt{
		Message:     "GitHub Enterprise URL/domain (blank for github.com)",
		Placeholder: "company.ghe.com",
		AllowEmpty:  true,
	})
	if err != nil {
		return Credentials{}, err
	}

	enterpriseDomain, err := NormalizeDomain(input)
	if err != nil {
		return Credentials{}, err
	}
	domain := "github.com"
	if enterpriseDomain != "" {
		domain = enterpriseDomain
	}

	// Start device flow.
	deviceCode, accessTokenURL, _ := copilotURLs(domain)
	device, err := copilotStartDeviceFlow(ctx, deviceCode)
	if err != nil {
		return Credentials{}, err
	}

	cb.OnAuth(AuthInfo{
		URL:          device.VerificationURI,
		Instructions: "Enter code: " + device.UserCode,
	})

	githubToken, err := copilotPollForToken(ctx, accessTokenURL, device)
	if err != nil {
		return Credentials{}, err
	}

	creds, err := RefreshGitHubCopilotToken(ctx, githubToken, enterpriseDomain)
	if err != nil {
		return Credentials{}, err
	}

	if cb.OnProgress != nil {
		cb.OnProgress("Enabling models...")
	}
	copilotEnableAllModels(ctx, creds.Access, enterpriseDomain)

	return creds, nil
}

type deviceCodeResponse struct {
	DeviceCode      string `json:"device_code"`
	UserCode        string `json:"user_code"`
	VerificationURI string `json:"verification_uri"`
	Interval        int    `json:"interval"`
	ExpiresIn       int    `json:"expires_in"`
}

func copilotStartDeviceFlow(ctx context.Context, deviceCodeURL string) (deviceCodeResponse, error) {
	body := fmt.Sprintf(`{"client_id":"%s","scope":"read:user"}`, copilotClientID)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, deviceCodeURL, strings.NewReader(body))
	if err != nil {
		return deviceCodeResponse{}, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "GitHubCopilotChat/0.35.0")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return deviceCodeResponse{}, fmt.Errorf("copilot oauth: device flow: %w", err)
	}
	defer resp.Body.Close()

	var out deviceCodeResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return deviceCodeResponse{}, fmt.Errorf("copilot oauth: device flow: decode: %w", err)
	}
	if out.DeviceCode == "" {
		return deviceCodeResponse{}, fmt.Errorf("copilot oauth: device flow: empty device_code")
	}
	return out, nil
}

func copilotPollForToken(ctx context.Context, accessTokenURL string, device deviceCodeResponse) (string, error) {
	deadline := time.Now().Add(time.Duration(device.ExpiresIn) * time.Second)
	interval := time.Duration(max(device.Interval, 1)) * time.Second

	for time.Now().Before(deadline) {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}

		body := fmt.Sprintf(
			`{"client_id":"%s","device_code":"%s","grant_type":"urn:ietf:params:oauth:grant-type:device_code"}`,
			copilotClientID, device.DeviceCode,
		)
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, accessTokenURL, strings.NewReader(body))
		if err != nil {
			return "", err
		}
		req.Header.Set("Accept", "application/json")
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("User-Agent", "GitHubCopilotChat/0.35.0")

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return "", fmt.Errorf("copilot oauth: poll: %w", err)
		}

		var raw map[string]json.RawMessage
		decErr := json.NewDecoder(resp.Body).Decode(&raw)
		resp.Body.Close()
		if decErr != nil {
			return "", fmt.Errorf("copilot oauth: poll: decode: %w", decErr)
		}

		if tok, ok := raw["access_token"]; ok {
			var token string
			if err := json.Unmarshal(tok, &token); err == nil && token != "" {
				return token, nil
			}
		}

		if errField, ok := raw["error"]; ok {
			var errStr string
			json.Unmarshal(errField, &errStr) //nolint:errcheck
			switch errStr {
			case "authorization_pending":
				// normal — keep polling
			case "slow_down":
				interval += 5 * time.Second
			default:
				return "", fmt.Errorf("copilot oauth: device flow failed: %s", errStr)
			}
		}

		select {
		case <-time.After(interval):
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}
	return "", fmt.Errorf("copilot oauth: device flow timed out")
}

func copilotEnableAllModels(ctx context.Context, token, enterpriseDomain string) {
	// Best-effort: enable all models so they appear in the Copilot account.
	// Failures are silently ignored.
	baseURL := GetGitHubCopilotBaseURL(token, enterpriseDomain)
	// We don't have the model list here; callers that need fine-grained control
	// can call copilotEnableModel directly.
	_ = baseURL
}

// CopilotEnableModel enables a specific model in the user's Copilot account.
// This is required for non-default models (e.g. Claude, Grok) before first use.
// Failures are returned but are typically non-fatal.
func CopilotEnableModel(ctx context.Context, token, modelID, enterpriseDomain string) error {
	baseURL := GetGitHubCopilotBaseURL(token, enterpriseDomain)
	reqURL := baseURL + "/models/" + modelID + "/policy"

	body := `{"state":"enabled"}`
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, reqURL, strings.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("openai-intent", "chat-policy")
	req.Header.Set("x-interaction-type", "chat-policy")
	for k, v := range copilotHeaders {
		req.Header.Set(k, v)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("copilot: enable model %q: status %d", modelID, resp.StatusCode)
	}
	return nil
}

// GitHubCopilotProvider is the OAuth Provider for GitHub Copilot.
var GitHubCopilotProvider Provider = &gitHubCopilotProvider{}

type gitHubCopilotProvider struct{}

func (g *gitHubCopilotProvider) ID() string               { return "github-copilot" }
func (g *gitHubCopilotProvider) Name() string             { return "GitHub Copilot" }
func (g *gitHubCopilotProvider) UsesCallbackServer() bool { return false }

func (g *gitHubCopilotProvider) Login(ctx context.Context, cb LoginCallbacks) (Credentials, error) {
	return LoginGitHubCopilot(ctx, cb)
}

func (g *gitHubCopilotProvider) RefreshToken(ctx context.Context, creds Credentials) (Credentials, error) {
	enterprise := ""
	if creds.Extra != nil {
		enterprise = creds.Extra["enterpriseUrl"]
	}
	return RefreshGitHubCopilotToken(ctx, creds.Refresh, enterprise)
}

func (g *gitHubCopilotProvider) APIKey(creds Credentials) string {
	return creds.Access
}
