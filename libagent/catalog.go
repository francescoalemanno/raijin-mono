package libagent

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"time"

	"charm.land/catwalk/pkg/catwalk"
	"charm.land/catwalk/pkg/embedded"
	"charm.land/fantasy"
	"charm.land/fantasy/providers/anthropic"
	"charm.land/fantasy/providers/google"
	"charm.land/fantasy/providers/openai"
	"charm.land/fantasy/providers/openaicompat"
	"charm.land/fantasy/providers/openrouter"
	"github.com/francescoalemanno/raijin-mono/libagent/oauth"
)

// defaultOAuthIDMap maps catalog provider IDs to oauth package provider IDs
// for the cases where they differ.
var defaultOAuthIDMap = map[string]string{
	// catwalk uses "copilot"; the oauth package uses "github-copilot".
	string(catwalk.InferenceProviderCopilot): "github-copilot",
	// Anthropic catwalk ID matches the oauth ID — listed for documentation.
	string(catwalk.InferenceProviderAnthropic): "anthropic",
	// OpenAI Codex is a custom provider; its ID matches the oauth ID directly.
	CodexProviderID: "openai-codex",
}

// http1OnlyClient is a shared *http.Client whose transport has HTTP/2 disabled.
// Many OpenAI-compatible endpoints announce HTTP/2 support via ALPN but have
// broken or incomplete multiplexed-stream implementations that stall SSE
// responses. Forcing HTTP/1.1 avoids those stalls without any correctness cost.
var http1OnlyClient = &http.Client{
	Transport: &http.Transport{
		// Empty TLSNextProto map disables the automatic HTTP/2 upgrade that
		// http.DefaultTransport would otherwise negotiate via ALPN.
		TLSNextProto: make(map[string]func(string, *tls.Conn) http.RoundTripper),
	},
}

// ModelInfo carries metadata about a model as returned by the catalog.
type ModelCapability string

const (
	// ModelCapabilityText indicates text input/output capability.
	ModelCapabilityText ModelCapability = "text"
	// ModelCapabilityImage indicates image input capability.
	ModelCapabilityImage ModelCapability = "image"
	// ModelCapabilityAudio indicates audio input capability.
	ModelCapabilityAudio ModelCapability = "audio"
)

// ModelInfo carries metadata about a model as returned by the catalog.
type ModelInfo struct {
	// ProviderID is the inference provider identifier (e.g. "openai", "anthropic").
	ProviderID string
	// ModelID is the model identifier within the provider (e.g. "gpt-4o").
	ModelID string
	// Name is the human-readable model name.
	Name string
	// ContextWindow is the model's context window in tokens.
	ContextWindow int64
	// DefaultMaxTokens is the model's default maximum output tokens.
	DefaultMaxTokens int64
	// CanReason indicates whether the model supports extended reasoning.
	CanReason bool
	// Capabilities declares model modalities/features (e.g. text, image, audio).
	Capabilities []ModelCapability
	// SupportsImages is kept for backward compatibility; prefer Capabilities.
	// SupportsImages indicates whether the model accepts image input.
	SupportsImages bool
	// CostPer1MIn is the cost per 1M input tokens in USD.
	CostPer1MIn float64
	// CostPer1MOut is the cost per 1M output tokens in USD.
	CostPer1MOut float64
	// CostPer1MInCached is the cost per 1M cached input tokens in USD.
	CostPer1MInCached float64
	// CostPer1MOutCached is the cost per 1M cached output tokens in USD.
	CostPer1MOutCached float64
}

// HasCapability reports whether the model advertises a capability.
func (m ModelInfo) HasCapability(c ModelCapability) bool {
	for _, v := range m.Capabilities {
		if v == c {
			return true
		}
	}
	return false
}

func normalizeModelInfo(m ModelInfo) ModelInfo {
	seen := map[ModelCapability]struct{}{}
	normCaps := make([]ModelCapability, 0, len(m.Capabilities)+2)
	for _, c := range m.Capabilities {
		if c == "" {
			continue
		}
		if _, ok := seen[c]; ok {
			continue
		}
		seen[c] = struct{}{}
		normCaps = append(normCaps, c)
	}
	if _, ok := seen[ModelCapabilityText]; !ok {
		seen[ModelCapabilityText] = struct{}{}
		normCaps = append(normCaps, ModelCapabilityText)
	}
	if m.SupportsImages {
		if _, ok := seen[ModelCapabilityImage]; !ok {
			seen[ModelCapabilityImage] = struct{}{}
			normCaps = append(normCaps, ModelCapabilityImage)
		}
	}
	m.SupportsImages = false
	for _, c := range normCaps {
		if c == ModelCapabilityImage {
			m.SupportsImages = true
			break
		}
	}
	slices.Sort(normCaps)
	m.Capabilities = normCaps
	return m
}

