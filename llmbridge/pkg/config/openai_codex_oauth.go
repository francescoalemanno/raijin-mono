package config

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/francescoalemanno/raijin-mono/internal/paths"
	"github.com/francescoalemanno/raijin-mono/llmbridge/pkg/llm"
)

const (
	openAICodexProviderID        = "openai-codex"
	openAICodexDefaultBaseURL    = "https://chatgpt.com/backend-api/codex"
	openAICodexTokenURL          = "https://auth.openai.com/oauth/token"
	openAICodexAuthorizeURL      = "https://auth.openai.com/oauth/authorize"
	openAICodexClientID          = "app_EMoamEEZ73f0CkXaXp7hrann"
	openAICodexRedirectURI       = "http://localhost:1455/auth/callback"
	openAICodexScope             = "openid profile email offline_access"
	openAICodexJWTClaimPath      = "https://api.openai.com/auth"
	openAICodexRefreshSkew       = 30 * time.Second
	openAICodexAuthPathEnv       = "RAIJIN_AUTH_PATH"
	openAICodexTokenURLOverride  = "RAIJIN_OPENAI_CODEX_TOKEN_URL"
	openAICodexAuthURLOverride   = "RAIJIN_OPENAI_CODEX_AUTH_URL"
	openAICodexOriginatorDefault = "raijin"
)

var openAICodexOpenBrowser = openURLInBrowser

var (
	openAICodexRefreshMu       sync.Mutex
	openAICodexLastRefreshFail time.Time
	openAICodexRefreshCooldown = 30 * time.Second
)

type OpenAICodexOAuthCredentials struct {
	AccessToken string
	AccountID   string
	ExpiresAt   time.Time
}

type openAICodexAuthRecord struct {
	Type      string `json:"type,omitempty"`
	Access    string `json:"access"`
	Refresh   string `json:"refresh"`
	Expires   int64  `json:"expires"`
	AccountID string `json:"accountId,omitempty"`
}

type openAICodexTokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int64  `json:"expires_in"`
}

func configureOpenAICodexProvider(pc ProviderConfig) (ProviderConfig, error) {
	record, authPath, found, err := loadOpenAICodexAuthRecord()
	if err != nil {
		return pc, err
	}

	if found {
		now := time.Now().UnixMilli()
		if record.Expires <= now+openAICodexRefreshSkew.Milliseconds() {
			refreshed, err := refreshOpenAICodexRecord(record)
			if err != nil {
				return pc, err
			}
			record = refreshed
			if err := persistOpenAICodexAuthRecord(authPath, record); err != nil {
				return pc, err
			}
		}
		pc.APIKey = record.Access
	}

	return configureOpenAICodexProviderHeaders(pc, found, record)
}

// MaybeRefreshOpenAICodexToken checks if the OpenAI Codex token needs refreshing
// and refreshes it if necessary. Returns the current valid access token and account ID.
// This should be called before API requests to ensure the token is valid.
//
// Concurrent calls are serialized via a mutex, and failed refresh attempts
// enforce a cooldown period to avoid spamming the provider.
func MaybeRefreshOpenAICodexToken() (accessToken, accountID string, err error) {
	openAICodexRefreshMu.Lock()
	defer openAICodexRefreshMu.Unlock()

	record, authPath, found, err := loadOpenAICodexAuthRecord()
	if err != nil {
		return "", "", err
	}

	if !found {
		return "", "", fmt.Errorf("no openai-codex auth record found")
	}

	now := time.Now()
	expiresAt := time.UnixMilli(record.Expires)
	needsRefresh := record.Expires <= now.UnixMilli()+openAICodexRefreshSkew.Milliseconds()
	if needsRefresh {
		cooldownActive := !openAICodexLastRefreshFail.IsZero() && now.Before(openAICodexLastRefreshFail.Add(openAICodexRefreshCooldown))
		tokenExpired := !expiresAt.After(now)
		if cooldownActive && !tokenExpired {
			return record.Access, record.AccountID, fmt.Errorf("token refresh on cooldown (last failure %s ago)", now.Sub(openAICodexLastRefreshFail).Round(time.Second))
		}

		refreshed, refreshErr := refreshOpenAICodexRecord(record)
		if refreshErr != nil {
			openAICodexLastRefreshFail = now
			if tokenExpired {
				return "", "", fmt.Errorf("refresh token: %w", refreshErr)
			}
			return record.Access, record.AccountID, fmt.Errorf("refresh token: %w", refreshErr)
		}
		openAICodexLastRefreshFail = time.Time{}
		record = refreshed
		if err := persistOpenAICodexAuthRecord(authPath, record); err != nil {
			return "", "", fmt.Errorf("persist refreshed token: %w", err)
		}
	}

	return record.Access, record.AccountID, nil
}

