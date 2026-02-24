package chat

import (
	"fmt"
	"strings"
	"sync"
)

type commandHelp struct {
	Command string
	Desc    string
}

var commandNamesDescs = []commandHelp{
	{Command: "/help", Desc: "Show this help message"},
	{Command: "/new", Desc: "Start a new conversation"},
	{Command: "/sessions", Desc: "Browse and resume a previous session"},
	{Command: "/fork", Desc: "Fork from a previous user message and edit it before resubmitting"},
	{Command: "/compact", Desc: "Summarize old context and keep recent messages (optional instructions)"},
	{Command: "/exit", Desc: "Exit Raijin"},
	{Command: "/models", Desc: "Select a model to use"},
	{Command: "/models add", Desc: "Select and configure a model from available providers"},
	{Command: "/templates", Desc: "List available prompt templates and their sources"},
}

func helpText() string {
	var b strings.Builder
	b.WriteString("Commands:\n")
	for _, cmd := range commandNamesDescs {
		fmt.Fprintf(&b, "  %-20s %s\n", cmd.Command, cmd.Desc)
	}
	b.WriteString("\n")
	return b.String()
}

var builtinSlashCommands = sync.OnceValue(func() map[string]struct{} {
	out := make(map[string]struct{}, len(commandNamesDescs))
	for _, cmd := range commandNamesDescs {
		name := strings.TrimPrefix(strings.Fields(cmd.Command)[0], "/")
		if name != "" {
			out[name] = struct{}{}
		}
	}
	return out
})
