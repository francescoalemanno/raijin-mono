# Agent Guidelines
Go powers Raijin, an AI-powered command line assistant. Raijin provides a TUI chat interface for interacting with LLMs, with support for multiple providers, tools, and skills.

# Dependencies
- Go 1.26
- The project uses Charm libraries (charm.land/catwalk as a model list; charm.land/fantasy for LLM multiprovider support)
- Other

# Development tools to run after modifications
```bash
go test ./...
go test -race ./...
go build ./...
go test ./vetting/...
staticcheck ./...
gofumpt -l -w .
```

# Code Organization
- **cmd/raijin/**: Main entry point
- **internal/**: Internal packages
- **libtui/**: TUI rendering library
- **llmbridge/**: Bridge between API providers and raijin chat

# Naming Conventions
- Follow Go standard naming: PascalCase for exported, camelCase for unexported
- Suffix interface names with "-er" (e.g., `Reader`, `Writer`)
- Name test files with the `*_test.go` pattern

# Go Guidelines
- Go 1.24+ provides generic `min` and `max` functions in the builtin package — do not reimplement them

# Testing Guidelines
- Write unit tests using standard Go testing with `*_test.go` files

# Build for Release
Run the provided build script for cross-platform builds:
```bash
./build-all.sh
```
This script creates binaries for multiple architectures in the `build/` directory.

<AMP-SPECIFIC-INSTRUCTIONS>
- Do not use subagents.
- After using oracle, ask for user feedback.
</AMP-SPECIFIC-INSTRUCTIONS>

# Implementation instructions
- Before implementing a new tool, examine the read tool and the bash tool to identify patterns to follow.
- When the user requests catalog updates:
  1. Update Catwalk to the latest version.
  2. Run catalog generation after the dependency update (example: `cd llmbridge/pkg/catalog && go generate`).

# TODOs
See [TODOS/todo.md](./TODOS/todo.md) for the current task list.
