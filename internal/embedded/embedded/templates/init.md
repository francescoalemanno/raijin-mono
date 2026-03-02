---
description: Generates or updates AGENTS.MD for this repository.
---

<purpose>
Analyze this codebase and create/update AGENTS.MD to help future agents work effectively in this repository.
</purpose>

<precondition>
Check if directory is empty or contains only config files. If so, stop and say "Directory appears empty or only contains config. Add source code first, then run this command to generate AGENTS.MD."
</precondition>

<discovery_process>
1. Check directory contents with `read('.')`
2. Identify project type from config files and directory structure
3. Find build/test/lint commands from config files, scripts, Makefiles, or CI configs
4. Read representative source files to understand code patterns and coding preferences
5. If AGENTS.MD exists, read and improve it
</discovery_process>

<content_to_include>
- Essential commands (build, test, run, deploy, etc.) - whatever is relevant for this project
- Code organization and structure
- Naming conventions and style patterns
- Testing approach and patterns
- Important gotchas or non-obvious patterns
</content_to_include>

<output_format>
Clear markdown sections. Use your judgment on structure based on what you find. Aim for completeness over brevity - include everything an agent would need to know.
</output_format>

<golden_rule>
Only document what you actually observe. Never invent commands, patterns, or conventions. If you can't find something, don't include it.
</golden_rule>

{{ARGUMENTS}}