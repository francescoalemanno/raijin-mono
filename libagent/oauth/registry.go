package oauth

import (
	"context"
	"fmt"
	"time"
)

var registry = map[string]Provider{
	AnthropicProvider.ID():     AnthropicProvider,
	GitHubCopilotProvider.ID(): GitHubCopilotProvider,
	GeminiCliProvider.ID():     GeminiCliProvider,
	AntigravityProvider.ID():   AntigravityProvider,
	OpenAICodexProvider.ID():   OpenAICodexProvider,
}

// Get returns the Provider registered under id, or nil if not found.
func Get(id string) Provider {
	return registry[id]
}

// Register adds or replaces a Provider in the global registry.
// Use this to add custom OAuth providers.
func Register(p Provider) {
	registry[p.ID()] = p
}

// All returns all registered providers in unspecified order.
func All() []Provider {
	out := make([]Provider, 0, len(registry))
	for _, p := range registry {
		out = append(out, p)
	}
	return out
}

// GetAPIKey returns the API key for providerId from a credential store,
// refreshing the token automatically if it has expired.
//
// credStore maps provider ID → stored Credentials.
// If a refresh occurs, the updated Credentials are written back to credStore
// and returned alongside the key. Callers should persist the updated store.
//
// Returns (apiKey, updatedCreds, nil) on success, or an error if the provider
// is unknown, credentials are missing, or refresh fails.
func GetAPIKey(
	ctx context.Context,
	providerID string,
	credStore map[string]Credentials,
) (apiKey string, updated *Credentials, err error) {
	p := Get(providerID)
	if p == nil {
		return "", nil, fmt.Errorf("oauth: unknown provider %q", providerID)
	}

	creds, ok := credStore[providerID]
	if !ok {
		return "", nil, fmt.Errorf("oauth: no credentials for provider %q", providerID)
	}

	if time.Now().UnixMilli() >= creds.Expires {
		fresh, err := p.RefreshToken(ctx, creds)
		if err != nil {
			return "", nil, fmt.Errorf("oauth: refresh %q: %w", providerID, err)
		}
		credStore[providerID] = fresh
		creds = fresh
		updated = &fresh
	}

	return p.APIKey(creds), updated, nil
}
