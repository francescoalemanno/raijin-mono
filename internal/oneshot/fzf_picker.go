package oneshot

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"strings"

	"golang.org/x/term"

	"github.com/francescoalemanno/raijin-mono/internal/shellinit"
)

type fzfPickerItem struct {
	key     string
	label   string
	preview string
}

type fzfPickerAction string

const (
	fzfPickerActionCancel fzfPickerAction = "cancel"
	fzfPickerActionSelect fzfPickerAction = "select"
	fzfPickerActionDelete fzfPickerAction = "delete"
)

var errFZFPickerUnavailable = errors.New("fzf picker unavailable")

func pickWithEmbeddedFZF(items []fzfPickerItem, query string, allowDelete bool, preserveOrder bool) (string, fzfPickerAction, error) {
	return pickWithEmbeddedFZFInitial(items, query, allowDelete, preserveOrder, "")
}

func pickWithEmbeddedFZFInitial(items []fzfPickerItem, query string, allowDelete bool, preserveOrder bool, initialKey string) (string, fzfPickerAction, error) {
	if !canUseEmbeddedFZF() {
		return "", fzfPickerActionCancel, errFZFPickerUnavailable
	}

	lines, lineToKey, previewEnabled := buildFZFPickerLines(items)
	if len(lines) == 0 {
		return "", fzfPickerActionCancel, nil
	}

	var stdin bytes.Buffer
	for _, line := range lines {
		stdin.WriteString(line)
		stdin.WriteByte('\n')
	}

	cfg := shellinit.RunFZFOptions{}
	cfg.DisableSort = preserveOrder
	cfg.InitialPosition = pickerLinePosition(lines, lineToKey, initialKey)
	if previewEnabled {
		cfg.Delimiter = "\t"
		cfg.WithNth = "1"
		cfg.PreviewCommand = "printf '%b' {2}"
		cfg.PreviewWindow = "right:55%,wrap"
		cfg.PreviewLabel = "Docs"
	}
	if allowDelete {
		cfg.ExpectKeys = []string{"ctrl-x"}
		cfg.Bindings = []string{"ctrl-x:accept"}
		cfg.DisableSingleItemBypass = true
		cfg.DisableSelectOne = true
		cfg.Header = ">>> ENTER = SELECT | CTRL+X = DELETE <<<"
	}
	result, err := shellinit.RunFZFWithOptions("default", strings.TrimSpace(query), &stdin, cfg)
	if err != nil {
		return "", fzfPickerActionCancel, err
	}
	if result.Code != 0 {
		return "", fzfPickerActionCancel, nil
	}

	chosen := ""
	if len(result.Selected) > 0 {
		chosen = strings.TrimRight(result.Selected[0], "\r")
	}
	if strings.TrimSpace(chosen) == "" {
		return "", fzfPickerActionCancel, nil
	}
	key, ok := lineToKey[chosen]
	if !ok {
		return "", fzfPickerActionCancel, nil
	}
	if result.Key == "ctrl-x" {
		return key, fzfPickerActionDelete, nil
	}
	return key, fzfPickerActionSelect, nil
}

func pickerLinePosition(lines []string, lineToKey map[string]string, initialKey string) int {
	if strings.TrimSpace(initialKey) == "" {
		return 0
	}
	for i, line := range lines {
		if lineToKey[line] == initialKey {
			return i + 1
		}
	}
	return 0
}

func canUseEmbeddedFZF() bool {
	return term.IsTerminal(int(os.Stdin.Fd())) && term.IsTerminal(int(os.Stdout.Fd()))
}

func buildFZFPickerLines(items []fzfPickerItem) ([]string, map[string]string, bool) {
	lines := make([]string, 0, len(items))
	lineToKey := make(map[string]string, len(items))
	previewEnabled := false

	for _, item := range items {
		label := item.label
		if strings.TrimSpace(label) == "" {
			continue
		}
		line := label
		if _, exists := lineToKey[line]; exists {
			line = fmt.Sprintf("%s [%s]", label, strings.TrimSpace(item.key))
		}
		for _, exists := lineToKey[line]; exists; _, exists = lineToKey[line] {
			line += "*"
		}
		if preview := strings.TrimSpace(item.preview); preview != "" {
			previewEnabled = true
			line += "\t" + encodeFZFPreviewText(preview)
		}
		lineToKey[line] = item.key
		lines = append(lines, line)
	}

	return lines, lineToKey, previewEnabled
}

func encodeFZFPreviewText(text string) string {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	text = strings.ReplaceAll(text, "\\", "\\\\")
	text = strings.ReplaceAll(text, "\t", "    ")
	text = strings.ReplaceAll(text, "\n", "\\n")
	return text
}
