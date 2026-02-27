package libagent

import "strings"

// ThinkingLevel is a provider-agnostic reasoning intensity.
type ThinkingLevel string

const (
	ThinkingLevelLow    ThinkingLevel = "low"
	ThinkingLevelMedium ThinkingLevel = "medium"
	ThinkingLevelHigh   ThinkingLevel = "high"
	ThinkingLevelMax    ThinkingLevel = "max"
)

// Enabled reports whether thinking is turned on.
func (l ThinkingLevel) Enabled() bool {
	return NormalizeThinkingLevel(l) != ""
}

// NormalizeThinkingLevel resolves a ThinkingLevel string into a canonical level.
func NormalizeThinkingLevel(level ThinkingLevel) ThinkingLevel {
	if normalized, ok := parseThinkingLevel(string(level)); ok {
		return normalized
	}
	return ThinkingLevelMedium
}

func parseThinkingLevel(raw string) (ThinkingLevel, bool) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", "default":
		return ThinkingLevelMedium, true
	case "none", "disabled", "false":
		return ThinkingLevelMedium, true
	case "low", "minimal":
		return ThinkingLevelLow, true
	case "medium", "on", "enabled", "true":
		return ThinkingLevelMedium, true
	case "high":
		return ThinkingLevelHigh, true
	case "max", "xhigh":
		return ThinkingLevelMax, true
	default:
		return ThinkingLevelMedium, true
	}
}
