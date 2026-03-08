package chat

import (
	"fmt"
	"sort"
	"strings"

	"github.com/francescoalemanno/raijin-mono/libtui/pkg/components"
	"github.com/francescoalemanno/raijin-mono/libtui/pkg/fuzzy"
	"github.com/francescoalemanno/raijin-mono/libtui/pkg/keybindings"
	"github.com/francescoalemanno/raijin-mono/libtui/pkg/keys"
	"github.com/francescoalemanno/raijin-mono/libtui/pkg/tui"

	modelconfig "github.com/francescoalemanno/raijin-mono/internal/config"
	"github.com/francescoalemanno/raijin-mono/internal/theme"
)

const modelSelectorMaxVisible = 10

type modelItem struct {
	name          string // key in ModelStore
	provider      string
	model         string
	contextWindow int64
}

// ModelSelectorComponent renders a filterable model list with an Input for search.
// Layout: border → list → border → input
type ModelSelectorComponent struct {
	searchInput   *components.Input
	listContainer *tui.Container
	hintText      *components.Text
	titleText     *components.Text
	borderTop     *borderLine
	borderBottom  *borderLine

	allModels     []modelItem
	filtered      []modelItem
	selectedIndex int
	currentModel  string // name of the currently active model
	nav           listNavigator
	pendingDelete string // name of model awaiting delete confirmation

	onSelect func(name string)
	onDelete func(name string)
	onCancel func()
}

// NewModelSelector creates a new model selector component.
func NewModelSelector(
	store *modelconfig.ModelStore,
	currentModel string,
	title string,
	onSelect func(name string),
	onDelete func(name string),
	onCancel func(),
) *ModelSelectorComponent {
	title = strings.TrimSpace(title)
	if title == "" {
		title = "SELECT MODEL"
	}

	m := &ModelSelectorComponent{
		searchInput:   components.NewInput(),
		listContainer: &tui.Container{},
		hintText:      components.NewText(theme.Default.Muted.Ansi24("Type to filter · ↑/↓ move · ←/→ page · Enter select · ctrl+x delete · Esc cancel"), 0, 0, nil),
		titleText:     components.NewText(theme.Default.Accent.Ansi24(title), 0, 0, nil),
		borderTop:     &borderLine{},
		borderBottom:  &borderLine{},
		currentModel:  currentModel,
		onSelect:      onSelect,
		onDelete:      onDelete,
		onCancel:      onCancel,
	}

	// Set foreground color for padding/margins
	m.hintText.SetFgColorFn(theme.Default.Foreground.Ansi24)
	m.searchInput.SetPaddingColorFn(theme.Default.Foreground.Ansi24)

	m.searchInput.SetOnSubmit(func(_ string) {
		m.confirmSelection()
	})
	m.searchInput.SetOnEscape(func() {
		if m.onCancel != nil {
			m.onCancel()
		}
	})

	m.nav = listNavigator{
		count:    func() int { return len(m.filtered) },
		selected: &m.selectedIndex,
		update:   m.updateList,
		pageSize: modelSelectorMaxVisible,
	}
	m.loadModels(store)
	m.updateList()
	return m
}

func (m *ModelSelectorComponent) loadModels(store *modelconfig.ModelStore) {
	if store == nil {
		return
	}
	names := store.List()
	items := make([]modelItem, 0, len(names))
	for _, name := range names {
		cfg, ok := store.Get(name)
		if !ok {
			continue
		}
		items = append(items, modelItem{
			name:          name,
			provider:      cfg.Provider,
			model:         cfg.Model,
			contextWindow: cfg.ContextWindow,
		})
	}
	// Sort: current model first, then by context window (larger first), then alphabetically
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].name == m.currentModel {
			return true
		}
		if items[j].name == m.currentModel {
			return false
		}
		if items[i].contextWindow != items[j].contextWindow {
			return items[i].contextWindow > items[j].contextWindow
		}
		return items[i].name < items[j].name
	})
	m.allModels = items
	m.filtered = items
}

func (m *ModelSelectorComponent) filterModels(query string) {
	m.pendingDelete = "" // reset confirmation on any filter change
	if strings.TrimSpace(query) == "" {
		m.filtered = m.allModels
	} else {
		m.filtered = fuzzy.FuzzyFilter(m.allModels, query, func(item modelItem) string {
			return item.name + " " + item.provider + " " + item.model
		})
	}
	if m.selectedIndex >= len(m.filtered) {
		m.selectedIndex = max(0, len(m.filtered)-1)
	}
	m.updateList()
}

func (m *ModelSelectorComponent) updateList() {
	m.listContainer.Clear()

	// Update hint text based on pending-delete state.
	if m.pendingDelete != "" {
		m.hintText.SetText(theme.Default.Danger.Ansi24("Press ctrl+x again to confirm deletion · Esc to cancel"))
	} else {
		m.hintText.SetText(theme.Default.Muted.Ansi24("Type to filter · ↑/↓ move · ←/→ page · Enter select · ctrl+x delete · Esc cancel"))
	}

	if len(m.filtered) == 0 {
		m.listContainer.AddChild(components.NewText(theme.Default.Muted.Ansi24("  No matching models"), 0, 0, nil))
		return
	}

	startIndex, endIndex := visibleRange(m.selectedIndex, len(m.filtered), modelSelectorMaxVisible)

	for i := startIndex; i < endIndex; i++ {
		item := m.filtered[i]
		line := m.renderModelLine(item, i == m.selectedIndex)
		m.listContainer.AddChild(components.NewText(line, 0, 0, nil))
	}

	appendScrollInfo(m.listContainer, m.selectedIndex, len(m.filtered), startIndex, endIndex)
}

