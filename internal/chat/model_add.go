package chat

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/francescoalemanno/raijin-mono/libtui/pkg/components"
	"github.com/francescoalemanno/raijin-mono/libtui/pkg/fuzzy"
	"github.com/francescoalemanno/raijin-mono/libtui/pkg/keybindings"
	"github.com/francescoalemanno/raijin-mono/libtui/pkg/tui"

	"github.com/francescoalemanno/raijin-mono/internal/theme"
	"github.com/francescoalemanno/raijin-mono/llmbridge/pkg/catalog"
)

const modelAddMaxVisible = 10

// catalogItem represents a provider+model from the catwalk catalog.
type catalogItem struct {
	providerID     string
	providerName   string
	providerType   string
	providerAPIKey string
	baseURL        string
	modelID        string
	modelName      string
	maxTokens      int64
	contextWindow  int64
	canReason      bool
}

type modelAddStep int

const (
	stepSelectModel modelAddStep = iota
	stepEnterAPIKey
)

// ModelAddComponent is a multi-step dialog for adding a model from the catwalk catalog.
// Step 1: Select provider/model (fuzzy-filtered list with Input)
// Step 2: Enter API key (Input; optional)
// Layout follows the same pattern as ModelSelectorComponent.
type ModelAddComponent struct {
	searchInput   *components.Input
	apiKeyInput   *components.Input
	listContainer *tui.Container
	hintText      *components.Text
	titleText     *components.Text
	borderTop     *borderLine
	borderBottom  *borderLine

	step          modelAddStep
	allItems      []catalogItem
	filtered      []catalogItem
	selectedIndex int
	nav           listNavigator
	pendingItem   *catalogItem

	// Pre-populated provider API keys
	providerKeys map[string]string

	onDone   func(result ModelAddResult)
	onCancel func()
}

// ModelAddResult contains all information needed to persist and apply the new model.
type ModelAddResult struct {
	ProviderID    string
	ProviderName  string
	ProviderType  string
	BaseURL       string
	ModelID       string
	ModelName     string
	MaxTokens     int64
	ContextWindow int64
	CanReason     bool
	APIKey        string
}

// NewModelAdd creates a new model add component.
func NewModelAdd(
	providerKeys map[string]string,
	onDone func(result ModelAddResult),
	onCancel func(),
) *ModelAddComponent {
	m := &ModelAddComponent{
		searchInput:   components.NewInput(),
		apiKeyInput:   components.NewInput(),
		listContainer: &tui.Container{},
		hintText:      components.NewText(theme.ColorMuted("Type to filter · Enter to select · Esc to cancel"), 0, 0, nil),
		titleText:     components.NewText(theme.ColorAccent("SELECT MODEL"), 0, 0, nil),
		borderTop:     &borderLine{},
		borderBottom:  &borderLine{},
		providerKeys:  providerKeys,
		onDone:        onDone,
		onCancel:      onCancel,
	}

	m.searchInput.SetOnSubmit(func(_ string) {
		m.confirmModelSelection()
	})
	m.searchInput.SetOnEscape(func() {
		if m.onCancel != nil {
			m.onCancel()
		}
	})

	m.apiKeyInput.SetOnSubmit(func(_ string) {
		m.confirmAPIKey()
	})
	m.apiKeyInput.SetOnEscape(func() {
		m.goBackToModelList()
	})

	m.nav = listNavigator{
		count:    func() int { return len(m.filtered) },
		selected: &m.selectedIndex,
		update:   m.updateList,
	}
	m.loadCatalog()
	m.updateList()
	return m
}

