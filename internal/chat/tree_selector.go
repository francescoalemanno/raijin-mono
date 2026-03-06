package chat

import (
	"fmt"
	"strings"

	"github.com/francescoalemanno/raijin-mono/internal/persist"
	"github.com/francescoalemanno/raijin-mono/internal/theme"
	libagent "github.com/francescoalemanno/raijin-mono/libagent"
	"github.com/francescoalemanno/raijin-mono/libtui/pkg/components"
	"github.com/francescoalemanno/raijin-mono/libtui/pkg/fuzzy"
	"github.com/francescoalemanno/raijin-mono/libtui/pkg/keybindings"
	"github.com/francescoalemanno/raijin-mono/libtui/pkg/tui"
)

const treeSelectorMaxVisible = 12

// TreeSelectorComponent lets the user navigate to any node in the session tree.
type TreeSelectorComponent struct {
	searchInput   *components.Input
	listContainer *tui.Container
	hintText      *components.Text
	titleText     *components.Text
	borderTop     *borderLine
	borderBottom  *borderLine

	allEntries    []persist.TreeEntry
	filtered      []persist.TreeEntry
	selectedIndex int
	nav           listNavigator

	onSelect func(entry persist.TreeEntry)
	onCancel func()
}

func NewTreeSelector(
	entries []persist.TreeEntry,
	onSelect func(entry persist.TreeEntry),
	onCancel func(),
) *TreeSelectorComponent {
	m := &TreeSelectorComponent{
		searchInput:   components.NewInput(),
		listContainer: &tui.Container{},
		hintText:      components.NewText(theme.Default.Muted.Ansi24("Type to filter · Enter to navigate · Esc to cancel"), 0, 0, nil),
		titleText:     components.NewText(theme.Default.Accent.Ansi24("TREE"), 0, 0, nil),
		borderTop:     &borderLine{},
		borderBottom:  &borderLine{},
		allEntries:    append([]persist.TreeEntry(nil), entries...),
		onSelect:      onSelect,
		onCancel:      onCancel,
	}

	m.hintText.SetFgColorFn(theme.Default.Foreground.Ansi24)
	m.searchInput.SetPaddingColorFn(theme.Default.Foreground.Ansi24)

	m.filtered = append([]persist.TreeEntry(nil), entries...)
	m.nav = listNavigator{
		count:    func() int { return len(m.filtered) },
		selected: &m.selectedIndex,
		update:   m.updateList,
	}

	m.searchInput.SetOnSubmit(func(_ string) { m.confirmSelection() })
	m.searchInput.SetOnEscape(func() {
		if m.onCancel != nil {
			m.onCancel()
		}
	})

	m.updateList()
	return m
}

func (m *TreeSelectorComponent) filter(query string) {
	query = strings.TrimSpace(query)
	if query == "" {
		m.filtered = append([]persist.TreeEntry(nil), m.allEntries...)
	} else {
		m.filtered = fuzzy.FuzzyFilter(m.allEntries, query, treeEntrySearchText)
	}
	if m.selectedIndex >= len(m.filtered) {
		m.selectedIndex = max(0, len(m.filtered)-1)
	}
	m.updateList()
}

func (m *TreeSelectorComponent) updateList() {
	m.listContainer.Clear()
	if len(m.filtered) == 0 {
		m.listContainer.AddChild(components.NewText(theme.Default.Muted.Ansi24("  No matching entries"), 0, 0, nil))
		return
	}

	startIndex, endIndex := visibleRange(m.selectedIndex, len(m.filtered), treeSelectorMaxVisible)
	for i := startIndex; i < endIndex; i++ {
		entry := m.filtered[i]
		line := m.renderLine(entry, i == m.selectedIndex)
		m.listContainer.AddChild(components.NewText(line, 0, 0, nil))
	}
	appendScrollInfo(m.listContainer, m.selectedIndex, len(m.filtered), startIndex, endIndex)
}

