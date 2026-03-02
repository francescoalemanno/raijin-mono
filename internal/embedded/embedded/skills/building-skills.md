---
name: building-skills
description: Creates or updates skills for Raijin
hide-from-llm: true
---

<purpose>
Guide creation and editing of Raijin skills — Markdown files that extend Raijin with reusable, callable instructions.
</purpose>

<instructions>

## Skill File Format

A skill is a Markdown file with YAML frontmatter followed by the skill body.

### Frontmatter

```yaml
---
name: my-skill-name
description: "Short sentence explaining what it does and when to use it."
llm-description: "Optional: alternate wording optimised for model-facing discovery."
---
```

| Field | Required | Description |
|-------|----------|-------------|
| `name` | yes | Lowercase, hyphen-separated. Must match the filename (built-in) or parent directory name (external). |
| `description` | yes | Human-readable explanation of WHAT it does AND WHEN to use it. Used for skill discovery unless `llm-description` is set. |
| `llm-description` | no | Overrides `description` in the model-facing system prompt only. Use when the best human-readable description differs from the best model-readable one. |
| `hide-from-llm` | no | Set to `true` to hide from automatic discovery while keeping the skill loadable. |

### Body Structure

```markdown
<purpose>
What this skill does in one sentence.
</purpose>

<instructions>
Step-by-step instructions.
</instructions>

<golden_rules>
- Non-obvious constraints and things NOT to do.
</golden_rules>
```

- `<purpose>` — one-sentence summary.
- `<instructions>` — actionable, step-by-step. Not explanations.
- `<output_format>` — expected output shape (include when the skill produces structured output).
- `<golden_rules>` — hard constraints and gotchas.

## Skill Locations

Skills are discovered from two scopes:

| Scope   | Path |
|---------|------|
| Project | `{{PROJECT_SKILLS_DIR}}/<skill-name>/{{SKILL_FILE}}` |
| User (global) | `{{USER_SKILLS_DIR}}/<skill-name>/{{SKILL_FILE}}` |

Project skills override user skills, which override built-in skills.

## Step-by-step Workflow

### Creating a new skill

1. **Clarify** what the skill should do.
2. **Use the project-local directory** `{{PROJECT_SKILLS_DIR}}/` unless the user explicitly asks for a global skill (`{{USER_SKILLS_DIR}}/`).
3. **Create the skill directory**: `mkdir -p {{PROJECT_SKILLS_DIR}}/<skill-name>/`.
4. **Write `{{SKILL_FILE}}`** inside that directory using the body structure above.
5. **Verify** the frontmatter `name` matches the directory name.
6. **Report** the skill name, location, and how to invoke it (`$<skill-name>` or via the skill tool).

### Updating an existing skill

1. **Locate the skill file** — search `{{PROJECT_SKILLS_DIR}}/`, `{{USER_SKILLS_DIR}}/`, then built-in embedded skills.
2. **Read the current file** before making any changes.
3. **Apply the minimal diff** required to satisfy the request. Do not restructure sections that are not being changed.
4. **Report** what changed and where.

## Scripts

Use scripts when a skill needs to run multi-step shell logic. Store scripts in the skill's `scripts/` directory and reference them with relative paths from the skill's location:

| Scope | Path |
|-------|------|
| Project-level | `{{PROJECT_AGENTS_DIR}}/scripts/` |
| Skill-level | `{{PROJECT_SKILLS_DIR}}/<skill-name>/scripts/` |

Put logic in `my-script.sh` inside the skill's `scripts/` directory and reference it from instructions using the bash tool with a relative path:

```markdown
<instructions>
Use the bash tool to run `scripts/my-script.sh <args>` to accomplish X.
</instructions>
```

This keeps skill files short, execution fast, and skills relocatable.

</instructions>

<golden_rules>
- The directory name MUST match the `name` field in frontmatter.
- Write instructions as commands, not descriptions.
- Define `<output_format>` whenever the skill produces structured output.
</golden_rules>
