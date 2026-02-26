package chat

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/francescoalemanno/raijin-mono/internal/persist"
	"github.com/francescoalemanno/raijin-mono/internal/theme"
	"github.com/francescoalemanno/raijin-mono/libtui/pkg/components"
	"github.com/francescoalemanno/raijin-mono/libtui/pkg/fuzzy"
	"github.com/francescoalemanno/raijin-mono/libtui/pkg/keybindings"
	"github.com/francescoalemanno/raijin-mono/libtui/pkg/keys"
	"github.com/francescoalemanno/raijin-mono/libtui/pkg/tui"
)

const sessionSelectorMaxVisible = 10

type sessionCandidate struct {
	ID                  string
	ShortID             string
	Title               string
	ParentSessionID     string
	ForkedFromMessageID string
	Depth               int
	IsActiveLineage     bool
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

	// Set foreground color for padding/margins
	m.hintText.SetFgColorFn(theme.Default.Foreground.Ansi24)
	m.searchInput.SetPaddingColorFn(theme.Default.Foreground.Ansi24)

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
	query = strings.TrimSpace(query)
	if query == "" {
		m.filtered = append([]sessionCandidate(nil), m.allCandidates...)
	} else {
		ranked := fuzzy.FuzzyFilter(m.allCandidates, query, func(item sessionCandidate) string {
			return item.ShortID + " " + item.Title
		})

		byID := make(map[string]sessionCandidate, len(m.allCandidates))
		for _, c := range m.allCandidates {
			byID[c.ID] = c
		}

		m.filtered = m.filtered[:0]
		added := make(map[string]struct{}, len(ranked)*2)
		addWithAncestors := func(id string) {
			chain := make([]string, 0, 8)
			seen := make(map[string]struct{}, 8)
			for id != "" {
				if _, ok := seen[id]; ok {
					break
				}
				seen[id] = struct{}{}
				node, ok := byID[id]
				if !ok {
					break
				}
				chain = append(chain, id)
				id = node.ParentSessionID
			}
			for i := len(chain) - 1; i >= 0; i-- {
				nodeID := chain[i]
				if _, ok := added[nodeID]; ok {
					continue
				}
				node, ok := byID[nodeID]
				if !ok {
					continue
				}
				added[nodeID] = struct{}{}
				m.filtered = append(m.filtered, node)
			}
		}

		for _, c := range ranked {
			addWithAncestors(c.ID)
		}
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
	indent := strings.Repeat("  ", max(0, item.Depth))
	label := fmt.Sprintf("%s[%s] %s", indent, item.ShortID, title)
	awaitingDelete := m.pendingDelete == item.ID
	if selected {
		if awaitingDelete {
			return theme.Default.Danger.Ansi24("->") + theme.Default.Danger.Ansi24(label)
		}
		if item.IsActiveLineage {
			return theme.Default.Success.Ansi24("->") + theme.Default.Success.Ansi24(label)
		}
		return theme.Default.Accent.Ansi24("->") + theme.Default.Accent.Ansi24(label)
	}
	if awaitingDelete {
		return theme.Default.Danger.Ansi24("  " + label)
	}
	if item.IsActiveLineage {
		return theme.Default.Success.Ansi24("  ") + theme.Default.Success.Ansi24(label)
	}
	return theme.Default.Foreground.Ansi24("  ") + label
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

	if target.IsActiveLineage {
		m.pendingDelete = ""
		m.hintText.SetText(theme.Default.Danger.Ansi24("session can be deleted only if inactive"))
		return
	}

	if m.pendingDelete != target.ID {
		// First press: arm the confirmation.
		m.pendingDelete = target.ID
		m.updateList()
		return
	}

	// Second press: confirmed — remove target subtree and notify.
	m.pendingDelete = ""
	m.allCandidates = removeCandidateTreeByID(m.allCandidates, target.ID)
	m.filtered = removeCandidateTreeByID(m.filtered, target.ID)
	if m.selectedIndex >= len(m.filtered) {
		m.selectedIndex = max(0, len(m.filtered)-1)
	}
	m.updateList()

	if m.onDelete != nil {
		m.onDelete(target)
	}
}

func removeCandidateTreeByID(slice []sessionCandidate, rootID string) []sessionCandidate {
	remove := map[string]struct{}{rootID: {}}
	changed := true
	for changed {
		changed = false
		for _, c := range slice {
			if c.ParentSessionID == "" {
				continue
			}
			if _, ok := remove[c.ParentSessionID]; !ok {
				continue
			}
			if _, exists := remove[c.ID]; exists {
				continue
			}
			remove[c.ID] = struct{}{}
			changed = true
		}
	}

	out := slice[:0:len(slice)]
	for _, c := range slice {
		if _, drop := remove[c.ID]; drop {
			continue
		}
		out = append(out, c)
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

	ordered := orderSessionSummariesTree(summaries)
	depthByID := sessionDepthByID(ordered)
	currentID := app.session.ID()
	activeLineage := activeLineageSet(currentID, ordered)
	ordered = prioritizeActiveLineage(ordered, activeLineage)

	candidates := make([]sessionCandidate, len(ordered))
	for i, s := range ordered {
		_, isActiveLineage := activeLineage[s.ID]
		candidates[i] = sessionCandidate{
			ID:                  s.ID,
			ShortID:             s.ShortID,
			Title:               s.Title,
			ParentSessionID:     s.ParentSessionID,
			ForkedFromMessageID: s.ForkedFromMessageID,
			Depth:               depthByID[s.ID],
			IsActiveLineage:     isActiveLineage,
		}
	}
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

func activeLineageSet(currentID string, summaries []persist.SessionSummary) map[string]struct{} {
	lineage := make(map[string]struct{}, 8)
	if currentID == "" {
		return lineage
	}

	byID := make(map[string]persist.SessionSummary, len(summaries))
	for _, s := range summaries {
		byID[s.ID] = s
	}

	seen := make(map[string]struct{}, 8)
	id := currentID
	for id != "" {
		if _, ok := seen[id]; ok {
			break
		}
		seen[id] = struct{}{}
		lineage[id] = struct{}{}
		s, ok := byID[id]
		if !ok {
			break
		}
		id = s.ParentSessionID
	}
	return lineage
}

func orderSessionSummariesTree(summaries []persist.SessionSummary) []persist.SessionSummary {
	if len(summaries) == 0 {
		return nil
	}

	byParent := make(map[string][]persist.SessionSummary)
	for _, s := range summaries {
		parent := s.ParentSessionID
		byParent[parent] = append(byParent[parent], s)
	}

	sortGroup := func(items []persist.SessionSummary) {
		sort.SliceStable(items, func(i, j int) bool {
			if items[i].UpdatedAt == items[j].UpdatedAt {
				return items[i].ID < items[j].ID
			}
			return items[i].UpdatedAt > items[j].UpdatedAt
		})
	}
	for parent := range byParent {
		sortGroup(byParent[parent])
	}

	roots := append([]persist.SessionSummary(nil), byParent[""]...)
	sortGroup(roots)

	ordered := make([]persist.SessionSummary, 0, len(summaries))
	seen := make(map[string]struct{}, len(summaries))
	var walk func(items []persist.SessionSummary)
	walk = func(items []persist.SessionSummary) {
		for _, s := range items {
			if _, ok := seen[s.ID]; ok {
				continue
			}
			seen[s.ID] = struct{}{}
			ordered = append(ordered, s)
			walk(byParent[s.ID])
		}
	}
	walk(roots)

	// Orphans or cycles: append remaining nodes by recency.
	remaining := make([]persist.SessionSummary, 0)
	for _, s := range summaries {
		if _, ok := seen[s.ID]; !ok {
			remaining = append(remaining, s)
		}
	}
	sortGroup(remaining)
	walk(remaining)

	return ordered
}

func sessionDepthByID(summaries []persist.SessionSummary) map[string]int {
	depth := make(map[string]int, len(summaries))
	byID := make(map[string]persist.SessionSummary, len(summaries))
	for _, s := range summaries {
		byID[s.ID] = s
	}

	var resolve func(id string) int
	resolve = func(id string) int {
		if d, ok := depth[id]; ok {
			return d
		}
		s, ok := byID[id]
		if !ok || s.ParentSessionID == "" {
			depth[id] = 0
			return 0
		}
		d := resolve(s.ParentSessionID) + 1
		depth[id] = d
		return d
	}

	for _, s := range summaries {
		resolve(s.ID)
	}
	return depth
}

func prioritizeActiveLineage(summaries []persist.SessionSummary, active map[string]struct{}) []persist.SessionSummary {
	if len(summaries) == 0 || len(active) == 0 {
		return summaries
	}

	byID := make(map[string]persist.SessionSummary, len(summaries))
	for _, s := range summaries {
		byID[s.ID] = s
	}

	isRelated := func(id string) bool {
		seen := make(map[string]struct{}, 8)
		for id != "" {
			if _, ok := active[id]; ok {
				return true
			}
			if _, ok := seen[id]; ok {
				break
			}
			seen[id] = struct{}{}
			n, ok := byID[id]
			if !ok {
				break
			}
			id = n.ParentSessionID
		}
		return false
	}

	out := make([]persist.SessionSummary, 0, len(summaries))
	for _, s := range summaries {
		if isRelated(s.ID) {
			out = append(out, s)
		}
	}
	for _, s := range summaries {
		if isRelated(s.ID) {
			continue
		}
		out = append(out, s)
	}
	return out
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
