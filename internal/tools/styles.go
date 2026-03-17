package tools

import (
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
