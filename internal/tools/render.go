package tools

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
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
		if after, ok := strings.CutPrefix(normalized, cwdNormalized+"/"); ok {
			normalized = "./" + after
		} else if normalized == cwdNormalized {
			normalized = "."
		}
	}

	// If not under cwd, try home directory
	if home, err := os.UserHomeDir(); err == nil {
		homeNormalized := filepath.ToSlash(home)
		homeNormalized = strings.TrimSuffix(homeNormalized, "/")
		if after, ok := strings.CutPrefix(normalized, homeNormalized+"/"); ok {
			normalized = "~/" + after
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
	return highlightCodeANSI(content, "", path, defaultCodeStyle())
}

func renderDiffPreview(path, oldStr, newStr string) string {
	details := generateDiffString(oldStr, newStr, 2)
	if details.Diff == "" {
		return ""
	}
	return renderDiffText(details.Diff)
}

// RenderDiffText renders plain diff text using TUI diff colors.
func RenderDiffText(diff string) string {
	return renderDiffText(diff)
}

// DiffFromMetadata extracts diff text from tool metadata, if present.
func DiffFromMetadata(metadata string) string {
	if strings.TrimSpace(metadata) == "" {
		return ""
	}
	var details EditToolDetails
	if err := json.Unmarshal([]byte(metadata), &details); err != nil {
		return ""
	}
	return strings.TrimSpace(details.Diff)
}

func renderDiffText(diff string) string {
	if strings.TrimSpace(diff) == "" {
		return ""
	}

	var b strings.Builder
	for line := range strings.SplitSeq(diff, "\n") {
		switch {
		case strings.HasPrefix(line, "+"):
			b.WriteString(diffAddedStyle.Render(line))
		case strings.HasPrefix(line, "-"):
			b.WriteString(diffRemovedStyle.Render(line))
		default:
			b.WriteString(diffContextStyle.Render(line))
		}
		b.WriteByte('\n')
	}

	return strings.TrimRight(b.String(), "\n")
}
