package vfs

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/francescoalemanno/raijin-mono/internal/embedded"
	"github.com/francescoalemanno/raijin-mono/internal/fsutil"
)

// Backend identifies the filesystem backend selected by scheme dispatch.
type Backend string

const (
	BackendOS       Backend = "os"
	BackendEmbedded Backend = "embedded"
	Scheme                  = embedded.Scheme
)

var (
	ErrNotFound    = errors.New("vfs: not found")
	ErrNotDir      = errors.New("vfs: not a directory")
	ErrIsDir       = errors.New("vfs: is a directory")
	ErrReadOnly    = errors.New("vfs: read-only")
	ErrInvalidPath = errors.New("vfs: invalid path")
)

// ResolvedPath contains scheme routing information for a user-supplied path.
type ResolvedPath struct {
	Original  string
	Backend   Backend
	Path      string // absolute OS path for os backend, embedded-relative path for embedded backend
	Qualified string // absolute OS path for os backend, embedded://... for embedded backend
}

// RelToRoot returns walkPath relative to the given resolved root.
// For OS backend this is filepath.Rel(root.Path, walkPath).
// For embedded backend, walkPath is expected to be Scheme-prefixed and is made
// relative to root.Path within the embedded namespace.
func RelToRoot(root ResolvedPath, walkPath string) (string, error) {
	if root.Backend == BackendEmbedded {
		full := strings.TrimPrefix(walkPath, Scheme)
		if root.Path == "" {
			return full, nil
		}
		return filepath.Rel(root.Path, full)
	}
	return filepath.Rel(root.Path, walkPath)
}

// FS defines virtual filesystem operations used across tools and loaders.
type FS interface {
	Resolve(path string) (ResolvedPath, error)
	Stat(path string) (fs.FileInfo, error)
	ReadFile(path string) ([]byte, error)
	ReadDir(path string) ([]fs.DirEntry, error)
	Walk(root string, fn fs.WalkDirFunc) error
	MkdirAll(path string, perm fs.FileMode) error
	WriteFile(path string, data []byte, perm fs.FileMode) error
}

// Unified routes operations to embedded assets or OS filesystem based on scheme.
type Unified struct {
	cwd string
}

func New(cwd string) *Unified {
	if strings.TrimSpace(cwd) == "" {
		cwd = "."
	}
	return &Unified{cwd: cwd}
}

func NewFromWD() *Unified {
	cwd, err := os.Getwd()
	if err != nil {
		cwd = "."
	}
	return New(cwd)
}

func IsEmbedded(p string) bool {
	return strings.HasPrefix(strings.TrimSpace(p), Scheme)
}

// Join appends path elements to root preserving embedded:// semantics.
// For embedded roots, path separators are normalized to '/'.
func Join(root string, elems ...string) string {
	if IsEmbedded(root) {
		prefix := strings.TrimSuffix(root, "/")
		if len(elems) == 0 {
			return prefix
		}
		sanitized := make([]string, 0, len(elems))
		for _, e := range elems {
			e = strings.TrimPrefix(strings.ReplaceAll(e, "\\", "/"), "/")
			if e != "" {
				sanitized = append(sanitized, e)
			}
		}
		if len(sanitized) == 0 {
			return prefix
		}
		return prefix + "/" + path.Join(sanitized...)
	}
	parts := make([]string, 0, len(elems)+1)
	parts = append(parts, root)
	parts = append(parts, elems...)
	return filepath.Join(parts...)
}

func (u *Unified) Resolve(p string) (ResolvedPath, error) {
	if strings.TrimSpace(p) == "" {
		return ResolvedPath{}, ErrInvalidPath
	}

	if IsEmbedded(p) {
		rel := strings.TrimPrefix(strings.TrimSpace(p), Scheme)
		rel = strings.TrimSpace(strings.ReplaceAll(rel, "\\", "/"))
		if hasParentTraversal(rel) {
			return ResolvedPath{}, ErrInvalidPath
		}
		rel = path.Clean(rel)
		if rel == "." {
			rel = ""
		}
		qualified := Scheme + rel
		if rel == "" {
			qualified = Scheme
		}
		return ResolvedPath{
			Original:  p,
			Backend:   BackendEmbedded,
			Path:      rel,
			Qualified: qualified,
		}, nil
	}

	resolved := filepath.Clean(fsutil.ResolveToCwd(p, u.cwd))
	return ResolvedPath{
		Original:  p,
		Backend:   BackendOS,
		Path:      resolved,
		Qualified: resolved,
	}, nil
}

