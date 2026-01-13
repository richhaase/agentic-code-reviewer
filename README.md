# ACR - Agentic Code Reviewer

A CLI tool that runs parallel AI-powered code reviews using [Codex CLI](https://github.com/openai/codex-cli) and aggregates findings intelligently.

## How It Works

ACR spawns multiple parallel reviewers, each running `codex exec review` independently. The parallel approach increases coverage: different reviewers may catch different issues. After all reviewers complete, ACR aggregates and clusters similar findings using an LLM summarizer, then presents a consolidated report.

```
┌─────────────┐
│   acr       │
└─────┬───────┘
      │ spawns N reviewers
      ▼
┌──────────┬──────────┬──────────┐
│Reviewer 1│Reviewer 2│Reviewer N│  (parallel codex exec review)
└────┬─────┴────┬─────┴────┬─────┘
     │          │          │
     └──────────┼──────────┘
                ▼
        ┌───────────────┐
        │  Summarizer   │  (clusters & deduplicates)
        └───────┬───────┘
                ▼
        ┌───────────────┐
        │ Consolidated  │
        │    Report     │
        └───────────────┘
```

## Installation

### Homebrew (macOS)

```bash
brew install richhaase/tap/acr
```

### From Source

```bash
go install github.com/richhaase/agentic-code-reviewer/cmd/acr@latest
```

### Prerequisites

- **codex CLI** - Required for running reviews. Install via `npm install -g @openai/codex-cli`
- **gh CLI** - Optional, for posting comments/approvals to GitHub PRs

## Usage

```bash
# Review current branch against main with 5 parallel reviewers
acr

# Review with custom settings
acr --reviewers 10 --base develop --timeout 10m

# Review a specific branch in a temporary worktree
acr --worktree-branch feature/my-branch

# Local mode (don't post to PR)
acr --local

# Auto-approve without prompting
acr --yes

# Verbose mode (show reviewer messages as they arrive)
acr --verbose
```

### Options

| Flag                | Short | Default | Description                              |
| ------------------- | ----- | ------- | ---------------------------------------- |
| `--reviewers`       | `-r`  | 5       | Number of parallel reviewers             |
| `--base`            | `-b`  | main    | Base ref for diff comparison             |
| `--timeout`         | `-t`  | 5m      | Timeout per reviewer                     |
| `--retries`         | `-R`  | 1       | Retry failed reviewers N times           |
| `--verbose`         | `-v`  | false   | Print agent messages in real-time        |
| `--local`           | `-l`  | false   | Skip posting to GitHub PR                |
| `--worktree-branch` | `-B`  |         | Review a branch in a temp worktree       |
| `--yes`             | `-y`  | false   | Auto-submit without prompting            |
| `--no`              | `-n`  | false   | Skip submitting review                   |
| `--exclude-pattern` |       |         | Exclude findings matching regex (repeat) |
| `--no-config`       |       | false   | Skip loading .acr.yaml config file       |

### Environment Variables

- `REVIEW_REVIEWERS` - Default number of reviewers
- `REVIEW_BASE_REF` - Default base ref
- `REVIEW_TIMEOUT` - Default timeout (e.g., "5m" or "300")
- `REVIEW_RETRIES` - Default retry count
- `REVIEW_EXCLUDE_PATTERNS` - Comma-separated list of exclude patterns

## Configuration

Create `.acr.yaml` in your repository root to configure persistent settings:

```yaml
filters:
  exclude_patterns:
    - "Next\\.js forbids"      # Regex patterns to exclude
    - "deprecated API"
    - "consider using"
```

### Behavior

- Config file is loaded from the git repository root
- Missing config file is not an error (empty defaults used)
- Invalid YAML or regex patterns produce an error
- CLI `--exclude-pattern` flags are merged with config patterns (union)
- Use `--no-config` to skip loading the config file for a single run

## Exit Codes

| Code | Meaning                      |
| ---- | ---------------------------- |
| 0    | No findings                  |
| 1    | Findings found               |
| 2    | Error                        |
| 130  | Interrupted (SIGINT/SIGTERM) |

## GitHub Integration

When not in `--local` mode, ACR can:

- **Post findings** as PR comments when issues are found
- **Approve PRs** with an LGTM message when no issues are found (checks CI status first)

Requires the `gh` CLI to be authenticated.

## Development

```bash
# Run tests
go test ./...

# Run linter
go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@latest run

# Build
go build -o acr ./cmd/acr
```

## License

Apache License 2.0 - see [LICENSE](LICENSE)
