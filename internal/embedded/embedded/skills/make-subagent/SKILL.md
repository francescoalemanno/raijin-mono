---
name: make-subagent
description: "Creates or updates subagent profiles for Raijin. Use when asked to create, add, write, or modify a subagent."
hide-from-llm: true
---

<purpose>
Guide creation and editing of Raijin subagent profiles — Markdown files that define specialized agent personas invoked via `%subagent-name query`.
</purpose>

<instructions>

## Subagent Profile Format

A subagent is a Markdown file with YAML frontmatter followed by the persona definition.

### Frontmatter

```yaml
---
description: "When to invoke this subagent — for the main LLM's routing decision"
tools:
  - read
  - grep
  - glob
---
```

| Field | Required | Description |
|-------|----------|-------------|
| `description` | yes | **For the main LLM's routing decision** — describe WHEN to delegate to this subagent, not what the subagent does. The main LLM reads this to decide if your request matches this subagent's specialty. Be specific about the trigger conditions. |
| `tools` | no | List of tool names the subagent is allowed to use. Omit to inherit all tools, or specify a restricted set. |

### Body

The body defines the subagent's persona, role, capabilities, behavior, and constraints. Write it as a system prompt that shapes how the subagent responds.

Structure it with clear sections:

```markdown
Role:
- What this subagent specializes in

Capabilities:
- What it can do

Behavior:
- How it should act and respond

Constraints:
- What it should NOT do
- Any limitations
```

Guidelines for the body:
- Be specific about the subagent's expertise and focus
- Define clear boundaries (what it handles vs. what it escalates)
- Keep it concise but complete
- The subagent works in the current workspace — mention this if relevant
- Subagents should not mention the parent agent

### Minimal example

```markdown
---
description: "Delegate when the user asks for security review, vulnerability analysis, or safety audit of code"
tools:
  - read
  - grep
---
You are a security-focused code reviewer.

Role:
- Identify security vulnerabilities and unsafe patterns

Capabilities:
- Analyze code for common security issues (injection, auth, secrets, etc.)
- Suggest specific fixes with code examples

Behavior:
- Be direct and specific
- Cite file paths and line numbers
- Prioritize by severity

Constraints:
- Focus only on security, not general style
- Do not modify files — only report findings
```

### Full example

```markdown
---
description: "Delegate when the user needs strategic advice: architecture decisions, complex debugging, code review, or engineering guidance"
tools:
  - glob
  - grep
  - read
---
You are Oracle, a strategic technical advisor.

Role:
- High-IQ debugging
- Architecture decisions
- Code review
- Engineering guidance

Capabilities:
- Analyze complex codebases and identify plausible root causes
- Propose architectural solutions with explicit tradeoffs
- Review code for correctness, performance, and maintainability
- Guide debugging when standard approaches fail

Behavior:
- Be direct and concise
- Provide actionable recommendations
- Explain reasoning briefly
- Acknowledge uncertainty when present

Constraints:
- READ-ONLY: advise, do not implement
- Focus on strategy, not execution
- Point to specific files and lines when relevant

Work directly in the current workspace. Use the available read-only tools to inspect the codebase before answering when the task depends on repository context.

Answer only the requested advisory task. Do not mention the parent agent.
```

## Subagent Locations

Subagents are discovered from two scopes:

| Scope   | Path |
|---------|------|
| Project | `{{PROJECT_AGENTS_DIR}}/subagents/<name>.md` |
| User (global) | `{{USER_AGENTS_DIR}}/subagents/<name>.md` |

The filename (without `.md`) becomes the subagent name used in `%name` invocation.
Project subagents override user subagents, which override built-in subagents.

## Step-by-step Workflow

### Creating a new subagent

1. **Clarify** what the subagent should do, its expertise, and scope.
2. **Determine tool restrictions** — should it have limited tools (e.g., read-only) or full access?
3. **Use the project-local directory** `{{PROJECT_AGENTS_DIR}}/subagents/` unless the user explicitly asks for a global subagent (`{{USER_AGENTS_DIR}}/subagents/`).
4. **Create the directory** if it does not exist.
5. **Write the subagent file** `<name>.md` using the format above.
6. **Report** the subagent name, location, and how to invoke it (`%<name> query`).

### Updating an existing subagent

1. **Locate the subagent file** — search `{{PROJECT_AGENTS_DIR}}/subagents/`, `{{USER_AGENTS_DIR}}/subagents/`, then built-in embedded subagents.
2. **Read the current file** before making any changes.
3. **Apply the minimal diff** required to satisfy the request. Do not restructure sections that are not being changed.
4. **Report** what changed and where.

## Common Subagent Patterns

| Purpose | Tool Set | Key Constraints |
|---------|----------|-----------------|
| Code exploration/discovery | `read`, `grep`, `glob` | Report findings only |
| Architecture review | `read`, `grep`, `glob` | Read-only, advisory only |
| Security audit | `read`, `grep` | Report only, cite specific lines |
| Testing strategy | `read`, `glob` | Suggest approaches, don't write tests |
| Documentation review | `read` | Check completeness and clarity |

</instructions>

<golden_rules>
- The filename (without `.md`) MUST match how users will invoke it (`%name`).
- Always include a `description` in frontmatter — it's shown in subagent listings.
- Tool restrictions are optional but recommended for focused subagents.
- The body should read like a system prompt — direct instructions to shape behavior.
- Subagents should never mention the parent agent or their own nature as a subagent.
- Keep the persona focused — one primary responsibility per subagent.
</golden_rules>
