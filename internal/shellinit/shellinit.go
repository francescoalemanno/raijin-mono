// Package shellinit provides shell integration scripts and completion data
// for the `:` prefix shortcut.
package shellinit

import (
	"bytes"
	"embed"
	"fmt"
	"strings"
	"text/template"
	"unicode"

	"github.com/francescoalemanno/raijin-mono/internal/commands"
	fzfmatch "github.com/francescoalemanno/raijin-mono/internal/fzf"
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
	tmpl, err := template.New(filename).Parse(string(data))
	if err != nil {
		return "", fmt.Errorf("parsing init script template for %s: %w", shell, err)
	}
	model := initTemplateData{
		CommandShortcuts: validShortcutNames(commandAndTemplateNames()),
		SkillShortcuts:   validShortcutNames(skillNames()),
	}
	var rendered bytes.Buffer
	if err := tmpl.Execute(&rendered, model); err != nil {
		return "", fmt.Errorf("rendering init script for %s: %w", shell, err)
	}
	return strings.TrimRight(rendered.String(), "\n") + "\n", nil
}

// Completions returns all completable entries shown in /help, one per line.
// Slash commands and templates are returned without the leading "/".
// Skills are returned with the leading "+".
// This is meant to be called by shell completion functions via
// `raijin --completions`.
func Completions() string {
	return strings.Join(allCompletions(), "\n")
}

// Complete resolves shell completions and returns one candidate per line.
// It accepts either a token or the full current input line.
// If the active token starts with ":" (shell shortcut mode), returned
// completions preserve the ":" prefix.
func Complete(current string) string {
	token, prefix, _ := completionContext(current)
	query := completionQuery(token)

	candidates := Candidates(current)
	if query == "" {
		return strings.Join(candidates, "\n")
	}
	type rankedCandidate struct {
		value  string
		suffix string
	}
	ranked := make([]rankedCandidate, 0, len(candidates))
	for _, candidate := range candidates {
		ranked = append(ranked, rankedCandidate{
			value:  candidate,
			suffix: completionCandidateSuffix(token, prefix, candidate),
		})
	}
	matches := fzfmatch.Rank(ranked, query, func(c rankedCandidate) string { return c.suffix })
	out := make([]string, 0, len(matches))
	for _, candidate := range matches {
		out = append(out, candidate.value)
	}
	return strings.Join(out, "\n")
}

// Candidates returns the full eligible completion set for the active token.
// Unlike Complete, it does not filter by the token text, which makes it
// suitable for fuzzy ranking and fzf-driven selection.
func Candidates(current string) []string {
	token, prefix, atPromptStart := completionContext(current)
	if token == "" && !atPromptStart {
		return nil
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
		for _, name := range skillNames() {
			out = append(out, prefix+"+"+name)
		}
	case strings.HasPrefix(token, "/"):
		if !atPromptStart {
			return nil
		}
		for _, name := range commandAndTemplateNames() {
			out = append(out, prefix+"/"+name)
		}
	default:
		if !atPromptStart {
			return nil
		}
		for _, name := range commandAndTemplateNames() {
			out = append(out, prefix+"/"+name)
		}
	}
	return out
}

func allCompletions() []string {
	entries := make([]string, 0, len(commandAndTemplateNames())+len(skillNames()))
	entries = append(entries, commandAndTemplateNames()...)
	for _, name := range skillNames() {
		entries = append(entries, "+"+name)
	}
	return entries
}

func completionQuery(token string) string {
	switch {
	case strings.HasPrefix(token, "+"):
		return strings.TrimPrefix(token, "+")
	case strings.HasPrefix(token, "/"):
		return strings.TrimPrefix(token, "/")
	default:
		return token
	}
}

func completionCandidateSuffix(token, prefix, candidate string) string {
	candidate = strings.TrimPrefix(candidate, prefix)

	switch {
	case strings.HasPrefix(token, "+"):
		return strings.TrimPrefix(candidate, "+")
	case strings.HasPrefix(token, "/"):
		return strings.TrimPrefix(candidate, "/")
	default:
		return strings.TrimPrefix(candidate, "/")
	}
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

func isValidShortcutName(name string) bool {
	if strings.TrimSpace(name) == "" {
		return false
	}
	for _, r := range name {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			continue
		}
		switch r {
		case '-', '_', '.', '+':
			continue
		default:
			return false
		}
	}
	return true
}

func validShortcutNames(names []string) []string {
	out := make([]string, 0, len(names))
	for _, name := range names {
		if isValidShortcutName(name) {
			out = append(out, name)
		}
	}
	return out
}

type initTemplateData struct {
	CommandShortcuts []string
	SkillShortcuts   []string
}