func (u *Unified) Stat(p string) (fs.FileInfo, error) {
	rp, err := u.Resolve(p)
	if err != nil {
		return nil, err
	}
	if rp.Backend == BackendEmbedded {
		fi, err := fs.Stat(embedded.FS(), embeddedFullPath(rp.Path))
		return fi, normalizeError(err)
	}
	fi, err := os.Stat(rp.Path)
	return fi, normalizeError(err)
}

func (u *Unified) ReadFile(p string) ([]byte, error) {
	rp, err := u.Resolve(p)
	if err != nil {
		return nil, err
	}
	if rp.Backend == BackendEmbedded {
		data, err := fs.ReadFile(embedded.FS(), embeddedFullPath(rp.Path))
		return data, normalizeError(err)
	}
	data, err := os.ReadFile(rp.Path)
	return data, normalizeError(err)
}

func (u *Unified) ReadDir(p string) ([]fs.DirEntry, error) {
	rp, err := u.Resolve(p)
	if err != nil {
		return nil, err
	}
	if rp.Backend == BackendEmbedded {
		entries, err := fs.ReadDir(embedded.FS(), embeddedFullPath(rp.Path))
		return entries, normalizeError(err)
	}
	entries, err := os.ReadDir(rp.Path)
	return entries, normalizeError(err)
}

func (u *Unified) Walk(root string, fn fs.WalkDirFunc) error {
	rp, err := u.Resolve(root)
	if err != nil {
		return err
	}
	if rp.Backend == BackendEmbedded {
		embRoot := embeddedFullPath(rp.Path)
		return normalizeError(fs.WalkDir(embedded.FS(), embRoot, func(p string, d fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			rel := strings.TrimPrefix(strings.TrimPrefix(p, embedded.Root()), "/")
			if rel == "" {
				return fn(Scheme, d, nil)
			}
			return fn(Scheme+rel, d, nil)
		}))
	}
	return normalizeError(filepath.WalkDir(rp.Path, fn))
}

func (u *Unified) MkdirAll(p string, perm fs.FileMode) error {
	rp, err := u.Resolve(p)
	if err != nil {
		return err
	}
	if rp.Backend == BackendEmbedded {
		return ErrReadOnly
	}
	return normalizeError(os.MkdirAll(rp.Path, perm))
}

func (u *Unified) WriteFile(p string, data []byte, perm fs.FileMode) error {
	rp, err := u.Resolve(p)
	if err != nil {
		return err
	}
	if rp.Backend == BackendEmbedded {
		return ErrReadOnly
	}
	return normalizeError(os.WriteFile(rp.Path, data, perm))
}

func embeddedFullPath(rel string) string {
	return path.Join(embedded.Root(), rel)
}

func hasParentTraversal(raw string) bool {
	if raw == "" {
		return false
	}
	for _, seg := range strings.Split(strings.ReplaceAll(raw, "\\", "/"), "/") {
		if seg == ".." {
			return true
		}
	}
	return false
}

func normalizeError(err error) error {
	if err == nil {
		return nil
	}
	switch {
	case errors.Is(err, fs.ErrNotExist), os.IsNotExist(err):
		return fmt.Errorf("%w: %v", ErrNotFound, err)
	case errors.Is(err, fs.ErrInvalid):
		return fmt.Errorf("%w: %v", ErrInvalidPath, err)
	case errors.Is(err, syscall.ENOTDIR):
		return fmt.Errorf("%w: %v", ErrNotDir, err)
	case errors.Is(err, syscall.EISDIR):
		return fmt.Errorf("%w: %v", ErrIsDir, err)
	default:
		return err
	}
}

// DescribeAccessError converts normalized VFS errors into user-facing messages.
func DescribeAccessError(path string, err error) string {
	if err == nil {
		return ""
	}
	switch {
	case errors.Is(err, ErrNotFound):
		if IsEmbedded(path) {
			return fmt.Sprintf("embedded path not found: %s", path)
		}
		return fmt.Sprintf("Path not found: %s", path)
	case errors.Is(err, ErrReadOnly):
		return "embedded paths are read-only"
	case errors.Is(err, ErrInvalidPath):
		return fmt.Sprintf("invalid path: %s", path)
	default:
		return fmt.Sprintf("accessing path: %s", err)
	}
}
