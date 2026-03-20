package oneshot

import (
	"errors"
	"fmt"

	modelconfig "github.com/francescoalemanno/raijin-mono/internal/config"
)

type modelItem struct {
	name      string
	isDefault bool
}

func runModelSelector(store *modelconfig.ModelStore) error {
	for {
		names := store.List()
		if len(names) == 0 {
			fmt.Println("No models configured")
			return nil
		}
		defaultName := store.DefaultName()
		items := make([]modelItem, len(names))
		for i, name := range names {
			items[i] = modelItem{name: name, isDefault: name == defaultName}
		}

		fzfItems := make([]fzfPickerItem, 0, len(items))
		for _, item := range items {
			label := item.name
			if item.isDefault {
				label += " (default)"
			}
			fzfItems = append(fzfItems, fzfPickerItem{key: item.name, label: label})
		}
		chosen, action, err := pickWithEmbeddedFZFInitial(fzfItems, "", true, false, defaultName)
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
			if err := store.Delete(chosen); err != nil {
				return err
			}
			fmt.Fprintf(stderrWriter, "%s Removed model: %s\n", renderStatusSuccess("✓"), chosen)
			continue
		case fzfPickerActionSelect:
			if err := store.SetDefault(chosen); err != nil {
				return err
			}
			fmt.Fprintf(stderrWriter, "%s Switched to model: %s\n", renderStatusSuccess("✓"), chosen)
			return nil
		default:
			return nil
		}
	}
}
