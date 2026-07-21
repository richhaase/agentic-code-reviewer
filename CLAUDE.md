# CLAUDE.md - Development Guide

This file provides guidance for AI assistants working on the ACR codebase.

## Project Overview

ACR (Agentic Code Reviewer) is a Go CLI that runs parallel code reviews using LLM agents (Antigravity, Codex, or Claude). It spawns N reviewers, collects their findings, deduplicates/clusters them via an LLM summarizer, and optionally posts results to GitHub PRs.

## Build & Test Commands

Use `make` for all build/test/lint operations. Run `make help` to see available targets.

```bash
make build       # Build with version info (outputs to bin/)
make check       # Run all quality checks (fmt, lint, vet, staticcheck, tests)
make test        # Run all tests
make lint        # Run golangci-lint v2
make staticcheck # Run staticcheck
make fmt         # Format code
make clean       # Clean build artifacts
```

Direct go commands (if needed):

```bash
go test ./...      # Run tests directly
go install ./cmd/acr  # Install locally
```

## Architecture

```
cmd/acr/main.go          # CLI entry point, flag parsing, orchestration
internal/
  agent/                 # LLM agent abstraction layer
    agent.go             # Agent interface (ExecuteReview, ExecuteSummary)
    antigravity.go       # Antigravity agy CLI agent implementation
    codex.go             # Codex CLI agent implementation
    claude.go            # Claude CLI agent implementation
    factory.go           # Agent and parser factory functions
    parser.go            # ReviewParser and SummaryParser interfaces
    *_review_parser.go   # Agent-specific review output parsers
    *_summary_parser.go  # Agent-specific summary output parsers
    prompts.go           # Default review prompts per agent
  config/                # Configuration file support
    config.go            # Load/parse .acr.yaml, resolve precedence (flags > env > config > defaults)
  domain/                # Core types: Finding, AggregatedFinding, GroupedFindings
    finding.go           # Finding types and aggregation logic
    result.go            # ReviewerResult and ReviewStats
    exitcode.go          # Exit code constants
  filter/                # Finding filtering
    filter.go            # Exclude findings by regex pattern matching
  fpfilter/              # False positive filtering
    fpfilter.go          # LLM-based false positive detection and removal
  feedback/              # PR feedback summarization
    fetch.go             # Fetch PR description and comments via gh CLI
    summarizer.go        # LLM-based summarization of prior PR discussion
  runner/                # Review execution engine
    runner.go            # Parallel reviewer orchestration
    report.go            # Report rendering (terminal + markdown)
  summarizer/            # LLM-based finding summarization
    summarizer.go        # Orchestrates agent execution and output parsing
  github/                # GitHub PR operations via gh CLI
    pr.go                # Post comments, approve PRs, check CI status, poll PR state
  watch/                 # acr watch loop
    watch.go             # Poll/trigger/bounds state machine with injected effects
    clock.go             # Clock abstraction (real + test fakes)
  git/                   # Git operations
    worktree.go          # Temporary worktree management
  terminal/              # Terminal UI
    spinner.go           # Progress spinner
    logger.go            # Styled logging
    colors.go            # ANSI color codes
    format.go            # Text formatting utilities
```

## Key Design Decisions

1. **Multi-Agent Support**: Supports multiple LLM backends (Antigravity, Codex, Claude) via the `Agent` interface. Each agent handles its own CLI invocation and output parsing. Adding new agents requires implementing `Agent`, `ReviewParser`, and `SummaryParser`.

2. **External Dependencies**: Uses LLM CLIs (`agy`, `codex`, `claude`) for reviews and `gh` CLI for GitHub. All are exec'd as subprocesses - no SDK dependencies.

3. **Parallel Execution**: Reviewers run concurrently via goroutines. Results collected via channels with context cancellation support.

4. **Finding Aggregation**: Three-phase process:
   - First: Exact-match deduplication in `domain.AggregateFindings()`
   - Then: Semantic clustering via LLM in `summarizer.Summarize()`
   - Finally: LLM-based false positive filtering in `fpfilter.Filter()` (enabled by default, configurable threshold)

5. **Exit Codes**: Semantic exit codes (0=clean, 1=findings, 2=error, 130=interrupted) for CI integration.

6. **Terminal Detection**: Colors auto-disabled when stdout is not a TTY.

## Code Comments — Global Rule

Code comments are prohibited throughout this repository. Do not add or retain
inline comments, block comments, doc comments, TODO or FIXME comments, section
markers, commented-out code, explanatory annotations, or comments in tests,
scripts, configuration, and code examples. A comment introduced in a change is
a defect and must be removed before the change is complete. Express intent
through names, types, functions, and code structure instead.

The sole exception is a very brief comment that is explicitly required as part
of public API documentation, such as text consumed by API documentation tooling
or required for API consumers. This exception must not be used for ordinary
implementation explanation, internal documentation, or optional doc comments.
User-facing prose in README files, CLI help, and other documentation is allowed,
but fenced code examples must follow the no-comments rule.

## Code Patterns

- **Error handling**: Return errors up the call stack. Log at the top level in main.go.
- **Context propagation**: All long-running operations accept `context.Context` for cancellation.
- **Configuration**: Three-tier precedence (flags > env vars > .acr.yaml > defaults). See `internal/config/config.go` for resolution logic.
- **Testing**: Table-driven tests preferred. See `internal/domain/finding_test.go` for examples.

## Adding New Features

When adding features:

1. **Domain types go in `internal/domain/`** - Keep them simple, no external dependencies.
2. **New CLI flags** - Add to `cmd/acr/main.go`, follow existing pattern with env var defaults.
3. **Tests required** - Add `_test.go` files alongside implementation.
4. **Lint clean** - Run `make lint` before committing.

## Common Tasks

### Adding a new CLI flag

```go
var myFlag string

rootCmd.Flags().StringVarP(&myFlag, "my-flag", "m", getEnvStr("ACR_MY_FLAG", "default"), "Description")
```

### Adding a new finding field

1. Update `domain.Finding` struct
2. Update `domain.AggregatedFinding` if needed
3. Update aggregation logic in `domain.AggregateFindings()`
4. Update summarizer prompt if the field should be considered in clustering
5. Add tests

## Release Process

Releases are automated via GoReleaser when tags are pushed:

```bash
git tag v1.2.3
git push origin v1.2.3
```

This triggers `.github/workflows/release.yml` which builds binaries for Linux/macOS (amd64/arm64), creates GitHub releases, and updates the Homebrew tap.
