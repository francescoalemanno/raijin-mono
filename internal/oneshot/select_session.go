package oneshot

import (
	"context"
	"errors"
	"fmt"

	"github.com/francescoalemanno/raijin-mono/internal/persist"
	"github.com/francescoalemanno/raijin-mono/internal/session"
)

func runSessionSelector(summaries []persist.SessionSummary, opts Options, store *persist.Store) error {
	for {
		if len(summaries) == 0 {
			fmt.Println("No previous sessions found")
			return nil
		}

		activeID := store.CurrentSessionID()
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
		chosenID, action, err := pickWithEmbeddedFZF(fzfItems, "", true, false)
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
			if err := store.RemoveSession(chosenID); err != nil {
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
			summaries = store.ListSessionSummaries()
			continue
		case fzfPickerActionSelect:
			if err := store.SetCurrent(chosenID); err != nil {
				return err
			}

			sess, sessErr := session.New(opts.RuntimeModel)
			if sessErr != nil && sess == nil {
				return sessErr
			}
			if sess != nil {
				_ = sess.SwitchTo(context.Background(), chosenID)
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
