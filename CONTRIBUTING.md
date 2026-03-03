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
