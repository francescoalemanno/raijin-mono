// Package fsutil provides common file system utilities used across the codebase.
package fsutil

import (
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// VCSDirs are VCS directories that should always be skipped.
var VCSDirs = map[string]bool{
	".git": true,
	".hg":  true,
	".svn": true,
}

// MentionExcludedDirs are high-churn/non-source directories excluded from @ completion scans.
var MentionExcludedDirs = map[string]bool{
	"node_modules": true,
	"vendor":       true,
	"build":        true,
}

// IsHiddenName reports whether a path entry should be treated as hidden.
func IsHiddenName(name string) bool {
	return strings.HasPrefix(name, ".")
}

// ShouldSkipMentionDir reports whether a directory should be skipped when building @ completion candidates.
func ShouldSkipMentionDir(name string) bool {
	return IsHiddenName(name) || VCSDirs[name] || MentionExcludedDirs[name] || strings.HasPrefix(name, "_external")
}

// IsTextFile checks if a file is a text file by examining its MIME type.
func IsTextFile(filePath string) bool {
	file, err := os.Open(filePath)
	if err != nil {
		return false
	}
	defer file.Close()

	buffer := make([]byte, 512)
	n, err := file.Read(buffer)
	if err != nil && err != io.EOF {
		return false
	}

	contentType := http.DetectContentType(buffer[:n])

	return strings.HasPrefix(contentType, "text/") ||
		contentType == "application/json" ||
		contentType == "application/xml" ||
		contentType == "application/javascript" ||
		contentType == "application/x-sh"
}

// NormalizePath converts a file path to use forward slashes for cross-platform consistency.
func NormalizePath(path string) string {
	return filepath.ToSlash(path)
}

// NormalizePaths converts all paths in a slice to use forward slashes.
func NormalizePaths(paths []string) {
	for i, p := range paths {
		paths[i] = filepath.ToSlash(p)
	}
}
