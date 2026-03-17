package oneshot

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"

	modelconfig "github.com/francescoalemanno/raijin-mono/internal/config"
)

type modelItem struct {
	name      string
	isDefault bool
}

var (
	modelSelectedStyle = oneshotAccentStyle
	modelDefaultStyle  = oneshotSuccessStyle
	modelNormalStyle   = oneshotNormalStyle
)

func runModelSelector(store *modelconfig.ModelStore) error {
	names := store.List()
	defaultName := store.DefaultName()
	items := make([]modelItem, len(names))
	cursor := 0
	for i, name := range names {
		items[i] = modelItem{name: name, isDefault: name == defaultName}
		if name == defaultName {
			cursor = i
		}
	}

	fl := newFilterList(
		"SELECT MODEL",
		items,
		cursor,
		0,
		func(item modelItem) string { return item.name },
		func(item modelItem, selected bool) string {
			label := item.name
			if item.isDefault {
				label += " (default)"
			}
			pointer := "  "
			if selected {
				pointer = "→ "
			}
			switch {
			case selected:
				return modelSelectedStyle.Render(pointer + label)
			case item.isDefault:
				return modelDefaultStyle.Render(pointer + label)
			default:
				return modelNormalStyle.Render(pointer + label)
			}
		},
	)
	fl.deletableFn = func(_ modelItem) bool { return true }

	p := tea.NewProgram(fl, tea.WithAltScreen())
	final, err := p.Run()
	if err != nil {
		return err
	}
	result := final.(filterList[modelItem])
	if result.quitting {
		return nil
	}

	if result.deleted != nil {
		name := result.deleted.name
		if err := store.Delete(name); err != nil {
			return err
		}
		fmt.Fprintf(stderrWriter, "%s Removed model: %s\n", renderStatusSuccess("✓"), name)
		return nil
	}

	if result.chosen == nil {
		return nil
	}

	name := result.chosen.name
	if err := store.SetDefault(name); err != nil {
		return err
	}

	fmt.Fprintf(stderrWriter, "%s Switched to model: %s\n", renderStatusSuccess("✓"), name)
	return nil
}
