package artifacts

import (
	"fmt"
	"slices"
	"sort"
	"strings"
	"sync"
)

// Source identifies where an artifact originated from.
type Source string

const (
	SourceEmbedded Source = "embedded"
	SourceUser     Source = "user"
	SourceProject  Source = "project"
)

// Merge combines layers left-to-right with later layers winning on name
// collisions. name extracts the canonical key for each element.
// The result is sorted by name.
func Merge[T any](name func(T) string, layers ...[]T) []T {
	merged := make([]T, 0)
	indexByName := make(map[string]int)
	for _, layer := range layers {
		for _, item := range layer {
			n := strings.ToLower(strings.TrimSpace(name(item)))
			if n == "" {
				continue
			}
			if idx, ok := indexByName[n]; ok {
				merged[idx] = item
				continue
			}
			indexByName[n] = len(merged)
			merged = append(merged, item)
		}
	}
	sort.Slice(merged, func(i, j int) bool {
		return name(merged[i]) < name(merged[j])
	})
	return merged
}

// Kind identifies a cached artifact category.
type Kind string

const (
	KindSkill    Kind = "skill"
	KindPrompt   Kind = "prompt"
	KindSubagent Kind = "subagent"
	KindContext  Kind = "context"
	KindTools    Kind = "tools"
)

// Item is a cached artifact entry.
type Item struct {
	Kind  Kind
	Name  string
	Value any
}

// Loader returns fresh artifacts for a kind.
type Loader func() ([]Item, error)

// Manager stores registered loaders and an in-memory artifact cache.
type Manager struct {
	mu      sync.RWMutex
	loaders map[Kind][]Loader
	cache   map[Kind][]Item
	loaded  bool
}

// NewManager creates an empty artifact manager.
func NewManager() *Manager {
	return &Manager{
		loaders: make(map[Kind][]Loader),
		cache:   make(map[Kind][]Item),
	}
}

// Register adds a loader for a kind.
func (m *Manager) Register(kind Kind, loader Loader) {
	if loader == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.loaders[kind] = append(m.loaders[kind], loader)
	// A new loader means current cache may be incomplete.
	m.loaded = false
	m.cache = make(map[Kind][]Item)
}

// Load populates cache lazily on first use.
//
// Cache invalidation is explicit: callers should use Reload when they need to
// refresh artifacts after environmental changes (cwd/config/home, file edits, etc.).
func (m *Manager) Load() error {
	m.mu.RLock()
	loaded := m.loaded
	m.mu.RUnlock()
	if loaded {
		return nil
	}
	return m.Reload()
}

// Reload forces a fresh load from all registered loaders.
func (m *Manager) Reload() error {
	m.mu.RLock()
	loaders := make(map[Kind][]Loader, len(m.loaders))
	for kind, kindLoaders := range m.loaders {
		copied := make([]Loader, len(kindLoaders))
		copy(copied, kindLoaders)
		loaders[kind] = copied
	}
	m.mu.RUnlock()

	next := make(map[Kind][]Item, len(loaders))
	kinds := make([]Kind, 0, len(loaders))
	for kind := range loaders {
		kinds = append(kinds, kind)
	}
	slices.Sort(kinds)

	for _, kind := range kinds {
		for _, loader := range loaders[kind] {
			items, err := loader()
			if err != nil {
				return fmt.Errorf("load %s artifacts: %w", kind, err)
			}
			for _, item := range items {
				if item.Kind == "" {
					item.Kind = kind
				}
				next[kind] = append(next[kind], item)
			}
		}
	}

	m.mu.Lock()
	m.cache = next
	m.loaded = true
	m.mu.Unlock()
	return nil
}

// GetAll returns cached artifacts for a kind.
func (m *Manager) GetAll(kind Kind) []Item {
	_ = m.Load()

	m.mu.RLock()
	defer m.mu.RUnlock()
	items := m.cache[kind]
	out := make([]Item, len(items))
	copy(out, items)
	return out
}

var defaultManager = NewManager()

// RegisterLoader registers a loader on the default manager.
func RegisterLoader(kind Kind, loader Loader) {
	defaultManager.Register(kind, loader)
}

// Load ensures all artifacts are cached on first use.
func Load() error {
	return defaultManager.Load()
}

// Reload refreshes cached artifacts from disk and embedded sources.
func Reload() error {
	return defaultManager.Reload()
}

// GetAll returns cached artifacts by kind from the default manager.
func GetAll(kind Kind) []Item {
	return defaultManager.GetAll(kind)
}

// GetAllTyped filters artifacts by value type and returns typed values.
func GetAllTyped[T any](kind Kind) []T {
	items := GetAll(kind)
	out := make([]T, 0, len(items))
	for _, item := range items {
		value, ok := item.Value.(T)
		if !ok {
			continue
		}
		out = append(out, value)
	}
	return out
}
