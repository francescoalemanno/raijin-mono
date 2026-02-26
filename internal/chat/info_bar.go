package chat

import (
	"strings"
	"sync"

	"github.com/francescoalemanno/raijin-mono/internal/theme"
	"github.com/francescoalemanno/raijin-mono/libtui/pkg/tui"
	"github.com/francescoalemanno/raijin-mono/libtui/pkg/utils"
)

// infoBar renders a single line: left-aligned info + right-aligned model info.
type infoBar struct {
	mu        sync.RWMutex
	leftInfo  string
	rightInfo string
}

func newInfoBar() *infoBar {
	return &infoBar{}
}

func (b *infoBar) SetInfo(left, right string) {
	b.mu.Lock()
	b.leftInfo = left
	b.rightInfo = right
	b.mu.Unlock()
}

func (b *infoBar) Invalidate() {}

func (b *infoBar) HandleInput(data string) {}

func (b *infoBar) Render(width int) []string {
	b.mu.RLock()
	left, right := b.leftInfo, b.rightInfo
	b.mu.RUnlock()

	if width < 1 {
		width = 1
	}
	if left == "" && right == "" {
		return nil
	}

	leftW := utils.VisibleWidth(left)
	rightW := utils.VisibleWidth(right)
	gap := width - leftW - rightW
	if gap < 1 {
		gap = 1
	}
	// Theme the gap spaces with foreground color
	gapSpaces := theme.Default.Foreground.Ansi24(strings.Repeat(" ", gap))
	line := left + gapSpaces + right
	return []string{utils.TruncateToWidth(padToWidth(line, width), width, "")}
}

func padToWidth(line string, width int) string {
	vis := utils.VisibleWidth(line)
	if pad := width - vis; pad > 0 {
		return line + strings.Repeat(" ", pad)
	}
	return line
}

var _ tui.Component = (*infoBar)(nil)
