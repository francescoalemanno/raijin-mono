package core

import (
	"sort"
	"strings"

	"github.com/francescoalemanno/raijin-mono/llmbridge/pkg/llm"
)

// Normalize trims and lowercases a tool name.
func Normalize(name string) string {
	return strings.ToLower(strings.TrimSpace(name))
}

// Dedupe returns unique normalized names preserving first-seen order.
func Dedupe(names []string) []string {
	if len(names) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(names))
	out := make([]string, 0, len(names))
	for _, name := range names {
		normalized := Normalize(name)
		if normalized == "" {
			continue
		}
		if _, ok := seen[normalized]; ok {
			continue
		}
		seen[normalized] = struct{}{}
		out = append(out, normalized)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// DedupeSorted returns unique normalized names in lexicographic order.
func DedupeSorted(names []string) []string {
	out := Dedupe(names)
	sort.Strings(out)
	return out
}

// FilterUnknown returns the elements of allowed whose normalized name does not
// match any tool in available. The result is sorted for deterministic output.
func FilterUnknown(allowed []string, available []llm.Tool) []string {
	if len(allowed) == 0 || len(available) == 0 {
		return nil
	}
	known := make(map[string]struct{}, len(available))
	for _, t := range available {
		known[Normalize(t.Info().Name)] = struct{}{}
	}
	var unknown []string
	for _, name := range allowed {
		if _, ok := known[Normalize(name)]; !ok {
			unknown = append(unknown, name)
		}
	}
	sort.Strings(unknown)
	return unknown
}
