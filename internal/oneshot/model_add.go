package oneshot

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"charm.land/fantasy"
	modelconfig "github.com/francescoalemanno/raijin-mono/internal/config"
	libagent "github.com/francescoalemanno/raijin-mono/libagent"
	"github.com/francescoalemanno/raijin-mono/libagent/oauth"
)

// catalogEntry represents a model entry for the oneshot selector.
type catalogEntry struct {
	providerID    string
	providerName  string
	providerType  string
	baseURL       string
	modelID       string
	modelName     string
	maxTokens     int64
	contextWindow int64
	canReason     bool
	apiKeyHint    string // raw env-var hint from the catalog
}

func loadCatalogEntries() []catalogEntry {
	cat := libagent.DefaultCatalog()
	var entries []catalogEntry
	for _, p := range cat.ListProviders() {
		provName := p.Name
		if provName == "" {
			provName = string(p.ID)
		}
		for _, model := range p.Models {
			mName := model.Name
			if mName == "" {
				mName = model.ID
			}
			entries = append(entries, catalogEntry{
				providerID:    string(p.ID),
				providerName:  provName,
				providerType:  string(p.Type),
				baseURL:       p.APIEndpoint,
				modelID:       model.ID,
				modelName:     mName,
				maxTokens:     model.DefaultMaxTokens,
				contextWindow: model.ContextWindow,
				canReason:     model.CanReason,
				apiKeyHint:    p.APIKey,
			})
		}
	}
	return entries
}

var (
	catSelectedStyle = oneshotAccentStyle
	catNormalStyle   = oneshotNormalStyle
	catProviderStyle = oneshotProviderStyle
)

// ---------------------------------------------------------------------------
// API-key text input model (step 2)
// ---------------------------------------------------------------------------

type apiKeyInput struct {
	prompt   string
	value    string
	done     bool
	quitting bool
}

func (m apiKeyInput) Init() tea.Cmd { return nil }

func (m apiKeyInput) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "esc":
			m.quitting = true
			return m, tea.Quit
		case "enter":
			m.done = true
			return m, tea.Quit
		case "backspace":
			if len(m.value) > 0 {
				m.value = m.value[:len(m.value)-1]
			}
		default:
			r := msg.String()
			if len(r) == 1 && r[0] >= ' ' {
				m.value += r
			}
		}
	}
	return m, nil
}

func (m apiKeyInput) View() string {
	var b strings.Builder
	b.WriteString(flTitleStyle.Render(m.prompt))
	b.WriteString("\n")
	if m.value != "" {
		b.WriteString(flPromptStyle.Render("> ") + flFilterStyle.Render(strings.Repeat("•", len(m.value))))
	} else {
		b.WriteString(flPromptStyle.Render("> ") + flDimStyle.Render("paste API key or leave blank to skip…"))
	}
	b.WriteString("\n")
	b.WriteString(flDimStyle.Render("enter confirm · esc cancel"))
	return b.String()
}

// ---------------------------------------------------------------------------
// handleModelsAdd orchestrates the two-step flow
// ---------------------------------------------------------------------------

func handleModelsAdd(opts Options) error {
	if opts.Store == nil {
		return fmt.Errorf("no model store available")
	}

	entries := loadCatalogEntries()
	if len(entries) == 0 {
		return fmt.Errorf("model catalog is empty")
	}

	// Collect existing provider keys for pre-population.
	providerKeys := collectProviderKeys(opts.Store)

	// Step 1: pick a model from the catalog.
	entry, ok := pickCatalogModel(entries, providerKeys)
	if !ok {
		return nil // user cancelled
	}

	// Step 2: API key (skip for Codex).
	apiKey := ""
	if !strings.EqualFold(strings.TrimSpace(entry.providerID), libagent.CodexProviderID) {
		// Try to pre-populate from existing keys or env.
		prefill := ""
		if k := providerKeys[entry.providerID]; k != "" {
			prefill = k
		} else if k := resolveEnvAPIKey(entry.providerID, entry.apiKeyHint); k != "" {
			prefill = k
		}

		var cancelled bool
		apiKey, cancelled = promptAPIKey(entry.providerName, prefill)
		if cancelled {
			return nil
		}
	}

	// Apply the result.
	return applyModelAdd(opts, entry, apiKey)
}

