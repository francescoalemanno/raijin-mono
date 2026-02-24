package llm

import "strings"

// ThinkingLevel is a provider-agnostic reasoning intensity.
type ThinkingLevel string

const (
	ThinkingLevelOff    ThinkingLevel = "off"
	ThinkingLevelLow    ThinkingLevel = "low"
	ThinkingLevelMedium ThinkingLevel = "medium"
	ThinkingLevelHigh   ThinkingLevel = "high"
	ThinkingLevelMax    ThinkingLevel = "max"
)

func (l ThinkingLevel) Enabled() bool {
	return l != "" && l != ThinkingLevelOff
}

// NormalizeThinkingLevel resolves a thinking_level string into a canonical level.
func NormalizeThinkingLevel(level ThinkingLevel) ThinkingLevel {
	if normalized, ok := parseThinkingLevel(string(level)); ok {
		return normalized
	}
	return ThinkingLevelOff
}

func parseThinkingLevel(raw string) (ThinkingLevel, bool) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", "default":
		return "", false
	case "off", "none", "disabled", "false":
		return ThinkingLevelOff, true
	case "low", "minimal":
		return ThinkingLevelLow, true
	case "medium", "on", "enabled", "true":
		return ThinkingLevelMedium, true
	case "high":
		return ThinkingLevelHigh, true
	case "max", "xhigh":
		return ThinkingLevelMax, true
	default:
		return ThinkingLevelOff, true
	}
}
