package commands

import (
	"fmt"
	"strings"
	"sync"
)

// Command describes a supported slash command.
type Command struct {
	Command string
	Desc    string
}

var BuiltinCommands = []Command{
	{Command: "/help", Desc: "Show this help message"},
	{Command: "/new", Desc: "Start a new conversation"},
	{Command: "/exit", Desc: "Exit Raijin"},
	{Command: "/models", Desc: "Select a model to use"},
	{Command: "/add-model", Desc: "Select and configure a model from available providers"},
	{Command: "/setup", Desc: "Automatically configure shell integration and first model"},
	{Command: "/sessions", Desc: "Browse and resume a previous session"},
	{Command: "/tree", Desc: "Navigate the session tree and branch from any previous message"},
	{Command: "/history", Desc: "Replay all assistant output from the active session"},
	{Command: "/compact", Desc: "Summarize old context and keep recent messages (optional instructions)"},
	{Command: "/status", Desc: "Show current model, reasoning, and context fill percentage"},
	{Command: "/reasoning", Desc: "Select reasoning level for the default model"},
	{Command: "/edit", Desc: "Open an editor and send the saved content as a prompt"},
}

func HelpText() string {
	var b strings.Builder
	b.WriteString("Commands:\n")
	for _, cmd := range BuiltinCommands {
		fmt.Fprintf(&b, "  %-20s %s\n", cmd.Command, cmd.Desc)
	}
	b.WriteString("\n")
	return b.String()
}

var builtinSlashCommands = sync.OnceValue(func() map[string]struct{} {
	out := make(map[string]struct{}, len(BuiltinCommands))
	for _, cmd := range BuiltinCommands {
		name := strings.TrimPrefix(strings.Fields(cmd.Command)[0], "/")
		if name != "" {
			out[name] = struct{}{}
		}
	}
	return out
})

func IsBuiltin(name string) bool {
	_, ok := builtinSlashCommands()[name]
	return ok
}
