package substitution

import (
	"sort"
	"strings"
)

// NamedStyle controls placeholder syntax used by ReplaceNamed.
type NamedStyle struct {
	Open   string
	Close  string
	Escape string
}

// ReplaceNamed substitutes named placeholders using the provided delimiter style.
// Substitution is non-recursive: replacement text is not re-scanned.
func ReplaceNamed(content string, values map[string]string, style NamedStyle) string {
	if content == "" || len(values) == 0 || style.Open == "" || style.Close == "" {
		return content
	}

	const (
		escapedOpenSentinel  = "\x00SUB_OPEN\x00"
		escapedCloseSentinel = "\x00SUB_CLOSE\x00"
	)

	if style.Escape != "" {
		content = strings.ReplaceAll(content, style.Escape+style.Open, escapedOpenSentinel)
		content = strings.ReplaceAll(content, style.Escape+style.Close, escapedCloseSentinel)
	}

	// Stable key order keeps behavior deterministic.
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	replacements := make([]string, 0, len(keys)*2)
	for _, key := range keys {
		replacements = append(replacements, style.Open+key+style.Close, values[key])
	}
	content = strings.NewReplacer(replacements...).Replace(content)

	if style.Escape != "" {
		content = strings.ReplaceAll(content, escapedOpenSentinel, style.Open)
		content = strings.ReplaceAll(content, escapedCloseSentinel, style.Close)
	}

	return content
}
