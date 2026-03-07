package chat

import (
	"fmt"
	"os"
	"strings"

	"github.com/francescoalemanno/raijin-mono/libtui/pkg/components"
	"github.com/francescoalemanno/raijin-mono/libtui/pkg/fuzzy"
	"github.com/francescoalemanno/raijin-mono/libtui/pkg/keybindings"
	"github.com/francescoalemanno/raijin-mono/libtui/pkg/tui"

	libagent "github.com/francescoalemanno/raijin-mono/libagent"

	"github.com/francescoalemanno/raijin-mono/internal/theme"
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
		hintText:      components.NewText(theme.Default.Muted.Ansi24("Type to filter · Enter to select · Esc to cancel"), 0, 0, nil),
		titleText:     components.NewText(theme.Default.Accent.Ansi24("SELECT MODEL"), 0, 0, nil),
		borderTop:     &borderLine{},
		borderBottom:  &borderLine{},
		providerKeys:  providerKeys,
		onDone:        onDone,
		onCancel:      onCancel,
	}

	// Set foreground color for padding/margins
	m.hintText.SetFgColorFn(theme.Default.Foreground.Ansi24)
	m.searchInput.SetPaddingColorFn(theme.Default.Foreground.Ansi24)
	m.apiKeyInput.SetPaddingColorFn(theme.Default.Foreground.Ansi24)

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
	items := knownCatalogItems()
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
		m.listContainer.AddChild(components.NewText(theme.Default.Muted.Ansi24("  No matching models"), 0, 0, nil))
		return
	}

	startIndex, endIndex := visibleRange(m.selectedIndex, len(m.filtered), modelAddMaxVisible)

	currentProvider := ""
	for i := startIndex; i < endIndex; i++ {
		item := m.filtered[i]
		// Provider header when provider changes
		if item.providerID != currentProvider {
			currentProvider = item.providerID
			providerDisplay := item.providerName
			style := theme.Default.Accent.Ansi24
			if m.providerKeys[item.providerID] != "" {
				providerDisplay += " ✓"
				style = theme.Default.Success.Ansi24
			}
			m.listContainer.AddChild(components.NewText(style(providerDisplay), 0, 0, nil))
		}
		line := m.renderCatalogLine(item, i == m.selectedIndex)
		m.listContainer.AddChild(components.NewText(line, 0, 0, nil))
	}

	appendScrollInfo(m.listContainer, m.selectedIndex, len(m.filtered), startIndex, endIndex)
}

func (m *ModelAddComponent) renderCatalogLine(item catalogItem, selected bool) string {
	if selected {
		return theme.Default.Accent.Ansi24("→ " + item.modelName)
	}
	return theme.Default.Foreground.Ansi24("  ") + theme.Default.Foreground.Ansi24(item.modelName)
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

	m.titleText.SetText(theme.Default.Accent.Ansi24(fmt.Sprintf("API KEY FOR %s", strings.ToUpper(item.providerName))))
	m.hintText.SetText(theme.Default.Muted.Ansi24("Enter to confirm · Leave blank to skip · Esc to go back"))
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
	m.titleText.SetText(theme.Default.Accent.Ansi24("SELECT MODEL"))
	m.hintText.SetText(theme.Default.Muted.Ansi24("Type to filter · Enter to select · Esc to cancel"))
}

// --- Component interface ---

func (m *ModelAddComponent) Render(width int) []string {
	switch m.step {
	case stepSelectModel:
		return renderSelectorFrame(width, m.borderTop, m.borderBottom, m.titleText, m.listContainer, m.hintText, m.searchInput)
	case stepEnterAPIKey:
		return renderPromptInputFrame(width, m.borderTop, m.borderBottom, m.titleText, m.hintText, m.apiKeyInput)
	}
	return nil
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

// knownCatalogItems returns all model entries from the embedded catalog.
func knownCatalogItems() []catalogItem {
	cat := libagent.DefaultCatalog()
	var items []catalogItem

	// Unified providers list (catwalk + custom providers).
	for _, p := range cat.ListProviders() {
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
				providerID:    string(p.ID),
				providerName:  providerName,
				providerType:  string(p.Type),
				baseURL:       p.APIEndpoint,
				modelID:       model.ID,
				modelName:     modelName,
				maxTokens:     model.DefaultMaxTokens,
				contextWindow: model.ContextWindow,
				canReason:     model.CanReason,
			})
		}
	}

	return items
}

func resolveCatalogProviderAPIKey(providerID, raw string) string {
	trimmed := strings.TrimSpace(raw)
	if after, ok := strings.CutPrefix(trimmed, "$"); ok {
		envVar := after
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
	return strings.EqualFold(strings.TrimSpace(providerID), libagent.CodexProviderID)
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
	case "opencode", "opencode-go", "opencode-zen-free":
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
