package tools

import "sync"

// PathRegistry is a thread-safe registry of extra directories to prepend to
// the PATH environment variable. Tools that execute commands (e.g. bash) read
// from it, while tools that load skills or project configuration write to it.
type PathRegistry struct {
	mu    sync.RWMutex
	paths []string
}

// NewPathRegistry creates an empty PathRegistry.
func NewPathRegistry() *PathRegistry {
	return &PathRegistry{}
}

// Add appends a directory to the registry. Duplicate paths are ignored.
func (r *PathRegistry) Add(dir string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, p := range r.paths {
		if p == dir {
			return
		}
	}
	r.paths = append(r.paths, dir)
}

// Paths returns a snapshot of all registered directories.
func (r *PathRegistry) Paths() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]string, len(r.paths))
	copy(out, r.paths)
	return out
}