// CustomProvider lets callers register providers that are not in catwalk's
// embedded catalog.  The Build function receives the resolved API key and
// must return a ready fantasy.Provider.
type CustomProvider struct {
	// ID is the unique provider identifier used in NewModel / FindModel calls.
	ID string
	// Name is the human-readable provider name shown in ListProviders.
	Name string
	// Type is the provider type used for option parsing/routing metadata.
	// Optional; defaults to openai-compat when omitted.
	Type catwalk.Type
	// APIKey is an optional env-var placeholder (for UI prefill), e.g. "$OPENAI_API_KEY".
	APIKey string
	// APIEndpoint is an optional base URL shown in provider listings.
	APIEndpoint string
	// Models is the list of models this provider exposes.
	Models []ModelInfo
	// Build constructs the fantasy.Provider for a given API key.
	// It is called every time NewModel is invoked for this provider.
	Build func(apiKey string) (fantasy.Provider, error)
}

// Catalog provides model discovery backed by catwalk's embedded provider list
// and an optional live catwalk server for up-to-date data.
//
// Use DefaultCatalog() to get the ready-to-use catalog backed by catwalk's
// embedded data, or NewCatalog() to build one with custom providers.
//
// OAuth: every Catalog is pre-configured with DefaultLoginCallbacks so that
// NewModel triggers authentication automatically (browser open + stdin/stdout).
// Override with SetLoginCallbacks for custom UI, and persist credentials
// between runs via SetOnCredentialsUpdated and SetCredentials.
type Catalog struct {
	providers       map[string]catwalk.Provider // keyed by catwalk provider ID
	customProviders map[string]CustomProvider   // keyed by provider ID

	credStore      map[string]oauth.Credentials // keyed by provider ID
	loginCallbacks *oauth.LoginCallbacks
	onCredsUpdated func(providerID string, creds oauth.Credentials)
}

// NewCatalog returns an empty catalog pre-configured with DefaultLoginCallbacks
// and automatic credential persistence to
// ~/.config/libagent/oauth_credentials.json.
// Add providers with AddProvider or AddEmbedded before calling NewModel.
func NewCatalog() *Catalog {
	cb := DefaultLoginCallbacks()

	// Load any previously persisted credentials; silently ignore I/O errors so
	// a missing or malformed file never prevents the catalog from being created.
	store, _ := loadCredentials()

	c := &Catalog{
		providers:       make(map[string]catwalk.Provider),
		customProviders: make(map[string]CustomProvider),
		credStore:       store,
		loginCallbacks:  &cb,
	}

	// Persist to disk whenever credentials are created or refreshed.
	c.onCredsUpdated = func(_ string, _ oauth.Credentials) {
		snapshot := make(map[string]oauth.Credentials, len(c.credStore))
		for k, v := range c.credStore {
			snapshot[k] = v
		}
		_ = saveCredentials(snapshot) // best-effort; callers are not burdened with I/O errors
	}

	return c
}

// AddCustomProvider registers a CustomProvider, overriding any existing entry
// with the same ID.  Custom providers take precedence over catwalk providers
// when both share the same ID.
func (c *Catalog) AddCustomProvider(p CustomProvider) {
	models := make([]ModelInfo, 0, len(p.Models))
	for _, m := range p.Models {
		if m.ProviderID == "" {
			m.ProviderID = p.ID
		}
		models = append(models, normalizeModelInfo(m))
	}
	p.Models = models
	c.customProviders[p.ID] = p
}

// SetLoginCallbacks overrides the OAuth login callbacks used when NewModel
// must trigger an authentication flow.  Every Catalog starts with
// DefaultLoginCallbacks; call this only when custom UI behaviour is needed.
func (c *Catalog) SetLoginCallbacks(cb oauth.LoginCallbacks) {
	c.loginCallbacks = &cb
}

// SetOnCredentialsUpdated registers a hook called whenever credentials are
// created or refreshed.  Use it to persist the updated Credentials to disk.
func (c *Catalog) SetOnCredentialsUpdated(fn func(providerID string, creds oauth.Credentials)) {
	c.onCredsUpdated = fn
}