func (m *ModelAddComponent) loadCatalog() {
	providers := knownProviders()
	var items []catalogItem
	for _, p := range providers {
		providerName := p.Name
		if providerName == "" {
			providerName = string(p.ID)
		}
		for _, model := range p.Models {
			modelName := model.Name
			if modelName == "" {
				modelName = model.ID
			}
			items = append(items, catalogItem{
				providerID:     string(p.ID),
				providerName:   providerName,
				providerType:   string(p.Type),
				providerAPIKey: p.APIKey,
				baseURL:        p.APIEndpoint,
				modelID:        model.ID,
				modelName:      modelName,
				maxTokens:      model.DefaultMaxTokens,
				contextWindow:  model.ContextWindow,
				canReason:      model.CanReason,
			})
		}
	}
	m.allItems = items
	m.filtered = items
}

func (m *ModelAddComponent) filterCatalog(query string) {
	if strings.TrimSpace(query) == "" {
		m.filtered = m.allItems
	} else {
		m.filtered = fuzzy.FuzzyFilter(m.allItems, query, func(item catalogItem) string {
			return item.providerName + " " + item.modelName + " " + item.modelID
		})
	}
	if m.selectedIndex >= len(m.filtered) {
		m.selectedIndex = max(0, len(m.filtered)-1)
	}
	m.updateList()
}

func (m *ModelAddComponent) updateList() {
	m.listContainer.Clear()

	if len(m.filtered) == 0 {
		m.listContainer.AddChild(components.NewText(theme.ColorMuted("  No matching models"), 0, 0, nil))
		return
	}

	startIndex := max(0,
		min(m.selectedIndex-modelAddMaxVisible/2, len(m.filtered)-modelAddMaxVisible))
	endIndex := min(startIndex+modelAddMaxVisible, len(m.filtered))

	currentProvider := ""
	for i := startIndex; i < endIndex; i++ {
		item := m.filtered[i]
		// Provider header when provider changes
		if item.providerID != currentProvider {
			currentProvider = item.providerID
			providerDisplay := item.providerName
			style := theme.ColorAccent
			if m.providerKeys[item.providerID] != "" {
				providerDisplay += " ✓"
				style = theme.ColorSuccess
			}
			m.listContainer.AddChild(components.NewText(style(providerDisplay), 0, 0, nil))
		}
		line := m.renderCatalogLine(item, i == m.selectedIndex)
		m.listContainer.AddChild(components.NewText(line, 0, 0, nil))
	}

	if startIndex > 0 || endIndex < len(m.filtered) {
		scrollInfo := theme.ColorMuted(fmt.Sprintf("  (%d/%d)", m.selectedIndex+1, len(m.filtered)))
		m.listContainer.AddChild(components.NewText(scrollInfo, 0, 0, nil))
	}
}

func (m *ModelAddComponent) renderCatalogLine(item catalogItem, selected bool) string {
	if selected {
		return theme.ColorAccent("→ " + item.modelName)
	}
	return "  " + item.modelName
}

func (m *ModelAddComponent) confirmModelSelection() {
	if m.selectedIndex < 0 || m.selectedIndex >= len(m.filtered) {
		return
	}
	item := m.filtered[m.selectedIndex]

	if shouldSkipAPIKeyPrompt(item.providerID) {
		if m.onDone != nil {
			m.onDone(ModelAddResult{
				ProviderID:    item.providerID,
				ProviderName:  item.providerName,
				ProviderType:  item.providerType,
				BaseURL:       item.baseURL,
				ModelID:       item.modelID,
				ModelName:     item.modelName,
				MaxTokens:     item.maxTokens,
				ContextWindow: item.contextWindow,
				CanReason:     item.canReason,
				APIKey:        "",
			})
		}
		return
	}

	// Move to API key step
	m.pendingItem = &item
	m.step = stepEnterAPIKey

	// Pre-populate with existing key if available, else try provider default from env placeholder.
	if existingKey := m.providerKeys[item.providerID]; existingKey != "" {
		m.apiKeyInput.SetValue(existingKey)
	} else if envKey := resolveCatalogProviderAPIKey(item.providerID, item.providerAPIKey); envKey != "" {
		m.apiKeyInput.SetValue(envKey)
	} else {
		m.apiKeyInput.SetValue("")
	}

	m.titleText.SetText(theme.ColorAccent(fmt.Sprintf("API KEY FOR %s", strings.ToUpper(item.providerName))))
	m.hintText.SetText(theme.ColorMuted("Enter to confirm · Leave blank to skip · Esc to go back"))
}

