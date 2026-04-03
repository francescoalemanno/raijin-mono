package oneshot

import (
	"strings"

	"charm.land/lipgloss/v2"
)

const userSeparatorWidth = 24

func themeStyle(color string) lipgloss.Style {
	return lipgloss.NewStyle().Foreground(lipgloss.Color(color))
}

var (
	oneshotAccentStyle   = themeStyle("6")
	oneshotSuccessStyle  = themeStyle("10")
	oneshotWarningStyle  = themeStyle("11")
	oneshotDangerStyle   = themeStyle("9")
	oneshotMutedStyle    = themeStyle("8")
	oneshotNormalStyle   = themeStyle("7")
	oneshotProviderStyle = themeStyle("13").Bold(true)
	oneshotDimStyle      = lipgloss.NewStyle().Faint(true)

	oneshotUserPrefixStyle = themeStyle("10").Bold(true)
	oneshotInfoIconStyle   = themeStyle("12").Bold(true)
	oneshotOkIconStyle     = themeStyle("10").Bold(true)
	oneshotWarnIconStyle   = themeStyle("11").Bold(true)
	oneshotErrIconStyle    = themeStyle("9").Bold(true)
	oneshotTimestampStyle  = themeStyle("8").Faint(true)
)

func renderUserPrefix() string {
	return oneshotUserPrefixStyle.Render(userPromptGlyph) + " "
}

func renderUserSeparator() string {
	return oneshotMutedStyle.Render(strings.Repeat("─", userSeparatorWidth))
}

func renderStatusInfo(icon string) string {
	return oneshotInfoIconStyle.Render(icon)
}

func renderStatusSuccess(icon string) string {
	return oneshotOkIconStyle.Render(icon)
}

func renderStatusWarning(icon string) string {
	return oneshotWarnIconStyle.Render(icon)
}

func renderStatusError(icon string) string {
	return oneshotErrIconStyle.Render(icon)
}

func renderStatusTimestamp(ts string) string {
	return oneshotTimestampStyle.Render(ts)
}

func renderDimText(s string) string {
	return oneshotDimStyle.Render(s)
}

// RenderThemedDim renders muted text using the shared theme.
func RenderThemedDim(s string) string {
	return oneshotDimStyle.Render(s)
}

// RenderThemedAccent renders accent text using the shared theme.
func RenderThemedAccent(s string) string {
	return oneshotInfoIconStyle.Render(s)
}

// RenderThemedModel renders provider/model text using the shared theme.
func RenderThemedModel(s string) string {
	return oneshotProviderStyle.Render(s)
}

// RenderThemedWarn renders warning text using the shared theme.
func RenderThemedWarn(s string) string {
	return oneshotWarnIconStyle.Render(s)
}

// RenderThemedOK renders success text using the shared theme.
func RenderThemedOK(s string) string {
	return oneshotOkIconStyle.Render(s)
}

// RenderThemedErr renders error text using the shared theme.
func RenderThemedErr(s string) string {
	return oneshotErrIconStyle.Render(s)
}

// RenderThemedInfo renders info text using the shared theme.
func RenderThemedInfo(s string) string {
	return oneshotInfoIconStyle.Render(s)
}
