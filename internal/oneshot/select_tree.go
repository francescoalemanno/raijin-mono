package oneshot

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/francescoalemanno/raijin-mono/internal/persist"
	"github.com/francescoalemanno/raijin-mono/internal/session"
	libagent "github.com/francescoalemanno/raijin-mono/libagent"
)

type treeItem struct {
	entry persist.TreeEntry
	text  string // plain text content (for filtering)
}

var (
	treeAccentStyle    = oneshotAccentStyle
	treeSuccessStyle   = oneshotSuccessStyle
	treeMutedStyle     = oneshotMutedStyle
	treeAccentAltStyle = oneshotWarningStyle
	treeFgStyle        = oneshotNormalStyle
)

func treeSearchText(e persist.TreeEntry) string {
	if e.Msg == nil {
		return e.ID
	}
	switch m := e.Msg.(type) {
	case *libagent.UserMessage:
		return "user " + m.Content
	case *libagent.AssistantMessage:
		return "assistant " + libagent.AssistantText(m)
	case *libagent.ToolResultMessage:
		return m.ToolName + " " + m.Content
	}
	return e.Msg.GetRole()
}

func treePrefix(e persist.TreeEntry) string {
	totalChars := e.Depth * 3
	runes := make([]rune, totalChars)
	for i := range runes {
		runes[i] = ' '
	}

	connectorPos := -1
	if e.ShowConnector && e.Depth > 0 {
		connectorPos = e.Depth - 1
	}

	for i := range totalChars {
		level := i / 3
		pos := i % 3

		gutterShow := false
		hasGutter := false
		for _, g := range e.Gutters {
			if g.Position == level {
				hasGutter = true
				gutterShow = g.Show
				break
			}
		}

		if hasGutter {
			if pos == 0 && gutterShow {
				runes[i] = '│'
			}
		} else if connectorPos >= 0 && level == connectorPos {
			switch pos {
			case 0:
				if e.IsLastSibling {
					runes[i] = '└'
				} else {
					runes[i] = '├'
				}
			case 1:
				runes[i] = '─'
			}
		}
	}
	return treeMutedStyle.Render(string(runes))
}

func treeContentLabel(e persist.TreeEntry, selected bool) string {
	boldFn := func(s string) string {
		if selected {
			return treeAccentAltStyle.Bold(true).Render(s)
		}
		return treeFgStyle.Render(s)
	}

	if e.Msg == nil {
		return treeMutedStyle.Render(fmt.Sprintf("[node %s]", e.ID))
	}
	switch m := e.Msg.(type) {
	case *libagent.UserMessage:
		role := treeAccentStyle.Render("user: ")
		return role + boldFn(truncateText(m.Content, 80))
	case *libagent.AssistantMessage:
		role := treeSuccessStyle.Render("assistant: ")
		text := libagent.AssistantText(m)
		if text != "" {
			return role + boldFn(truncateText(text, 80))
		}
		return role + treeMutedStyle.Render("(no content)")
	case *libagent.ToolResultMessage:
		label := fmt.Sprintf("[%s]: %s", m.ToolName, truncateText(m.Content, 60))
		return treeMutedStyle.Render(label)
	}
	return treeMutedStyle.Render(fmt.Sprintf("[%s]", e.Msg.GetRole()))
}

func truncateText(s string, maxLen int) string {
	s = strings.Join(strings.Fields(s), " ")
	runes := []rune(s)
	if len(runes) <= maxLen {
		return s
	}
	return string(runes[:maxLen-1]) + "…"
}

func runTreeSelector(entries []persist.TreeEntry, sess *session.Session) error {
	items := make([]treeItem, len(entries))
	leafIdx := 0
	for i, e := range entries {
		items[i] = treeItem{
			entry: e,
			text:  treeSearchText(e),
		}
		if e.IsLeaf {
			leafIdx = i
		}
	}

	fl := newFilterList(
		"SESSION TREE",
		items,
		leafIdx,
		0,
		func(item treeItem) string { return item.text },
		func(item treeItem, selected bool) string {
			e := item.entry

			// Cursor glyph.
			cursor := "  "
			if selected {
				cursor = treeAccentStyle.Render("› ")
			}

			// Tree structure prefix (connectors/gutters).
			prefix := treePrefix(e)

			// Active-path bullet.
			bullet := treeMutedStyle.Render("+ ")
			if e.IsOnActivePath {
				bullet = treeAccentStyle.Render("• ")
			}

			// Content.
			content := treeContentLabel(e, selected)

			return cursor + prefix + bullet + content
		},
	)

	p := tea.NewProgram(fl, tea.WithAltScreen())
	final, err := p.Run()
	if err != nil {
		return err
	}
	result := final.(filterList[treeItem])
	if result.quitting || result.chosen == nil {
		return nil
	}

	editorText, err := sess.Navigate(result.chosen.entry.ID)
	if err != nil {
		return err
	}

	if editorText != "" {
		fmt.Fprintf(stderrWriter, "%s Navigated to node (branching from user message)\n", renderStatusSuccess("✓"))
		fmt.Fprintf(stderrWriter, "%s %s\n", renderDimText("Previous prompt:"), truncateText(editorText, 80))
	} else {
		fmt.Fprintf(stderrWriter, "%s Navigated to tree node\n", renderStatusSuccess("✓"))
	}
	return nil
}
