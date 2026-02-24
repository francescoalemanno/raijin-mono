package substitution

import "context"

// ArgMode controls which argument-reference syntax is expanded.
type ArgMode int

const (
	// ArgModeNone skips argument expansion entirely.
	ArgModeNone ArgMode = iota
	// ArgModeText expands only $@ and $ARGUMENTS from a raw string.
	ArgModeText
	// ArgModeList parses args and expands $1, $@, $ARGUMENTS, ${@:N}, ${@:N:L}.
	ArgModeList
)

// ExpandAll performs non-recursive substitution in the canonical order:
//  1. Named placeholders ({{KEY}} with BracesStyle)
//  2. Argument references ($@, $1, etc. depending on ArgMode)
//  3. Shell substitutions (~~ cmd lines)
func ExpandAll(ctx context.Context, content string, argsString string, mode ArgMode) string {
	content = ReplaceNamed(content, DefaultNamedValues(argsString), BracesStyle())

	switch mode {
	case ArgModeList:
		content = ExpandArgRefsFromList(content, ParseCommandArgs(argsString))
	case ArgModeText:
		content = ExpandArgRefsFromText(content, argsString)
	}

	content, _ = ExpandShellSubstitutions(ctx, content)
	return content
}
