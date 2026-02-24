package substitution

import (
	"bytes"
	"context"
	"regexp"

	shellrun "github.com/francescoalemanno/raijin-mono/internal/shell"
)

// shellSubRe matches the ^~~ <command>$ syntax anywhere in text.
// The pattern must occupy an entire line: optional leading whitespace,
// then ~~, then the command.
var shellSubRe = regexp.MustCompile(`(?m)^\s*~~(.+?)$`)

// ExpandShellSubstitutions replaces every line matching ^~~ <command>$
// with "shell$ command\n<output>", truncating with the same limits as the
// bash tool. Substitution is non-recursive.
func ExpandShellSubstitutions(ctx context.Context, content string) (string, error) {
	var firstErr error
	result := shellSubRe.ReplaceAllFunc([]byte(content), func(match []byte) []byte {
		if firstErr != nil {
			return match
		}
		sub := shellSubRe.FindSubmatch(match)
		if sub == nil {
			return match
		}
		cmd := string(bytes.TrimSpace(sub[1]))
		if cmd == "" {
			return match
		}

		var buf bytes.Buffer
		cmdPath, cmdArgs := shellrun.UserShellCommand(cmd)
		_ = shellrun.Run(ctx, shellrun.ExecSpec{
			Path: cmdPath,
			Args: cmdArgs,
		}, &buf, &buf)

		return []byte("shell$ " + cmd + "\n" + buf.String())
	})
	return string(result), firstErr
}