func configureOpenAICodexProviderHeaders(pc ProviderConfig, found bool, record openAICodexAuthRecord) (ProviderConfig, error) {
	if pc.APIKey == "" {
		return pc, nil
	}

	accountID := ""
	if found {
		accountID = record.AccountID
	}
	if accountID == "" {
		var err error
		accountID, err = extractOpenAICodexAccountID(pc.APIKey)
		if err != nil {
			return pc, err
		}
	}

	if pc.BaseURL == "" {
		pc.BaseURL = openAICodexDefaultBaseURL
	} else if strings.TrimRight(pc.BaseURL, "/") == "https://chatgpt.com/backend-api" {
		// Migrate older configs that pointed to backend-api root.
		pc.BaseURL = openAICodexDefaultBaseURL
	}
	if pc.ExtraHeaders == nil {
		pc.ExtraHeaders = make(map[string]string)
	}
	if pc.ProviderOptions == nil {
		pc.ProviderOptions = make(map[string]any)
	}
	if _, ok := pc.ProviderOptions["useResponsesAPI"]; !ok {
		pc.ProviderOptions["useResponsesAPI"] = true
	}
	if _, ok := pc.ExtraHeaders["chatgpt-account-id"]; !ok {
		pc.ExtraHeaders["chatgpt-account-id"] = accountID
	}
	if _, ok := pc.ExtraHeaders["OpenAI-Beta"]; !ok {
		pc.ExtraHeaders["OpenAI-Beta"] = "responses=experimental"
	}
	if _, ok := pc.ExtraHeaders["originator"]; !ok {
		pc.ExtraHeaders["originator"] = openAICodexOriginatorDefault
	}

	return pc, nil
}

// EnsureOpenAICodexOAuth ensures local OAuth credentials exist for openai-codex.
// It first tries loading/refreshing existing credentials, then falls back to a browser login flow.
func EnsureOpenAICodexOAuth(ctx context.Context, progress func(string)) (OpenAICodexOAuthCredentials, error) {
	logProgress := func(msg string) {
		if progress != nil {
			progress(msg)
		}
	}

	record, authPath, found, err := loadOpenAICodexAuthRecord()
	if err != nil {
		return OpenAICodexOAuthCredentials{}, err
	}

	if found {
		now := time.Now().UnixMilli()
		if record.Expires <= now+openAICodexRefreshSkew.Milliseconds() {
			logProgress("Refreshing OpenAI Codex OAuth token...")
			refreshed, refreshErr := refreshOpenAICodexRecord(record)
			if refreshErr != nil {
				logProgress("Token refresh failed; starting browser login...")
			} else {
				record = refreshed
				if err := persistOpenAICodexAuthRecord(authPath, record); err != nil {
					return OpenAICodexOAuthCredentials{}, err
				}
			}
		}

		if record.Access != "" && record.AccountID != "" && record.Expires > now {
			return OpenAICodexOAuthCredentials{
				AccessToken: record.Access,
				AccountID:   record.AccountID,
				ExpiresAt:   time.UnixMilli(record.Expires),
			}, nil
		}
	}

	logProgress("Starting OpenAI Codex login in your browser...")
	loginRecord, err := loginOpenAICodexViaBrowser(ctx, logProgress)
	if err != nil {
		return OpenAICodexOAuthCredentials{}, err
	}
	if err := persistOpenAICodexAuthRecord(authPath, loginRecord); err != nil {
		return OpenAICodexOAuthCredentials{}, err
	}

	return OpenAICodexOAuthCredentials{
		AccessToken: loginRecord.Access,
		AccountID:   loginRecord.AccountID,
		ExpiresAt:   time.UnixMilli(loginRecord.Expires),
	}, nil
}