func (m *ModelSelectorComponent) renderModelLine(item modelItem, selected bool) string {
	isCurrent := item.name == m.currentModel
	checkmark := ""
	if isCurrent {
		checkmark = theme.Default.Success.Ansi24(" ✓")
	}
	providerBadge := theme.Default.Muted.Ansi24(fmt.Sprintf(" [%s]", item.provider))

	awaitingDelete := m.pendingDelete == item.name
	if selected {
		if awaitingDelete {
			return theme.Default.Danger.Ansi24("→ "+item.name) + providerBadge + checkmark
		}
		return theme.Default.Accent.Ansi24("→ "+item.name) + providerBadge + checkmark
	}
	if awaitingDelete {
		return theme.Default.Danger.Ansi24("  "+item.name) + providerBadge + checkmark
	}
	return theme.Default.Foreground.Ansi24("  ") + theme.Default.Foreground.Ansi24(item.name) + providerBadge + checkmark
}

func (m *ModelSelectorComponent) confirmSelection() {
	if m.selectedIndex >= 0 && m.selectedIndex < len(m.filtered) {
		if m.onSelect != nil {
			m.onSelect(m.filtered[m.selectedIndex].name)
		}
	}
}

func (m *ModelSelectorComponent) handleDeleteKey() {
	if m.onDelete == nil {
		return
	}
	if m.selectedIndex < 0 || m.selectedIndex >= len(m.filtered) {
		return
	}
	target := m.filtered[m.selectedIndex]

	if m.pendingDelete != target.name {
		// First press: arm the confirmation.
		m.pendingDelete = target.name
		m.updateList()
		return
	}

	// Second press: confirmed — remove from list and notify.
	m.pendingDelete = ""
	m.allModels = removeModelByName(m.allModels, target.name)
	// Rebuild filtered from allModels to ensure consistency
	query := m.searchInput.GetValue()
	if strings.TrimSpace(query) == "" {
		m.filtered = make([]modelItem, len(m.allModels))
		copy(m.filtered, m.allModels)
	} else {
		m.filtered = fuzzy.FuzzyFilter(m.allModels, query, func(item modelItem) string {
			return item.name + " " + item.provider + " " + item.model
		})
	}
	if m.selectedIndex >= len(m.filtered) {
		m.selectedIndex = max(0, len(m.filtered)-1)
	}
	m.updateList()

	m.onDelete(target.name)
}

func removeModelByName(slice []modelItem, name string) []modelItem {
	out := slice[:0:len(slice)]
	for _, c := range slice {
		if c.name != name {
			out = append(out, c)
		}
	}
	return out
}

// --- Component interface ---

func (m *ModelSelectorComponent) Render(width int) []string {
	return renderSelectorFrame(width, m.borderTop, m.borderBottom, m.titleText, m.listContainer, m.hintText, m.searchInput)
}

func (m *ModelSelectorComponent) HandleInput(data string) {
	kb := keybindings.GetEditorKeybindings()

	if keys.ParseKey(data) == "ctrl+x" {
		m.handleDeleteKey()
		return
	}

	// Navigation cancels any pending delete confirmation.
	if m.nav.handleNav(data) {
		if m.pendingDelete != "" {
			m.pendingDelete = ""
			m.updateList()
		}
		return
	}
	if kb.Matches(data, keybindings.ActionSelectConfirm) {
		if m.pendingDelete != "" {
			m.pendingDelete = ""
			m.updateList()
			return
		}
		m.confirmSelection()
		return
	}
	if kb.Matches(data, keybindings.ActionSelectCancel) {
		if m.pendingDelete != "" {
			m.pendingDelete = ""
			m.updateList()
			return
		}
		if m.onCancel != nil {
			m.onCancel()
		}
		return
	}

	// Everything else goes to the search input, then re-filter
	m.searchInput.HandleInput(data)
	m.filterModels(m.searchInput.GetValue())
}

func (m *ModelSelectorComponent) Invalidate() {
	m.listContainer.Invalidate()
	m.searchInput.Invalidate()
}

// --- Focusable interface (delegate to Input for cursor positioning) ---

func (m *ModelSelectorComponent) SetFocused(focused bool) {
	m.searchInput.SetFocused(focused)
}

func (m *ModelSelectorComponent) IsFocused() bool {
	return m.searchInput.GetFocused()
}

var (
	_ tui.Component = (*ModelSelectorComponent)(nil)
	_ tui.Focusable = (*ModelSelectorComponent)(nil)
)

// borderLine renders a horizontal rule scaled to the terminal width.
type borderLine struct{}

func (b *borderLine) Render(width int) []string {
	if width < 1 {
		return []string{""}
	}
	return []string{theme.Default.Muted.Ansi24(strings.Repeat("─", width))}
}

func (b *borderLine) HandleInput(string) {}
func (b *borderLine) Invalidate()        {}

var _ tui.Component = (*borderLine)(nil)
