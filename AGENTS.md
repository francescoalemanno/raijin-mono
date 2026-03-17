# Raijin Development Guidelines
Raijin is an AI coding assistant for terminal workflows. It supports one-shot CLI usage, a subprocess REPL mode, built-in tools, skills, templates, and plugin tools.

## Dependencies
- Go 1.26
- Core libraries:
  - `charm.land/fantasy` (LLM multiprovider/runtime)
  - `charm.land/catwalk` (model catalog)
- Other dependencies are listed in `go.mod`.

## Development commands (run after modifications)
```bash
go test ./...
go test -race ./...
go build ./...
go test ./vetting/...
staticcheck ./...
gofumpt -l -w .
```

## Code Organization
- **main.go**: CLI entrypoint, flag parsing, mode selection (one-shot vs REPL subprocess)
- **repl.go**: subprocess-based REPL wrapper with history/completion/status line
- **internal/oneshot/**: non-interactive execution pipeline and slash command handling
- **internal/session/**: session lifecycle wiring (agent + tools + persistence)
- **internal/agent/**: session agent orchestration and system prompt construction
- **internal/tools/**: built-in tools and plugin loading
- **internal/persist/**: append-only JSONL session store with tree navigation/compaction
- **internal/skills/**, **internal/prompts/**: skill/template loading and source precedence
- **internal/artifacts/**: cache + merge layer for prompts/skills/plugins/context
- **libagent/**: provider-agnostic runtime loop, streaming/event model, tool-call execution

## Naming Conventions
- Follow Go naming conventions:
  - PascalCase for exported identifiers
  - camelCase for unexported identifiers
- Prefer interface names ending in `-er` when idiomatic (`Reader`, `Writer`, etc.)
- Test files should use `*_test.go`

## Go Guidelines
- Go 1.24+ includes generic `min` and `max` builtins — do not reimplement them.

## Testing Guidelines
- Use standard Go testing with focused unit tests near modified behavior.
- Prefer deterministic tests for persistence, tool output shaping, and event sequencing.

## Implementation Notes
- Before implementing new tools, review patterns in:
  - `internal/tools/read.go`
  - `internal/tools/bash.go`
- Prefer existing internal abstractions (`artifacts`, `vfs`, `persist`, `session`) over ad-hoc new layers.
- Keep compatibility with tree-based session history and tool-call/result coupling invariants.

## Catalog Update Procedure
When user requests catalog updates:
1. Update Catwalk to the latest version.
2. Regenerate catalog artifacts after dependency update (example):
   `cd llmbridge/pkg/catalog && go generate`

## TODOs
See [TODOS/todo.md](./TODOS/todo.md) for current tasks.
