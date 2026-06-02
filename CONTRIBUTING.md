# Contributing to ACR

Contributions are greatly appreciated! Please note that all contributions
are reviewed at the maintainer's discretion — submitting a PR does not
obligate acceptance.

## Prerequisites

- Go 1.25+
- At least one LLM CLI (agy, codex, or claude) installed and authenticated
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
(which uses the configured agent mix):

Note: the repository configuration uses Claude Code for part of the review, but
we recommend against using Claude with ACR unless you intentionally accept
Anthropic's non-interactive `claude -p`/Agent SDK billing model. ACR invokes
Claude in non-interactive mode (`claude -p`; ACR uses the equivalent `--print`
flag internally) for Claude review and summary phases. Starting June 15, 2026,
subscription-authenticated usage draws from a separate Agent SDK credit, while
`ANTHROPIC_API_KEY` authentication uses pay-as-you-go API billing. Prefer
overriding `--reviewer-agent`, `--summarizer-agent`, `--reviewers`, or
`--concurrency` if you want to avoid Claude usage.

    acr --pr <your-pr-number>

If you don't have access to all configured agents (agy, codex, claude),
you must review with at least 2. Override with:

    acr --pr <your-pr-number> --reviewer-agent agy,codex

## Project Structure

See [CLAUDE.md](CLAUDE.md) for architecture overview, code patterns,
and guidance on adding features.

## AI Contributors

This project uses CLAUDE.md as the primary development guide for AI
assistants. If you're using Claude Code, Codex, or similar tools to
contribute, that file has everything you need.
