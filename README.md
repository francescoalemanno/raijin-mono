# Raijin

![Raijin](raijin.png)

Raijin is an AI-powered command line assistant written in Go. A coding agent that does the bare minimum.

## The Philosophy: Less Is More

Most coding agents are bloated. They come with MCP servers, a dozen integrations, permission dialogs, and safety rails that get in your way. We think that's unnecessary.

Raijin strips away the complexity. No MCP. No custom tool bloat. No hidden sub-agents. Just a model, a terminal, and eight tools.

## Why This Works

Models already understand how to code. They don't need elaborate system prompts or curated tool descriptions. Give them read, write, edit, and bash—and they'll figure out the rest.

The "agent" part doesn't need to be complex. It's just:
1. Read the conversation
2. Decide what to do
3. Call a tool
4. Repeat

That's it.

## The Toolset

Raijin has exactly what you need to act on files:

| Tool | Purpose |
|------|---------|
| **read** | Inspect files |
| **write** | Create or overwrite files |
| **edit** | Surgical in-place edits |
| **bash** | Execute commands |
| **grep** | Search file contents |
| **glob** | Find files by pattern |
| **webfetch** | Fetch web pages, convert to clean Markdown |
| **skill** | Load reusable workflows |

That's all. No MCP servers to configure. No external service dependencies. Just you, your terminal, and the model.

## Provider Support

Raijin supports almost all providers from the Charmbracelet/Fantasy library, plus ChatGPT Codex Plan and OpenCodeZen. We also improve the synthetic provider by fetching models directly from their API.

We haven't been able to test all providers personally (it takes API keys and money), but if you run into issues with a specific provider, we should be able to fix it soon enough.

## Features

- **`~~ expansion`** - Expand git/bash command output directly in your prompt to send to the LLM without copy-paste
- **`/amplify`** - Generate custom tools, skills, or prompt templates for your own use -- e.g. Search Engine tool ;)
- **Prompt templates** - Build reusable prompts for recurring tasks
- **Skills** - Callable both by the LLM and manually by the user using the $SkillName syntax (works also within prompt templates)
- **Session persistence** - Resume past sessions seamlessly
- **Compaction** - Split session history to keep recent interactions intact while compressing earlier ones
- **`/fork`** - Fork a session at any user prompt to branch from a good checkpoint

## What You Don't Get

- MCP tool support
- Permission prompts or safety rails
- Hidden sub-agents or black boxes
- Complex configuration files
- "Are you sure?" dialogs

If you need safety, run Raijin in a container. That's what we do.

## Installation

```bash
go build -o raijin ./cmd/raijin
```

## Usage

```bash
./raijin
./raijin "fix the bug in main.go"
./raijin "add unit tests for auth"
./raijin "refactor this messy function"
```

## Development

```bash
go test ./...
go build ./...
staticcheck ./...
gofumpt -l -w .
```

For cross-platform builds:
```bash
./build-all.sh
```

## Credits

A special mention goes to Mario Zechner, creator of the Pi coding agent. He's built an awesome coding agent following the same philosophy, along with excellent libraries that helped bring this project forward. The libtui TUI rendering library was ported from his TypeScript implementation to Go.
