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
	key   string
	label string
}

type fzfPickerAction string

const (
	fzfPickerActionCancel fzfPickerAction = "cancel"
	fzfPickerActionSelect fzfPickerAction = "select"
	fzfPickerActionDelete fzfPickerAction = "delete"
)

var errFZFPickerUnavailable = errors.New("fzf picker unavailable")

func pickWithEmbeddedFZF(items []fzfPickerItem, query string, allowDelete bool, preserveOrder bool) (string, fzfPickerAction, error) {
	if !canUseEmbeddedFZF() {
		return "", fzfPickerActionCancel, errFZFPickerUnavailable
	}

	lines, lineToKey := buildFZFPickerLines(items)
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

func canUseEmbeddedFZF() bool {
	return term.IsTerminal(int(os.Stdin.Fd())) && term.IsTerminal(int(os.Stdout.Fd()))
}

func buildFZFPickerLines(items []fzfPickerItem) ([]string, map[string]string) {
	lines := make([]string, 0, len(items))
	lineToKey := make(map[string]string, len(items))

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
		lineToKey[line] = item.key
		lines = append(lines, line)
	}

	return lines, lineToKey
}