func pickCatalogModel(entries []catalogEntry, providerKeys map[string]string) (catalogEntry, bool) {
	fl := newFilterList(
		"SELECT MODEL",
		entries,
		0,
		0,
		func(item catalogEntry) string {
			return item.providerName + " " + item.modelName + " " + item.modelID
		},
		func(item catalogEntry, selected bool) string {
			label := item.modelName
			pointer := "  "
			if selected {
				pointer = "→ "
			}

			provTag := catProviderStyle.Render("[" + item.providerName + "]")
			if selected {
				return catSelectedStyle.Render(pointer+label) + " " + provTag
			}
			return catNormalStyle.Render(pointer+label) + " " + provTag
		},
	)

	p := tea.NewProgram(fl, tea.WithAltScreen())
	final, err := p.Run()
	if err != nil {
		return catalogEntry{}, false
	}
	result := final.(filterList[catalogEntry])
	if result.quitting || result.chosen == nil {
		return catalogEntry{}, false
	}
	return *result.chosen, true
}

func promptAPIKey(providerName, prefill string) (string, bool) {
	m := apiKeyInput{
		prompt: fmt.Sprintf("API KEY FOR %s", strings.ToUpper(providerName)),
		value:  prefill,
	}
	p := tea.NewProgram(m, tea.WithAltScreen())
	final, err := p.Run()
	if err != nil {
		return "", true
	}
	result := final.(apiKeyInput)
	if result.quitting {
		return "", true
	}
	return strings.TrimSpace(result.value), false
}

func applyModelAdd(opts Options, entry catalogEntry, apiKey string) error {
	// Handle Codex OAuth login.
	if strings.EqualFold(entry.providerID, libagent.CodexProviderID) && strings.TrimSpace(apiKey) == "" {
		fmt.Fprintf(os.Stderr, "%s Starting OpenAI Codex OAuth login…\n", renderStatusInfo("●"))
		cat := libagent.DefaultCatalog()
		loginCallbacks := libagent.LoginCallbacksWithPrinter(func(msg string) {
			fmt.Fprintf(os.Stderr, "  %s\n", msg)
		})
		cat.SetLoginCallbacks(loginCallbacks)
		loginModelID := strings.TrimSpace(entry.modelID)
		if loginModelID == "" {
			loginModelID = "gpt-5.3-codex"
		}
		model, err := cat.NewModel(context.Background(), libagent.CodexProviderID, loginModelID, "")
		if err != nil {
			return fmt.Errorf("openai-codex login failed: %w", err)
		}
		if err := verifyModelConnectivity(context.Background(), model); err != nil {
			if isCodexAuthFailure(err) {
				fmt.Fprintf(os.Stderr, "%s Existing Codex credentials were rejected; starting fresh OAuth login…\n", renderStatusWarning("●"))
				model, err = forceCodexReauthentication(context.Background(), cat, loginCallbacks, loginModelID)
				if err != nil {
					return fmt.Errorf("openai-codex re-authentication failed: %w", err)
				}
				if err := verifyModelConnectivity(context.Background(), model); err != nil {
					return fmt.Errorf("openai-codex re-authentication completed but test request failed: %w", err)
				}
			} else {
				return fmt.Errorf("openai-codex login completed but test request failed: %w", err)
			}
		}
		fmt.Fprintf(os.Stderr, "%s OpenAI Codex authentication verified\n", renderStatusSuccess("✓"))
	}

	maxTokens := entry.maxTokens
	if maxTokens <= 0 {
		maxTokens = libagent.DefaultMaxTokens
	}
	if entry.contextWindow > 0 && maxTokens >= entry.contextWindow {
		maxTokens = max(entry.contextWindow/2, 1)
	}

	modelCfg := libagent.ModelConfig{
		Name:          entry.providerID + "/" + entry.modelID,
		Provider:      entry.providerID,
		Model:         entry.modelID,
		APIKey:        apiKey,
		MaxTokens:     maxTokens,
		ContextWindow: entry.contextWindow,
		ThinkingLevel: libagent.ThinkingLevelMedium,
	}
	if entry.baseURL != "" {
		modelCfg.BaseURL = &entry.baseURL
	}

	// Verify the model can be built before persisting it.
	if _, err := RebuildRuntimeModel(modelCfg); err != nil {
		return fmt.Errorf("model added but failed to configure: %w", err)
	}
	if err := opts.Store.Add(modelCfg); err != nil {
		return err
	}
	if err := opts.Store.SetDefault(modelCfg.Name); err != nil {
		return err
	}

	fmt.Fprintf(os.Stderr, "%s Model configured: %s/%s\n", renderStatusSuccess("✓"), entry.providerID, entry.modelID)
	return nil
}

