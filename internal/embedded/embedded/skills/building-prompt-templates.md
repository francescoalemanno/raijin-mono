---
name: building-prompt-templates
description: "Creates or updates prompt templates for Raijin. Use when asked to create, add, write, or modify a prompt template."
hide-from-llm: true
---

<purpose>
Guide creation and editing of Raijin prompt templates — Markdown files that define reusable starting prompts invoked with `/template-name [args]`.
</purpose>

<instructions>

## Prompt Template Format

A prompt template is a Markdown file with optional YAML frontmatter followed by the prompt body.

### Frontmatter

```yaml
---
description: "Short sentence shown to the user when listing templates."
---
```

| Field | Required | Description |
|-------|----------|-------------|
| `description` | no | Shown when listing templates. Falls back to first non-empty line of the body. |

### Body

The body is the prompt sent to the LLM when the template is invoked. It supports:

| Placeholder | Expands to |
|-------------|------------|
| `\$ARGUMENTS` or `\$@` | All arguments passed after the template name |
| `$1`, `$2`, … | Individual positional arguments |
| `{{PROJECT_PROMPTS_DIR}}` | `{{PROJECT_PROMPTS_DIR}}` |
| `{{USER_PROMPTS_DIR}}` | `{{USER_PROMPTS_DIR}}` |
| `\{{VAR}}` | Literal `{{VAR}}` (escaped) |

### Minimal example

```markdown
---
description: Summarise a file
---
Summarise the file at `$1` in plain language. Focus on what it does, not how.
```

### Full example

```markdown
---
description: Review code for correctness and style
---
Review the code described below for correctness, style, and potential bugs.
Be concise. List issues as a numbered list, most critical first.
If there are no issues, say so explicitly.

Task: $ARGUMENTS
```

## Template Locations

| Scope | Path |
|-------|------|
| Project | `{{PROJECT_PROMPTS_DIR}}/<name>.md` |
| User (global) | `{{USER_PROMPTS_DIR}}/<name>.md` |

The filename (without `.md`) becomes the template name used in `/name` invocation.
Project templates override user templates, which override built-in templates.

## Step-by-step Workflow

### Creating a new template

1. **Clarify** what the template should do and what arguments it takes.
2. **Use the project-local directory** `{{PROJECT_PROMPTS_DIR}}/` unless the user explicitly asks for a global template (`{{USER_PROMPTS_DIR}}/`).
3. **Create the directory** if it does not exist.
4. **Write the template file** `{{PROJECT_PROMPTS_DIR}}/<name>.md`.
5. **Report** the template name and how to invoke it (`/<name> [args]`).

### Updating an existing template

1. **Locate the template file** — search `{{PROJECT_PROMPTS_DIR}}/`, `{{USER_PROMPTS_DIR}}/`, then built-in embedded templates.
2. **Read the current file** before making any changes.
3. **Apply the minimal diff** required to satisfy the request.
4. **Report** what changed and where.

</instructions>

<golden_rules>
- Keep the body prompt-shaped: write it as if you are addressing the LLM directly.
- Use `$ARGUMENTS` when the task varies per invocation. Omit it for fixed-purpose templates.

- Never add `\{{ARGUMENTS}}` (double-brace form) to the body — that is the skill substitution syntax, not the template syntax. Use `\$ARGUMENTS` instead.
</golden_rules>