func (m *ModelAddComponent) confirmAPIKey() {
	if m.pendingItem == nil {
		return
	}
	apiKey := strings.TrimSpace(m.apiKeyInput.GetValue())
	if m.onDone != nil {
		m.onDone(ModelAddResult{
			ProviderID:    m.pendingItem.providerID,
			ProviderName:  m.pendingItem.providerName,
			ProviderType:  m.pendingItem.providerType,
			BaseURL:       m.pendingItem.baseURL,
			ModelID:       m.pendingItem.modelID,
			ModelName:     m.pendingItem.modelName,
			MaxTokens:     m.pendingItem.maxTokens,
			ContextWindow: m.pendingItem.contextWindow,
			CanReason:     m.pendingItem.canReason,
			APIKey:        apiKey,
		})
	}
}

func (m *ModelAddComponent) goBackToModelList() {
	m.step = stepSelectModel
	m.pendingItem = nil
	m.apiKeyInput.SetValue("")
	m.titleText.SetText(theme.ColorAccent("SELECT MODEL"))
	m.hintText.SetText(theme.ColorMuted("Type to filter · Enter to select · Esc to cancel"))
}

// --- Component interface ---

func (m *ModelAddComponent) Render(width int) []string {
	var lines []string
	lines = append(lines, m.borderTop.Render(width)...)
	lines = append(lines, "")
	lines = append(lines, m.titleText.Render(width)...)
	lines = append(lines, "")

	switch m.step {
	case stepSelectModel:
		lines = append(lines, m.listContainer.Render(width)...)
		lines = append(lines, "")
		lines = append(lines, m.hintText.Render(width)...)
		lines = append(lines, m.borderBottom.Render(width)...)
		lines = append(lines, m.searchInput.Render(width)...)
		lines = append(lines, m.borderBottom.Render(width)...)
	case stepEnterAPIKey:
		lines = append(lines, m.hintText.Render(width)...)
		lines = append(lines, m.borderBottom.Render(width)...)
		lines = append(lines, m.apiKeyInput.Render(width)...)
		lines = append(lines, m.borderBottom.Render(width)...)
	}
	return lines
}

func (m *ModelAddComponent) HandleInput(data string) {
	switch m.step {
	case stepSelectModel:
		m.handleModelSelectInput(data)
	case stepEnterAPIKey:
		m.handleAPIKeyInput(data)
	}
}

func (m *ModelAddComponent) handleModelSelectInput(data string) {
	kb := keybindings.GetEditorKeybindings()

	if m.nav.handleNav(data) {
		return
	}
	if kb.Matches(data, keybindings.ActionSelectConfirm) {
		m.confirmModelSelection()
		return
	}
	if kb.Matches(data, keybindings.ActionSelectCancel) {
		if m.onCancel != nil {
			m.onCancel()
		}
		return
	}

	// Everything else goes to the search input, then re-filter
	m.searchInput.HandleInput(data)
	m.filterCatalog(m.searchInput.GetValue())
}

func (m *ModelAddComponent) handleAPIKeyInput(data string) {
	kb := keybindings.GetEditorKeybindings()

	if kb.Matches(data, keybindings.ActionSelectConfirm) {
		m.confirmAPIKey()
		return
	}
	if kb.Matches(data, keybindings.ActionSelectCancel) {
		m.goBackToModelList()
		return
	}

	// Forward to API key input
	m.apiKeyInput.HandleInput(data)
}

func (m *ModelAddComponent) Invalidate() {
	m.listContainer.Invalidate()
	m.searchInput.Invalidate()
	m.apiKeyInput.Invalidate()
}

