package tools

import (
	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/styles"
	"github.com/charmbracelet/lipgloss"
)

var (
	diffAddedStyle = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{
		Light: "#1A7F37",
		Dark:  "#7EE787",
	})
	diffRemovedStyle = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{
		Light: "#B42318",
		Dark:  "#FFA198",
	})
	diffContextStyle = lipgloss.NewStyle().Faint(true)
)

func defaultCodeStyle() *chroma.Style {
	if lipgloss.HasDarkBackground() {
		if style := styles.Get("monokai"); style != nil {
			return style
		}
	}
	if style := styles.Get("github"); style != nil {
		return style
	}
	return styles.Fallback
}
