package config

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestConfigureProviders_OpenAICodexFromOAuthAuthFile(t *testing.T) {
	tempDir := t.TempDir()
	authPath := filepath.Join(tempDir, "auth.json")
	accountID := "acct_test_123"

	records := map[string]openAICodexAuthRecord{
		openAICodexProviderID: {
			Type:    "oauth",
			Access:  testOpenAICodexJWT(t, accountID),
			Refresh: "refresh_123",
			Expires: time.Now().Add(1 * time.Hour).UnixMilli(),
		},
	}
	writeOpenAICodexAuthFile(t, authPath, records)

	t.Setenv(openAICodexAuthPathEnv, authPath)

	cfg := &Config{
		Providers: map[string]ProviderConfig{
			openAICodexProviderID: {ID: openAICodexProviderID},
		},
	}

	if err := cfg.ConfigureProviders(); err != nil {
		t.Fatalf("ConfigureProviders returned error: %v", err)
	}

	provider := cfg.Providers[openAICodexProviderID]
	if provider.APIKey == "" {
		t.Fatal("expected API key to be populated from OAuth auth file")
	}
	if provider.BaseURL != openAICodexDefaultBaseURL {
		t.Fatalf("expected base URL %q, got %q", openAICodexDefaultBaseURL, provider.BaseURL)
	}
	if got := provider.ExtraHeaders["chatgpt-account-id"]; got != accountID {
		t.Fatalf("expected chatgpt-account-id %q, got %q", accountID, got)
	}
	if got := provider.ExtraHeaders["OpenAI-Beta"]; got != "responses=experimental" {
		t.Fatalf("unexpected OpenAI-Beta header: %q", got)
	}
	if got := provider.ExtraHeaders["originator"]; got != openAICodexOriginatorDefault {
		t.Fatalf("unexpected originator header: %q", got)
	}
	if provider.Type != "openai" {
		t.Fatalf("expected provider type openai, got %q", provider.Type)
	}
}

func TestConfigureProviders_OpenAICodexRefreshesExpiredToken(t *testing.T) {
	tempDir := t.TempDir()
	authPath := filepath.Join(tempDir, "auth.json")
	oldAccountID := "acct_old"
	newAccountID := "acct_new"

	records := map[string]openAICodexAuthRecord{
		openAICodexProviderID: {
			Type:      "oauth",
			Access:    testOpenAICodexJWT(t, oldAccountID),
			Refresh:   "refresh_old",
			Expires:   time.Now().Add(-1 * time.Minute).UnixMilli(),
			AccountID: oldAccountID,
		},
	}
	writeOpenAICodexAuthFile(t, authPath, records)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Fatalf("ParseForm failed: %v", err)
		}
		if got := r.FormValue("grant_type"); got != "refresh_token" {
			t.Fatalf("unexpected grant_type: %q", got)
		}
		if got := r.FormValue("refresh_token"); got != "refresh_old" {
			t.Fatalf("unexpected refresh token: %q", got)
		}

		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token":  testOpenAICodexJWT(t, newAccountID),
			"refresh_token": "refresh_new",
			"expires_in":    3600,
		})
	}))
	defer server.Close()

	t.Setenv(openAICodexAuthPathEnv, authPath)
	t.Setenv(openAICodexTokenURLOverride, server.URL)

	cfg := &Config{
		Providers: map[string]ProviderConfig{
			openAICodexProviderID: {ID: openAICodexProviderID},
		},
	}

	if err := cfg.ConfigureProviders(); err != nil {
		t.Fatalf("ConfigureProviders returned error: %v", err)
	}

	provider := cfg.Providers[openAICodexProviderID]
	if got := provider.ExtraHeaders["chatgpt-account-id"]; got != newAccountID {
		t.Fatalf("expected refreshed account id %q, got %q", newAccountID, got)
	}

	updated := readOpenAICodexAuthFile(t, authPath)
	if got := updated[openAICodexProviderID].Refresh; got != "refresh_new" {
		t.Fatalf("expected persisted refresh token to be updated, got %q", got)
	}
}

