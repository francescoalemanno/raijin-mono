---
name: make-tool
description: "Creates or updates custom tools for Raijin. Use when asked to create, add, write, or modify a custom tool."
hide-from-llm: true
---

<purpose>
Guide creation and editing of Raijin custom tools — executable scripts that extend Raijin via the tool protocol.
</purpose>

<instructions>

## Tool Protocol

A custom tool is any executable file that implements two modes:

### 1. Info mode: `./my-tool --info`

Print a JSON object describing the tool to stdout, then exit 0:

```json
{
  "name": "tool-name",
  "description": "What this tool does",
  "parameters": {
    "param1": { "type": "string", "description": "First parameter" },
    "count":  { "type": "integer", "description": "How many items" }
  },
  "required": ["param1"]
}
```

**Schema rules:**
- `name` (required): lowercase identifier, no spaces.
- `description` (required): clear sentence explaining what the tool does.
- `parameters`: map of parameter name → JSON Schema object. Supported types: `string`, `integer`, `number`, `boolean`, `array`, `object`.
- `required`: list of parameter names that must be provided.

### 2. Run mode: `./my-tool` (with JSON on stdin)

Read a JSON object from stdin matching the declared parameters, execute the tool logic, and print the result as plain text to stdout. Exit 0 on success.

On failure, print an error message to stderr and exit non-zero.

## Tool Locations

Custom tools are discovered from two directories:

| Scope   | Path                                       |
|---------|--------------------------------------------|
| Global  | `{{USER_TOOLS_DIR}}/`                      |
| Project | `{{PROJECT_TOOLS_DIR}}/` (relative to working directory) |

Project tools take precedence if names collide with global ones.

## Step-by-step Workflow

### Creating a new tool

1. **Clarify** what the tool should do and what parameters it needs.
2. **Use the project-local directory** `{{PROJECT_TOOLS_DIR}}/` unless the user explicitly asks for a global tool (`{{USER_TOOLS_DIR}}/`).
3. **Create the directory** if it does not exist.
4. **Write the tool script.** Use Python 3 (`#!/usr/bin/env python3`) unless the user specifies another language.
5. **Make it executable:** `chmod +x <path>`.
6. **Test the tool** by running:
   - `<path> --info` — verify valid JSON with name, description, and parameters.
   - `echo '{"param":"value"}' | <path>` — verify it produces output.
7. **Report the result** to the user: tool name, location, and how to use it.

### Updating an existing tool

1. **Locate the tool file** — search `{{PROJECT_TOOLS_DIR}}/` then `{{USER_TOOLS_DIR}}/`.
2. **Read the current file** before making any changes.
3. **Apply the minimal diff** required to satisfy the request.
4. **Re-test** both `--info` and a sample run after editing.
5. **Report** what changed and where.

## Python Tool Template

```python
#!/usr/bin/env python3
"""Raijin custom tool: <tool-name>."""
import json
import sys


def info():
    return {
        "name": "<tool-name>",
        "description": "<What it does>",
        "parameters": {
            "param1": {"type": "string", "description": "<Description>"},
        },
        "required": ["param1"],
    }


def run(params):
    # Tool logic here
    result = params["param1"]
    return result


def main():
    if "--info" in sys.argv:
        print(json.dumps(info()))
        return

    params = json.load(sys.stdin)
    try:
        result = run(params)
        print(result)
    except Exception as e:
        print(str(e), file=sys.stderr)
        sys.exit(1)


if __name__ == "__main__":
    main()
```

## Shell Tool Template

```bash
#!/bin/sh
# Raijin custom tool: <tool-name>

if [ "$1" = "--info" ]; then
  cat <<'EOF'
{
  "name": "<tool-name>",
  "description": "<What it does>",
  "parameters": {
    "param1": { "type": "string", "description": "<Description>" }
  },
  "required": ["param1"]
}
EOF
  exit 0
fi

# Read JSON input from stdin and process it
INPUT=$(cat)
# Tool logic here
echo "Result: $INPUT"
```

</instructions>

<golden_rules>
- Always make tool files executable after creation.
- Always test both `--info` and a sample run before reporting success.
- Never create tools with empty name or description — the tool loader silently skips them.
- Prefer Python unless the user requests otherwise.
- Keep tool scripts self-contained with no external dependencies beyond the standard library, unless the user explicitly needs a library.
</golden_rules>