func verifyModelConnectivity(ctx context.Context, model fantasy.LanguageModel) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	return libagent.StreamText(ctx, model, "", "Reply with OK.", 16, func(_ string) {})
}

func forceCodexReauthentication(ctx context.Context, cat *libagent.Catalog, cb oauth.LoginCallbacks, modelID string) (fantasy.LanguageModel, error) {
	// Force the OAuth resolver down the refresh/login path. It will fall back
	// to interactive login and persist fresh credentials through the catalog's
	// onCredsUpdated hook.
	cat.SetLoginCallbacks(cb)
	cat.SetCredentials(libagent.CodexProviderID, oauth.Credentials{Expires: 0})
	return cat.NewModel(ctx, libagent.CodexProviderID, modelID, "")
}

func isCodexAuthFailure(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "status 401") ||
		strings.Contains(msg, "unauthorized") ||
		strings.Contains(msg, "token_invalidated")
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func collectProviderKeys(store *modelconfig.ModelStore) map[string]string {
	keys := make(map[string]string)
	if store == nil {
		return keys
	}
	for _, name := range store.List() {
		if mc, ok := store.Get(name); ok && mc.APIKey != "" {
			if keys[mc.Provider] == "" {
				keys[mc.Provider] = mc.APIKey
			}
		}
	}
	return keys
}

func resolveEnvAPIKey(providerID, raw string) string {
	trimmed := strings.TrimSpace(raw)
	if after, ok := strings.CutPrefix(trimmed, "$"); ok && after != "" {
		if value := strings.TrimSpace(os.Getenv(after)); value != "" {
			return value
		}
	}
	if trimmed != "" && !strings.HasPrefix(trimmed, "$") {
		return trimmed
	}
	for _, envVar := range fallbackProviderEnvVars(providerID) {
		if value := strings.TrimSpace(os.Getenv(envVar)); value != "" {
			return value
		}
	}
	return ""
}

func fallbackProviderEnvVars(providerID string) []string {
	switch strings.ToLower(strings.TrimSpace(providerID)) {
	case "openai":
		return []string{"OPENAI_API_KEY"}
	case "anthropic":
		return []string{"ANTHROPIC_API_KEY"}
	case "gemini", "google":
		return []string{"GEMINI_API_KEY", "GOOGLE_API_KEY"}
	case "openrouter":
		return []string{"OPENROUTER_API_KEY"}
	case "xai":
		return []string{"XAI_API_KEY"}
	case "groq":
		return []string{"GROQ_API_KEY"}
	case "cerebras":
		return []string{"CEREBRAS_API_KEY"}
	case "copilot":
		return []string{"GITHUB_TOKEN", "COPILOT_API_KEY"}
	case "azure":
		return []string{"AZURE_OPENAI_API_KEY", "AZURE_API_KEY"}
	default:
		return nil
	}
}
