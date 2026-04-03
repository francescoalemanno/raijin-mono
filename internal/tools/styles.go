package tools

import (
	"charm.land/lipgloss/v2"
)

var (
	diffAddedStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("#7EE787"))
	diffRemovedStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#FFA198"))
	diffContextStyle = lipgloss.NewStyle().Faint(true)
)
