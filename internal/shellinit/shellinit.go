// Package shellinit provides shell integration scripts and completion data
// for the `:` prefix shortcut.
package shellinit

import (
	"embed"
	"fmt"
	"strings"

	"github.com/francescoalemanno/raijin-mono/internal/commands"
	"github.com/francescoalemanno/raijin-mono/internal/prompts"
	"github.com/francescoalemanno/raijin-mono/internal/skills"
)

//go:embed scripts/*
var scriptFS embed.FS

// SupportedShells returns the list of shells that have init scripts.
func SupportedShells() []string {
	return []string{"zsh", "bash", "fish"}
}

// Init returns the shell integration script for the given shell.
func Init(shell string) (string, error) {
	var filename string
	switch shell {
	case "zsh":
		filename = "scripts/raijin.zsh"
	case "bash":
		filename = "scripts/raijin.bash"
	case "fish":
		filename = "scripts/raijin.fish"
	default:
		return "", fmt.Errorf("unsupported shell %q; supported: %s", shell, strings.Join(SupportedShells(), ", "))
	}
	data, err := scriptFS.ReadFile(filename)
	if err != nil {
		return "", fmt.Errorf("reading init script for %s: %w", shell, err)
	}
	return string(data), nil
}

// Completions returns all slash command and template names (without the "/"
// prefix), one per line. This is meant to be called by shell completion
// functions via `raijin --completions`.
func Completions() string {
	return strings.Join(commandAndTemplateNames(), "\n")
}

// Complete resolves shell completions and returns one candidate per line.
// It accepts either a token or the full current input line.
// If the active token starts with ":" (shell shortcut mode), returned
// completions preserve the ":" prefix.
func Complete(current string) string {
	token, prefix, atPromptStart := completionContext(current)
	if token == "" && !atPromptStart {
		return ""
	}

	var out []string
	switch {
	case token == "":
		for _, name := range skillNames() {
			out = append(out, prefix+"+"+name)
		}
		for _, name := range commandAndTemplateNames() {
			out = append(out, prefix+"/"+name)
		}
	case strings.HasPrefix(token, "+"):
		query := strings.TrimPrefix(token, "+")
		for _, name := range skillNames() {
			if strings.HasPrefix(name, query) {
				out = append(out, prefix+"+"+name)
			}
		}
	case strings.HasPrefix(token, "/"):
		if !atPromptStart {
			return ""
		}
		query := strings.TrimPrefix(token, "/")
		for _, name := range commandAndTemplateNames() {
			if strings.HasPrefix(name, query) {
				out = append(out, prefix+"/"+name)
			}
		}
	default:
		if !atPromptStart {
			return ""
		}
		for _, name := range commandAndTemplateNames() {
			if strings.HasPrefix(name, token) {
				out = append(out, prefix+"/"+name)
			}
		}
	}

	return strings.Join(out, "\n")
}

func completionContext(current string) (token, prefix string, atPromptStart bool) {
	trimmed := strings.TrimSpace(current)
	if trimmed == "" {
		return "", "", false
	}

	rawToken := trimmed
	if strings.ContainsAny(trimmed, " \t\r\n") {
		rawParts := strings.Fields(trimmed)
		if len(rawParts) == 0 {
			return "", "", false
		}
		rawToken = rawParts[len(rawParts)-1]
	}

	if strings.HasPrefix(rawToken, ":") {
		prefix = ":"
		rawToken = strings.TrimPrefix(rawToken, ":")
	}

	prompt := strings.TrimPrefix(trimmed, ":")
	parts := strings.Fields(prompt)
	if len(parts) == 0 {
		return rawToken, prefix, true
	}
	return rawToken, prefix, len(parts) == 1
}

func commandAndTemplateNames() []string {
	seen := make(map[string]struct{})
	var lines []string

	for _, cmd := range commands.BuiltinCommands {
		// Emit full command value without leading "/".
		name := strings.TrimPrefix(cmd.Command, "/")
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		lines = append(lines, name)
	}
	reserved := make(map[string]struct{})
	for _, cmd := range commands.BuiltinCommands {
		reserved[strings.TrimPrefix(strings.Fields(cmd.Command)[0], "/")] = struct{}{}
	}

	for _, tmpl := range prompts.GetTemplates() {
		if _, ok := reserved[tmpl.Name]; ok {
			continue
		}
		if _, ok := seen[tmpl.Name]; ok {
			continue
		}
		seen[tmpl.Name] = struct{}{}
		lines = append(lines, tmpl.Name)
	}

	return lines
}

func skillNames() []string {
	seen := make(map[string]struct{})
	var names []string
	for _, s := range skills.GetSkills() {
		name := strings.TrimSpace(s.Name)
		if name == "" {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		names = append(names, name)
	}
	return names
}
