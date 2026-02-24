package frontmatter

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/goccy/go-yaml"
)

// Header holds parsed frontmatter values by normalized key.
// Repeated keys and list items are accumulated in insertion order.
type Header map[string][]string

// Parse reads a simple YAML frontmatter block delimited by --- lines.
// It returns parsed header values, body content, and whether frontmatter was found.
func Parse(content string) (Header, string, bool) {
	lines := strings.Split(content, "\n")
	if len(lines) == 0 || strings.TrimRight(lines[0], "\r") != "---" {
		return nil, content, false
	}

	end := -1
	for i := 1; i < len(lines); i++ {
		if strings.TrimRight(lines[i], "\r") == "---" {
			end = i
			break
		}
	}
	if end == -1 {
		return nil, content, false
	}

	yamlBlock := strings.Join(lines[1:end], "\n")

	var raw map[string]any
	if err := yaml.Unmarshal([]byte(yamlBlock), &raw); err != nil || raw == nil {
		return nil, content, false
	}

	header := make(Header)
	for k, v := range raw {
		key := normalizeKey(k)
		switch val := v.(type) {
		case []any:
			for _, item := range val {
				if s, ok := anyToString(item); ok {
					header[key] = append(header[key], s)
				}
			}
		default:
			if s, ok := anyToString(v); ok {
				header[key] = append(header[key], s)
			}
		}
	}

	body := strings.Join(lines[end+1:], "\n")
	body = strings.TrimPrefix(body, "\n")
	return header, body, true
}

func anyToString(v any) (string, bool) {
	if v == nil {
		return "", false
	}
	if s, ok := v.(string); ok {
		return s, true
	}
	return fmt.Sprint(v), true
}

// Values returns all values associated with key.
func Values(header Header, key string) []string {
	if len(header) == 0 {
		return nil
	}
	return header[normalizeKey(key)]
}

// FirstValue returns the first value associated with key.
func FirstValue(header Header, key string) string {
	values := Values(header, key)
	if len(values) == 0 {
		return ""
	}
	return strings.TrimSpace(values[0])
}

// FirstValueFrom returns the first non-empty value for any key in order.
func FirstValueFrom(header Header, keys ...string) string {
	for _, key := range keys {
		value := strings.TrimSpace(FirstValue(header, key))
		if value != "" {
			return value
		}
	}
	return ""
}

// StripOptionalQuotes removes a matching outer pair of single or double quotes,
// unescaping Go-style escape sequences for double-quoted strings.
func StripOptionalQuotes(s string) string {
	s = strings.TrimSpace(s)
	if len(s) < 2 {
		return s
	}
	if (s[0] == '"' && s[len(s)-1] == '"') || (s[0] == '\'' && s[len(s)-1] == '\'') {
		unquoted, err := strconv.Unquote(string(s[0]) + s[1:len(s)-1] + string(s[0]))
		if err == nil {
			return unquoted
		}
		return s[1 : len(s)-1]
	}
	return s
}

// FirstNonEmptyLine returns the first non-blank line in content.
func FirstNonEmptyLine(content string) string {
	for _, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func normalizeKey(key string) string {
	return strings.ToLower(strings.TrimSpace(key))
}
