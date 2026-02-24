package components

import (
	"strconv"
	"strings"

	"github.com/francescoalemanno/raijin-mono/libtui/pkg/keybindings"
	"github.com/francescoalemanno/raijin-mono/libtui/pkg/tui"
	"github.com/francescoalemanno/raijin-mono/libtui/pkg/utils"
)

// SelectItem represents an item in a SelectList.
type SelectItem struct {
	Value       string
	Label       string
	Description string
}

// SelectListTheme defines the theme for SelectList rendering.
type SelectListTheme struct {
	SelectedPrefix func(string) string
	SelectedText   func(string) string
	Description    func(string) string
	ScrollInfo     func(string) string
	NoMatch        func(string) string
}

// SelectList is a component for selecting items from a list.
type SelectList struct {
	items             []SelectItem
	filteredItems     []SelectItem
	selectedIndex     int
	maxVisible        int
	theme             SelectListTheme
	OnSelect          func(item SelectItem)
	OnCancel          func()
	OnSelectionChange func(item SelectItem)
}

// NewSelectList creates a new SelectList.
func NewSelectList(items []SelectItem, maxVisible int, theme SelectListTheme) *SelectList {
	return &SelectList{
		items:         items,
		filteredItems: items,
		selectedIndex: 0,
		maxVisible:    maxVisible,
		theme:         theme,
	}
}

// SetFilter filters items by value prefix (case-insensitive).
func (s *SelectList) SetFilter(filter string) {
	filter = strings.ToLower(filter)
	s.filteredItems = []SelectItem{}
	for _, item := range s.items {
		if strings.HasPrefix(strings.ToLower(item.Value), filter) {
			s.filteredItems = append(s.filteredItems, item)
		}
	}
	s.selectedIndex = 0
}

// SetSelectedIndex sets the selected index, clamped to valid range.
func (s *SelectList) SetSelectedIndex(index int) {
	s.selectedIndex = max(0, min(index, len(s.filteredItems)-1))
}

// Invalidate satisfies Component interface (no-op for SelectList).
func (s *SelectList) Invalidate() {
	// No cached state to invalidate currently
}

// Render renders the SelectList.
func (s *SelectList) Render(width int) []string {
	lines := []string{}

	// If no items match filter, show message
	if len(s.filteredItems) == 0 {
		lines = append(lines, s.theme.NoMatch("  No matching commands"))
		return lines
	}

	// Calculate visible range with scrolling
	startIndex := max(0,
		min(s.selectedIndex-s.maxVisible/2, len(s.filteredItems)-s.maxVisible))
	endIndex := min(startIndex+s.maxVisible, len(s.filteredItems))

	// Render visible items
	for i := startIndex; i < endIndex; i++ {
		item := s.filteredItems[i]
		lines = append(lines, s.renderItem(item, i == s.selectedIndex, width)...)
	}

	// Add scroll indicators if needed
	if startIndex > 0 || endIndex < len(s.filteredItems) {
		scrollText := s.theme.ScrollInfo(
			utils.TruncateToWidth(
				strings.Join([]string{
					"  (",
					strconv.Itoa(s.selectedIndex + 1),
					"/",
					strconv.Itoa(len(s.filteredItems)),
					")",
				}, ""),
				width-2,
				"",
			))
		lines = append(lines, scrollText)
	}

	return lines
}

