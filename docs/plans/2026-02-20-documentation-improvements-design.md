# Documentation Improvements Design

**Date**: 2026-02-20

## Goal

Improve ACR documentation for both users and contributors without drowning a small project in docs. Keep it tight.

## Scope

Three deliverables:

1. **Asciinema demo in README** — show the full PR review flow
2. **CONTRIBUTING.md** — prerequisites, workflow, PR requirements
3. **PR template update** — add ACR run checkbox

## Deliverable 1: README + Asciinema Demo

Add an SVG recording immediately after the project description, before Quick Start.

```markdown
# ACR - Agentic Code Reviewer

A CLI tool that runs parallel AI-powered code reviews...

<p align="center">
  <img src="docs/assets/demo.svg" alt="ACR demo" width="800">
</p>

## Quick Start
```

Add a CONTRIBUTING.md link in the Development section:

```markdown
See [CONTRIBUTING.md](CONTRIBUTING.md) for development setup and contribution guidelines.
```

### Recording Details

**What the demo shows** (~45 seconds):

1. `acr --pr <number>` against a real PR
2. Spinner showing reviewers completing
3. Summarizer and FP filter phases
4. Consolidated report with findings
5. Submission prompt — choose to post
6. Confirmation that review was posted to the PR

**Production:**
- Record with `asciinema rec`, convert to SVG via `svg-term`
- Store SVG at `docs/assets/demo.svg`
- Store recording script at `docs/assets/demo-script.sh` for reproducibility
- Recording must be done manually (requires live ACR run with real agent output)

## Deliverable 2: CONTRIBUTING.md

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

## Deliverable 3: PR Template Update

Add ACR run checkbox to `.github/PULL_REQUEST_TEMPLATE.md`:

```markdown
## Test Plan

- [ ] `make check` passes
- [ ] ACR run completed (see CONTRIBUTING.md for requirements)
- [ ] New tests added (if applicable)
- [ ] Manual testing performed (describe below)
```

## Out of Scope

- No additional docs pages (troubleshooting, CI examples, etc.)
- No README restructuring beyond the demo embed and CONTRIBUTING link
- No changelog generation
- Existing bug report and feature request templates are unchanged
