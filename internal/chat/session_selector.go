package chat

import (
	"context"
	"fmt"
	"strings"

	"github.com/francescoalemanno/raijin-mono/internal/theme"
	"github.com/francescoalemanno/raijin-mono/libtui/pkg/components"
	"github.com/francescoalemanno/raijin-mono/libtui/pkg/fuzzy"
	"github.com/francescoalemanno/raijin-mono/libtui/pkg/keybindings"
	"github.com/francescoalemanno/raijin-mono/libtui/pkg/keys"
	"github.com/francescoalemanno/raijin-mono/libtui/pkg/tui"
)

const sessionSelectorMaxVisible = 10

type sessionCandidate struct {
	ID      string
	ShortID string
	Title   string
}

// SessionSelectorComponent lets the user pick a previous session.
type SessionSelectorComponent struct {
	searchInput   *components.Input
	listContainer *tui.Container
	hintText      *components.Text
	titleText     *components.Text
	borderTop     *borderLine
	borderBottom  *borderLine

	allCandidates []sessionCandidate
	filtered      []sessionCandidate
	selectedIndex int
	nav           listNavigator
	pendingDelete string // ID of session awaiting delete confirmation

	onSelect func(candidate sessionCandidate)
	onDelete func(candidate sessionCandidate)
	onCancel func()
}

