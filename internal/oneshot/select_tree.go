package oneshot

import (
	"errors"
	"fmt"
	"strings"

	"github.com/francescoalemanno/raijin-mono/internal/persist"
	"github.com/francescoalemanno/raijin-mono/internal/session"
	libagent "github.com/francescoalemanno/raijin-mono/libagent"
)

type treeItem struct {
	entry persist.TreeEntry
	text  string // plain text content (for filtering)
}

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

func truncateText(s string, maxLen int) string {
	s = strings.Join(strings.Fields(s), " ")
	runes := []rune(s)
	if len(runes) <= maxLen {
		return s
	}
	return string(runes[:maxLen-1]) + "…"
}

func treeFZFPrefix(e persist.TreeEntry) string {
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
	return string(runes)
}

func treeFZFContent(e persist.TreeEntry) string {
	if e.Msg == nil {
		return fmt.Sprintf("[node %s]", e.ID)
	}
	switch m := e.Msg.(type) {
	case *libagent.UserMessage:
		return "user: " + truncateText(m.Content, 80)
	case *libagent.AssistantMessage:
		text := libagent.AssistantText(m)
		if text == "" {
			return "assistant: (no content)"
		}
		return "assistant: " + truncateText(text, 80)
	case *libagent.ToolResultMessage:
		return fmt.Sprintf("[%s]: %s", m.ToolName, truncateText(m.Content, 60))
	}
	return fmt.Sprintf("[%s]", e.Msg.GetRole())
}

func runTreeSelector(entries []persist.TreeEntry, sess *session.Session) error {
	items := make([]treeItem, len(entries))
	for i, e := range entries {
		items[i] = treeItem{
			entry: e,
			text:  treeSearchText(e),
		}
	}

	fzfItems := make([]fzfPickerItem, 0, len(items))
	activeID := ""
	for _, item := range items {
		e := item.entry
		if e.IsLeaf {
			activeID = e.ID
		}
		bullet := "+ "
		if e.IsOnActivePath {
			bullet = "• "
		}
		label := treeFZFPrefix(e) + bullet + treeFZFContent(e)
		fzfItems = append(fzfItems, fzfPickerItem{
			key:   e.ID,
			label: label,
		})
	}
	chosenID, action, err := pickWithEmbeddedFZFInitial(fzfItems, "", false, true, activeID)
	if errors.Is(err, errFZFPickerUnavailable) {
		return fmt.Errorf("interactive picker requires a TTY")
	}
	if err != nil {
		return err
	}
	if action != fzfPickerActionSelect {
		return nil
	}

	editorText, err := sess.Navigate(chosenID)
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
