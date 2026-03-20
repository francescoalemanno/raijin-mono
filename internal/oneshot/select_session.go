package oneshot

import (
	"context"
	"errors"
	"fmt"

	"github.com/francescoalemanno/raijin-mono/internal/persist"
	"github.com/francescoalemanno/raijin-mono/internal/session"
)

func runSessionSelector(summaries []persist.SessionSummary, sess *session.Session) error {
	for {
		if len(summaries) == 0 {
			fmt.Println("No previous sessions found")
			return nil
		}

		activeID := sess.ID()
		fzfItems := make([]fzfPickerItem, 0, len(summaries))
		for _, summary := range summaries {
			title := summary.Title
			if title == "" {
				title = "(untitled)"
			}
			label := fmt.Sprintf("[%s] %s", summary.ShortID, title)
			if summary.ID == activeID {
				label += " (current)"
			}
			fzfItems = append(fzfItems, fzfPickerItem{key: summary.ID, label: label})
		}
		chosenID, action, err := pickWithEmbeddedFZFInitial(fzfItems, "", true, false, activeID)
		if errors.Is(err, errFZFPickerUnavailable) {
			return fmt.Errorf("interactive picker requires a TTY")
		}
		if err != nil {
			return err
		}
		switch action {
		case fzfPickerActionCancel:
			return nil
		case fzfPickerActionDelete:
			if err := sess.RemoveSession(chosenID); err != nil {
				return err
			}
			var deletedTitle string
			var deletedShortID string
			for _, summary := range summaries {
				if summary.ID == chosenID {
					deletedTitle = summary.Title
					deletedShortID = summary.ShortID
					break
				}
			}
			if deletedTitle == "" {
				deletedTitle = "(untitled)"
			}
			fmt.Fprintf(stderrWriter, "%s Removed session [%s] %s\n",
				renderStatusSuccess("✓"),
				deletedShortID,
				deletedTitle)
			summaries = sess.ListSessionSummaries()
			continue
		case fzfPickerActionSelect:
			if err := sess.SwitchTo(context.Background(), chosenID); err != nil {
				return err
			}

			var selected persist.SessionSummary
			for _, summary := range summaries {
				if summary.ID == chosenID {
					selected = summary
					break
				}
			}
			fmt.Fprintf(stderrWriter, "%s Switched to session [%s] %s\n",
				renderStatusSuccess("✓"),
				selected.ShortID,
				selected.Title)
			return nil
		default:
			return nil
		}
	}
}
