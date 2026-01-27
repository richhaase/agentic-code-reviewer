# Ralph Build Mode

You are working on the ACR (Agentic Code Reviewer) codebase. Your task is to implement ONE user story from the PRD, then exit.

## Instructions

### 1. Study Context

Read these files to understand the current state:

- **PRD file** - Path specified at top of this prompt (Current PRD: ...)
- **Progress file** - Path specified at top of this prompt (Progress File: ...)
- `CLAUDE.md` - Project conventions and commands
- `AGENTS.md` - Workflow instructions

### 2. Select Next Story

From the PRD file, review ALL user stories where `"passes": false`.

**Choose the highest priority story based on:**
- **Dependencies** - Some stories build on others (e.g., data structures before logic that uses them)
- **Foundation first** - Prefer stories that unblock other work
- **Risk reduction** - Tackle uncertain or complex stories earlier when possible

If ALL stories have `"passes": true`, output `<promise>COMPLETE</promise>` and exit.

**Document your choice** - In your implementation, briefly note why you selected this story over others.

### 3. Implement the Story

For the selected story:

1. **Read the files** listed in the story's `files` array
2. **Study related code** to understand patterns and conventions
3. **Implement the changes** to satisfy ALL acceptance criteria
4. **Follow existing patterns** - match the code style of surrounding code

### 4. Verify Quality

Run the quality checks:

```bash
make check
```

This runs fmt, lint, vet, staticcheck, and tests. ALL must pass before committing.

If checks fail:
- Fix the issues
- Run `make check` again
- Repeat until all checks pass

### 5. Update Progress

Append learnings to the progress file:

```
## Iteration N - Story [ID]: [Title]
- Why this story was selected (dependencies, priority reasoning)
- What was implemented
- Any gotchas or patterns discovered
- Files modified
- What stories are now unblocked (if any)
```

### 6. Mark Story Complete

Update the PRD file to set `"passes": true` for the completed story.

### 7. Commit Changes

Create a commit with all changes:

```bash
git add -A
git commit -m "feat(fpfilter): [brief description]

Implements story [ID]: [title]

- [bullet points of changes]

Co-Authored-By: Claude <noreply@anthropic.com>"
```

### 8. Exit

After committing, exit cleanly. The loop will start a fresh iteration for the next story.

---

## Critical Rules

- **ONE story per iteration** - Do not implement multiple stories
- **CHOOSE WISELY** - Pick the highest priority unblocked story, not just the first one
- **ALL acceptance criteria** - Story is not complete until all criteria are met
- **Tests MUST pass** - Never commit if `make check` fails
- **Follow conventions** - Read CLAUDE.md, match existing code patterns
- **Update the progress file** - Document learnings AND selection reasoning for future iterations
- **Update the PRD file** - Mark story as `passes: true` when complete

## Quality Checklist

Before committing, verify:

- [ ] All acceptance criteria from the story are implemented
- [ ] `make check` passes (fmt, lint, vet, staticcheck, tests)
- [ ] Code follows existing patterns in the codebase
- [ ] No unrelated changes included
- [ ] the progress file updated with learnings
- [ ] the PRD file updated with `passes: true`
