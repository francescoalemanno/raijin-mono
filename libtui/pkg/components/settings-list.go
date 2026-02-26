package components

import (
	"strconv"
	"strings"

	"github.com/francescoalemanno/raijin-mono/libtui/pkg/fuzzy"
	"github.com/francescoalemanno/raijin-mono/libtui/pkg/keybindings"
	"github.com/francescoalemanno/raijin-mono/libtui/pkg/tui"
	"github.com/francescoalemanno/raijin-mono/libtui/pkg/utils"
)

// SettingItem represents a setting in a SettingsList.
type SettingItem struct {
	// Unique identifier for this setting
	ID string
	// Display label (left side)
	Label string
	// Optional description shown when selected
	Description string
	// Current value to display (right side)
	CurrentValue string
	// If provided, Enter/Space cycles through these values
	Values []string
	// If provided, Enter opens this submenu. Receives current value and done callback.
	Submenu func(currentValue string, done func(selectedValue *string)) tui.Component
}

// SettingsListTheme defines the theme for SettingsList rendering.
type SettingsListTheme struct {
	Label       func(text string, selected bool) string
	Value       func(text string, selected bool) string
	Description func(text string) string
	Cursor      string
	Hint        func(text string) string
	Prefix      func(text string) string // For non-selected item prefix
}

// SettingsListOptions provides configuration options for SettingsList.
type SettingsListOptions struct {
	EnableSearch bool
}

// SettingsList is a component for displaying and editing settings.
type SettingsList struct {
	items            []SettingItem
	filteredItems    []SettingItem
	theme            SettingsListTheme
	selectedIndex    int
	maxVisible       int
	onChange         func(id string, newValue string)
	onCancel         func()
	searchInput      *Input
	searchEnabled    bool
	submenuComponent tui.Component
	submenuItemIndex int
	lastSelectedID   string
}

// NewSettingsList creates a new SettingsList.
func NewSettingsList(
	items []SettingItem,
	maxVisible int,
	theme SettingsListTheme,
	onChange func(id string, newValue string),
	onCancel func(),
	options SettingsListOptions,
) *SettingsList {
	s := &SettingsList{
		items:         items,
		filteredItems: items,
		maxVisible:    maxVisible,
		theme:         theme,
		onChange:      onChange,
		onCancel:      onCancel,
		searchEnabled: options.EnableSearch,
	}
	if len(items) > 0 {
		s.lastSelectedID = items[0].ID
	}
	if s.searchEnabled {
		s.searchInput = NewInput()
	}
	return s
}

// UpdateValue updates an item's currentValue.
func (s *SettingsList) UpdateValue(id string, newValue string) {
	for i := range s.items {
		if s.items[i].ID == id {
			s.items[i].CurrentValue = newValue
			break
		}
	}
}

// Invalidate satisfies Component interface.
func (s *SettingsList) Invalidate() {
	if s.submenuComponent != nil {
		s.submenuComponent.Invalidate()
	}
}

// Render renders the SettingsList.
func (s *SettingsList) Render(width int) []string {
	// If submenu is active, render it instead
	if s.submenuComponent != nil {
		return s.submenuComponent.Render(width)
	}

	return s.renderMainList(width)
}

