---
name: commit
description: Commits git changes by appropriately breaking them into atomic units.
llm-description: Use anytime the user asks to commit changes to the current repository.
---

<purpose>
Make small, atomic commits with clear messages following Conventional Commits v1.0.
</purpose>

<workflow>
1. If you don't already understand the changes, review them first using the dedicated tools:

```bash
git -P diff --stat
git -P status
git -P diff
```
If needed run other batched more specific requests.

2. Make small, atomic commits—each commit should address one logical change. If your work spans multiple concerns (e.g., a refactor and a bug fix), break it into separate commits.
3. Follow this strict loop for each logical change:

```bash
# Stage only the files for the FIRST logical change
git add <files-for-first-commit>
# Commit those files ONLY
git commit -m "type(scope): description" -m "body"
# Now stage files for the SECOND logical change
git add <files-for-second-commit>
# Commit those files ONLY
git commit -m "type(scope): description" -m "body"
```

IMPORTANT: Never stage all files at once before committing. Always follow the protocol.

For finer control, stage specific hunks:
```bash
git hunks list                            # List all hunks with IDs
git hunks add 'file:@-old,len+new,len'    # Stage specific hunks by ID
```
</workflow>

<commit_message_format>
Follow [Conventional Commits v1.0](https://www.conventionalcommits.org/en/v1.0.0/).

**Title (first line):**
- Format: `<type>[optional scope]: <description>`
- Types: `feat`, `fix`, `docs`, `style`, `refactor`, `perf`, `test`, `build`, `ci`, `chore`
- Limit to 60 characters maximum
- Use lowercase except for symbols or acronyms
- Use imperative mood ("add feature" not "adds feature")
- Add `!` after type/scope for breaking changes: `feat!: remove deprecated API`

**Body:**
- Explain what the change does and why
- Use proper grammar and punctuation
- Use imperative mood throughout

**Trailers:**
- If fixing a ticket, add appropriate trailers
- If fixing a regression, add a "Fixes:" trailer with the commit id and title
- For breaking changes, add `BREAKING CHANGE: <description>` trailer
</commit_message_format>
