package tools

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/francescoalemanno/raijin-mono/internal/theme"
	tuiutils "github.com/francescoalemanno/raijin-mono/libtui/pkg/utils"
)

// RenderPath formats a path for display:
// - Paths in cwd are shown as relative to ./
// - Paths in home directory are shown as relative to ~/
func RenderPath(path string) string {
	if path == "" {
		return path
	}

	// Convert to forward slashes for consistency
	normalized := filepath.ToSlash(path)

	// Try to make path relative to cwd first (higher priority)
	if cwd, err := os.Getwd(); err == nil {
		cwdNormalized := filepath.ToSlash(cwd)
		cwdNormalized = strings.TrimSuffix(cwdNormalized, "/")
		if strings.HasPrefix(normalized, cwdNormalized+"/") {
			normalized = "./" + strings.TrimPrefix(normalized, cwdNormalized+"/")
		} else if normalized == cwdNormalized {
			normalized = "."
		}
	}

	// If not under cwd, try home directory
	if home, err := os.UserHomeDir(); err == nil {
		homeNormalized := filepath.ToSlash(home)
		homeNormalized = strings.TrimSuffix(homeNormalized, "/")
		if strings.HasPrefix(normalized, homeNormalized+"/") {
			normalized = "~/" + strings.TrimPrefix(normalized, homeNormalized+"/")
		} else if normalized == homeNormalized {
			normalized = "~"
		}
	}

	return normalized
}

// renderDiffPreview formats old_str → new_str as a compact, context-aware diff for TUI display.
func renderCodePreview(path, content string) string {
	if content == "" {
		return ""
	}
	return tuiutils.HighlightCodeANSI(content, "", path, theme.Default.ChromaStyle())
}

func renderDiffPreview(path, oldStr, newStr string) string {
	details := generateDiffString(oldStr, newStr, 2)
	if details.Diff == "" {
		return ""
	}

	var b strings.Builder
	for _, line := range strings.Split(details.Diff, "\n") {
		switch {
		case strings.HasPrefix(line, "+"):
			b.WriteString(theme.Default.DiffAdded.Ansi24(line))
		case strings.HasPrefix(line, "-"):
			b.WriteString(theme.Default.DiffRemoved.Ansi24(line))
		default:
			b.WriteString(theme.Default.Muted.Ansi24(line))
		}
		b.WriteByte('\n')
	}

	return strings.TrimRight(b.String(), "\n")
}
