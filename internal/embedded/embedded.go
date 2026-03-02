// Package embedded provides unified access to embedded resources through
// a simple file-system like interface supporting listing, filtering, and reading.
package embedded

import (
	"embed"
	"io/fs"
	"path"
	"strings"
)

// Scheme is the URL-style prefix for embedded resources.
const Scheme = "embedded://"

//go:embed embedded/*
var embeddedFS embed.FS

// FS provides access to the embedded file system.
func FS() embed.FS {
	return embeddedFS
}

// Root returns the root directory path for embedded resources.
func Root() string {
	return "embedded"
}

// List returns all entries in a directory.
// dir should be relative to the embedded root (e.g., "skills", "templates").
func List(dir string) ([]fs.DirEntry, error) {
	fullPath := path.Join("embedded", dir)
	return embeddedFS.ReadDir(fullPath)
}

// ListFiles returns only files (not subdirectories) in a directory.
// If ext is non-empty, only files with that extension are returned.
func ListFiles(dir string, ext string) ([]fs.DirEntry, error) {
	entries, err := List(dir)
	if err != nil {
		return nil, err
	}

	var files []fs.DirEntry
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if ext != "" && !strings.HasSuffix(strings.ToLower(entry.Name()), ext) {
			continue
		}
		files = append(files, entry)
	}
	return files, nil
}

// ListSubdirs returns only subdirectories in a directory.
func ListSubdirs(dir string) ([]fs.DirEntry, error) {
	entries, err := List(dir)
	if err != nil {
		return nil, err
	}

	var dirs []fs.DirEntry
	for _, entry := range entries {
		if entry.IsDir() {
			dirs = append(dirs, entry)
		}
	}
	return dirs, nil
}

// ReadFile reads a file from the embedded FS.
// filePath should be relative to the embedded root.
func ReadFile(filePath string) ([]byte, error) {
	fullPath := path.Join("embedded", filePath)
	return embeddedFS.ReadFile(fullPath)
}

// ReadString reads a file and returns its contents as a string.
func ReadString(filePath string) (string, error) {
	data, err := ReadFile(filePath)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// Exists reports whether a path exists in the embedded FS.
func Exists(filePath string) bool {
	fullPath := path.Join("embedded", filePath)
	_, err := embeddedFS.Open(fullPath)
	return err == nil
}

// DirExists reports whether a directory exists in the embedded FS.
func DirExists(dir string) bool {
	entries, err := List(dir)
	return err == nil && entries != nil
}

// Walk walks the directory tree rooted at dir, calling fn for each file.
func Walk(dir string, fn func(path string, d fs.DirEntry) error) error {
	fullPath := path.Join("embedded", dir)
	return fs.WalkDir(embeddedFS, fullPath, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		// Strip the "embedded/" prefix from the path
		relPath := strings.TrimPrefix(p, "embedded/")
		return fn(relPath, d)
	})
}

// Filter is a predicate for filtering directory entries.
type Filter func(fs.DirEntry) bool

// ListFiltered returns entries that match all provided filters.
func ListFiltered(dir string, filters ...Filter) ([]fs.DirEntry, error) {
	entries, err := List(dir)
	if err != nil {
		return nil, err
	}

	var filtered []fs.DirEntry
	for _, entry := range entries {
		match := true
		for _, f := range filters {
			if !f(entry) {
				match = false
				break
			}
		}
		if match {
			filtered = append(filtered, entry)
		}
	}
	return filtered, nil
}

// Common filters

// IsFile returns true for regular files (not directories).
func IsFile(entry fs.DirEntry) bool {
	return !entry.IsDir()
}

// IsDir returns true for directories.
func IsDir(entry fs.DirEntry) bool {
	return entry.IsDir()
}

// HasExtension returns a filter that matches files with the given extension.
// The extension should include the dot (e.g., ".md").
func HasExtension(ext string) Filter {
	return func(entry fs.DirEntry) bool {
		return strings.HasSuffix(strings.ToLower(entry.Name()), ext)
	}
}

// NameContains returns a filter that matches entries containing the given substring.
func NameContains(substr string) Filter {
	return func(entry fs.DirEntry) bool {
		return strings.Contains(strings.ToLower(entry.Name()), strings.ToLower(substr))
	}
}

// NameStartsWith returns a filter that matches entries starting with the given prefix.
func NameStartsWith(prefix string) Filter {
	return func(entry fs.DirEntry) bool {
		return strings.HasPrefix(strings.ToLower(entry.Name()), strings.ToLower(prefix))
	}
}

// NameEquals returns a filter that matches entries with the exact name.
func NameEquals(name string) Filter {
	return func(entry fs.DirEntry) bool {
		return entry.Name() == name
	}
}
