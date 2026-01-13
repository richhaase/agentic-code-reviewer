# CLAUDE.md - Development Guide

This file provides guidance for AI assistants working on the ACR codebase.

## Project Overview

ACR (Agentic Code Reviewer) is a Go CLI that runs parallel code reviews using Codex CLI. It spawns N reviewers, collects their findings, deduplicates/clusters them via an LLM summarizer, and optionally posts results to GitHub PRs.

## Build & Test Commands

Use `just` recipes for all build/test/lint operations. Run `just` to see available recipes.

```bash
just build    # Build with version info (outputs to bin/)
just test     # Run all tests
just lint     # Run golangci-lint v2
just fmt      # Format code
just clean    # Clean build artifacts
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
  domain/                # Core types: Finding, AggregatedFinding, GroupedFindings
    finding.go           # Finding types and aggregation logic
    result.go            # ReviewerResult and ReviewStats
    exitcode.go          # Exit code constants
  runner/                # Review execution engine
    runner.go            # Parallel reviewer orchestration
    report.go            # Report rendering (terminal + markdown)
  summarizer/            # LLM-based finding summarization
    summarizer.go        # Calls codex exec with clustering prompt
  github/                # GitHub PR operations via gh CLI
    pr.go                # Post comments, approve PRs, check CI status
  git/                   # Git operations
    worktree.go          # Temporary worktree management
  terminal/              # Terminal UI
    spinner.go           # Progress spinner
    logger.go            # Styled logging
    colors.go            # ANSI color codes
    format.go            # Text formatting utilities
```

## Key Design Decisions

1. **External Dependencies**: Uses `codex` CLI for reviews and `gh` CLI for GitHub. Both are exec'd as subprocesses - no SDK dependencies.

2. **Parallel Execution**: Reviewers run concurrently via goroutines. Results collected via channels with context cancellation support.

3. **Finding Aggregation**: Two-phase process:
   - First: Exact-match deduplication in `domain.AggregateFindings()`
   - Then: Semantic clustering via LLM in `summarizer.Summarize()`

4. **Exit Codes**: Semantic exit codes (0=clean, 1=findings, 2=error, 130=interrupted) for CI integration.

5. **Terminal Detection**: Colors auto-disabled when stdout is not a TTY.

## Code Patterns

- **Error handling**: Return errors up the call stack. Log at the top level in main.go.
- **Context propagation**: All long-running operations accept `context.Context` for cancellation.
- **Configuration**: Flags with env var fallbacks. See `getEnvStr`, `getEnvInt`, `getEnvDuration`.
- **Testing**: Table-driven tests preferred. See `internal/domain/finding_test.go` for examples.

## Adding New Features

When adding features:

1. **Domain types go in `internal/domain/`** - Keep them simple, no external dependencies.
2. **New CLI flags** - Add to `cmd/acr/main.go`, follow existing pattern with env var defaults.
3. **Tests required** - Add `_test.go` files alongside implementation.
4. **Lint clean** - Run `just lint` before committing.

## Common Tasks

### Adding a new CLI flag

```go
// In cmd/acr/main.go, add to var block:
var myFlag string

// In run(), add flag definition:
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
