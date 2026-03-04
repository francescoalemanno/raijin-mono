package chat

import (
	"fmt"

	"github.com/francescoalemanno/raijin-mono/internal/theme"
	"github.com/francescoalemanno/raijin-mono/libtui/pkg/components"
	"github.com/francescoalemanno/raijin-mono/libtui/pkg/tui"
)

func visibleRange(selectedIndex, total, maxVisible int) (int, int) {
	if total <= 0 {
		return 0, 0
	}
	startIndex := max(0, min(selectedIndex-maxVisible/2, total-maxVisible))
	endIndex := min(startIndex+maxVisible, total)
	return startIndex, endIndex
}

func appendScrollInfo(list *tui.Container, selectedIndex, total, startIndex, endIndex int) {
	if startIndex > 0 || endIndex < total {
		scrollInfo := theme.Default.Muted.Ansi24(fmt.Sprintf("  (%d/%d)", selectedIndex+1, total))
		list.AddChild(components.NewText(scrollInfo, 0, 0, nil))
	}
}

func renderSelectorFrame(width int, borderTop, borderBottom *borderLine, titleText *components.Text, listContainer *tui.Container, hintText *components.Text, input *components.Input) []string {
	var lines []string
	lines = append(lines, borderTop.Render(width)...)
	lines = append(lines, "")
	lines = append(lines, titleText.Render(width)...)
	lines = append(lines, "")
	lines = append(lines, listContainer.Render(width)...)
	lines = append(lines, "")
	lines = append(lines, hintText.Render(width)...)
	lines = append(lines, borderBottom.Render(width)...)
	lines = append(lines, input.Render(width)...)
	lines = append(lines, borderBottom.Render(width)...)
	return lines
}

func renderPromptInputFrame(width int, borderTop, borderBottom *borderLine, titleText, hintText *components.Text, input *components.Input) []string {
	var lines []string
	lines = append(lines, borderTop.Render(width)...)
	lines = append(lines, "")
	lines = append(lines, titleText.Render(width)...)
	lines = append(lines, "")
	lines = append(lines, hintText.Render(width)...)
	lines = append(lines, borderBottom.Render(width)...)
	lines = append(lines, input.Render(width)...)
	lines = append(lines, borderBottom.Render(width)...)
	return lines
}
