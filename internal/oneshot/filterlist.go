package oneshot

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/francescoalemanno/raijin-mono/internal/fzf"
)

// filterList is a reusable filterable list component for Bubbletea selectors.
// It handles keyboard navigation, fzf-style fuzzy filtering, and paginated rendering.
type filterList[T any] struct {
	all      []T
	filtered []T
	cursor   int
	filter   string
	chosen   *T
	deleted  *T // item removed via ctrl+x (two-press confirm)
	quitting bool

	title         string
	maxVisible    int                 // <=0 means auto-fit to full viewport height
	textFn        func(item T) string // returns the searchable text for fuzzy matching
	renderFn      func(item T, selected bool) string
	hintFn        func(item T) string // optional extra hint after the line (e.g. "●" for leaf)
	deletableFn   func(item T) bool   // if set, ctrl+x triggers delete flow
	pendingDelete bool                // first ctrl+x pressed — waiting for confirmation
	hintOverride  string              // temporary hint shown during delete confirmation
	height        int                 // latest viewport height from WindowSizeMsg
}

func newFilterList[T any](
	title string,
	items []T,
	initialCursor int,
	maxVisible int,
	textFn func(item T) string,
	renderFn func(item T, selected bool) string,
) filterList[T] {
	return filterList[T]{
		all:        items,
		filtered:   append([]T(nil), items...),
		cursor:     initialCursor,
		title:      title,
		maxVisible: maxVisible,
		textFn:     textFn,
		renderFn:   renderFn,
	}
}

func (m filterList[T]) Init() tea.Cmd { return nil }

func (m filterList[T]) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		if msg.Height > 0 {
			m.height = msg.Height
		}
	case tea.KeyMsg:
		key := msg.String()

		// Any key other than ctrl+x resets pending delete.
		if key != "ctrl+x" && m.pendingDelete {
			m.pendingDelete = false
			m.hintOverride = ""
		}

		switch key {
		case "ctrl+c", "esc":
			m.quitting = true
			return m, tea.Quit
		case "up":
			if m.cursor > 0 {
				m.cursor--
			}
		case "down":
			if m.cursor < len(m.filtered)-1 {
				m.cursor++
			}
		case "enter":
			if m.cursor >= 0 && m.cursor < len(m.filtered) {
				item := m.filtered[m.cursor]
				m.chosen = &item
			}
			return m, tea.Quit
		case "ctrl+x":
			if m.deletableFn != nil && m.cursor >= 0 && m.cursor < len(m.filtered) {
				if m.deletableFn(m.filtered[m.cursor]) {
					if m.pendingDelete {
						// Second press — confirm deletion.
						item := m.filtered[m.cursor]
						m.deleted = &item
						m.pendingDelete = false
						m.hintOverride = ""
						return m, tea.Quit
					}
					// First press — ask for confirmation.
					m.pendingDelete = true
					m.hintOverride = "press ctrl+x again to confirm deletion"
				}
			}
		case "backspace":
			if len(m.filter) > 0 {
				m.filter = m.filter[:len(m.filter)-1]
				m.applyFilter()
			}
		default:
			r := msg.String()
			if len(r) == 1 && r[0] >= ' ' {
				m.filter += r
				m.applyFilter()
			}
		}
	}
	return m, nil
}

func (m *filterList[T]) applyFilter() {
	if m.filter == "" {
		m.filtered = append(m.filtered[:0], m.all...)
	} else {
		m.filtered = fzf.Rank(m.all, m.filter, m.textFn)
	}
	if m.cursor >= len(m.filtered) {
		m.cursor = max(0, len(m.filtered)-1)
	}
}

var (
	flTitleStyle  = oneshotAccentStyle.Bold(true)
	flFilterStyle = oneshotWarningStyle
	flDimStyle    = oneshotMutedStyle
	flEmptyStyle  = oneshotMutedStyle
	flPromptStyle = oneshotAccentStyle.Bold(true)
)

func (m filterList[T]) View() string {
	var b strings.Builder

	b.WriteString(flTitleStyle.Render(m.title))
	b.WriteString("\n")

	// Filter input line — always visible.
	if m.filter != "" {
		b.WriteString(flPromptStyle.Render("> ") + flFilterStyle.Render(m.filter))
	} else {
		b.WriteString(flPromptStyle.Render("> ") + flDimStyle.Render("type to filter…"))
	}
	b.WriteString("\n")

	if len(m.filtered) == 0 {
		b.WriteString(flEmptyStyle.Render("  No matches") + "\n")
	} else {
		vis := min(m.visibleRows(), len(m.filtered))
		start := 0
		if m.cursor >= vis {
			start = m.cursor - vis + 1
		}
		end := min(start+vis, len(m.filtered))

		for i := start; i < end; i++ {
			selected := i == m.cursor
			line := m.renderFn(m.filtered[i], selected)
			b.WriteString(line)
			if m.hintFn != nil {
				if hint := m.hintFn(m.filtered[i]); hint != "" {
					b.WriteString(hint)
				}
			}
			b.WriteString("\n")
		}

		if len(m.filtered) > vis {
			b.WriteString(flDimStyle.Render(fmt.Sprintf("  %d/%d shown", vis, len(m.filtered))))
			b.WriteString("\n")
		}
	}

	if m.hintOverride != "" {
		b.WriteString(oneshotDangerStyle.Bold(true).Render(m.hintOverride))
	} else if m.deletableFn != nil {
		b.WriteString(flDimStyle.Render("↑/↓ navigate · enter select · ctrl+x delete · type to filter · esc cancel"))
	} else {
		b.WriteString(flDimStyle.Render("↑/↓ navigate · enter select · type to filter · esc cancel"))
	}
	return b.String()
}

func (m filterList[T]) visibleRows() int {
	// Explicit fixed cap wins.
	if m.maxVisible > 0 {
		return max(m.maxVisible, 1)
	}

	// Full-page mode: title + filter + footer always take 3 rows.
	available := m.height - 3
	if available < 1 {
		available = len(m.filtered)
	}
	if available < 1 {
		return 1
	}

	// Keep one row for "... shown" when the list is larger than the window.
	if len(m.filtered) > available && available > 1 {
		return available - 1
	}
	return available
}
