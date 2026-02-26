package chat

import (
	"fmt"
	"strings"

	"github.com/francescoalemanno/raijin-mono/libtui/pkg/components"
	"github.com/francescoalemanno/raijin-mono/libtui/pkg/fuzzy"
	"github.com/francescoalemanno/raijin-mono/libtui/pkg/keybindings"
	"github.com/francescoalemanno/raijin-mono/libtui/pkg/tui"

	"github.com/francescoalemanno/raijin-mono/internal/theme"
)

const forkSelectorMaxVisible = 10

type forkCandidate struct {
	MessageID string
	Prompt    string
	Preview   string
	Ordinal   int // 1-based user-message index in chronological order
}

type ForkSelectorComponent struct {
	searchInput   *components.Input
	listContainer *tui.Container
	hintText      *components.Text
	titleText     *components.Text
	borderTop     *borderLine
	borderBottom  *borderLine

	allCandidates []forkCandidate
	filtered      []forkCandidate
	selectedIndex int
	nav           listNavigator

	onSelect func(candidate forkCandidate)
	onCancel func()
}

func NewForkSelector(candidates []forkCandidate, onSelect func(candidate forkCandidate), onCancel func()) *ForkSelectorComponent {
	m := &ForkSelectorComponent{
		searchInput:   components.NewInput(),
		listContainer: &tui.Container{},
		hintText:      components.NewText(theme.Default.Muted.Ansi24("Type to filter · Enter to fork · Esc to cancel"), 0, 0, nil),
		titleText:     components.NewText(theme.Default.Accent.Ansi24("FORK FROM USER MESSAGE"), 0, 0, nil),
		borderTop:     &borderLine{},
		borderBottom:  &borderLine{},
		allCandidates: append([]forkCandidate(nil), candidates...),
		onSelect:      onSelect,
		onCancel:      onCancel,
	}

	// Set foreground color for padding/margins
	m.hintText.SetFgColorFn(theme.Default.Foreground.Ansi24)
	m.searchInput.SetPaddingColorFn(theme.Default.Foreground.Ansi24)

	m.filtered = append([]forkCandidate(nil), candidates...)
	m.nav = listNavigator{
		count:    func() int { return len(m.filtered) },
		selected: &m.selectedIndex,
		update:   m.updateList,
	}

	m.searchInput.SetOnSubmit(func(_ string) {
		m.confirmSelection()
	})
	m.searchInput.SetOnEscape(func() {
		if m.onCancel != nil {
			m.onCancel()
		}
	})

	m.updateList()
	return m
}

func (m *ForkSelectorComponent) filter(query string) {
	if strings.TrimSpace(query) == "" {
		m.filtered = append([]forkCandidate(nil), m.allCandidates...)
	} else {
		m.filtered = fuzzy.FuzzyFilter(m.allCandidates, query, func(item forkCandidate) string {
			return item.Preview + " " + item.Prompt
		})
	}
	if m.selectedIndex >= len(m.filtered) {
		m.selectedIndex = max(0, len(m.filtered)-1)
	}
	m.updateList()
}

func (m *ForkSelectorComponent) updateList() {
	m.listContainer.Clear()
	if len(m.filtered) == 0 {
		m.listContainer.AddChild(components.NewText(theme.Default.Muted.Ansi24("  No matching user messages"), 0, 0, nil))
		return
	}

	startIndex := max(0,
		min(m.selectedIndex-forkSelectorMaxVisible/2, len(m.filtered)-forkSelectorMaxVisible))
	endIndex := min(startIndex+forkSelectorMaxVisible, len(m.filtered))

	for i := startIndex; i < endIndex; i++ {
		item := m.filtered[i]
		line := m.renderLine(item, i == m.selectedIndex)
		m.listContainer.AddChild(components.NewText(line, 0, 0, nil))
	}

	if startIndex > 0 || endIndex < len(m.filtered) {
		scrollInfo := theme.Default.Muted.Ansi24(fmt.Sprintf("  (%d/%d)", m.selectedIndex+1, len(m.filtered)))
		m.listContainer.AddChild(components.NewText(scrollInfo, 0, 0, nil))
	}
}

func (m *ForkSelectorComponent) renderLine(item forkCandidate, selected bool) string {
	label := fmt.Sprintf("#%d %s", item.Ordinal, item.Preview)
	if selected {
		return theme.Default.Accent.Ansi24("→ ") + theme.Default.Accent.Ansi24(label)
	}
	return theme.Default.Foreground.Ansi24("  ") + theme.Default.Foreground.Ansi24(label)
}

func (m *ForkSelectorComponent) confirmSelection() {
	if m.selectedIndex < 0 || m.selectedIndex >= len(m.filtered) {
		return
	}
	if m.onSelect != nil {
		m.onSelect(m.filtered[m.selectedIndex])
	}
}

func (m *ForkSelectorComponent) Render(width int) []string {
	var lines []string
	lines = append(lines, m.borderTop.Render(width)...)
	lines = append(lines, "")
	lines = append(lines, m.titleText.Render(width)...)
	lines = append(lines, "")
	lines = append(lines, m.listContainer.Render(width)...)
	lines = append(lines, "")
	lines = append(lines, m.hintText.Render(width)...)
	lines = append(lines, m.borderBottom.Render(width)...)
	lines = append(lines, m.searchInput.Render(width)...)
	lines = append(lines, m.borderBottom.Render(width)...)
	return lines
}

func (m *ForkSelectorComponent) HandleInput(data string) {
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

func (m *ForkSelectorComponent) Invalidate() {
	m.listContainer.Invalidate()
	m.searchInput.Invalidate()
}

func (m *ForkSelectorComponent) SetFocused(focused bool) {
	m.searchInput.SetFocused(focused)
}

func (m *ForkSelectorComponent) IsFocused() bool {
	return m.searchInput.GetFocused()
}

var (
	_ tui.Component = (*ForkSelectorComponent)(nil)
	_ tui.Focusable = (*ForkSelectorComponent)(nil)
)