func NewSessionSelector(
	candidates []sessionCandidate,
	onSelect func(candidate sessionCandidate),
	onDelete func(candidate sessionCandidate),
	onCancel func(),
) *SessionSelectorComponent {
	m := &SessionSelectorComponent{
		searchInput:   components.NewInput(),
		listContainer: &tui.Container{},
		hintText:      components.NewText("", 0, 0, nil),
		titleText:     components.NewText(theme.Default.Accent.Ansi24("SESSIONS"), 0, 0, nil),
		borderTop:     &borderLine{},
		borderBottom:  &borderLine{},
		allCandidates: append([]sessionCandidate(nil), candidates...),
		onSelect:      onSelect,
		onDelete:      onDelete,
		onCancel:      onCancel,
	}

	m.filtered = append([]sessionCandidate(nil), candidates...)
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

func (m *SessionSelectorComponent) filter(query string) {
	m.pendingDelete = "" // reset confirmation on any filter change
	if strings.TrimSpace(query) == "" {
		m.filtered = append([]sessionCandidate(nil), m.allCandidates...)
	} else {
		m.filtered = fuzzy.FuzzyFilter(m.allCandidates, query, func(item sessionCandidate) string {
			return item.ShortID + " " + item.Title
		})
	}
	if m.selectedIndex >= len(m.filtered) {
		m.selectedIndex = max(0, len(m.filtered)-1)
	}
	m.updateList()
}

func (m *SessionSelectorComponent) updateList() {
	m.listContainer.Clear()

	// Update hint text based on pending-delete state.
	if m.pendingDelete != "" {
		m.hintText.SetText(theme.Default.Danger.Ansi24("Press ctrl+x again to confirm deletion · Esc to cancel"))
	} else {
		m.hintText.SetText(theme.Default.Muted.Ansi24("Type to filter · Enter to switch · ctrl+x delete · Esc to cancel"))
	}

	if len(m.filtered) == 0 {
		m.listContainer.AddChild(components.NewText(theme.Default.Muted.Ansi24("  No matching sessions"), 0, 0, nil))
		return
	}

	startIndex := max(0, min(m.selectedIndex-sessionSelectorMaxVisible/2, len(m.filtered)-sessionSelectorMaxVisible))
	endIndex := min(startIndex+sessionSelectorMaxVisible, len(m.filtered))

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

func (m *SessionSelectorComponent) renderLine(item sessionCandidate, selected bool) string {
	title := item.Title
	if title == "" {
		title = "(untitled)"
	}
	label := fmt.Sprintf("#%s %s", item.ShortID, title)
	awaitingDelete := m.pendingDelete == item.ID
	if selected {
		if awaitingDelete {
			return theme.Default.Danger.Ansi24("→ ") + theme.Default.Danger.Ansi24(label)
		}
		return theme.Default.Accent.Ansi24("→ ") + theme.Default.Accent.Ansi24(label)
	}
	if awaitingDelete {
		return theme.Default.Danger.Ansi24("  " + label)
	}
	return "  " + label
}

func (m *SessionSelectorComponent) confirmSelection() {
	if m.selectedIndex < 0 || m.selectedIndex >= len(m.filtered) {
		return
	}
	if m.onSelect != nil {
		m.onSelect(m.filtered[m.selectedIndex])
	}
}

func (m *SessionSelectorComponent) Render(width int) []string {
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

func (m *SessionSelectorComponent) HandleInput(data string) {
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

	m.searchInput.HandleInput(data)
	m.filter(m.searchInput.GetValue())
}

func (m *SessionSelectorComponent) handleDeleteKey() {
	if m.selectedIndex < 0 || m.selectedIndex >= len(m.filtered) {
		return
	}
	target := m.filtered[m.selectedIndex]

	if m.pendingDelete != target.ID {
		// First press: arm the confirmation.
		m.pendingDelete = target.ID
		m.updateList()
		return
	}

	// Second press: confirmed — remove from both lists and notify.
	m.pendingDelete = ""
	m.allCandidates = removeCandidateByID(m.allCandidates, target.ID)
	m.filtered = removeCandidateByID(m.filtered, target.ID)
	if m.selectedIndex >= len(m.filtered) {
		m.selectedIndex = max(0, len(m.filtered)-1)
	}
	m.updateList()

	if m.onDelete != nil {
		m.onDelete(target)
	}
}

func removeCandidateByID(slice []sessionCandidate, id string) []sessionCandidate {
	out := slice[:0:len(slice)]
	for _, c := range slice {
		if c.ID != id {
			out = append(out, c)
		}
	}
	return out
}

func (m *SessionSelectorComponent) Invalidate() {
	m.listContainer.Invalidate()
	m.searchInput.Invalidate()
}

func (m *SessionSelectorComponent) SetFocused(focused bool) {
	m.searchInput.SetFocused(focused)
}

func (m *SessionSelectorComponent) IsFocused() bool {
	return m.searchInput.GetFocused()
}

var (
	_ tui.Component = (*SessionSelectorComponent)(nil)
	_ tui.Focusable = (*SessionSelectorComponent)(nil)
)

// ---------------------------------------------------------------------------
// showSessionSelector wired into ChatApp
// ---------------------------------------------------------------------------

func (app *ChatApp) showSessionSelector() {
	if app.state == stateRunning {
		app.appendMessage("cannot switch sessions while a response is running; interrupt first", theme.BorderThin, theme.Default.Muted.Ansi24, theme.Default.Foreground.Ansi24, false)
		app.ui.RequestRender()
		return
	}

	if app.session == nil {
		return
	}

	store := app.session.PersistStore()
	if store == nil {
		app.appendMessage("session persistence is not available", theme.BorderThin, theme.Default.Muted.Ansi24, theme.Default.Foreground.Ansi24, false)
		app.ui.RequestRender()
		return
	}

	summaries := store.ListSessionSummaries()
	if len(summaries) == 0 {
		app.appendMessage("no previous sessions found", theme.BorderThin, theme.Default.Muted.Ansi24, theme.Default.Foreground.Ansi24, false)
		app.ui.RequestRender()
		return
	}

	candidates := make([]sessionCandidate, len(summaries))
	for i, s := range summaries {
		candidates[i] = sessionCandidate{
			ID:      s.ID,
			ShortID: s.ShortID,
			Title:   s.Title,
		}
	}

	currentID := app.session.ID()
	app.showSelector(func(done func()) tui.Component {
		return NewSessionSelector(candidates,
			func(candidate sessionCandidate) {
				done()
				go func() {
					if err := app.applySessionSwitch(context.Background(), candidate.ID); err != nil {
						app.dispatchSync(func(_ tui.UIToken) {
							app.appendMessage("/sessions: "+err.Error(), theme.BorderThin, theme.Default.Danger.Ansi24, theme.Default.Foreground.Ansi24, false)
						})
						app.ui.RequestRender()
					}
				}()
			},
			func(candidate sessionCandidate) {
				go func() {
					if err := store.RemoveSession(candidate.ID); err != nil {
						app.dispatchSync(func(_ tui.UIToken) {
							app.appendMessage("/sessions delete: "+err.Error(), theme.BorderThin, theme.Default.Danger.Ansi24, theme.Default.Foreground.Ansi24, false)
						})
						app.ui.RequestRender()
						return
					}
					// If the deleted session was active, start a new one.
					if candidate.ID == currentID {
						app.dispatchSync(func(_ tui.UIToken) {
							done()
						})
						if err := app.reloadFromScratch(""); err != nil {
							app.dispatchSync(func(_ tui.UIToken) {
								app.appendMessage("failed to reset session: "+err.Error(), theme.BorderThin, theme.Default.Danger.Ansi24, theme.Default.Foreground.Ansi24, false)
							})
							app.ui.RequestRender()
						}
					}
				}()
			},
			func() {
				done()
			},
		)
	})
}

func (app *ChatApp) applySessionSwitch(ctx context.Context, sessionID string) error {
	if err := app.session.SwitchTo(ctx, sessionID); err != nil {
		return err
	}
	app.dispatchSync(func(_ tui.UIToken) {
		app.resetConversationView(false)
		app.restoreHistoryFromSession(ctx)
		if len(app.items) == 0 {
			welcome := app.newWelcomeComponent()
			app.history.AddChild(welcome)
			app.items = append(app.items, historyEntry{component: welcome})
		}
		app.refreshHeader()
		app.refreshStatus()
	})
	app.ui.RequestRender(true)
	return nil
}