// SetCredentials stores pre-loaded credentials for a provider (keyed by
// catwalk provider ID, e.g. "copilot", "anthropic").  Call this at startup
// to restore previously persisted credentials.
func (c *Catalog) SetCredentials(providerID string, creds oauth.Credentials) {
	c.credStore[providerID] = creds
}

// Credentials returns the stored credentials for a provider, if any.
func (c *Catalog) Credentials(providerID string) (oauth.Credentials, bool) {
	creds, ok := c.credStore[providerID]
	return creds, ok
}

// DefaultCatalog returns a catalog pre-loaded with all of catwalk's embedded
// (offline) providers plus built-in custom providers (e.g. OpenAI Codex).
// This is the most convenient starting point.
func DefaultCatalog() *Catalog {
	c := NewCatalog()
	c.AddEmbedded()
	c.AddCustomProvider(CodexProvider())
	c.AddCustomProvider(SyntheticProvider())
	c.AddCustomProvider(ZenProvider())
	c.AddCustomProvider(ZenGoProvider())
	return c
}

// AddEmbedded loads all providers from catwalk's offline embedded snapshot.
func (c *Catalog) AddEmbedded() {
	for _, p := range embedded.GetAll() {
		c.AddProvider(p)
	}
}

// AddProvider registers a catwalk.Provider, overriding any existing entry with
// the same ID. Use this to add custom or updated providers on top of the
// embedded catalog.
func (c *Catalog) AddProvider(p catwalk.Provider) {
	c.providers[string(p.ID)] = p
}

// Refresh fetches the latest provider list from a live catwalk server and
// merges it into the catalog, overriding stale embedded entries.
// The etag parameter enables conditional requests; pass "" to always fetch.
// Returns catwalk.ErrNotModified if the server returned 304.
func (c *Catalog) Refresh(ctx context.Context, client *catwalk.Client, etag string) error {
	providers, err := client.GetProviders(ctx, etag)
	if err != nil {
		return err
	}
	for _, p := range providers {
		c.AddProvider(p)
	}
	return nil
}

// ListProviders returns all registered providers (catwalk + custom).
// When both define the same provider ID, the custom provider wins.
func (c *Catalog) ListProviders() []catwalk.Provider {
	combined := make(map[string]catwalk.Provider, len(c.providers)+len(c.customProviders))
	for _, p := range c.providers {
		combined[string(p.ID)] = p
	}
	for _, cp := range c.customProviders {
		combined[cp.ID] = customProviderAsCatwalk(cp)
	}
	out := make([]catwalk.Provider, 0, len(combined))
	for _, p := range combined {
		out = append(out, p)
	}
	return out
}

func customProviderAsCatwalk(cp CustomProvider) catwalk.Provider {
	providerType := cp.Type
	if providerType == "" {
		providerType = catwalk.TypeOpenAICompat
	}
	models := make([]catwalk.Model, 0, len(cp.Models))
	for _, m := range cp.Models {
		norm := normalizeModelInfo(m)
		models = append(models, catwalk.Model{
			ID:               norm.ModelID,
			Name:             norm.Name,
			CostPer1MIn:      norm.CostPer1MIn,
			CostPer1MOut:     norm.CostPer1MOut,
			ContextWindow:    norm.ContextWindow,
			DefaultMaxTokens: norm.DefaultMaxTokens,
			CanReason:        norm.CanReason,
			SupportsImages:   norm.SupportsImages,
		})
	}
	return catwalk.Provider{
		ID:          catwalk.InferenceProvider(cp.ID),
		Name:        cp.Name,
		Type:        providerType,
		APIKey:      cp.APIKey,
		APIEndpoint: cp.APIEndpoint,
		Models:      models,
	}
}

// FindModel returns the ModelInfo for a given provider ID and model ID.
// It searches custom providers first, then catwalk providers.
// The second return value is the catwalk.Provider; it is zero for custom providers.
func (c *Catalog) FindModel(providerID, modelID string) (ModelInfo, catwalk.Provider, error) {
	cp, isCustom := c.customProviders[providerID]
	p, isCatwalk := c.providers[providerID]

	if isCustom {
		for _, m := range cp.Models {
			if m.ModelID == modelID {
				return normalizeModelInfo(m), catwalk.Provider{}, nil
			}
		}
		return ModelInfo{}, catwalk.Provider{}, fmt.Errorf("libagent: model %q not found in provider %q", modelID, providerID)
	}
	if isCatwalk {
		for _, m := range p.Models {
			if m.ID == modelID {
				return fromCatwalkModel(providerID, m), p, nil
			}
		}
		return ModelInfo{}, catwalk.Provider{}, fmt.Errorf("libagent: model %q not found in provider %q", modelID, providerID)
	}
	return ModelInfo{}, catwalk.Provider{}, fmt.Errorf("libagent: provider %q not found in catalog", providerID)
}

