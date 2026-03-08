package chat

import (
	"strings"
	"sync"

	"github.com/francescoalemanno/raijin-mono/internal/theme"
	"github.com/francescoalemanno/raijin-mono/libtui/pkg/tui"
	"github.com/francescoalemanno/raijin-mono/libtui/pkg/utils"
)

// infoBar renders a single row made of ordered parts.
// Parts are spread across the full width; when they don't fit, rightmost parts
// are dropped until the remaining parts fit.
type infoBar struct {
	mu    sync.RWMutex
	parts []string
}

func newInfoBar() *infoBar {
	return &infoBar{}
}

func (b *infoBar) SetParts(parts []string) {
	b.mu.Lock()
	b.parts = append([]string(nil), parts...)
	b.mu.Unlock()
}

// SetInfo preserves backward compatibility for older callers.
func (b *infoBar) SetInfo(left, right string) {
	parts := make([]string, 0, 2)
	if left != "" {
		parts = append(parts, left)
	}
	if right != "" {
		parts = append(parts, right)
	}
	b.SetParts(parts)
}

func (b *infoBar) Invalidate() {}

func (b *infoBar) HandleInput(data string) {}

func (b *infoBar) Render(width int) []string {
	b.mu.RLock()
	parts := append([]string(nil), b.parts...)
	b.mu.RUnlock()

	if width < 1 {
		width = 1
	}
	parts = filterNonEmpty(parts)
	if len(parts) == 0 {
		return nil
	}

	for len(parts) > 1 && !partsFit(parts, width) {
		parts = parts[:len(parts)-1]
	}
	if len(parts) == 1 {
		return []string{utils.TruncateToWidthPadded(parts[0], width, "")}
	}

	total := 0
	for _, p := range parts {
		total += utils.VisibleWidth(p)
	}

	gaps := len(parts) - 1
	free := width - total
	if free < gaps {
		free = gaps
	}
	baseGap := free / gaps
	extra := free % gaps

	var line strings.Builder
	for i, p := range parts {
		if i > 0 {
			gap := baseGap
			if extra > 0 {
				gap++
				extra--
			}
			line.WriteString(theme.Default.Foreground.Ansi24(strings.Repeat(" ", gap)))
		}
		line.WriteString(p)
	}

	return []string{padToWidth(line.String(), width)}
}

func partsFit(parts []string, width int) bool {
	if len(parts) == 0 {
		return true
	}
	total := 0
	for _, part := range parts {
		total += utils.VisibleWidth(part)
	}
	return total+(len(parts)-1) <= width
}

func filterNonEmpty(parts []string) []string {
	out := parts[:0]
	for _, p := range parts {
		if strings.TrimSpace(p) == "" {
			continue
		}
		out = append(out, p)
	}
	return out
}

func padToWidth(line string, width int) string {
	vis := utils.VisibleWidth(line)
	if pad := width - vis; pad > 0 {
		return line + strings.Repeat(" ", pad)
	}
	return line
}

var _ tui.Component = (*infoBar)(nil)
