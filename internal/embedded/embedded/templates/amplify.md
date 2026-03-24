description: Create or update a Raijin extension (skill, custom tool, or prompt template)
---
The user wants to create or modify a Raijin extension. Their request is:

$@

## Your job

1. Determine whether this is a **create** or **update** request.
2. Identify the extension type.
3. Load the corresponding skill, passing the full original request as arguments.

## Extension Types

| Type | Use when the user wants to… | Skill to load |
|------|------------------------------|---------------|
| **Skill** | Teach the LLM a reusable workflow, checklist, or recipe it follows step-by-step. No code — just structured instructions the model executes. | `make-skill` |
| **Custom tool** | Give the LLM a new callable tool: fetch data, run a script, query an API, interact with a service. Implemented as an executable. | `make-tool` |
| **Prompt template** | Create a reusable starting prompt the user invokes with `/name [args]`. Supports positional args, tool restrictions, and `{{VAR}}` substitutions. | `make-prompt` |

## Decision Rules

- The user describes **a behaviour or process** the LLM should follow → **skill**
- The user describes **a capability or action** the LLM should be able to invoke → **custom tool**
- The user describes **a prompt** they want to reuse across sessions → **prompt template**
- When in doubt between skill and prompt template: if it needs step-by-step instructions the model executes autonomously → skill; if it's a starting message the user fires manually → prompt template.
- If the request mentions an existing name, or uses words like "update", "fix", "change", "edit", "improve" → treat as **update**.

## Steps

1. Determine the extension type and whether this is a create or update.
2. Call `skill("<skill-name>", "<original user request>")` with the matching skill.
3. If the request is genuinely ambiguous, ask one clarifying question before proceeding.