// NewModel resolves providerID and modelID from the catalog, constructs the
// appropriate fantasy provider using apiKey, and returns a ready-to-use
// fantasy.LanguageModel.
//
// It supports all provider types known to catwalk (openai, anthropic, google,
// openrouter, openai-compat) as well as custom providers registered via
// AddCustomProvider.
//
// OAuth: when apiKey is empty and the provider has a registered OAuth flow,
// NewModel automatically resolves credentials — refreshing if stale or running
// the login flow if none exist (requires SetLoginCallbacks to have been called).
func (c *Catalog) NewModel(ctx context.Context, providerID, modelID, apiKey string) (fantasy.LanguageModel, error) {
	_, p, err := c.FindModel(providerID, modelID)
	if err != nil {
		return nil, err
	}
	if apiKey == "" {
		resolved, err := c.resolveOAuthAPIKey(ctx, providerID)
		if err != nil {
			return nil, err
		}
		apiKey = resolved
	}

	// Dispatch custom providers through their own Build function.
	cp, isCustom := c.customProviders[providerID]
	if isCustom {
		fp, err := cp.Build(apiKey)
		if err != nil {
			return nil, fmt.Errorf("libagent: build custom provider %q: %w", providerID, err)
		}
		model, err := fp.LanguageModel(ctx, modelID)
		if err != nil {
			return nil, fmt.Errorf("libagent: get language model %q from %q: %w", modelID, providerID, err)
		}
		return model, nil
	}

	fp, err := buildFantasyProvider(p, apiKey)
	if err != nil {
		return nil, err
	}
	model, err := fp.LanguageModel(ctx, modelID)
	if err != nil {
		return nil, fmt.Errorf("libagent: get language model %q from %q: %w", modelID, providerID, err)
	}
	return model, nil
}

// resolveOAuthAPIKey returns the API key for providerID via OAuth, triggering
// a login or token refresh as needed.  Returns "" without error when no OAuth
// provider is registered for this catwalk provider ID.
func (c *Catalog) resolveOAuthAPIKey(ctx context.Context, providerID string) (string, error) {
	// Map catwalk provider ID → oauth provider ID (may be the same).
	oauthID, ok := defaultOAuthIDMap[providerID]
	if !ok {
		oauthID = providerID
	}
	p := oauth.Get(oauthID)
	if p == nil {
		return "", nil // no OAuth support for this provider
	}

	// Read current credential/login callback state.
	creds, hasCreds := c.credStore[providerID]
	callbacks := c.loginCallbacks

	var newCreds oauth.Credentials

	switch {
	case !hasCreds:
		if callbacks == nil {
			return "", fmt.Errorf("libagent: OAuth required for %q but no login callbacks configured; call SetLoginCallbacks first", providerID)
		}
		nc, err := p.Login(ctx, *callbacks)
		if err != nil {
			return "", fmt.Errorf("libagent: OAuth login for %q: %w", providerID, err)
		}
		newCreds = nc

	case time.Now().UnixMilli() < creds.Expires:
		return p.APIKey(creds), nil

	default:
		// Stale — try refresh, fall back to re-login.
		fresh, err := p.RefreshToken(ctx, creds)
		if err != nil {
			if callbacks == nil {
				return "", fmt.Errorf("libagent: OAuth token refresh for %q failed: %w", providerID, err)
			}
			nc, loginErr := p.Login(ctx, *callbacks)
			if loginErr != nil {
				return "", fmt.Errorf("libagent: OAuth refresh for %q failed (%v); re-login also failed: %w", providerID, err, loginErr)
			}
			newCreds = nc
		} else {
			newCreds = fresh
		}
	}

	// Persist new credentials and notify callbacks.
	c.credStore[providerID] = newCreds
	onUpdate := c.onCredsUpdated

	if onUpdate != nil {
		onUpdate(providerID, newCreds)
	}

	return p.APIKey(newCreds), nil
}