func TestConfigureProviders_OpenAICodexPrefersAuthRecordOverConfiguredAPIKey(t *testing.T) {
	tempDir := t.TempDir()
	authPath := filepath.Join(tempDir, "auth.json")
	newAccountID := "acct_from_record"

	records := map[string]openAICodexAuthRecord{
		openAICodexProviderID: {
			Type:      "oauth",
			Access:    testOpenAICodexJWT(t, newAccountID),
			Refresh:   "refresh_record",
			Expires:   time.Now().Add(1 * time.Hour).UnixMilli(),
			AccountID: newAccountID,
		},
	}
	writeOpenAICodexAuthFile(t, authPath, records)
	t.Setenv(openAICodexAuthPathEnv, authPath)

	cfg := &Config{
		Providers: map[string]ProviderConfig{
			openAICodexProviderID: {
				ID:     openAICodexProviderID,
				APIKey: "manually-configured-stale-key",
			},
		},
	}

	if err := cfg.ConfigureProviders(); err != nil {
		t.Fatalf("ConfigureProviders returned error: %v", err)
	}

	provider := cfg.Providers[openAICodexProviderID]
	if provider.APIKey != records[openAICodexProviderID].Access {
		t.Fatalf("expected provider API key to come from auth record")
	}
	if got := provider.ExtraHeaders["chatgpt-account-id"]; got != newAccountID {
		t.Fatalf("expected account id %q, got %q", newAccountID, got)
	}
}

func TestEnsureOpenAICodexOAuth_UsesExistingTokenWithoutBrowserLogin(t *testing.T) {
	tempDir := t.TempDir()
	authPath := filepath.Join(tempDir, "auth.json")
	accountID := "acct_existing"

	records := map[string]openAICodexAuthRecord{
		openAICodexProviderID: {
			Type:      "oauth",
			Access:    testOpenAICodexJWT(t, accountID),
			Refresh:   "refresh_existing",
			Expires:   time.Now().Add(1 * time.Hour).UnixMilli(),
			AccountID: accountID,
		},
	}
	writeOpenAICodexAuthFile(t, authPath, records)

	t.Setenv(openAICodexAuthPathEnv, authPath)

	openedBrowser := false
	originalOpenBrowser := openAICodexOpenBrowser
	openAICodexOpenBrowser = func(string) error {
		openedBrowser = true
		return nil
	}
	t.Cleanup(func() {
		openAICodexOpenBrowser = originalOpenBrowser
	})

	creds, err := EnsureOpenAICodexOAuth(context.Background(), nil)
	if err != nil {
		t.Fatalf("EnsureOpenAICodexOAuth returned error: %v", err)
	}
	if openedBrowser {
		t.Fatal("expected existing token path to skip browser login")
	}
	if creds.AccountID != accountID {
		t.Fatalf("expected accountID %q, got %q", accountID, creds.AccountID)
	}
}

