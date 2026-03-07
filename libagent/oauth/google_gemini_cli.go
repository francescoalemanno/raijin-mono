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
	geminiCliClientID = mustDecode(
		"NjgxMjU1ODA5Mzk1LW9vOGZ0Mm9wcmRybnA5ZTNhcWY2YXYzaG1kaWIxMzVqLmFwcHMuZ29vZ2xldXNlcmNvbnRlbnQuY29t",
	)
	geminiCliClientSecret = mustDecode("R09DU1BYLTR1SGdNUG0tMW83U2stZ2VWNkN1NWNsWEZzeGw=")
	geminiCliRedirectURI  = "http://localhost:8085/oauth2callback"
	geminiCliScopes       = []string{
		"https://www.googleapis.com/auth/cloud-platform",
		"https://www.googleapis.com/auth/userinfo.email",
		"https://www.googleapis.com/auth/userinfo.profile",
	}
	geminiCliAuthURL   = "https://accounts.google.com/o/oauth2/v2/auth"
	geminiCliTokenURL  = "https://oauth2.googleapis.com/token"
	geminiCliAssistURL = "https://cloudcode-pa.googleapis.com"
)

// RefreshGeminiCliToken refreshes a Google Cloud Code Assist OAuth token.
func RefreshGeminiCliToken(ctx context.Context, creds Credentials) (Credentials, error) {
	projectID := ""
	if creds.Extra != nil {
		projectID = creds.Extra["projectId"]
	}
	if projectID == "" {
		return Credentials{}, fmt.Errorf("gemini cli oauth: missing projectId in credentials")
	}

	vals := url.Values{
		"client_id":     {geminiCliClientID},
		"client_secret": {geminiCliClientSecret},
		"refresh_token": {creds.Refresh},
		"grant_type":    {"refresh_token"},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, geminiCliTokenURL, strings.NewReader(vals.Encode()))
	if err != nil {
		return Credentials{}, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return Credentials{}, fmt.Errorf("gemini cli oauth: refresh: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return Credentials{}, fmt.Errorf("gemini cli oauth: refresh: status %d", resp.StatusCode)
	}

	var data struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int64  `json:"expires_in"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return Credentials{}, fmt.Errorf("gemini cli oauth: refresh: decode: %w", err)
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

// LoginGeminiCli runs the Google Cloud Code Assist OAuth flow.
// It starts a local HTTP server on :8085 to receive the browser redirect,
// then discovers or provisions the user's Cloud project.
func LoginGeminiCli(ctx context.Context, cb LoginCallbacks) (Credentials, error) {
	p, err := generatePKCE()
	if err != nil {
		return Credentials{}, fmt.Errorf("gemini cli oauth: pkce: %w", err)
	}

	if cb.OnProgress != nil {
		cb.OnProgress("Starting local server for OAuth callback...")
	}
	srv, err := startCallbackServer("127.0.0.1:8085", "/oauth2callback")
	if err != nil {
		return Credentials{}, fmt.Errorf("gemini cli oauth: %w", err)
	}
	defer srv.close()

	params := url.Values{
		"client_id":             {geminiCliClientID},
		"response_type":         {"code"},
		"redirect_uri":          {geminiCliRedirectURI},
		"scope":                 {strings.Join(geminiCliScopes, " ")},
		"code_challenge":        {p.Challenge},
		"code_challenge_method": {"S256"},
		"state":                 {p.Verifier},
		"access_type":           {"offline"},
		"prompt":                {"consent"},
	}
	authURL := geminiCliAuthURL + "?" + params.Encode()

	cb.OnAuth(AuthInfo{URL: authURL, Instructions: "Complete the sign-in in your browser."})
	if cb.OnProgress != nil {
		cb.OnProgress("Waiting for OAuth callback...")
	}

	code, err := raceManualInput(
		ctx, srv, cb.OnManualCodeInput,
		parseRedirectURL,
		func(state string) error {
			if state != p.Verifier {
				return fmt.Errorf("gemini cli oauth: state mismatch — possible CSRF attack")
			}
			return nil
		},
	)
	if err != nil || code == "" {
		if err == nil {
			err = fmt.Errorf("gemini cli oauth: no authorization code received")
		}
		return Credentials{}, err
	}

	// Exchange code for tokens.
	if cb.OnProgress != nil {
		cb.OnProgress("Exchanging authorization code for tokens...")
	}
	vals := url.Values{
		"client_id":     {geminiCliClientID},
		"client_secret": {geminiCliClientSecret},
		"code":          {code},
		"grant_type":    {"authorization_code"},
		"redirect_uri":  {geminiCliRedirectURI},
		"code_verifier": {p.Verifier},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, geminiCliTokenURL, strings.NewReader(vals.Encode()))
	if err != nil {
		return Credentials{}, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return Credentials{}, fmt.Errorf("gemini cli oauth: exchange: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return Credentials{}, fmt.Errorf("gemini cli oauth: exchange: status %d", resp.StatusCode)
	}

	var tokenData struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int64  `json:"expires_in"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&tokenData); err != nil {
		return Credentials{}, fmt.Errorf("gemini cli oauth: exchange: decode: %w", err)
	}
	if tokenData.RefreshToken == "" {
		return Credentials{}, fmt.Errorf("gemini cli oauth: no refresh token received")
	}

	if cb.OnProgress != nil {
		cb.OnProgress("Getting user info...")
	}
	projectID, err := geminiCliDiscoverProject(ctx, tokenData.AccessToken, cb.OnProgress)
	if err != nil {
		return Credentials{}, err
	}

	return Credentials{
		Refresh: tokenData.RefreshToken,
		Access:  tokenData.AccessToken,
		Expires: time.Now().UnixMilli() + tokenData.ExpiresIn*1000 - 5*60*1000,
		Extra:   map[string]string{"projectId": projectID},
	}, nil
}

