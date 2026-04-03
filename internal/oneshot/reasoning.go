package oneshot

import (
	"errors"
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"

	libagent "github.com/francescoalemanno/raijin-mono/libagent"
)

type reasoningItem struct {
	level libagent.ThinkingLevel
	desc  string
}

var (
	reasoningSelectedStyle = oneshotAccentStyle
	reasoningCurrentStyle  = oneshotSuccessStyle
	reasoningNormalStyle   = oneshotNormalStyle

	reasoningLevels = []reasoningItem{
		{level: libagent.ThinkingLevelLow, desc: "faster, lighter reasoning"},
		{level: libagent.ThinkingLevelMedium, desc: "balanced default"},
		{level: libagent.ThinkingLevelHigh, desc: "deeper reasoning"},
		{level: libagent.ThinkingLevelMax, desc: "maximum reasoning effort"},
	}
)

func handleReasoning(opts Options, rawArg string) error {
	if opts.Store == nil {
		return errors.New("no model store available")
	}

	defaultName := strings.TrimSpace(opts.Store.DefaultName())
	if defaultName == "" {
		return errors.New("no default model configured; use /add-model first")
	}

	modelCfg, ok := opts.Store.Get(defaultName)
	if !ok {
		return fmt.Errorf("default model not found in store: %s", defaultName)
	}

	current := libagent.NormalizeThinkingLevel(modelCfg.ThinkingLevel)
	next, err := resolveReasoningSelection(rawArg, current)
	if err != nil {
		return err
	}
	if next == "" {
		return nil
	}

	modelCfg.ThinkingLevel = next
	if err := opts.Store.Add(modelCfg); err != nil {
		return err
	}

	fmt.Fprintf(stderrWriter, "%s Reasoning level set to %s for %s\n", renderStatusSuccess("✓"), next, defaultName)
	return nil
}

func resolveReasoningSelection(rawArg string, current libagent.ThinkingLevel) (libagent.ThinkingLevel, error) {
	arg := strings.TrimSpace(rawArg)
	if arg == "" {
		selected, ok, err := runReasoningSelector(current)
		if err != nil || !ok {
			return "", err
		}
		return selected, nil
	}

	level, ok := parseReasoningLevel(arg)
	if !ok {
		return "", fmt.Errorf("invalid reasoning level %q; expected one of: low, medium, high, max", arg)
	}
	return level, nil
}

func parseReasoningLevel(raw string) (libagent.ThinkingLevel, bool) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "low":
		return libagent.ThinkingLevelLow, true
	case "medium":
		return libagent.ThinkingLevelMedium, true
	case "high":
		return libagent.ThinkingLevelHigh, true
	case "max":
		return libagent.ThinkingLevelMax, true
	default:
		return "", false
	}
}

func runReasoningSelector(current libagent.ThinkingLevel) (libagent.ThinkingLevel, bool, error) {
	items := make([]reasoningItem, len(reasoningLevels))
	copy(items, reasoningLevels)

	cursor := 0
	for i, item := range items {
		if item.level == current {
			cursor = i
			break
		}
	}

	fl := newFilterList(
		"SELECT REASONING LEVEL",
		items,
		cursor,
		0,
		func(item reasoningItem) string {
			return string(item.level) + " " + item.desc
		},
		func(item reasoningItem, selected bool) string {
			label := string(item.level)
			if item.level == current {
				label += " (current)"
			}
			if item.desc != "" {
				label += " — " + item.desc
			}
			pointer := "  "
			if selected {
				pointer = "→ "
			}
			switch {
			case selected:
				return reasoningSelectedStyle.Render(pointer + label)
			case item.level == current:
				return reasoningCurrentStyle.Render(pointer + label)
			default:
				return reasoningNormalStyle.Render(pointer + label)
			}
		},
	)

	p := tea.NewProgram(fl)
	final, err := p.Run()
	if err != nil {
		return "", false, err
	}
	result := final.(filterList[reasoningItem])
	if result.quitting || result.chosen == nil {
		return "", false, nil
	}
	return result.chosen.level, true, nil
}
