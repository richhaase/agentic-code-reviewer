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
| `--concurrency`     | `-c`  | -r      | Max concurrent reviewers (see below)     |
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
| `--agent`           | `-a`  | codex   | Agent backend (codex, claude, gemini)    |
| `--prompt`          |       |         | Custom review prompt (inline)            |
| `--prompt-file`     |       |         | Path to file containing review prompt    |

### Concurrency Control

The `--concurrency` flag limits how many reviewers run simultaneously, independent of the total reviewer count. This helps avoid API rate limits when running many reviewers or using high retry counts.

```bash
# Run 15 total reviewers, but only 5 at a time
acr -r 15 -c 5

# With retries, -c prevents retry storms from overwhelming the API
acr -r 10 -R 3 -c 3
```

By default, concurrency equals the reviewer count (all run in parallel).

### Agent Selection

ACR supports multiple AI backends for code review:

| Agent | CLI | Description |
|-------|-----|-------------|
| `codex` | [Codex CLI](https://github.com/openai/codex-cli) | Default. Uses built-in `codex exec review` |
| `claude` | [Claude Code](https://github.com/anthropics/claude-code) | Anthropic's Claude via CLI |
| `gemini` | [Gemini CLI](https://github.com/google-gemini/gemini-cli) | Google's Gemini via CLI |

```bash
# Use Claude instead of Codex
acr --agent claude

# Use Gemini
acr -a gemini
```

Different agents may find different issues. The appropriate CLI must be installed and authenticated for the selected agent.

### Custom Prompts

Override the default review prompt to focus on specific concerns:

```bash
# Inline prompt
acr --prompt "Review for security vulnerabilities only. Output: file:line: description"

# Prompt from file
acr --prompt-file prompts/security-review.txt
```

Effective prompts should:
- Be specific about what to look for
- Explicitly state what to skip (reduces noise)
- Specify the desired output format

The git diff is automatically appended to your prompt.

### Environment Variables

| Variable                  | Description                              |
| ------------------------- | ---------------------------------------- |
| `ACR_REVIEWERS`           | Default number of reviewers              |
| `ACR_CONCURRENCY`         | Default max concurrent reviewers         |
| `ACR_BASE_REF`            | Default base ref                         |
| `ACR_TIMEOUT`             | Default timeout (e.g., "5m" or "300")    |
| `ACR_RETRIES`             | Default retry count                      |
| `ACR_AGENT`               | Default agent backend                    |
| `ACR_REVIEW_PROMPT`       | Default review prompt                    |
| `ACR_REVIEW_PROMPT_FILE`  | Path to default review prompt file       |

## Configuration

Create `.acr.yaml` in your repository root to configure persistent settings:

```yaml
# All fields are optional - defaults shown in comments
reviewers: 5              # Number of parallel reviewers
concurrency: 5            # Max concurrent reviewers (defaults to reviewers)
base: main                # Base ref for diff comparison
timeout: 5m               # Timeout per reviewer (supports "5m", "300s", or 300)
retries: 1                # Retry failed reviewers N times
agent: codex              # Agent backend (codex, claude, gemini)

# Custom review prompt (inline or file)
# review_prompt: |
#   Review for bugs only. Skip style issues.
#   Output: file:line: description
# review_prompt_file: prompts/security.txt

filters:
  exclude_patterns:       # Regex patterns to exclude from findings
    - "Next\\.js forbids"
    - "deprecated API"
    - "consider using"
```

### Precedence

Configuration is resolved with the following precedence (highest to lowest):
1. CLI flags (e.g., `--reviewers 10`)
2. Environment variables (e.g., `ACR_REVIEWERS=10`)
3. Config file (`.acr.yaml`)
4. Built-in defaults

### Behavior

- Config file is loaded from the git repository root
- Missing config file is not an error (empty defaults used)
- Invalid YAML or regex patterns produce an error
- Unknown keys in config file produce a warning with "did you mean?" suggestions
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
# List available targets
make help

# Build with version info (outputs to bin/)
make build

# Run all quality checks (format, lint, vet, staticcheck, tests)
make check

# Run tests
make test

# Run linter
make lint

# Run staticcheck
make staticcheck

# Format code
make fmt

# Clean build artifacts
make clean
```

## License

Apache License 2.0 - see [LICENSE](LICENSE)