func (s *SettingsList) renderMainList(width int) []string {
	lines := []string{}

	if s.searchEnabled && s.searchInput != nil {
		lines = append(lines, s.searchInput.Render(width)...)
		lines = append(lines, "")
	}

	if len(s.items) == 0 {
		lines = append(lines, s.theme.Hint("  No settings available"))
		if s.searchEnabled {
			s.addHintLine(&lines, width)
		}
		return lines
	}

	displayItems := s.items
	if s.searchEnabled {
		displayItems = s.filteredItems
	}

	if len(displayItems) == 0 {
		lines = append(lines, utils.TruncateToWidth(s.theme.Hint("  No matching settings"), width))
		s.addHintLine(&lines, width)
		return lines
	}

	// Calculate visible range with scrolling
	startIndex := max(0,
		min(s.selectedIndex-s.maxVisible/2, len(displayItems)-s.maxVisible))
	endIndex := min(startIndex+s.maxVisible, len(displayItems))

	// Calculate max label width for alignment
	maxLabelWidth := s.maxLabelWidth(displayItems)
	maxLabelWidth = min(30, maxLabelWidth)

	// Render visible items
	for i := startIndex; i < endIndex; i++ {
		if i < 0 || i >= len(displayItems) {
			continue
		}
		item := displayItems[i]
		line := s.renderItem(item, i == s.selectedIndex, maxLabelWidth, width)
		lines = append(lines, line)
	}

	// Add scroll indicator if needed
	if startIndex > 0 || endIndex < len(displayItems) {
		scrollText := s.theme.Hint(
			utils.TruncateToWidth(
				strings.Join([]string{
					"  (",
					strconv.Itoa(s.selectedIndex + 1),
					"/",
					strconv.Itoa(len(displayItems)),
					")",
				}, ""),
				width-2,
				"",
			))
		lines = append(lines, scrollText)
	}

	// Add description for selected item
	if s.selectedIndex >= 0 && s.selectedIndex < len(displayItems) {
		selectedItem := displayItems[s.selectedIndex]
		if selectedItem.Description != "" {
			lines = append(lines, "")
			wrappedDesc := utils.WrapTextWithAnsi(selectedItem.Description, width-4)
			for _, line := range wrappedDesc {
				lines = append(lines, s.theme.Description("  "+line))
			}
		}
	}

	// Add hint
	s.addHintLine(&lines, width)

	return lines
}

func (s *SettingsList) maxLabelWidth(items []SettingItem) int {
	maxWidth := 0
	for _, item := range items {
		w := utils.VisibleWidth(item.Label)
		if w > maxWidth {
			maxWidth = w
		}
	}
	return maxWidth
}

func (s *SettingsList) renderItem(item SettingItem, isSelected bool, maxLabelWidth int, width int) string {
	prefix := s.theme.Cursor
	if !isSelected {
		prefix = "  "
		if s.theme.Prefix != nil {
			prefix = s.theme.Prefix(prefix)
		}
	}
	prefixWidth := utils.VisibleWidth(prefix)

	// Pad label to align values
	labelPadding := strings.Repeat(" ", max(0, maxLabelWidth-utils.VisibleWidth(item.Label)))
	if s.theme.Prefix != nil {
		labelPadding = s.theme.Prefix(labelPadding)
	}
	labelPadded := item.Label + labelPadding
	labelText := s.theme.Label(labelPadded, isSelected)

	// Calculate space for value
	separator := "  "
	if s.theme.Prefix != nil {
		separator = s.theme.Prefix(separator)
	}
	usedWidth := prefixWidth + maxLabelWidth + len("  ")
	valueMaxWidth := width - usedWidth - 2

	valueText := s.theme.Value(utils.TruncateToWidth(item.CurrentValue, valueMaxWidth, ""), isSelected)

	return utils.TruncateToWidth(prefix+labelText+separator+valueText, width)
}

// HandleInput handles keyboard input.
func (s *SettingsList) HandleInput(data string) {
	// If submenu is active, delegate all input to it
	if s.submenuComponent != nil {
		s.submenuComponent.HandleInput(data)
		return
	}

	// Main list input handling
	kb := keybindings.GetEditorKeybindings()
	displayItems := s.items
	if s.searchEnabled {
		displayItems = s.filteredItems
	}

	if kb.Matches(data, keybindings.ActionSelectUp) {
		if len(displayItems) == 0 {
			return
		}
		if s.selectedIndex == 0 {
			s.selectedIndex = len(displayItems) - 1
		} else {
			s.selectedIndex--
		}
		s.lastSelectedID = displayItems[s.selectedIndex].ID
	} else if kb.Matches(data, keybindings.ActionSelectDown) {
		if len(displayItems) == 0 {
			return
		}
		if s.selectedIndex == len(displayItems)-1 {
			s.selectedIndex = 0
		} else {
			s.selectedIndex++
		}
		s.lastSelectedID = displayItems[s.selectedIndex].ID
	} else if kb.Matches(data, keybindings.ActionSelectConfirm) || data == " " {
		s.activateItem()
	} else if kb.Matches(data, keybindings.ActionSelectCancel) {
		s.onCancel()
	} else if s.searchEnabled && s.searchInput != nil {
		sanitized := strings.ReplaceAll(data, " ", "")
		if sanitized == "" {
			return
		}
		s.searchInput.HandleInput(sanitized)
		s.applyFilter(s.searchInput.GetValue())
	}
}

