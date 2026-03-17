package oneshot

import (
	"context"
	"fmt"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/francescoalemanno/raijin-mono/internal/persist"
	"github.com/francescoalemanno/raijin-mono/internal/session"
)

var (
	sessSelectedStyle = oneshotAccentStyle
	sessActiveStyle   = oneshotSuccessStyle
	sessNormalStyle   = oneshotNormalStyle
)

func runSessionSelector(summaries []persist.SessionSummary, opts Options, store *persist.Store) error {
	for {
		if len(summaries) == 0 {
			fmt.Println("No previous sessions found")
			return nil
		}

		activeID := store.CurrentSessionID()
		fl := newFilterList(
			"SESSIONS",
			summaries,
			0,
			0,
			func(item persist.SessionSummary) string {
				return item.ShortID + " " + item.Title
			},
			func(item persist.SessionSummary, selected bool) string {
				title := item.Title
				if title == "" {
					title = "(untitled)"
				}
				label := fmt.Sprintf("[%s] %s", item.ShortID, title)
				if item.ID == activeID {
					label += " (current)"
				}
				pointer := "  "
				if selected {
					pointer = "→ "
					return sessSelectedStyle.Render(pointer + label)
				}
				if item.ID == activeID {
					return sessActiveStyle.Render(pointer + label)
				}
				return sessNormalStyle.Render(pointer + label)
			},
		)
		fl.deletableFn = func(item persist.SessionSummary) bool {
			_ = item
			return true
		}

		p := tea.NewProgram(fl, tea.WithAltScreen())
		final, err := p.Run()
		if err != nil {
			return err
		}
		result := final.(filterList[persist.SessionSummary])
		if result.quitting {
			return nil
		}

		if result.deleted != nil {
			if err := store.RemoveSession(result.deleted.ID); err != nil {
				return err
			}
			title := result.deleted.Title
			if title == "" {
				title = "(untitled)"
			}
			fmt.Fprintf(stderrWriter, "%s Removed session [%s] %s\n",
				renderStatusSuccess("✓"),
				result.deleted.ShortID,
				title)
			summaries = store.ListSessionSummaries()
			continue
		}

		if result.chosen == nil {
			return nil
		}

		sessionID := result.chosen.ID
		if err := store.SetCurrent(sessionID); err != nil {
			return err
		}

		sess, sessErr := session.New(opts.RuntimeModel)
		if sessErr != nil && sess == nil {
			return sessErr
		}
		if sess != nil {
			_ = sess.SwitchTo(context.Background(), sessionID)
		}

		fmt.Fprintf(stderrWriter, "%s Switched to session [%s] %s\n",
			renderStatusSuccess("✓"),
			result.chosen.ShortID,
			result.chosen.Title)
		return nil
	}
}