func loginOpenAICodexViaBrowser(ctx context.Context, progress func(string)) (openAICodexAuthRecord, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, 10*time.Minute)
		defer cancel()
	}

	state, err := randomHex(16)
	if err != nil {
		return openAICodexAuthRecord{}, fmt.Errorf("generate oauth state: %w", err)
	}
	verifier, challenge, err := generatePKCEValues()
	if err != nil {
		return openAICodexAuthRecord{}, err
	}

	authURL, err := openAICodexAuthorizationURL(state, challenge)
	if err != nil {
		return openAICodexAuthRecord{}, err
	}

	codeCh := make(chan string, 1)
	errCh := make(chan error, 1)
	server := &http.Server{Addr: "127.0.0.1:1455"}
	server.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/auth/callback" {
			http.NotFound(w, r)
			return
		}
		if r.URL.Query().Get("state") != state {
			http.Error(w, "state mismatch", http.StatusBadRequest)
			return
		}
		code := strings.TrimSpace(r.URL.Query().Get("code"))
		if code == "" {
			http.Error(w, "missing authorization code", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = io.WriteString(w, `<html><body><p>Authentication successful. Return to Raijin.</p></body></html>`)
		select {
		case codeCh <- code:
		default:
		}
	})

	go func() {
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errCh <- err
		}
	}()
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
	}()

	if err := openAICodexOpenBrowser(authURL); err != nil {
		if progress != nil {
			progress("Could not auto-open browser. Open this URL manually: " + authURL)
		}
	} else if progress != nil {
		progress("Browser opened for OpenAI Codex authentication...")
	}

	select {
	case <-ctx.Done():
		return openAICodexAuthRecord{}, fmt.Errorf("oauth login timed out")
	case err := <-errCh:
		return openAICodexAuthRecord{}, fmt.Errorf("oauth callback server failed: %w", err)
	case code := <-codeCh:
		tokenResp, err := exchangeOpenAICodexAuthorizationCode(code, verifier)
		if err != nil {
			return openAICodexAuthRecord{}, err
		}
		accountID, err := extractOpenAICodexAccountID(tokenResp.AccessToken)
		if err != nil {
			return openAICodexAuthRecord{}, err
		}
		return openAICodexAuthRecord{
			Type:      "oauth",
			Access:    tokenResp.AccessToken,
			Refresh:   tokenResp.RefreshToken,
			Expires:   time.Now().Add(time.Duration(tokenResp.ExpiresIn) * time.Second).UnixMilli(),
			AccountID: accountID,
		}, nil
	}
}

func loadOpenAICodexAuthRecord() (openAICodexAuthRecord, string, bool, error) {
	authPath, err := openAICodexAuthPath()
	if err != nil {
		return openAICodexAuthRecord{}, "", false, err
	}

	data, err := os.ReadFile(authPath)
	if err != nil {
		if os.IsNotExist(err) {
			return openAICodexAuthRecord{}, authPath, false, nil
		} else {
			return openAICodexAuthRecord{}, authPath, false, fmt.Errorf("read auth file: %w", err)
		}
	}

	records := make(map[string]openAICodexAuthRecord)
	if err := json.Unmarshal(data, &records); err != nil {
		return openAICodexAuthRecord{}, authPath, false, fmt.Errorf("parse auth file: %w", err)
	}

	record, ok := records[openAICodexProviderID]
	if !ok || record.Access == "" || record.Refresh == "" {
		return openAICodexAuthRecord{}, authPath, false, nil
	}

	if record.Type != "" && record.Type != "oauth" {
		return openAICodexAuthRecord{}, authPath, false, nil
	}

	if record.AccountID == "" {
		record.AccountID, err = extractOpenAICodexAccountID(record.Access)
		if err != nil {
			return openAICodexAuthRecord{}, authPath, false, err
		}
	}

	return record, authPath, true, nil
}

