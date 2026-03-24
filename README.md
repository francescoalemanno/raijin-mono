# Raijin

![Raijin](raijin.png)

Raijin is an AI coding assistant that lives in your terminal.

If you like fast iteration, direct control, and minimal ceremony, this is for you.

## Why Raijin

Most coding agents add layers: external servers, extra approvals, complex setup, hidden workflows.
Raijin keeps the loop short:

- one terminal UI
- one active model
- a small toolset
- explicit commands

No MCP. No hidden sub-agents. No permission popups.

## Built-in tools

| Tool | Purpose |
|------|---------|
| `read` | Inspect files and load skills |
| `write` | Create or overwrite files |
| `edit` | Surgical in-place edits |
| `bash` | Execute shell commands |
| `grep` | Search file contents |
| `glob` | Find files by pattern |

## What you can do right away

- Ask directly in the TUI
- Start with one-shot CLI prompts
- Attach files with `@path` (text and images)
- Inject shell output with `~~ command` without copy-paste
- Load skills inline with `+skill-name`
- Use reusable prompt templates with `/template-name args` (for example `/amplify add a tool that checks Jira status`)
- Fork a conversation from any previous user prompt with `/tree`
- Resume old chats with `/sessions`
- Compact long history with `/compact` or let Raijin auto-compact once the session reaches 60% estimated context fill or 150k estimated tokens, targeting roughly 20% usage after compaction

Raijin runs in one-shot CLI mode and supports prompt features such as attachments, skills, shell substitution, and templates.

## Slash commands

- `/help` - show command help
- `/new` - start a fresh conversation
- `/models` - switch model
- `/add-model` - add and configure models/providers
- `/setup [zsh|bash|fish]` - auto-configure shell integration and first model (shell autodetected when omitted)
- `/sessions` - browse and resume prior sessions
- `/tree` - browse conversation history and fork at any safe boundary
- `/history` - replay all assistant output from the active session
- `/compact [instructions]` - summarize old context, keep recent context
- `/status` - show current model, reasoning, and context fill percentage
- `/reasoning [low|medium|high|max]` - select reasoning level (interactive when omitted)
- `/edit` - open `$EDITOR` (or fallback editors), then send saved file content as prompt
- `/exit` - quit

## Keyboard shortcuts

- `Ctrl+P` - open model selector
- `Ctrl+O` - expand/collapse tool output blocks
- `Ctrl+T` or `Shift+Tab` - cycle thinking level
- `Ctrl+C` or `Esc` - interrupt current run
- `Ctrl+D` - quit

## Prompt templates and skills

Raijin supports three template/skill layers with precedence:

1. Project
2. User
3. Embedded defaults

You can:

- invoke templates as slash commands
- pass template args (`$@`, `$1`, `${@:2}`)
- call skills from prompts via `+skill-name`

Built-in templates include:

- `/amplify` - generate or update Raijin extensions (skills, custom tools, prompt templates, subagents)
- `/init` - generate or refresh an `AGENTS.md` for the current repository

## Custom extensions

You can extend Raijin without changing core code:

- Custom tools from `.agents/tools` (project) or user tools directory
- Skills from `.agents/skills` (project) or user skill directory
- Prompt templates from `.agents/prompts` (project) or user prompt directory

## Installation

Install the latest release with a single command:

```sh
curl -fsSL https://raw.githubusercontent.com/francescoalemanno/raijin-mono/main/scripts/install.sh | sh
```

This downloads the latest matching prebuilt release archive, installs `raijin` to `~/.local/bin`, and adds it to your `PATH` automatically.

Prebuilt archives are published for macOS, Linux, and Windows on `amd64` and `arm64`.

Or build from source:

```bash
go build -o raijin .
```

## Usage

```bash
raijin "fix the bug in main.go"
raijin "add unit tests for auth"
raijin "refactor this messy function"
raijin "summarize TODOs and propose a plan"
```

### Live profiling

Raijin can capture full-session Go profiling artifacts while you run prompts.

```bash
raijin -profile-dir ./profiles
```

This creates a timestamped folder like `./profiles/raijin-profile-YYYYMMDD-HHMMSS` containing:

- `cpu.pprof` (continuous CPU profile for the full session)
- `trace.out` (runtime trace for the full session)
- `heap.pprof`, `goroutine.pprof`, `block.pprof`, `mutex.pprof` (end-of-session snapshots)
- `memstats.json`

For live, on-demand pprof capture during a session:

```bash
raijin -profile-dir ./profiles -pprof-addr 127.0.0.1:6060
```

Then inspect bottlenecks with:

```bash
go tool pprof -http=:0 ./raijin ./profiles/raijin-profile-*/cpu.pprof
go tool trace ./profiles/raijin-profile-*/trace.out
go tool pprof -http=:0 ./raijin http://127.0.0.1:6060/debug/pprof/profile?seconds=30
go tool pprof -http=:0 ./raijin http://127.0.0.1:6060/debug/pprof/heap
```

## Development

```bash
go test ./...
go test -race ./...
go build ./...
go test ./vetting/...
staticcheck ./...
gofumpt -l -w .
```

## Credits

Special mention to Mario Zechner, creator of Pi. Raijin shares a similar philosophy, and the TUI foundation is ported from his TypeScript work.