func (s *SelectList) renderItem(item SelectItem, isSelected bool, width int) []string {
	descriptionSingleLine := ""
	if item.Description != "" {
		descriptionSingleLine = normalizeToSingleLine(item.Description)
	}

	prefixWidth := 2 // "→ " is 2 characters visually
	displayValue := item.Label
	if displayValue == "" {
		displayValue = item.Value
	}

	if isSelected {
		// Use arrow indicator for selection - entire line uses selectedText color
		if descriptionSingleLine != "" && width > 40 {
			// Calculate how much space we have for value + description
			maxValueWidth := min(30, width-prefixWidth-4)
			truncatedValue := utils.TruncateToWidth(displayValue, maxValueWidth, "")
			spacing := strings.Repeat(" ", max(1, 32-len(truncatedValue)))

			// Calculate remaining space for description
			descriptionStart := prefixWidth + len(truncatedValue) + len(spacing)
			remainingWidth := width - descriptionStart - 2 // -2 for safety

			if remainingWidth > 10 {
				truncatedDesc := utils.TruncateToWidth(descriptionSingleLine, remainingWidth, "")
				// Apply selectedText to entire line content
				line := s.theme.SelectedText("→ " + truncatedValue + spacing + truncatedDesc)
				return []string{line}
			}
		}

		// Not enough space for description
		maxWidth := width - prefixWidth - 2
		line := s.theme.SelectedText("→ " + utils.TruncateToWidth(displayValue, maxWidth, ""))
		return []string{line}
	} else {
		prefix := "  "

		if descriptionSingleLine != "" && width > 40 {
			// Calculate how much space we have for value + description
			maxValueWidth := min(30, width-len(prefix)-4)
			truncatedValue := utils.TruncateToWidth(displayValue, maxValueWidth, "")
			spacing := strings.Repeat(" ", max(1, 32-len(truncatedValue)))

			// Calculate remaining space for description
			descriptionStart := len(prefix) + len(truncatedValue) + len(spacing)
			remainingWidth := width - descriptionStart - 2 // -2 for safety

			if remainingWidth > 10 {
				truncatedDesc := utils.TruncateToWidth(descriptionSingleLine, remainingWidth, "")
				descText := s.theme.Description(spacing + truncatedDesc)
				line := prefix + truncatedValue + descText
				return []string{line}
			}
		}

		// Not enough space for description
		maxWidth := width - len(prefix) - 2
		line := prefix + utils.TruncateToWidth(displayValue, maxWidth, "")
		return []string{line}
	}
}

// HandleInput handles keyboard input.
func (s *SelectList) HandleInput(data string) {
	kb := keybindings.GetEditorKeybindings()
	// Up arrow - wrap to bottom when at top
	if kb.Matches(data, keybindings.ActionSelectUp) {
		if s.selectedIndex == 0 {
			s.selectedIndex = len(s.filteredItems) - 1
		} else {
			s.selectedIndex--
		}
		s.notifySelectionChange()
	} else if kb.Matches(data, keybindings.ActionSelectDown) {
		// Down arrow - wrap to top when at bottom
		if s.selectedIndex == len(s.filteredItems)-1 {
			s.selectedIndex = 0
		} else {
			s.selectedIndex++
		}
		s.notifySelectionChange()
	} else if kb.Matches(data, keybindings.ActionSelectConfirm) {
		// Enter
		if s.selectedIndex < len(s.filteredItems) {
			selectedItem := s.filteredItems[s.selectedIndex]
			if s.OnSelect != nil {
				s.OnSelect(selectedItem)
			}
		}
	} else if kb.Matches(data, keybindings.ActionSelectCancel) {
		// Escape or Ctrl+C
		if s.OnCancel != nil {
			s.OnCancel()
		}
	}
}

func (s *SelectList) notifySelectionChange() {
	if s.selectedIndex < len(s.filteredItems) {
		selectedItem := s.filteredItems[s.selectedIndex]
		if s.OnSelectionChange != nil {
			s.OnSelectionChange(selectedItem)
		}
	}
}

// GetSelectedItem returns the currently selected item, or nil if none.
func (s *SelectList) GetSelectedItem() *SelectItem {
	if s.selectedIndex < 0 || s.selectedIndex >= len(s.filteredItems) {
		return nil
	}
	return &s.filteredItems[s.selectedIndex]
}

func normalizeToSingleLine(text string) string {
	// Replace multiline with single line
	result := strings.ReplaceAll(text, "\n", " ")
	result = strings.ReplaceAll(result, "\r", " ")
	return strings.TrimSpace(result)
}

// Ensure SelectList implements Component interface.
var _ tui.Component = (*SelectList)(nil)