func openAICodexAuthPath() (string, error) {
	if p := strings.TrimSpace(os.Getenv(openAICodexAuthPathEnv)); p != "" {
		return p, nil
	}
	p := paths.RaijinAuthPath()
	if p == "" {
		return "", fmt.Errorf("resolve user config dir")
	}
	return p, nil
}

func refreshOpenAICodexRecord(record openAICodexAuthRecord) (openAICodexAuthRecord, error) {
	tokenURL := strings.TrimSpace(os.Getenv(openAICodexTokenURLOverride))
	if tokenURL == "" {
		tokenURL = openAICodexTokenURL
	}

	values := url.Values{}
	values.Set("grant_type", "refresh_token")
	values.Set("refresh_token", record.Refresh)
	values.Set("client_id", openAICodexClientID)
	body := []byte(values.Encode())
	req, err := http.NewRequest(http.MethodPost, tokenURL, bytes.NewReader(body))
	if err != nil {
		return openAICodexAuthRecord{}, fmt.Errorf("build token refresh request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return openAICodexAuthRecord{}, fmt.Errorf("refresh token: %w", err)
	}
	defer resp.Body.Close()

	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return openAICodexAuthRecord{}, fmt.Errorf("refresh token failed (%d): %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}

	var parsed openAICodexTokenResponse
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return openAICodexAuthRecord{}, fmt.Errorf("parse refresh token response: %w", err)
	}
	if parsed.AccessToken == "" || parsed.RefreshToken == "" || parsed.ExpiresIn <= 0 {
		return openAICodexAuthRecord{}, fmt.Errorf("refresh token response missing fields")
	}

	accountID, err := extractOpenAICodexAccountID(parsed.AccessToken)
	if err != nil {
		return openAICodexAuthRecord{}, err
	}

	return openAICodexAuthRecord{
		Type:      "oauth",
		Access:    parsed.AccessToken,
		Refresh:   parsed.RefreshToken,
		Expires:   time.Now().Add(time.Duration(parsed.ExpiresIn) * time.Second).UnixMilli(),
		AccountID: accountID,
	}, nil
}

func exchangeOpenAICodexAuthorizationCode(code, verifier string) (openAICodexTokenResponse, error) {
	tokenURL := strings.TrimSpace(os.Getenv(openAICodexTokenURLOverride))
	if tokenURL == "" {
		tokenURL = openAICodexTokenURL
	}
	values := url.Values{}
	values.Set("grant_type", "authorization_code")
	values.Set("client_id", openAICodexClientID)
	values.Set("code", code)
	values.Set("code_verifier", verifier)
	values.Set("redirect_uri", openAICodexRedirectURI)
	body := []byte(values.Encode())

	req, err := http.NewRequest(http.MethodPost, tokenURL, bytes.NewReader(body))
	if err != nil {
		return openAICodexTokenResponse{}, fmt.Errorf("build token exchange request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return openAICodexTokenResponse{}, fmt.Errorf("exchange authorization code: %w", err)
	}
	defer resp.Body.Close()

	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return openAICodexTokenResponse{}, fmt.Errorf("exchange code failed (%d): %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}

	var parsed openAICodexTokenResponse
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return openAICodexTokenResponse{}, fmt.Errorf("parse exchange token response: %w", err)
	}
	if parsed.AccessToken == "" || parsed.RefreshToken == "" || parsed.ExpiresIn <= 0 {
		return openAICodexTokenResponse{}, fmt.Errorf("token exchange response missing fields")
	}

	return parsed, nil
}

func openAICodexAuthorizationURL(state, challenge string) (string, error) {
	authURL := strings.TrimSpace(os.Getenv(openAICodexAuthURLOverride))
	if authURL == "" {
		authURL = openAICodexAuthorizeURL
	}
	u, err := url.Parse(authURL)
	if err != nil {
		return "", fmt.Errorf("parse authorize url: %w", err)
	}
	q := u.Query()
	q.Set("response_type", "code")
	q.Set("client_id", openAICodexClientID)
	q.Set("redirect_uri", openAICodexRedirectURI)
	q.Set("scope", openAICodexScope)
	q.Set("code_challenge", challenge)
	q.Set("code_challenge_method", "S256")
	q.Set("state", state)
	q.Set("id_token_add_organizations", "true")
	q.Set("codex_cli_simplified_flow", "true")
	q.Set("originator", openAICodexOriginatorDefault)
	u.RawQuery = q.Encode()
	return u.String(), nil
}

func generatePKCEValues() (verifier string, challenge string, err error) {
	raw, err := randomBytes(32)
	if err != nil {
		return "", "", fmt.Errorf("generate pkce verifier: %w", err)
	}
	verifier = base64.RawURLEncoding.EncodeToString(raw)
	hash := sha256.Sum256([]byte(verifier))
	challenge = base64.RawURLEncoding.EncodeToString(hash[:])
	return verifier, challenge, nil
}

func randomHex(size int) (string, error) {
	raw, err := randomBytes(size)
	if err != nil {
		return "", err
	}
	b := strings.Builder{}
	b.Grow(size * 2)
	for _, v := range raw {
		_, _ = fmt.Fprintf(&b, "%02x", v)
	}
	return b.String(), nil
}

func randomBytes(size int) ([]byte, error) {
	raw := make([]byte, size)
	if _, err := rand.Read(raw); err != nil {
		return nil, err
	}
	return raw, nil
}

func openURLInBrowser(rawURL string) error {
	if strings.TrimSpace(rawURL) == "" {
		return fmt.Errorf("empty browser url")
	}

	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", rawURL)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", rawURL)
	default:
		cmd = exec.Command("xdg-open", rawURL)
	}

	if err := cmd.Start(); err != nil {
		slog.Debug("open browser failed", "err", err, "url", html.EscapeString(rawURL))
		return err
	}
	return nil
}