func TestEnsureOpenAICodexOAuth_LoginViaBrowserCallback(t *testing.T) {
	tempDir := t.TempDir()
	authPath := filepath.Join(tempDir, "auth.json")
	t.Setenv(openAICodexAuthPathEnv, authPath)

	newAccountID := "acct_login"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Fatalf("ParseForm failed: %v", err)
		}
		if got := r.FormValue("grant_type"); got != "authorization_code" {
			t.Fatalf("unexpected grant_type: %q", got)
		}

		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token":  testOpenAICodexJWT(t, newAccountID),
			"refresh_token": "refresh_login",
			"expires_in":    3600,
		})
	}))
	defer server.Close()
	t.Setenv(openAICodexTokenURLOverride, server.URL)

	originalOpenBrowser := openAICodexOpenBrowser
	openAICodexOpenBrowser = func(rawURL string) error {
		go func() {
			u, err := urlFromString(rawURL)
			if err != nil {
				return
			}
			state := u.Query().Get("state")
			callback := fmt.Sprintf("http://127.0.0.1:1455/auth/callback?code=test_code&state=%s", state)
			_, _ = http.Get(callback)
		}()
		return nil
	}
	t.Cleanup(func() {
		openAICodexOpenBrowser = originalOpenBrowser
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	creds, err := EnsureOpenAICodexOAuth(ctx, nil)
	if err != nil {
		t.Fatalf("EnsureOpenAICodexOAuth returned error: %v", err)
	}
	if creds.AccountID != newAccountID {
		t.Fatalf("expected accountID %q, got %q", newAccountID, creds.AccountID)
	}

	persisted := readOpenAICodexAuthFile(t, authPath)
	if persisted[openAICodexProviderID].Refresh != "refresh_login" {
		t.Fatalf("expected refreshed token to be persisted")
	}
}

func TestMaybeRefreshOpenAICodexToken_CooldownAfterFailure(t *testing.T) {
	tempDir := t.TempDir()
	authPath := filepath.Join(tempDir, "auth.json")
	accountID := "acct_cooldown"

	records := map[string]openAICodexAuthRecord{
		openAICodexProviderID: {
			Type:      "oauth",
			Access:    testOpenAICodexJWT(t, accountID),
			Refresh:   "refresh_bad",
			Expires:   time.Now().Add(10 * time.Second).UnixMilli(),
			AccountID: accountID,
		},
	}
	writeOpenAICodexAuthFile(t, authPath, records)

	refreshCalls := 0
	// Use a mock server that always fails.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		refreshCalls++
		http.Error(w, "invalid_grant", http.StatusBadRequest)
	}))
	defer server.Close()

	t.Setenv(openAICodexAuthPathEnv, authPath)
	t.Setenv(openAICodexTokenURLOverride, server.URL)

	// Reset cooldown state for this test.
	openAICodexRefreshMu.Lock()
	openAICodexLastRefreshFail = time.Time{}
	openAICodexRefreshMu.Unlock()
	t.Cleanup(func() {
		openAICodexRefreshMu.Lock()
		openAICodexLastRefreshFail = time.Time{}
		openAICodexRefreshMu.Unlock()
	})

	// First call should attempt the refresh and fail, while still returning current token.
	token, gotAccountID, err := MaybeRefreshOpenAICodexToken()
	if err == nil {
		t.Fatal("expected error on first refresh attempt")
	}
	if token == "" || gotAccountID != accountID {
		t.Fatalf("expected existing token/account on soft refresh failure, got token=%q account=%q", token, gotAccountID)
	}

	// Second call should hit cooldown and avoid contacting the provider.
	_, _, err = MaybeRefreshOpenAICodexToken()
	if err == nil {
		t.Fatal("expected error on cooldown")
	}
	if !strings.Contains(err.Error(), "cooldown") {
		t.Fatalf("expected cooldown error, got: %v", err)
	}
	if refreshCalls != 1 {
		t.Fatalf("expected exactly one refresh call, got %d", refreshCalls)
	}
}

func TestMaybeRefreshOpenAICodexToken_ExpiredTokenRefreshFailureReturnsNoToken(t *testing.T) {
	tempDir := t.TempDir()
	authPath := filepath.Join(tempDir, "auth.json")
	accountID := "acct_expired"

	records := map[string]openAICodexAuthRecord{
		openAICodexProviderID: {
			Type:      "oauth",
			Access:    testOpenAICodexJWT(t, accountID),
			Refresh:   "refresh_bad",
			Expires:   time.Now().Add(-1 * time.Minute).UnixMilli(),
			AccountID: accountID,
		},
	}
	writeOpenAICodexAuthFile(t, authPath, records)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "invalid_grant", http.StatusBadRequest)
	}))
	defer server.Close()

	t.Setenv(openAICodexAuthPathEnv, authPath)
	t.Setenv(openAICodexTokenURLOverride, server.URL)

	openAICodexRefreshMu.Lock()
	openAICodexLastRefreshFail = time.Time{}
	openAICodexRefreshMu.Unlock()
	t.Cleanup(func() {
		openAICodexRefreshMu.Lock()
		openAICodexLastRefreshFail = time.Time{}
		openAICodexRefreshMu.Unlock()
	})

	token, gotAccountID, err := MaybeRefreshOpenAICodexToken()
	if err == nil {
		t.Fatal("expected error for expired token refresh failure")
	}
	if token != "" || gotAccountID != "" {
		t.Fatalf("expected no token/account when expired refresh fails, got token=%q account=%q", token, gotAccountID)
	}
}

