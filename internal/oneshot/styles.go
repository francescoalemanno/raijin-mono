package oneshot

import "github.com/charmbracelet/lipgloss"

func adaptiveStyle(light, dark string) lipgloss.Style {
	return lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{
		Light: light,
		Dark:  dark,
	})
}

var (
	oneshotAccentStyle   = adaptiveStyle("4", "6")
	oneshotSuccessStyle  = adaptiveStyle("2", "10")
	oneshotWarningStyle  = adaptiveStyle("3", "11")
	oneshotDangerStyle   = adaptiveStyle("1", "9")
	oneshotMutedStyle    = adaptiveStyle("8", "8")
	oneshotNormalStyle   = adaptiveStyle("0", "7")
	oneshotProviderStyle = adaptiveStyle("5", "13").Bold(true)
	oneshotDimStyle      = lipgloss.NewStyle().Faint(true)

	oneshotUserPrefixStyle = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "2", Dark: "10"}).Bold(true)
	oneshotInfoIconStyle   = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "4", Dark: "12"}).Bold(true)
	oneshotOkIconStyle     = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "2", Dark: "10"}).Bold(true)
	oneshotWarnIconStyle   = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "3", Dark: "11"}).Bold(true)
	oneshotErrIconStyle    = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "1", Dark: "9"}).Bold(true)
	oneshotTimestampStyle  = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "8", Dark: "8"}).Faint(true)
)

func renderUserPrefix() string {
	return oneshotUserPrefixStyle.Render(userPromptGlyph) + " "
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

// RenderThemedDim renders muted text using the shared adaptive theme.
func RenderThemedDim(s string) string {
	return oneshotDimStyle.Render(s)
}

// RenderThemedAccent renders accent text using the shared adaptive theme.
func RenderThemedAccent(s string) string {
	return oneshotInfoIconStyle.Render(s)
}

// RenderThemedModel renders provider/model text using the shared adaptive theme.
func RenderThemedModel(s string) string {
	return oneshotProviderStyle.Render(s)
}

// RenderThemedWarn renders warning text using the shared adaptive theme.
func RenderThemedWarn(s string) string {
	return oneshotWarnIconStyle.Render(s)
}

// RenderThemedOK renders success text using the shared adaptive theme.
func RenderThemedOK(s string) string {
	return oneshotOkIconStyle.Render(s)
}

// RenderThemedErr renders error text using the shared adaptive theme.
func RenderThemedErr(s string) string {
	return oneshotErrIconStyle.Render(s)
}

// RenderThemedInfo renders info text using the shared adaptive theme.
func RenderThemedInfo(s string) string {
	return oneshotInfoIconStyle.Render(s)
}
