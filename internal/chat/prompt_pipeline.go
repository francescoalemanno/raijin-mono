package chat

import (
	"context"
	"fmt"
	"strings"

	"github.com/francescoalemanno/raijin-mono/internal/input"
	"github.com/francescoalemanno/raijin-mono/internal/message"
	"github.com/francescoalemanno/raijin-mono/internal/prompts"
	"github.com/francescoalemanno/raijin-mono/internal/substitution"
	"github.com/francescoalemanno/raijin-mono/internal/tools"
)

type promptRunOptions struct {
	TemplateName string
}

type preparedPromptInput struct {
	text        string
	attachments []message.BinaryContent
}

type promptMode int

const (
	promptModeInteractive promptMode = iota
	promptModeOneShot
)

type builtinCommandCall struct {
	name   string
	args   string
	fields []string
}

type resolvedPrompt struct {
	promptText string
	opts       promptRunOptions
	builtin    *builtinCommandCall
}

func resolvePromptSubmission(ctx context.Context, raw string, mode promptMode) (resolvedPrompt, error) {
	text := strings.TrimSpace(raw)
	if text == "" {
		return resolvedPrompt{}, fmt.Errorf("empty prompt")
	}

	if !strings.HasPrefix(text, "/") {
		expanded, _ := substitution.ExpandShellSubstitutions(ctx, text)
		return resolvedPrompt{promptText: expanded}, nil
	}

	fields := strings.Fields(text)
	if len(fields) == 0 {
		return resolvedPrompt{}, fmt.Errorf("empty prompt")
	}

	cmdToken := fields[0]
	if !strings.HasPrefix(cmdToken, "/") {
		expanded, _ := substitution.ExpandShellSubstitutions(ctx, text)
		return resolvedPrompt{promptText: expanded}, nil
	}

	commandName := strings.TrimPrefix(cmdToken, "/")
	args := text[len(cmdToken):]
	if _, isBuiltin := builtinSlashCommands()[commandName]; isBuiltin {
		if mode == promptModeOneShot {
			return resolvedPrompt{}, fmt.Errorf("interactive slash command /%s is not supported in -p mode", commandName)
		}
		return resolvedPrompt{builtin: &builtinCommandCall{name: commandName, args: args, fields: fields}}, nil
	}

	result := prompts.Load()
	tmpl, found := result.Find(commandName)
	if !found {
		return resolvedPrompt{}, fmt.Errorf("unknown command: %s", commandName)
	}
	if _, reserved := builtinSlashCommands()[tmpl.Name]; reserved {
		return resolvedPrompt{}, fmt.Errorf("template /%s is reserved by a built-in command", tmpl.Name)
	}

	args = strings.TrimSpace(args)
	if args == "" && templateNeedsArguments(tmpl.Content) {
		return resolvedPrompt{}, fmt.Errorf("template /%s requires arguments", tmpl.Name)
	}

	expanded := substitution.ExpandAll(ctx, strings.TrimSpace(tmpl.Content), args, substitution.ArgModeList)
	return resolvedPrompt{
		promptText: expanded,
		opts:       promptRunOptions{TemplateName: tmpl.Name},
	}, nil
}

func preparePromptInput(raw string, paths *tools.PathRegistry) (preparedPromptInput, error) {
	text, files, err := input.ParseAndLoadResources(raw)
	if err != nil {
		return preparedPromptInput{}, err
	}

	out := preparedPromptInput{
		text:        text,
		attachments: make([]message.BinaryContent, 0, len(files)),
	}
	for _, f := range files {
		out.attachments = append(out.attachments, message.BinaryContent{
			Path:     f.Path,
			MIMEType: f.MediaType,
			Data:     f.Data,
		})
	}
	return out, nil
}

func templateNeedsArguments(content string) bool {
	for i := 0; i < len(content); i++ {
		switch content[i] {
		case '\\':
			i++
		case '$':
			if strings.HasPrefix(content[i:], "$@") || strings.HasPrefix(content[i:], "${@:") {
				return true
			}
			if i+1 < len(content) && content[i+1] >= '1' && content[i+1] <= '9' {
				return true
			}
		}
	}
	return false
}

func unescapedToken(content, token string) bool {
	for i := 0; i < len(content); i++ {
		if content[i] == '\\' {
			i++
			continue
		}
		if strings.HasPrefix(content[i:], token) {
			return true
		}
	}
	return false
}