func geminiCliDiscoverProject(ctx context.Context, accessToken string, onProgress func(string)) (string, error) {
	if onProgress != nil {
		onProgress("Checking for existing Cloud Code Assist project...")
	}

	envProjectID := ""
	// Respect standard Google Cloud env vars.
	for _, k := range []string{"GOOGLE_CLOUD_PROJECT", "GOOGLE_CLOUD_PROJECT_ID"} {
		// We avoid os.Getenv import cycle by using a helper; in practice this
		// is fine as a direct call since we are in a library.
		if v := googleCloudEnv(k); v != "" {
			envProjectID = v
			break
		}
	}

	headers := map[string]string{
		"Authorization":     "Bearer " + accessToken,
		"Content-Type":      "application/json",
		"User-Agent":        "google-api-nodejs-client/9.15.1",
		"X-Goog-Api-Client": "gl-node/22.17.0",
	}

	loadBody := map[string]any{
		"cloudaicompanionProject": envProjectID,
		"metadata": map[string]string{
			"ideType":     "IDE_UNSPECIFIED",
			"platform":    "PLATFORM_UNSPECIFIED",
			"pluginType":  "GEMINI",
			"duetProject": envProjectID,
		},
	}
	bodyBytes, _ := json.Marshal(loadBody)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		geminiCliAssistURL+"/v1internal:loadCodeAssist", strings.NewReader(string(bodyBytes)))
	if err != nil {
		return "", err
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("gemini cli oauth: discover project: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK {
		var payload struct {
			CloudaicompanionProject string `json:"cloudaicompanionProject"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&payload); err == nil && payload.CloudaicompanionProject != "" {
			return payload.CloudaicompanionProject, nil
		}
	}

	// Project not found: trigger onboarding.
	if onProgress != nil {
		onProgress("Provisioning Google Cloud project...")
	}
	return geminiCliOnboardUser(ctx, accessToken, headers, envProjectID, onProgress)
}

func geminiCliOnboardUser(
	ctx context.Context,
	accessToken string,
	headers map[string]string,
	projectHint string,
	onProgress func(string),
) (string, error) {
	onboardBody := map[string]any{
		"cloudaicompanionProject": projectHint,
		"metadata": map[string]string{
			"ideType":    "IDE_UNSPECIFIED",
			"platform":   "PLATFORM_UNSPECIFIED",
			"pluginType": "GEMINI",
		},
	}
	bodyBytes, _ := json.Marshal(onboardBody)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		geminiCliAssistURL+"/v1internal:onboardUser", strings.NewReader(string(bodyBytes)))
	if err != nil {
		return "", err
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("gemini cli oauth: onboard: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("gemini cli oauth: onboard: status %d", resp.StatusCode)
	}

	var lro struct {
		Name     string `json:"name"`
		Done     bool   `json:"done"`
		Response struct {
			CloudaicompanionProject struct {
				ID string `json:"id"`
			} `json:"cloudaicompanionProject"`
		} `json:"response"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&lro); err != nil {
		return "", fmt.Errorf("gemini cli oauth: onboard: decode: %w", err)
	}
	if lro.Done {
		if id := lro.Response.CloudaicompanionProject.ID; id != "" {
			return id, nil
		}
	}
	if lro.Name == "" {
		return "", fmt.Errorf("gemini cli oauth: onboard: no operation name")
	}

	// Poll the long-running operation.
	for attempt := 0; ; attempt++ {
		if attempt > 0 {
			if onProgress != nil {
				onProgress(fmt.Sprintf("Waiting for project provisioning (attempt %d)...", attempt+1))
			}
			select {
			case <-time.After(5 * time.Second):
			case <-ctx.Done():
				return "", ctx.Err()
			}
		}

		req2, err := http.NewRequestWithContext(ctx, http.MethodGet,
			geminiCliAssistURL+"/v1internal/"+lro.Name, nil)
		if err != nil {
			return "", err
		}
		for k, v := range headers {
			req2.Header.Set(k, v)
		}

		resp2, err := http.DefaultClient.Do(req2)
		if err != nil {
			return "", fmt.Errorf("gemini cli oauth: poll operation: %w", err)
		}

		var poll struct {
			Done     bool `json:"done"`
			Response struct {
				CloudaicompanionProject struct {
					ID string `json:"id"`
				} `json:"cloudaicompanionProject"`
			} `json:"response"`
		}
		decErr := json.NewDecoder(resp2.Body).Decode(&poll)
		resp2.Body.Close()
		if decErr != nil {
			return "", fmt.Errorf("gemini cli oauth: poll: decode: %w", decErr)
		}
		if poll.Done {
			if id := poll.Response.CloudaicompanionProject.ID; id != "" {
				return id, nil
			}
			return "", fmt.Errorf("gemini cli oauth: onboard completed but no project ID")
		}
	}
}

// GeminiCliProvider is the OAuth Provider for Google Cloud Code Assist.
var GeminiCliProvider Provider = &geminiCliProvider{}

type geminiCliProvider struct{}

func (g *geminiCliProvider) ID() string               { return "google-gemini-cli" }
func (g *geminiCliProvider) Name() string             { return "Google Cloud Code Assist (Gemini CLI)" }
func (g *geminiCliProvider) UsesCallbackServer() bool { return true }

func (g *geminiCliProvider) Login(ctx context.Context, cb LoginCallbacks) (Credentials, error) {
	return LoginGeminiCli(ctx, cb)
}

func (g *geminiCliProvider) RefreshToken(ctx context.Context, creds Credentials) (Credentials, error) {
	return RefreshGeminiCliToken(ctx, creds)
}

// APIKey encodes credentials as JSON {"token":"...","projectId":"..."} which is
// what the google-gemini-cli fantasy provider expects.
func (g *geminiCliProvider) APIKey(creds Credentials) string {
	projectID := ""
	if creds.Extra != nil {
		projectID = creds.Extra["projectId"]
	}
	b, _ := json.Marshal(map[string]string{"token": creds.Access, "projectId": projectID})
	return string(b)
}

// googleCloudEnv reads an environment variable; placed here to avoid importing os
// in every file (os is always available in the standard library, this is just
// a thin wrapper for testability and clarity).
func googleCloudEnv(key string) string {
	// Use a build-tag-free approach: directly wrap os.Getenv.
	// This compiles fine; the import is at the package level via os.
	return osGetenv(key)
}