// FindModelOptions returns the per-model provider options from the catwalk catalog,
// plus the catwalk provider type string. Returns empty values for custom providers.
func (c *Catalog) FindModelOptions(providerID, modelID string) (providerType string, catalogProviderOptions map[string]any) {
	p, isCatwalk := c.providers[providerID]
	if !isCatwalk {
		return "", nil
	}
	providerType = string(p.Type)
	for _, m := range p.Models {
		if m.ID == modelID {
			return providerType, m.Options.ProviderOptions
		}
	}
	return providerType, nil
}

// NewModelFromProvider creates a fantasy.LanguageModel from any already-constructed
// fantasy.Provider. Use this when you need full control over provider options.
func NewModelFromProvider(ctx context.Context, p fantasy.Provider, modelID string) (fantasy.LanguageModel, error) {
	model, err := p.LanguageModel(ctx, modelID)
	if err != nil {
		return nil, fmt.Errorf("libagent: get language model %q: %w", modelID, err)
	}
	return model, nil
}

// buildFantasyProvider maps a catwalk.Provider to the correct fantasy provider
// constructor based on its Type field.
func buildFantasyProvider(p catwalk.Provider, apiKey string) (fantasy.Provider, error) {
	endpoint := p.APIEndpoint
	headers := p.DefaultHeaders

	switch p.Type {
	case catwalk.TypeOpenAI:
		opts := []openai.Option{
			openai.WithAPIKey(apiKey),
			openai.WithUseResponsesAPI(),
		}
		if endpoint != "" {
			opts = append(opts, openai.WithBaseURL(endpoint))
		}
		if len(headers) > 0 {
			opts = append(opts, openai.WithHeaders(headers))
		}
		return openai.New(opts...)

	case catwalk.TypeAnthropic:
		opts := []anthropic.Option{anthropic.WithAPIKey(apiKey)}
		if endpoint != "" {
			opts = append(opts, anthropic.WithBaseURL(endpoint))
		}
		if len(headers) > 0 {
			opts = append(opts, anthropic.WithHeaders(headers))
		}
		return anthropic.New(opts...)

	case catwalk.TypeGoogle:
		opts := []google.Option{google.WithGeminiAPIKey(apiKey)}
		if endpoint != "" {
			opts = append(opts, google.WithBaseURL(endpoint))
		}
		if len(headers) > 0 {
			opts = append(opts, google.WithHeaders(headers))
		}
		return google.New(opts...)

	case catwalk.TypeOpenRouter:
		// The openrouter fantasy provider hardcodes its base URL; passing one
		// would be silently ignored, so we intentionally omit endpoint here.
		opts := []openrouter.Option{openrouter.WithAPIKey(apiKey)}
		if len(headers) > 0 {
			opts = append(opts, openrouter.WithHeaders(headers))
		}
		return openrouter.New(opts...)

	case catwalk.TypeOpenAICompat:
		// http1OnlyClient forces HTTP/1.1. Many OpenAI-compatible endpoints
		// advertise HTTP/2 via ALPN but implement streaming (SSE) on it
		// incorrectly, causing responses to stall indefinitely. HTTP/1.1 has
		// no such issue and is universally supported by these servers.
		opts := []openaicompat.Option{
			openaicompat.WithAPIKey(apiKey),
			openaicompat.WithHTTPClient(http1OnlyClient),
		}
		// GitHub Copilot requires the Responses API (not Chat Completions).
		if p.ID == catwalk.InferenceProviderCopilot {
			opts = append(opts, openaicompat.WithUseResponsesAPI())
		}
		if endpoint != "" {
			opts = append(opts, openaicompat.WithBaseURL(endpoint))
		}
		if len(headers) > 0 {
			opts = append(opts, openaicompat.WithHeaders(headers))
		}
		return openaicompat.New(opts...)

	default:
		return nil, fmt.Errorf("libagent: unsupported provider type %q for provider %q", p.Type, p.ID)
	}
}

func fromCatwalkModel(providerID string, m catwalk.Model) ModelInfo {
	return normalizeModelInfo(ModelInfo{
		ProviderID:       providerID,
		ModelID:          m.ID,
		Name:             m.Name,
		ContextWindow:    m.ContextWindow,
		DefaultMaxTokens: m.DefaultMaxTokens,
		CanReason:        m.CanReason,
		SupportsImages:   m.SupportsImages,
		CostPer1MIn:      m.CostPer1MIn,
		CostPer1MOut:     m.CostPer1MOut,
	})
}

// errNotModified is re-exported so callers can detect 304 responses from Refresh.
var ErrCatalogNotModified = errors.New("catalog not modified")