func persistOpenAICodexAuthRecord(authPath string, record openAICodexAuthRecord) error {
	data, err := os.ReadFile(authPath)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("read auth file before persist: %w", err)
	}

	records := make(map[string]openAICodexAuthRecord)
	if len(data) > 0 {
		if err := json.Unmarshal(data, &records); err != nil {
			return fmt.Errorf("parse auth file before persist: %w", err)
		}
	}
	records[openAICodexProviderID] = record

	encoded, err := json.MarshalIndent(records, "", "  ")
	if err != nil {
		return fmt.Errorf("encode auth file: %w", err)
	}

	if err := os.MkdirAll(filepath.Dir(authPath), 0o700); err != nil {
		return fmt.Errorf("ensure auth dir: %w", err)
	}
	if err := os.WriteFile(authPath, append(encoded, '\n'), 0o600); err != nil {
		return fmt.Errorf("write auth file: %w", err)
	}

	return nil
}

func extractOpenAICodexAccountID(token string) (string, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return "", fmt.Errorf("invalid openai-codex access token")
	}

	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return "", fmt.Errorf("decode openai-codex token payload: %w", err)
	}

	var parsed map[string]any
	if err := json.Unmarshal(payload, &parsed); err != nil {
		return "", fmt.Errorf("parse openai-codex token payload: %w", err)
	}

	authClaims, ok := parsed[openAICodexJWTClaimPath].(map[string]any)
	if !ok {
		return "", fmt.Errorf("openai-codex token missing auth claims")
	}

	accountID, _ := authClaims["chatgpt_account_id"].(string)
	if accountID == "" {
		return "", fmt.Errorf("openai-codex token missing chatgpt_account_id")
	}

	return accountID, nil
}

func init() {
	// Register the token refresh callback for openai-codex with the llm package.
	llm.RegisterOpenAICodexTokenRefresh(func(providerID string) (string, error) {
		if providerID != openAICodexProviderID {
			return "", nil
		}
		accessToken, _, err := MaybeRefreshOpenAICodexToken()
		return accessToken, err
	})
}