func (m *TreeSelectorComponent) renderLine(entry persist.TreeEntry, selected bool) string {
	// 1. Cursor glyph (Pi: "› " accent when selected, "  " otherwise).
	cursor := "  "
	if selected {
		cursor = theme.Default.Accent.Ansi24("› ")
	}

	// 2. Build prefix char-by-char (Pi algorithm): each depth level = 3 chars.
	//    Positions are filled with gutters (│), connectors (├─ / └─), or spaces.
	totalChars := entry.Depth * 3
	prefixRunes := make([]rune, totalChars)
	for i := range prefixRunes {
		prefixRunes[i] = ' '
	}

	connectorPos := -1
	if entry.ShowConnector && entry.Depth > 0 {
		connectorPos = entry.Depth - 1
	}

	for i := 0; i < totalChars; i++ {
		level := i / 3
		pos := i % 3

		// Check gutter at this level.
		gutterShow := false
		hasGutter := false
		for _, g := range entry.Gutters {
			if g.Position == level {
				hasGutter = true
				gutterShow = g.Show
				break
			}
		}

		if hasGutter {
			if pos == 0 && gutterShow {
				prefixRunes[i] = '│'
			}
		} else if connectorPos >= 0 && level == connectorPos {
			switch pos {
			case 0:
				if entry.IsLastSibling {
					prefixRunes[i] = '└'
				} else {
					prefixRunes[i] = '├'
				}
			case 1:
				prefixRunes[i] = '─'
			}
		}
	}
	prefix := theme.Default.Muted.Ansi24(string(prefixRunes))

	// 3. Active-path bullet (Pi: "• " in accent, empty otherwise).
	bullet := ""
	if entry.IsOnActivePath {
		bullet = theme.Default.Accent.Ansi24("• ")
	} else {
		bullet = "  "
	}

	// 4. Content text.
	content := treeEntryLabel(entry, selected)

	line := cursor + prefix + bullet + content
	return line
}

func (m *TreeSelectorComponent) confirmSelection() {
	if m.selectedIndex < 0 || m.selectedIndex >= len(m.filtered) {
		return
	}
	if m.onSelect != nil {
		m.onSelect(m.filtered[m.selectedIndex])
	}
}

func (m *TreeSelectorComponent) Render(width int) []string {
	return renderSelectorFrame(width, m.borderTop, m.borderBottom, m.titleText, m.listContainer, m.hintText, m.searchInput)
}

func (m *TreeSelectorComponent) HandleInput(data string) {
	kb := keybindings.GetEditorKeybindings()

	if m.nav.handleNav(data) {
		return
	}
	if kb.Matches(data, keybindings.ActionSelectConfirm) {
		m.confirmSelection()
		return
	}
	if kb.Matches(data, keybindings.ActionSelectCancel) {
		if m.onCancel != nil {
			m.onCancel()
		}
		return
	}

	m.searchInput.HandleInput(data)
	m.filter(m.searchInput.GetValue())
}

func (m *TreeSelectorComponent) Invalidate() {
	m.listContainer.Invalidate()
	m.searchInput.Invalidate()
}

func (m *TreeSelectorComponent) SetFocused(focused bool) {
	m.searchInput.SetFocused(focused)
}

func (m *TreeSelectorComponent) IsFocused() bool {
	return m.searchInput.GetFocused()
}

// treeEntryLabel returns a Pi-style coloured label for a tree entry.
// selected controls whether the content text is bold (Pi behaviour).
func treeEntryLabel(e persist.TreeEntry, selected bool) string {
	bold := func(s string) string {
		if selected {
			return theme.Default.AccentAlt.AnsiBold(s)
		}
		return s
	}

	if e.Msg == nil {
		return theme.Default.Muted.Ansi24(fmt.Sprintf("[node %s]", e.ID))
	}
	switch m := e.Msg.(type) {
	case *libagent.UserMessage:
		role := theme.Default.Accent.Ansi24("user: ")
		return role + bold(buildTreePreview(m.Content, 80))
	case *libagent.AssistantMessage:
		role := theme.Default.Success.Ansi24("assistant: ")
		if m.Text != "" {
			return role + bold(buildTreePreview(m.Text, 80))
		}
		return role + theme.Default.Muted.Ansi24("(no content)")
	case *libagent.ToolResultMessage:
		label := fmt.Sprintf("[%s]: %s", m.ToolName, buildTreePreview(m.Content, 60))
		return theme.Default.Muted.Ansi24(label)
	}
	return theme.Default.Muted.Ansi24(fmt.Sprintf("[%s]", e.Msg.GetRole()))
}

// treeEntrySearchText returns the searchable text for fuzzy filtering.
func treeEntrySearchText(e persist.TreeEntry) string {
	if e.Msg == nil {
		return e.ID
	}
	switch m := e.Msg.(type) {
	case *libagent.UserMessage:
		return "user " + m.Content
	case *libagent.AssistantMessage:
		return "assistant " + m.Text
	case *libagent.ToolResultMessage:
		return m.ToolName + " " + m.Content
	}
	return e.Msg.GetRole()
}

func buildTreePreview(text string, maxRunes int) string {
	if maxRunes <= 1 {
		maxRunes = 1
	}
	normalized := strings.Join(strings.Fields(text), " ")
	runes := []rune(normalized)
	if len(runes) <= maxRunes {
		return normalized
	}
	return string(runes[:maxRunes-1]) + "…"
}

var (
	_ tui.Component = (*TreeSelectorComponent)(nil)
	_ tui.Focusable = (*TreeSelectorComponent)(nil)
)
