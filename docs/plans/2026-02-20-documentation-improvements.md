# Documentation Improvements Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add asciinema demo to README, create CONTRIBUTING.md, and update PR template.

**Architecture:** Three independent file changes plus tooling setup for asciinema recording. The recording itself is a manual step performed by the maintainer.

**Tech Stack:** Markdown, asciinema, svg-term-cli (npm)

---

### Task 1: Create CONTRIBUTING.md

**Files:**
- Create: `CONTRIBUTING.md`

**Step 1: Create the file**

```markdown
# Contributing to ACR

Contributions are greatly appreciated! Please note that all contributions
are reviewed at the maintainer's discretion — submitting a PR does not
obligate acceptance.

## Prerequisites

- Go 1.25+
- At least one LLM CLI (codex, claude, or gemini) installed and authenticated
- gh CLI (for integration testing)

## Development Workflow

1. Fork and clone the repo
2. Create a feature branch
3. Make your changes
4. Run `make check` (must pass — covers fmt, lint, vet, staticcheck, tests)
5. Open a PR

## PR Requirements

All PRs must include evidence of a successful ACR run against the
contributed code using the repository's `.acr.yaml` configuration
(which uses all three agent types with 6 reviewers):

    acr --pr <your-pr-number>

If you don't have access to all three agents (codex, claude, gemini),
you must review with at least 2. Override with:

    acr --pr <your-pr-number> --reviewer-agent codex,claude

## Project Structure

See [CLAUDE.md](CLAUDE.md) for architecture overview, code patterns,
and guidance on adding features.

## AI Contributors

This project uses CLAUDE.md as the primary development guide for AI
assistants. If you're using Claude Code, Codex, or similar tools to
contribute, that file has everything you need.
```

**Step 2: Commit**

```bash
git add CONTRIBUTING.md
git commit -m "docs: add CONTRIBUTING.md with PR requirements"
```

---

### Task 2: Update PR Template

**Files:**
- Modify: `.github/PULL_REQUEST_TEMPLATE.md:15-17`

**Step 1: Add ACR run checkbox**

Change the Test Plan section from:

```markdown
## Test Plan

<!-- How were these changes tested? -->

- [ ] `make check` passes
- [ ] New tests added (if applicable)
- [ ] Manual testing performed (describe below)
```

To:

```markdown
## Test Plan

<!-- How were these changes tested? -->

- [ ] `make check` passes
- [ ] ACR run completed (see CONTRIBUTING.md for requirements)
- [ ] New tests added (if applicable)
- [ ] Manual testing performed (describe below)
```

**Step 2: Commit**

```bash
git add .github/PULL_REQUEST_TEMPLATE.md
git commit -m "docs: add ACR run checkbox to PR template"
```

---

### Task 3: Set Up Asciinema Recording Infrastructure

**Files:**
- Create: `docs/assets/demo-script.sh`

**Step 1: Create the docs/assets directory**

```bash
mkdir -p docs/assets
```

**Step 2: Create the recording script**

This script is what the maintainer runs inside `asciinema rec` to produce a clean, repeatable demo. It uses `expect`-style typing simulation for a polished look.

```bash
#!/usr/bin/env bash
# Demo recording script for ACR README.
# Usage:
#   1. Have a repo with an open PR that will produce findings
#   2. asciinema rec demo.cast --cols 100 --rows 30
#   3. Run this script (or type the commands manually)
#   4. Convert: svg-term --in demo.cast --out docs/assets/demo.svg --window

# Set a clean prompt for the recording
export PS1="$ "

# Review a PR
acr --pr <PR_NUMBER>

# When prompted, choose to post the review
```

**Step 3: Create a .gitkeep for the SVG location**

The actual `demo.svg` will be added after the maintainer records the demo. Add a note:

```bash
# docs/assets/README (not a .md — just a note)
```

No README needed — the script is self-documenting.

**Step 4: Commit**

```bash
git add docs/assets/demo-script.sh
git commit -m "docs: add asciinema demo recording script"
```

---

### Task 4: Update README with Demo Placeholder and CONTRIBUTING Link

**Files:**
- Modify: `README.md:1-5` (add demo image after description)
- Modify: `README.md:347-373` (add CONTRIBUTING link in Development section)

**Step 1: Add demo image after the project description**

After line 3 (the description paragraph), add:

```markdown

<p align="center">
  <img src="docs/assets/demo.svg" alt="ACR demo" width="800">
</p>
```

Note: This will show a broken image until the recording is created. Alternatively, wrap it in a comment until the SVG exists:

```markdown
<!-- Uncomment after recording the demo:
<p align="center">
  <img src="docs/assets/demo.svg" alt="ACR demo" width="800">
</p>
-->
```

**Step 2: Add CONTRIBUTING link in Development section**

After the Development section's closing code block (after `make clean`), before `## License`, add:

```markdown

See [CONTRIBUTING.md](CONTRIBUTING.md) for contribution guidelines.
```

**Step 3: Commit**

```bash
git add README.md
git commit -m "docs: add demo placeholder and CONTRIBUTING link to README"
```

---

### Task 5: Record the Demo (Manual — Maintainer Only)

This task cannot be automated. The maintainer must:

1. Install tooling: `brew install asciinema` and `npm install -g svg-term-cli`
2. Find or create a PR that will produce findings
3. Record: `asciinema rec demo.cast --cols 100 --rows 30`
4. During recording, run `acr --pr <number>` and complete the full flow
5. Convert: `svg-term --in demo.cast --out docs/assets/demo.svg --window`
6. If README used the comment placeholder, uncomment the image tag
7. Commit:

```bash
git add docs/assets/demo.svg README.md
git commit -m "docs: add asciinema demo recording"
```