func TestMaybeRefreshOpenAICodexToken_CooldownResetsOnSuccess(t *testing.T) {
	tempDir := t.TempDir()
	authPath := filepath.Join(tempDir, "auth.json")
	accountID := "acct_reset"

	records := map[string]openAICodexAuthRecord{
		openAICodexProviderID: {
			Type:      "oauth",
			Access:    testOpenAICodexJWT(t, accountID),
			Refresh:   "refresh_ok",
			Expires:   time.Now().Add(-1 * time.Minute).UnixMilli(),
			AccountID: accountID,
		},
	}
	writeOpenAICodexAuthFile(t, authPath, records)

	newAccountID := "acct_refreshed"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token":  testOpenAICodexJWT(t, newAccountID),
			"refresh_token": "refresh_new",
			"expires_in":    3600,
		})
	}))
	defer server.Close()

	t.Setenv(openAICodexAuthPathEnv, authPath)
	t.Setenv(openAICodexTokenURLOverride, server.URL)

	// Simulate a prior failure by setting the cooldown state,
	// but far enough in the past that the cooldown has expired.
	openAICodexRefreshMu.Lock()
	openAICodexLastRefreshFail = time.Now().Add(-2 * openAICodexRefreshCooldown)
	openAICodexRefreshMu.Unlock()
	t.Cleanup(func() {
		openAICodexRefreshMu.Lock()
		openAICodexLastRefreshFail = time.Time{}
		openAICodexRefreshMu.Unlock()
	})

	accessToken, gotAccountID, err := MaybeRefreshOpenAICodexToken()
	if err != nil {
		t.Fatalf("expected success after cooldown expired, got: %v", err)
	}
	if gotAccountID != newAccountID {
		t.Fatalf("expected account %q, got %q", newAccountID, gotAccountID)
	}
	if accessToken == "" {
		t.Fatal("expected non-empty access token")
	}

	// Cooldown should be reset.
	openAICodexRefreshMu.Lock()
	if !openAICodexLastRefreshFail.IsZero() {
		t.Fatal("expected cooldown to be reset after successful refresh")
	}
	openAICodexRefreshMu.Unlock()
}

func writeOpenAICodexAuthFile(t *testing.T, path string, records map[string]openAICodexAuthRecord) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll failed: %v", err)
	}
	data, err := json.Marshal(records)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}
}

func readOpenAICodexAuthFile(t *testing.T, path string) map[string]openAICodexAuthRecord {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile failed: %v", err)
	}
	out := make(map[string]openAICodexAuthRecord)
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}
	return out
}

func testOpenAICodexJWT(t *testing.T, accountID string) string {
	t.Helper()
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"none","typ":"JWT"}`))
	payloadMap := map[string]any{
		openAICodexJWTClaimPath: map[string]any{
			"chatgpt_account_id": accountID,
		},
	}
	payloadJSON, err := json.Marshal(payloadMap)
	if err != nil {
		t.Fatalf("Marshal payload failed: %v", err)
	}
	payload := base64.RawURLEncoding.EncodeToString(payloadJSON)
	return header + "." + payload + ".signature"
}

func urlFromString(raw string) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return nil, err
	}
	return u, nil
}