// --- Focusable interface (delegate to active Input for cursor positioning) ---

func (m *ModelAddComponent) SetFocused(focused bool) {
	switch m.step {
	case stepSelectModel:
		m.searchInput.SetFocused(focused)
		m.apiKeyInput.SetFocused(false)
	case stepEnterAPIKey:
		m.apiKeyInput.SetFocused(focused)
		m.searchInput.SetFocused(false)
	}
}

func (m *ModelAddComponent) IsFocused() bool {
	switch m.step {
	case stepSelectModel:
		return m.searchInput.GetFocused()
	case stepEnterAPIKey:
		return m.apiKeyInput.GetFocused()
	}
	return false
}

var (
	_ tui.Component = (*ModelAddComponent)(nil)
	_ tui.Focusable = (*ModelAddComponent)(nil)
)

// ---------------------------------------------------------------------------
// Provider catalog helpers
// ---------------------------------------------------------------------------

func knownProviders() []catalog.Provider {
	source := catalog.NewRaijinSource()

	result, err := source.ListProviders(context.Background())
	if err != nil {
		return nil
	}
	return result
}

func resolveCatalogProviderAPIKey(providerID, raw string) string {
	trimmed := strings.TrimSpace(raw)
	if strings.HasPrefix(trimmed, "$") {
		envVar := strings.TrimPrefix(trimmed, "$")
		if envVar != "" {
			if value := strings.TrimSpace(os.Getenv(envVar)); value != "" {
				return value
			}
		}
	}
	if trimmed != "" && !strings.HasPrefix(trimmed, "$") {
		return trimmed
	}

	for _, envVar := range fallbackProviderAPIKeyEnvVars(providerID) {
		if value := strings.TrimSpace(os.Getenv(envVar)); value != "" {
			return value
		}
	}
	return ""
}

func shouldSkipAPIKeyPrompt(providerID string) bool {
	return strings.EqualFold(strings.TrimSpace(providerID), catalog.OpenAICodexProviderID)
}

func fallbackProviderAPIKeyEnvVars(providerID string) []string {
	switch strings.ToLower(strings.TrimSpace(providerID)) {
	case "openai":
		return []string{"OPENAI_API_KEY"}
	case "anthropic":
		return []string{"ANTHROPIC_API_KEY"}
	case "gemini", "google":
		return []string{"GEMINI_API_KEY", "GOOGLE_API_KEY"}
	case "openrouter":
		return []string{"OPENROUTER_API_KEY"}
	case "synthetic":
		return []string{"SYNTHETIC_API_KEY"}
	case "opencode", "opencode-zen-free":
		return []string{"OPENCODE_API_KEY", "OPENCODE_ZEN_API_KEY"}
	case "xai":
		return []string{"XAI_API_KEY"}
	case "zai":
		return []string{"ZAI_API_KEY"}
	case "groq":
		return []string{"GROQ_API_KEY"}
	case "cerebras":
		return []string{"CEREBRAS_API_KEY"}
	case "venice":
		return []string{"VENICE_API_KEY"}
	case "chutes":
		return []string{"CHUTES_API_KEY"}
	case "huggingface":
		return []string{"HUGGINGFACE_API_KEY", "HF_TOKEN"}
	case "aihubmix":
		return []string{"AIHUBMIX_API_KEY"}
	case "kimi-coding":
		return []string{"KIMI_API_KEY", "MOONSHOT_API_KEY"}
	case "copilot":
		return []string{"GITHUB_TOKEN", "COPILOT_API_KEY"}
	case "vercel":
		return []string{"VERCEL_API_KEY", "VERCEL_TOKEN"}
	case "minimax":
		return []string{"MINIMAX_API_KEY"}
	case "ionet":
		return []string{"IONET_API_KEY"}
	case "azure":
		return []string{"AZURE_OPENAI_API_KEY", "AZURE_API_KEY"}
	default:
		return nil
	}
}
