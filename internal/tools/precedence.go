package tools

import (
	"github.com/francescoalemanno/raijin-mono/libagent"
	"github.com/francescoalemanno/raijin-mono/internal/core"
)

// mergeByPrecedence merges slices left-to-right, with later entries overriding
// earlier ones on name collisions. name extracts the canonical key for each element.
func mergeByPrecedence[T any](name func(T) string, levels ...[]T) []T {
	merged := make([]T, 0)
	indexByName := make(map[string]int)
	for _, level := range levels {
		for _, item := range level {
			n := core.Normalize(name(item))
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
	return merged
}

func mergeToolsByPrecedence(levels ...[]libagent.Tool) []libagent.Tool {
	return mergeByPrecedence(func(t libagent.Tool) string { return t.Info().Name }, levels...)
}

func mergePluginArtifactsByPrecedence(levels ...[]pluginArtifact) []pluginArtifact {
	return mergeByPrecedence(func(p pluginArtifact) string { return p.meta.Name }, levels...)
}
