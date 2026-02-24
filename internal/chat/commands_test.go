package chat

import (
	"strings"
	"testing"
)

func TestCommandsAreNotParameterized(t *testing.T) {
	t.Parallel()

	for _, cmd := range commandNamesDescs {
		if strings.Contains(cmd.Command, "<") || strings.Contains(cmd.Command, ">") {
			t.Fatalf("command %q is parameterized", cmd.Command)
		}
	}
}

func TestCommandsIncludeFork(t *testing.T) {
	t.Parallel()

	const want = "/fork"
	for _, cmd := range commandNamesDescs {
		if cmd.Command == want {
			return
		}
	}

	t.Fatalf("expected command %q in help list", want)
}