func (s *SettingsList) findMasterItem(id string) *SettingItem {
	for i := range s.items {
		if s.items[i].ID == id {
			return &s.items[i]
		}
	}
	return nil
}

func (s *SettingsList) activateItem() {
	displayItems := s.items
	if s.searchEnabled {
		displayItems = s.filteredItems
	}

	if s.selectedIndex < 0 || s.selectedIndex >= len(displayItems) {
		return
	}

	// Use the display item only for reading ID/Values; mutate via master pointer.
	displayed := displayItems[s.selectedIndex]
	s.lastSelectedID = displayed.ID
	item := s.findMasterItem(displayed.ID)
	if item == nil {
		return
	}

	if item.Submenu != nil {
		// Open submenu, passing current value so it can pre-select correctly
		s.submenuItemIndex = s.selectedIndex
		s.submenuComponent = item.Submenu(item.CurrentValue, func(selectedValue *string) {
			if selectedValue != nil {
				item.CurrentValue = *selectedValue
				s.onChange(item.ID, *selectedValue)
			}
			s.closeSubmenu()
		})
	} else if len(item.Values) > 0 {
		// Cycle through values
		currentIndex := -1
		for i, v := range item.Values {
			if v == item.CurrentValue {
				currentIndex = i
				break
			}
		}
		nextIndex := 0
		if currentIndex >= 0 {
			nextIndex = (currentIndex + 1) % len(item.Values)
		}
		newValue := item.Values[nextIndex]
		item.CurrentValue = newValue
		s.onChange(item.ID, newValue)
	}
}

func (s *SettingsList) closeSubmenu() {
	s.submenuComponent = nil
	// Restore selection to the item that opened the submenu
	if s.submenuItemIndex >= 0 {
		s.selectedIndex = s.submenuItemIndex
		s.submenuItemIndex = -1
	}
	displayItems := s.items
	if s.searchEnabled {
		displayItems = s.filteredItems
	}
	if s.selectedIndex >= 0 && s.selectedIndex < len(displayItems) {
		s.lastSelectedID = displayItems[s.selectedIndex].ID
	}
}

func (s *SettingsList) applyFilter(query string) {
	prevID := s.lastSelectedID
	if s.selectedIndex >= 0 && s.selectedIndex < len(s.filteredItems) {
		prevID = s.filteredItems[s.selectedIndex].ID
	}
	s.filteredItems = fuzzy.FuzzyFilter(s.items, query, func(item SettingItem) string {
		return item.Label
	})
	s.selectedIndex = 0
	if prevID != "" {
		for i, item := range s.filteredItems {
			if item.ID == prevID {
				s.selectedIndex = i
				break
			}
		}
	}
	if s.selectedIndex >= 0 && s.selectedIndex < len(s.filteredItems) {
		s.lastSelectedID = s.filteredItems[s.selectedIndex].ID
	}
}

func (s *SettingsList) addHintLine(lines *[]string, width int) {
	*lines = append(*lines, "")
	hintText := "  Enter/Space to change · Esc to cancel"
	if s.searchEnabled {
		hintText = "  Type to search · Enter/Space to change · Esc to cancel"
	}
	*lines = append(*lines, utils.TruncateToWidth(s.theme.Hint(hintText), width))
}

// Ensure SettingsList implements Component interface.
var _ tui.Component = (*SettingsList)(nil)
