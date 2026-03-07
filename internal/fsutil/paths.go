package fsutil

import (
	"os"
	"path/filepath"
	"strings"
)

var unicodeSpaceReplacer = strings.NewReplacer(
	"\u00A0", " ",
	"\u2000", " ",
	"\u2001", " ",
	"\u2002", " ",
	"\u2003", " ",
	"\u2004", " ",
	"\u2005", " ",
	"\u2006", " ",
	"\u2007", " ",
	"\u2008", " ",
	"\u2009", " ",
	"\u200A", " ",
	"\u202F", " ",
	"\u205F", " ",
	"\u3000", " ",
)

func normalizeUnicodeSpaces(path string) string {
	return unicodeSpaceReplacer.Replace(path)
}

func normalizeAtPrefix(path string) string {
	if after, ok := strings.CutPrefix(path, "@"); ok {
		return after
	}
	return path
}

// ExpandPath normalizes unicode spaces, strips the @ mention prefix,
// and expands ~ to the current user's home directory.
func ExpandPath(path string) string {
	normalized := normalizeUnicodeSpaces(normalizeAtPrefix(path))
	if normalized == "~" {
		home, err := os.UserHomeDir()
		if err != nil {
			return normalized
		}
		return home
	}
	if strings.HasPrefix(normalized, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return normalized
		}
		return home + normalized[1:]
	}
	return normalized
}

// ResolveToCwd expands path semantics and resolves relative paths against cwd.
func ResolveToCwd(path string, cwd string) string {
	expanded := ExpandPath(path)
	if filepath.IsAbs(expanded) {
		return expanded
	}
	return filepath.Join(cwd, expanded)
}
