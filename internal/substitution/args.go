package substitution

import (
	"strconv"
	"strings"
)

// ParseCommandArgs parses command arguments with shell-like quoting.
func ParseCommandArgs(argsString string) []string {
	var args []string
	var current strings.Builder
	var inQuote byte
	escaped := false
	tokenStarted := false

	flush := func() {
		if !tokenStarted {
			return
		}
		args = append(args, current.String())
		current.Reset()
		tokenStarted = false
	}

	for i := 0; i < len(argsString); i++ {
		ch := argsString[i]

		if escaped {
			current.WriteByte(ch)
			escaped = false
			tokenStarted = true
			continue
		}

		if inQuote != 0 {
			if ch == '\\' {
				escaped = true
				continue
			}
			if ch == inQuote {
				inQuote = 0
				tokenStarted = true
				continue
			}
			current.WriteByte(ch)
			tokenStarted = true
			continue
		}

		switch ch {
		case ' ', '\t':
			flush()
		case '\'', '"':
			inQuote = ch
			tokenStarted = true
		case '\\':
			escaped = true
			tokenStarted = true
		default:
			current.WriteByte(ch)
			tokenStarted = true
		}
	}

	if escaped {
		current.WriteByte('\\')
		tokenStarted = true
	}
	flush()
	return args
}

// ExpandArgRefsFromList expands shell-like argument references from a parsed arg list:
// $1, $2, ..., $@, ${@:N}, ${@:N:L}.
// Replacement is non-recursive.
func ExpandArgRefsFromList(content string, args []string) string {
	return substituteArgRefs(content, argRefOptions{
		allArgs:          strings.Join(args, " "),
		args:             args,
		allowPositional:  true,
		allowSliceSyntax: true,
	})
}

// ExpandArgRefsFromText expands only $@ from a raw arguments string.
// It intentionally leaves $1/$2/... and ${@:...} unchanged.
func ExpandArgRefsFromText(content string, arguments string) string {
	return substituteArgRefs(content, argRefOptions{
		allArgs: arguments,
	})
}

type argRefOptions struct {
	allArgs          string
	args             []string
	allowPositional  bool
	allowSliceSyntax bool
}

func substituteArgRefs(content string, opts argRefOptions) string {
	var out strings.Builder

	for i := 0; i < len(content); {
		if content[i] == '\\' && i+1 < len(content) && content[i+1] == '$' {
			out.WriteByte('$')
			i += 2
			continue
		}

		if content[i] != '$' {
			out.WriteByte(content[i])
			i++
			continue
		}

		if strings.HasPrefix(content[i:], "$@") {
			out.WriteString(opts.allArgs)
			i += 2
			continue
		}

		if opts.allowSliceSyntax && strings.HasPrefix(content[i:], "${@:") {
			if end := strings.IndexByte(content[i:], '}'); end > 0 {
				expr := content[i+4 : i+end]
				if sliced, ok := substituteSlice(expr, opts.args); ok {
					out.WriteString(sliced)
					i += end + 1
					continue
				}
				out.WriteString(content[i : i+end+1])
				i += end + 1
				continue
			}
		}

		if opts.allowPositional {
			j := i + 1
			for j < len(content) && content[j] >= '0' && content[j] <= '9' {
				j++
			}
			if j > i+1 {
				idx, _ := strconv.Atoi(content[i+1 : j])
				if idx > 0 && idx-1 < len(opts.args) {
					out.WriteString(opts.args[idx-1])
				}
				i = j
				continue
			}
		}

		out.WriteByte(content[i])
		i++
	}

	return out.String()
}

func substituteSlice(expr string, args []string) (string, bool) {
	expr = strings.TrimSpace(expr)
	if expr == "" {
		return "", false
	}
	parts := strings.Split(expr, ":")
	if len(parts) < 1 || len(parts) > 2 {
		return "", false
	}

	start, err := strconv.Atoi(strings.TrimSpace(parts[0]))
	if err != nil {
		return "", false
	}
	start--
	if start < 0 {
		start = 0
	}
	if start >= len(args) {
		return "", true
	}

	if len(parts) == 1 {
		return strings.Join(args[start:], " "), true
	}

	length, err := strconv.Atoi(strings.TrimSpace(parts[1]))
	if err != nil {
		return "", false
	}
	if length <= 0 {
		return "", true
	}
	end := start + length
	if end > len(args) {
		end = len(args)
	}
	return strings.Join(args[start:end], " "), true
}
