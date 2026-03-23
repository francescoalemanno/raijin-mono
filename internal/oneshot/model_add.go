package oneshot

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

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

// ---------------------------------------------------------------------------
// API-key text input model (step 2)
// ---------------------------------------------------------------------------

type apiKeyInput struct {
	prompt   string
	value    string
	cursor   int // cursor position in runes (not bytes)
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
			m.deleteBackward()
		case "delete":
			m.deleteForward()
		case "left":
			m.moveLeft()
		case "right":
			m.moveRight()
		case "home":
			m.cursor = 0
		case "end":
			m.cursor = m.runeLen()
		case "ctrl+a":
			m.cursor = 0
		case "ctrl+e":
			m.cursor = m.runeLen()
		case "ctrl+b":
			m.moveLeft()
		case "ctrl+f":
			m.moveRight()
		case "ctrl+h":
			m.deleteBackward()
		case "ctrl+d":
			m.deleteForward()
		case "ctrl+u":
			// Delete from cursor to beginning of line
			m.value = m.value[m.cursorRuneOffset():]
			m.cursor = 0
		case "ctrl+k":
			// Delete from cursor to end of line
			m.value = m.value[:m.cursorRuneOffset()]
		case "ctrl+w":
			// Delete word backward
			m.deleteWordBackward()
		default:
			// Handle character input - use Runes to properly support unicode and paste
			if len(msg.Runes) > 0 {
				for _, r := range msg.Runes {
					if r >= ' ' || r == '\t' {
						m.insertRune(r)
					}
				}
			}
		}
	}
	return m, nil
}

func (m *apiKeyInput) runeLen() int {
	return len([]rune(m.value))
}

func (m *apiKeyInput) cursorRuneOffset() int {
	runes := []rune(m.value)
	if m.cursor > len(runes) {
		return len(m.value)
	}
	offset := 0
	for i := 0; i < m.cursor && i < len(runes); i++ {
		offset += len(string(runes[i]))
	}
	return offset
}

func (m *apiKeyInput) insertRune(r rune) {
	offset := m.cursorRuneOffset()
	m.value = m.value[:offset] + string(r) + m.value[offset:]
	m.cursor++
}

func (m *apiKeyInput) moveLeft() {
	if m.cursor > 0 {
		m.cursor--
	}
}

func (m *apiKeyInput) moveRight() {
	if m.cursor < m.runeLen() {
		m.cursor++
	}
}

func (m *apiKeyInput) deleteBackward() {
	if m.cursor == 0 {
		return
	}
	m.moveLeft()
	m.deleteForward()
}

func (m *apiKeyInput) deleteForward() {
	if m.cursor >= m.runeLen() {
		return
	}
	runes := []rune(m.value)
	if m.cursor < len(runes) {
		runes = append(runes[:m.cursor], runes[m.cursor+1:]...)
		m.value = string(runes)
	}
}

func (m *apiKeyInput) deleteWordBackward() {
	runes := []rune(m.value)
	if m.cursor == 0 {
		return
	}
	// Skip trailing spaces
	start := m.cursor
	for start > 0 && isWordSep(runes[start-1]) {
		start--
	}
	// Skip word characters
	for start > 0 && !isWordSep(runes[start-1]) {
		start--
	}
	// Remove the word
	newRunes := append(runes[:start], runes[m.cursor:]...)
	m.value = string(newRunes)
	m.cursor = start
}

func isWordSep(r rune) bool {
	return r == ' ' || r == '\t' || r == '\n' || r == '/' || r == '-' || r == '_'
}

func (m apiKeyInput) View() string {
	var b strings.Builder
	b.WriteString(flTitleStyle.Render(m.prompt))
	b.WriteString("\n")
	if m.value != "" {
		b.WriteString(flPromptStyle.Render("> ") + flFilterStyle.Render(strings.Repeat("•", m.runeLen())))
	} else {
		b.WriteString(flPromptStyle.Render("> ") + flDimStyle.Render("paste API key or leave blank to skip…"))
	}
	b.WriteString("\n")
	b.WriteString(flDimStyle.Render("enter confirm · esc cancel · ←/→ move · ctrl+w delete word · ctrl+u clear"))
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
	entry, ok, err := pickCatalogModel(entries, providerKeys)
	if err != nil {
		return err
	}
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

func pickCatalogModel(entries []catalogEntry, providerKeys map[string]string) (catalogEntry, bool, error) {
	_ = providerKeys
	fzfItems := make([]fzfPickerItem, 0, len(entries))
	entryByKey := make(map[string]catalogEntry, len(entries))
	for _, entry := range entries {
		key := strings.TrimSpace(entry.providerID) + "/" + strings.TrimSpace(entry.modelID)
		if key == "/" {
			key = entry.modelName
		}
		entryByKey[key] = entry
		fzfItems = append(fzfItems, fzfPickerItem{
			key:   key,
			label: fmt.Sprintf("%s [%s]", entry.modelName, entry.providerName),
		})
	}
	chosenKey, action, err := pickWithEmbeddedFZF(fzfItems, "", false, false)
	if errors.Is(err, errFZFPickerUnavailable) {
		return catalogEntry{}, false, fmt.Errorf("interactive picker requires a TTY")
	}
	if err != nil {
		return catalogEntry{}, false, err
	}
	if action != fzfPickerActionSelect {
		return catalogEntry{}, false, nil
	}
	chosen, exists := entryByKey[chosenKey]
	if !exists {
		return catalogEntry{}, false, nil
	}
	return chosen, true, nil
}

func promptAPIKey(providerName, prefill string) (string, bool) {
	m := apiKeyInput{
		prompt: fmt.Sprintf("API KEY FOR %s", strings.ToUpper(providerName)),
		value:  prefill,
		cursor: len([]rune(prefill)),
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

func verifyModelConnectivity(ctx context.Context, model libagent.LanguageModel) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	return libagent.StreamText(ctx, model, "", "Reply with OK.", 16, func(_ string) {})
}

func forceCodexReauthentication(ctx context.Context, cat *libagent.Catalog, cb oauth.LoginCallbacks, modelID string) (libagent.LanguageModel, error) {
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
